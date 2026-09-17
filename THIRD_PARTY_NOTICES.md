# Third-party notices

dragfm-gui is licensed under GPL-3.0-only.

- FlySSH (`github.com/flyssh/flyssh`): MIT. Its public `pkg/connector`, `pkg/cli`, `pkg/socks`, and `pkg/transfer` packages are consumed through the vendored module. Copyright and license remain with the FlySSH authors.
- Hans v1.7.0 (`github.com/lovitus/hans`): GPL-3.0. Embedded Linux helpers are the official static musl release binaries, pinned and verified by their GitHub release SHA-256 digests in `build/fetch-linux-hans.sh`. The corresponding source and GPL license are included under `third_party/hans-1.7.0`.
- Fyne v2.8.0 (`fyne.io/fyne/v2`): BSD-3-Clause.
- Fyne Terminal commit `86c23cc49e342a21b263bfe086cacdd410e51d7e` (`github.com/fyne-io/terminal`): BSD-3-Clause.
- Wails v2.12.0 (`github.com/wailsapp/wails/v2`): MIT.
- React, xterm.js, and CodeMirror are used by the Wails frontend. Exact versions are pinned in `frontend/package-lock.json`; their upstream licenses apply.
- Other Go modules and their pinned versions are recorded in `go.mod`, `go.sum`, and `vendor/modules.txt`. Their upstream licenses apply.

No private validation hostnames, credentials, or runner logs are included in this source tree.
