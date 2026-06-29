package agent

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// mapProber reports reachability from a fixed map; absent paths are unreachable.
type mapProber map[string]bool

func (m mapProber) Reachable(socket string) bool { return m[socket] }

// recordRunner stands in for ssh-agent: it records the socket it was asked to
// start and returns a fixed pid.
type recordRunner struct {
	pid     int
	err     error
	started string
}

func (r *recordRunner) Start(socket string) (int, error) {
	r.started = socket
	return r.pid, r.err
}

// recordSignaler records the pids it was asked to terminate.
type recordSignaler struct {
	killed []int
}

func (s *recordSignaler) Terminate(pid int) error {
	s.killed = append(s.killed, pid)
	return nil
}

// makeSocketFile leaves a real socket inode at path without auto-unlinking it,
// so removeSocket has a genuine socket to act on.
func makeSocketFile(t *testing.T, path string) {
	t.Helper()
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	_ = l.Close()
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestManagerReap(t *testing.T) {
	root := t.TempDir()
	const ourUID = 1000

	deadOurs := filepath.Join(root, "dead-ours.sock")
	deadOther := filepath.Join(root, "dead-other.sock")
	makeSocketFile(t, deadOurs)
	makeSocketFile(t, deadOther)

	fakeProc(t, root, 100, []string{"ssh-agent", "-a", "/healthy.sock"}, ourUID) // healthy → spare
	fakeProc(t, root, 200, []string{"ssh-agent", "-a", deadOurs}, ourUID)        // dead + ours → reap
	fakeProc(t, root, 300, []string{"ssh-agent", "-a", deadOther}, 1001)         // dead, other user → spare
	fakeProc(t, root, 400, []string{"ssh-agent", "-D"}, ourUID)                  // no socket → spare

	prober := mapProber{"/healthy.sock": true} // everything else is unreachable
	sig := &recordSignaler{}
	m := Manager{Prober: prober, Inspector: Inspector{ProcRoot: root}, Signaler: sig}

	res, err := m.Reap(ourUID)
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}

	if len(sig.killed) != 1 || !contains(sig.killed, 200) {
		t.Fatalf("terminated %v, want only pid 200", sig.killed)
	}
	if len(res.RemovedSockets) != 1 || res.RemovedSockets[0] != deadOurs {
		t.Fatalf("removed %v, want only %s", res.RemovedSockets, deadOurs)
	}
	if _, err := os.Lstat(deadOurs); !os.IsNotExist(err) {
		t.Errorf("our dead socket should be gone, lstat err = %v", err)
	}
	if _, err := os.Lstat(deadOther); err != nil {
		t.Errorf("another user's socket must be left intact, lstat err = %v", err)
	}
}

func TestManagerStart(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "agent.sock")
	state := filepath.Join(dir, "agent.state")

	// A stale socket sits at the target; the prober says it is unreachable.
	makeSocketFile(t, socket)
	runner := &recordRunner{pid: 4242}
	m := Manager{Prober: mapProber{}, Runner: runner}

	pid, err := m.Start(socket, state)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if pid != 4242 || runner.started != socket {
		t.Fatalf("started pid=%d socket=%q", pid, runner.started)
	}

	got, err := ReadState(state)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.PID != 4242 || got.Socket != socket {
		t.Fatalf("state = %+v, want pid 4242 socket %q", got, socket)
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.state")
	want := State{PID: 99, Socket: "/run/user/1000/sshakku/tok/agent.sock"}
	if err := WriteState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round-trip = %+v, want %+v", got, want)
	}
	if _, err := ReadState(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("ReadState of a missing file should error")
	}
}

func TestRemoveSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "a.sock")
	makeSocketFile(t, sock)
	if !removeSocket(sock) {
		t.Fatal("removeSocket should remove a socket")
	}
	if _, err := os.Lstat(sock); !os.IsNotExist(err) {
		t.Errorf("socket should be gone, err = %v", err)
	}

	reg := filepath.Join(dir, "regular")
	if err := os.WriteFile(reg, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if removeSocket(reg) {
		t.Error("removeSocket must refuse a regular file")
	}
	if _, err := os.Lstat(reg); err != nil {
		t.Errorf("regular file must survive, err = %v", err)
	}
	if removeSocket(filepath.Join(dir, "missing")) {
		t.Error("removeSocket of a missing path should report false")
	}
}

func TestParseAgentPID(t *testing.T) {
	good := "SSH_AUTH_SOCK=/x; export SSH_AUTH_SOCK;\nSSH_AGENT_PID=12345; export SSH_AGENT_PID;\n"
	pid, err := parseAgentPID([]byte(good))
	if err != nil || pid != 12345 {
		t.Fatalf("parseAgentPID = %d, %v; want 12345", pid, err)
	}
	if _, err := parseAgentPID([]byte("no pid here")); err == nil {
		t.Error("want error when SSH_AGENT_PID is absent")
	}
	if _, err := parseAgentPID([]byte("SSH_AGENT_PID=;")); err == nil {
		t.Error("want error for a malformed SSH_AGENT_PID")
	}
}
