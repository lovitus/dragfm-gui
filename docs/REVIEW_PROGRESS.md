# Review progress — 2026-09-17

The interrupted review has resumed. This is an evidence checkpoint, not final acceptance.

## Committed

- CodeMirror state dependency deduplicated; reproducible frontend builds restored.
- Queue cancellation/admission serialized; bounded events, authoritative revisioned snapshots, final counters and panic isolation.
- Frontend no longer fabricates successful native operations when the Wails bridge is absent.
- Native JobSnapshot and TerminalReady RPCs integrated; an API-contract regression checks every frontend RPC name.
- Lock drains final job state and persists redacted history before dropping the vault key. Unlock creates a new queue and rejects stale-session work.
- PTY output starts only after the frontend acknowledges its session ID; cwd feedback and stale-session updates are guarded.
- Configuration password visibility no longer erases the draft; frontend action failures are surfaced.
- Failed native copies and failed final renames no longer delete the old destination. Directory merges use atomic per-file writes.
- Command output is bounded and concurrency-safe; configured bare secrets are redacted before job/history serialization.

## Evidence so far

- `0f5f6da`: Actions run 35217545505 passed Go unit/race/vet, frontend tests/build, macOS arm64 compilation.
- `0f6c55b`: recovered frontend passed run 35242382987. This alone did NOT prove native RPC integration; the added contract test closes that gap.
- `0501334`: reviewed backend source recovered with SHA256 verification. Hosted race run 35244883845 passed the new lifecycle/safety/PTY/contract tests, but failed one old directory-merge verification-label assertion. That assertion is being updated without removing file-preservation checks.

## Still required

Real OpenSSH transfer/queue/history tests on disposable GitHub-hosted fixtures; actual native GUI acceptance; complex route/sudo/Hans cancellation coverage; release workflow and artifact verification. Existing UNFINISHED.md acceptance boxes remain unclaimed until corresponding evidence exists. Repository visibility remains private.
