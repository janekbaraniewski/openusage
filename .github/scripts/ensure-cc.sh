#!/usr/bin/env bash
# Ensure a C compiler is available for CGO (mattn/go-sqlite3).
#
# The GitHub ubuntu-latest image already ships gcc, so the apt path is a
# fallback for other runners rather than the normal case. It matters: an
# unconditional `apt-get update` refreshes every archive index over the
# network, and when a mirror stalls the step hangs indefinitely — which it
# did, twice, blocking Vet and Test while Build happened to get through.
set -euo pipefail

if command -v gcc >/dev/null 2>&1; then
  echo "gcc already present: $(gcc --version | head -1)"
  exit 0
fi
if command -v cc >/dev/null 2>&1; then
  echo "cc already present: $(cc --version | head -1)"
  exit 0
fi

echo "No C compiler found; installing gcc."
export DEBIAN_FRONTEND=noninteractive
# Bound both apt calls so a stalled mirror fails the step instead of hanging.
timeout 300 sudo -E apt-get update
timeout 300 sudo -E apt-get install -y --no-install-recommends gcc
gcc --version | head -1
