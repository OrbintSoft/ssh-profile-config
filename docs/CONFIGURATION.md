# Configuration

SSHakku reads its settings from environment variables. Set them before the login
hook runs (for example in `/etc/profile.d` or your shell profile) so `sshakku
load-keys` and the askpass broker see them.

## Environment variables

| Variable | Default | Effect |
| --- | --- | --- |
| `SSHAKKU_KEY_LIFETIME` | `8h` | How long an added key stays in the agent before it expires, as a Go duration (`30m`, `1h`, `8h`). Passed to `ssh-add -t`. A zero or negative value (`0`) disables expiry, so the key stays until the agent does. |
| `SSHAKKU_MAX_ATTEMPTS` | `3` | How many passphrase attempts to make per key before giving up. Values below `1` fall back to the default. |
| `SSHAKKU_GIVEUP_TTL` | `1h` | How long a key stays in the give-up state before it is retried, as a Go duration. A zero or negative value never expires (the state still clears at logout or reboot). |
| `SSHAKKU_NO_GIVEUP` | unset | When truthy, disables the give-up memory entirely: every shell retries every key. |
| `SSHAKKU_QUIET` | unset | When truthy, suppresses the user-facing failure notice on the terminal. |

Truthy means `1`, `true`, `yes`, or `on` (case-insensitive). A malformed duration
is ignored, logged to the session log, and the default is used.

## Key expiry and the wallet

Keys are added to the agent with a lifetime (`SSHAKKU_KEY_LIFETIME`, default 8h).
When that elapses the agent drops the key; the passphrase stays in the OS wallet,
so re-adding the key never asks you to retype it.

- **Opening a new terminal** re-adds any expired key automatically: SSHakku sees
  the fingerprint is no longer in the agent and re-adds it from the wallet,
  silently. Because every shell shares one agent on a fixed socket, this refills
  the key for all terminals at once.
- **In a still-open terminal** where a key just expired, the next `ssh` (or
  `git`, `rsync`, or any program that uses ssh) is routed through SSHakku's
  askpass broker. The broker fetches the passphrase from the wallet and hands it
  to ssh without prompting on the terminal. Only if the wallet entry is missing,
  the wallet does not exist, or wallet access fails does it fall back to prompting
  on the terminal — and a passphrase typed at that fallback is then stored in the
  wallet for next time.

The askpass routing is enabled only when a graphical secret prompter is available.
A headless session keeps ssh's own terminal prompting, and non-interactive
sessions (such as `scp`, `rsync`, or `git` in scripts) are never touched.

A short lifetime keeps the window in which a key sits in the agent small. Because
the wallet refills the key silently, you can keep that window short without ever
retyping a passphrase from memory — which also makes rotating keys cheaper.

## Retries and giving up

A wrong passphrase is retried up to `SSHAKKU_MAX_ATTEMPTS` times. On the graphical
path a stored passphrase that ssh-add rejects is treated as stale: SSHakku prompts
once and, on success, replaces it in the wallet.

When the attempts are exhausted, SSHakku gives up on that key and notifies you on
the terminal (unless `SSHAKKU_QUIET`). It then skips the key in every new shell for
`SSHAKKU_GIVEUP_TTL`, so a misconfigured key does not re-prompt on every terminal
you open. A later successful load clears the give-up state. The state is per-login
and lives in tmpfs, so logging out or rebooting clears it; `SSHAKKU_NO_GIVEUP`
disables it entirely.
