//go:build unix

package agent

import "syscall"

// SysSignaler terminates a process with SIGTERM via the kernel.
type SysSignaler struct{}

// Terminate sends SIGTERM to pid.
func (SysSignaler) Terminate(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

var _ Signaler = SysSignaler{}
