# SSHakku — Threat model

Formal threat model for the secret and SSH-agent handling. It exists so the
rewrite — and every later platform port and secret backend — has one explicit
reference for what we defend, what we accept, and which concrete threat each
design decision answers.

It is **design-level and generic**: it describes the system, not any particular
machine or deployment.

## Summary (two lines)

- **Protects** the key passphrase from logs, shell history, process arguments
  (`ps` / `/proc/<pid>/cmdline`) and plaintext on disk: at rest it lives only in
  the OS secret store, in transit only via a short-lived `keyctl` entry / stdin.
- **Threats whose response is still open**: processes of the same user, root,
  secrets reaching swap or coredumps, and physical access. Same-user processes can
  already use the key loaded in the agent today, so we currently do not defend
  against them — but these are enumerated below with their decision **Deferred**,
  not excluded in advance. Which to mitigate and which to accept is settled per
  threat and confirmed at a final security evaluation.

## Origin — the June 2026 incident

This model is grounded in a real failure. A login script killed the session's
**healthy** agent — misreading "reachable but no keys yet" (`ssh-add -l` exit 1)
as dead — and restarted it; because recent OpenSSH relocates and randomises the
agent socket once `~/.ssh/agent/` exists, the replacement was unreachable from the
already-running session, which kept a stale `SSH_AUTH_SOCK`. The lasting lessons
are captured below as threats **D1** (never kill a reachable agent) and **D2**
(keep our files out of `~/.ssh`; pin a fixed socket) and in the derived security
invariants.

## Method

Threats are categorised with **STRIDE** (Spoofing, Tampering, Repudiation,
Information disclosure, Denial of service, Elevation of privilege) and each is
tagged with a **status**:

| Status | Meaning |
|---|---|
| **Present** | Concretely applies to the current shell implementation. Marked *(mitigated)* if already handled, *(open)* if not yet. |
| **Presumed** | Plausible given the approach; not confirmed against the code. |
| **Future** | Relevant only to planned work (the Go core, the diagnostic tool, other OS ports, pluggable secret backends). |

The status says whether a threat *applies*; it is separate from how we have
decided to *respond*. The response is tagged with a **decision**:

| Decision | Meaning |
|---|---|
| **Mitigate** | We defend against it; the mitigation is described. |
| **Accept** | We consciously accept the residual risk, with a stated reason. |
| **Deferred** | The threat is considered, but how to respond is not decided yet. |

In this early phase we enumerate every threat we can think of and leave most
responses **Deferred**: which ones are worth mitigating and which we accept is a
design decision settled per threat as the work matures, and confirmed in a final
security evaluation. A threat is never excluded "by design" before that decision
is made.

## Assets

| ID | Asset | Why it matters |
|---|---|---|
| A1 | Key passphrase | Unlocks the private key; the most sensitive secret. |
| A2 | Key loaded in the agent | The usable credential; equivalent to the private key for the agent's lifetime. |
| A3 | Agent socket / endpoint | Gateway to A2 — anyone who can talk to it can authenticate. |
| A4 | Secret-store entry | The passphrase at rest in the OS vault. |
| A5 | Handoff channel | The short-lived `keyctl` entry / stdin used to pass A1 to `ssh-add`. |
| A6 | Logs & state files | Must never contain A1; integrity of the give-up sentinel. |
| A7 | Session agent variables | `SSH_AUTH_SOCK` / `SSH_AGENT_PID`; their correctness is the availability goal. |
| A8 | Config files | Drive behaviour; tampering changes what we trust or execute. |

## Trust boundaries & actors

| Actor | Trust | Note |
|---|---|---|
| The user | Trusted | Owns everything. |
| Other processes of the same user | Currently trusted | They must be able to use A2/A3, so today we do not defend against them; broader hardening here is a **Deferred** decision (see the residual-risk register). |
| Other local (unprivileged) users | Untrusted | The primary adversary we defend against. |
| root / kernel | Decision deferred | Can already take A1–A8; defending is likely **Accept** (not worth it), but that is confirmed at the final review, not assumed here. |
| Remote SSH peers | Other system's responsibility | We only make the agent available; authenticating peers is OpenSSH's job, not this component's. |
| OS secret store / session keyring | Trusted component | Gated by session unlock; we rely on it. |
| External secret CLIs & OS keychains | Trusted components | `op`, macOS Keychain, Windows Credential Manager once wired in. |

## Residual-risk register (decisions deferred)

These threats are identified and **kept in scope for consideration**; how we
respond to each is not decided yet (**Deferred**), to be settled as the design
matures and confirmed at the final security evaluation. None is excluded "by
design".

