package keys

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// passphrasePromptRe matches OpenSSH's key-passphrase prompt in both the ssh
// client form (`Enter passphrase for key '/path':`) and the ssh-add form
// (`Enter passphrase for /path:`), capturing the key path.
var passphrasePromptRe = regexp.MustCompile(`^Enter passphrase for (?:key )?'?(.+?)'?:?\s*$`)

// ParsePassphrasePrompt extracts the key file from an SSH_ASKPASS prompt and
// reports whether the prompt is a key-passphrase request. A prompt it does not
// recognise (a host-key confirmation, a login password, anything else) returns
// ok=false so the broker passes it through to the terminal instead of treating it
// as a passphrase to fetch from the wallet.
func ParsePassphrasePrompt(prompt string) (keyfile string, ok bool) {
	m := passphrasePromptRe.FindStringSubmatch(strings.TrimSpace(prompt))
	if m == nil {
		return "", false
	}
	keyfile = strings.TrimSpace(m[1])
	if keyfile == "" {
		return "", false
	}
	return keyfile, true
}

// TTY prompts the user on the controlling terminal as a fallback: when the wallet
// has no passphrase for the requested key, or for a prompt that is not a key
// passphrase at all (host-key confirmation, login password). It returns an error
// when no terminal is available.
type TTY interface {
	// Prompt writes prompt to the terminal and reads one line; when secret is
	// true the input is not echoed.
	Prompt(prompt string, secret bool) (string, error)
}

// Broker answers ssh's SSH_ASKPASS request when an interactive ssh hits a key
// that has expired from the agent. A key-passphrase prompt is served from the
// secret store (silently), falling back to the terminal — and storing what the
// user types — on a miss; any other prompt is passed straight through to the
// terminal. It never reads or writes the keyring: that one-shot path belongs to
// the proactive key-loading flow, not to this reactive broker.
type Broker struct {
	Secret SecretBackend
	TTY    TTY
	Log    Logger
	Config Config
}

// Answer returns the reply to send back to ssh on stdout and whether the request
// succeeded (a false ok maps to a non-zero askpass exit, which ssh treats as a
// declined prompt).
func (b Broker) Answer(prompt string) (reply string, ok bool) {
	keyfile, isPassphrase := ParsePassphrasePrompt(prompt)
	if !isPassphrase {
		ans, err := b.TTY.Prompt(prompt, !looksLikeConfirmation(prompt))
		if err != nil {
			b.logf("ERROR", "askpass: no terminal for prompt: %v", err)
			return "", false
		}
		return ans, true
	}

	keyname := filepath.Base(keyfile)
	service := servicePrefixOf(b.Config) + "-" + keyname

	pass, found, err := b.Secret.Lookup(service)
	if err != nil {
		b.logf("ERROR", "askpass: secret lookup for %s: %v", keyname, err)
	} else if found && strings.TrimSpace(pass) != "" {
		b.logf("INFO", "askpass: provided passphrase for %s from the wallet", keyname)
		return pass, true
	}

	// Wallet miss: prompt on the terminal, then store what the user types so the
	// next expiry is silent.
	typed, err := b.TTY.Prompt(prompt, true)
	if err != nil {
		b.logf("ERROR", "askpass: no terminal to prompt for %s: %v", keyname, err)
		return "", false
	}
	if strings.TrimSpace(typed) != "" {
		b.storePassphrase(service, keyname, typed)
	}
	return typed, true
}

// looksLikeConfirmation reports whether prompt is a yes/no host-key confirmation,
// which must be echoed (unlike a passphrase or password).
func looksLikeConfirmation(prompt string) bool {
	p := strings.ToLower(prompt)
	return strings.Contains(p, "(yes/no") ||
		strings.Contains(p, "fingerprint)") ||
		strings.Contains(p, "continue connecting")
}

func (b Broker) storePassphrase(service, keyname, passphrase string) {
	if err := storeInWallet(b.Secret, service, keyname, passphrase); err != nil {
		b.logf("ERROR", "askpass: store passphrase for %s: %v", keyname, err)
		return
	}
	b.logf("INFO", "askpass: stored passphrase for %s", keyname)
}

func (b Broker) logf(level, format string, args ...any) {
	if b.Log == nil {
		return
	}
	_ = b.Log.Log(level, fmt.Sprintf(format, args...))
}

// servicePrefixOf returns the per-key secret-store service prefix for c.
func servicePrefixOf(c Config) string {
	if c.ServicePrefix != "" {
		return c.ServicePrefix
	}
	return defaultServicePrefix
}

// storeInWallet saves passphrase under service with the standard label, so the
// loader and the broker write entries the same way.
func storeInWallet(secret SecretBackend, service, keyname, passphrase string) error {
	return secret.Store(service, "SSH Passphrase for "+keyname, passphrase)
}
