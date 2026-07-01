package diagnose

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OrbintSoft/sshakku/internal/agent"
)

// fakeSource returns a fixed set of agent processes and an optional error.
type fakeSource struct {
	procs []agent.AgentProc
	err   error
}

func (f fakeSource) Agents() ([]agent.AgentProc, error) { return f.procs, f.err }

// fakeProber reports a socket reachable iff it is in the up set.
type fakeProber struct{ up map[string]bool }

func (f fakeProber) Reachable(sock string) bool { return f.up[sock] }

const (
	fixed  = "/run/user/1000/sshakku/tok/agent.sock"
	legacy = "/home/u/.ssh/agent"
)

// hasFinding reports whether any finding contains sub.
func hasFinding(r Report, sub string) bool {
	for _, f := range r.Findings {
		if strings.Contains(f, sub) {
			return true
		}
	}
	return false
}

func TestGatherHealthy(t *testing.T) {
	src := fakeSource{procs: []agent.AgentProc{
		{PID: 100, UID: 1000, Socket: fixed, Args: []string{"ssh-agent", "-a", fixed}},
	}}
	prober := fakeProber{up: map[string]bool{fixed: true}}

	r := Gather(Inputs{
		FixedSock: fixed,
		LegacyDir: legacy,
		EnvSock:   fixed,
		OurUID:    1000,
	}, src, prober, nil)

	if len(r.Agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(r.Agents))
	}
	a := r.Agents[0]
	if a.Kind != agent.KindOurs || !a.Reachable {
		t.Errorf("agent: kind=%v reachable=%v, want ours/reachable", a.Kind, a.Reachable)
	}
	if !r.EnvReachable {
		t.Error("EnvReachable = false, want true")
	}
	if r.State != StateOursHealthy {
		t.Errorf("State = %v, want StateOursHealthy", r.State)
	}
	if !hasFinding(r, "no problems detected") {
		t.Errorf("findings = %v, want a clean bill", r.Findings)
	}
}

func TestGatherEnvUnset(t *testing.T) {
	src := fakeSource{procs: []agent.AgentProc{
		{PID: 100, UID: 1000, Socket: fixed},
	}}
	prober := fakeProber{up: map[string]bool{fixed: true}}

	r := Gather(Inputs{FixedSock: fixed, LegacyDir: legacy, OurUID: 1000}, src, prober, nil)
	if !hasFinding(r, "SSH_AUTH_SOCK is unset") {
		t.Errorf("findings = %v, want an unset-env finding", r.Findings)
	}
}

func TestGatherEnvNotAnswering(t *testing.T) {
	src := fakeSource{}
	prober := fakeProber{} // nothing up
	r := Gather(Inputs{FixedSock: fixed, LegacyDir: legacy, EnvSock: fixed, OurUID: 1000}, src, prober, nil)

	if r.EnvReachable {
		t.Error("EnvReachable = true, want false")
	}
	if !hasFinding(r, "not answering") {
		t.Errorf("findings = %v, want a not-answering finding", r.Findings)
	}
	if !hasFinding(r, "no ssh-agent is answering") {
		t.Errorf("findings = %v, want a no-agent finding", r.Findings)
	}
}

func TestGatherEnvMismatch(t *testing.T) {
	const other = "/tmp/other.sock"
	src := fakeSource{procs: []agent.AgentProc{
		{PID: 100, UID: 1000, Socket: other},
	}}
	prober := fakeProber{up: map[string]bool{other: true}}
	r := Gather(Inputs{FixedSock: fixed, LegacyDir: legacy, EnvSock: other, OurUID: 1000}, src, prober, nil)

	if !hasFinding(r, "not our fixed socket") {
		t.Errorf("findings = %v, want a mismatch finding", r.Findings)
	}
}

