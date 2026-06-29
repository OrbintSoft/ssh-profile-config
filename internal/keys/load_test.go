package keys

import (
	"errors"
	"testing"
)

// agentEmpty answers `ssh-add -l` as an empty agent; keygen answers `ssh-keygen
// -lf` with a fingerprint line for a key file.
func agentEmpty() func(Cmd) (Result, error) { return stdout("The agent has no identities.\n", 1) }
func keygen(fp string) func(Cmd) (Result, error) {
	return stdout("256 "+fp+" comment (ED25519)\n", 0)
}

func TestLoadKeysSkipsLoaded(t *testing.T) {
	r := newFakeRunner().
		on("ssh-add", stdout("256 SHA256:DUP loaded (ED25519)\n", 0)).
		on("ssh-keygen", keygen("SHA256:DUP"))
	adder := &fakeKeyAdder{}
	log := &fakeLogger{}
	l := Loader{
		Keys:   fakeLister{paths: []string{"/ssh/id_x"}},
		Runner: r,
		Adder:  adder,
		Log:    log,
		Config: Config{GUI: true},
	}
	if err := l.LoadKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adder.calls) != 0 {
		t.Fatalf("a loaded key must not be added, got %d adds", len(adder.calls))
	}
	if !log.contains("already added") {
		t.Fatalf("expected an 'already added' log, got %v", log.lines)
	}
}

func TestLoadKeysStoredPassphrase(t *testing.T) {
	r := newFakeRunner().on("ssh-add", agentEmpty()).on("ssh-keygen", keygen("SHA256:NEW"))
	secret := &fakeSecret{lookupPass: "stored-pass", lookupFound: true}
	adder := &fakeKeyAdder{withCodes: []int{0}}
	log := &fakeLogger{}
	l := Loader{
		Keys:   fakeLister{paths: []string{"/ssh/id_rsa"}},
		Runner: r, Secret: secret, Prompt: &fakePrompter{}, Adder: adder, Log: log,
		Config: Config{GUI: true},
	}
	if err := l.LoadKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adder.calls) != 1 || adder.calls[0].interactive || adder.calls[0].passphrase != "stored-pass" {
		t.Fatalf("calls = %+v, want one askpass add with the stored pass", adder.calls)
	}
	if len(secret.stored) != 0 {
		t.Fatalf("a looked-up passphrase must not be re-stored, got %v", secret.stored)
	}
}

func TestLoadKeysPromptThenStore(t *testing.T) {
	r := newFakeRunner().on("ssh-add", agentEmpty()).on("ssh-keygen", keygen("SHA256:NEW"))
	secret := &fakeSecret{lookupFound: false}
	prompter := &fakePrompter{pass: "typed-pass"}
	adder := &fakeKeyAdder{withCodes: []int{0}}
	l := Loader{
		Keys:   fakeLister{paths: []string{"/ssh/id_rsa"}},
		Runner: r, Secret: secret, Prompt: prompter, Adder: adder, Log: &fakeLogger{},
		Config: Config{GUI: true},
	}
	if err := l.LoadKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adder.calls) != 1 || adder.calls[0].passphrase != "typed-pass" {
		t.Fatalf("calls = %+v, want one add with the prompted pass", adder.calls)
	}
	if len(secret.stored) != 1 {
		t.Fatalf("a prompted passphrase must be stored once, got %v", secret.stored)
	}
	got := secret.stored[0]
	if got.service != "SSH-Key-id_rsa" || got.label != "SSH Passphrase for id_rsa" || got.passphrase != "typed-pass" {
		t.Fatalf("store = %+v, want service/label/pass for id_rsa", got)
	}
}

func TestLoadKeysRetriesThenGivesUp(t *testing.T) {
	r := newFakeRunner().on("ssh-add", agentEmpty()).on("ssh-keygen", keygen("SHA256:NEW"))
	secret := &fakeSecret{lookupPass: "wrong", lookupFound: true}
	adder := &fakeKeyAdder{withCodes: []int{1, 1, 1}}
	log := &fakeLogger{}
	l := Loader{
		Keys:   fakeLister{paths: []string{"/ssh/id_rsa"}},
		Runner: r, Secret: secret, Prompt: &fakePrompter{}, Adder: adder, Log: log,
		Config: Config{GUI: true},
	}
	if err := l.LoadKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adder.calls) != 3 {
		t.Fatalf("want 3 attempts, got %d", len(adder.calls))
	}
	if !log.contains("attempt 3/3") {
		t.Fatalf("expected a final failed-attempt log, got %v", log.lines)
	}
}

func TestLoadKeysPromptCanceled(t *testing.T) {
	r := newFakeRunner().on("ssh-add", agentEmpty()).on("ssh-keygen", keygen("SHA256:NEW"))
	secret := &fakeSecret{lookupFound: false}
	prompter := &fakePrompter{err: ErrPromptCanceled}
	adder := &fakeKeyAdder{}
	log := &fakeLogger{}
	l := Loader{
		Keys:   fakeLister{paths: []string{"/ssh/id_rsa"}},
		Runner: r, Secret: secret, Prompt: prompter, Adder: adder, Log: log,
		Config: Config{GUI: true},
	}
	if err := l.LoadKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adder.calls) != 0 {
		t.Fatalf("a canceled prompt must not add, got %d", len(adder.calls))
	}
	if !log.contains("canceled") {
		t.Fatalf("expected a canceled log, got %v", log.lines)
	}
}

