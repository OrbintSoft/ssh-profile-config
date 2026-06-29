package keys

import "fmt"

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

// fails builds a handler that reports a failure to start the process.
func fails(err error) func(Cmd) (Result, error) {
	return func(Cmd) (Result, error) { return Result{}, err }
}
