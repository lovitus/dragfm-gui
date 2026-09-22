# FlySSH vendored patch

dragfm-gui consumes `github.com/flyssh/flyssh` at the commit recorded in
`go.mod` and `vendor/modules.txt`. One narrow connector correction is applied
to the vendored public package:

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
