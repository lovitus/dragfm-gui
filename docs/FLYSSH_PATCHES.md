# FlySSH vendored patch

dragfm-gui consumes `github.com/flyssh/flyssh` at the commit recorded in
`go.mod` and `vendor/modules.txt`. The following corrections are applied to
the vendored public packages; they must be retained on dependency refresh:

- Vault private-key signers and SSH-agent signers are combined into one
  `publickey` authentication method.
- A reachable SSH agent that returns no signers is ignored.
- SSH-agent sockets are closed immediately after each SSH handshake.
- SOCKS and SSH-hop dials that finish after cancellation/timeout have their
  late connection closed instead of leaking it.

Go's SSH client will not retry a second authentication method with the same
method name. Previously, an empty agent (or an agent whose keys were rejected)
could prevent valid vault private keys from ever being offered. The private
keys remain first, followed by agent identities, then password and
keyboard-interactive authentication.

This file contains no forked protocol behavior; it records the source delta
that must be retained when refreshing the vendored FlySSH revision.

## Default SCP file umask (limited hosted behavior validated in 7ed)

Without `-p`, `receiveFile` previously called `Chmod` with the source mode
unconditionally after copying. That can broaden a new file from the actual
caller-umask mode `0640` to source mode `0644`. The latest upstream HEAD
`9f299339930bbb7589ad0596f699d0d5ba7a5bc9` was inspected on 2026-09-30 and still
has this branch, so upgrading alone does not supply the correction.

The local patch passes the existing preserve flag into the receiver and only
restores source mode for explicit `-p`. Default creation continues to use
OpenFile's actual process umask; existing file permissions are not reset. It
does not change global umask, the wire protocol, authentication or GUI SCP's
explicit preserve request. Reference: [OpenSSH sink file-mode handling](https://github.com/openssh/openssh-portable/blob/master/scp.c).

The same public FromOptions/Run driver checks actual content, non-preserving
new/retained directory modes and new file permissions in an isolated child
process. A hosted contrast restores only the real old unconditional chmod;
only correct content/directory modes followed by actual `0644` instead of
`0640` count as the designated red. Unchanged pinned upstream SCP tests must
also pass, including `-p`. These are library contracts through real loopback
SSH/system scp, not an external FlySSH CLI executable or an upstream patch
acceptance claim. In [7ed run36694314681](https://github.com/lovitus/dragfm-gui/actions/runs/36694314681), both designated owner-write/file-umask
withdrawals were old-red/current-green and the pinned upstream preserve tests
passed. The full run still failed later in the Mac native double-click flow,
so this evidence does not approve A–P or the application artifact. Current
exact runtime evidence belongs in CURRENT_TASK.md.

The unchanged pinned upstream/default-directory contracts ran again in
[749 run36698319439](https://github.com/lovitus/dragfm-gui/actions/runs/36698319439):
27 cases passed, with no failed test. The full macOS main and extracted-binary
flows also passed in that run. The two completed designated contrasts were
not rerun; no external CLI executable or four-architecture runtime acceptance
is inferred from these library contracts.

## SCP lifecycle (Issue #3 batch, not yet runtime-validated)

- Directory receive keeps owner traversal/write access until children finish,
  then restores the requested `-p` mode and timestamp. Previously MkdirAll alone
  allowed umask to strip permissions even from empty directories, and ignored
  Chtimes errors. Without `-p`, effective umask and pre-existing modes remain.
  The existing hosted initiating-user pull regression reproduced the mode loss;
  b630ad2's candidate/withdrawn-vendor comparison passed in hosted run
  [36589938706](https://github.com/lovitus/dragfm-gui/actions/runs/36589938706).
  This proves that directory-mode regression, not every SCP behavior. Reference:
  [OpenSSH sink directory handling](https://github.com/openssh/openssh-portable/blob/master/scp.c).
  FlySSH upstream master still lacked the restoration on 2026-09-29; no version
  upgrade is claimed to solve it.
- Header parsing removes only the protocol LF, preserving trailing spaces
  and tabs in file/directory names. Upstream `master` still used TrimSpace
  when reviewed on 2026-09-29; distinct `report` / `report ` siblings could
  collide during download. Application preflights exclude CR/LF names from
  SCP and retain the existing lossless stream fallback. A real queued
  OpenSSH-to-local regression is prepared, not yet run.
- `Spec.RemoteCommandPrefix` optionally prefixes the generated remote SCP
  command with individually shell-quoted argv. dragfm uses its task-owned
  helper's `--transfer-server` mode so the real system SCP process holds the
  same installation lease as the receiver's cleanup service. Nil preserves
  the FlySSH CLI's original command. The SCP wire protocol is unchanged.
- `ExitUnconfirmedError` retains both the original protocol/I/O failure and a
  missing remote exit result. It unwraps its cause and is non-retryable for
  callers that own staging paths. Previously `finishSession` could discard
  `Wait`'s missing-exit error whenever a prior SCP error existed.
- `RunContext` joins cancellation with the actual transfer error rather than
  claiming connection closure proves that a remote child exited. Exit codes
  and ordinary CLI operation remain unchanged; failure diagnostics preserve
  more detail. No credential is added to arguments, environment or files.

Upstream `master`'s public `pkg/transfer/scp.go` and `spec.go` were inspected
while designing this change; they did not expose this supervision prefix or
preserve missing-exit evidence in the prior-error branch. This is an explicit
local vendor delta, not a claim that FlySSH upstream has accepted the patch.
The prepared hosted SCP/rsync peer-disconnect regression and existing normal
direction/method flows still need behavioral old-red/new-green evidence.

## Noninteractive initiating SSH agent (unverified batch)

`Credentials.AgentTimeout` optionally bounds the Unix-socket connection and
agent operations. The remote transfer helper sets five seconds, so a stale
socket cannot indefinitely prevent the transfer queue from advancing. Zero
retains the existing CLI/controller timeout policy; no new environment or
credential file is created. The actual initiating UID/socket ownership and
explicit per-hop permission are checked by dragfm before enabling this API.
This is an additive vendored API, not an upstream feature claim. Signer
enumeration failure still leaves configured key/password methods available;
runtime signing/connection failure remains an SSH error, not fake success.
