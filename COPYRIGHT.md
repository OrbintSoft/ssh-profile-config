# Copyright

Copyright © 2026 Stefano Balzarotti (OrbintSoft) and contributors.

    Licensed under the EUPL

## Licence

This project is released under the **European Union Public Licence v. 1.2**
(`EUPL-1.2`). The full, authoritative text is in [LICENSE](LICENSE). The list of
contributors is in [AUTHORS.md](AUTHORS.md).

## Contributions

Contributors keep the copyright in their contributions. By contributing they
license their work to the public under the EUPL 1.2 and grant the copyright holder
a *non-exclusive* licence under the [Contributor License Agreement](CLA.md) — there
is no copyright assignment. See [CONTRIBUTING.md](CONTRIBUTING.md) for how this is
accepted (a `Signed-off-by` trailer certifying the [DCO](DCO.txt) and the CLA).

## Third-party components

Compiled into the distributed binary:

- **Go standard library** — BSD-3-Clause.
- **golang.org/x/sys** — BSD-3-Clause (Linux kernel keyring and syscall access).

Both are BSD-3-Clause: compatible with the EUPL 1.2 and with offering the project
under additional licences, so they do not obstruct relicensing. Build- and
CI-only tools (the Go toolchain and the linters) run as separate processes, are
neither bundled nor distributed, and impose no terms on the software.

## Relicensing

The project's public release is, and will remain, under the EUPL 1.2. In addition,
the copyright holder may distribute the project under other licences — for example
a proprietary or OEM licence — alongside the public EUPL 1.2 release. The CLA's
non-exclusive grant covers contributors' work for this purpose, so no contribution
has to be removed or re-negotiated.

Preserving that freedom is a project rule: before any third-party code or
dependency is introduced, its licence is checked for compatibility with the
EUPL 1.2 and with the ability to offer the project under additional licences.
