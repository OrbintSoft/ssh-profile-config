//go:build linux

package keyring

import (
	"bytes"
	"fmt"
	"testing"
	"time"
)

func TestKeyringRoundTrip(t *testing.T) {
	desc := fmt.Sprintf("sshakku-keyring-test-%d", time.Now().UnixNano())
	payload := []byte("a-secret-passphrase")

	s, err := Add(desc, payload)
	if err != nil {
		t.Skipf("user keyring unavailable: %v", err)
	}
	// Best-effort cleanup if an assertion below aborts before the explicit unlink.
	defer func() { _ = Unlink(s) }()

	got, err := Read(s)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Read = %q, want %q", got, payload)
	}

	if s2, ok := Search(desc); !ok || s2 != s {
		t.Fatalf("Search = (%d, %v), want (%d, true)", s2, ok, s)
	}

	if err := SetTimeout(s, time.Minute); err != nil {
		t.Fatalf("SetTimeout: %v", err)
	}

	if err := Unlink(s); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	if _, ok := Search(desc); ok {
		t.Fatal("key still found after Unlink")
	}
}
