// Package giveup records, per key, that sshakku abandoned loading it after the
// bounded retries, so later shells skip the key instead of re-prompting on every
// terminal. Each record is a small file under the per-login runtime directory
// (wiped on logout/reboot) holding only a timestamp; it expires after a TTL so
// fixing the vault mid-session recovers without a relogin. Records never hold any
// secret — only the key name (as the filename) and the time it was abandoned.
package giveup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	dirPerm  = 0o700
	filePerm = 0o600
)

// Store records give-up state as one file per key under Dir.
type Store struct {
	// Dir holds the per-key sentinels; it is created on the first Record. An
	// empty Dir disables the store: Record and Clear are no-ops and GivenUp is
	// always false.
	Dir string
	// TTL bounds how long a record counts as "given up"; once it elapses the key
	// is retried. A zero or negative TTL means a record never expires (it then
	// clears only on a successful add or when the runtime dir is wiped).
	TTL time.Duration
	// Now is the clock, overridable in tests; nil uses time.Now.
	Now func() time.Time
}

// GivenUp reports whether the key is currently in the give-up state. An expired
// or malformed record is removed and reported as not-given-up so the key retries.
func (s Store) GivenUp(key string) bool {
	p, ok := s.path(key)
	if !ok {
		return false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	if s.TTL <= 0 {
		return true
	}
	stamp, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		_ = os.Remove(p)
		return false
	}
	if s.now().Sub(stamp) >= s.TTL {
		_ = os.Remove(p)
		return false
	}
	return true
}

// Record marks the key as given up, stamping it with the current time.
func (s Store) Record(key string) error {
	p, ok := s.path(key)
	if !ok {
		return nil
	}
	if err := os.MkdirAll(s.Dir, dirPerm); err != nil {
		return err
	}
	stamp := s.now().UTC().Format(time.RFC3339)
	return os.WriteFile(p, []byte(stamp+"\n"), filePerm)
}

// Clear removes any give-up record for the key; a missing record is not an error.
func (s Store) Clear(key string) error {
	p, ok := s.path(key)
	if !ok {
		return nil
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// path maps a key to its sentinel file, taking the base name so a path cannot
// escape Dir. It reports false when the store is disabled or the name is unusable.
func (s Store) path(key string) (string, bool) {
	if s.Dir == "" {
		return "", false
	}
	name := filepath.Base(key)
	if name == "." || name == ".." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
		return "", false
	}
	return filepath.Join(s.Dir, name), true
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
