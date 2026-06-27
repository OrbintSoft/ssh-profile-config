# Contributing to sshepherd

Thanks for your interest in contributing! This page explains the licensing terms
every contribution must follow and how to send changes.

## Licensing and sign-off

sshepherd is, and will remain, made available to the public under the
**European Union Public Licence v. 1.2 (`EUPL-1.2`)**. Your contributions go in
under that licence and are also covered by our
[Contributor License Agreement](CLA.md).

In short:

- **You keep the copyright in your contribution.** The CLA grants a *non-exclusive*
  licence — it does not assign or transfer your copyright, and you stay free to use
  and license your own code however you like.
- **Your contribution stays public under EUPL-1.2.** It will always be available to
  everyone under that licence.
- **The copyright holder may additionally offer the project under other licences**
  (for example, a proprietary or OEM licence) alongside the public EUPL-1.2
  release. This is what the CLA's non-exclusive grant allows.

You accept these terms by signing off your commits. **Every commit must carry a
`Signed-off-by` trailer.** By adding it you certify the
[Developer Certificate of Origin](DCO.txt) and agree to the [CLA](CLA.md).

Sign off automatically with `-s`:

```sh
git commit -s -m "Your message"
```

This appends a line in the form:

```text
Signed-off-by: Your Name <your.email@example.com>
```

Use your real name and an email you can be reached at; it must match the author
identity of the commit. If you contribute in the course of your employment or
otherwise on behalf of an employer or other legal entity, your sign-off also
confirms you are authorised to grant the rights on its behalf (see the
[CLA](CLA.md), Section 3) — no separate paperwork is required.

To sign off every commit in this clone automatically, enable the bundled Git
hook once:

```sh
git config core.hooksPath .githooks
```

It adds the `Signed-off-by` trailer when you forget `-s` — it never duplicates an
existing one and leaves merge and squash messages untouched.

### If the DCO check fails

The check verifies **every commit** in the pull request, so it can fail on an
earlier commit even when your latest one is signed. Add the sign-off to all the
commits on your branch and update the pull request:

```sh
git rebase --signoff origin/master
git push --force-with-lease
```

To fix only the most recent commit, amend it instead:

```sh
git commit --amend --signoff --no-edit
git push --force-with-lease
```

## How to send changes

- Open a pull request against the `master` branch.
- Keep the repository language **English** (code, comments, docs, commit messages).
- Keep changes small and focused; one logical change per pull request.
- Run the linters before opening the pull request:

  ```sh
  make lint
  ```

- Add or update tests when the project has them and your change affects behaviour.

## Reporting issues

Open an issue describing what you expected, what happened, and how to reproduce it.
Never include passphrases, private keys, or other secrets in an issue, a log
excerpt, or a pull request.
