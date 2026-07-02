# SSHakku — PLAN

Roadmap for the rewrite. We fix the **goals** first; the **phases** come after the
goals are reviewed and agreed. See `CLAUDE.md` for the project rules and
`docs/THREAT-MODEL.md` for the threat model and the June 2026 incident that
motivated the rewrite.

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
   environment variables don't match, fix them as far as possible. A healthy agent
   we did not start is adopted (via the fixed-socket symlink) and the anomaly is
   reported — never killed; only dead sockets/agents are reaped. (Note the hard
   limit: a child process cannot rewrite the environment of an already-running
   parent such as the session/GUI; the fixed-socket approach is what makes this
   robust.) See open decision 15 for the full five-state policy.

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
   **Decided (Phase 0) — see `docs/THREAT-MODEL.md` (source of truth).** In two
   lines: *protects* the passphrase from logs, shell history, `argv`
   (`ps` / `/proc/<pid>/cmdline`) and plaintext on disk — at rest only in the OS
   secret store, in transit only via a short-lived `keyctl` entry / stdin.
   Same-user processes, root, swap/coredumps and physical access are **enumerated as
   deferred decisions, not excluded by design**: each is settled per threat and
   confirmed at a final security evaluation.

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
   symlink as a last resort). Don't promise more. The same symlink is how a healthy
   foreign agent is adopted (open decision 15): the fixed path points at the
   foreign socket so the session's pinned `SSH_AUTH_SOCK` resolves to it.

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
    KDE), then the Go core, then widen to other backends and OSes; each step stays
    committable. **Decided:** the "possibly still in bash" hedge is settled as a
    **bash/Go split** — Phase 1 ships only the permanent shell plumbing in
    cleaned-up bash (paths, install modes, silence, agent lifecycle) as a stable
    baseline, and the branchy, stateful logic (retries / give-up, key-expiry,
    Wayland detection, secret-handling) moves to the Go core in Phase 2, written
    once rather than re-written from throwaway bash. The diagnostic tool follows
    the core (Phase 3) so it reuses Go primitives. **Brought forward:** rather than
    a wholesale Phase 2, the Go core is grown incrementally (strangler) starting
    with the path/token/dir/log slice, so no more throwaway bash accumulates — see
    the Phase 2 note for the slice breakdown.

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
    and the XDG/FHS path layout in Phase 1 (steps 1.1–1.2) — config in `/etc` or
    `$XDG_CONFIG_HOME`, state/logs in `$XDG_STATE_HOME`, agent socket in
    `$XDG_RUNTIME_DIR`, nothing under `~/.ssh`. Open within: the per-user mode can't
    write `/etc/profile.d`, so its bootstrap hook moves to `~/.bashrc` /
    `~/.config/plasma-workspace/env/` — pick the per-user hook in step 1.2.
    **Decided (step 1.1):** config **and** the session log live together under
    `${XDG_CONFIG_HOME:-~/.config}/sshakku` (one discoverable tree, not the
    `$XDG_STATE_HOME` split sketched above). The agent socket goes in the per-user
    tmpfs, resolved independently of the desktop/display server:
    `$XDG_RUNTIME_DIR/sshakku` → `/run/user/$UID/sshakku` (probed, owned by us)
    → `${XDG_CACHE_HOME:-~/.cache}/sshakku` when no logind exists. An
    unpredictable per-login token from the `@u` user keyring is inserted as a path
    component (`<runtime_dir>/<token>/agent.sock`) so the path is not reproducible
    across logins/reboots — defense-in-depth above the ownership+`0700` boundary;
    it degrades to the plain runtime dir when `keyctl` is absent. Deferred to the
    Go core (which owns path computation behind the entrypoint seam): keyring via
    syscalls (no `keyctl` binary), a `/dev/shm/sshakku/$UID/<token>/` tmpfs
    fallback with parent-validation (`lstat`/owner/no-symlink) + a `tmpfiles.d`
    entry for the system install, optional `boot_id` rotation for the `~/.cache`
    fallback, and optional per-login agent isolation as a config flag. The
    per-user bootstrap hook stays open for step 1.2.

