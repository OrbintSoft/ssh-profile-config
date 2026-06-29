package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// File mirrors the on-disk TOML config. Each field is a pointer so an absent key
// stays nil, letting Resolve tell "unset in the file" from "set to the zero
// value" and apply the precedence environment variable > file > default.
type File struct {
	KeyLifetime *string `toml:"key_lifetime"`
	MaxAttempts *int    `toml:"max_attempts"`
	GiveupTTL   *string `toml:"giveup_ttl"`
	NoGiveup    *bool   `toml:"no_giveup"`
	Quiet       *bool   `toml:"quiet"`
}

// Settings is the configuration resolved from environment, file, and defaults.
type Settings struct {
	KeyLifetime time.Duration // 0 disables agent key expiry
	MaxAttempts int           // 0 lets the loader use its own default
	GiveupTTL   time.Duration // 0 never expires the give-up record
	NoGiveup    bool          // true disables give-up tracking entirely
	Quiet       bool          // true suppresses the failure notice
}

// DefaultPath returns the standard config-file location,
// $XDG_CONFIG_HOME/sshakku/config.toml (≈ ~/.config/sshakku/config.toml).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sshakku", "config.toml"), nil
}

// Load reads and decodes the TOML config at path. A missing file is not an
// error: it returns the zero File so callers fall back to environment and
// defaults. Unrecognised keys are reported as an error alongside the decoded
// File, so the caller can warn yet still use the keys it understood; a syntax
// error returns the zero File and the error.
func Load(path string) (File, error) {
	var f File
	md, err := toml.DecodeFile(path, &f)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return f, fmt.Errorf("unrecognised config keys: %s", joinKeys(undecoded))
	}
	return f, nil
}

// Resolve merges the file with the environment over the built-in defaults,
// applying the precedence environment variable > config file > default for each
// setting. lookup is the os.LookupEnv signature; its second result distinguishes
// an unset variable from one set to an empty or "false" value, so an environment
// variable can override a file value in either direction. Non-fatal parse
// problems (a malformed duration) are returned for the caller to log; the
// affected setting falls back to its default.
func Resolve(file File, lookup func(string) (string, bool)) (Settings, []error) {
	var errs []error
	var s Settings

	lifetime, err := KeyLifetime(coalesce(lookup, "SSHAKKU_KEY_LIFETIME", file.KeyLifetime))
	if err != nil {
		errs = append(errs, err)
	}
	s.KeyLifetime = lifetime

	ttl, err := GiveupTTL(coalesce(lookup, "SSHAKKU_GIVEUP_TTL", file.GiveupTTL))
	if err != nil {
		errs = append(errs, err)
	}
	s.GiveupTTL = ttl

	s.MaxAttempts = resolveMaxAttempts(lookup, file.MaxAttempts)
	s.NoGiveup = resolveBool(lookup, "SSHAKKU_NO_GIVEUP", file.NoGiveup)
	s.Quiet = resolveBool(lookup, "SSHAKKU_QUIET", file.Quiet)

	return s, errs
}

// coalesce returns the environment value when the variable is set, else the file
// value when present, else "" (which the duration parsers map to the default).
func coalesce(lookup func(string) (string, bool), key string, fileVal *string) string {
	if v, ok := lookup(key); ok {
		return v
	}
	if fileVal != nil {
		return *fileVal
	}
	return ""
}

// resolveMaxAttempts applies env > file > 0 (the loader's own default). A
// set-but-invalid environment value falls through to the file then the default.
func resolveMaxAttempts(lookup func(string) (string, bool), fileVal *int) int {
	if v, ok := lookup("SSHAKKU_MAX_ATTEMPTS"); ok {
		if n := EnvInt(v); n > 0 {
			return n
		}
	}
	if fileVal != nil && *fileVal > 0 {
		return *fileVal
	}
	return 0
}

// resolveBool applies env > file > false. A set environment variable wins in
// either direction (e.g. SSHAKKU_QUIET=0 overrides quiet = true in the file).
func resolveBool(lookup func(string) (string, bool), key string, fileVal *bool) bool {
	if v, ok := lookup(key); ok {
		return IsTruthy(v)
	}
	if fileVal != nil {
		return *fileVal
	}
	return false
}

// joinKeys renders TOML key paths for an error message.
func joinKeys(keys []toml.Key) string {
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = k.String()
	}
	return strings.Join(names, ", ")
}
