package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLogger records the level-tagged lines EnsureAgent emits.
type fakeLogger struct{ lines []string }

func (f *fakeLogger) Log(level, message string) error {
	f.lines = append(f.lines, level+" "+message)
	return nil
}

func (f *fakeLogger) hasLevel(level string) bool {
	for _, l := range f.lines {
		if strings.HasPrefix(l, level+" ") {
			return true
		}
	}
	return false
}

func TestEnsureAgentHealthy(t *testing.T) {
	dir := t.TempDir()
	fixed := filepath.Join(dir, "agent.sock")
	runner := &recordRunner{pid: 1}

	m := Manager{Prober: mapProber{fixed: true}, Runner: runner, Signaler: &recordSignaler{}}
	res, err := m.EnsureAgent(EnsureConfig{FixedSock: fixed, StatePath: filepath.Join(dir, "st"), OurUID: 1000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Situation != SituationHealthy || res.LiveSock != fixed {
		t.Fatalf("got %+v, want healthy on %s", res, fixed)
	}
	if runner.started != "" {
		t.Errorf("healthy path must not start an agent, started %q", runner.started)
	}
}

func TestEnsureAgentClean(t *testing.T) {
	dir := t.TempDir()
	fixed := filepath.Join(dir, "agent.sock")
	state := filepath.Join(dir, "agent.state")
	runner := &recordRunner{pid: 4242}

	m := Manager{
		Prober:    mapProber{}, // nothing reachable
		Inspector: Inspector{ProcRoot: t.TempDir()},
		Runner:    runner,
		Signaler:  &recordSignaler{},
	}
	res, err := m.EnsureAgent(EnsureConfig{FixedSock: fixed, StatePath: state, OurUID: 1000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Situation != SituationClean || res.LiveSock != fixed || res.Started != 4242 {
		t.Fatalf("got %+v, want clean start pid 4242", res)
	}
	if runner.started != fixed {
		t.Errorf("started %q, want %q", runner.started, fixed)
	}
	if st, err := ReadState(state); err != nil || st.PID != 4242 {
		t.Errorf("state = %+v, err = %v", st, err)
	}
}

func TestEnsureAgentZombie(t *testing.T) {
	dir := t.TempDir()
	fixed := filepath.Join(dir, "agent.sock")
	state := filepath.Join(dir, "agent.state")
	proc := t.TempDir()

	makeSocketFile(t, fixed)                                         // a real stale socket at our path
	fakeProc(t, proc, 200, []string{"ssh-agent", "-a", fixed}, 1000) // dead agent of ours

	runner := &recordRunner{pid: 7000}
	sig := &recordSignaler{}
	m := Manager{Prober: mapProber{}, Inspector: Inspector{ProcRoot: proc}, Runner: runner, Signaler: sig}
	log := &fakeLogger{}

	res, err := m.EnsureAgent(EnsureConfig{FixedSock: fixed, StatePath: state, OurUID: 1000}, log)
	if err != nil {
		t.Fatal(err)
	}
	if res.Situation != SituationZombie {
		t.Fatalf("situation = %v, want zombie", res.Situation)
	}
	if !contains(sig.killed, 200) {
		t.Errorf("killed %v, want dead-ours 200 reaped", sig.killed)
	}
	if runner.started != fixed {
		t.Errorf("should restart ours on %q, started %q", fixed, runner.started)
	}
	if !log.hasLevel("INFO") {
		t.Error("expected an INFO line for the reap and restart")
	}
}

func TestEnsureAgentForeign(t *testing.T) {
	dir := t.TempDir()
	fixed := filepath.Join(dir, "agent.sock")
	proc := t.TempDir()
	foreignSock := filepath.Join(dir, "foreign.sock")

	fakeProc(t, proc, 300, []string{"ssh-agent", "-a", foreignSock}, 1000)

	runner := &recordRunner{pid: 1}
	m := Manager{
		Prober:    mapProber{foreignSock: true}, // fixed silent, foreign healthy
		Inspector: Inspector{ProcRoot: proc},
		Runner:    runner,
		Signaler:  &recordSignaler{},
	}
	log := &fakeLogger{}

	cfg := EnsureConfig{FixedSock: fixed, LegacyDir: "/nope", StatePath: filepath.Join(dir, "st"), OurUID: 1000}
	res, err := m.EnsureAgent(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	if res.Situation != SituationForeign {
		t.Fatalf("situation = %v, want foreign", res.Situation)
	}
	if res.Adopted == nil || res.Adopted.PID != 300 {
		t.Fatalf("adopted = %+v, want pid 300", res.Adopted)
	}
	if res.Anomaly == "" || !log.hasLevel("WARN") {
		t.Error("foreign adoption must report an anomaly at WARN")
	}
	if runner.started != "" {
		t.Error("adoption must not start a new agent")
	}
	if res.LiveSock != fixed {
		t.Errorf("live sock = %q, want fixed %q", res.LiveSock, fixed)
	}
	if target, err := os.Readlink(fixed); err != nil || target != foreignSock {
		t.Errorf("readlink(fixed) = %q, %v; want %q", target, err, foreignSock)
	}
}

func TestEnsureAgentDisasterMultiple(t *testing.T) {
	dir := t.TempDir()
	fixed := filepath.Join(dir, "agent.sock")
	proc := t.TempDir()
	f1 := filepath.Join(dir, "f1.sock")
	f2 := filepath.Join(dir, "f2.sock")

	fakeProc(t, proc, 400, []string{"ssh-agent", "-a", f2}, 1000)
	fakeProc(t, proc, 300, []string{"ssh-agent", "-a", f1}, 1000)

	m := Manager{
		Prober:    mapProber{f1: true, f2: true},
		Inspector: Inspector{ProcRoot: proc},
		Runner:    &recordRunner{},
		Signaler:  &recordSignaler{},
	}
	res, err := m.EnsureAgent(EnsureConfig{FixedSock: fixed, OurUID: 1000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Situation != SituationDisaster {
		t.Fatalf("situation = %v, want disaster", res.Situation)
	}
	if res.Adopted == nil || res.Adopted.PID != 300 {
		t.Fatalf("adopted = %+v, want lowest pid 300", res.Adopted)
	}
	if !strings.Contains(res.Anomaly, "2 healthy agents") {
		t.Errorf("anomaly should note multiple agents, got %q", res.Anomaly)
	}
	if target, _ := os.Readlink(fixed); target != f1 {
		t.Errorf("readlink = %q, want %q (lowest pid's socket)", target, f1)
	}
}

func TestEnsureAgentDisasterReapAndAdopt(t *testing.T) {
	dir := t.TempDir()
	fixed := filepath.Join(dir, "agent.sock")
	proc := t.TempDir()
	foreignSock := filepath.Join(dir, "foreign.sock")

	makeSocketFile(t, fixed)                                               // stale socket of ours
	fakeProc(t, proc, 200, []string{"ssh-agent", "-a", fixed}, 1000)       // dead ours
	fakeProc(t, proc, 300, []string{"ssh-agent", "-a", foreignSock}, 1000) // healthy foreign

	sig := &recordSignaler{}
	m := Manager{
		Prober:    mapProber{foreignSock: true},
		Inspector: Inspector{ProcRoot: proc},
		Runner:    &recordRunner{},
		Signaler:  sig,
	}
	res, err := m.EnsureAgent(EnsureConfig{FixedSock: fixed, OurUID: 1000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Situation != SituationDisaster {
		t.Fatalf("situation = %v, want disaster", res.Situation)
	}
	if !contains(sig.killed, 200) {
		t.Errorf("should reap dead-ours 200, killed %v", sig.killed)
	}
	if res.Adopted == nil || res.Adopted.PID != 300 {
		t.Fatalf("adopted = %+v, want 300", res.Adopted)
	}
	if target, _ := os.Readlink(fixed); target != foreignSock {
		t.Errorf("readlink = %q, want %q", target, foreignSock)
	}
}

func TestClearStalePath(t *testing.T) {
	dir := t.TempDir()

	sock := filepath.Join(dir, "a.sock")
	makeSocketFile(t, sock)
	clearStalePath(sock)
	if _, err := os.Lstat(sock); !os.IsNotExist(err) {
		t.Errorf("socket should be cleared, lstat err = %v", err)
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink("/dangling", link); err != nil {
		t.Fatal(err)
	}
	clearStalePath(link)
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("symlink should be cleared, lstat err = %v", err)
	}

	reg := filepath.Join(dir, "regular")
	if err := os.WriteFile(reg, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	clearStalePath(reg)
	if _, err := os.Lstat(reg); err != nil {
		t.Errorf("regular file must survive clearStalePath, err = %v", err)
	}
}

func TestSituationString(t *testing.T) {
	for s, want := range map[Situation]string{
		SituationHealthy:  "healthy",
		SituationClean:    "clean",
		SituationZombie:   "zombie",
		SituationForeign:  "foreign",
		SituationDisaster: "disaster",
	} {
		if got := s.String(); got != want {
			t.Errorf("Situation(%d).String() = %q, want %q", s, got, want)
		}
	}
}
