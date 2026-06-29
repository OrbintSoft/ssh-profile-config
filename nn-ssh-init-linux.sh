#!/bin/bash

# SSH bootstrap, sourced from /etc/profile.d.
#
# This is a thin hook around the sshakku core. `sshakku shell-init`, evaluated
# below, keeps an ssh-agent healthy on a fixed socket and prints the runtime
# paths to use; the fixed socket means the SSH_AUTH_SOCK we export never goes
# stale even if the agent is restarted. In interactive shells `sshakku load-keys`
# then adds the user's keys, pulling each passphrase from the OS secret store and
# skipping any key already in the agent. All the logic lives in the core; this
# script only pins the shell to the socket and invokes it.

sshakku_bin="/usr/local/bin/sshakku"

# Resolve the runtime paths, keep the agent healthy, and print the shell
# assignments to eval. Declare them first so an absent or failing binary leaves
# them empty rather than unset.
agent_sock=""
log_file=""
if [ -x "$sshakku_bin" ]; then
	eval "$("$sshakku_bin" shell-init)"
fi
# Without the resolved paths there is nothing we can safely do.
[ -n "$agent_sock" ] && [ -n "$log_file" ] || return

# Always pin this shell -- and, at login, the whole session -- to the fixed path.
export SSH_AUTH_SOCK="$agent_sock"
unset SSH_AGENT_PID

# Load keys only in interactive shells: key loading may prompt and writes to the
# terminal, which must never happen for non-interactive sessions (scp/rsync/git).
if [[ $- == *i* ]]; then
	"$sshakku_bin" load-keys
	# Route this shell's ssh passphrase prompts through sshakku's wallet-aware
	# askpass, so a key that expires from the agent is refilled from the wallet
	# without a terminal prompt. Emitted only when a graphical prompter exists.
	eval "$("$sshakku_bin" askpass-env)"
fi
