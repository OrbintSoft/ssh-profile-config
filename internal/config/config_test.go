package config

import (
	"testing"
	"time"
)

func TestKeyLifetime(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{"empty defaults", "", DefaultKeyLifetime, false},
		{"explicit hours", "1h", time.Hour, false},
		{"minutes", "20m", 20 * time.Minute, false},
		{"zero disables", "0", 0, false},
		{"negative disables", "-5m", 0, false},
		{"malformed falls back", "banana", DefaultKeyLifetime, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := KeyLifetime(tc.raw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("KeyLifetime(%q) err = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("KeyLifetime(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestGiveupTTL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{"empty defaults", "", DefaultGiveupTTL, false},
		{"explicit hours", "2h", 2 * time.Hour, false},
		{"zero never expires", "0", 0, false},
		{"negative never expires", "-1h", 0, false},
		{"malformed falls back", "soon", DefaultGiveupTTL, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GiveupTTL(tc.raw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("GiveupTTL(%q) err = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("GiveupTTL(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestEnvInt(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"", 0},
		{" 5 ", 5},
		{"3", 3},
		{"0", 0},
		{"-2", 0},
		{"banana", 0},
	}
	for _, tc := range tests {
		if got := EnvInt(tc.raw); got != tc.want {
			t.Errorf("EnvInt(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestIsTruthy(t *testing.T) {
	truthy := []string{"1", "true", "yes", "on", "TRUE", " On "}
	for _, raw := range truthy {
		if !IsTruthy(raw) {
			t.Errorf("IsTruthy(%q) = false, want true", raw)
		}
	}
	falsy := []string{"", "0", "false", "no", "off", "banana"}
	for _, raw := range falsy {
		if IsTruthy(raw) {
			t.Errorf("IsTruthy(%q) = true, want false", raw)
		}
	}
}
