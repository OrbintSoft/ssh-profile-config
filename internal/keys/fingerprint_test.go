package keys

import (
	"errors"
	"testing"
)

func TestFileFingerprint(t *testing.T) {
	t.Run("reads the SHA256 field", func(t *testing.T) {
		r := newFakeRunner().on("ssh-keygen", stdout("256 SHA256:abc123 user@host (ED25519)\n", 0))
		fp, err := FileFingerprint(r, "/home/u/.ssh/id_ed25519")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fp != "SHA256:abc123" {
			t.Fatalf("fingerprint = %q, want SHA256:abc123", fp)
		}
		if got := r.calls[0].Args; len(got) != 2 || got[0] != "-lf" || got[1] != "/home/u/.ssh/id_ed25519" {
			t.Fatalf("ssh-keygen args = %v, want [-lf <path>]", got)
		}
	})

	t.Run("unreadable key yields empty fingerprint, no error", func(t *testing.T) {
		r := newFakeRunner().on("ssh-keygen", stdout("", 1))
		fp, err := FileFingerprint(r, "/home/u/.ssh/id_rsa")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fp != "" {
			t.Fatalf("fingerprint = %q, want empty", fp)
		}
	})

	t.Run("a failure to start ssh-keygen is an error", func(t *testing.T) {
		wantErr := errors.New("exec: \"ssh-keygen\": not found")
		r := newFakeRunner().on("ssh-keygen", fails(wantErr))
		if _, err := FileFingerprint(r, "/home/u/.ssh/id_rsa"); !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
	})
}

func TestAgentFingerprints(t *testing.T) {
	t.Run("collects every loaded fingerprint", func(t *testing.T) {
		out := "256 SHA256:aaa one (ED25519)\n2048 SHA256:bbb two (RSA)\n"
		r := newFakeRunner().on("ssh-add", stdout(out, 0))
		set, err := AgentFingerprints(r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !set["SHA256:aaa"] || !set["SHA256:bbb"] {
			t.Fatalf("set = %v, want both aaa and bbb", set)
		}
		if len(set) != 2 {
			t.Fatalf("set has %d entries, want 2", len(set))
		}
	})

	t.Run("empty agent yields an empty set, no error", func(t *testing.T) {
		r := newFakeRunner().on("ssh-add", stdout("The agent has no identities.\n", 1))
		set, err := AgentFingerprints(r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(set) != 0 {
			t.Fatalf("set = %v, want empty", set)
		}
	})

	t.Run("a failure to start ssh-add is an error", func(t *testing.T) {
		wantErr := errors.New("boom")
		r := newFakeRunner().on("ssh-add", fails(wantErr))
		if _, err := AgentFingerprints(r); !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
	})
}

func TestFingerprintField(t *testing.T) {
	cases := map[string]string{
		"256 SHA256:abc user@host (ED25519)": "SHA256:abc",
		"2048 MD5:aa:bb:cc legacy (RSA)":     "MD5:aa:bb:cc",
		"The agent has no identities.":       "",
		"":                                   "",
		"single":                             "",
	}
	for line, want := range cases {
		if got := fingerprintField(line); got != want {
			t.Errorf("fingerprintField(%q) = %q, want %q", line, got, want)
		}
	}
}
