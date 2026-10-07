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

## Completed validation (2026-10-05)

Affected Kimi, detect, daemon, telemetry, and TUI race tests passed. The local
integration also passed Codex race tests, vet, and the CGO build. The documentation
build succeeded without broken links. golangci-lint is not installed locally.
A real-history probe took about three seconds cold and 25 milliseconds warm.
The installed daemon recovered stale quota from existing SQLite history, then
returned fresh subscription metrics after Kimi Code renewed its own access token.
The new dashboard showed both the five-hour and monthly gauges. The previous
binary was backed up before installation; the local integration retains the
existing Codex quota changes.

## Maintenance regression (2026-10-07)

Raw-payload compaction originally retained only the latest poll. An expired-token
poll therefore caused the previous successful quota to be blanked after one
hour, breaking the promised fallback after a restart. Maintenance now retains
at most the newest poll plus the latest successful Kimi quota for each account;
normal event retention still applies. Recovery and maintenance share the same
successful-payload predicate. Older successful payloads remain reclaimable.
Read-model cache refresh has a one-minute maximum age even without new ingest.
Cached Kimi quota older than two minutes is served visibly stale without mutating
the stored cache. Lifecycle coverage includes successful quota, auth failure,
payload cleanup, restart, another success, another failure, and bounded storage.
An HTTP regression checks that a stale cache cannot advertise old quota as fresh
and refreshes current activity/errors despite an unchanged data version.
