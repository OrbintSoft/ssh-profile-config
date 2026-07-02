# Diagnostics

SSHakku includes a diagnostic command that reports the state of your SSH agent
and, on request, repairs it.

## `sshakku doctor`

Run `sshakku doctor` to print a read-only report of the current ssh-agent
situation. It changes nothing. The report covers:

- **State** — the agent lifecycle state, one of:
  - **A — clean**: no agent is running.
  - **B — ours healthy**: sshakku's agent is answering on the fixed socket.
  - **C — ours zombie**: only dead remnants of our agent remain.
  - **D — foreign agent serving**: a single agent sshakku did not start is
    answering.
  - **E — disaster**: several agents are answering at once.
- **Sockets** — the fixed socket sshakku uses, and this shell's `SSH_AUTH_SOCK`
  with whether it is reachable.
- **Processes** — every `ssh-agent` process found, each labelled *ours*,
  *legacy*, or *foreign*, with its reachability, owning user, socket, and the
  process chain that launched it.
- **Findings** — plain-language observations (a stale environment, dead agents
  lingering, a foreign agent answering, and so on).
- **Recommendation** — what to do about the current state.
- **Recent log** — the tail of the session log.

### Attribution

For each agent the report shows the process chain up toward `init` and names the
launcher when it recognises one (systemd, the KDE Plasma or GNOME session, a
display manager, an SSH login, a login shell). Because `ssh-agent` daemonizes and
is then reparented to `init`, the original launcher is often no longer in the
process tree; the report says so rather than guessing.

## `sshakku doctor --fix`

`sshakku doctor --fix` first prints the diagnosis, then applies the same
self-heal the login path runs: it reaps dead agents and their stale sockets,
starts a fresh agent on the fixed socket, or adopts a healthy agent started by
something else. A healthy agent is never killed. It then re-reports the result.

A running program cannot change the environment of the shell that started it, so
`--fix` cannot rewrite the current shell's `SSH_AUTH_SOCK`. When the shell still
points somewhere other than the healed socket, the command prints an
`export SSH_AUTH_SOCK=…` line to run — or you can simply open a new shell.

## Scope

`doctor` inspects the invoking user's session, and `--fix` acts as that user: it
never escalates privileges, so it reaps only your own dead agents. Run it as the
user whose agent you are diagnosing.
