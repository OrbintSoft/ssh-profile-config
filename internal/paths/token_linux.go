//go:build linux

package paths

import (
	"crypto/rand"
	"encoding/hex"

	"golang.org/x/sys/unix"
)

const (
	tokenKeyType     = "user"
	tokenDescription = app + "-socket-token"
	tokenByteLen     = 16
)

// SocketToken returns the per-login token shared via the @u user keyring,
// creating it on first use. Every shell of a login converges on a single value:
// the keyring is keyed by (type, description), so a racing creator only updates
// the same key, and we read the canonical payload back. It returns "" (no error)
// when the keyring is unavailable, so the caller degrades to a tokenless path.
func SocketToken() string {
	if tok := readUserKey(tokenDescription); tok != "" {
		return tok
	}
	b := make([]byte, tokenByteLen)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	token := hex.EncodeToString(b)
	if _, err := unix.AddKey(tokenKeyType, tokenDescription, []byte(token), unix.KEY_SPEC_USER_KEYRING); err != nil {
		return ""
	}
	// Read back so all racing creators converge on whichever payload won.
	if back := readUserKey(tokenDescription); back != "" {
		return back
	}
	return token
}

// readUserKey returns the payload of a "user" key in the @u keyring, or "" if it
// is absent or unreadable.
func readUserKey(description string) string {
	serial, err := unix.KeyctlSearch(unix.KEY_SPEC_USER_KEYRING, tokenKeyType, description, 0)
	if err != nil {
		return ""
	}
	// A nil buffer sizes the payload; the second read fills it.
	size, err := unix.KeyctlBuffer(unix.KEYCTL_READ, serial, nil, 0)
	if err != nil || size <= 0 {
		return ""
	}
	buf := make([]byte, size)
	n, err := unix.KeyctlBuffer(unix.KEYCTL_READ, serial, buf, 0)
	if err != nil || n <= 0 {
		return ""
	}
	if n > len(buf) {
		n = len(buf)
	}
	return string(buf[:n])
}
