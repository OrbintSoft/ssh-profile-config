package keys

import (
	"errors"
	"strings"
	"testing"
)

func TestSecretToolLookup(t *testing.T) {
	t.Run("hit trims the trailing newline", func(t *testing.T) {
		r := newFakeRunner().on("secret-tool", stdout("hunter2\n", 0))
		b := SecretToolBackend{Runner: r, User: "alice"}
		pass, found, err := b.Lookup("SSH-Key-id_rsa")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found || pass != "hunter2" {
			t.Fatalf("Lookup = (%q, %v), want (hunter2, true)", pass, found)
		}
		want := []string{"lookup", "service", "SSH-Key-id_rsa", "username", "alice"}
		if got := r.calls[0].Args; !equalStrings(got, want) {
			t.Fatalf("args = %v, want %v", got, want)
		}
	})

	t.Run("miss is found=false, no error", func(t *testing.T) {
		r := newFakeRunner().on("secret-tool", stdout("", 1))
		b := SecretToolBackend{Runner: r, User: "alice"}
		_, found, err := b.Lookup("SSH-Key-id_rsa")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Fatal("found = true, want false for a miss")
		}
	})

	t.Run("a failure to start secret-tool is an error", func(t *testing.T) {
		wantErr := errors.New("boom")
		b := SecretToolBackend{Runner: newFakeRunner().on("secret-tool", fails(wantErr)), User: "alice"}
		if _, _, err := b.Lookup("x"); !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
	})
}

func TestSecretToolStore(t *testing.T) {
	const passphrase = "s3cr3t-pass"

	t.Run("passphrase goes on stdin, never in argv", func(t *testing.T) {
		r := newFakeRunner().on("secret-tool", stdout("", 0))
		b := SecretToolBackend{Runner: r, User: "alice"}
		if err := b.Store("SSH-Key-id_rsa", "SSH Passphrase for id_rsa", passphrase); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := r.calls[0]
		if call.Stdin != passphrase {
			t.Fatalf("stdin = %q, want the passphrase", call.Stdin)
		}
		for _, a := range call.Args {
			if strings.Contains(a, passphrase) {
				t.Fatalf("passphrase leaked into argv: %q", a)
			}
		}
		if call.Args[0] != "store" || call.Args[1] != "--label=SSH Passphrase for id_rsa" {
			t.Fatalf("args = %v, want store with the label", call.Args)
		}
	})

	t.Run("a non-zero exit is an error", func(t *testing.T) {
		r := newFakeRunner().on("secret-tool", func(Cmd) (Result, error) {
			return Result{Stderr: []byte("no wallet"), Code: 1}, nil
		})
		b := SecretToolBackend{Runner: r, User: "alice"}
		if err := b.Store("x", "y", passphrase); err == nil {
			t.Fatal("expected an error for a non-zero exit")
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
