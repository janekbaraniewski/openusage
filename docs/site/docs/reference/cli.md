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
openusage daily|weekly|monthly [flags]          # headless usage/cost report by period
openusage session [flags]                        # usage/cost grouped by Claude Code session
openusage blocks [flags]                          # usage by 5-hour billing block + burn rate
openusage statusline [flags]                     # one-line status bar for Claude Code
openusage tmux [subcommand] [flags]              # tmux status bar integration
openusage telemetry hook <source> [flags]       # forward an event from a tool hook
openusage telemetry daemon <subcommand> [flags] # daemon lifecycle
openusage integrations <subcommand> [flags]     # tool integration management
openusage export [flags]                         # export current snapshots (JSON/CSV)
openusage pricing <model> [flags]                # resolve model pricing
openusage hub [flags]                           # aggregate snapshots from multiple machines
openusage hub-view <url> [flags]                # read-only TUI over a remote hub
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

## `openusage daily` / `weekly` / `monthly` / `session` / `blocks`

Headless usage and cost reports printed to stdout as an aligned table or, with
`--json`, as machine-readable JSON. They reuse the same local parsing and
pricing as the dashboard, so you can script spend tracking in CI without
running the TUI.

```
openusage daily [flags]
openusage weekly [flags]
openusage monthly [flags]
openusage session [flags]
openusage blocks [flags]
```

- `daily` / `weekly` / `monthly` aggregate **every configured provider**. Local
  tools come from their on-disk logs at full fidelity (per-model cost,
  sessions); remote API platforms come from their daily spend via a snapshot
  poll.
- `session` groups usage by conversation session.
- `blocks` groups usage into 5-hour billing windows. The active block shows a
  burn rate (`$/hour`) and a projected end-of-block cost.

`session` and `blocks` cover every local provider that records per-turn (or
per-session) timestamps — Claude Code, Codex, Gemini CLI, Copilot, Cursor,
OpenCode, Ollama, Amp, Codebuff, OpenClaw, Roo Code, Kilo Code, Crush, Goose,
Hermes, Zed, Droid and Kiro. Remote API platforms appear only in the periodic
reports. Tools that record tokens but no cost have it computed from tokens via
the pricing layer (online).

### Flags

| Flag | Default | Purpose |
|---|---|---|
| `--json` | off | Emit JSON instead of a table. |
| `--since YYYY-MM-DD` | (none) | Only include usage on/after this date. |
| `--until YYYY-MM-DD` | (none) | Only include usage on/before this date (inclusive). |
| `--breakdown`, `-b` | off | Add a per-model breakdown under each row. |
| `--provider ID` | (all) | Limit to a single provider id (e.g. `claude_code`). |
| `--project NAME` | (all) | Limit to a single project/workspace label. |
| `--mode MODE` | `calculate` | Cost mode: `calculate` (recompute from tokens), `display` (trust the cost recorded in the logs), or `auto` (logged cost when present, else recompute). |
| `--offline` | off | Skip network pricing lookups; use embedded rates. |
| `--top-models N` | `0` (all) | Cap the models shown per breakdown row. |
| `--source` | `auto` | (`daily`/`weekly`/`monthly`) Snapshot source for non-Claude providers: `auto`, `direct`, or `daemon`. |
| `--week-start` | `monday` | (`weekly`) Week boundary: `monday` or `sunday`. |

Costs are API-equivalent estimates derived from token counts, not subscription
charges.

### Examples

```bash
openusage daily                              # unified daily spend, all providers
openusage daily --provider claude_code --offline   # fast, local-only
openusage monthly --json                     # machine-readable monthly totals
openusage blocks                             # billing blocks with burn rate
openusage session --since 2026-05-01 -b      # sessions since May, per-model
```

## `openusage statusline`

Renders a single status line for the Claude Code status bar. Claude Code pipes
the active session JSON to this command on stdin; the output summarizes the
current model, session / today / active-block cost, the burn rate, and
context-window usage.

```
openusage statusline [flags]
openusage statusline --install      # wire into ~/.claude/settings.json
openusage statusline --uninstall    # remove it again
```

It runs offline by default (embedded pricing) so it responds instantly.

