#!/usr/bin/env bash
set -e

if ! command -v inotifywait >/dev/null 2>&1; then
  echo "Error: inotifywait is required for dev.sh (install with: sudo apt install inotify-tools)" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec python3 -u "$SCRIPT_DIR/dev.py" "$@"
