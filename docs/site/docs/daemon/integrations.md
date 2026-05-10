---
title: Tool integrations
description: Install hook integrations for Claude Code, Codex, and OpenCode so every turn is captured by the daemon.
---

# Tool integrations

Integrations install hook scripts and plugins that emit telemetry to the [daemon](./overview.md) as your AI tools run. With integrations active, every turn, message, and tool call is recorded the moment it happens — no polling lag, no gaps when the dashboard isn't open.

OpenUsage ships three official integrations.

| ID | Tool | Hook artifact | Tool config | Format |
|---|---|---|---|---|
| `claude_code` | Claude Code | `~/.config/openusage/hooks/claude-hook.sh` | `~/.claude/settings.json` | JSON |
| `codex` | Codex | `~/.config/openusage/hooks/codex-notify.sh` | `~/.codex/config.toml` | TOML |
| `opencode` | OpenCode | `~/.config/opencode/plugins/openusage-telemetry.ts` | `~/.config/opencode/opencode.json` | JSON |

## Listing integrations

```bash
openusage integrations list
openusage integrations list --all   # include not-installed
```

Each row shows: ID, tool name, install state, version, and any pending upgrade.

## Install

```bash
openusage integrations install <id>
```

The installer is symmetric and idempotent. On install it:

1. Reads any existing template to detect a prior version.
2. Creates parent directories.
3. Renders the embedded template, expanding `__OPENUSAGE_INTEGRATION_VERSION__` and `__OPENUSAGE_BIN_DEFAULT__`.
4. Backs up any existing file to `<file>.bak`.
5. Writes the rendered hook script (mode `0755`) or plugin (mode `0644`).
6. Patches the tool's config file to register the hook entry.
7. Writes the patched config (mode `0600`) — preserving existing keys.
8. Saves install state (version, timestamp) into `~/.config/openusage/settings.json` under `integrations.<id>`.

Hook scripts are tiny shell or TS wrappers that pipe the tool's event payload into:

```
openusage telemetry hook <source>
```

…which forwards over the Unix socket (or to the spool, if the daemon is offline).

## Uninstall

```bash
openusage integrations uninstall <id>
```

Uninstall is the inverse of install:

1. Removes the hook script or plugin file.
2. De-registers the entry from the tool's config (preserves siblings).
3. Restores the most recent `.bak` if present and the config would otherwise be empty.
4. Marks `integrations.<id>.installed = false` in settings.

No telemetry data is touched. Old events stay in `telemetry.db` until retention prunes them.

## Upgrade

```bash
openusage integrations upgrade <id>
openusage integrations upgrade --all
```

Reinstalls only when the embedded template version is newer than the installed version. Existing config entries are preserved; only the script body and version stamp change.

---

## claude_code

**What it adds.** A `Hook` entry in `~/.claude/settings.json` that runs on every Claude Code turn. The hook delivers a JSON event with token counts, model id, message ids, and tool calls. Telemetry source string: `anthropic` (mapped to display provider `claude_code` by [provider links](./storage.md#provider-links)).

**Files written.**

```
~/.config/openusage/hooks/claude-hook.sh    (mode 0755)
~/.claude/settings.json                     (patched, mode 0600)
```

**Install.**

```bash
openusage integrations install claude_code
```

**Uninstall.**

```bash
openusage integrations uninstall claude_code
```

Override the Claude config path with `CLAUDE_SETTINGS_FILE` when needed.

---

## codex

**What it adds.** A `notify` entry in `~/.codex/config.toml` pointing at a shell wrapper. Codex invokes the script after each turn with a JSON payload on stdin. Telemetry source: `codex`.

**Files written.**

```
~/.config/openusage/hooks/codex-notify.sh   (mode 0755)
~/.codex/config.toml                        (patched, mode 0600)
```

**Install.**

```bash
openusage integrations install codex
```

**Example patched TOML.**

```toml
[notify]
command = ["/Users/me/.config/openusage/hooks/codex-notify.sh"]
```

Override the Codex config directory with `CODEX_CONFIG_DIR`.

---

## opencode

**What it adds.** A TypeScript plugin loaded by OpenCode at startup. The plugin subscribes to OpenCode's session events and POSTs them to the daemon's `/v1/hook/opencode` endpoint. Telemetry source: `opencode`.

**Files written.**

```
~/.config/opencode/plugins/openusage-telemetry.ts   (mode 0644)
~/.config/opencode/opencode.json                    (patched, mode 0600)
```

**Install.**

```bash
openusage integrations install opencode
```

**Example patched config.**

```json
{
  "plugins": {
    "openusage-telemetry": {
      "enabled": true
    }
  }
}
```

The plugin uses `OPENUSAGE_BIN` and `OPENUSAGE_TELEMETRY_SOCKET` if set; otherwise it falls back to the embedded defaults captured at install time.

---

## How hook events become snapshots

1. Tool fires hook → wrapper script runs → `openusage telemetry hook <source>` reads stdin.
2. Hook command opens the Unix socket and POSTs to `/v1/hook/{source}`. If the dial fails (socket missing, daemon down), the event is appended to the on-disk spool.
3. Daemon pipeline ingests the event, dedups by `tool_call_id` → `message_id` → `turn_id` → fingerprint hash, and stores it in `usage_events`.
4. Provider links map source → display provider id. Defaults: `anthropic → claude_code`, `google → gemini_api`, `github-copilot → copilot`. Override under `telemetry.provider_links` in [settings.json](../reference/configuration.md).
5. The TUI requests `/v1/read-model` on each refresh; the daemon hydrates a `UsageSnapshot` per provider for the current time window.

:::tip Verifying a hook
Trigger one turn in your tool, then watch `~/.local/state/openusage/daemon.stderr.log` (with `OPENUSAGE_DEBUG=1`). You should see one `POST /v1/hook/<source>` per turn. If you instead see entries written to `telemetry-spool/`, the daemon is not running.
:::
