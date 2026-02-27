#!/usr/bin/env bash
# openusage-integration-version: __OPENUSAGE_INTEGRATION_VERSION__
set -euo pipefail

parse_bool() {
  local value="${1:-}"
  local default_value="${2:-false}"
  local normalized
  normalized="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]' | xargs)"
  if [[ -z "$normalized" ]]; then
    [[ "$default_value" == "true" ]] && return 0 || return 1
  fi
  case "$normalized" in
    1|true|yes|on) return 0 ;;
    0|false|no|off) return 1 ;;
    *)
      [[ "$default_value" == "true" ]] && return 0 || return 1
      ;;
  esac
}

if ! parse_bool "${OPENUSAGE_TELEMETRY_ENABLED:-true}" "true"; then
  exit 0
fi

payload="${1:-}"
if [[ -z "${payload//[[:space:]]/}" ]]; then
  # Read stdin via perl to avoid busy-spinning. Node.js parents may leave
  # stdin in O_NONBLOCK mode, which causes cat/read to spin at 100% CPU.
  # Perl's buffered I/O handles this correctly after clearing the flag.
  payload="$(perl -MFcntl -e '
    fcntl(STDIN, F_SETFL, fcntl(STDIN, F_GETFL, 0) & ~O_NONBLOCK);
    alarm(30);
    local $/;
    my $d = <STDIN>;
    print $d if defined $d;
  ' 2>/dev/null)" || true
fi
if [[ -z "${payload//[[:space:]]/}" ]]; then
  exit 0
fi

default_openusage_bin="__OPENUSAGE_BIN_DEFAULT__"
openusage_bin="${OPENUSAGE_BIN:-$default_openusage_bin}"
if [[ -z "${openusage_bin//[[:space:]]/}" ]]; then
  openusage_bin="openusage"
fi

account_id="${OPENUSAGE_TELEMETRY_ACCOUNT_ID:-}"
db_path="${OPENUSAGE_TELEMETRY_DB_PATH:-}"
spool_dir="${OPENUSAGE_TELEMETRY_SPOOL_DIR:-}"
verbose_env="${OPENUSAGE_TELEMETRY_VERBOSE:-false}"

cmd=("$openusage_bin" "telemetry" "hook" "codex")
if [[ -n "${account_id//[[:space:]]/}" ]]; then
  cmd+=("--account-id" "$account_id")
fi
if [[ -n "${db_path//[[:space:]]/}" ]]; then
  cmd+=("--db-path" "$db_path")
fi
if [[ -n "${spool_dir//[[:space:]]/}" ]]; then
  cmd+=("--spool-dir" "$spool_dir")
fi
if parse_bool "${OPENUSAGE_TELEMETRY_SPOOL_ONLY:-false}" "false"; then
  cmd+=("--spool-only")
fi
if parse_bool "$verbose_env" "false"; then
  cmd+=("--verbose")
fi

if parse_bool "$verbose_env" "false"; then
  printf '%s' "$payload" | "${cmd[@]}"
else
  if ! printf '%s' "$payload" | "${cmd[@]}" >/dev/null 2>&1; then
    exit 0
  fi
fi