13. **Which keys to auto-load is configurable (goals 1, 2, 15).** The config file
    selects the auto-load set in one of two modes: *all keys except a denylist*, or
    *only an allowlist*. Default: all keys (convenience); a security-conscious user
    narrows it to an allowlist to shrink the agent's blast radius — fewer keys in
    the agent (A2) means fewer credentials exposed to same-user processes and to any
    agent forwarding. Realised with the config file in the configurability phase.

14. **Project name (goal identity).** **Decided:** the project is named
    **SSHakku** (from Akkadian *iššakku*, a steward who administers an estate on
    behalf of its owner — here it tends the SSH agent and guards the keys, pulling
    each passphrase from the OS secret store; the name also surfaces `ssh`). It
    replaces the original `ssh-profile-config`, which mislabelled the tool as an
    `~/.ssh/config` manager (it manages neither SSH connection profiles nor
    `~/.ssh/config`) and described the bootstrap mechanism (`profile.d`) rather than
    the purpose. An earlier working name, *sshepherd*, was dropped to avoid a clash
    with an active registered trademark (FullArmor's **SSHepherd®**) in the
    SSH-security space. The `<name>` placeholder in the path layout (goal 19, open
    decision 12) resolves to `sshakku`. A short command alias `shak` is to be
    provided by the CLI when it lands. The GitHub repository and the Gentoo package
    are renamed to match.

15. **Agent lifecycle: self-healing & foreign-agent adoption (goals 5, 6, 8).** At
    shell-init the world is in one of five states; sshakku resolves them in
    precedence order rather than only "never kill a healthy agent":

    - **A — clean** (nothing reachable): reap any dead socket at our path, start
      *our* agent on the fixed socket, load the keys.
    - **B — ours healthy** (agent on our fixed socket): attach, load only the
      missing keys (fingerprint dedup), stay silent.
    - **C — ours zombie** (our socket/process dead, including the legacy
      `~/.ssh/agent`): reap what is ours, restart on the fixed socket.
    - **D — foreign healthy** (a reachable agent we did not start, env points
      elsewhere): never spawn a competitor — adopt it and **report the anomaly**.
    - **E — disaster** (mixed stale env, dead sockets, several agents): be
      maximally resilient — use any healthy agent (ours first, then a foreign one
      with a report), reap the dead, never leave the shell on a dead socket.

    Identity: "ours" = the agent listening on our fixed socket (PID recorded in a
    state file when we start it); "legacy-ours" = `ssh-agent -a ~/.ssh/agent/…`;
    anything else is foreign. The hard limit of open decision 4 still holds — we fix
    only the current shell and its descendants; already-open GUI apps are reached
    only via the fixed-socket symlink.

    **Decided (this discussion):**
    - **Adopt a foreign healthy agent by symlink** (case D): point the fixed socket
      at the foreign socket (`fixed → foreign`), keep `SSH_AUTH_SOCK` on the fixed
      path, and load our keys into the foreign agent — accepting that this widens
      the keys' blast radius, which is exactly why it is reported as an anomaly, not
      the steady state. If the foreign agent dies the fixed path is left dangling
      and handled like any other dead socket.
    - **Reap dead foreign sockets/agents too**, not only ours — but *only the dead*
      (no listener / unreachable process); a healthy foreign agent is never killed
      (that is case D). Automatic reaping stays within the invoking user's
      privileges (rule 18); deeper cleanup across users is the diagnostic tool's job
      under `sudo`.

    The reporting/attribution side — who started the foreign agent, and how to
    return to the clean state where only we run the agent — is the diagnostic tool's
    mandate (goal 8, Phase 3).

16. **Which keys' passphrases are stored in the wallet is configurable (goals 2,
    7; open decision 13).** Independently of which keys are auto-loaded (decision
    13), the config file selects the *wallet-store* set in one of two modes: *all
    keys except an exclude-list*, or *only an include-list*. Default: store all
    (convenience — once typed, a passphrase is never asked again). A
    security-conscious user excludes sensitive keys so their passphrase is never
    persisted: it is typed each time it is needed and kept in memory only. The
    policy is consulted at **every** wallet write — the load-keys prompt-then-store
    path and the askpass broker's miss-then-store fallback — so an excluded key is
    still used but never saved. Realised with the config file in the configurability
    phase; until then every successfully typed passphrase is stored.

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
the later design. Settle contributor licensing (a CLA preserving the holder's
freedom to relicense) while the project has no external contributors yet.
→ goals 16; open decisions 1, 11; rules 12, 14, 16.

