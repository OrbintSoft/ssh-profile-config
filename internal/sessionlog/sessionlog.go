// Package sessionlog appends timestamped, level-tagged lines to sshepherd's
// owner-only session log and keeps it bounded to a fixed number of recent lines.
package sessionlog

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// DefaultMaxLines bounds the session log; older lines are dropped on write.
const DefaultMaxLines = 100

const (
	filePerm   = 0o600
	timeLayout = "2006-01-02 15:04:05"
)

// Logger appends to a single log file, trimming it to maxLines after each write.
type Logger struct {
	path     string
	maxLines int
}

// New returns a Logger writing to path, bounded to DefaultMaxLines.
func New(path string) *Logger {
	return &Logger{path: path, maxLines: DefaultMaxLines}
}

// Log appends one "TIMESTAMP | [LEVEL] message" line, then trims the file to the
// most recent maxLines lines.
func (l *Logger) Log(level, message string) error {
	line := fmt.Sprintf("%s | [%s] %s\n", time.Now().Format(timeLayout), level, message)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, filePerm)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return l.trim()
}

// trim rewrites the file keeping only its last maxLines lines.
func (l *Logger) trim() error {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= l.maxLines {
		return nil
	}
	kept := lines[len(lines)-l.maxLines:]
	return os.WriteFile(l.path, []byte(strings.Join(kept, "\n")+"\n"), filePerm)
}
