//go:build unix

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCreatesLayout(t *testing.T) {
	root := t.TempDir()
	runtime := filepath.Join(root, "run", "sshepherd")
	config := filepath.Join(root, "cfg", "sshepherd")
	l := Layout{
		ConfigDir:  config,
		RuntimeDir: runtime,
		SocketDir:  runtime,
		AgentSock:  filepath.Join(runtime, "agent.sock"),
		AgentLock:  filepath.Join(runtime, ".start.lock"),
		LogFile:    filepath.Join(config, "sessions.log"),
	}
	if err := Ensure(l); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, dir := range []string{config, runtime} {
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if perm := fi.Mode().Perm(); perm != 0o700 {
			t.Errorf("%s perm = %o, want 700", dir, perm)
		}
	}
	fi, err := os.Stat(l.LogFile)
	if err != nil {
		t.Fatalf("stat log: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("log perm = %o, want 600", perm)
	}
}

func TestEnsureRejectsSymlinkDir(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "cfg")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	l := Layout{ConfigDir: link, RuntimeDir: filepath.Join(root, "run"), SocketDir: filepath.Join(root, "run")}
	if err := Ensure(l); err == nil {
		t.Error("Ensure accepted a symlinked leaf directory, want error")
	}
}

func TestCleanupLegacyAgentDir(t *testing.T) {
	home := t.TempDir()
	agent := filepath.Join(home, ".ssh", "agent")
	if err := os.MkdirAll(agent, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"ssh-agent.sock", ".start.lock"} {
		if err := os.WriteFile(filepath.Join(agent, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	CleanupLegacyAgentDir(home)
	if _, err := os.Stat(agent); !os.IsNotExist(err) {
		t.Errorf("legacy agent dir still present: %v", err)
	}
}

func TestCleanupLegacyAgentDirLeavesForeignFiles(t *testing.T) {
	home := t.TempDir()
	agent := filepath.Join(home, ".ssh", "agent")
	if err := os.MkdirAll(agent, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(agent, "keep-me")
	if err := os.WriteFile(foreign, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	CleanupLegacyAgentDir(home)
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("foreign file was removed: %v", err)
	}
}
