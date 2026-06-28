// Command sshepherd tends the SSH agent: it computes the per-user runtime
// paths, keeps the agent healthy, and loads keys with passphrases pulled from
// the OS secret store. The login shell wires it in by evaluating its output:
//
//	eval "$(sshepherd shell-init)"
package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `sshepherd — SSH agent and key shepherd

usage: sshepherd <command>

commands:
  shell-init   print shell assignments for the login entrypoint to eval
  help         show this help
`

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

// run dispatches a subcommand and returns the process exit code. Output goes to
// the supplied writers so the command is testable without touching real stdio.
func run(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "shell-init":
		return shellInit(stdout, stderr)
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "sshepherd: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// shellInit computes the runtime paths and prints them as shell assignments for
// the login entrypoint to eval. The path, keyring-token and log logic lands in
// the following sub-steps; for now it is a stub that emits nothing.
func shellInit(_, _ io.Writer) int {
	return 0
}
