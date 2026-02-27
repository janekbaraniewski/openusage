#!/usr/bin/env bash
# openusage-integration-version: __OPENUSAGE_INTEGRATION_VERSION__
set -euo pipefail

case "${OPENUSAGE_TELEMETRY_ENABLED:-true}" in
  0|false|False|FALSE|no|No|NO|off|Off|OFF) exit 0 ;;
esac

# Read stdin via perl — Node.js parents (Claude Code, Cursor) may leave
# stdin in O_NONBLOCK mode, causing cat/read to busy-spin at 100% CPU.
# Perl clears the flag, reads with proper blocking I/O, and alarm()
# provides a hard 30s timeout with zero background processes.
payload="$(perl -MFcntl -e '
  fcntl(STDIN, F_SETFL, fcntl(STDIN, F_GETFL, 0) & ~O_NONBLOCK);
  alarm(30);
  local $/;
  my $d = <STDIN>;
  print $d if defined $d;
' 2>/dev/null)" || true

[[ "$payload" =~ [^[:space:]] ]] || exit 0

bin="${OPENUSAGE_BIN:-__OPENUSAGE_BIN_DEFAULT__}"
[[ -n "${bin}" && "${bin}" != " " ]] || bin="openusage"

cmd=("$bin" "telemetry" "hook" "claude_code")
[[ -z "${OPENUSAGE_TELEMETRY_ACCOUNT_ID:-}" ]] || cmd+=("--account-id" "$OPENUSAGE_TELEMETRY_ACCOUNT_ID")
[[ -z "${OPENUSAGE_TELEMETRY_DB_PATH:-}" ]]    || cmd+=("--db-path" "$OPENUSAGE_TELEMETRY_DB_PATH")
[[ -z "${OPENUSAGE_TELEMETRY_SPOOL_DIR:-}" ]]  || cmd+=("--spool-dir" "$OPENUSAGE_TELEMETRY_SPOOL_DIR")

case "${OPENUSAGE_TELEMETRY_SPOOL_ONLY:-0}" in 1|true|True|TRUE|yes|Yes|YES|on|On|ON) cmd+=("--spool-only") ;; esac
case "${OPENUSAGE_TELEMETRY_VERBOSE:-0}" in    1|true|True|TRUE|yes|Yes|YES|on|On|ON) cmd+=("--verbose") ;; esac

case "${OPENUSAGE_TELEMETRY_VERBOSE:-0}" in
  1|true|True|TRUE|yes|Yes|YES|on|On|ON)
    printf '%s' "$payload" | "${cmd[@]}" ;;
  *)
    printf '%s' "$payload" | "${cmd[@]}" >/dev/null 2>&1 || true ;;
esac
