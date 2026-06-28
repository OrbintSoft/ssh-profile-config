package agent

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Situation names the agent landscape EnsureAgent found and resolved, following
// the five-state self-healing policy. It is reported for logging and diagnostics.
type Situation int

const (
	// SituationHealthy: our agent already answered on the fixed socket.
	SituationHealthy Situation = iota
	// SituationClean: nothing was running; we started a fresh agent.
	SituationClean
	// SituationZombie: we reaped dead agents or sockets, then started ours.
	SituationZombie
	// SituationForeign: we adopted one healthy agent we did not start.
	SituationForeign
	// SituationDisaster: we recovered from an ambiguous or messy landscape.
	SituationDisaster
)

func (s Situation) String() string {
	switch s {
	case SituationHealthy:
		return "healthy"
	case SituationClean:
		return "clean"
	case SituationZombie:
		return "zombie"
	case SituationForeign:
		return "foreign"
	case SituationDisaster:
		return "disaster"
	default:
		return "unknown"
	}
}

// Logger records one level-tagged line. A nil Logger disables logging; tests pass
// a recorder, and the command wires in the session log.
type Logger interface {
	Log(level, message string) error
}

// EnsureConfig parameterises EnsureAgent without coupling the package to the path
// layout. FixedSock is the socket we pin our agent to and expose to the shell.
type EnsureConfig struct {
	FixedSock string // the socket we pin our agent to and expose to the shell
	LegacyDir string // pre-sshakku agents live under here (~/.ssh/agent)
	StatePath string // where we record the agent we start
	OurUID    int    // our real uid; gates same-user reaping
}

// EnsureResult reports what EnsureAgent observed and did.
type EnsureResult struct {
	Situation Situation
	LiveSock  string     // the healthy socket to expose to the shell
	Started   int        // pid of the agent we started, or 0
	Adopted   *AgentProc // the agent we adopted, or nil
	Reaped    ReapResult
	Anomaly   string // human-readable anomaly, set when we adopt a foreign agent
}

// EnsureAgent drives the fixed socket to a healthy ssh-agent and returns the
// socket to expose to the shell. In precedence order it attaches to our healthy
// agent; otherwise it reaps the dead, then either adopts a healthy agent it did
// not start (reporting the anomaly) or starts its own. It never reimplements the
// agent and, on success, never leaves the shell pointed at a dead socket.
func (m Manager) EnsureAgent(cfg EnsureConfig, log Logger) (EnsureResult, error) {
	logf := func(level, format string, a ...any) {
		if log != nil {
			_ = log.Log(level, fmt.Sprintf(format, a...))
		}
	}

	// Ours is already healthy on the fixed socket: attach and go, silently.
	if m.Prober.Reachable(cfg.FixedSock) {
		return EnsureResult{Situation: SituationHealthy, LiveSock: cfg.FixedSock}, nil
	}

	// The fixed socket is silent. Clear the dead agents we are allowed to, then
	// survey which healthy agents remain.
	reap, err := m.Reap(cfg.OurUID)
	if err != nil {
		return EnsureResult{}, fmt.Errorf("reap dead agents: %w", err)
	}
	reaped := len(reap.Terminated) > 0 || len(reap.RemovedSockets) > 0
	if reaped {
		logf("INFO", "reaped dead agents: pids %v, sockets %v", reap.Terminated, reap.RemovedSockets)
	}

	foreign, err := m.healthyForeign(cfg.FixedSock)
	if err != nil {
		return EnsureResult{}, err
	}

	// No other healthy agent: start our own on the fixed socket.
	if len(foreign) == 0 {
		clearStalePath(cfg.FixedSock)
		pid, err := m.Start(cfg.FixedSock, cfg.StatePath)
		if err != nil {
			return EnsureResult{}, fmt.Errorf("start agent: %w", err)
		}
		sit := SituationClean
		if reaped {
			sit = SituationZombie
		}
		logf("INFO", "started ssh-agent pid %d on %s (%s)", pid, cfg.FixedSock, sit)
		return EnsureResult{Situation: sit, LiveSock: cfg.FixedSock, Started: pid, Reaped: reap}, nil
	}

	// A healthy agent we did not start exists: adopt the lowest-pid one by
	// pointing the fixed socket at it, keep the shell on the fixed path, and
	// report the anomaly. Several candidates, or adoption after a reap, is a
	// disaster-grade landscape worth the louder report.
	adopt := foreign[0]
	if err := adoptSymlink(cfg.FixedSock, adopt.Socket); err != nil {
		return EnsureResult{}, fmt.Errorf("adopt agent pid %d: %w", adopt.PID, err)
	}
	sit := SituationForeign
	if len(foreign) > 1 || reaped {
		sit = SituationDisaster
	}
	kind := Classify(adopt, cfg.FixedSock, cfg.LegacyDir)
	anomaly := fmt.Sprintf("adopted %s ssh-agent pid %d (uid %d) on %s via the fixed socket; argv: %s",
		kind, adopt.PID, adopt.UID, adopt.Socket, strings.Join(adopt.Args, " "))
	if len(foreign) > 1 {
		anomaly += fmt.Sprintf("; %d healthy agents were present", len(foreign))
	}
	logf("WARN", "%s", anomaly)
	return EnsureResult{
		Situation: sit,
		LiveSock:  cfg.FixedSock,
		Adopted:   &adopt,
		Reaped:    reap,
		Anomaly:   anomaly,
	}, nil
}

// healthyForeign returns the live ssh-agents bound to a socket other than the
// fixed one, sorted by pid for a deterministic adoption choice.
func (m Manager) healthyForeign(fixedSock string) ([]AgentProc, error) {
	procs, err := m.Inspector.Agents()
	if err != nil {
		return nil, fmt.Errorf("inspect agents: %w", err)
	}
	var out []AgentProc
	for _, p := range procs {
		if p.Socket == "" || p.Socket == fixedSock {
			continue
		}
		if m.Prober.Reachable(p.Socket) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out, nil
}

// clearStalePath removes a leftover socket or symlink at path so a fresh agent
// can bind there. It never disturbs a regular file or a directory.
func clearStalePath(path string) {
	fi, err := os.Lstat(path)
	if err != nil {
		return
	}
	if fi.Mode()&(os.ModeSocket|os.ModeSymlink) != 0 {
		_ = os.Remove(path)
	}
}

// adoptSymlink points fixedSock at target through an atomic symlink swap, so
// clients that already follow the fixed path reach the adopted agent.
func adoptSymlink(fixedSock, target string) error {
	tmp := fixedSock + ".adopt"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("symlink -> %s: %w", target, err)
	}
	if err := os.Rename(tmp, fixedSock); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("activate %s: %w", fixedSock, err)
	}
	return nil
}
