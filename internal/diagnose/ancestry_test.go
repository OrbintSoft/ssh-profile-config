package diagnose

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/OrbintSoft/sshakku/internal/agent"
)

// procParent is one entry in a fake process tree.
type procParent struct {
	ppid int
	name string
}

// fakeAncestry is a fixed pid → parent map standing in for /proc.
type fakeAncestry map[int]procParent

func (f fakeAncestry) Parent(pid int) (int, string, bool) {
	e, ok := f[pid]
	if !ok {
		return 0, "", false
	}
	return e.ppid, e.name, true
}

func TestParseStat(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantComm string
		wantPPID int
		wantOK   bool
	}{
		{"simple", "3358 (ssh-agent) S 3300 3358 3300 0 -1", "ssh-agent", 3300, true},
		{"space in comm", "42 (sd pam) S 1 42", "sd pam", 1, true},
		{"parens in comm", "7 ((sd-pam)) S 1 7", "(sd-pam)", 1, true},
		{"init", "1 (systemd) S 0 1 1", "systemd", 0, true},
		{"no parens", "garbage", "", 0, false},
		{"truncated fields", "9 (x) S", "x", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			comm, ppid, ok := parseStat([]byte(c.line))
			if ok != c.wantOK || comm != c.wantComm || (ok && ppid != c.wantPPID) {
				t.Errorf("parseStat(%q) = (%q,%d,%v), want (%q,%d,%v)",
					c.line, comm, ppid, ok, c.wantComm, c.wantPPID, c.wantOK)
			}
		})
	}
}

func TestAncestry(t *testing.T) {
	tree := fakeAncestry{
		100: {ppid: 50, name: "ssh-agent"},
		50:  {ppid: 1, name: "bash"},
		1:   {ppid: 0, name: "systemd"},
	}
	got := ancestry(100, tree)
	want := []ProcInfo{{100, "ssh-agent"}, {50, "bash"}, {1, "systemd"}}
	if len(got) != len(want) {
		t.Fatalf("ancestry = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chain[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestAncestryNilSource(t *testing.T) {
	if got := ancestry(100, nil); got != nil {
		t.Errorf("ancestry(nil source) = %v, want nil", got)
	}
}

func TestAncestryMissingParent(t *testing.T) {
	// A parent absent from the tree stops the walk without error.
	got := ancestry(100, fakeAncestry{100: {ppid: 50, name: "ssh-agent"}})
	if len(got) != 1 || got[0].Name != "ssh-agent" {
		t.Errorf("chain = %v, want just the agent", got)
	}
}

func TestAncestryCycle(t *testing.T) {
	tree := fakeAncestry{
		100: {ppid: 50, name: "a"},
		50:  {ppid: 100, name: "b"}, // points back → cycle
	}
	if got := ancestry(100, tree); len(got) != 2 {
		t.Errorf("cycle: chain = %v, want 2 entries then stop", got)
	}
}

func TestAncestryDepthCap(t *testing.T) {
	tree := fakeAncestry{}
	for i := 1; i <= 100; i++ {
		tree[i] = procParent{ppid: i + 1, name: "p" + strconv.Itoa(i)}
	}
	if got := ancestry(1, tree); len(got) != maxAncestry {
		t.Errorf("depth cap: chain len = %d, want %d", len(got), maxAncestry)
	}
}

func TestStartedBy(t *testing.T) {
	cases := []struct {
		name  string
		chain []ProcInfo
		want  string
		ok    bool
	}{
		{"known launcher deeper", []ProcInfo{{9, "ssh-agent"}, {8, "dbus-daemon"}, {1, "systemd"}}, "systemd (user or system manager)", true},
		{"daemonized to init", []ProcInfo{{9, "ssh-agent"}, {1, "init"}}, "an unknown launcher (daemonized, reparented to init)", true},
		{"immediate parent fallback", []ProcInfo{{9, "ssh-agent"}, {8, "weirdlauncher"}}, "weirdlauncher", true},
		{"too shallow", []ProcInfo{{9, "ssh-agent"}}, "", false},
		{"empty", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := startedBy(c.chain)
			if ok != c.ok || got != c.want {
				t.Errorf("startedBy(%v) = (%q,%v), want (%q,%v)", c.chain, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestLauncherLabel(t *testing.T) {
	if _, ok := launcherLabel("nope"); ok {
		t.Error("launcherLabel(nope) reported known")
	}
	for _, comm := range []string{"systemd", "plasmashell", "sshd", "bash", "sddm-helper", "gnome-keyring-d"} {
		if _, ok := launcherLabel(comm); !ok {
			t.Errorf("launcherLabel(%q) not recognised", comm)
		}
	}
	if got, _ := launcherLabel("zsh"); !strings.Contains(got, "zsh") {
		t.Errorf("login-shell label = %q, want it to name zsh", got)
	}
}

func TestChainString(t *testing.T) {
	got := chainString([]ProcInfo{{100, "ssh-agent"}, {1, "systemd"}})
	want := "ssh-agent(100) ← systemd(1)"
	if got != want {
		t.Errorf("chainString = %q, want %q", got, want)
	}
}

func TestProcfsAncestryParent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "77")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte("77 (ssh-agent) S 42 77 42"), 0o644); err != nil {
		t.Fatal(err)
	}
	ppid, name, ok := ProcfsAncestry{Root: root}.Parent(77)
	if !ok || ppid != 42 || name != "ssh-agent" {
		t.Errorf("Parent(77) = (%d,%q,%v), want (42,ssh-agent,true)", ppid, name, ok)
	}
	if _, _, ok := (ProcfsAncestry{Root: root}).Parent(999); ok {
		t.Error("Parent(999) reported ok for a missing process")
	}
}

func TestGatherForeignAttribution(t *testing.T) {
	const foreign = "/tmp/foreign.sock"
	src := fakeSource{procs: []agent.AgentProc{
		{PID: 200, UID: 1000, Socket: foreign},
	}}
	prober := fakeProber{up: map[string]bool{foreign: true}}
	anc := fakeAncestry{
		200: {ppid: 8, name: "ssh-agent"},
		8:   {ppid: 1, name: "gnome-keyring-d"},
		1:   {ppid: 0, name: "systemd"},
	}
	r := Gather(Inputs{FixedSock: fixed, LegacyDir: legacy, EnvSock: fixed, OurUID: 1000}, src, prober, anc)

	if len(r.Agents) != 1 || len(r.Agents[0].Ancestry) != 3 {
		t.Fatalf("ancestry not populated: %+v", r.Agents)
	}
	if !hasFinding(r, "started by gnome-keyring-daemon") {
		t.Errorf("findings = %v, want a foreign-attribution finding", r.Findings)
	}
}
