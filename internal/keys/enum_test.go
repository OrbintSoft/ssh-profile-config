package keys

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnumeratorKeys(t *testing.T) {
	dir := t.TempDir()
	// Regular key files we expect to find.
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		writeFile(t, filepath.Join(dir, name))
	}
	// Files we must skip: public keys, unrelated files, and a non-id_ name.
	for _, name := range []string{"id_ed25519.pub", "id_rsa.pub", "config", "known_hosts", "authorized_keys"} {
		writeFile(t, filepath.Join(dir, name))
	}
	// A subdirectory named like a key must be skipped (not a regular file).
	if err := os.Mkdir(filepath.Join(dir, "id_dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A symlink named like a key must be skipped (matches `find -type f`).
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(dir, "id_rsa"), filepath.Join(dir, "id_link")); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Enumerator{Dir: dir}.Keys()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{filepath.Join(dir, "id_ed25519"), filepath.Join(dir, "id_rsa")}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %v, want %v", got, want)
		}
	}
}

func TestEnumeratorMissingDir(t *testing.T) {
	got, err := Enumerator{Dir: filepath.Join(t.TempDir(), "no-such-dir")}.Keys()
	if err != nil {
		t.Fatalf("missing dir should be no error, got %v", err)
	}
	if got != nil {
		t.Fatalf("keys = %v, want nil for a missing dir", got)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}
