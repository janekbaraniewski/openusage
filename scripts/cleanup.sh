#!/bin/bash
# Cleanup script: run this manually to complete deletion
set -euo pipefail

# Delete dev scripts
rm -f /home/nurul/openusage/scripts/dev.sh
rm -f /home/nurul/openusage/scripts/dev.py
rm -f /home/nurul/openusage/scripts/__pycache__/dev.cpython-314.pyc

# Kill matching processes (but NOT 'openusage telemetry' or 'openusage serve')
for pattern in 'dev\.sh' 'dev\.py' 'openusage-dev' 'inotifywait'; do
    pids=$(pgrep -f "$pattern" 2>/dev/null || true)
    for pid in $pids; do
        cmdline=$(cat /proc/$pid/cmdline 2>/dev/null | tr '\0' ' ' || true)
        # Skip openusage telemetry and openusage serve
        if echo "$cmdline" | grep -qE 'openusage (telemetry|serve)'; then
            echo "SKIP pid=$pid ($cmdline)"
            continue
        fi
        echo "KILL pid=$pid ($cmdline)"
        kill "$pid" 2>/dev/null || true
    done
done

# Verify
echo ""
echo "=== Verification ==="
for f in /home/nurul/openusage/scripts/dev.sh /home/nurul/openusage/scripts/dev.py; do
    if [ -e "$f" ]; then
        echo "STILL EXISTS: $f"
    else
        echo "GONE: $f"
    fi
done

remaining=$(pgrep -f 'dev\.sh|dev\.py|openusage-dev|inotifywait' 2>/dev/null || true)
if [ -z "$remaining" ]; then
    echo "No matching processes running."
else
    echo "Remaining PIDs: $remaining"
fi

# Self-delete
rm -f /home/nurul/openusage/scripts/cleanup.sh
echo "Done."
