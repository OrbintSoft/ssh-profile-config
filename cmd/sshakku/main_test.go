package main

import (
	"io"
	"testing"
	"time"
)

// TestRun exercises argument dispatch only. shell-init and ensure-agent are
// omitted: both now drive the real agent lifecycle (start, reap, adopt), so
// invoking them here would spawn and reap agents on the test host; that logic is
// covered by the agent package's tests.
func TestRun(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no args", nil, 2},
		{"help", []string{"help"}, 0},
		{"help flag", []string{"--help"}, 0},
		{"unknown command", []string{"bogus"}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := run(io.Discard, io.Discard, tc.args); got != tc.want {
				t.Errorf("run(%q) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}

func TestAskpassExports(t *testing.T) {
	got := askpassExports("/usr/local/bin/sshakku")
	want := "export SSH_ASKPASS='/usr/local/bin/sshakku'\n" +
		"export SSH_ASKPASS_REQUIRE=prefer\n" +
		"export SSHAKKU_ASKPASS=1\n"
	if got != want {
		t.Errorf("askpassExports = %q, want %q", got, want)
	}
}

func TestKeyLifetime(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{"empty defaults", "", defaultKeyLifetime, false},
		{"explicit hours", "1h", time.Hour, false},
		{"minutes", "20m", 20 * time.Minute, false},
		{"zero disables", "0", 0, false},
		{"negative disables", "-5m", 0, false},
		{"malformed falls back", "banana", defaultKeyLifetime, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := keyLifetime(tc.raw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("keyLifetime(%q) err = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("keyLifetime(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
