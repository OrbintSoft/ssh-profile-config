# sshepherd — Design notes & lessons learned

Living document. Captures how this thing actually behaves on the target box,
the June 2026 breakage and its root cause, and the principles we want the
**rewrite** to follow so it stays resilient across OpenSSH / Plasma / Gentoo
updates.

---

## 1. What the project is for

On login, make every interactive shell able to use SSH non-interactively:

1. Ensure an `ssh-agent` is running and reachable for the whole desktop session.
2. Load the user's private key(s) into it.
3. Pull the key passphrase from the secret store (KWallet via `secret-tool`),
   prompting only the first time, then caching it so future logins are silent.

Two scripts + a makefile:

- `nn-ssh-init-linux.sh` → installed to `/etc/profile.d/<NN>-ssh-init.sh`
  (sourced by login shells; agent setup runs always, key-loading only when
  the shell is interactive).
- `ssh-ask-pass-linux.sh` → installed to `$BINDIR/ssh-ask-pass.sh`; used as
  `SSH_ASKPASS` to feed the passphrase to `ssh-add` out of a short-lived
  `keyctl` entry.
- `ssh-init-macos.zsh` → macOS counterpart (uses the system keychain).

### Install convention on the target box
```
sudo make install PREFIX=/usr        # -> /etc/profile.d/001-ssh-init.sh
                                     #    /usr/bin/ssh-ask-pass.sh
```
The makefile `sed`s the literal placeholder `/usr/local/bin/ssh-ask-pass.sh`
(the `ssh_askpass_script=` line) to `$BINDIR/ssh-ask-pass.sh` at install time —
**keep that placeholder literal in the source.**

---

## 2. Target environment (verified June 2026)

| Thing | Value |
|---|---|
| Distro | Gentoo |
| Desktop | KDE Plasma 6.6.5, **X11** session (not Wayland) |
| Shell | bash |
| OpenSSH | **10.3_p1** |
| `DISPLAY` | `:0`; `xset q` works → GUI branch is taken |
| Secret store | `secret-tool` (KWallet/libsecret), `kdialog` for prompts |
| `keyctl`, `uuidgen`, `setsid`, `flock` | all present (util-linux / keyutils) |
| `~/.ssh/config` | `AddKeysToAgent yes`, `IdentityFile ~/.ssh/id_ed25519_github` |
| Key | `id_ed25519` (fingerprint redacted) |

Quirks that matter:

- **`ksshaskpass` is masked.** Stefano ships an empty mock ebuild
  (`ksshaskpass-99999999.ebuild` → installs nothing, so `/usr/bin/ksshaskpass`
  is absent) so KDE's askpass does not hijack his own ssh-add flow.
- **Plasma does *not* start the agent.** `/etc/xdg/plasma-workspace/env/10-agent-startup.sh`
  has `SSH_AGENT` commented out, and the user override dir
  `~/.config/plasma-workspace/env/` is empty. The agent the session ends up
  with is the one **this script** starts at login.
- `gcr-ssh-agent` exists under `/usr/libexec` but is **not** running and **not**
  wired via systemd user units. No `gcr/ssh` socket. Not a factor here.

---

## 3. The June 2026 breakage — root cause

Symptom: "every new terminal can no longer load the key / remember the
passphrase." In reality the key *was* loaded in a live agent, but the session
pointed at a **dead** one.

### 3a. OpenSSH 10.x moved the agent socket (the "update")
`ssh-agent` no longer defaults to `/tmp/ssh-XXXXXX/agent.PID`. When
`~/.ssh/agent/` exists it now creates the socket **there**, named:

```
~/.ssh/agent/s.<base>.agent.<random>
```

Confirmed from the binary strings (`/usr/bin/ssh-agent`, OpenSSH 10.3_p1):
```
%s/%s/s.%s.%s.XXXXXXXXXX     ->  <dir>/s.<base>.agent.<random>
/tmp/ssh-XXXXXXXXXXXX        ->  old default
%s/agent.%ld                ->  old "agent.<pid>"
.ssh/agent
```
- `<base>` is **stable per session** (e.g. `etBPaPPmha`).
- `<random>` (the `XXXXXXXXXX`) is **fresh on every `ssh-agent` start**.

