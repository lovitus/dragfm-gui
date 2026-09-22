# v0.2.0-rc.2 follow-up: passwordless sudo completion

The post-merge repetition of the rc.1 race suite exposed a real edge case in the credential-only `SudoLocal.run` path. A fast NOPASSWD/cached-ticket sudo command could finish successfully without reading a supplied password. A synchronous password write could then return EPIPE, which was incorrectly reported as an operation failure before inspecting the actual successful exit status. This could make a protected-path transfer fail during chmod/chown despite successful filesystem work.

The fix gives `os/exec` ownership of the finite stdin reader and process wait for these non-streaming commands. Its standard unused-pipe handling does not replace the real command exit result. Nonzero authentication/command results and context cancellation still fail. Framed filesystem data streams are unchanged and remain separate from optional credentials.

A deterministic regression uses a disposable fake sudo that closes stdin, with synthetic input larger than the pipe capacity. It tests both successful and nonzero child exits. The existing framed writer test still checks both password-consuming and non-consuming cases. The release validation additionally repeats the sudo tests under `-race` twenty times.

rc.1 is retained as a historical release; rc.2 is a new tag with new binaries and complete native/SSH/extracted-package evidence, not a silent replacement of published files. Publisher signing/notarization and runtime boundaries remain as documented in RELEASE_NOTES.md.
