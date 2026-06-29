// Command sshakku tends the SSH agent: it computes the per-user runtime
// paths, keeps the agent healthy, and loads keys with passphrases pulled from
// the OS secret store. The login shell wires it in by evaluating its output:
//
//	eval "$(sshakku shell-init)"
package main

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/OrbintSoft/sshakku/internal/agent"
	"github.com/OrbintSoft/sshakku/internal/giveup"
	"github.com/OrbintSoft/sshakku/internal/keyring"
	"github.com/OrbintSoft/sshakku/internal/keys"
	"github.com/OrbintSoft/sshakku/internal/paths"
	"github.com/OrbintSoft/sshakku/internal/sessionlog"
)

// agentLockWait bounds how long a login blocks for the start lock before it
// proceeds without it, so a stuck holder slows the login but never hangs it.
const agentLockWait = 5 * time.Second

// defaultKeyLifetime caps how long an added key stays in the agent before it
// expires and must be re-added from the vault. $SSHAKKU_KEY_LIFETIME overrides
// it; a zero or negative value disables expiry.
const defaultKeyLifetime = 8 * time.Hour

// defaultGiveupTTL bounds how long a key stays in the give-up state after its
// retries are exhausted, before a later shell tries it again.
// $SSHAKKU_GIVEUP_TTL overrides it; a zero or negative value never expires (the
// record then clears only on a successful add or when the runtime dir is wiped).
const defaultGiveupTTL = time.Hour

const usage = `sshakku — SSH agent and key shepherd

usage: sshakku <command>

commands:
  shell-init     drive the agent healthy and print shell assignments to eval
  ensure-agent   drive the agent to a healthy state and print agent_sock
  load-keys      add the user's ssh keys to the agent (interactive sessions)
  help           show this help
`