func TestGatherMultipleAndDead(t *testing.T) {
	const foreign = "/tmp/foreign.sock"
	src := fakeSource{procs: []agent.AgentProc{
		{PID: 100, UID: 1000, Socket: fixed},                      // ours, reachable
		{PID: 200, UID: 1000, Socket: foreign},                    // foreign, reachable
		{PID: 300, UID: 1000, Socket: legacy + "/ssh-agent.sock"}, // legacy, dead
	}}
	prober := fakeProber{up: map[string]bool{fixed: true, foreign: true}}
	r := Gather(Inputs{FixedSock: fixed, LegacyDir: legacy, EnvSock: fixed, OurUID: 1000}, src, prober, nil)

	if !hasFinding(r, "2 agents are answering") {
		t.Errorf("findings = %v, want a multiple-agents finding", r.Findings)
	}
	if !hasFinding(r, "1 dead ssh-agent") {
		t.Errorf("findings = %v, want a dead-agent finding", r.Findings)
	}

	kinds := map[int]agent.ProcKind{}
	for _, a := range r.Agents {
		kinds[a.PID] = a.Kind
	}
	if kinds[200] != agent.KindForeign || kinds[300] != agent.KindLegacy {
		t.Errorf("classification: 200=%v 300=%v", kinds[200], kinds[300])
	}
}

func TestGatherInspectError(t *testing.T) {
	src := fakeSource{err: errors.New("boom")}
	r := Gather(Inputs{FixedSock: fixed, LegacyDir: legacy, EnvSock: fixed, OurUID: 1000},
		src, fakeProber{up: map[string]bool{fixed: true}}, nil)
	if r.InspectErr == nil {
		t.Fatal("InspectErr = nil, want the enumeration error")
	}
	if !hasFinding(r, "could not enumerate processes") {
		t.Errorf("findings = %v, want an enumerate-error finding", r.Findings)
	}
}

func TestGatherRecordedPID(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent.state")
	if err := agent.WriteState(statePath, agent.State{PID: 4242, Socket: fixed}); err != nil {
		t.Fatal(err)
	}
	r := Gather(Inputs{FixedSock: fixed, LegacyDir: legacy, EnvSock: fixed, StatePath: statePath, OurUID: 1000},
		fakeSource{}, fakeProber{up: map[string]bool{fixed: true}}, nil)
	if r.RecordedPID != 4242 {
		t.Errorf("RecordedPID = %d, want 4242", r.RecordedPID)
	}
}

func TestTailLines(t *testing.T) {
	dir := t.TempDir()

	if got := tailLines(filepath.Join(dir, "missing.log"), 5); got != nil {
		t.Errorf("missing file: got %v, want nil", got)
	}

	empty := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(empty, []byte("\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := tailLines(empty, 5); got != nil {
		t.Errorf("empty file: got %v, want nil", got)
	}

	full := filepath.Join(dir, "full.log")
	if err := os.WriteFile(full, []byte("l1\nl2\nl3\nl4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := tailLines(full, 2)
	if len(got) != 2 || got[0] != "l3" || got[1] != "l4" {
		t.Errorf("tailLines = %v, want [l3 l4]", got)
	}
	if got := tailLines(full, 10); len(got) != 4 {
		t.Errorf("tailLines(all) = %v, want 4 lines", got)
	}
}

func TestFormat(t *testing.T) {
	r := Report{
		FixedSock:    fixed,
		EnvSock:      fixed,
		EnvReachable: true,
		OurUID:       1000,
		RecordedPID:  4242,
		State:        StateOursHealthy,
		Agents: []AgentView{
			{PID: 100, UID: 1000, Kind: agent.KindOurs, Socket: fixed, Reachable: true},
			{PID: 200, UID: 1001, Kind: agent.KindForeign, Socket: "/tmp/f.sock", Reachable: false},
			{PID: 300, UID: -1, Kind: agent.KindForeign, Socket: "", Reachable: false},
		},
		Findings: []string{"no problems detected"},
		LogTail:  []string{"2026-07-01T00:00:00Z INFO started"},
	}
	var buf bytes.Buffer
	Format(&buf, r)
	out := buf.String()

	for _, want := range []string{
		"ssh-agent diagnostics",
		"state: B —",
		"fixed socket:  " + fixed,
		"(reachable)",
		"recorded pid:  4242",
		"pid 100",
		"you",      // our own agent
		"uid 1001", // another user's agent
		"uid ?",    // unknown owner
		"reachable",
		"dead",
		"no problems detected",
		"recommendation:",
		"recent log:",
		"INFO started",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Format output missing %q\n---\n%s", want, out)
		}
	}
}
