package keys

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	defaultMaxAttempts   = 3
	defaultServicePrefix = "SSH-Key"
)

// Logger records one level-tagged line. A nil Logger disables logging.
type Logger interface {
	Log(level, message string) error
}

// KeyLister lists the private-key files to consider, in load order.
type KeyLister interface {
	Keys() ([]string, error)
}

// KeyAdder adds one private key to the agent via ssh-add, returning ssh-add's
// exit code (0 = added). A non-zero code is a normal "wrong passphrase" outcome,
// reported in the int; only a failure to run ssh-add is an error.
type KeyAdder interface {
	// AddWithAskpass adds keyfile, handing passphrase to ssh-add out of band
	// through the keyring + SSH_ASKPASS helper, so it never appears in argv.
	AddWithAskpass(keyfile, passphrase string) (int, error)
	// AddInteractive adds keyfile, letting ssh-add prompt on the terminal.
	AddInteractive(keyfile string) (int, error)
}

// Config tunes a Loader.
type Config struct {
	// GUI is true when a graphical session and prompter are available, selecting
	// the secret-store path over a terminal prompt.
	GUI bool
	// ServicePrefix prefixes the per-key secret-store service; "" uses "SSH-Key".
	ServicePrefix string
	// MaxAttempts bounds the retries per key; <1 uses 3.
	MaxAttempts int
}

// Loader loads the user's keys into the agent, skipping any already present and
// pulling passphrases from the secret store (or prompting) when needed.
type Loader struct {
	Keys   KeyLister
	Runner Runner
	Secret SecretBackend
	Prompt Prompter
	Adder  KeyAdder
	Log    Logger
	Config Config
}

// LoadKeys enumerates the keys, snapshots the agent's loaded fingerprints once,
// and loads each missing key (best-effort: a failure on one key is logged and the
// rest still run). It returns an error only when the keys cannot be enumerated or
// the agent cannot be queried.
func (l Loader) LoadKeys() error {
	keyfiles, err := l.Keys.Keys()
	if err != nil {
		return fmt.Errorf("enumerate keys: %w", err)
	}
	if len(keyfiles) == 0 {
		l.logf("INFO", "no keys to load")
		return nil
	}
	loaded, err := AgentFingerprints(l.Runner)
	if err != nil {
		return fmt.Errorf("read agent fingerprints: %w", err)
	}
	for _, keyfile := range keyfiles {
		l.loadOne(keyfile, loaded)
	}
	return nil
}

// loadOne loads a single key unless its fingerprint is already in the agent.
func (l Loader) loadOne(keyfile string, loaded map[string]bool) {
	keyname := filepath.Base(keyfile)

	fp, err := FileFingerprint(l.Runner, keyfile)
	if err != nil {
		// ssh-keygen could not run; dedup is impossible, but ssh-add may still
		// add the key, so press on rather than skip it.
		l.logf("ERROR", "fingerprint %s: %v", keyname, err)
	}
	if fp != "" && loaded[fp] {
		l.logf("INFO", "%s already added to agent", keyname)
		return
	}
	l.addWithRetries(keyfile, keyname)
}

// addWithRetries tries to add keyfile up to MaxAttempts times. A canceled prompt
// or a hard error gives up immediately; only a non-zero ssh-add exit is retried.
func (l Loader) addWithRetries(keyfile, keyname string) {
	max := l.Config.MaxAttempts
	if max < 1 {
		max = defaultMaxAttempts
	}
	for attempt := 1; attempt <= max; attempt++ {
		rc, err := l.addOnce(keyfile, keyname)
		if err != nil {
			if errors.Is(err, ErrPromptCanceled) {
				l.logf("ERROR", "passphrase prompt canceled for %s", keyname)
			} else {
				l.logf("ERROR", "add %s: %v", keyname, err)
			}
			return
		}
		if rc == 0 {
			l.logf("INFO", "added %s to agent", keyname)
			return
		}
		l.logf("ERROR", "failed to add %s (attempt %d/%d)", keyname, attempt, max)
	}
}

// addOnce performs one add attempt: the secret-store + askpass path when a GUI is
// available, otherwise a terminal prompt by ssh-add.
func (l Loader) addOnce(keyfile, keyname string) (int, error) {
	if !l.Config.GUI {
		l.logf("INFO", "no GUI detected, adding %s on the terminal", keyname)
		return l.Adder.AddInteractive(keyfile)
	}

	service := l.servicePrefix() + "-" + keyname
	passphrase, stored, err := l.passphraseFor(service, keyname)
	if err != nil {
		return 0, err
	}
	rc, err := l.Adder.AddWithAskpass(keyfile, passphrase)
	if err != nil {
		return 0, err
	}
	if rc == 0 && !stored {
		l.storePassphrase(service, keyname, passphrase)
	}
	return rc, nil
}

// passphraseFor returns the passphrase for a key and whether it came from the
// secret store. A store miss or error falls back to prompting the user.
func (l Loader) passphraseFor(service, keyname string) (string, bool, error) {
	pass, found, err := l.Secret.Lookup(service)
	if err != nil {
		l.logf("ERROR", "secret lookup for %s: %v", keyname, err)
		found = false
	}
	if found && strings.TrimSpace(pass) != "" {
		l.logf("INFO", "using stored passphrase for %s", keyname)
		return pass, true, nil
	}
	l.logf("INFO", "no stored passphrase for %s, prompting", keyname)
	pass, err = l.Prompt.Prompt(keyname)
	if err != nil {
		return "", false, err
	}
	return pass, false, nil
}

// storePassphrase saves a freshly prompted passphrase after a successful add.
// Storing is best-effort: the key is already in the agent if this fails.
func (l Loader) storePassphrase(service, keyname, passphrase string) {
	label := "SSH Passphrase for " + keyname
	if err := l.Secret.Store(service, label, passphrase); err != nil {
		l.logf("ERROR", "store passphrase for %s: %v", keyname, err)
		return
	}
	l.logf("INFO", "stored passphrase for %s", keyname)
}

func (l Loader) servicePrefix() string {
	if l.Config.ServicePrefix != "" {
		return l.Config.ServicePrefix
	}
	return defaultServicePrefix
}

func (l Loader) logf(level, format string, args ...any) {
	if l.Log == nil {
		return
	}
	_ = l.Log.Log(level, fmt.Sprintf(format, args...))
}

var _ KeyLister = Enumerator{}
