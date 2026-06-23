# ssh-profile-config — PLAN

Roadmap for the rewrite. We fix the **goals** first; the **phases** come after the
goals are reviewed and agreed. See `CLAUDE.md` for the project rules and
`docs/DESIGN-NOTES.md` for background and the June 2026 incident that motivated
the rewrite.

---

## Goals

Authoritative list of what the rewrite must achieve. "(open: …)" marks a decision
still to be made.

### Core behaviour

1. **SSH always ready on terminal open, without re-typing the passphrase.** The
   original reason the project exists. The stock approach (login init +
   `ssh-askpass`) is rejected as too fragile: it breaks often and each breakage
   costs time to diagnose. This project does not claim to be better in principle,
   but it performs explicit checks, reasons about the problem, and writes a
   detailed log.

2. **Security: the passphrase lives in a secure vault and never transits an
   environment variable** (where it could leak into a log or elsewhere). Only the
   key id is passed around; the passphrase is handed over out of band (today: a
   short-lived `keyctl` entry) and stored in a secret store (today: KDE Wallet).
   Planned extension: the loaded key **expires** after a configurable lifetime
   (e.g. 20 min / 1 h / 4 h), and simply opening a new shell silently re-activates
   it from the vault. (open: expire the *key in the agent* vs the *stored
   passphrase* — intended meaning is key-in-agent expiry, passphrase stays in the
   vault.)

3. **Silent on success.** When everything is fine the script prints nothing to
   stdout/stderr — no spam, no interference with other commands.

4. **Bounded retries, no loops.** It may retry, but after N attempts (say 3) it
   gives up and must not keep spamming in every shell. (open: also limit over time
   / reset at next login; ideally provide an opt-out.)

5. **No SSH keys → no breakage.** With nothing to load, the script must still exit
   cleanly.

6. **Best-effort recovery.** An SSH session already started by something else is
   fine — at most we load the keys that are missing. If a socket is up but the
   environment variables don't match, fix them as far as possible. (Note the hard
   limit: a child process cannot rewrite the environment of an already-running
   parent such as the session/GUI; the fixed-socket approach is what makes this
   robust.)

7. **No database — plain text files only. No secrets or otherwise sensitive
   information in logs.**

### Diagnostics

8. **A diagnostic tool (currently missing).** Reports problems: who started the
   ssh socket, why it isn't working, which processes are involved, etc. It can be
   run with `sudo` to have the privileges needed to inspect the full picture.

### Portability

9. **Work without a graphical environment, and under Wayland** (not only X11).

10. **Primary target: Gentoo Linux with OpenRC and KDE.** It must work here first.

11. **Adaptable to other Linux systems:** Gentoo with systemd; other distributions
    with other desktops such as GNOME and its keyring. The passphrase store must be
    pluggable — beyond KDE Wallet and the GNOME equivalent, support e.g. 1Password.

12. **Secondary target: macOS** (zsh, Keychain or 1Password).

13. **Later: Windows.** First under bash, then PowerShell (open: module vs profile
    vs other). Credential Manager or 1Password.

### Engineering

14. **Move logic out of pure bash into a more maintainable, testable,
    cross-platform language, minimizing duplication.** A lot of shell glue will
    remain, but the core logic should not live in bash. Candidate: Go. (The
    login-shell entrypoint is necessarily a thin shell layer; keep it minimal.)

15. **Highly parametrizable and configurable.**

16. **Maximally testable:** unit tests, plus integration tests in containers at
    least on Linux. macOS/Windows to be decided — Windows containers exist, macOS
    is unclear; possibly Vagrant, otherwise CI runners, or best-effort on macOS.

### Installation & filesystem

17. **Two installation modes.** *System-wide* (requires `sudo`, as today:
    `/etc/profile.d`, `$BINDIR`) **or** *per-user* (no root, everything under
    `$HOME`). The same logic must work in both; only the paths and the bootstrap
    hook differ.

18. **Least-privilege execution.** Executables/scripts run with the privileges of
    the user who opens the terminal — never escalate. The only exception is the
    diagnostic tool (goal 8), which may be run with `sudo` on demand to inspect the
    full picture.

19. **Standard file locations, outside `~/.ssh`.** Config in `/etc/<name>/` (system)
    or `$XDG_CONFIG_HOME` (per-user); logs/state in `$XDG_STATE_HOME`; the agent
    socket in `$XDG_RUNTIME_DIR` (per-user, mode 0700) — all with correct
    ownership/permissions. Never store our own files under `~/.ssh`: it is reserved
    for OpenSSH and, as the June 2026 incident showed, creating `~/.ssh/agent/` is
    precisely what makes OpenSSH 10.x relocate the socket to a random path.