Sub-phases (detailed steps written when we start each one):

- **0.1 — Repo hygiene. ✅ Done.** Renamed `makefile` → `Makefile`; added an
  `.editorconfig` (UTF-8, LF line endings, final newline, trim trailing whitespace,
  per-file-type indentation) and a `.gitattributes` (`* text=auto eol=lf`, explicit
  handling for shell scripts) to fix one formatting/line-ending standard across the
  repo. `.gitignore` already covers scratch/step files.
- **0.2 — Threat model. ✅ Done.** `docs/THREAT-MODEL.md` — a formal STRIDE model
  (assets, trust boundaries, threats tagged present/presumed/future, and the derived
  security invariants) to anchor the rewrite and the platform ports. The two-line
  summary stays in open decision 1 above.
- **0.3 — `make lint` target (rule 12). ✅ Done.** `make lint` runs `lint-sh`
  (`shellcheck` + `shfmt -d`), `lint-md` (`markdownlint-cli2`), `lint-make`
  (`checkmake`), `lint-yaml` (`actionlint`) and `lint-editorconfig`
  (`editorconfig-checker`). Renamed `ssh-init-macos.sh` → `ssh-init-macos.zsh`
  (zsh linting deferred to the macOS phase). `editorconfig-checker` **adopted**
  (whole tree; it honours `.gitignore`, so scratch files are skipped). Each tool
  reads its own config file (rule 13): `.markdownlint-cli2.yaml` (disables
  MD013/MD029/MD060 — see below), `checkmake.ini` (relaxes
  `minphony`/`maxbodylength`),
  `.editorconfig-checker.json` (excludes `LICENSE` verbatim and the deferred
  `*.zsh`). To satisfy the new linters with no behaviour change: shell scripts
  reformatted with `shfmt -w`, `.vscode/settings.json` reindented to 2 spaces, and
  `.editorconfig` marks Markdown indentation `unset` (content-driven). The lint
  tools are external dev/CI tools (separate processes, not bundled or
  distributed), so they carry no EUPL-1.2 obligations and don't obstruct
  relicensing (rule 16). `linting.yml`'s `ignore_names` was updated to the `.zsh`
  name (the shellcheck action scans `*.zsh`); the full CI rework (permissions
  block + running `make lint`) stays in 0.4.
  - Disabled Markdown rules: `MD013` (line-length — prose is hand-wrapped, tables
    and URLs legitimately exceed 80), `MD029` (ol-prefix — goals are numbered
    continuously across sub-sections and referenced by number), `MD060`
    (table-column-style — pipe spacing left to the author).
- **0.4 — CI alignment & least-privilege (open decision 11, rule 14). ✅ Done.**
  `linting.yml` now declares top-level `permissions: contents: read` and runs a
  single `lint` job that installs the six tools and invokes `make lint`,
  replacing the per-tool actions (`ludeeus/action-shellcheck`,
  `reviewdog/action-shfmt`) and dropping the `ignore_names` workaround. GitHub
  Actions are pinned by full commit SHA with a `# vX.Y.Z` comment (minor+patch),
  and a new `.github/dependabot.yml` enables the `github-actions` ecosystem to
  keep them current. The lint tools are pinned to explicit versions in the
  install step (shellcheck via release tarball; shfmt, checkmake, actionlint and
  editorconfig-checker via `go install`; markdownlint-cli2 via `npm`); auto-bump
  of those waits for the `go.mod`/`package.json` that arrive with the Go core
  (Phase 3). `dependabot.yml` is non-workflow YAML, already covered by the 0.3
  lint decision (editorconfig-checker for formatting; GitHub validates the schema
  server-side), so it needs no new per-file-type decision.