func main() {
	// ssh-add execs this binary as its SSH_ASKPASS program, passing only the
	// prompt as an argument and marking the call via the environment. Handle that
	// before subcommand dispatch and return the passphrase from the keyring.
	if os.Getenv(keys.EnvAskpassMode) != "" {
		os.Exit(askpass(os.Stdout))
	}
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

// run dispatches a subcommand and returns the process exit code. Output goes to
// the supplied writers so the command is testable without touching real stdio.
func run(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "shell-init":
		return shellInit(stdout, stderr)
	case "ensure-agent":
		return ensureAgent(stdout, stderr)
	case "load-keys":
		return loadKeys(stderr)
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "sshakku: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// shellInit resolves and creates the per-user runtime layout, drives the fixed
// socket to a healthy ssh-agent, then prints the result as shell assignments for
// the login entrypoint to eval:
//
//	agent_sock='…'
//	agent_lock='…'
//	log_file='…'
//
// agent_sock is the live socket EnsureAgent settled on, which may be an adopted
// agent rather than the fixed path. Only these assignments go to stdout;
// diagnostics and anomalies go to stderr and the session log.
func shellInit(stdout, stderr io.Writer) int {
	env := paths.FromOS()
	layout := paths.Resolve(env, paths.ProbeDir).WithSocketToken(paths.SocketToken())
	if err := paths.Ensure(layout); err != nil {
		// Best-effort: the log dir may be the very thing we failed to create.
		_ = sessionlog.New(layout.LogFile).Log("ERROR", fmt.Sprintf("shell-init: %v", err))
		_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
		return 1
	}
	paths.CleanupLegacyAgentDir(env.Home)

	liveSock, code := runEnsure(stderr, env, layout)
	if code != 0 {
		return code
	}

	assignments := []struct{ name, value string }{
		{"agent_sock", liveSock},
		{"agent_lock", layout.AgentLock},
		{"log_file", layout.LogFile},
	}
	for _, a := range assignments {
		if _, err := fmt.Fprintf(stdout, "%s=%s\n", a.name, shellSingleQuote(a.value)); err != nil {
			_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
			return 1
		}
	}
	return 0
}

// ensureAgent resolves the runtime layout, drives the fixed socket to a healthy
// ssh-agent (starting, reaping, or adopting as needed), and prints the live
// socket as a shell assignment:
//
//	agent_sock='…'
//
// It is a standalone entry point for exercising the lifecycle; the login path
// reaches the same logic through shell-init, which adds the other assignments.
func ensureAgent(stdout, stderr io.Writer) int {
	env := paths.FromOS()
	layout := paths.Resolve(env, paths.ProbeDir).WithSocketToken(paths.SocketToken())
	if err := paths.Ensure(layout); err != nil {
		_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
		return 1
	}

	liveSock, code := runEnsure(stderr, env, layout)
	if code != 0 {
		return code
	}
	if _, err := fmt.Fprintf(stdout, "agent_sock=%s\n", shellSingleQuote(liveSock)); err != nil {
		_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
		return 1
	}
	return 0
}

// askpass is the SSH_ASKPASS program ssh-add execs while adding a key: it reads
// the passphrase the loader stashed in the @u keyring, identified by the serial in
// $SSHAKKU_KEYCTL_SERIAL, prints it on stdout for ssh-add, and unlinks the
// one-shot entry. The passphrase never touches stderr or argv; only the keyring
// serial crosses the environment. Diagnostics go to the session log alone, so the
// success path stays silent.
func askpass(stdout io.Writer) int {
	log := sessionlog.New(paths.Resolve(paths.FromOS(), paths.ProbeDir).LogFile)

	raw := os.Getenv(keys.EnvKeyctlSerial)
	serial, err := strconv.Atoi(raw)
	if err != nil {
		_ = log.Log("ERROR", "askpass: missing or malformed keyctl serial")
		return 1
	}

	pass, readErr := keyring.Read(keyring.Serial(serial))
	// One-shot: drop the entry whether or not the read succeeded, so a leaked
	// passphrase cannot linger in the keyring.
	_ = keyring.Unlink(keyring.Serial(serial))
	if readErr != nil {
		_ = log.Log("ERROR", fmt.Sprintf("askpass: read keyring serial …%s: %v", tail(raw, 3), readErr))
		return 1
	}
	if len(pass) == 0 {
		_ = log.Log("ERROR", fmt.Sprintf("askpass: empty passphrase for serial …%s", tail(raw, 3)))
		return 1
	}

	// ssh-add reads the passphrase from stdout and strips the trailing newline.
	if _, err := fmt.Fprintf(stdout, "%s\n", pass); err != nil {
		_ = log.Log("ERROR", fmt.Sprintf("askpass: write passphrase: %v", err))
		return 1
	}
	_ = log.Log("INFO", fmt.Sprintf("askpass: provided passphrase for serial …%s", tail(raw, 3)))
	return 0
}

// tail returns the last n characters of s, for logging a key serial without
// recording it in full.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// loadKeys adds the user's ~/.ssh keys to the agent: it skips keys already loaded
// and, for the rest, pulls each passphrase from the secret store (or prompts) and
// hands it to ssh-add out of band. The login entrypoint calls it only in
// interactive shells. SSH_ASKPASS points at this very binary, which ssh-add re-execs
// to fetch the passphrase from the keyring. The success path is silent; problems go
// to the session log (and stderr for a hard failure).
func loadKeys(stderr io.Writer) int {
	env := paths.FromOS()
	layout := paths.Resolve(env, paths.ProbeDir).WithSocketToken(paths.SocketToken())
	log := sessionlog.New(layout.LogFile)

	self, err := os.Executable()
	if err != nil {
		_ = log.Log("ERROR", fmt.Sprintf("load-keys: locate self: %v", err))
		_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
		return 1
	}

	lifetime, lerr := keyLifetime(os.Getenv("SSHAKKU_KEY_LIFETIME"))
	if lerr != nil {
		_ = log.Log("ERROR", lerr.Error())
	}

	ttl, terr := giveupTTL(os.Getenv("SSHAKKU_GIVEUP_TTL"))
	if terr != nil {
		_ = log.Log("ERROR", terr.Error())
	}
	var giveupStore keys.GiveupStore
	if !isTruthy(os.Getenv("SSHAKKU_NO_GIVEUP")) {
		giveupStore = giveup.Store{
			Dir: filepath.Join(filepath.Dir(layout.AgentSock), "giveup"),
			TTL: ttl,
		}
	}

	runner := keys.ExecRunner{}
	prompter := keys.KDialogPrompter{Runner: runner}
	guiEnv := keys.GUIEnv{
		WaylandDisplay: os.Getenv("WAYLAND_DISPLAY"),
		Display:        os.Getenv("DISPLAY"),
	}

	loader := keys.Loader{
		Keys:   keys.Enumerator{Dir: filepath.Join(env.Home, ".ssh")},
		Runner: runner,
		Secret: keys.SecretToolBackend{Runner: runner, User: currentUser()},
		Prompt: prompter,
		Adder:  keys.ExecKeyAdder{AskpassProg: self, KeyLifetime: lifetime},
		Log:    log,
		Giveup: giveupStore,
		Config: keys.Config{
			GUI:         keys.GUIAvailable(guiEnv, runner, prompter),
			MaxAttempts: envInt(os.Getenv("SSHAKKU_MAX_ATTEMPTS")),
		},
	}
	if err := loader.LoadKeys(); err != nil {
		_ = log.Log("ERROR", fmt.Sprintf("load-keys: %v", err))
		_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
		return 1
	}
	return 0
}

// keyLifetime resolves the agent key lifetime from $SSHAKKU_KEY_LIFETIME (a Go
// duration such as "1h" or "20m"), defaulting to defaultKeyLifetime. A zero or
// negative value disables expiry (the key stays until the agent does); a
// malformed value falls back to the default and is returned with an error for
// the caller to log.
func keyLifetime(raw string) (time.Duration, error) {
	if raw == "" {
		return defaultKeyLifetime, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return defaultKeyLifetime, fmt.Errorf("invalid SSHAKKU_KEY_LIFETIME %q: %w", raw, err)
	}
	if d < 0 {
		return 0, nil
	}
	return d, nil
}

// giveupTTL resolves the give-up TTL from $SSHAKKU_GIVEUP_TTL (a Go duration
// such as "1h"), defaulting to defaultGiveupTTL. A zero or negative value means
// "never expire"; a malformed value falls back to the default and is returned
// with an error for the caller to log.
func giveupTTL(raw string) (time.Duration, error) {
	if raw == "" {
		return defaultGiveupTTL, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return defaultGiveupTTL, fmt.Errorf("invalid SSHAKKU_GIVEUP_TTL %q: %w", raw, err)
	}
	if d < 0 {
		return 0, nil
	}
	return d, nil
}

// envInt parses a positive integer from raw, returning 0 (let the consumer use
// its own default) for an empty, malformed, or non-positive value.
func envInt(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// isTruthy reports whether raw is a recognised affirmative value, used for
// boolean opt-out environment switches.
func isTruthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// currentUser returns the login name for the secret-store "username" attribute,
// matching $USER so entries the earlier shell version stored are still found.
func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

// runEnsure drives the fixed socket to a healthy ssh-agent for the resolved
// layout, serialising concurrent logins on the start lock and reporting
// anomalies and errors to stderr and the session log. It returns the live socket
// to expose and a process exit code (0 on success). shell-init and ensure-agent
// share it so the login path and the standalone command drive the agent
// identically; each caller prints the assignments it needs.
func runEnsure(stderr io.Writer, env paths.Env, layout paths.Layout) (string, int) {
	log := sessionlog.New(layout.LogFile)
	m := agent.Manager{
		Prober:    agent.SocketProber{},
		Inspector: agent.Inspector{},
		Runner:    agent.ExecRunner{},
		Signaler:  agent.SysSignaler{},
		Locker:    agent.FlockLocker{Wait: agentLockWait},
	}
	cfg := agent.EnsureConfig{
		FixedSock: layout.AgentSock,
		LegacyDir: filepath.Join(env.Home, ".ssh", "agent"),
		StatePath: filepath.Join(filepath.Dir(layout.AgentSock), "agent.state"),
		LockPath:  layout.AgentLock,
		OurUID:    env.UID,
	}

	res, err := m.EnsureAgent(cfg, log)
	if err != nil {
		_ = log.Log("ERROR", fmt.Sprintf("ensure-agent: %v", err))
		_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
		return "", 1
	}
	if res.Anomaly != "" {
		_, _ = fmt.Fprintf(stderr, "sshakku: %s\n", res.Anomaly)
	}
	return res.LiveSock, 0
}

// shellSingleQuote wraps s in single quotes safe for POSIX shell eval, so paths
// containing spaces or metacharacters survive intact.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