---

## Open decisions

Points raised during goal review that need a decision (or an explicit constraint
honoured) before or during the phases. Each notes the related goal.

1. **Threat model (goal 2, 7).** State, in two lines, what the secret handling
   protects against and what it does not. The user keyring (`keyctl @u`) and the
   secret store do **not** protect against other processes of the same user — by
   design, since those processes must be able to use the key. Decide the target
   (other local users / root / swap & coredumps / logs) — it drives the design.
   **Decided (Phase 0) — threat model (two lines):**
   *Protects* the passphrase from logs, shell history, `argv`
   (`ps` / `/proc/<pid>/cmdline`) and plaintext on disk — at rest it lives only in
   the OS secret store, in transit only via a short-lived `keyctl` entry / stdin.
   *Does not* defend against other same-user processes or root (both can already use
   the key in the agent, by design); swap/coredump hardening is out of scope.

2. **No passphrase in `argv` (goal 2).** Never pass the passphrase as a
   command-line argument (visible via `ps` / `/proc/<pid>/cmdline`). Feed it
   through stdin instead (e.g. `keyctl padd … <<<"$passphrase"`). Audit every tool
   invocation that touches the passphrase.

3. **"Silent" means zero stdout/stderr when non-interactive (goal 3).** Anything
   sourced from `profile.d` runs for non-interactive SSH sessions too; a single
   byte on stdout corrupts `scp` / `rsync` / `git`-over-ssh. The success path must
   emit nothing on stdout/stderr — only the log file.

4. **Recovery has a hard limit (goal 6).** A child process cannot rewrite the
   environment of an already-running parent (the session / GUI). "Fix mismatched
   env vars" can only fix the current shell and its descendants; already-open GUI
   apps are reachable only via the fixed socket path (plus a dangling-socket
   symlink as a last resort). Don't promise more.

5. **Give-up state & opt-out (goal 4).** Bounded retries need a persistent text
   sentinel ("gave up on key X") with a defined reset (next login? time-based?) and
   an opt-out switch (config flag / sentinel file). Define lifetime and reset rule.

6. **Key-expiry semantics (goal 2).** Confirm: expire the *key inside the agent*
   (`ssh-add -t <lifetime>`), keep the passphrase in the vault, and let a new shell
   re-add it silently — rather than expiring the stored passphrase itself.

7. **Secret backend abstraction (goal 11).** KDE and GNOME are the *same* backend:
   both implement the D-Bus Secret Service API (`secret-tool`/libsecret). The real
   backends are ~4: `secret-service` (KDE + GNOME), macOS Keychain, Windows
   Credential Manager, 1Password CLI (`op`). Define a `SecretBackend` interface
   early — it is also what makes integration tests mockable (goal 16).

8. **Thin platform ports (goals 12, 13).** macOS already does silent passphrase
   caching natively via launchd + `ssh-add --apple-use-keychain`; the macOS port
   may reduce to "add keys with keychain", so avoid over-engineering it. Windows is
   the most divergent (service + named pipe, no socket) — keep it last.

9. **CI vs containers for non-Linux (goal 16).** Use GitHub Actions `macos-*` /
   `windows-*` runners for those platforms (more realistic than containers); keep
   Linux containers for the rest, noting that `keyctl` / D-Bus need setup there —
   another reason for the mockable backend interface.

10. **Phasing (rules 1, 9).** Harden the primary target first (Gentoo / OpenRC /
    KDE, goals 1–10) — possibly still in cleaned-up bash — then introduce the Go
    core, then widen to other backends and OSes. Each step must stay committable.

11. **CI least-privilege & lint coverage (rule 14, 12).** The existing
    `.github/workflows/linting.yml` has no explicit `permissions:` block (it runs
    on the repository default). Add a least-privilege block — verifying which
    scopes `reviewdog`/`shfmt` actually need before tightening, so CI doesn't
    break. While there, decide the lint story: wire a `make lint` target
    (shellcheck + a Markdown linter) and align CI with it. Go and a Markdown linter
    will be new file types needing a lint decision.
    **Decided (Phase 0):** `make lint` runs `shellcheck` + `shfmt -d` +
    `markdownlint-cli2` + `checkmake` + `actionlint`; CI declares `permissions:
    contents: read` and invokes the same `make lint`, replacing the per-tool actions
    (which would need write scopes for inline annotations). Per-file-type lint
    decisions are recorded under Phase 0.

