//go:build linux

package keyring

import (
	"time"

	"golang.org/x/sys/unix"
)

// keyType is the only kernel key class sshakku uses: an opaque "user" payload.
const keyType = "user"

// Add stores payload under description in the @u user keyring and returns its
// serial. A later Add with the same description updates that key rather than
// creating a second one, so concurrent callers converge on one entry.
func Add(description string, payload []byte) (Serial, error) {
	id, err := unix.AddKey(keyType, description, payload, unix.KEY_SPEC_USER_KEYRING)
	if err != nil {
		return 0, err
	}
	return Serial(id), nil
}

// Search returns the serial of the @u "user" key with description and whether it
// exists; a missing key is reported as ok=false, not an error.
func Search(description string) (Serial, bool) {
	id, err := unix.KeyctlSearch(unix.KEY_SPEC_USER_KEYRING, keyType, description, 0)
	if err != nil {
		return 0, false
	}
	return Serial(id), true
}

// Read returns the payload of the key with serial s. The first call sizes the
// payload (nil buffer); the second fills it.
func Read(s Serial) ([]byte, error) {
	size, err := unix.KeyctlBuffer(unix.KEYCTL_READ, int(s), nil, 0)
	if err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	n, err := unix.KeyctlBuffer(unix.KEYCTL_READ, int(s), buf, 0)
	if err != nil {
		return nil, err
	}
	if n > len(buf) {
		n = len(buf)
	}
	return buf[:n], nil
}

// SetTimeout schedules the key with serial s to expire after d (rounded up to
// whole seconds, minimum one), so a secret that is never read still cannot live
// indefinitely.
func SetTimeout(s Serial, d time.Duration) error {
	secs := int((d + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	_, err := unix.KeyctlInt(unix.KEYCTL_SET_TIMEOUT, int(s), secs, 0, 0)
	return err
}

// Unlink removes the key with serial s from the @u user keyring.
func Unlink(s Serial) error {
	_, err := unix.KeyctlInt(unix.KEYCTL_UNLINK, int(s), unix.KEY_SPEC_USER_KEYRING, 0, 0)
	return err
}
