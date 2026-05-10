---
title: CLI reference
description: Every openusage command and subcommand with flags and behavior.
---

# CLI reference

The `openusage` binary is the dashboard, the daemon, the hook receiver, and the integrations manager. Everything is exposed via cobra subcommands.

## Top-level

```
openusage                                       # run the dashboard (default)
openusage version                               # print version and build info
openusage detect [--all]                        # print credential auto-detection report
openusage telemetry hook <source> [flags]       # forward an event from a tool hook
openusage telemetry daemon <subcommand> [flags] # daemon lifecycle
openusage integrations <subcommand> [flags]     # tool integration management
```

## `openusage`

Runs the TUI dashboard. With no flags it auto-detects accounts, connects to the [daemon](../daemon/overview.md) over its Unix socket, and opens the dashboard. If the daemon is not yet installed, run `openusage telemetry daemon install` first.

### Flags

The default command takes no flags beyond cobra's built-ins. Configuration lives in `~/.config/openusage/settings.json` — see [configuration reference](./configuration.md).

## `openusage version`

```
openusage version
```

Prints the binary version, commit, and build date. Useful for bug reports.

## `openusage detect`

Runs the same auto-detection pipeline used at dashboard startup and prints a report:

- **Tools detected** — name, type (`ide` / `cli`), and binary path.
- **Accounts detected** — provider, account ID, auth mode, masked credential, and a `SOURCE` column with the precise locator (`env`, `shell_rc:/path`, `aider_yaml:/path`, `aider_dotenv:/path`, `opencode_auth_json`, `codex_auth_json`, `keychain:Claude Code-credentials`, etc.).
- **No credentials found for** — every registered provider that produced no account.

```
openusage detect
openusage detect --all      # also list every registered provider, even those already covered
```

Tokens are masked (`first4...last4`); nothing is written to disk. Use this to debug "why doesn't OpenUsage see my key?" before opening an issue. See [Auto-detection](../concepts/auto-detection.md) for the full source order.

## `openusage telemetry hook`

Reads a JSON event from stdin and forwards it to the daemon. Used by hook scripts installed via [integrations](../daemon/integrations.md).

```
openusage telemetry hook <source> [flags]
```

Argument:

- `<source>` — the source tag (e.g. `anthropic`, `codex`, `opencode`). Maps to a display provider via [provider links](../daemon/storage.md#provider-links).

### Flags

| Flag | Default | Purpose |
|---|---|---|
| `--socket-path PATH` | `~/.local/state/openusage/telemetry.sock` | Daemon socket. Honors `OPENUSAGE_TELEMETRY_SOCKET`. |
| `--account-id ID` | (none) | Tag the event with an explicit account id. |
| `--db-path PATH` | `~/.local/state/openusage/telemetry.db` | Used only when bypassing the daemon (`--spool-only` write path). |
| `--spool-dir PATH` | `~/.local/state/openusage/telemetry-spool/` | Where to spool the event if the daemon is unreachable. |
| `--spool-only` | off | Write to the spool unconditionally; do not contact the daemon. |
| `--verbose` | off | Verbose stderr logging. |

### Behavior

- Tries to POST to `/v1/hook/<source>?account_id=…` with an overall 15-second context timeout.
- On dial failure, writes the event to a JSON line in the spool directory.
- Returns exit code 0 in both cases — hooks should not fail their parent tool because telemetry is offline.

## `openusage telemetry daemon`

The daemon process and its lifecycle.

```
openusage telemetry daemon [run|install|uninstall|status]
```

### `daemon run`

Start the daemon in the foreground. Used when launchd / systemd run it as a service, and useful for ad-hoc debugging.

| Flag | Default | Purpose |
|---|---|---|
| `--socket-path PATH` | `~/.local/state/openusage/telemetry.sock` | Bind path. |
| `--db-path PATH` | `~/.local/state/openusage/telemetry.db` | SQLite file. |
| `--spool-dir PATH` | `~/.local/state/openusage/telemetry-spool/` | Spool directory. |
| `--interval DURATION` | `30s` | Default poll/collect interval. |
| `--collect-interval DURATION` | (inherits `--interval`) | Override collectors only. |
| `--poll-interval DURATION` | (inherits `--interval`) | Override provider polling only. |
| `--verbose` | off | Verbose stderr. |

### `daemon install`

```
openusage telemetry daemon install
```

Writes the platform service file and starts the daemon.

- macOS: `~/Library/LaunchAgents/com.openusage.telemetryd.plist`, label `com.openusage.telemetryd`, `KeepAlive=true`, `RunAtLoad=true`.
- Linux: `~/.config/systemd/user/openusage-telemetry.service`, `Type=simple`, `Restart=always`, `RestartSec=2`.

Refuses to install if the binary path is a `go run` temp file.

### `daemon uninstall`

```
openusage telemetry daemon uninstall
```

Stops and removes the service. Does **not** delete the database, spool, or logs.

### `daemon status`

```
openusage telemetry daemon status [--details]
```

Prints whether the service is running. With `--details`, includes:

- Service state from the platform tool
- Socket path and `/healthz` reachability
- Resolved DB and spool paths
- Recent log file sizes

## `openusage integrations`

Manage tool hook integrations. See [integrations](../daemon/integrations.md) for what each one installs.

```
openusage integrations <subcommand>
```

### `integrations list`

```
openusage integrations list [--all]
```

Lists installed integrations. `--all` includes integrations that aren't installed yet.

### `integrations install`

```
openusage integrations install <id>
```

Renders the embedded template, writes the hook artifact, patches the tool's config, and saves the install state to `settings.json`.

Backs up any existing file as `<file>.bak` before overwriting.

### `integrations uninstall`

```
openusage integrations uninstall <id>
```

Removes the hook artifact, de-registers the entry from the tool's config, and marks the integration as not installed.

### `integrations upgrade`

```
openusage integrations upgrade <id>
openusage integrations upgrade --all
```

Reinstalls integrations whose embedded version is newer than the installed version.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Generic failure (see stderr) |
| `2` | Usage error (cobra) |

## Environment variables

The CLI honors the following — see [environment variables](./env-vars.md) for the full list:

- `OPENUSAGE_DEBUG` — verbose stderr logging
- `OPENUSAGE_BIN` — override the binary path used by hook scripts
- `OPENUSAGE_TELEMETRY_SOCKET` — override socket path
- `OPENUSAGE_THEME_DIR` — extra theme search paths
- `XDG_CONFIG_HOME`, `XDG_STATE_HOME` — base directories
- `CLAUDE_SETTINGS_FILE`, `CODEX_CONFIG_DIR` — tool-specific overrides

## See also

- [Paths reference](./paths.md) — every file path the CLI reads or writes
- [Configuration reference](./configuration.md) — `settings.json` schema
- [Daemon overview](../daemon/overview.md) — what the daemon does