func TestLoadKeysNoGUITerminalPath(t *testing.T) {
	r := newFakeRunner().on("ssh-add", agentEmpty()).on("ssh-keygen", keygen("SHA256:NEW"))
	adder := &fakeKeyAdder{intCodes: []int{0}}
	log := &fakeLogger{}
	l := Loader{
		Keys:   fakeLister{paths: []string{"/ssh/id_rsa"}},
		Runner: r, Adder: adder, Log: log,
		Config: Config{GUI: false},
	}
	if err := l.LoadKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adder.calls) != 1 || !adder.calls[0].interactive {
		t.Fatalf("calls = %+v, want one interactive add", adder.calls)
	}
	if !log.contains("no GUI") || !log.contains("added id_rsa") {
		t.Fatalf("expected no-GUI + added logs, got %v", log.lines)
	}
}

func TestLoadKeysSkipsGivenUp(t *testing.T) {
	r := newFakeRunner().on("ssh-add", agentEmpty()).on("ssh-keygen", keygen("SHA256:NEW"))
	adder := &fakeKeyAdder{}
	give := newFakeGiveup()
	give.given["id_rsa"] = true
	log := &fakeLogger{}
	l := Loader{
		Keys:   fakeLister{paths: []string{"/ssh/id_rsa"}},
		Runner: r, Secret: &fakeSecret{}, Prompt: &fakePrompter{}, Adder: adder, Log: log,
		Giveup: give, Config: Config{GUI: true},
	}
	if err := l.LoadKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adder.calls) != 0 {
		t.Fatalf("a given-up key must not be added, got %d adds", len(adder.calls))
	}
	if !log.contains("given up earlier") {
		t.Fatalf("expected a skip log, got %v", log.lines)
	}
}

func TestLoadKeysRecordsGiveupAfterRetries(t *testing.T) {
	r := newFakeRunner().on("ssh-add", agentEmpty()).on("ssh-keygen", keygen("SHA256:NEW"))
	secret := &fakeSecret{lookupPass: "wrong", lookupFound: true}
	adder := &fakeKeyAdder{withCodes: []int{1, 1, 1}}
	give := newFakeGiveup()
	l := Loader{
		Keys:   fakeLister{paths: []string{"/ssh/id_rsa"}},
		Runner: r, Secret: secret, Prompt: &fakePrompter{}, Adder: adder, Log: &fakeLogger{},
		Giveup: give, Config: Config{GUI: true},
	}
	if err := l.LoadKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(give.recorded) != 1 || give.recorded[0] != "id_rsa" {
		t.Fatalf("recorded = %v, want [id_rsa]", give.recorded)
	}
}

func TestLoadKeysClearsGiveupOnSuccess(t *testing.T) {
	r := newFakeRunner().on("ssh-add", agentEmpty()).on("ssh-keygen", keygen("SHA256:NEW"))
	secret := &fakeSecret{lookupPass: "ok", lookupFound: true}
	adder := &fakeKeyAdder{withCodes: []int{0}}
	give := newFakeGiveup()
	l := Loader{
		Keys:   fakeLister{paths: []string{"/ssh/id_rsa"}},
		Runner: r, Secret: secret, Prompt: &fakePrompter{}, Adder: adder, Log: &fakeLogger{},
		Giveup: give, Config: Config{GUI: true},
	}
	if err := l.LoadKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(give.cleared) != 1 || give.cleared[0] != "id_rsa" {
		t.Fatalf("cleared = %v, want [id_rsa]", give.cleared)
	}
}

func TestLoadKeysNoKeys(t *testing.T) {
	r := newFakeRunner() // ssh-add must not be consulted
	log := &fakeLogger{}
	l := Loader{Keys: fakeLister{paths: nil}, Runner: r, Log: log}
	if err := l.LoadKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !log.contains("no keys") {
		t.Fatalf("expected a no-keys log, got %v", log.lines)
	}
	if len(r.calls) != 0 {
		t.Fatalf("the agent must not be queried with no keys, got %v", r.calls)
	}
}

func TestLoadKeysEnumerateError(t *testing.T) {
	l := Loader{Keys: fakeLister{err: errors.New("readdir boom")}, Runner: newFakeRunner()}
	if err := l.LoadKeys(); err == nil {
		t.Fatal("expected an error when enumeration fails")
	}
}

func TestLoadKeysAgentSnapshotError(t *testing.T) {
	r := newFakeRunner().on("ssh-add", fails(errors.New("no ssh-add")))
	l := Loader{Keys: fakeLister{paths: []string{"/ssh/id_rsa"}}, Runner: r}
	if err := l.LoadKeys(); err == nil {
		t.Fatal("expected an error when the agent snapshot fails")
	}
}
