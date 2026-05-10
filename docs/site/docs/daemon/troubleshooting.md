---
title: Daemon troubleshooting
description: Diagnose and fix daemon startup failures, socket errors, missing events, and database corruption.
---

# Daemon troubleshooting

Most daemon issues fall into one of four buckets: the service won't start, the socket isn't reachable, events aren't appearing, or the database got corrupted. This page walks each.

:::tip Turn on debug logging first
Set `OPENUSAGE_DEBUG=1` in your shell or in the launchd plist / systemd unit's `Environment=`. Verbose output in `daemon.stderr.log` (or `journalctl --user-unit openusage-telemetry.service`) is usually enough to diagnose the problem.
:::

## Daemon won't start

### Symptom
`openusage telemetry daemon status` reports the service is not running. `launchctl print` or `systemctl --user status` shows a failure.

### Check the binary path

The plist or systemd unit captures the absolute path of `openusage` at install time. If you moved or replaced the binary, the service can't find it.

```bash
# macOS
launchctl print gui/$(id -u)/com.openusage.telemetryd | grep program

# Linux
systemctl --user cat openusage-telemetry.service | grep ExecStart
```

Fix:

```bash
openusage telemetry daemon uninstall
openusage telemetry daemon install
```

### Check CGO

If the binary aborts immediately, you may have a non-CGO build. The daemon depends on `mattn/go-sqlite3`, which fails at runtime without CGO. Use the official release build, or compile with `CGO_ENABLED=1`.

### macOS: re-load the plist

```bash
launchctl bootout gui/$(id -u)/com.openusage.telemetryd
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.openusage.telemetryd.plist
launchctl kickstart -k gui/$(id -u)/com.openusage.telemetryd
```

### Linux: lingering disabled

If `systemctl --user` services don't survive logout, enable lingering once:

```bash
loginctl enable-linger $USER
```

## Socket errors

### Symptom
TUI shows "telemetry: not connected" or hooks log `dial unix … connect: no such file or directory`.

### Check the socket

```bash
ls -la ~/.local/state/openusage/telemetry.sock
curl --unix-socket ~/.local/state/openusage/telemetry.sock http://localhost/healthz
```

A healthy daemon answers `200 OK`. If the file is missing, the daemon isn't running. If it exists but `/healthz` hangs or refuses, the daemon is wedged — restart it.

### Override paths drift

If you set `--socket-path` or `OPENUSAGE_TELEMETRY_SOCKET` for the daemon but not the TUI/hooks (or vice versa), they connect at different paths. Set the env var in your shell init so every process inherits it.

```bash
export OPENUSAGE_TELEMETRY_SOCKET=/tmp/openusage-telemetry.sock
```

### Stale socket after crash

```bash
rm ~/.local/state/openusage/telemetry.sock
# then restart the service
```

The daemon recreates it on startup.

## Inspecting logs

### macOS

```bash
tail -f ~/.local/state/openusage/daemon.stderr.log
tail -f ~/.local/state/openusage/daemon.stdout.log
```

### Linux

```bash
journalctl --user-unit openusage-telemetry.service -f
journalctl --user-unit openusage-telemetry.service --since "10 min ago"
```

The log files in `~/.local/state/openusage/` are also written on Linux when the unit redirects stdout/stderr.

## Missing or duplicate events

### Spool not draining

Files piling up in `~/.local/state/openusage/telemetry-spool/` indicate the daemon hasn't been able to ingest them.

Common causes:

- Daemon was offline when hooks fired — files will drain automatically once it's running.
- Persistent malformed payload — daemon logs will show parse errors. Move the offending file aside, restart, and investigate.
- DB was corrupt — fixed automatically (see below) but spool drain is paused until reinit completes.

### Events show under the wrong provider

This is a [provider link](./storage.md#provider-links) mismatch. Open the Telemetry settings tab (<kbd>,</kbd> then <kbd>6</kbd>) and use <kbd>m</kbd> on the source row to pick the correct display provider, or edit `telemetry.provider_links` in `settings.json`.

### Dedup ate a real event

The pipeline drops events whose dedup key matches an earlier row. If a tool re-uses `tool_call_id` across distinct events (an upstream bug), distinct turns can collapse into one. Workarounds:

- Upgrade the tool integration: `openusage integrations upgrade <id>`.
- Check `raw_events` for the dropped payload — it's still there even when the canonical row is deduped.

```bash
sqlite3 ~/.local/state/openusage/telemetry.db \
  "SELECT id, source, schema, occurred_at FROM raw_events ORDER BY occurred_at DESC LIMIT 20;"
```

## Database corruption

### Symptom
Daemon log shows `database disk image is malformed` or `file is not a database`.

### Automatic recovery

The daemon detects corruption on startup and:

1. Renames the bad file to `telemetry.db.corrupt.{unix-ts}`.
2. Removes orphaned `telemetry.db-shm` and `telemetry.db-wal`.
3. Initializes a fresh `telemetry.db` and continues.

Look for the corrupt file:

```bash
ls ~/.local/state/openusage/telemetry.db.corrupt.*
```

You can attempt a recovery dump:

```bash
sqlite3 ~/.local/state/openusage/telemetry.db.corrupt.1715000000 \
  ".recover" > /tmp/recovered.sql
```

Once you're satisfied, delete the corrupt file.

### Prevent future corruption

- Don't kill the daemon with `SIGKILL` while a write is in flight.
- Don't `cp` the live DB — use `sqlite3 … .backup` (see [Storage](./storage.md#backups)).
- Keep the state directory on a local disk; SQLite + WAL on networked filesystems is unreliable.

## Reset everything

When you just want a clean start:

```bash
openusage telemetry daemon uninstall
rm -rf ~/.local/state/openusage/
openusage telemetry daemon install
```

This wipes the DB, spool, and logs. Hook scripts and tool config patches are unaffected (managed separately by `openusage integrations`).

## Still stuck?

- Run the daemon in the foreground with verbose logging: `openusage telemetry daemon run --verbose`.
- Open an issue on GitHub with the relevant log excerpt and your platform.
