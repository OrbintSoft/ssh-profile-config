//go:build unix

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// ttyPrompter reads one line from the controlling terminal (/dev/tty), optionally
// with echo disabled, as the askpass broker's fallback. Reaching /dev/tty rather
// than stdin works even though ssh runs the askpass helper detached from stdin.
type ttyPrompter struct{}

func (ttyPrompter) Prompt(prompt string, secret bool) (string, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	if _, err := fmt.Fprint(f, prompt); err != nil {
		return "", err
	}

	if secret {
		restore, err := disableEcho(f)
		if err != nil {
			return "", err
		}
		defer restore()
	}

	line, readErr := bufio.NewReader(f).ReadString('\n')
	if secret {
		// The newline the user pressed was not echoed; emit one so the terminal
		// does not run the next output onto the prompt line.
		_, _ = fmt.Fprintln(f)
	}
	if readErr != nil && line == "" {
		return "", readErr
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// disableEcho turns off terminal echo on f, returning a function that restores
// the previous terminal state.
func disableEcho(f *os.File) (func(), error) {
	fd := int(f.Fd())
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}
	raw := *old
	raw.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, unix.TCSETS, old) }, nil
}
