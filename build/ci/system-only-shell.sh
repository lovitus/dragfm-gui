#!/usr/bin/env bash
# Disposable SSH fixture policy: system commands work, uploaded dragfm helpers
# cannot execute. This is a real login shell, not a transfer Endpoint substitute.
# Avoid ForceCommand: OpenSSH restricts signal requests for forced commands,
# which would also change the cancellation behavior being exercised.
set -eu
# Opt-in I/O-failure injection for the disposable tar+ncat move regression.
# Real SSH, extraction, metadata, rename and cleanup still run normally.
if [[ "${1:-}" == '-c' && "${2:-}" == *'command sync -f -- '* && -f "$HOME/.sync-failure-mode" ]]; then
  attempt=0
  [[ ! -f "$HOME/.sync-attempts" ]] || read -r attempt <"$HOME/.sync-attempts"
  attempt=$((attempt + 1))
  printf '%s\n' "$attempt" >"$HOME/.sync-attempts"
  read -r fail_at <"$HOME/.sync-failure-mode"
  if [[ "$attempt" == "$fail_at" ]]; then
    printf 'fixture sync I/O failure\n' >&2
    exit 74
  fi
fi
if [[ "${1:-}" == '-c' && "${2:-}" == *'/dragfm-agent'* ]] &&
   { [[ "${USER:-}" == 'systemtester' ]] || [[ -f "$HOME/.deny-helper" ]]; }; then
  # Opt-in real cleanup refusal: an independently surviving child holds the
  # installation lock before this exec is rejected. The controller must not
  # hide that PreserveSource error behind a successful system-file fallback.
  if [[ -f "$HOME/.hold-denied-helper" && "${2:-}" =~ /tmp/(\.dragfm-[0-9a-f]{32})/dragfm-agent ]]; then
    held_directory="/tmp/${BASH_REMATCH[1]}"
    coproc HELD_LEASE {
      exec 9<"$held_directory"
      flock -s 9
      printf 'ready\n'
      exec sleep 120 </dev/null >/dev/null 2>&1
    }
    held_pid=$HELD_LEASE_PID
    IFS= read -r held_ready <&"${HELD_LEASE[0]}"
    [[ "$held_ready" == ready ]]
    printf '%s\n' "$held_pid" >"$HOME/.held-helper-pid"
    printf '%s\n' "$held_directory" >"$HOME/.held-helper-path"
    disown "$held_pid"
  fi
  printf 'denied\n' >"$HOME/.helper-denied"
  printf 'fixture policy denies uploaded helpers\n' >&2
  exit 126
fi
exec /bin/bash "$@"
