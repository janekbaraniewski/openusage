---
title: Headless servers
description: Running OpenUsage on a server without a desktop — daemon-only mode, tmux for the TUI, and SSH viewing.
---

OpenUsage works on remote servers — for example a dedicated build host that runs many agent jobs. The two main patterns are **daemon-only** (collect data, no UI) and **TUI over SSH** (occasional inspection from your laptop).

## Pattern 1: daemon-only

If the server runs jobs but you never want to view a TUI on it, install just the daemon. It will poll providers and ingest hooks even though no terminal is attached.

```bash
# On the server
openusage telemetry daemon install
openusage telemetry daemon status
```

Logs:

- `~/.local/state/openusage/daemon.stdout.log`
- `~/.local/state/openusage/daemon.stderr.log`
- Linux: `journalctl --user-unit openusage-telemetry.service`

Storage:

- `~/.local/state/openusage/telemetry.db`

Inspect from your laptop later:

- Copy the SQLite file: `scp server:.local/state/openusage/telemetry.db .`
- Or open the dashboard over SSH (next pattern).

## Pattern 2: TUI over SSH

The Bubble Tea TUI runs fine in any terminal that supports ANSI colors. Connect over SSH and launch it directly:

```bash
ssh build-host
openusage
```

If a daemon is installed on the server, the TUI connects to its socket automatically and the data shown is the daemon's data. If not, the TUI runs in [direct mode](../concepts/direct-vs-daemon.md).

Tips:

- Use a terminal with 256-color support (Alacritty, Kitty, Wezterm, modern Terminal.app, iTerm2). The 18 themes assume true color is available.
- Resize your terminal to at least 100 columns. Below ~80 columns the dashboard auto-falls-back to a single-column **Stacked** view.
- Mouse wheel scroll works over most SSH clients (3 lines/tick).

## Pattern 3: tmux for persistent TUI

If you want the dashboard to stay open across SSH disconnects:

```bash
ssh build-host
tmux new -A -s usage
openusage
# Detach with Ctrl+b d
```

Reconnect later:

```bash
ssh build-host
tmux attach -t usage
```

The TUI keeps rendering whether anyone is attached or not.

## Disabling Analytics on small servers

The Analytics screen is opt-in (`cfg.Experimental.Analytics`). On a server you may want to leave it off to keep the rendering loop tight:

```json
{
  "experimental": { "analytics": false }
}
```

The Tab cycle then just bounces between dashboard views.

## Integrations on a server

If the server itself runs Claude Code, Codex, or OpenCode jobs, install the hooks the same way as on a workstation:

```bash
openusage integrations install claude_code
openusage integrations install codex
openusage integrations install opencode
```

Each tool's config file is patched (Claude `~/.claude/settings.json`, Codex `~/.codex/config.toml`, OpenCode `~/.config/opencode/opencode.json`). The hook scripts shell out to `openusage telemetry hook <source>` and post events to the daemon.

If the daemon is briefly unavailable, hooks spool to `~/.local/state/openusage/telemetry-spool/` and are drained when it comes back.

## Things to watch out for

- **No display server needed.** The TUI uses raw terminal escape codes, not X11. Any SSH session works.
- **CGO required at build time.** Use the prebuilt release binary — `go install` from source on a server without a C toolchain will fail.
- **File permissions.** The daemon writes its socket under `~/.local/state/openusage/`. If multiple users run on the same host, each has their own daemon and store; they do not share state.
- **Time zones.** The `1d` window is local-midnight-relative. If your server runs in UTC and you're in a different zone, the day boundary will surprise you. Set `TZ` or use `3d` instead.

## See also

- [Direct vs daemon](../concepts/direct-vs-daemon.md)
- [Daemon overview](/daemon)
- [Daemon issues troubleshooting](../troubleshooting/daemon-issues.md)
