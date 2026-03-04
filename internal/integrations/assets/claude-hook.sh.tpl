#!/usr/bin/env bash
# openusage-integration-version: __OPENUSAGE_INTEGRATION_VERSION__
set -euo pipefail

case "${OPENUSAGE_TELEMETRY_ENABLED:-true}" in
  0|false|False|FALSE|no|No|NO|off|Off|OFF) exit 0 ;;
esac

# Pure bash — no Perl, no external commands after mkdir.
# Writes hook payload to spool file; daemon picks up every 5s.
# Single process spawn (~250ms macOS overhead), zero CPU work.
IFS= read -r -d '' payload 2>/dev/null || true
[[ -z "${payload:-}" || "${#payload}" -lt 2 ]] && exit 0

dir="${OPENUSAGE_HOOK_SPOOL:-${XDG_STATE_HOME:-$HOME/.local/state}/openusage/hook-spool}"
[[ -d "$dir" ]] || mkdir -p "$dir" 2>/dev/null || exit 0

acct="${OPENUSAGE_TELEMETRY_ACCOUNT_ID:-}"
printf '{"source":"claude_code","account_id":"%s","payload":%s}\n' "$acct" "$payload" \
  > "$dir/$$$RANDOM.json" 2>/dev/null
