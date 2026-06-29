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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/OrbintSoft/sshakku/internal/agent"
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
  askpass        print a key passphrase from the keyring (used as SSH_ASKPASS)
  help           show this help
`

func main() {
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
	case "askpass":
		return askpass(stdout)
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
