---
title: Storage and retention
description: SQLite schema, deduplication strategy, provider links, spool, and retention controls for the OpenUsage daemon.
---

# Storage and retention

The daemon persists everything to a single SQLite database with WAL enabled. This page covers the schema, how events are deduplicated, how unreachable hooks are buffered, and how to tune retention.

## Database file

```
~/.local/state/openusage/telemetry.db
```

Pragmas at open:

- `journal_mode = WAL`
- `synchronous = NORMAL`
- `foreign_keys = ON`

Override the path with `--db-path`:

```bash
openusage telemetry daemon run --db-path /var/data/openusage/telemetry.db
```

## Tables

| Table | Purpose |
|---|---|
| `usage_events` | Canonical normalized events. One row per turn, message, tool call, or limit snapshot. |
| `usage_raw_events` | Untouched payload bodies with a schema discriminator. Useful for replay and debugging. |
| `usage_rollup_daily` | Daily downsample of `usage_events` (per day × provider/account/model/tool/project/status). Kept long-term so raw rows past the hot window can be pruned without losing the shape of history. |
| `balance_observations` | Compact numeric time-series of balance/credit metrics per provider/account. |
| `daemon_meta` | Key/value daemon state (e.g. the rollup watermark). |

Event types written into `usage_events.event_type`:

- `turn_completed`
- `message_usage`
- `tool_usage`
- `raw_envelope`
- `limit_snapshot`
- `reconcile_adjustment`

## Deduplication

The same turn can reach the pipeline more than once: a hook may retry, a spool drain may overlap a live POST, or a collector poll may re-observe the same billing snapshot. The pipeline picks a dedup key in priority order:

1. `tool_call_id` — most specific
2. `message_id`
3. `turn_id`
4. SHA256 fingerprint over `(source, account_id, event_type, occurred_at, payload_subset)`

The first key present wins. Subsequent inserts with a matching key are silently dropped.

:::note Why fingerprinting?
Hooks that don't carry a stable id (older tool versions, partial payloads) still need to dedup correctly. The fingerprint hash gives that without forcing every emitter to mint ids.
:::

## Provider links

Hook payloads come tagged with a **source** string from the tool. The TUI displays them under a **provider** id. The bridge is the provider link map.

Defaults:

```
anthropic       → claude_code
google          → gemini_api
github-copilot  → copilot
```

Override in `~/.config/openusage/settings.json`:

```json
{
  "telemetry": {
    "provider_links": {
      "my-custom-source": "openrouter"
    }
  }
}
```

Edit interactively from the Telemetry settings tab (<kbd>,</kbd> then <kbd>6</kbd>, then <kbd>m</kbd>).

## Spool

When a hook fires while the daemon is offline (or the socket is missing), the wrapper writes the payload to disk:

```
~/.local/state/openusage/telemetry-spool/
```

On daemon startup, the pipeline scans the spool, drains every file through the dedup gate, and deletes successfully ingested files.

Cleanup limits applied during drain and during periodic maintenance:

- **MaxAge** — delete spool entries older than the retention window
- **MaxFiles** — cap on total file count
- **MaxBytes** — cap on directory size

Hard-stuck spool files (corrupt JSON, repeated dedup misses) remain on disk until manually removed.

## Retention (downsample and keep)

OpenUsage keeps history bounded **without discarding it**, via two tiers:

- **Hot tier** — full per-event detail in `usage_events`, kept for
  `data.retention_days` (the *hot window*, default `90`). Powers recent/detailed
  views, dedup, drill-down, the 5h block and burn rate.
- **Cold tier** — a per-day aggregate in `usage_rollup_daily` (summed
  tokens/cost/requests with provider/account/model/tool/project/status
  dimensions), kept long-term. Powers 30d, analytics, and all-time totals.

The daemon runs, in order each cycle:

1. **Rollup** — recompute the daily aggregate from the (deduped) raw events and
   advance a watermark to the last fully-settled day. Idempotent: re-running a
   day reproduces the same totals.
2. **Prune-after-rollup** — `PruneOldEvents` deletes raw rows past the hot
   window **only for days at or below the rollup watermark**, in bounded
   batches. So per-event detail is never deleted before its aggregate exists;
   you lose the per-turn timeline beyond the hot window, not the totals or
   breakdowns.
3. **Payload pruning** — `PruneRawEventPayloads` clears the heavy payload blob
   from old raw rows.

```json
{
  "data": {
    "retention_days": 90
  }
}
```

`retention_days` is the hot-window length. It is no longer capped at 90 — set it
as high as you like (e.g. a year) to keep more full detail; the daily rollup
keeps the long-term shape regardless. There is no hard upper wall beyond a
~10-year sanity ceiling.

:::note Late-arriving / re-imported history
Local-file sources (codex, opencode, …) re-derive events from their own session
logs each cycle. Because the hot window (90d) is at least as long as that
re-import lookback, re-imported events land inside the hot window and are simply
deduplicated — they do not fight retention.
:::

:::warning Lowering the window prunes detail
Lowering `retention_days` lets the next prune delete raw per-event rows older
than the new window (after they are rolled up). The daily aggregate is kept, but
per-event drill-down for that period is gone. Back up the DB first if you want
the raw rows.
:::

## Backups

The DB is a single file plus a `-shm` and `-wal` companion in WAL mode. The safe copy procedure:

```bash
sqlite3 ~/.local/state/openusage/telemetry.db ".backup '/path/to/backup.db'"
```

`cp` of the file alone while the daemon is writing risks an incomplete WAL and a corrupt restore.

## Corruption recovery

On detected corruption (failed page checksum, unreadable header), the daemon:

1. Closes the bad handle.
2. Renames the file to `telemetry.db.corrupt.{unix-ts}`.
3. Removes orphaned `-shm` and `-wal` files.
4. Reinitializes a fresh `telemetry.db`.

Hooks fired during this window go to the spool and drain into the new DB on next pipeline cycle. Only the **most recent** corrupt copy is kept for forensics; older `telemetry.db.corrupt.*` snapshots are removed automatically on startup so they cannot accumulate on disk.

To reduce the chance of corruption, read paths (the dashboard read model) open the database **read-only** — a reader can never modify (and therefore never corrupt) the writer's file, and its queries do not take the write lock or contend with the daemon's writes.

## Manual cleanup

To wipe everything and start over:

```bash
openusage telemetry daemon uninstall   # if installed as a service
rm -rf ~/.local/state/openusage/
```

Reinstall the daemon ([install guide](./install.md)) and the database is recreated empty.

## See also

- [Daemon overview](./overview.md) — pipeline and data flow
- [Tool integrations](./integrations.md) — what hooks emit
- [Configuration reference](../reference/configuration.md) — full `data.*` and `telemetry.*` schema
