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
| `raw_events` | Untouched payload bodies with a schema discriminator. Useful for replay and debugging. |
| `provider_snapshots` | The most recent collector snapshot per provider/account. Cheap reads for the TUI. |
| `metadata` | Schema version, last-prune timestamps, and other key/value state. |

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

## Retention

Configured under `data.retention_days` in settings.json (default `30`). Two prune jobs run inside the daemon:

- `PruneOldEvents` — deletes rows from `usage_events` older than the window.
- `PruneRawEventPayloads` — deletes the heavier blob in `raw_events` older than the window, keeping the row for traceability if needed.

Both run on startup and on a periodic timer. After a long downtime, expect the first cycle to take longer.

```json
{
  "data": {
    "retention_days": 90
  }
}
```

:::warning
Lowering `retention_days` causes immediate deletion of older rows the next time the daemon starts. There is no soft-delete or archive — back the DB up first if you want a copy.
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

Hooks fired during this window go to the spool and drain into the new DB on next pipeline cycle. The corrupt copy is left in place — delete it once you've confirmed nothing useful remains.

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
