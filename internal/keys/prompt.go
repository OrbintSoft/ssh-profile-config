package keys

import (
	"errors"
	"strings"
)

// ErrPromptCanceled is returned by a Prompter when the user dismisses the dialog
// instead of entering a passphrase. The loader treats it as "give up on this key"
// without retrying.
var ErrPromptCanceled = errors.New("passphrase prompt canceled")

// Prompter asks the user for a key's passphrase through a graphical dialog.
type Prompter interface {
	// Prompt returns the passphrase entered for keyname, ErrPromptCanceled if the
	// user dismisses the dialog, or another error if the prompt cannot run.
	Prompt(keyname string) (string, error)
	// Available reports whether the prompter's program is installed, so the loader
	// can fall back to a terminal prompt when no graphical prompter exists.
	Available() bool
}

// kdialogBin is KDE's dialog tool.
const kdialogBin = "kdialog"

// KDialogPrompter prompts via `kdialog --password`. The entered text is returned
// on stdout; a canceled or closed dialog exits non-zero.
type KDialogPrompter struct {
	Runner Runner
	// lookPath resolves a binary on PATH; nil uses the os/exec default. Injectable
	// for tests.
	lookPath func(string) (string, error)
}

// Prompt shows the password dialog for keyname.
func (p KDialogPrompter) Prompt(keyname string) (string, error) {
	res, err := p.Runner.Run(Cmd{
		Name: kdialogBin,
		Args: []string{"--password", "Enter passphrase for " + keyname},
	})
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", ErrPromptCanceled
	}
	return strings.TrimRight(string(res.Stdout), "\n"), nil
}

// Available reports whether kdialog is on PATH.
func (p KDialogPrompter) Available() bool {
	look := p.lookPath
	if look == nil {
		look = execLookPath
	}
	_, err := look(kdialogBin)
	return err == nil
}

var _ Prompter = KDialogPrompter{}
