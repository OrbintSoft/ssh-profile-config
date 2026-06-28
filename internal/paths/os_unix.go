//go:build unix

package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// FromOS reads the path inputs from the process environment.
func FromOS() Env {
	home := os.Getenv("HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	return Env{
		Home:       home,
		ConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		RuntimeDir: os.Getenv("XDG_RUNTIME_DIR"),
		CacheHome:  os.Getenv("XDG_CACHE_HOME"),
		UID:        os.Getuid(),
	}
}

// ProbeDir reports whether path is a directory. When requireOwner is set it must
// also be owned by the current user.
func ProbeDir(path string, requireOwner bool) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.IsDir() {
		return false
	}
	if requireOwner {
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || int(st.Uid) != os.Getuid() {
			return false
		}
	}
	return true
}

// Ensure creates the layout's directories (0700, leaf only) and the log file
// (0600). Intermediate parents (e.g. ~/.config) are created with the process
// umask but never re-permissioned — only our own leaf dirs are forced to 0700.
func Ensure(l Layout) error {
	for _, dir := range dedupe(l.ConfigDir, l.RuntimeDir, l.SocketDir) {
		if err := ensureDir(dir); err != nil {
			return err
		}
	}
	return ensureFile(l.LogFile, 0o600)
}

// CleanupLegacyAgentDir retires our previous location under ~/.ssh: the agent/
// dir there is what makes OpenSSH 10.x relocate its socket to a random path.
// Best-effort: it removes only our own socket/lock and leaves the dir if
// anything else remains.
func CleanupLegacyAgentDir(home string) {
	if home == "" {
		return
	}
	dir := filepath.Join(home, ".ssh", "agent")
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return
	}
	_ = os.Remove(filepath.Join(dir, "ssh-agent.sock"))
	_ = os.Remove(filepath.Join(dir, ".start.lock"))
	_ = os.Remove(dir) // rmdir; harmless if the dir is not empty
}

func ensureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	// Reject a symlink planted in our place: the leaf must be a real directory.
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func ensureFile(path string, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	_ = f.Close()
	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func dedupe(items ...string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		if !seen[it] {
			seen[it] = true
			out = append(out, it)
		}
	}
	return out
}
