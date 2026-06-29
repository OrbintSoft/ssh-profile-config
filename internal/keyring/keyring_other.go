//go:build !linux

package keyring

import (
	"errors"
	"time"
)

// ErrUnavailable signals that no kernel keyring exists on this platform, so the
// caller must degrade (e.g. skip the keyring handoff).
var ErrUnavailable = errors.New("kernel keyring unavailable on this platform")

// Add reports ErrUnavailable.
func Add(string, []byte) (Serial, error) { return 0, ErrUnavailable }

// Search reports no key.
func Search(string) (Serial, bool) { return 0, false }

// Read reports ErrUnavailable.
func Read(Serial) ([]byte, error) { return nil, ErrUnavailable }

// SetTimeout reports ErrUnavailable.
func SetTimeout(Serial, time.Duration) error { return ErrUnavailable }

// Unlink reports ErrUnavailable.
func Unlink(Serial) error { return ErrUnavailable }
