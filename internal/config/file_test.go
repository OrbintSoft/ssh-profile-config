package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoadValid(t *testing.T) {
	f, err := Load(filepath.Join("testdata", "valid.toml"))
	if err != nil {
		t.Fatalf("Load(valid) error = %v", err)
	}
	if f.KeyLifetime == nil || *f.KeyLifetime != "8h" {
		t.Errorf("KeyLifetime = %v, want 8h", f.KeyLifetime)
	}
	if f.MaxAttempts == nil || *f.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %v, want 5", f.MaxAttempts)
	}
	if f.GiveupTTL == nil || *f.GiveupTTL != "30m" {
		t.Errorf("GiveupTTL = %v, want 30m", f.GiveupTTL)
	}
	if f.NoGiveup == nil || !*f.NoGiveup {
		t.Errorf("NoGiveup = %v, want true", f.NoGiveup)
	}
	if f.Quiet == nil || !*f.Quiet {
		t.Errorf("Quiet = %v, want true", f.Quiet)
	}
}

func TestLoadPartialLeavesAbsentKeysNil(t *testing.T) {
	f, err := Load(filepath.Join("testdata", "partial.toml"))
	if err != nil {
		t.Fatalf("Load(partial) error = %v", err)
	}
	if f.KeyLifetime == nil || *f.KeyLifetime != "2h" {
		t.Errorf("KeyLifetime = %v, want 2h", f.KeyLifetime)
	}
	if f.MaxAttempts != nil || f.GiveupTTL != nil || f.NoGiveup != nil {
		t.Errorf("absent keys must stay nil, got %+v", f)
	}
}

func TestLoadMissingIsZeroNoError(t *testing.T) {
	f, err := Load(filepath.Join("testdata", "does-not-exist.toml"))
	if err != nil {
		t.Fatalf("a missing file must not error, got %v", err)
	}
	if (f != File{}) {
		t.Errorf("a missing file must give the zero File, got %+v", f)
	}
}

func TestLoadUnknownKeyErrorsButDecodesKnown(t *testing.T) {
	f, err := Load(filepath.Join("testdata", "unknown.toml"))
	if err == nil || !strings.Contains(err.Error(), "bogus_key") {
		t.Fatalf("want an error naming bogus_key, got %v", err)
	}
	if f.KeyLifetime == nil || *f.KeyLifetime != "1h" {
		t.Errorf("the recognised key must still decode, got %v", f.KeyLifetime)
	}
}

func TestLoadMalformedErrors(t *testing.T) {
	f, err := Load(filepath.Join("testdata", "malformed.toml"))
	if err == nil {
		t.Fatal("a syntax error must be reported")
	}
	if (f != File{}) {
		t.Errorf("a malformed file must give the zero File, got %+v", f)
	}
}

func TestResolveDefaults(t *testing.T) {
	s, errs := Resolve(File{}, lookupFrom(nil))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := Settings{KeyLifetime: DefaultKeyLifetime, GiveupTTL: DefaultGiveupTTL}
	if s != want {
		t.Errorf("Resolve(empty) = %+v, want %+v", s, want)
	}
}

func TestResolveFileWins(t *testing.T) {
	file := File{
		KeyLifetime: ptr("2h"),
		MaxAttempts: ptr(5),
		GiveupTTL:   ptr("30m"),
		NoGiveup:    ptr(true),
		Quiet:       ptr(true),
	}
	s, errs := Resolve(file, lookupFrom(nil))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := Settings{
		KeyLifetime: 2 * time.Hour,
		MaxAttempts: 5,
		GiveupTTL:   30 * time.Minute,
		NoGiveup:    true,
		Quiet:       true,
	}
	if s != want {
		t.Errorf("Resolve(file) = %+v, want %+v", s, want)
	}
}

func TestResolveEnvOverridesFile(t *testing.T) {
	file := File{KeyLifetime: ptr("2h"), MaxAttempts: ptr(2)}
	env := map[string]string{
		"SSHAKKU_KEY_LIFETIME": "15m",
		"SSHAKKU_MAX_ATTEMPTS": "7",
	}
	s, errs := Resolve(file, lookupFrom(env))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if s.KeyLifetime != 15*time.Minute {
		t.Errorf("KeyLifetime = %v, want 15m (env wins)", s.KeyLifetime)
	}
	if s.MaxAttempts != 7 {
		t.Errorf("MaxAttempts = %d, want 7 (env wins)", s.MaxAttempts)
	}
}

func TestResolveEnvCanOverrideBoolToFalse(t *testing.T) {
	file := File{Quiet: ptr(true)}
	s, _ := Resolve(file, lookupFrom(map[string]string{"SSHAKKU_QUIET": "0"}))
	if s.Quiet {
		t.Error("SSHAKKU_QUIET=0 must override quiet = true in the file")
	}
}

func TestResolveInvalidEnvMaxAttemptsFallsToFile(t *testing.T) {
	file := File{MaxAttempts: ptr(4)}
	s, _ := Resolve(file, lookupFrom(map[string]string{"SSHAKKU_MAX_ATTEMPTS": "0"}))
	if s.MaxAttempts != 4 {
		t.Errorf("MaxAttempts = %d, want 4 (invalid env falls through to file)", s.MaxAttempts)
	}
}

func TestResolveMalformedEnvDurationReportsAndDefaults(t *testing.T) {
	s, errs := Resolve(File{}, lookupFrom(map[string]string{"SSHAKKU_KEY_LIFETIME": "banana"}))
	if len(errs) == 0 {
		t.Fatal("a malformed duration must be reported")
	}
	if s.KeyLifetime != DefaultKeyLifetime {
		t.Errorf("KeyLifetime = %v, want the default on a malformed value", s.KeyLifetime)
	}
}
