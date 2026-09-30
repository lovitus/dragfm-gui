# Requirements ledger

The original communication baseline remains in [USER_REQUIREMENTS.md](USER_REQUIREMENTS.md). The migration-era ledger mixed “implemented” with historical tests; the executable [ACCEPTANCE.md](ACCEPTANCE.md) mapping and Issue #3 A–P retain the acceptance scope. Neither a mapping nor a green workflow automatically closes a requirement.

The user's later delivery decision takes precedence: the current candidate is **macOS arm64 only**, verified by the unified `build.yml` chain, its three real-SSH shards and independent extracted-Mac native acceptance. Its exact source, original executable, complete checksums, license texts and separate source-bound acceptance receipt are required. This technical chain does not replace Issue #3's full behavior audit or the user's acceptance; other-platform jobs and publication must not be triggered merely to satisfy an older six-platform sentence.

When a full-platform public release is subsequently requested, the retained contract also requires all six jobs in `platforms.yml`, all six extracted-package native checks and uploaded-asset checksum verification in `release.yml`, for the exact revision. That release contains the corresponding source, licenses and both test-evidence archives. The current candidate workflow does not merge or publish.

The current ncat implementation safely replays through SOCKS, SSH relay and official Hans. The old restriction to direct ncat no longer describes the code. Native platform acceptance is no longer deferred or replaced by cross-compilation. Publisher signing/notarization still requires external credentials; see [UNFINISHED.md](UNFINISHED.md).
