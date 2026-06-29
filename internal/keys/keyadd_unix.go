//go:build unix

package keys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/OrbintSoft/sshakku/internal/keyring"
)

const (
	// defaultAddTimeout caps each ssh-add so a stuck prompt cannot hang login.
	defaultAddTimeout = 60 * time.Second
	// defaultKeyTTL bounds how long the stashed passphrase lives in the keyring,
	// so an entry ssh-add never reads still expires.
	defaultKeyTTL = 60 * time.Second
	// keyDescBytes sizes the random keyring description for a stashed passphrase.
	keyDescBytes = 16
)

// ExecKeyAdder adds keys with the real ssh-add.
type ExecKeyAdder struct {
	// AskpassProg is the absolute path to the SSH_ASKPASS helper — the sshakku
	// binary under a name that runs `askpass`. Required by AddWithAskpass.
	AskpassProg string
	// AddTimeout caps each ssh-add; 0 uses defaultAddTimeout.
	AddTimeout time.Duration
	// KeyTTL bounds the stashed passphrase's lifetime; 0 uses defaultKeyTTL.
	KeyTTL time.Duration
}

// AddWithAskpass stashes passphrase in the keyring, then runs ssh-add detached
// from any terminal so it fetches the passphrase through the SSH_ASKPASS helper
// keyed by the keyring serial. The passphrase never enters argv or the inherited
// environment of any other process.
func (a ExecKeyAdder) AddWithAskpass(keyfile, passphrase string) (int, error) {
	desc, err := randomKeyDesc()
	if err != nil {
		return 0, err
	}
	serial, err := keyring.Add(desc, []byte(passphrase))
	if err != nil {
		return 0, fmt.Errorf("stash passphrase: %w", err)
	}
	ttl := a.KeyTTL
	if ttl == 0 {
		ttl = defaultKeyTTL
	}
	_ = keyring.SetTimeout(serial, ttl)

	env := []string{
		"SSH_ASKPASS=" + a.AskpassProg,
		"SSH_ASKPASS_REQUIRE=force",
		EnvKeyctlSerial + "=" + strconv.Itoa(int(serial)),
	}
	env = passThrough(env, "PATH", "HOME", "USER", "DISPLAY", "WAYLAND_DISPLAY",
		"SSH_AUTH_SOCK", "XDG_RUNTIME_DIR", "XDG_CONFIG_HOME", "DBUS_SESSION_BUS_ADDRESS")
	return a.runSSHAdd(env, keyfile, true)
}

// AddInteractive runs ssh-add attached to the terminal so it can prompt the user
// directly — the path taken when no graphical prompter is available.
func (a ExecKeyAdder) AddInteractive(keyfile string) (int, error) {
	return a.runSSHAdd(os.Environ(), keyfile, false)
}

// runSSHAdd runs `ssh-add <keyfile>` with env, returning its exit code. With
// askpass, ssh-add is detached (setsid, no stdin) so it uses SSH_ASKPASS and its
// chatter is discarded; otherwise it inherits the terminal to prompt.
func (a ExecKeyAdder) runSSHAdd(env []string, keyfile string, askpass bool) (int, error) {
	to := a.AddTimeout
	if to == 0 {
		to = defaultAddTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), to)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh-add", keyfile)
	cmd.Env = env
	if askpass {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		cmd.Stdin = nil
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return 0, err
	}
	return 0, nil
}

// randomKeyDesc returns a unique keyring description, so concurrent key loads do
// not collide on (and overwrite) one another's stashed passphrase.
func randomKeyDesc() (string, error) {
	b := make([]byte, keyDescBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sshakku-pass-" + hex.EncodeToString(b), nil
}

// passThrough appends "NAME=value" for each named variable that is set, leaving
// the child env minimal — only what ssh-add and the askpass helper need.
func passThrough(env []string, names ...string) []string {
	for _, name := range names {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

var _ KeyAdder = ExecKeyAdder{}
