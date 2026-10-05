# Kimi quota recovery

Approved plan: keep the last successful subscription gauges visibly stale after
network or credential failures, then replace them after a successful read.

## Implementation

Combine the session, tile, and read-only quota work from PRs #374, #393, #394.
Kimi Code retains exclusive ownership of OAuth refresh and credential writes.
Check quota TTL and credential changes before adaptive local-provider backoff.
Reuse parsed unchanged session files within each provider instance to prevent
repeated history scans from exhausting the eight-second fetch budget. Keep
stale quota visible in the compact provider list even during a failed poll.
Record `quota_state` and `quota_fetched_at` in snapshot attributes and retain
failure diagnostics. Include freshness transitions in daemon change detection.
The daemon read model borrows only the three subscription metrics and their
reset timestamps from a successful poll of the same provider/account in SQLite.
Current session statistics, status, errors, and snapshot time remain current.
No public interface or database migration is needed.

Show stale state and original observation time next to gauges in both tile and
detail views, even when Other Data is hidden. Expired windows retain the old
value as a previous-window observation without forecasts or automatic zeroing.
Show quota unavailable with a reason when no successful history exists.

## Acceptance and rollout

Cover timeout/recovery, CLI token rotation, idle sessions, adaptive backoff,
daemon restart, unchanged percentages, reset boundaries, and account isolation.
Run affected package tests with race detection, vet, build, and docs validation.
Publish the fix for review. Build a local integration preserving Codex changes,
back up the installed binary, atomically install it, and restart telemetry.
Verify through the read model and dashboard. Live percentages require a valid
access token supplied by Kimi Code itself.