### Flags

| Flag | Default | Purpose |
|---|---|---|
| `--offline` | on | Use embedded pricing and skip network lookups. Pass `--offline=false` for live pricing. |
| `--mode MODE` | `calculate` | Cost mode: `calculate`, `display`, or `auto`. |
| `--color` | on | Colorize the output with ANSI escapes. |
| `--context-medium PCT` | `50` | Context-% threshold for the yellow warning color. |
| `--context-high PCT` | `80` | Context-% threshold for the red warning color. |
| `--install` | off | Add the statusLine block to `~/.claude/settings.json` (creates a `.bak` backup, preserves other keys). |
| `--uninstall` | off | Remove the openusage statusLine from `~/.claude/settings.json`. |

`--install` honors the `CLAUDE_SETTINGS_FILE` override and only removes a
statusLine it manages, leaving any third-party statusLine untouched.

### Manual wiring

If you prefer to edit `~/.claude/settings.json` by hand:

```json
{
  "statusLine": {
    "type": "command",
    "command": "openusage statusline",
    "padding": 0
  }
}
```

## `openusage tmux`

Renders a one-line tmux status segment for the active AI tool. Picks the most recently used local provider (recency then priority order) and renders the `compact` preset by default. The renderer self-times out at 800ms so a slow daemon can never freeze tmux.

```
openusage tmux                                 # render compact preset
openusage tmux --preset claude-focused
openusage tmux --format '{tool} {today_cost:money}'
openusage tmux --segment cost
openusage tmux --json
```

See the [tmux integration guide](../guides/tmux-integration.md) for the full template grammar, the preset gallery, and theming.

### Flags

| Flag | Default | Purpose |
| --- | --- | --- |
| `--preset NAME` | `compact` | Named preset (see `openusage tmux presets`). |
| `--format STR` | (none) | Custom template. Overrides `--preset`. |
| `--segment NAME` | (none) | Render a single named segment. |
| `--provider ID` | (auto) | Pin a provider id. Skips active-tool detection. |
| `--strategy LIST` | `recency,priority` | Comma-separated active-tool detection strategies. |
| `--color-mode MODE` | `truecolor` | `truecolor`, `256`, `ansi`, or `none`. |
| `--no-color` | off | Equivalent to `--color-mode none`. |
| `--no-truecolor` | off | Downgrade to 256-color output. |
| `--glyphs TIER` | per preset | `ascii`, `unicode`, `nerdfont`, or `customfont`. With the bundled font installed, the default auto-upgrades to `customfont`. |
| `--theme NAME` | (inherits) | Override the configured theme for this invocation. |
| `--source MODE` | `auto` | Snapshot source: `auto`, `daemon`, `direct`. |
| `--max-runtime DURATION` | `800ms` | Self-kill budget so tmux never blocks. |
| `--raw` | off | Force tmux-format output even when stdout is a TTY. |
| `--json` | off | Emit structured JSON. |
| `--no-cache` | off | Bypass the active-tool detection cache (~15s TTL). |

`--preset`, `--format`, `--segment`, and `--json` are mutually exclusive.

### `tmux install`

Prints (or writes, with `--write`) a sentinel-bracketed snippet that wires the renderer into your tmux config. The helper looks for `$XDG_CONFIG_HOME/tmux/tmux.conf`, then `~/.config/tmux/tmux.conf`, then `~/.tmux.conf`. With `--write` and no existing config, it creates `~/.config/tmux/tmux.conf`.

```
openusage tmux install                          # print snippet
openusage tmux install --write                  # write to tmux.conf with .bak backup
openusage tmux install --write --position both --bind-popup u
```

| Flag | Default | Purpose |
| --- | --- | --- |
| `--write` | off | Apply to tmux.conf. Creates a `.bak` of any existing content. |
| `--position SIDE` | `right` | `left`, `right`, or `both`. `right` prepends the segment to the inner (left) edge of `status-right` so it sits ahead of your existing segments rather than at the far-right edge. |
| `--preset NAME` | `compact` | Embedded preset to wire in. |
| `--interval N` | 5 | Sets `status-interval`. |
| `--right-length N` | 200 | Sets `status-right-length`. |
| `--left-length N` | 80 | Sets `status-left-length`. |
| `--bind-popup KEY` | (none) | Bind a key to `display-popup -E openusage` (tmux 3.2+). |
| `--bind-refresh KEY` | (none) | Bind a key to refresh the status bar on demand. |
| `--binary PATH` | (auto) | Override the openusage binary path in the snippet. |
| `--with-font` | off | Install the bundled provider-icon font without prompting (requires `--write`). |
| `--no-font` | off | Skip the provider-icon font prompt entirely. |