- **0.5 — Contributor licensing & CLA (rule 16). ✅ Done.** Added `CONTRIBUTING.md`,
  `CLA.md` and `DCO.txt` so contributors **keep the copyright** in their work while
  granting the copyright holder a **non-exclusive** licence to also distribute the
  project under other licences (e.g. proprietary/OEM) alongside the permanent public
  EUPL-1.2 release — no copyright assignment, no commit reverts. Mechanism: **DCO 1.1
  sign-off** (`Signed-off-by`) **+ acceptance-by-action** of the CLA (no signing
  bot); opening a PR with a sign-off certifies the DCO and accepts the CLA. The CLA
  adapts the **Harmony HA-CLA-I** (individual; HA-CLA-E noted for entities). The
  Harmony text is **CC BY 3.0 Unported**, adapted with attribution — a contract
  document, not runtime code or a dependency, so it imposes no terms on the software
  and does not obstruct relicensing (rule 16). `COPYRIGHT.md`, `AUTHORS.md` and
  `README.md` were updated to match. The new files are Markdown / plain text, already
  covered by `markdownlint-cli2` and `editorconfig-checker`, so no new per-file-type
  linter (rule 12). Governing law follows EUPL Art. 15 (law of the EU Member State
  where the holder is established, with Belgian law as the fallback), interpreted
  consistently with Union law and the EUPL — not a hard-coded national choice. A
  final IP-lawyer review is advisable before the first non-EUPL (OEM) sale. **Follow-up (rule 2):** propose a Rule 17 —
  "every contribution requires a DCO sign-off and CLA acceptance before merge" — to
  be formalised when the contribution flow is enforced.
- **0.6 — Contributor DX for the sign-off flow. ✅ Done.** Lower the friction a
  contributor meets with the DCO/CLA sign-off requirement. `CONTRIBUTING.md` gains
  a recovery recipe (`git rebase --signoff origin/master` + `git push
  --force-with-lease`) for when the DCO check fails on an earlier commit, plus an
  opt-in `prepare-commit-msg` hook under `.githooks/` (enabled with `git config
  core.hooksPath .githooks`) that adds the trailer automatically via `git
  interpret-trailers`, never duplicating one and skipping merge/squash messages. A
  `.github/pull_request_template.md` checklist nudges sign-off, `make lint`, scope
  and English before a PR is opened. The hook is an extensionless shell script, so
  it is wired into `make lint`'s `lint-sh` (`SH_SCRIPTS` now also globs
  `.githooks/*`) and given a tab-indent `.editorconfig` rule (`[.githooks/*]`) so
  shellcheck, shfmt and editorconfig-checker all cover it consistently (rule 12). A
  custom "comment on DCO failure" action was **rejected**: the DCO app already
  links its own remediation, and the action would widen the workflow token to
  `pull-requests: write` against the least-privilege default (rule 14).

Per-file-type lint decisions (rule 12):

