# SSHakku

Tends your SSH agent so every shell can use SSH without retyping the passphrase:
it starts and watches the agent (lifecycle, health checks, diagnostics, recovery)
and loads your keys, pulling each passphrase from the OS secret store.

## Configuration

Key lifetime, retry, and notification behaviour are tuned through environment
variables — see [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

## Diagnostics

`sshakku doctor` reports the state of your SSH agent, and `sshakku doctor --fix`
repairs it — see [docs/DIAGNOSTICS.md](docs/DIAGNOSTICS.md).

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). You keep the
copyright in your work; contributions are released under the EUPL-1.2 and covered
by the [Contributor License Agreement](CLA.md).

## License

Copyright © 2026 Stefano Balzarotti (OrbintSoft) and contributors.
Licensed under the [European Union Public Licence v. 1.2](LICENSE) (`EUPL-1.2`).
The public release stays EUPL-1.2; the copyright holder may additionally offer the
project under other licences. See [COPYRIGHT.md](COPYRIGHT.md) and
[AUTHORS.md](AUTHORS.md).
