// Package keys loads the user's SSH keys into the agent: it enumerates the
// private keys under ~/.ssh, skips any whose fingerprint is already in the agent,
// and adds the rest, pulling each passphrase from the OS secret store and handing
// it to ssh-add out of band. It never reimplements ssh-add or ssh-keygen — it
// drives the OpenSSH tools and the secret store through the seams below.
package keys

// EnvAskpassMode marks an invocation as ssh-add's SSH_ASKPASS helper. The loader
// sets it (to "1") in the ssh-add child environment and points SSH_ASKPASS at the
// sshakku binary itself; the binary, seeing the marker, returns the passphrase
// instead of dispatching a subcommand — ssh-add execs SSH_ASKPASS as a single
// program with only the prompt as an argument, so a "sshakku askpass" command line
// is not an option.
const EnvAskpassMode = "SSHAKKU_ASKPASS"

// EnvKeyctlSerial names the environment variable carrying the kernel-keyring
// serial of a passphrase entry from the loader to the askpass helper. Only the
// serial — a handle — crosses the env; the passphrase itself stays in the keyring.
const EnvKeyctlSerial = "SSHAKKU_KEYCTL_SERIAL"

// Cmd describes one external command invocation. Env entries are appended to the
// current environment; Stdin, when non-empty, is fed to the process — the way a
// passphrase reaches secret-tool without ever appearing in argv.
type Cmd struct {
	Name  string
	Args  []string
	Stdin string
	Env   []string
}

// Result is the outcome of running a Cmd. A non-zero Code is reported here, not
// as an error: callers distinguish meaningful exit codes (e.g. ssh-add -l exits 1
// for an empty agent, secret-tool exits non-zero for a miss) from a failure to
// start the process, which is returned as the error.
type Result struct {
	Stdout []byte
	Stderr []byte
	Code   int
}

// Runner runs an external command and returns its result. It is the seam that
// lets the loader be tested without spawning real ssh-add/ssh-keygen/secret-tool.
type Runner interface {
	Run(Cmd) (Result, error)
}
