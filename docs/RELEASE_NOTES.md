# dragfm-gui — public six-platform release candidate

This distribution is the Wails + React desktop application in `cmd/dragfm-wails`, not the retained Fyne prototype. It does not replace the original dragfm TUI.

## Choose the matching package

| System | CPU | Package |
|---|---|---|
| macOS | Apple Silicon | `dragfm-gui-darwin-arm64.tar.gz` |
| macOS | Intel | `dragfm-gui-darwin-amd64.tar.gz` |
| Linux | x86-64 | `dragfm-gui-linux-amd64.tar.gz` |
| Linux | AArch64 | `dragfm-gui-linux-arm64.tar.gz` |
| Windows | x86-64 | `dragfm-gui-windows-amd64.zip` |
| Windows | ARM64 | `dragfm-gui-windows-arm64.zip` |

Extract the archive and launch `dragfm-gui/dragfm-gui` or `dragfm-gui/dragfm-gui.exe`. Keep the accompanying license files. POSIX archives preserve executable permission. Back up an existing vault before evaluating a release candidate.

The executable embeds the application frontend and four Linux helper architectures. A single application executable is not a statically independent operating-system GUI: Linux requires a desktop session, GTK 3 and WebKitGTK 4.1; Windows requires WebView2; macOS requires an interactive desktop. Linux packages are built/tested on Ubuntu 24.04. macOS targets 11.0 but native acceptance uses macOS 15, so older systems are not separately certified. Windows native acceptance uses the hosted x64 Windows Server 2025 and ARM64 Windows 11 images. This release does not repair the historical offline Fyne/libGL build.

## Signing and trust

macOS executables are ad-hoc signed and checked after extraction. They are **not Apple Developer ID signed or notarized**. Windows binaries are **not Authenticode publisher-signed**. No publisher certificates or notarization credentials have been supplied to this project. Checksums establish byte integrity, not publisher identity. Gatekeeper or SmartScreen may request an explicit user decision; the package never disables these protections.

## Functionality

Two independent local/SSH file panes have editable paths, virtualized metadata rows, directory-aware drag/drop, copy/move/overwrite/merge, Backspace navigation and hash/delete shortcuts. Each pane has a persistent PTY. The global queue admits one task at a time; cancellation, measured activity, Pending and redacted History are visible. History survives restart; pending work is not resumed.

The encrypted Markdown configuration supports FlySSH multihop routes, vault-resident private keys, password masking, per-hop SSH fingerprint confirmation, SOCKS and known successful SSH relays. The vault uses Argon2id and XChaCha20-Poly1305, bounded untrusted header parameters, atomic persistence and current-user permissions.

Linux remote transfers try the documented direction/privilege matrix with rsync, SCP, authenticated encrypted streams and ncat carriers. SOCKS, SSH relay and official Hans v1.7.0 replay all four methods without putting proxy credentials into ncat arguments. Hans is pinned v5, uses an elevated server and a userspace SOCKS client, and supports reversed roles. Final controller relaying uses bounded memory rather than staging file contents locally. Cross-machine moves verify manifests and source identity before deleting the source; detected changes keep the source.

## Release evidence

Publication is conditional on all of these passing for the exact release revision:

* Go unit/race tests, vet, frontend tests and production compilation, and no reported npm vulnerabilities.
* Real Linux OpenSSH integration: 48 direction/privilege/method combinations across direct/SOCKS/SSH relay, eight official-Hans role/method combinations, scoped sudo, real blocked-network fallback, relay-cache invalidation, cancellation latency and cleanup.
* Actual native Wails windows on all six OS/CPU combinations: coordinate hit testing, non-overlapping virtual rows, metadata/terminal geometry, copy/move/hash/delete, quoted-path PTYs, cancellation and process-restart history.
* The larger macOS arm64 real-SSH native suite: simultaneous Running/Pending, configuration editing/masking, file operations, SSH PTYs, sudo downloads into protected local directories, declined-sudo source preservation, process restart and changed-host-key rejection.
* Extraction and native execution of every packaged binary on a second set of hosted runners, followed by download/checksum verification of every uploaded release asset before the draft is published.

`PROVENANCE.json` records the commit, source hash, per-platform executable hashes and workflow run. `TEST_EVIDENCE.zip` contains first-run reports. `PACKAGED_TEST_EVIDENCE.zip` contains extracted-package reports. `SHA256SUMS` covers all distribution assets. A source branch containing this file is not itself proof that publication succeeded; the published release and its evidence are the result.

## Operational boundaries

The test matrix is reproducible acceptance coverage, not proof against every shell customization, network failure or filesystem race. Arbitrary user commands are not time-limited, but cancellation and metadata probes are bounded. Navigation refuses to inject `cd` while the terminal is editing input or running another program. If an endpoint becomes unreachable, immediate remote deletion cannot be guaranteed; cleanup stays limited to owned resources and never deletes unrelated remote paths. Permission checks, security prompts and source-preservation failures are not bypassed to make a workflow green.

The source and notices are included under GPL-3.0-only and the respective third-party licenses. Go dependencies are vendored; frontend versions and integrity hashes are locked. Build/runtime settings are in the retained workflows.
