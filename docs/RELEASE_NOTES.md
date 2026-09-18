# dragfm-gui — macOS arm64 development preview

This is the current Wails + React desktop application, not the retained Fyne prototype. It does not replace the original dragfm TUI. This is a release candidate for evaluation, not a declaration that every item in the original migration PR is complete.

## Install and run

Extract `dragfm-gui-darwin-arm64.tar.gz` and run the executable `dragfm-gui/dragfm-gui`. The archive preserves its executable permission. Keep the accompanying licenses. Apple Silicon and an interactive macOS desktop are required. The build targets macOS 11; native acceptance runs on GitHub-hosted macOS 15 arm64, so earlier macOS versions are not separately certified.

The executable has an ad-hoc signature, not an Apple Developer ID signature or notarization. Gatekeeper may therefore require an explicit user decision through macOS security controls. This package does not disable Gatekeeper or change system security settings.

On first launch, create a master password and a nonempty hint. Existing vaults beside the executable take precedence; otherwise the application follows the documented portable/system-data fallback. Back up an existing vault before evaluating a preview.

## Validation and provenance

The release workflow requires Go unit tests, race tests, frontend tests/build, vet, a clean dependency advisory report, real Linux OpenSSH transfer/sudo/proxy/Hans tests, and actual native Wails-window acceptance. The native fixture exercises configuration editing and masking, real SSH, directory drag/drop, simultaneous Running/Pending, copying, verified moves, directory merging, failure/cancellation, hash/delete/Backspace actions, PTYs, process-restart history and changed-host-key rejection. Fixture credentials are generated for each hosted run and are not release assets.

The packaged executable is extracted and exercised again on a separate macOS runner. Uploaded release assets are downloaded and checked before the draft becomes a published prerelease. `PROVENANCE.json` identifies the exact source commit, source archive hash and native-tested executable hash. `TEST_EVIDENCE.zip` retains the validation reports. `SHA256SUMS` covers the distribution assets. All four embedded Linux agents are rebuilt from the controller's source revision; official Hans v1.7.0 assets remain pinned.

## Boundaries

See `UNFINISHED.md` for acceptance gaps. In particular, the full adversarial direction/privilege/fallback matrix is not equivalent to passing individual method fixtures. Proxy/jump/Hans ncat replay, extended prompt/editing stress, every cleanup/failure combination, and six-platform native acceptance are not declared complete. Unknown remote byte progress is shown as indeterminate rather than invented percentages. The old offline Linux/Fyne libGL issue and a server web UI are outside this release.

This workflow publishes a release in the existing repository; it does not change repository visibility. A release in a private repository remains visible only to users who can access that repository.

## License

GPL-3.0-only for dragfm-gui. The distribution includes third-party license notices. The corresponding source archive contains the vendored Go dependencies and official Hans source/license; pinned frontend dependency versions are recorded in the npm lockfile. Source/build instructions are retained in the repository.
