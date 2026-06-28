package sessionlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.log")
	lg := New(path)
	if err := lg.Log("INFO", "first"); err != nil {
		t.Fatal(err)
	}
	if err := lg.Log("ERROR", "second"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "[INFO] first") || !strings.Contains(got, "[ERROR] second") {
		t.Errorf("log missing entries:\n%s", got)
	}
	if n := strings.Count(got, "\n"); n != 2 {
		t.Errorf("newline count = %d, want 2", n)
	}
}

func TestLogPerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.log")
	if err := New(path).Log("INFO", "x"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
}

func TestLogTrims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.log")
	lg := &Logger{path: path, maxLines: 3}
	for i := 0; i < 10; i++ {
		if err := lg.Log("INFO", fmt.Sprintf("line-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("kept %d lines, want 3", len(lines))
	}
	if !strings.Contains(lines[0], "line-7") {
		t.Errorf("first kept line = %q, want line-7", lines[0])
	}
	if !strings.Contains(lines[2], "line-9") {
		t.Errorf("last line = %q, want line-9", lines[2])
	}
}
