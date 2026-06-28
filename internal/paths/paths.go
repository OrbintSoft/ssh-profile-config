// Package paths computes and creates sshepherd's per-user runtime layout: config
// and the session log under the config dir, the agent socket in the per-user
// tmpfs — always outside ~/.ssh, which is OpenSSH's domain.
package paths

import (
	"path/filepath"
	"strconv"
)

const app = "sshepherd"

// Env holds the environment inputs to the path computation, so Resolve stays a
// pure function that is easy to test.
type Env struct {
	Home       string // $HOME
	ConfigHome string // $XDG_CONFIG_HOME (may be empty)
	RuntimeDir string // $XDG_RUNTIME_DIR (may be empty)
	CacheHome  string // $XDG_CACHE_HOME (may be empty)
	UID        int
}

// Layout is the set of resolved paths.
type Layout struct {
	ConfigDir  string
	RuntimeDir string
	SocketDir  string // RuntimeDir; a per-login token component is added later
	AgentSock  string
	AgentLock  string
	LogFile    string
}

// Resolve computes the layout from env. probe reports whether a directory is
// usable; requireOwner additionally asks that it be owned by the current user —
// used for the guessed /run/user/$UID path, which we did not get from the
// environment and therefore must not trust blindly.
func Resolve(env Env, probe func(path string, requireOwner bool) bool) Layout {
	configHome := env.ConfigHome
	if configHome == "" {
		configHome = filepath.Join(env.Home, ".config")
	}
	configDir := filepath.Join(configHome, app)

	runtimeDir := resolveRuntimeDir(env, probe)
	socketDir := runtimeDir // a per-login token is inserted here in a later step

	return Layout{
		ConfigDir:  configDir,
		RuntimeDir: runtimeDir,
		SocketDir:  socketDir,
		AgentSock:  filepath.Join(socketDir, "agent.sock"),
		AgentLock:  filepath.Join(socketDir, ".start.lock"),
		LogFile:    filepath.Join(configDir, "sessions.log"),
	}
}

// resolveRuntimeDir picks the per-user tmpfs base, independent of the desktop or
// display server: XDG_RUNTIME_DIR, then its canonical /run/user/$UID (only if we
// own it), then a private dir under $HOME when no logind tmpfs exists.
func resolveRuntimeDir(env Env, probe func(string, bool) bool) string {
	if env.RuntimeDir != "" && probe(env.RuntimeDir, false) {
		return filepath.Join(env.RuntimeDir, app)
	}
	runUser := filepath.Join("/run/user", strconv.Itoa(env.UID))
	if probe(runUser, true) {
		return filepath.Join(runUser, app)
	}
	cacheHome := env.CacheHome
	if cacheHome == "" {
		cacheHome = filepath.Join(env.Home, ".cache")
	}
	return filepath.Join(cacheHome, app)
}
