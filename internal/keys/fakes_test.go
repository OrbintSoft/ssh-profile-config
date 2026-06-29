package keys

import (
	"fmt"
	"strings"
)

// fakeRunner answers Run from a per-binary handler table, so a test can stub
// ssh-keygen, ssh-add, secret-tool, etc. independently and inspect the calls.
type fakeRunner struct {
	handlers map[string]func(Cmd) (Result, error)
	calls    []Cmd
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{handlers: make(map[string]func(Cmd) (Result, error))}
}

// on registers a handler for a command name.
func (f *fakeRunner) on(name string, h func(Cmd) (Result, error)) *fakeRunner {
	f.handlers[name] = h
	return f
}

func (f *fakeRunner) Run(c Cmd) (Result, error) {
	f.calls = append(f.calls, c)
	if h, ok := f.handlers[c.Name]; ok {
		return h(c)
	}
	return Result{}, fmt.Errorf("unexpected command %q", c.Name)
}

// stdout builds a handler that returns out on stdout with the given exit code.
func stdout(out string, code int) func(Cmd) (Result, error) {
	return func(Cmd) (Result, error) {
		return Result{Stdout: []byte(out), Code: code}, nil
	}
}

// fakePrompter is a Prompter whose availability and answer are scripted.
type fakePrompter struct {
	avail bool
	pass  string
	err   error
	calls []string
}

func (p *fakePrompter) Available() bool { return p.avail }

func (p *fakePrompter) Prompt(keyname string) (string, error) {
	p.calls = append(p.calls, keyname)
	return p.pass, p.err
}

// fakeLister returns a fixed list of key paths (or an error).
type fakeLister struct {
	paths []string
	err   error
}

func (l fakeLister) Keys() ([]string, error) { return l.paths, l.err }

// fakeSecret is a scripted SecretBackend that records every Store.
type fakeSecret struct {
	lookupPass  string
	lookupFound bool
	lookupErr   error
	storeErr    error
	stored      []storeCall
}

type storeCall struct{ service, label, passphrase string }

func (s *fakeSecret) Lookup(string) (string, bool, error) {
	return s.lookupPass, s.lookupFound, s.lookupErr
}

func (s *fakeSecret) Store(service, label, passphrase string) error {
	if s.storeErr != nil {
		return s.storeErr
	}
	s.stored = append(s.stored, storeCall{service, label, passphrase})
	return nil
}

// fakeKeyAdder records each add and returns scripted exit codes per call.
type fakeKeyAdder struct {
	withCodes []int // exit codes for successive AddWithAskpass calls
	intCodes  []int // exit codes for successive AddInteractive calls
	err       error
	calls     []addCall
}

type addCall struct {
	keyfile     string
	passphrase  string
	interactive bool
}

func (a *fakeKeyAdder) AddWithAskpass(keyfile, passphrase string) (int, error) {
	a.calls = append(a.calls, addCall{keyfile: keyfile, passphrase: passphrase})
	if a.err != nil {
		return 0, a.err
	}
	return popCode(&a.withCodes), nil
}

func (a *fakeKeyAdder) AddInteractive(keyfile string) (int, error) {
	a.calls = append(a.calls, addCall{keyfile: keyfile, interactive: true})
	if a.err != nil {
		return 0, a.err
	}
	return popCode(&a.intCodes), nil
}

// popCode returns and removes the first code, defaulting to 0 when exhausted.
func popCode(codes *[]int) int {
	if len(*codes) == 0 {
		return 0
	}
	c := (*codes)[0]
	*codes = (*codes)[1:]
	return c
}

// fakeLogger records the level-tagged lines a Loader emits.
type fakeLogger struct{ lines []string }

func (f *fakeLogger) Log(level, message string) error {
	f.lines = append(f.lines, level+" "+message)
	return nil
}

func (f *fakeLogger) contains(sub string) bool {
	for _, l := range f.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// fails builds a handler that reports a failure to start the process.
func fails(err error) func(Cmd) (Result, error) {
	return func(Cmd) (Result, error) { return Result{}, err }
}

// fakeGiveup is an in-memory GiveupStore that scripts GivenUp and records the
// keys passed to Record and Clear.
type fakeGiveup struct {
	given    map[string]bool
	recorded []string
	cleared  []string
}

func newFakeGiveup() *fakeGiveup { return &fakeGiveup{given: map[string]bool{}} }

func (g *fakeGiveup) GivenUp(key string) bool { return g.given[key] }

func (g *fakeGiveup) Record(key string) error {
	g.recorded = append(g.recorded, key)
	g.given[key] = true
	return nil
}

func (g *fakeGiveup) Clear(key string) error {
	g.cleared = append(g.cleared, key)
	delete(g.given, key)
	return nil
}