12. **Install modes & path layout (goals 17–19).** Realise the two install modes
    and the XDG/FHS path layout in Phase 1 / packaging — config in `/etc` or
    `$XDG_CONFIG_HOME`, state/logs in `$XDG_STATE_HOME`, agent socket in
    `$XDG_RUNTIME_DIR`, nothing under `~/.ssh`. Open within: the per-user mode can't
    write `/etc/profile.d`, so its bootstrap hook moves to `~/.bashrc` /
    `~/.config/plasma-workspace/env/` — pick the per-user hook when Phase 1 lands.

---

## Phases

High-level roadmap, ordered so each phase leaves the repo committable (rule 9).
Only the *intent* of each phase is fixed here; the detailed sub-steps are written
into the phase when we reach it, and the open decisions above are resolved at the
phase that needs them (not up front).

The ordering follows open decision 10: harden the primary target first (possibly
still in bash), then introduce the Go core, then widen to other backends and OSes.

### Phase 0 — Foundations & repo hygiene

Lint and CI baseline with no behaviour change: a `make lint` target (shellcheck +
a Markdown linter) aligned with CI, and an explicit least-privilege `permissions:`
block in every workflow. Write the threat model down in two lines, since it drives
the later design. → goals 16; open decisions 1, 11; rules 12, 14.

Sub-phases (detailed steps written when we start each one):

- **0.1 — Repo hygiene. ✅ Done.** Renamed `makefile` → `Makefile`; added an
  `.editorconfig` (UTF-8, LF line endings, final newline, trim trailing whitespace,
  per-file-type indentation) and a `.gitattributes` (`* text=auto eol=lf`, explicit
  handling for shell scripts) to fix one formatting/line-ending standard across the
  repo. `.gitignore` already covers scratch/step files.
- **0.2 — Threat model.** The two lines are recorded in open decision 1 above
  (documentation only).
- **0.3 — `make lint` target (rule 12).** Wire into `make lint`: `lint-sh`
  (`shellcheck` + `shfmt -d`), `lint-md` (`markdownlint-cli2`), `lint-make`
  (`checkmake`), `lint-yaml` (`actionlint`). Rename `ssh-init-macos.sh` →
  `ssh-init-macos.zsh` (zsh linting deferred to the macOS phase, so the shellcheck
  by-name exclusion goes away). Decide here whether to add `editorconfig-checker`.
- **0.4 — CI alignment & least-privilege (open decision 11, rule 14).** Add
  `permissions: contents: read` to `linting.yml` and have CI run the same
  `make lint`, replacing the per-tool actions (`action-shellcheck`,
  `reviewdog/action-shfmt`) with that single target.

Per-file-type lint decisions (rule 12):

| File type | Decision |
|---|---|
| Shell — bash (`*.sh`) | `shellcheck` + `shfmt` |
| Shell — macOS (`*.zsh`) | Rename `ssh-init-macos.sh` → `*.zsh`; linting deferred to the macOS phase (also removes the shellcheck by-name exclusion) |
| Markdown (`*.md`) | `markdownlint-cli2` |
| Makefile | `checkmake` |
| YAML / GitHub workflows | `actionlint` |
| `.editorconfig` / `.gitattributes` | Files added in 0.1; whether to enforce them with `editorconfig-checker` is TBD — decide in 0.3 |
| Go | Deferred to Phase 3 when Go enters the repo (`gofmt`/`go vet`/`golangci-lint`) |

### Phase 1 — Harden the primary target (still bash)

Gentoo / OpenRC / KDE. Make the shipped behaviour match goals 1–10: fixed agent
socket, never kill a healthy agent, silent on success, bounded retries with a
give-up sentinel and an opt-out, clean exit with no keys, best-effort recovery,
key-expiry semantics, and GUI detection that also works under Wayland and without
a display. → goals 1–7, 9, 10; open decisions 2, 3, 4, 5, 6.

### Phase 2 — Diagnostic tool

The currently-missing diagnostic that reports who started the agent, why it isn't
working, and which processes are involved — runnable under `sudo` for the full
picture. → goal 8. (open: bash, or the first piece written in Go.)

### Phase 3 — Go core

Move the core logic out of bash into Go behind a thin shell entrypoint, minimizing
duplication; define the `SecretBackend` interface; stand up unit tests plus
container integration tests on Linux. → goals 14, 16; open decisions 7, 9.

### Phase 4 — Configurability & pluggable secret backends

Make the secret store pluggable (secret-service first, then 1Password) and the
tool highly parametrizable via a config file. → goals 11, 15; open decision 7.

### Phase 5 — Widen the OS targets

macOS as a thin port (it already caches passphrases natively — avoid
over-engineering), then Windows last as the most divergent target (service + named
pipe, no socket). → goals 12, 13; open decision 8.

### Phase 6 — Full test matrix

Extend CI to macOS and Windows runners and complete the cross-platform test
matrix. → goal 16; open decision 9.
