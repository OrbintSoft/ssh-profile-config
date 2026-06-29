//go:build unix

package agent

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// FlockLocker serialises the mutate path of EnsureAgent with an exclusive advisory
// lock (flock) on a lock file. The caller probes the fixed socket before locking,
// so a healthy login is never serialised; only the start/reap/adopt path contends.
type FlockLocker struct {
	// Wait bounds how long to block for the lock. On expiry the caller proceeds
	// without the lock rather than hang the login on a stuck holder; a brief race
	// is preferable to a stalled session.
	Wait time.Duration
	// Poll is the retry interval while waiting; zero selects a small default.
	Poll time.Duration
}

// Lock takes the exclusive lock on path and returns a function that releases it.
// It always returns a usable release function on success, including the timed-out
// case where the lock was not actually held (the release then just closes the fd).
func (l FlockLocker) Lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	closeOnly := func() { _ = f.Close() }

	poll := l.Poll
	if poll <= 0 {
		poll = 50 * time.Millisecond
	}
	deadline := time.Now().Add(l.Wait)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			closeOnly()
			return nil, fmt.Errorf("flock %s: %w", path, err)
		}
		if !time.Now().Before(deadline) {
			// Bounded wait elapsed: proceed without the lock rather than hang login.
			return closeOnly, nil
		}
		time.Sleep(poll)
	}
}

var _ Locker = FlockLocker{}
