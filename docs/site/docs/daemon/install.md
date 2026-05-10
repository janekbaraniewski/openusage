---
title: Install the daemon
description: Install, uninstall, and check the OpenUsage telemetry daemon on macOS launchd and Linux systemd-user.
---

# Install the daemon

The daemon ships with the `openusage` binary. There is no separate package — the same binary is the dashboard, the hook receiver, and the daemon itself.

:::warning CGO required
The daemon links `mattn/go-sqlite3`, which requires CGO. Use the official release binaries or build with `CGO_ENABLED=1`. A `go run` build is **rejected** at install time because the path points at a transient temp file.
:::

## Prerequisites

- macOS (launchd) or Linux with `systemd --user`
- A persistent install of `openusage` on `$PATH` (e.g. `/usr/local/bin/openusage`)
- Write access to `~/Library/LaunchAgents/` (macOS) or `~/.config/systemd/user/` (Linux)

## Install

```bash
openusage telemetry daemon install
```

What it does:

- **macOS** — writes `~/Library/LaunchAgents/com.openusage.telemetryd.plist` with `KeepAlive=true` and `RunAtLoad=true`, then `launchctl load`s it.
- **Linux** — writes `~/.config/systemd/user/openusage-telemetry.service` (`Type=simple`, `Restart=always`, `RestartSec=2`), runs `systemctl --user daemon-reload`, and `systemctl --user enable --now openusage-telemetry.service`.

After install the daemon is running and will restart automatically on logout/login or reboot (provided your platform's user services are active).

## Status

```bash
openusage telemetry daemon status
openusage telemetry daemon status --details
```

`--details` prints:

- Service state from launchd or systemctl
- Socket path and whether `/healthz` answers
- DB and spool paths
- Recent log file sizes

You can also query the platform tools directly:

```bash
# macOS
launchctl print gui/$(id -u)/com.openusage.telemetryd

# Linux
systemctl --user status openusage-telemetry.service
```

## Uninstall

```bash
openusage telemetry daemon uninstall
```

This stops the service and removes the plist or unit file. It does **not** delete:

- `~/.local/state/openusage/telemetry.db`
- The spool directory
- The log files

Remove those manually if you want a clean slate. See [Storage](./storage.md).

## Run in the foreground

For development or debugging:

```bash
openusage telemetry daemon run --verbose
```

Useful flags:

| Flag | Default | Purpose |
|---|---|---|
| `--socket-path PATH` | `~/.local/state/openusage/telemetry.sock` | Where to bind the Unix socket. Also honors `OPENUSAGE_TELEMETRY_SOCKET`. |
| `--db-path PATH` | `~/.local/state/openusage/telemetry.db` | SQLite file. |
| `--spool-dir PATH` | `~/.local/state/openusage/telemetry-spool/` | Disk queue for unreachable hooks. |
| `--interval DURATION` | `30s` | Default poll/collect interval. |
| `--collect-interval DURATION` | (inherits `--interval`) | Override only for collectors. |
| `--poll-interval DURATION` | (inherits `--interval`) | Override only for provider polling. |
| `--verbose` | off | Verbose log output to stderr. |

## Logs

When run as a service:

- `~/.local/state/openusage/daemon.stdout.log`
- `~/.local/state/openusage/daemon.stderr.log`

On Linux, `systemd-journal` also captures everything:

```bash
journalctl --user-unit openusage-telemetry.service -f
```

:::tip
Set `OPENUSAGE_DEBUG=1` in the launchd plist or systemd unit's environment to get verbose output without restarting with `--verbose`.
:::

## Verifying it works

After install:

```bash
# Liveness probe
curl --unix-socket ~/.local/state/openusage/telemetry.sock http://localhost/healthz

# Connect the TUI — it auto-detects a running daemon
openusage
```

If the dashboard shows "telemetry: connected" in the Telemetry settings tab (<kbd>,</kbd> then <kbd>6</kbd>), the daemon is reachable and the TUI is reading from it.

## Common pitfalls

- **`go run` install rejected.** Build with `make build` and put the binary on `$PATH` before running `daemon install`.
- **Multiple binaries on `$PATH`.** The plist or service unit pins the absolute path captured at install time. Reinstall (`uninstall` then `install`) after moving the binary.
- **Linux without lingering.** If `systemctl --user` services do not survive logout, enable lingering once: `loginctl enable-linger $USER`.

## Next steps

- [Add tool hook integrations](./integrations.md)
- [Tune storage and retention](./storage.md)
- [Troubleshoot install issues](./troubleshooting.md)