Re-running `install --write` replaces the existing sentinel block in place; nothing outside the block is changed.

Run **bare `openusage tmux install` on an interactive terminal** to get the
one-stop **wizard**: it asks for position, preset, and emoji-vs-real-icons, then
writes the snippet, installs the icon font, and configures your terminal. Pass
any flag (or use a non-interactive stdin) to skip the wizard.

### `tmux font`

Manages the bundled provider-icon font, which lets the status bar render real
provider logos instead of emoji. See [Provider icons](../guides/tmux-integration.md#provider-icons-custom-font).

```
openusage tmux font setup        # auto-configure detected terminals (preferred path)
openusage tmux font patch        # iTerm2/Terminal.app: install an augmented copy of your font
openusage tmux font install      # install the standalone icon font (used by the fallback path)
openusage tmux font status       # family, version, path, and whether it is up to date
openusage tmux font uninstall    # remove the standalone font
```

`setup` writes per-range font fallback for kitty/Ghostty (and prints a snippet
for WezTerm) — your main font is untouched. `patch` is for terminals without
per-range fallback (iTerm2, Terminal.app): it copies your terminal font, adds
the glyphs under a new `… +OpenUsage` family, and installs it (original
untouched); pass `--base <file>` to patch a specific font. `patch` needs a
source checkout and Python 3 with fonttools.

`status` compares the installed font against the version embedded in the binary
by content hash, so it reports when an installed font is **outdated** after you
upgrade `openusage`. After setup, restart your terminal and tmux; the default
preset then auto-upgrades to `customfont` glyphs.

### `tmux uninstall`

```
openusage tmux uninstall
```

Removes the sentinel-bracketed block from the tmux config, takes a `.bak`, and clears the integration entry in settings.json. Leaves the file in place even if newly empty.

### `tmux presets`

Lists the 12 built-in presets with a sample line for each.

```
openusage tmux presets
openusage tmux presets --show claude-focused    # dump one preset as JSON
```

### `tmux variables`

Lists the variables `--format` accepts: snapshot attributes (`tool`, `provider`, `account`, `model`), built-in segments, and semantic aliases.

```
openusage tmux variables
openusage tmux variables --markdown             # markdown table
openusage tmux variables --provider claude_code # scope hint
```

### `tmux doctor`

Diagnoses tmux version, `$TMUX` env var, truecolor advertisement, daemon socket reachability, the currently detected active provider, and whether your tmux.conf already has the openusage block.

```
openusage tmux doctor
```

### `tmux preview`

Renders the status line with ANSI escapes (rather than tmux `#[...]` tokens) so you can preview the output in a regular terminal.

```
openusage tmux preview --preset compact
```

Accepts the same `--preset`, `--format`, `--segment`, `--provider`, `--strategy`, `--color-mode`, `--glyphs`, `--theme`, and `--source` flags as the default command.

### `tmux watch`

Foreground push-alert loop. Polls the daemon (or direct snapshots) and on a configured threshold cross calls `tmux display-message` and `tmux refresh-client -S`. Thresholds and cooldown live in `settings.tmux.alerts`.

```
openusage tmux watch
openusage tmux watch --background --alert-mode both
```

| Flag | Default | Purpose |
| --- | --- | --- |
| `--background` | off | Write a pidfile so a second invocation replaces the first. |
| `--alert-mode MODE` | from settings | `message`, `bell`, `both`, `none`. |
| `--interval DURATION` | 5s | Poll interval. |

Pidfile location: `~/.cache/openusage/tmux-watch.pid`.

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

## `openusage hub`

Runs an HTTP server that aggregates `UsageSnapshot` batches pushed from one or more worker machines, then renders the merged view in the same TUI as the local dashboard. See [Multi-machine aggregation](../guides/multi-machine.md) for the end-to-end setup.

```
openusage hub [--listen ADDR] [--headless] [--allow-public]
```

### Flags

| Flag | Default | Purpose |
|---|---|---|
| `--listen ADDR` | `:9190` (or `hub.listen_addr`) | TCP address to bind. `:9190` listens on all interfaces; `127.0.0.1:9190` is loopback-only. |
| `--headless` | off | Run the HTTP server without the TUI. Use this in containers or background services. |
| `--allow-public` | off | Opt-in to bind a non-loopback interface without an auth token. Without this flag, the hub refuses to start in that configuration. |

### Endpoints

| Endpoint | Method | Auth required |
|---|---|---|
| `/v1/push` | POST | Bearer (if a token is configured) |
| `/v1/snapshots` | GET | Bearer (if a token is configured) |
| `/healthz` | GET | never (liveness probe) |

### Auth posture

- Export `OPENUSAGE_HUB_TOKEN` to require `Authorization: Bearer <token>` on `/v1/push` and `/v1/snapshots`. `/healthz` stays unauthenticated for liveness probes.
- The auth token is **never persisted** to `settings.json` — supply it via the env var at runtime.

### Unsafe-default guard

Without an auth token, the hub refuses to bind to a non-loopback interface. The startup message points to three fixes:

```
hub: refusing to listen on ":9190" without auth_token.
  Choose one:
    1. export OPENUSAGE_HUB_TOKEN=<secret> to enable Bearer auth, OR
    2. bind to loopback only:  --listen 127.0.0.1:9190, OR
    3. pass --allow-public if you have a network-level firewall in place
```

Loopback (`127.0.0.1`, `localhost`, `[::1]`) is always allowed; the guard only triggers for `:port` (all-interfaces) and explicit non-loopback IPs/hostnames.

### Examples

```bash
openusage hub                                        # TUI on 127.0.0.1:9190
openusage hub --listen 127.0.0.1:9190                # explicit loopback bind
openusage hub --listen :9190 --allow-public          # bind 0.0.0.0 without auth (trusted LAN)
OPENUSAGE_HUB_TOKEN=s3cret openusage hub --headless  # bind 0.0.0.0 with Bearer auth
```

## `openusage hub-view`

Connects to a remote hub, polls `GET /v1/snapshots`, and renders the result in a read-only dashboard. No local providers or daemon are needed.

```
openusage hub-view <url> [--interval DURATION] [--token TOKEN]
```

Argument:

- `<url>` — base URL of the hub, e.g. `http://hub.lan:9190` or `https://openusage.example.com`. The trailing slash is trimmed.

### Flags

| Flag | Default | Purpose |
|---|---|---|
| `--interval DURATION` | `30s` (or `ui.refresh_interval_seconds`) | Poll interval for `/v1/snapshots`. |
| `--token TOKEN` | (none) | Bearer token sent in `Authorization`. Falls back to `OPENUSAGE_HUB_TOKEN`. |

### Examples

```bash
openusage hub-view http://192.168.1.10:9190
openusage hub-view https://openusage.example.com --interval 10s
OPENUSAGE_HUB_TOKEN=s3cret openusage hub-view http://hub:9190
```

The TUI shows `hub <url> · N machine snapshots` in its status line, and switches to an error state if the hub becomes unreachable.

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
- `OPENUSAGE_HUB_TOKEN` — Bearer token shared by `hub`, `hub-view`, and the daemon exporter
- `OPENUSAGE_THEME_DIR` — extra theme search paths
- `XDG_CONFIG_HOME`, `XDG_STATE_HOME` — base directories
- `CLAUDE_SETTINGS_FILE`, `CODEX_CONFIG_DIR` — tool-specific overrides

## See also

- [Paths reference](./paths.md) — every file path the CLI reads or writes
- [Configuration reference](./configuration.md) — `settings.json` schema
- [Daemon overview](../daemon/overview.md) — what the daemon does
- [Multi-machine aggregation](../guides/multi-machine.md) — `hub` and `hub-view` setup walkthrough
