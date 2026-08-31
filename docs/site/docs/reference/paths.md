---
title: Paths reference
description: Every file and directory OpenUsage reads or writes, by operating system.
---

# Paths reference

OpenUsage follows the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/) on Linux and macOS. Windows uses `%APPDATA%`. Every path below can be overridden — see the **Override** column.

## OpenUsage paths

| Path | Purpose | Override |
|---|---|---|
| `~/.config/openusage/settings.json` | Main config file. | — |
| `~/.config/openusage/settings.json.corrupt` | Copy of a `settings.json` OpenUsage had to salvage, kept for inspection. Overwritten on each repair. See [common issues](../troubleshooting/common-issues.md). | — |
| `~/.config/openusage/custom-pricing.json` | User pricing overrides. | `OPENUSAGE_CUSTOM_PRICING`, `XDG_CONFIG_HOME` |
| `~/.config/openusage/themes/` | External themes directory (scanned for `*.json`). | `OPENUSAGE_THEME_DIR` (extra dirs only) |
| `~/.config/openusage/hooks/` | Hook scripts installed by `openusage integrations`. | — |
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
| `~/.config/opencode/opencode.json` | OpenCode | Plugin registration. | — |
| `~/.config/opencode/plugins/openusage-telemetry.ts` | OpenCode | Plugin source installed by `integrations install opencode`. | — |
| `~/.local/share/opencode/auth.json` | OpenCode | API keys adopted by auto-detection (OpenCode's data dir). | `XDG_DATA_HOME` |

:::note OpenCode `auth.json` on Windows
OpenCode resolves its data directory through the `xdg-basedir` library, which has no Windows-specific branch, so on Windows it writes credentials to `%USERPROFILE%\.local\share\opencode\auth.json` rather than under `%APPDATA%`. Auto-detection probes that XDG-style location first on Windows, then `%LOCALAPPDATA%\opencode\auth.json` and `%APPDATA%\opencode\auth.json` as forward-compatible fallbacks.
:::

## Per-OS expansion

### macOS

| Logical path | Resolved |
|---|---|
| Config dir | `~/.config/openusage/` (hardcoded; `XDG_CONFIG_HOME` is not honored) |
| State dir | `~/.local/state/openusage/` (or `$XDG_STATE_HOME/openusage/` if set) |
| Service file | `~/Library/LaunchAgents/com.openusage.telemetryd.plist` |

### Linux

| Logical path | Resolved |
|---|---|
| Config dir | `~/.config/openusage/` (hardcoded; `XDG_CONFIG_HOME` is not honored) |
| State dir | `~/.local/state/openusage/` (or `$XDG_STATE_HOME/openusage/` if set) |
| Service file | `~/.config/systemd/user/openusage-telemetry.service` |
| Logs | Files plus `journalctl --user-unit openusage-telemetry.service` |

### Windows

| Logical path | Resolved |
|---|---|
| Config dir | `%APPDATA%\openusage\` |
| `custom-pricing.json` | `%APPDATA%\openusage\custom-pricing.json` (or `$XDG_CONFIG_HOME\openusage\` if set) |
| Hooks dir | `%APPDATA%\openusage\hooks\` |
| State dir | `%APPDATA%\openusage\state\` (or `$XDG_STATE_HOME\openusage\` if set) |
| Theme dir separator | `;` (semicolon) for `OPENUSAGE_THEME_DIR` |

OpenUsage's own directories live under `%APPDATA%\openusage`. Third-party tool
directories that OpenUsage reads are resolved the way each tool resolves them,
which is **not** always the Windows-native location: tools that use the
`xdg-basedir`/`env-paths` libraries (e.g. OpenCode) read from
`%USERPROFILE%\.config\<tool>` and `%USERPROFILE%\.local\share\<tool>` on Windows
too. See the per-tool notes above.

:::note Daemon on Windows
The launchd / systemd-user service installer is not supported on Windows, and
the TUI cannot auto-spawn or auto-upgrade a daemon there. Telemetry mode works
if you start the daemon yourself with `openusage telemetry daemon run` (the TUI
connects to it over the local socket); a managed Windows-service lifecycle is
tracked as future work.
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
