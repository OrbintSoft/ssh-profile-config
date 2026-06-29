package keys

import (
	"fmt"
	"strings"
)

// SecretBackend stores and retrieves a key's passphrase in the OS secret store.
// It is the seam the platform secret stores plug into; this slice ships only the
// D-Bus Secret Service (KDE Wallet / GNOME Keyring) implementation below. service
// is an opaque per-key identifier the backend maps onto its own schema.
type SecretBackend interface {
	// Lookup returns the stored passphrase for service and whether one was found.
	// A miss is reported as found=false, not an error.
	Lookup(service string) (passphrase string, found bool, err error)
	// Store saves passphrase for service under a human-readable label.
	Store(service, label, passphrase string) error
}

// secretToolBin is the libsecret CLI shared by KDE Wallet and GNOME Keyring,
// which both implement the D-Bus Secret Service API.
const secretToolBin = "secret-tool"

// SecretToolBackend keeps passphrases in the D-Bus Secret Service via secret-tool.
// The passphrase travels on the process's stdin, never in argv, so it cannot leak
// through `ps` or /proc/<pid>/cmdline.
type SecretToolBackend struct {
	Runner Runner
	// User is the "username" attribute, constant for the login session.
	User string
}

// Lookup runs `secret-tool lookup service <service> username <user>`. secret-tool
// emits the secret verbatim, so a trailing newline (e.g. from an entry stored by
// the earlier shell version) is trimmed. A non-zero exit means no entry — handled
// as a miss, not an error, so the loader falls back to prompting.
func (b SecretToolBackend) Lookup(service string) (string, bool, error) {
	res, err := b.Runner.Run(Cmd{
		Name: secretToolBin,
		Args: []string{"lookup", "service", service, "username", b.User},
	})
	if err != nil {
		return "", false, err
	}
	if res.Code != 0 {
		return "", false, nil
	}
	return strings.TrimRight(string(res.Stdout), "\n"), true, nil
}

// Store runs `secret-tool store --label=<label> service <service> username
// <user>`, feeding the passphrase on stdin. Unlike the earlier `echo | …`, no
// trailing newline is appended, so the secret is stored exactly.
func (b SecretToolBackend) Store(service, label, passphrase string) error {
	res, err := b.Runner.Run(Cmd{
		Name:  secretToolBin,
		Args:  []string{"store", "--label=" + label, "service", service, "username", b.User},
		Stdin: passphrase,
	})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("secret-tool store exited %d: %s", res.Code, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

var _ SecretBackend = SecretToolBackend{}
