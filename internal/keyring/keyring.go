// Package keyring wraps the Linux kernel keyring (@u user keyring) operations
// sshakku needs: storing a short-lived secret, reading it back by serial, setting
// an expiry, and unlinking it. The payload travels in the syscall buffer, never
// in argv, so secrets handed through it cannot leak via `ps` or
// /proc/<pid>/cmdline. On platforms without a kernel keyring the operations
// degrade to ErrUnavailable.
package keyring

// Serial identifies a key within the kernel keyring.
type Serial int32
