---
title: Direct mode vs daemon mode
description: The two runtime modes of OpenUsage, when to pick each, and how to switch between them.
---

OpenUsage has two runtime modes. Both render the same dashboard from the same providers; what differs is **where polling happens** and **whether data persists** between sessions.

## At a glance

| | Direct mode | Daemon mode |
|---|---|---|
| Background process | none | one (`openusage telemetry daemon`) |
| Where polling runs | inside the TUI | inside the daemon |
| Data persists across TUI restarts | no | yes (SQLite) |
| Hooks from coding agents | no | yes |
| Disk footprint | config file only | + `~/.local/state/openusage/telemetry.db` |
| When you see data | only while TUI is open | continuously |
| Best for | quick check-ins, headless one-shot, demos | always-on monitoring, multi-session aggregation |

## Direct mode (default)

If you have not installed the daemon, this is what you get:

```
$ openusage
```

Inside the TUI, a ticker fires every 30 seconds (configurable). On each tick:

1. Each registered provider's `Fetch()` is called concurrently.
2. The resulting `UsageSnapshot[]` is shipped into the Bubble Tea event loop.
3. Tiles re-render.

Properties:

- **Zero infrastructure.** No socket, no SQLite file, no service.
- **Ephemeral.** When you quit, all in-memory snapshots vanish. The next launch starts cold.
- **Provider memory only.** Anything historical you see (a per-day bar chart, a 7-day spend total) was already remembered by the provider itself or by a local file the provider reads.

Direct mode is the right choice when:

- You just want to glance at quotas before a coding session.
- You're testing a new provider and don't want a service installed yet.
- You're running on a sandbox or short-lived shell.

## Daemon mode

The daemon is a long-running process that owns the polling loop and a SQLite store. The TUI becomes a thin client that fetches a precomputed read model over a Unix domain socket.

Install:

```bash
openusage telemetry daemon install
```

This sets up a launchd agent (macOS) or a systemd user unit (Linux) and starts the service. Verify with:

```bash
openusage telemetry daemon status
```

Once the daemon is running, plain `openusage` automatically detects the socket at `~/.local/state/openusage/telemetry.sock` and connects to it instead of polling itself.

Properties:

- **Background polling.** Data is collected every 30s (or whatever `--interval` you set) regardless of whether the TUI is open.
- **Persisted.** Snapshots and per-event records live in `telemetry.db`. The TUI sees the full history within `data.retention_days` (default 30).
- **Hooks.** Tools like Claude Code, Codex, and OpenCode can ship per-turn events directly to the daemon via the integration hooks. See [telemetry](telemetry.md).
- **Recoverable.** A corrupt SQLite file is auto-renamed and rebuilt; the spool catches events that arrive while the daemon is briefly offline.

Daemon mode is the right choice when:

- You want spend/usage to keep accruing in the background.
- You install the agent integrations to get richer per-turn data.
- You run multi-day comparisons or care about week-over-week trends.

## Switching between modes

### From direct to daemon

```bash
openusage telemetry daemon install
openusage telemetry daemon status
```

The next `openusage` launch picks up the socket automatically. Your `settings.json` is unchanged.

### From daemon to direct

```bash
openusage telemetry daemon uninstall
```

This stops and removes the launchd agent or systemd user unit. The SQLite store is left in place; you can delete it manually if you want to free disk:

```bash
rm -rf ~/.local/state/openusage/telemetry.db ~/.local/state/openusage/telemetry-spool
```

### Pinning a mode

If a daemon is installed but you want a one-off direct run (for example to debug a single provider), unset the socket override and stop the service:

```bash
openusage telemetry daemon status   # check state
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.openusage.telemetryd.plist   # macOS
systemctl --user stop openusage-telemetry.service                                       # Linux
openusage   # falls back to direct
```

Or run the daemon in the foreground without installing it:

```bash
openusage telemetry daemon
```

## Mixing them

You cannot run the daemon and a direct-mode TUI against the same provider accounts cleanly — both would call provider APIs on their own ticker. If a daemon is running, let the TUI connect to it.

## Where to read next

- [Telemetry](telemetry.md) — what the daemon stores and how dedup works.
- [Daemon overview](/daemon) — install, run, troubleshoot.
- [Headless servers](../guides/headless-servers.md) — daemon-only on a remote host.