| Threat | Current treatment | Candidate response | Likely decision |
|---|---|---|---|
| Other processes of the same user | Trusted today (must be able to use the key) | Configurable key expiry (`ssh-add -t`, opt-out), confirm-on-use (`ssh-add -c`), and loading only needed keys (allowlist mode) to bound the window | Deferred (to the rewrite) |
| root / kernel | No defence | — (an attacker here already owns the session) | Deferred — probably Accept |
| Secrets reaching swap, coredumps, memory forensics | No defence | `mlock`, disable core dumps around the handoff | Deferred |
| Physical access / cold-boot | No defence | Rely on full-disk encryption / OS lock screen | Deferred — probably Accept |
| Integrity of OpenSSH, OS secret store, desktop | Trusted components | — (other systems' responsibility) | Accept (dependency) |

Several of these are reduced by configuration **outside this software's control**:
full-disk encryption (swap, cold-boot), the desktop session lock (a same-user
process while the user is away), and — once the rewrite lands — choosing to set a
key timeout. Our responsibility is to *enable* and default to safe behaviour and
to document these expectations; we cannot enforce the user's environment. Overall
security is a shared responsibility between the software and how it is deployed.

## Threats

### Information disclosure (passphrase leakage — the core concern)

| ID | Threat & vector | Status | Mitigation / residual |
|---|---|---|---|
| I1 | Passphrase passed in an **environment variable** → inherited by children, visible in `/proc/<pid>/environ`, easily logged. | Present (open) | Never put A1 in the environment; only key ids transit env. Hand A1 over out of band. |
| I2 | Passphrase in a command **argument** → `/proc/<pid>/cmdline` is world-readable by default, so other local users can read it. | Present (open) | Feed A1 via stdin / `keyctl padd <<<…`; audit every invocation that touches A1. |
| I3 | Passphrase written to a **log** file. | Present (mitigated) | Never log secrets; the capped log holds only non-sensitive events. |
| I4 | Any byte on **stdout/stderr** on the success path → corrupts non-interactive `scp`/`rsync`/`git`-over-ssh and could echo a secret. | Present (open) | Success path emits nothing; all output goes to the log only. |
| I5 | `keyctl` handoff entry readable by **same-user** processes. | Present (mitigated, residual) | Short timeout + unlink on read narrows the window. **Deferred**: residual same-user exposure tracked in the residual-risk register. |
| I6 | Secret-store entry queryable by any same-user process once the session is unlocked. | Present (residual) | Relies on the session lock. **Deferred**: same-user exposure tracked in the residual-risk register. |
| I7 | **World-readable** state/log files or socket directory → other local users read A3/A6. | Present (open) | Per-user `0700` dirs, `0600` files; socket in `$XDG_RUNTIME_DIR` (already `0700`). |
| I8 | Passphrase reaching **swap or a coredump**. | Present (residual) | **Deferred**: candidate defence-in-depth is `mlock` / disabling core dumps; tracked in the residual-risk register. |
| I9 | **Agent forwarding** (`ssh -A` / `ForwardAgent`) exposes A2 to the remote host, which can use the loaded key for the connection's lifetime. | Present (open) | We do not enable forwarding; how to guard it (warn, confirm-on-use, scope `IdentityAgent`) is **Deferred** to the rewrite. |
| I10 | The current shell askpass writes its session log **under `~/.ssh`**, against the keep-files-out-of-`~/.ssh` invariant. | Present (open) | **Mitigate** (decided): relocate the log to the per-user `0700` state dir in the rewrite. |

### Denial of service (the "SSH is ready" guarantee is itself a goal)

| ID | Threat & vector | Status | Mitigation / residual |
|---|---|---|---|
| D1 | Script **kills a healthy agent** (misreads "reachable but empty", `ssh-add -l` exit 1, as dead). | Present (mitigated) | Treat exit 0 and 1 as healthy; restart only on no-socket / exit 2 / timeout. Invariant to keep. |
| D2 | Creating `~/.ssh/agent/` makes OpenSSH 10.x **relocate the socket** to a random path → the session points at a dead one. | Present (mitigated) | Keep our files out of `~/.ssh`; pin a fixed socket in `$XDG_RUNTIME_DIR`. |
| D3 | **Login-burst race**: simultaneous shells start competing agents. | Present (mitigated) | `flock` around agent start. |
| D4 | **Retry storm**: every shell retries forever, spamming output. | Present (planned) | Bounded retries + persistent give-up sentinel + opt-out. |
| D5 | Already-running session/GUI keeps a **stale** `SSH_AUTH_SOCK` we cannot rewrite. | Present (constraint) | Hard limit; mitigate with a fixed socket path and a last-resort dangling-socket symlink. |

### Tampering

| ID | Threat & vector | Status | Mitigation / residual |
|---|---|---|---|
| T1 | **Symlink / path attack** on the socket or recovery symlink in a shared dir → redirect or hijack the agent. | Presumed | Operate only inside a per-user `0700` dir; never `/tmp`; refuse to follow symlinks out of it. |
| T2 | **TOCTOU** races on socket checks / file creation. | Presumed | Atomic create, `flock`, re-check after acquiring the lock. |
| T3 | Hostile **`SSH_ASKPASS` / `PATH`** makes `ssh-add` run an attacker binary. | Presumed | Set an absolute `SSH_ASKPASS`, a clean env, and `SSH_ASKPASS_REQUIRE=force`. |
| T4 | Poisoned **config file** changes trusted paths or commands. | Future | Validate config in the Go core; never execute arbitrary paths read from config. |

### Spoofing

| ID | Threat & vector | Status | Mitigation / residual |
|---|---|---|---|
| S1 | A rogue process listens at our **predictable endpoint path** and impersonates the agent. | Presumed | Endpoint lives in a `0700` per-user dir an attacker cannot write; verify reachability before use. |
| S2 | A **fake askpass / vault prompt** phishes the user for the passphrase. | Presumed | Use only the real session keyring's prompt; never roll our own GUI prompt for A1. |

### Elevation of privilege

| ID | Threat & vector | Status | Mitigation / residual |
|---|---|---|---|
| E1 | The **diagnostic tool run with `sudo`** writes secrets as root, trusts user-controlled env, or follows attacker symlinks. | Future | Use elevation only for read-only inspection; never write A1; sanitise env; resolve paths safely. |
| E2 | Our login script runs in an **unexpectedly privileged** context (e.g. a root login shell). | Presumed | Behave safely at any privilege; never assume or require root. |
| E3 | System-wide install paths **writable by the user** → local privilege escalation. | Present (open) | System files owned by root and not user-writable; correct install modes and perms. |

### Repudiation

| ID | Threat & vector | Status | Mitigation / residual |
|---|---|---|---|
| R1 | Too little record of security-relevant actions (key load, give-up, agent restart) to diagnose abuse. | Present (mitigated) | Keep a capped, non-secret log of these events. |

## Future / multi-platform notes

When the logic moves into the **Go core** and to other platforms, re-evaluate the
threats above against each new mechanism:

- **macOS** — Keychain item ACLs (keep them narrow), `ssh-add --apple-use-keychain`,
  and ownership of the launchd-managed agent. Reuses A1–A7 with the Keychain as A4.
- **Windows** — the agent endpoint is a **named pipe**, not a socket: its security
  descriptor must restrict access to the owning user (the A3 / S1 / T1 analogue).
  Credential Manager / DPAPI as A4; mind service-vs-user context.
- **Pluggable secret backends** — a 1Password-style CLI (`op`) adds: trusting the
  binary found on `PATH` (S1), session-token handling, and never logging secret
  output (I3). The backend interface is the natural home for these invariants.
- **Go core** — dependency supply chain, parsing of untrusted config (T4), and safe
  temp-file handling (T2).
- **CI / containers** — mocked `keyctl` / D-Bus must not weaken the real backends,
  and CI secrets must not leak into logs (I3).

## Security invariants (derived)

The rewrite must uphold these on every platform; each traces to the threats above:

1. **No secret in env or argv.** A1 moves only via stdin / out-of-band handoff.
   (I1, I2)
2. **Silent, secret-free output & logs.** Nothing on stdout/stderr on success; no
   secret is ever logged. (I3, I4)
3. **Locked-down files.** State, logs and the socket live in per-user `0700` dirs
   with `0600` files, in standard paths, never under `~/.ssh`. (I7, I10, D2, T1)
4. **Never kill a reachable agent.** Restart only a genuinely dead one, at a fixed
   protected path. (D1, D2, S1)
5. **Bounded, resettable retries with an opt-out.** (D4)
6. **Least privilege.** Scripts are safe at any privilege and never require root;
   only the diagnostic may elevate, and only to read. (E1, E2, E3)
7. **Clean environment for child tools.** Absolute `SSH_ASKPASS`,
   `SSH_ASKPASS_REQUIRE=force`, no reliance on an inherited `PATH`. (T3)
8. **Restrict the agent endpoint to its owner** on every platform — socket
   permissions or pipe security descriptor. (A3, S1, T1)
