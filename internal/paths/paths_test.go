package paths

import (
	"path/filepath"
	"testing"
)

func TestResolveRuntimeDir(t *testing.T) {
	const home = "/home/u"
	tests := []struct {
		name     string
		env      Env
		probe    func(string, bool) bool
		wantBase string
	}{
		{
			name:     "XDG_RUNTIME_DIR present",
			env:      Env{Home: home, RuntimeDir: "/run/user/1000", UID: 1000},
			probe:    func(p string, _ bool) bool { return p == "/run/user/1000" },
			wantBase: "/run/user/1000/sshepherd",
		},
		{
			name:     "fallback to /run/user/UID when owned",
			env:      Env{Home: home, UID: 1000},
			probe:    func(p string, owner bool) bool { return p == "/run/user/1000" && owner },
			wantBase: "/run/user/1000/sshepherd",
		},
		{
			name:     "/run/user ignored when not owned by us",
			env:      Env{Home: home, UID: 1000},
			probe:    func(p string, owner bool) bool { return p == "/run/user/1000" && !owner },
			wantBase: filepath.Join(home, ".cache", "sshepherd"),
		},
		{
			name:     "cache fallback when no tmpfs",
			env:      Env{Home: home, UID: 1000},
			probe:    func(string, bool) bool { return false },
			wantBase: filepath.Join(home, ".cache", "sshepherd"),
		},
		{
			name:     "XDG_CACHE_HOME honoured in cache fallback",
			env:      Env{Home: home, CacheHome: "/cache", UID: 1000},
			probe:    func(string, bool) bool { return false },
			wantBase: "/cache/sshepherd",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.env, tc.probe)
			if got.RuntimeDir != tc.wantBase {
				t.Errorf("RuntimeDir = %q, want %q", got.RuntimeDir, tc.wantBase)
			}
			if want := filepath.Join(tc.wantBase, "agent.sock"); got.AgentSock != want {
				t.Errorf("AgentSock = %q, want %q", got.AgentSock, want)
			}
			if want := filepath.Join(tc.wantBase, ".start.lock"); got.AgentLock != want {
				t.Errorf("AgentLock = %q, want %q", got.AgentLock, want)
			}
		})
	}
}

func TestResolveConfigDir(t *testing.T) {
	noProbe := func(string, bool) bool { return false }

	got := Resolve(Env{Home: "/home/u", UID: 1}, noProbe)
	if want := "/home/u/.config/sshepherd"; got.ConfigDir != want {
		t.Errorf("ConfigDir = %q, want %q", got.ConfigDir, want)
	}
	if want := "/home/u/.config/sshepherd/sessions.log"; got.LogFile != want {
		t.Errorf("LogFile = %q, want %q", got.LogFile, want)
	}

	got = Resolve(Env{Home: "/home/u", ConfigHome: "/cfg", UID: 1}, noProbe)
	if want := "/cfg/sshepherd"; got.ConfigDir != want {
		t.Errorf("ConfigDir with XDG_CONFIG_HOME = %q, want %q", got.ConfigDir, want)
	}
}
