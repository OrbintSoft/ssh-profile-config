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

// GiveupStore persists, per key, that loading was abandoned after the bounded
// retries, so later shells skip the key instead of re-prompting on every new
// terminal. A nil GiveupStore disables give-up.
type GiveupStore interface {
	// GivenUp reports whether the key is currently in the give-up state.
	GivenUp(key string) bool
	// Record marks the key as given up after its retries were exhausted.
	Record(key string) error
	// Clear removes any give-up record for the key (e.g. after a success).
	Clear(key string) error
}

// Notifier surfaces a user-facing notice — to the terminal of the interactive
// shell that ran the loader — about a problem the user should act on, such as a
// key that could not be loaded. A nil Notifier suppresses notices; the success
// path never notifies.
type Notifier interface {
	Notify(message string)
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
	Notify Notifier
	Giveup GiveupStore
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
	if l.givenUp(keyname) {
		l.logf("INFO", "%s given up earlier, skipping until the retry window", keyname)
		return
	}
	l.addWithRetries(keyfile, keyname)
}

// addWithRetries loads keyfile, retrying on a wrong passphrase up to MaxAttempts
// times. On success it clears any give-up record; when the attempts are
// exhausted it gives up persistently and notifies the user. A canceled prompt or
// a hard error abandons the key without recording a give-up.
func (l Loader) addWithRetries(keyfile, keyname string) {
	max := l.Config.MaxAttempts
	if max < 1 {
		max = defaultMaxAttempts
	}

	var loaded, exhausted bool
	if l.Config.GUI {
		loaded, exhausted = l.loadViaVaultThenPrompt(keyfile, keyname, max)
	} else {
		loaded, exhausted = l.loadInteractive(keyfile, keyname, max)
	}

	switch {
	case loaded:
		l.clearGiveup(keyname)
	case exhausted:
		l.logf("ERROR", "giving up on %s after %d attempts", keyname, max)
		l.notify("could not load key %s after %d attempts", keyname, max)
		l.recordGiveup(keyname)
	}
}

// loadViaVaultThenPrompt tries a stored passphrase once (a silent success on the
// happy path), then prompts the user up to max times, storing the first prompted
// passphrase that works. A stored passphrase that ssh-add rejects is treated as
// stale and dropped in favour of prompting. It reports whether the key loaded and
// whether the retry attempts were exhausted.
func (l Loader) loadViaVaultThenPrompt(keyfile, keyname string, max int) (loaded, exhausted bool) {
	service := l.servicePrefix() + "-" + keyname

	if pass, ok := l.storedPassphrase(service, keyname); ok {
		rc, err := l.Adder.AddWithAskpass(keyfile, pass)
		if err != nil {
			l.failAdd(keyname, err)
			return false, false
		}
		if rc == 0 {
			l.logf("INFO", "added %s to agent", keyname)
			return true, false
		}
		l.logf("INFO", "stored passphrase for %s is stale, prompting", keyname)
	}

	for attempt := 1; attempt <= max; attempt++ {
		pass, err := l.Prompt.Prompt(keyname)
		if err != nil {
			if errors.Is(err, ErrPromptCanceled) {
				l.logf("ERROR", "passphrase prompt canceled for %s", keyname)
			} else {
				l.failPrompt(keyname, err)
			}
			return false, false
		}
		rc, err := l.Adder.AddWithAskpass(keyfile, pass)
		if err != nil {
			l.failAdd(keyname, err)
			return false, false
		}
		if rc == 0 {
			l.logf("INFO", "added %s to agent", keyname)
			l.storePassphrase(service, keyname, pass)
			return true, false
		}
		l.logf("ERROR", "failed to add %s (attempt %d/%d)", keyname, attempt, max)
	}
	return false, true
}

// loadInteractive lets ssh-add prompt on the terminal, retrying up to max times.
// It is the path taken when no graphical prompter is available.
func (l Loader) loadInteractive(keyfile, keyname string, max int) (loaded, exhausted bool) {
	l.logf("INFO", "no GUI detected, adding %s on the terminal", keyname)
	for attempt := 1; attempt <= max; attempt++ {
		rc, err := l.Adder.AddInteractive(keyfile)
		if err != nil {
			l.failAdd(keyname, err)
			return false, false
		}
		if rc == 0 {
			l.logf("INFO", "added %s to agent", keyname)
			return true, false
		}
		l.logf("ERROR", "failed to add %s (attempt %d/%d)", keyname, attempt, max)
	}
	return false, true
}

// storedPassphrase returns the stored passphrase for service and whether a
// non-empty one was found; a lookup error is logged and treated as a miss.
func (l Loader) storedPassphrase(service, keyname string) (string, bool) {
	pass, found, err := l.Secret.Lookup(service)
	if err != nil {
		l.logf("ERROR", "secret lookup for %s: %v", keyname, err)
		return "", false
	}
	if found && strings.TrimSpace(pass) != "" {
		l.logf("INFO", "using stored passphrase for %s", keyname)
		return pass, true
	}
	l.logf("INFO", "no stored passphrase for %s, prompting", keyname)
	return "", false
}

// failAdd logs and notifies a failure to run ssh-add for a key.
func (l Loader) failAdd(keyname string, err error) {
	l.logf("ERROR", "add %s: %v", keyname, err)
	l.notify("could not load key %s: %v", keyname, err)
}

// failPrompt logs and notifies a non-cancel failure to obtain a passphrase.
func (l Loader) failPrompt(keyname string, err error) {
	l.logf("ERROR", "prompt %s: %v", keyname, err)
	l.notify("could not load key %s: %v", keyname, err)
}

// storePassphrase saves a freshly prompted passphrase after a successful add.
// Storing is best-effort: the key is already in the agent if this fails.
func (l Loader) storePassphrase(service, keyname, passphrase string) {
	if err := storeInWallet(l.Secret, service, keyname, passphrase); err != nil {
		l.logf("ERROR", "store passphrase for %s: %v", keyname, err)
		return
	}
	l.logf("INFO", "stored passphrase for %s", keyname)
}

// givenUp reports whether give-up tracking is enabled and the key is currently
// in the give-up state.
func (l Loader) givenUp(keyname string) bool {
	return l.Giveup != nil && l.Giveup.GivenUp(keyname)
}

// recordGiveup persists that the key was abandoned after its retries, best-effort.
func (l Loader) recordGiveup(keyname string) {
	if l.Giveup == nil {
		return
	}
	if err := l.Giveup.Record(keyname); err != nil {
		l.logf("ERROR", "record give-up for %s: %v", keyname, err)
	}
}

// clearGiveup drops any give-up record after a successful add, best-effort.
func (l Loader) clearGiveup(keyname string) {
	if l.Giveup == nil {
		return
	}
	if err := l.Giveup.Clear(keyname); err != nil {
		l.logf("ERROR", "clear give-up for %s: %v", keyname, err)
	}
}

func (l Loader) servicePrefix() string {
	return servicePrefixOf(l.Config)
}

func (l Loader) logf(level, format string, args ...any) {
	if l.Log == nil {
		return
	}
	_ = l.Log.Log(level, fmt.Sprintf(format, args...))
}

// notify emits a user-facing notice when a Notifier is configured.
func (l Loader) notify(format string, args ...any) {
	if l.Notify == nil {
		return
	}
	l.Notify.Notify(fmt.Sprintf(format, args...))
}

var _ KeyLister = Enumerator{}
