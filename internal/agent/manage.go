package agent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Runner starts a fresh ssh-agent bound to socket and returns its pid. It is an
// interface so tests can stand in for the real `ssh-agent` process.
type Runner interface {
	Start(socket string) (pid int, err error)
}

// Signaler delivers a termination signal to a pid. It abstracts the kernel kill
// so reaping can be tested without real processes.
type Signaler interface {
	Terminate(pid int) error
}

// Manager owns the ssh-agent lifecycle: start one on the fixed socket, and reap
// dead agents and their stale sockets. It never reimplements the agent.
type Manager struct {
	Prober    Prober
	Inspector Inspector
	Runner    Runner
	Signaler  Signaler
}

// State is what we persist about the agent we started, so a later run can
// recognise it as ours by pid even if the socket path has since rotated.
type State struct {
	PID    int
	Socket string
}

// Start clears a stale socket already at path, launches a new ssh-agent there,
// and records pid+socket in the state file. A healthy socket is never disturbed:
// callers decide to start only when no agent of ours is already serving it.
func (m Manager) Start(socket, statePath string) (int, error) {
	if !m.Prober.Reachable(socket) {
		_ = removeSocket(socket) // clear a stale socket so the bind can succeed
	}
	pid, err := m.Runner.Start(socket)
	if err != nil {
		return 0, err
	}
	if err := WriteState(statePath, State{PID: pid, Socket: socket}); err != nil {
		// The agent is up; failing to record it is non-fatal but worth surfacing.
		return pid, fmt.Errorf("agent started (pid %d) but recording state failed: %w", pid, err)
	}
	return pid, nil
}

// ReapResult records what a Reap pass cleaned up, for logging and reporting.
type ReapResult struct {
	Terminated     []int    // pids we signalled
	RemovedSockets []string // stale socket files we unlinked
}

// Reap terminates dead, same-user ssh-agent processes and unlinks their stale
// sockets. A healthy agent is never touched, regardless of who started it, and
// another user's process is never signalled (least privilege). Agents started
// without an `-a` socket are left alone — we cannot probe or unlink them here.
func (m Manager) Reap(ourUID int) (ReapResult, error) {
	procs, err := m.Inspector.Agents()
	if err != nil {
		return ReapResult{}, err
	}
	var res ReapResult
	for _, p := range procs {
		if p.Socket == "" || m.Prober.Reachable(p.Socket) {
			continue // unknown socket, or healthy — never reap
		}
		if p.UID != ourUID {
			continue // not ours to signal
		}
		if err := m.Signaler.Terminate(p.PID); err == nil {
			res.Terminated = append(res.Terminated, p.PID)
		}
		if removeSocket(p.Socket) {
			res.RemovedSockets = append(res.RemovedSockets, p.Socket)
		}
	}
	return res, nil
}

// WriteState records the agent we started, 0600, in a small key=value file.
func WriteState(path string, s State) error {
	data := fmt.Sprintf("pid=%d\nsock=%s\n", s.PID, s.Socket)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		return fmt.Errorf("write state %s: %w", path, err)
	}
	return nil
}

// ReadState parses a state file written by WriteState. A missing file is an
// error; a present but partial file yields whatever fields were readable.
func ReadState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var s State
	for _, line := range strings.Split(string(b), "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "pid":
			s.PID, _ = strconv.Atoi(val)
		case "sock":
			s.Socket = val
		}
	}
	return s, nil
}

// removeSocket unlinks path only when it is a socket, tolerating a missing file,
// so a mis-parsed path can never delete a regular file. It reports removal.
func removeSocket(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSocket == 0 {
		return false
	}
	return os.Remove(path) == nil
}

// ExecRunner starts a real ssh-agent via os/exec.
type ExecRunner struct {
	// Path overrides the ssh-agent binary; empty resolves "ssh-agent" on PATH.
	Path string
}

// Start runs `ssh-agent -a <socket>`, which daemonizes and prints its
// environment, and returns the SSH_AGENT_PID it announces.
func (r ExecRunner) Start(socket string) (int, error) {
	bin := r.Path
	if bin == "" {
		bin = "ssh-agent"
	}
	out, err := exec.Command(bin, "-a", socket).Output()
	if err != nil {
		return 0, fmt.Errorf("start ssh-agent: %w", err)
	}
	return parseAgentPID(out)
}

// parseAgentPID extracts the pid from ssh-agent's `SSH_AGENT_PID=<pid>;` line.
func parseAgentPID(out []byte) (int, error) {
	const key = "SSH_AGENT_PID="
	s := string(out)
	i := strings.Index(s, key)
	if i < 0 {
		return 0, errors.New("ssh-agent output had no SSH_AGENT_PID")
	}
	s = s[i+len(key):]
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, errors.New("ssh-agent output had a malformed SSH_AGENT_PID")
	}
	return strconv.Atoi(s[:end])
}

var _ Runner = ExecRunner{}
