//go:build unix

package keys

import (
	"reflect"
	"testing"
	"time"
)

func TestSSHAddArgs(t *testing.T) {
	tests := []struct {
		name     string
		lifetime time.Duration
		want     []string
	}{
		{"no expiry", 0, []string{"/k"}},
		{"negative no expiry", -time.Minute, []string{"/k"}},
		{"sub-second no expiry", 500 * time.Millisecond, []string{"/k"}},
		{"one hour", time.Hour, []string{"-t", "3600", "/k"}},
		{"eight hours", 8 * time.Hour, []string{"-t", "28800", "/k"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sshAddArgs(tc.lifetime, "/k")
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("sshAddArgs(%v) = %v, want %v", tc.lifetime, got, tc.want)
			}
		})
	}
}
