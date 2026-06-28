package main

import (
	"io"
	"testing"
)

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
		{"shell-init", []string{"shell-init"}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := run(io.Discard, io.Discard, tc.args); got != tc.want {
				t.Errorf("run(%q) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
