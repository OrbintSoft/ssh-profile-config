// Package config resolves SSHakku's tunable settings — key lifetime, retry,
// give-up, and notification behaviour. Each setting is read from an environment
// variable; a value the caller cannot parse falls back to a built-in default.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultKeyLifetime caps how long an added key stays in the agent before it
// expires and must be re-added from the wallet. A zero or negative configured
// value disables expiry, so the key stays until the agent does.
const DefaultKeyLifetime = 8 * time.Hour

// DefaultGiveupTTL bounds how long a key stays in the give-up state after its
// retries are exhausted, before a later shell tries it again. A zero or negative
// configured value never expires (the record then clears only on a successful
// add or when the runtime dir is wiped).
const DefaultGiveupTTL = time.Hour

// KeyLifetime parses an agent key lifetime expressed as a Go duration (such as
// "1h" or "20m"), defaulting to DefaultKeyLifetime when raw is empty. A zero or
// negative value disables expiry (returned as 0); a malformed value falls back to
// the default and is returned with an error for the caller to log.
func KeyLifetime(raw string) (time.Duration, error) {
	if raw == "" {
		return DefaultKeyLifetime, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return DefaultKeyLifetime, fmt.Errorf("invalid key lifetime %q: %w", raw, err)
	}
	if d < 0 {
		return 0, nil
	}
	return d, nil
}

// GiveupTTL parses a give-up TTL expressed as a Go duration, defaulting to
// DefaultGiveupTTL when raw is empty. A zero or negative value means "never
// expire" (returned as 0); a malformed value falls back to the default and is
// returned with an error for the caller to log.
func GiveupTTL(raw string) (time.Duration, error) {
	if raw == "" {
		return DefaultGiveupTTL, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return DefaultGiveupTTL, fmt.Errorf("invalid give-up TTL %q: %w", raw, err)
	}
	if d < 0 {
		return 0, nil
	}
	return d, nil
}

// EnvInt parses a positive integer from raw, returning 0 (let the consumer use
// its own default) for an empty, malformed, or non-positive value.
func EnvInt(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// IsTruthy reports whether raw is a recognised affirmative value, used for
// boolean opt-out switches.
func IsTruthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
