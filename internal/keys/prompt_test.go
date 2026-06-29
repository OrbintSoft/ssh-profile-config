package keys

import (
	"errors"
	"testing"
)

func TestKDialogPrompt(t *testing.T) {
	t.Run("returns the entered passphrase, newline trimmed", func(t *testing.T) {
		r := newFakeRunner().on("kdialog", stdout("typed-pass\n", 0))
		got, err := KDialogPrompter{Runner: r}.Prompt("id_rsa")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "typed-pass" {
			t.Fatalf("passphrase = %q, want typed-pass", got)
		}
		want := []string{"--password", "Enter passphrase for id_rsa"}
		if a := r.calls[0].Args; !equalStrings(a, want) {
			t.Fatalf("args = %v, want %v", a, want)
		}
	})

	t.Run("a dismissed dialog is ErrPromptCanceled", func(t *testing.T) {
		r := newFakeRunner().on("kdialog", stdout("", 1))
		if _, err := (KDialogPrompter{Runner: r}).Prompt("id_rsa"); !errors.Is(err, ErrPromptCanceled) {
			t.Fatalf("error = %v, want ErrPromptCanceled", err)
		}
	})

	t.Run("a failure to start kdialog is an error", func(t *testing.T) {
		wantErr := errors.New("boom")
		r := newFakeRunner().on("kdialog", fails(wantErr))
		if _, err := (KDialogPrompter{Runner: r}).Prompt("id_rsa"); !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
	})
}

func TestKDialogAvailable(t *testing.T) {
	found := KDialogPrompter{lookPath: func(string) (string, error) { return "/usr/bin/kdialog", nil }}
	if !found.Available() {
		t.Fatal("Available = false, want true when kdialog is on PATH")
	}
	missing := KDialogPrompter{lookPath: func(string) (string, error) { return "", errors.New("not found") }}
	if missing.Available() {
		t.Fatal("Available = true, want false when kdialog is absent")
	}
}