Net effect: **the socket path changes every time the agent is (re)started.**
Ironically, the `~/.ssh/agent/` directory was created by us (the old
"fix ssh agent location" commit) — which is exactly what makes new OpenSSH
use that location.

The `ssh_add.sessions` history shows the transition: old rows are
`/tmp/ssh-XXX/agent.PID`, new rows are `~/.ssh/agent/s.*`.

### 3b. The script killed a healthy agent (the bug that pulled the trigger)
The "unresponsive agent" check (recent addition) did:
```bash
if ! timeout 2 ssh-add -l > /dev/null 2>&1; then
    agent_exit_code=$?          # BUG A
    ...
    kill "$SSH_AGENT_PID"; eval "$(ssh-agent -s)"   # BUG B
```
- **BUG A:** after `if ! cmd`, `$?` inside the `then` is the status of the
  *negation* (always `0`), not `ssh-add`'s real code. That's why the log said
  `returned unexpected exit code 0` and the timeout (124) branch was dead code.
- **BUG B:** it killed the agent on **any** non-zero `ssh-add -l`. But:

  | `ssh-add -l` | meaning | healthy? |
  |---|---|---|
  | 0 | agent has keys | yes |
  | **1** | **agent reachable, no keys yet** | **YES** |
  | 2 | cannot connect to agent | no |
  | 124 | timed out (`timeout`) | no |

  A freshly started agent at login has **no keys yet → exit 1** → the script
  killed the brand-new, perfectly good session agent and started a replacement
  with a *different* random socket.

### 3c. Why the session stays broken
The agent env (`SSH_AUTH_SOCK`, `SSH_AGENT_PID`) is exported **once** by the
login shell and inherited by `plasmashell` and every terminal/GUI app spawned
later. `export` in a per-terminal run of the script **cannot** update the
already-running session. So once the original agent is killed:
- `plasmashell` (PID 3782) kept `SSH_AUTH_SOCK=…/s.etBPaPPmha.agent.zJ4KVJ61LH`
  + `SSH_AGENT_PID=3507` — both **dead**.
- the live agent (4009, socket `…LmxvYpIpZI`, with the key) was **orphaned**
  under `init` and unknown to the session.
- only fresh *login* shells that re-ran the whole script recovered, and only
  for themselves → flaky, "sometimes works" behaviour.

### One-line summary
Not the secret store. The script kept **killing the session's good agent**
(misreading "reachable but empty" as dead), and because the new OpenSSH socket
path is random, the replacement was unreachable from the already-started
session.

---

## 4. The fix shipped (current `nn-ssh-init-linux.sh`)

Principles already applied — keep them in the rewrite:

1. **Fixed socket path.** Pin the agent to `~/.ssh/agent/ssh-agent.sock` via
   `ssh-agent -a`. The value exported into the session never goes stale, even
   if the agent is restarted at the same path.
2. **Never kill a healthy agent.** Treat `ssh-add -l` exit 0 **and 1** as
   healthy. Only (re)start when the fixed socket is genuinely unreachable
   (no socket / exit 2 / timeout 124). Capture the exit code on its own line,
   not via `$?` after `!`.
3. **No growing state file.** Decide "already loaded?" by comparing the key
   fingerprint (`ssh-keygen -lf`) against `ssh-add -l`. Dropped
   `ssh_add.sessions` (it had grown to 120+ dead rows).
4. **Login-burst safety.** `flock` around the start so simultaneous shells
   don't race to create the agent.

Immediate cleanup done for the running session: symlinked the dead socket to
the live one so already-open GUI apps recovered without a relogin; archived the
old `ssh_add.sessions`.

---

## 5. Mechanisms to preserve in the rewrite

