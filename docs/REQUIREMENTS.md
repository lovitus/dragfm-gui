# dragfm-gui completion ledger

> Migration correction: “Implemented” below means code exists, NOT user acceptance.
> Historical test descriptions are previous reports, not freshly reproduced evidence.
> The current acceptance baseline is [USER_REQUIREMENTS.md](USER_REQUIREMENTS.md),
> unresolved work is [UNFINISHED.md](UNFINISHED.md), and the sole task cursor is
> [CURRENT_TASK.md](CURRENT_TASK.md). No complete-production-readiness claim is made.

This file is the implementation gate for the original GUI plan. A row may be
marked complete only when the product path is reachable and the listed
evidence exists; embedded assets or unused framework code do not count.

| Area | Requirement | Current state | Completion evidence required |
|---|---|---|---|
| GUI | Two independent local/SSH panes with editable paths, sortable file metadata and persistent PTYs | Implemented | Component tests, local/SSH PTY tests and browser QA at default and 1024x700 viewports; native cross-platform smoke remains a release gate |
| GUI | Drop onto a directory row or the open directory; copy/move/cancel and overwrite variants | Implemented | Pointer-event component test plus real browser drag into a directory row and confirmation-modal inspection |
| GUI | Backspace parent navigation, `d` delete and `h` SHA-256 without stealing editor/terminal input | Implemented | Focus-scope unit/component coverage for body, rows, inputs, terminal, CodeMirror and dialogs |
| GUI | Refresh file list after every pane-terminal command, delete, transfer and path change | Implemented | Prompt refresh component test; PTY cwd marker and filesystem operation tests |
| Queue | Exactly one running task; Pending visible and not restored; redacted History restored | Implemented | Queue concurrency/cancel/progress tests, restart and credential-redaction tests; browser QA verifies Running, Pending, History and the event timeline remain simultaneously visible with no overflow |
| Config | Single no-blank-line Markdown format for hosts, vault keys and SOCKS pool | Implemented | Exact sample, metadata and identity-preserving round trips; physical line-number/no-wrap editor QA |
| Vault | Mandatory hint/password, Argon2id, XChaCha20-Poly1305, atomic current-user-only persistence and path fallback | Implemented | Crypto/error/path/interrupted-save tests; POSIX 0600 plus Windows protected-DACL implementation and cross-compile |
| SSH | Keys, agent, inline/runtime/saved passwords, keyboard-interactive and per-hop TOFU pinning | Implemented | In-process real SSH auth/SFTP suite, changed-fingerprint rejection and vendored auth-order tests |
| Browse | SFTP first and safe POSIX fallback | Implemented | Real SSH server tests with SFTP accepted and rejected |
| Preflight | Absolute paths, conflict, free space, source inode/size/mtime, identity, architecture and capabilities | Implemented | Unit and real Linux SSH E2E; ELF, SFTP statvfs and SFTP stat-file fallbacks cover shell stdout loss |
| Same host | `cp` for copy and `mv` for move, protected paths through scoped sudo | Implemented | Local and real SSH directory-merge cp plus same-host mv E2E; local protected-path sudo tests |
| Direct | Current-user push, reverse pull, source elevation push and target elevation pull | Implemented | Real bidirectional SCP/stream and configured-root-route E2E; scoped sudo protocol/ownership unit integration |
| Methods | rsync, built-in SCP, authenticated encrypted agent stream, then direct tar+ncat | Implemented with a safety restriction | Exact ordering tests; real SCP, rsync-through-pools, encrypted directory/symlink and bidirectional direct ncat E2E. Plaintext ncat is not replayed through credentialed pools because doing so with the system CLI would expose proxy credentials in process arguments. |
| SOCKS | Three probes from the actual initiator, median sort, success memory, secure-method strategy replay | Implemented | Authenticated SOCKS5 cross-host E2E and persisted success/RTT assertion; replays rsync, SCP and the authenticated encrypted stream, not plaintext ncat. |
| SSH pool | Cached successful relay first, bounded handshake probing, secure-method strategy replay | Implemented | Three-runner relay E2E and host-pair cache assertion; cached failure removal path; replays rsync, SCP and the authenticated encrypted stream. |
| Hans | Root-capable role selection, userspace SOCKS peer, random network/password, pinned v5 identity, role reversal | Implemented | Official v1.7.0 privileged-container v5 data-plane test and two-host GUI orchestration E2E |
| Relay | Final controller source stream -> bounded memory -> destination partial, no controller disk | Implemented | 256 KiB buffer, cancellation/staging tests and real remote/remote relay E2E |
| Move | SHA-256 manifest equality, root inode equality and unchanged source snapshot before deletion | Implemented | Local and remote target-pull move E2E; replacement-inode and changed-source tests |
| Cleanup | Cancel closes sessions/listeners/agents/Hans and removes only owned temporary resources | Implemented | Context/listener/SOCKS/partial tests, Linux parent-death lifecycle, ownership-marker tests and post-E2E zero-resource audit |
| Release | Current validation artifact: Wails single-file macOS arm64 with checksum | Awaiting user validation | Arm64 Mach-O, macOS 11.0 deployment floor, ad-hoc signature and checksum verified; full six-platform matrix intentionally deferred until the UI is accepted |
