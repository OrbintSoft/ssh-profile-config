package main

import (
	"io"
	"testing"
)

// TestRun exercises argument dispatch only. shell-init and ensure-agent are
// omitted: both now drive the real agent lifecycle (start, reap, adopt), so
// invoking them here would spawn and reap agents on the test host. doctor is
// omitted for a milder version of the same reason — it reads the host's real
// /proc and probes live sockets. That logic is covered by the agent and diagnose
// package tests.
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
