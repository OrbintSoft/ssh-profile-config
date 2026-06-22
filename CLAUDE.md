# ssh-profile-config — project rules

Rules for the rewrite of this project. Numbered, authoritative; add new rules by
appending. See `docs/DESIGN-NOTES.md` for background and the June 2026 incident.

## Rules

1. **Minimal changes.** Keep changes small, especially breaking ones. If a change
   touches many files or many lines, split it into sub-steps tracked in a
   gitignored `<activity>-steps.md` so work can resume even if the repo doesn't
   compile or is incomplete. Intermediate sub-steps may leave the repo broken, but
   each completed PLAN.md item must be committable (Rule 9). **Ask for
   authorization before each step.**

2. **Propose new rules** when one would help — don't add them silently.

3. **After a long task, before starting a new one**, decide whether to stay in the
   same session. To save tokens: ask for a compact, or write down what's done /
   what's left and start a new chat. Exception: keep context when it matters for
   the next task(s).

4. **Feel free to add skills** to the project (`.claude/skills/`).

5. **Before opening a PR or declaring done, verify quality** — run linters and
   tests if they aren't too heavy.

6. **Repo language is English** (code, comments, docs, log). Chat may be Italian.

7. **No sensitive data in committable files.** Never write personal data, anything
   about this machine/system, logs, or error output into a file that could be
   committed. That material goes only in gitignored scratch files.

8. **Step & scratch files.** A `<activity>-steps.md` holds the sub-steps of one
   PLAN.md activity. These — and any other scratch/working-memory files you create
   in the repo — are gitignored and free-form: store anything you need (logs,
   errors, notes), since they are never committed.

9. **Every PLAN.md item is committable when done.** Each plan item must leave the
   repo in a state that can be committed.

10. **Ask before committing.** Before each `git commit` (and before pushing), ask
    whether to commit — never commit or push unannounced. The user decides when.

11. **Check the current branch first.** At the start of a task and before every
    commit/push, run `git branch --show-current` and confirm you're on the intended
    feature branch (never `master`). The branch can change between turns — e.g. after
    a PR is merged — so never assume; verify.

12. **New file type → consider a linter.** Whenever a new kind or format of file
    first enters the repo (a new language, config/data format, etc.), evaluate
    whether a linter or validator exists for it and, if reasonable, add a
    `lint-<kind>` target wired into `make lint` (and CI). Record the decision —
    including a deliberate "no linter" — in PLAN.md.

13. **Don't embed one language inside another.** Never inline foreign-syntax
    content — a config-file body, another script, SQL, etc. — inside a heredoc or
    string literal of the host language. Author it as its own file of the proper
    type (so it can be read, diffed, and linted as that language) and have the host
    reference it: mount it, `source` it, or `sed`-substitute a committed template
    (`@TOKEN@` placeholders). Extends Rule 12. Writing a single literal value
    (`echo "x" > f`) is fine; a multi-line or structured fragment of another format
    is not.

14. **Least-privilege `GITHUB_TOKEN`.** Every GitHub Actions workflow declares an
    explicit top-level `permissions:` block granting only the scopes its steps use
    (default `permissions: contents: read`); grant any wider scope at the narrowest
    level (per-job) and only where a step needs it — never leave the token at the
    repository default. Re-audit whenever a workflow gains a step that writes (opens
    PRs, cuts releases, pushes packages/pages).

15. **Comments serve contributors and users, not the project's history.** A comment
    (and `--help`/`elog` text) states what the code does, how to use it, and any
    non-obvious *why* needed to maintain it safely (e.g. a workaround that must not be
    removed) — written for someone reading or using this project, who has no idea what
    our roadmap is. It does **not** narrate how the code came to be (no change log, no
    "we used to…", no PR storytelling) and contains **no references to PLAN.md, phase
    numbers, or rule numbers** — those are internal bookkeeping. The commit message and
    PLAN.md hold the history and the roadmap pointers; the code explains itself.
