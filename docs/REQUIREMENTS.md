# Requirements ledger

The original communication baseline remains in [USER_REQUIREMENTS.md](USER_REQUIREMENTS.md). The migration-era ledger mixed “implemented” with historical tests; it is superseded by the executable [ACCEPTANCE.md](ACCEPTANCE.md) mapping.

A release candidate is complete only when the exact source revision passes `build.yml`, `ssh-integration.yml`, all six jobs in `platforms.yml`, all six extracted-package native checks, and uploaded-asset checksum verification in `release.yml`. The public release contains the corresponding source, licenses and both test-evidence archives.

The current ncat implementation safely replays through SOCKS, SSH relay and official Hans. The old restriction to direct ncat no longer describes the code. Native platform acceptance is no longer deferred or replaced by cross-compilation. Publisher signing/notarization still requires external credentials; see [UNFINISHED.md](UNFINISHED.md).
