---
title: Daemon issues
description: Diagnosing problems with the telemetry daemon — install failures, socket errors, log inspection, and SQLite recovery.
---

The daemon is a background service that polls providers and accepts hook posts. When it misbehaves, the symptoms usually fall into one of the categories below.

## Daemon won't start

Symptoms: `openusage telemetry daemon status` reports not running, or the install command exits non-zero.

### Cause: installing from `go run`

`openusage telemetry daemon install` writes a launchd plist (macOS) or systemd unit (Linux) that points at the binary's current path. If you're running via `go run`, that path is a temporary build directory that disappears after the command exits.

```
Cannot install from go run (transient binary).
```

Fix: install a permanent binary first.

```bash
make build
sudo install -m 0755 bin/openusage /usr/local/bin/openusage
openusage telemetry daemon install
```

Or use the release tarball / Homebrew formula.

### Cause: service file already exists

Reinstalling can fail if a stale plist/unit is in place. Uninstall first:

```bash
openusage telemetry daemon uninstall
openusage telemetry daemon install
```

### Cause: socket directory not writable

The daemon creates `~/.local/state/openusage/` if missing. If `~/.local/` exists but is not writable by your user, creation fails. Check:

```bash
ls -ld ~/.local ~/.local/state ~/.local/state/openusage 2>/dev/null
```

Fix permissions with `chown` / `chmod` or pick a different state dir via `XDG_STATE_HOME`.

## Socket errors (`EACCES`, `ECONNREFUSED`)

Symptoms: TUI shows "daemon not reachable" or hooks log socket errors.

### `ECONNREFUSED`

The socket file exists but nothing is listening. Usually means the daemon crashed.

```bash
openusage telemetry daemon status
# macOS
launchctl print gui/$(id -u)/com.openusage.telemetryd
# Linux
systemctl --user status openusage-telemetry.service
```

If the service is dead, restart it:

```bash
# macOS
launchctl kickstart -k gui/$(id -u)/com.openusage.telemetryd
# Linux
systemctl --user restart openusage-telemetry.service
```

### `EACCES`

The socket file exists but the current user can't connect. This happens when:

- Two users share the host and one daemon owns the socket.
- A previous run wrote with different permissions.

Fix: each user runs their own daemon with their own socket. To force a different path:

```bash
export OPENUSAGE_TELEMETRY_SOCKET=$HOME/.local/state/openusage/telemetry.sock
```

### Socket path mismatch

Both the daemon and the TUI default to `~/.local/state/openusage/telemetry.sock`. If you set `--socket-path` on one but not the other, they don't meet. Use `OPENUSAGE_TELEMETRY_SOCKET` to set both.

## Log inspection

Logs are written in two places.

### Files

```
~/.local/state/openusage/daemon.stdout.log
~/.local/state/openusage/daemon.stderr.log
```

Tail them while reproducing the issue:

```bash
tail -f ~/.local/state/openusage/daemon.stderr.log
```

### journald (Linux)

```bash
journalctl --user-unit openusage-telemetry.service -f
```

### Verbose mode

If symptoms only appear under load, enable verbose logging:

```bash
openusage telemetry daemon uninstall
openusage telemetry daemon --verbose         # foreground, prints to terminal
```

Reproduce the issue, then reinstall when done.

## SQLite corruption

Symptoms: daemon logs show `database disk image is malformed` or `cannot open database`.

### Auto-recovery

The daemon does this for you on startup: it renames the corrupt file to `telemetry.db.corrupt.<timestamp>`, removes any stale `-shm` / `-wal` files, and creates a fresh database. You'll lose history beyond what's still in the spool, but the service comes back up.

### Manual recovery

If the auto-recovery doesn't fire (e.g. corruption appears mid-run), stop the daemon and clear the files:

```bash
# macOS
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.openusage.telemetryd.plist
# Linux
systemctl --user stop openusage-telemetry.service

# Move the files aside (don't delete in case forensics are needed)
mv ~/.local/state/openusage/telemetry.db ~/.local/state/openusage/telemetry.db.bak
rm -f ~/.local/state/openusage/telemetry.db-shm ~/.local/state/openusage/telemetry.db-wal

# Restart
openusage telemetry daemon install   # if uninstalled
# or kickstart (macOS) / restart (Linux) as above
```

### Preventing it

The store is configured with WAL and `synchronous=on`, so corruption is rare absent disk failure or `kill -9` mid-write. Avoid forcing the host to power off while a poll is in flight.

## Hooks not delivering events

Symptoms: integration is installed but new events from the agent don't appear in the dashboard.

1. **Confirm the hook is installed and current:**
   ```bash
   openusage integrations list
   ```
   `outdated` means the on-disk template lags behind the binary's bundled version. Run `openusage integrations upgrade --all`.

2. **Confirm the daemon is running** (see above).

3. **Check the spool.** If the daemon was down, hooks should have spooled. After the daemon comes back, events drain on the next interval.
   ```bash
   ls ~/.local/state/openusage/telemetry-spool/
   ```

4. **Re-install the hook.** Backup files are written to `.bak` next to the originals, so this is non-destructive:
   ```bash
   openusage integrations install claude_code
   ```

## Resetting everything

Last-resort wipe:

```bash
openusage telemetry daemon uninstall
rm -rf ~/.local/state/openusage
openusage telemetry daemon install
```

You'll lose all history. Auto-detection rebuilds account configuration on the next TUI launch.

## See also

- [Daemon overview](/daemon)
- [Telemetry pipeline](../concepts/telemetry.md)
- [Debug mode](debug-mode.md)
