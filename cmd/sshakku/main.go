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
	"github.com/OrbintSoft/sshakku/internal/config"
	"github.com/OrbintSoft/sshakku/internal/diagnose"
	"github.com/OrbintSoft/sshakku/internal/giveup"
	"github.com/OrbintSoft/sshakku/internal/keyring"
	"github.com/OrbintSoft/sshakku/internal/keys"
	"github.com/OrbintSoft/sshakku/internal/paths"
	"github.com/OrbintSoft/sshakku/internal/sessionlog"
)

// agentLockWait bounds how long a login blocks for the start lock before it
// proceeds without it, so a stuck holder slows the login but never hangs it.
const agentLockWait = 5 * time.Second

const usage = `sshakku — SSH agent and key shepherd

usage: sshakku <command>

commands:
  shell-init     drive the agent healthy and print shell assignments to eval
  ensure-agent   drive the agent to a healthy state and print agent_sock
  load-keys      add the user's ssh keys to the agent (interactive sessions)
  askpass-env    print exports routing ssh's askpass through sshakku (GUI only)
  doctor         report the ssh-agent situation; --fix applies the self-heal
  help           show this help
`

func main() {
	// ssh-add execs this binary as its SSH_ASKPASS program, passing only the
	// prompt as an argument and marking the call via the environment. Handle that
	// before subcommand dispatch and return the passphrase from the keyring.
	if os.Getenv(keys.EnvAskpassMode) != "" {
		os.Exit(askpass(os.Stdout, os.Args[1:]))
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
	case "askpass-env":
		return askpassEnv(stdout, stderr)
	case "doctor":
		return doctor(stdout, stderr, args[1:])
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

// askpass answers an SSH_ASKPASS request. The proactive key-loading path stashes
// the passphrase in the @u keyring and points us at it via $SSHAKKU_KEYCTL_SERIAL;
// with a serial we serve that one-shot stash. Without one we are the reactive
// broker for an interactive ssh whose key has expired, and answer the prompt in
// args from the wallet (or the terminal).
func askpass(stdout io.Writer, args []string) int {
	if os.Getenv(keys.EnvKeyctlSerial) != "" {
		return askpassFromKeyring(stdout)
	}
	return askpassBroker(stdout, args)
}

// askpassBroker answers ssh's passphrase or confirmation prompt: a key passphrase
// comes from the wallet (or the terminal on a miss), other prompts pass through to
// the terminal. Only the reply goes to stdout; diagnostics go to the session log.
func askpassBroker(stdout io.Writer, args []string) int {
	log := sessionlog.New(paths.Resolve(paths.FromOS(), paths.ProbeDir).LogFile)
	broker := keys.Broker{
		Secret: keys.SecretToolBackend{Runner: keys.ExecRunner{}, User: currentUser()},
		TTY:    ttyPrompter{},
		Log:    log,
	}
	reply, ok := broker.Answer(strings.Join(args, " "))
	if !ok {
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "%s\n", reply); err != nil {
		_ = log.Log("ERROR", fmt.Sprintf("askpass: write reply: %v", err))
		return 1
	}
	return 0
}

// askpassFromKeyring reads the passphrase the loader stashed in the @u keyring,
// identified by the serial in $SSHAKKU_KEYCTL_SERIAL, prints it on stdout for
// ssh-add, and unlinks the one-shot entry. The passphrase never touches stderr or
// argv; only the keyring serial crosses the environment. Diagnostics go to the
// session log alone, so the success path stays silent.
func askpassFromKeyring(stdout io.Writer) int {
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

	// Settings come from the environment, the TOML config file, and built-in
	// defaults, in that order of precedence. A missing file is fine; a path,
	// load, or parse problem is logged and the affected setting falls back to
	// its default.
	var file config.File
	if path, perr := config.DefaultPath(); perr != nil {
		_ = log.Log("ERROR", fmt.Sprintf("load-keys: config path: %v", perr))
	} else if f, lerr := config.Load(path); lerr != nil {
		_ = log.Log("ERROR", fmt.Sprintf("load-keys: config %s: %v", path, lerr))
		file = f
	} else {
		file = f
	}
	settings, errs := config.Resolve(file, os.LookupEnv)
	for _, e := range errs {
		_ = log.Log("ERROR", e.Error())
	}

	var giveupStore keys.GiveupStore
	if !settings.NoGiveup {
		giveupStore = giveup.Store{
			Dir: filepath.Join(filepath.Dir(layout.AgentSock), "giveup"),
			TTL: settings.GiveupTTL,
		}
	}

	var notifier keys.Notifier
	if !settings.Quiet {
		notifier = stderrNotifier{w: stderr}
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
		Adder:  keys.ExecKeyAdder{AskpassProg: self, KeyLifetime: settings.KeyLifetime},
		Log:    log,
		Notify: notifier,
		Giveup: giveupStore,
		Config: keys.Config{
			GUI:         keys.GUIAvailable(guiEnv, runner, prompter),
			MaxAttempts: settings.MaxAttempts,
		},
	}
	if err := loader.LoadKeys(); err != nil {
		_ = log.Log("ERROR", fmt.Sprintf("load-keys: %v", err))
		_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
		return 1
	}
	return 0
}

// stderrNotifier surfaces a user-facing notice to the terminal of the
// interactive shell that ran load-keys; $SSHAKKU_QUIET suppresses it.
type stderrNotifier struct{ w io.Writer }

func (n stderrNotifier) Notify(message string) {
	_, _ = fmt.Fprintf(n.w, "sshakku: %s\n", message)
}

// askpassEnv prints the export lines that route this interactive shell's ssh
// passphrase prompts through sshakku's wallet-aware broker, so a key that expires
// from the agent is refilled from the wallet without a terminal prompt. It emits
// them only when a graphical prompter is available — a headless session keeps
// ssh's own terminal prompting — and the login entrypoint evals it in interactive
// shells only, never for non-interactive sessions (scp/rsync/git).
func askpassEnv(stdout, stderr io.Writer) int {
	runner := keys.ExecRunner{}
	guiEnv := keys.GUIEnv{
		WaylandDisplay: os.Getenv("WAYLAND_DISPLAY"),
		Display:        os.Getenv("DISPLAY"),
	}
	if !keys.GUIAvailable(guiEnv, runner, keys.KDialogPrompter{Runner: runner}) {
		return 0
	}
	self, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
		return 1
	}
	if _, err := io.WriteString(stdout, askpassExports(self)); err != nil {
		_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
		return 1
	}
	return 0
}

// askpassExports returns the shell `export` lines pointing ssh's SSH_ASKPASS at
// self's wallet-aware broker; REQUIRE=prefer makes ssh consult it even with a tty.
func askpassExports(self string) string {
	return fmt.Sprintf(
		"export SSH_ASKPASS=%s\nexport SSH_ASKPASS_REQUIRE=prefer\nexport %s=1\n",
		shellSingleQuote(self), keys.EnvAskpassMode,
	)
}

// doctor reports the ssh-agent situation: which agents are running, which one is
// ours, whether each answers, and whether this shell's SSH_AUTH_SOCK is wired to
// a healthy agent. Plain `doctor` inspects only and changes nothing. With --fix
// it then applies the same self-heal the login path runs (reap dead agents,
// start on the fixed socket, or adopt a healthy foreign one) and re-reports.
func doctor(stdout, stderr io.Writer, args []string) int {
	fix := false
	for _, a := range args {
		if a == "--fix" {
			fix = true
			continue
		}
		_, _ = fmt.Fprintf(stderr, "sshakku: doctor: unknown argument %q\n", a)
		return 2
	}

	env := paths.FromOS()
	layout := paths.Resolve(env, paths.ProbeDir).WithSocketToken(paths.SocketToken())

	diagnose.Format(stdout, gatherReport(env, layout))
	if !fix {
		return 0
	}

	// --fix heals agent state, but a child process cannot rewrite this shell's
	// SSH_AUTH_SOCK, so the current shell may still need a new login or an export.
	_, _ = io.WriteString(stdout, "\n── applying self-heal ──\n")
	if err := paths.Ensure(layout); err != nil {
		_, _ = fmt.Fprintf(stderr, "sshakku: %v\n", err)
		return 1
	}
	paths.CleanupLegacyAgentDir(env.Home)
	liveSock, code := runEnsure(stderr, env, layout)
	if code != 0 {
		return code
	}

	_, _ = io.WriteString(stdout, "\nafter:\n\n")
	after := gatherReport(env, layout)
	diagnose.Format(stdout, after)
	if after.EnvSock != liveSock {
		_, _ = fmt.Fprintf(stdout,
			"\nthis shell still points elsewhere; open a new shell or run:\n  export SSH_AUTH_SOCK=%s\n",
			shellSingleQuote(liveSock))
	}
	return 0
}

// gatherReport builds the diagnostic report for the resolved layout, reading the
// real procfs, sockets, and process tree. Both the read-only and --fix paths use
// it so they present the situation identically.
func gatherReport(env paths.Env, layout paths.Layout) diagnose.Report {
	return diagnose.Gather(diagnose.Inputs{
		FixedSock: layout.AgentSock,
		LegacyDir: filepath.Join(env.Home, ".ssh", "agent"),
		StatePath: filepath.Join(filepath.Dir(layout.AgentSock), "agent.state"),
		EnvSock:   os.Getenv("SSH_AUTH_SOCK"),
		LogFile:   layout.LogFile,
		OurUID:    env.UID,
	}, agent.Inspector{}, agent.SocketProber{}, diagnose.ProcfsAncestry{})
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