| File type | Decision |
|---|---|
| Shell — bash (`*.sh`) | `shellcheck` + `shfmt` |
| Shell — macOS (`*.zsh`) | Renamed in 0.3; linting deferred to the macOS phase (also removes the shellcheck by-name exclusion) |
| Markdown (`*.md`) | `markdownlint-cli2` (config `.markdownlint-cli2.yaml`) |
| Makefile | `checkmake` (config `checkmake.ini`) |
| YAML / GitHub workflows | `actionlint`; non-workflow YAML/INI/JSON configs have no dedicated linter — `editorconfig-checker` enforces their charset/EOL/indent/final-newline |
| All committed files | `editorconfig-checker` **adopted in 0.3** (config `.editorconfig-checker.json` excludes `LICENSE` verbatim, the deferred `*.zsh`, and `*.go` — gofmt owns Go formatting and legitimately allows spaces inside string literals; `.gitignore` is honoured) |
| Shell — bats tests (`*.bats`) | Deferred to Phase 1.5 when test files enter the repo |
| Go (`*.go`) | `gofmt -l` + `go vet ./...` + `golangci-lint` (config `.golangci.yml`, standard set); compiled by `make build`. Wired into `make lint` as `lint-go` and installed in CI (pinned). License (rule 16): the Go toolchain, its standard library (BSD-3-Clause) and `golangci-lint` are EUPL-1.2 compatible and don't obstruct relicensing — build/dev tools follow the 0.3 precedent (no bundled obligations); the third-party module list (`golang.org/x/sys`, BSD-3-Clause) is recorded in `COPYRIGHT.md`. |
| TOML (`*.toml`) | `taplo lint` + `taplo format --check`, wired into `make lint` as `lint-toml` and installed in CI (pinned `@taplo/cli`); config `.taplo.toml` excludes only the deliberately malformed test fixture (the parser's error path, covered by Go tests). License (rule 16): the runtime parser `github.com/BurntSushi/toml` (MIT) is EUPL-1.2 compatible and doesn't obstruct relicensing, recorded in `COPYRIGHT.md`; `taplo` is a CI-only dev tool (0.3 precedent). |

### Phase 1 — Harden the primary target: shell plumbing (still bash)

Gentoo / OpenRC / KDE. Scope narrowed by the bash/Go split (open decision 10):
Phase 1 ships only the **permanent shell plumbing** in cleaned-up bash — a stable,
committable baseline on the primary box — while the branchy, stateful logic moves
to the Go core in Phase 2 (written once, not twice). Fixed agent socket and
never-kill-a-healthy-agent (already shipped), clean exit with no keys, best-effort
recovery, silent-on-success output discipline, and the standard path/install
layout that gets our files out of `~/.ssh`. The login entrypoint is shaped so the
Go core slots in behind it. → goals 3, 5, 6, 10, 17–19; open decisions 3, 4, 12.

Sub-phases (detailed steps written when we start each one):

- **1.1 — XDG path layout, out of `~/.ssh`.** Move our files to standard paths:
  socket + lock to `$XDG_RUNTIME_DIR/<name>/` (0700, with a fallback for when it
  is unset — possible under OpenRC/elogind), log/state to `$XDG_STATE_HOME/<name>/`
  (0600 files), config under `$XDG_CONFIG_HOME/<name>/` or `/etc/<name>/`. The keys
  stay in `~/.ssh` (OpenSSH's domain; we only read them). Align the askpass log to
  the same state dir. → goal 19; open decision 12; threats I7, I10, D2; invariant 3.
- **1.2 — Two install modes + bootstrap hook.** System-wide (`sudo`,
  `/etc/profile.d`, `$BINDIR`) vs per-user (no root, everything under `$HOME`); the
  same logic, only the paths and the bootstrap hook differ. Resolves the per-user
  hook left open in open decision 12 (`~/.bashrc` vs
  `~/.config/plasma-workspace/env/`). → goals 17, 18; open decision 12; threat E3.
- **1.3 — Silent-on-success & shell safety, with the Go seam.** Zero stdout/stderr
  on the success path; `set -u`-clean; degrade gracefully when `keyctl` / `flock`
  are absent. The seam is now real: the entrypoint evals the Go core
  (`sshakku shell-init`, Phase 2 slice 1), so the remaining 1.3 work is the
  silence / `set -u` hardening layered on top. → goal 3; open decision 3; threat
  I4; invariant 2.
- **1.4 — Agent lifecycle & recovery.** The five-state self-healing policy (open
  decision 15): never kill a healthy agent (`ssh-add -l` exit 0 and 1 both healthy),
  clean exit with no keys, reap dead sockets/agents (ours and dead foreign ones),
  adopt-by-symlink a healthy foreign agent with an anomaly report, and a last-resort
  dangling-socket symlink for already-open GUI apps. **Now Go slice 2** (see the
  Phase 2 note): this lifecycle logic moves into the Go core rather than staying in
  bash. → goals 5, 6, 8; threats D1, D5.
- **1.5 — Shell test harness (rule 12).** `bats` unit tests + container integration
  tests covering the plumbing regression scenarios below. `bats` is a new file
  type — evaluate a linter and record the decision (including a deliberate "no
  linter") here. → goal 16.

  Plumbing regression checklist (post-change):

  1. Fresh login → two terminals → both see the key in `ssh-add -l`, with **no**
     passphrase prompt the second time.
  2. `SSH_AUTH_SOCK` is the fixed socket path in every terminal and in a GUI app
     (e.g. inspect `/proc/<plasmashell-pid>/environ`).
  3. Kill the agent (`ssh-agent -k` / `pkill ssh-agent`) → a new terminal restarts
     it at the **same** socket path and reloads the key.
  4. First-ever run with an empty vault → prompts once, then `secret-tool lookup`
     returns the passphrase on later logins (no prompt).
  5. Reachable-but-empty agent (`ssh-add -l` exit 1) must be treated as **healthy**
     and never killed (the D1 regression).

### Phase 2 — Go logic core

Move the branchy, stateful logic out of bash into a small Go core behind the thin
shell entrypoint, minimizing duplication: bounded retries with a persistent
give-up sentinel and an opt-out, key-expiry semantics (`ssh-add -t`, silent re-add
from the vault), GUI / secret-prompt detection that works under Wayland and
headless, and secret-handling hardening (no passphrase in env or argv, absolute
`SSH_ASKPASS` + `SSH_ASKPASS_REQUIRE=force`, clean child env). Define the
`SecretBackend` interface (it also makes the tests mockable) and stand up unit
tests plus container integration tests on Linux. Go entered the repo early (slice
1 below), where the Go lint decision (`gofmt` / `go vet` / `golangci-lint`,
`make build`) and `go.mod` were already made (rule 12). → goals 1, 2, 4, 9, 14,
16; open decisions 2, 5, 6, 7, 9.

**Brought forward — Go core grown incrementally (strangler).** Instead of one
wholesale rewrite, the Go core is added slice by slice behind the entrypoint seam
while the bash shrinks toward a thin `eval "$(sshakku shell-init)"`. Each slice
is committable and the bash keeps working until each piece moves.

- **Slice 1 — path / token / dir / log core. ✅ Done.** `cmd/sshakku` +
  `internal/paths` + `internal/sessionlog`: path resolution (config dir; runtime
  dir XDG_RUNTIME_DIR → /run/user/$UID owned → ~/.cache), the per-login `@u`
  keyring token via the `keyctl(2)` syscall (`golang.org/x/sys`, no `keyctl`
  binary), 0700/0600 dir+log creation with a symlinked-leaf guard, legacy
  `~/.ssh/agent` cleanup, and a bounded session log. `shell-init` prints
  `agent_sock`/`agent_lock`/`log_file`; the entrypoint evals them. The Go lint and
  `golang.org/x/sys` (BSD-3-Clause) licence decisions are recorded (rules 12, 16).
- **Slice 2 — agent lifecycle. ✅ Done.** (the Phase 1.4 work, in Go): reachability
  plus the five-state self-healing policy of open decision 15 — start on the fixed
  socket when clean, attach when ours is healthy, reap dead sockets/agents (ours and
  dead foreign ones), and adopt-by-symlink a healthy foreign agent while reporting
  the anomaly. Never kill a healthy agent. `internal/agent` (probe/inspect/manage/
  ensure, flock-serialised); `shell-init` is the sole owner of the lifecycle.
- **Slice 3 — key loading + `askpass`. ✅ Done.** `internal/keys` +
  `internal/keyring`: enumerate `~/.ssh/id_*`, skip fingerprints already in the
  agent (`ssh-keygen`/`ssh-add -l`), and add the rest via the secret store
  (`secret-tool`) or a prompt (`kdialog`), handing each passphrase to ssh-add out of
  band through the `@u` keyring (payload never in argv) + an SSH_ASKPASS helper
  marked by `SSHAKKU_ASKPASS`. `sshakku load-keys` is driven from the login hook in
  interactive shells; the bash askpass script is retired. GUI detection covers
  Wayland and X11.
- **Slice 4 — retries / give-up + key-expiry. ✅ Done.** Resolves open decisions
  5 and 6. Keys expire in the agent via `ssh-add -t` (default 8h, configurable
  with `SSHAKKU_KEY_LIFETIME`; `0` disables); a new terminal silently re-adds an
  expired key from the wallet, and in a still-open shell the wallet-aware
  SSH_ASKPASS broker refills it without a terminal prompt, falling back to
  `/dev/tty` only on a wallet miss (and storing what is typed there). A wrong
  passphrase retries up to `SSHAKKU_MAX_ATTEMPTS`, a stale stored passphrase being
  re-prompted and replaced; after exhaustion the key is given up — notified on the
  terminal unless `SSHAKKU_QUIET` — and skipped in new shells for
  `SSHAKKU_GIVEUP_TTL` (per-login, tmpfs-backed; `SSHAKKU_NO_GIVEUP` opts out).
  `internal/giveup` + `internal/keys`; the env knobs are documented in
  `docs/CONFIGURATION.md` and the bash is now just the thin hook.

### Phase 3 — Diagnostic tool

The currently-missing diagnostic that reports who started the agent, why it isn't
working, and which processes are involved — runnable under `sudo` for the full
picture. It attributes a foreign agent (open decision 15, case D) to the process or
tool that started it, and guides — or applies — the fix back to the clean state in
which only sshakku runs the agent. Now lands after the Go core, so it is built in Go
reusing the core's inspection primitives rather than as throwaway bash. → goal 8;
threat E1.

**✅ Done.** `sshakku doctor` (`internal/diagnose`, reusing the `agent` package's
inspection primitives): a read-only report that names the agent-lifecycle state
(the five states of open decision 15, A–E), classifies every `ssh-agent` process
as ours/legacy/foreign, probes reachability, compares `SSH_AUTH_SOCK` against the
fixed socket, tails the session log, and derives plain findings with a
recommendation. It attributes each agent to its launcher by walking the `/proc`
PPid ancestry (systemd, KDE Plasma, GNOME/GDM/SDDM/LightDM, sshd, login shells);
because `ssh-agent` daemonizes and reparents to `init`, ancestry frequently
dead-ends at pid 1, and the report says so rather than crediting init.
`doctor --fix` applies the same self-heal the login path runs (`EnsureAgent`:
reap dead, start on the fixed socket, or adopt a healthy foreign agent — never
killing a healthy one) and re-reports; it warns that it cannot rewrite the calling
shell's `SSH_AUTH_SOCK`. Documented in `docs/DIAGNOSTICS.md` (linked from the
README). No new file type — the docs are Markdown, already covered by
`markdownlint-cli2` + `editorconfig-checker` (rule 12).

*Deferred refinements (not blocking):* deeper foreign-agent attribution via
socket-path heuristics (gnome-keyring `keyring/ssh`, gpg-agent, systemd
`ssh-agent.socket`) and environment probing to recover a launcher lost to the
daemonize/reparent; and cross-user inspection under `sudo` for the fuller picture
(today `doctor` resolves paths for, and reaps only, the invoking user).

### Phase 4 — Configurability & pluggable secret backends

Make the secret store pluggable (secret-service first, then 1Password) and the
tool highly parametrizable via a config file under `$XDG_CONFIG_HOME/sshakku/`
(default `~/.config/sshakku/`), into which the current `SSHAKKU_*` environment
knobs migrate. → goals 11, 15; open decisions 7, 13, 16.

### Phase 5 — Widen the OS targets

macOS as a wide port, never trust Apple; then Windows last as the most divergent target (service + named pipe, no socket, use win32 safe API). → goals 12, 13; open decision 8.

### Phase 6 — Full test matrix

Extend CI to macOS and Windows runners and complete the cross-platform test
matrix. → goal 16; open decision 9.

### Phase 7 — CI review & dependency hardening

A final pass over the whole CI once it spans every platform and language. Audit
each workflow for least-privilege `permissions:` (rule 14), de-duplicate the
lint/test jobs, add dependency caching and sensible `concurrency`, and confirm
`make lint` and the test suites stay the single entrypoints CI invokes. Settle
dependency automation: choose Dependabot vs Renovate (open) and extend it to
*every* ecosystem — `github-actions`, `gomod`, `npm` — so the lint-tool versions
pinned by hand in Phase 0.4 become auto-managed once the `go.mod`/`package.json`
manifests exist. Pin all third-party actions by full commit SHA with version
comments, and pin tool/runtime versions (Go, Node, the linters) for reproducible
builds. Re-evaluate per-file-type lint coverage (rule 12) against whatever file
types the repo has grown by then. → goal 16; open decisions 9, 11; rules 12, 14.