- **Passphrase handoff (don't break this — it works):**
  `secret-tool lookup service "SSH-Key-<keyname>" username "$USER"` →
  store the passphrase in a short-lived `keyctl` entry (60s timeout) →
  `SSH_TEMP_KEYCTL=… SSH_ASKPASS=…/ssh-ask-pass.sh setsid timeout 60 ssh-add key </dev/null`.
  `ssh-ask-pass.sh` `keyctl print`s it, unlinks it, echoes it. On first-time
  success, `secret-tool store` persists it. **Do not rename the wallet service
  key** (`SSH-Key-<keyname>`) or stored passphrases stop matching.
- **GUI detection:** GUI branch requires `$DISPLAY` and `xset q` succeeding.
  Note this is **X11-only**; under Wayland `DISPLAY`/`xset` may be absent →
  falls back to the terminal-prompt branch. If the box ever moves to Wayland,
  detection must change (e.g. `$WAYLAND_DISPLAY`, or just probe `secret-tool`/
  `kdialog` directly instead of `xset`).
- **`setsid … < /dev/null`** is what makes `ssh-add` use `SSH_ASKPASS` instead
  of the tty. Consider `SSH_ASKPASS_REQUIRE=force` (OpenSSH ≥ 8.4) as a more
  explicit alternative.

---

## 6. Rewrite goals / open decisions

- [ ] **Agent ownership.** Either (a) this script owns one fixed-socket agent
  (current approach), or (b) fully delegate to a session-managed agent
  (`gcr-ssh-agent` / Plasma) and only load keys. Pick one and don't fight the
  other. (a) is simpler and what's shipped.
- [ ] **Where to bootstrap.** profile.d (login shell) works today, but
  `~/.config/plasma-workspace/env/*.sh` is sourced by `startplasma` and is
  arguably the more correct, update-proof place to set the session-wide
  `SSH_AUTH_SOCK`. Keep key-loading in profile.d (needs interactivity).
- [ ] **Idempotency / concurrency.** Make every run safe to repeat; keep the
  `flock`. Consider cleaning **dangling** sockets/symlinks in `~/.ssh/agent/`
  opportunistically (only ones whose agent is unreachable).
- [ ] **Multi-key support.** Loop already handles `id_*`; make sure fingerprint
  dedup and per-key wallet entries scale.
- [ ] **Observability.** Keep the capped log (`sessions.log`, 100 lines). Maybe
  add a `--status` / dry-run mode for debugging.
- [ ] **Portability.** Don't hard-depend on `keyctl` (Linux-only) or `flock`;
  degrade gracefully if absent.
- [ ] **Shell safety.** Consider `set -u`-clean code; avoid `find | while`
  subshell gotchas if state needs to escape the loop.

---

## 7. Testing checklist (post-change)

1. Fresh logout/login → open 2 terminals → `ssh-add -l` shows the key in both,
   **no** passphrase prompt the 2nd time.
2. `echo $SSH_AUTH_SOCK` is `~/.ssh/agent/ssh-agent.sock` in every terminal and
   in a GUI app (e.g. check `/proc/$(pgrep -x plasmashell)/environ`).
3. Kill the agent (`ssh-agent -k` / `pkill ssh-agent`) → open a new terminal →
   it restarts at the **same** socket path and reloads the key.
4. First-ever run with an empty wallet → prompts once via `kdialog`, then
   `secret-tool lookup` returns it on subsequent logins.
5. Simulate "reachable but empty": start `ssh-agent -a <sock>` with no keys →
   confirm the script does **not** kill it (exit 1 must be treated as healthy).

---

## 8. Diagnostic one-liners (kept from the investigation)

```bash
# What the whole session thinks (this is what new terminals inherit):
tr '\0' '\n' < /proc/$(pgrep -x plasmashell)/environ | grep -i ssh

# Running agents and their sockets:
pgrep -au "$USER" -x ssh-agent
ls -l ~/.ssh/agent/

# Is a given socket alive and does it hold the key?
SSH_AUTH_SOCK=<sock> ssh-add -l ; echo "rc=$?"   # 0 keys / 1 empty / 2 dead

# What does ssh-agent actually produce here (socket location/naming)?
ssh-agent -s    # then kill the test pid

# Confirm OpenSSH's socket naming in the binary:
strings /usr/bin/ssh-agent | grep -E '\.ssh/agent|s\.%|/tmp/ssh-'

# Session/GUI detection inputs:
echo "$XDG_SESSION_TYPE $DISPLAY $WAYLAND_DISPLAY"; xset q >/dev/null 2>&1; echo $?
```
