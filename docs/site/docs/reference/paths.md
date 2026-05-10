---
title: Paths reference
description: Every file and directory OpenUsage reads or writes, by operating system.
---

# Paths reference

OpenUsage follows the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/) on Linux and macOS. Windows uses `%APPDATA%`. Every path below can be overridden — see the **Override** column.

## OpenUsage paths

| Path | Purpose | Override |
|---|---|---|
| `~/.config/openusage/settings.json` | Main config file. | `XDG_CONFIG_HOME` |
| `~/.config/openusage/themes/` | External themes directory (scanned for `*.json`). | `XDG_CONFIG_HOME`, plus `OPENUSAGE_THEME_DIR` for extra dirs |
| `~/.config/openusage/hooks/` | Hook scripts installed by `openusage integrations`. | `XDG_CONFIG_HOME` |
| `~/.local/state/openusage/` | State directory (DB, socket, spool, logs). | `XDG_STATE_HOME` |
| `~/.local/state/openusage/telemetry.db` | Daemon SQLite store. | `--db-path` |
| `~/.local/state/openusage/telemetry.sock` | Daemon Unix domain socket. | `--socket-path`, `OPENUSAGE_TELEMETRY_SOCKET` |
| `~/.local/state/openusage/telemetry-spool/` | Hook spool — events queued while the daemon is offline. | `--spool-dir` |
| `~/.local/state/openusage/daemon.stdout.log` | Daemon stdout when running as a service. | — |
| `~/.local/state/openusage/daemon.stderr.log` | Daemon stderr when running as a service. | — |

## Service files

| Path | OS | Purpose |
|---|---|---|
| `~/Library/LaunchAgents/com.openusage.telemetryd.plist` | macOS | launchd unit. Label `com.openusage.telemetryd`. |
| `~/.config/systemd/user/openusage-telemetry.service` | Linux | systemd-user unit. |

Created by `openusage telemetry daemon install`, removed by `openusage telemetry daemon uninstall`.

## Tool integration paths

These belong to the third-party tools OpenUsage hooks into.

| Path | Tool | Purpose | Override |
|---|---|---|---|
| `~/.claude/settings.json` | Claude Code | Hook registration. | `CLAUDE_SETTINGS_FILE` |
| `~/.codex/config.toml` | Codex | `notify` registration. | `CODEX_CONFIG_DIR` |
| `~/.config/opencode/opencode.json` | OpenCode | Plugin registration. | `XDG_CONFIG_HOME` |
| `~/.config/opencode/plugins/openusage-telemetry.ts` | OpenCode | Plugin source installed by `integrations install opencode`. | `XDG_CONFIG_HOME` |

## Per-OS expansion

### macOS

| Logical path | Resolved |
|---|---|
| `$XDG_CONFIG_HOME/openusage/` | `~/.config/openusage/` (XDG honored on macOS too) |
| `$XDG_STATE_HOME/openusage/` | `~/.local/state/openusage/` |
| Service file | `~/Library/LaunchAgents/com.openusage.telemetryd.plist` |

### Linux

| Logical path | Resolved |
|---|---|
| `$XDG_CONFIG_HOME/openusage/` | `~/.config/openusage/` |
| `$XDG_STATE_HOME/openusage/` | `~/.local/state/openusage/` |
| Service file | `~/.config/systemd/user/openusage-telemetry.service` |
| Logs | Files plus `journalctl --user-unit openusage-telemetry.service` |

### Windows

| Logical path | Resolved |
|---|---|
| Config dir | `%APPDATA%\openusage\` |
| State dir | `%APPDATA%\openusage\state\` |
| Theme dir separator | `;` (semicolon) for `OPENUSAGE_THEME_DIR` |

:::note Daemon on Windows
The launchd / systemd-user service installer is not supported on Windows. You can still run `openusage telemetry daemon run` manually, but there is no auto-start template.
:::

## Theme search order

Themes are loaded in this order; later files with the same `name` override earlier ones:

1. Built-in themes compiled into the binary.
2. `<config_dir>/themes/*.json` — i.e. `~/.config/openusage/themes/` on Linux/macOS, `%APPDATA%\openusage\themes\` on Windows.
3. Each path in `OPENUSAGE_THEME_DIR`, separated by `:` on Unix and `;` on Windows.

See [External themes](../customization/external-themes.md).

## See also

- [Environment variables](./env-vars.md) — every override variable
- [Daemon overview](../daemon/overview.md) — how the daemon uses the state directory
- [Configuration reference](./configuration.md) — what lives in `settings.json`
