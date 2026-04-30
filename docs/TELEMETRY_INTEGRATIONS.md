# Telemetry Integrations

This repository supports three native coding-agent telemetry streams:

1. OpenCode plugin hooks
2. Codex `notify` hook
3. Claude Code command hooks

All streams emit normalized telemetry events into the same SQLite store:

- `~/.local/state/openusage/telemetry.db`

When the OpenUsage app is running, background collection and canonical telemetry read-model updates are automatic.
You do not need to run `openusage telemetry collect` manually for normal operation.
OpenUsage does not auto-create synthetic providers from telemetry. Unmapped telemetry provider IDs are flagged for explicit user action.

## Installing Integrations

All integration hook/plugin definitions are embedded in the `openusage` binary.
Use the built-in CLI to install, upgrade, or uninstall them:

```bash
# List detected integrations and their status
openusage integrations list

# List all integrations, including ones for tools not detected on this machine
openusage integrations list --all

# Install an integration by ID
openusage integrations install claude_code
openusage integrations install codex
openusage integrations install opencode

# Upgrade an integration to the latest embedded version
openusage integrations upgrade claude_code

# Upgrade all outdated integrations at once
openusage integrations upgrade --all

# Uninstall an integration (removes hook and unregisters from tool config)
openusage integrations uninstall claude_code
```

The daemon also prints a hint at startup when it detects tools with missing integrations.

## What Gets Installed

### OpenCode (Plugin)

- `~/.config/opencode/plugins/openusage-telemetry.ts`
- plugin entry in `~/.config/opencode/opencode.json`

### Codex (Notify Hook)

- `~/.config/openusage/hooks/codex-notify.sh`
- `notify = ["~/.config/openusage/hooks/codex-notify.sh"]` in `~/.codex/config.toml`

### Claude Code (Command Hooks)

- `~/.config/openusage/hooks/claude-hook.sh`
- command hooks in `~/.claude/settings.json` for:
  - `Stop`
  - `SubagentStop`
  - `PostToolUse`

## Provider Linking (Explicit Control)

Telemetry events are tagged with whatever `provider_id` the source tool uses. When that id doesn't match any configured account, openusage attempts a link via `telemetry.provider_links`, then falls back to flagging the source as unmapped.

### Built-in defaults

The following links are applied automatically and cover known rename mismatches between source-tool vocabulary and openusage's internal provider ids:

| Source provider id | Mapped to    | Why                                                    |
|--------------------|--------------|--------------------------------------------------------|
| `anthropic`        | `claude_code`| OpenCode/Codex/Claude Code emit `anthropic`            |
| `google`           | `gemini_api` | OpenCode emits `google` for the Gemini API             |
| `github-copilot`   | `copilot`    | OpenCode emits `github-copilot` for GitHub Copilot     |

Identity links (e.g. `openai` → `openai`) are intentionally not enumerated — direct id matches are handled by the matcher without a link.

### User overrides

Add custom or override entries in `~/.config/openusage/settings.json`:

```json
{
  "telemetry": {
    "provider_links": {
      "google": "my-personal-gemini-account",
      "moonshot": "kimi"
    }
  }
}
```

User entries take precedence over defaults. The daemon picks up changes on the next poll cycle (no restart needed).

### Interactive remap

Open the TUI Settings modal (`s`), navigate to **6 TELEM**. Unmapped telemetry sources are listed below the time-window picker, each with a category badge:

- `[no account configured]` — no openusage account exists for this source.
- `[suggested: <id>]` — a configured provider id whose name overlaps with the source. Press `m` to open a picker pre-selecting the suggestion.
- `[mapped → <id>, target not configured]` — a link points to an id that has no account. Resolve by changing the link target or creating the missing account.

Keybindings on each unmapped row:

- `m` (or Enter) — open a target picker showing all configured provider ids; Enter to apply, Esc to cancel.
- `x` — clear an existing user-defined link for this source (built-in defaults can't be cleared this way; override them with a different target instead).

### Diagnostics emitted on snapshots

When at least one source is unmapped, every snapshot picks up two diagnostic keys:

- `telemetry_unmapped_providers` — comma-separated list of unmapped source ids.
- `telemetry_unmapped_meta` — comma-separated `<source>=<category>[:<suggestion-or-target>]` entries. Categories: `unconfigured`, `mapped_target_missing`. The optional suffix is a configured provider id suggestion (for `unconfigured`) or the link's target id (for `mapped_target_missing`).

### Behavior summary

1. No automatic telemetry-only providers are created — sources without a configured account stay flagged.
2. Canonical telemetry usage metrics are applied only to configured providers or explicitly linked providers.
3. Built-in defaults can be overridden but not erased; setting `provider_links.<source>` replaces the default for that source.

## Optional runtime env vars (all integrations)

- `OPENUSAGE_TELEMETRY_ENABLED=true|false`
- `OPENUSAGE_BIN=/absolute/path/to/openusage`
- `OPENUSAGE_TELEMETRY_ACCOUNT_ID=<logical account override>`
- `OPENUSAGE_TELEMETRY_DB_PATH=/path/to/telemetry.db`
- `OPENUSAGE_TELEMETRY_SPOOL_DIR=/path/to/spool`
- `OPENUSAGE_TELEMETRY_SPOOL_ONLY=true|false`
- `OPENUSAGE_TELEMETRY_VERBOSE=true|false`

## Verify Ingestion

OpenCode:

```bash
sqlite3 ~/.local/state/openusage/telemetry.db "select r.source_system, r.source_channel, e.event_type, count(*) from usage_events e join usage_raw_events r on r.raw_event_id=e.raw_event_id where r.source_system='opencode' group by 1,2,3 order by 1,2,3;"
```

Codex:

```bash
sqlite3 ~/.local/state/openusage/telemetry.db "select r.source_system, r.source_channel, e.event_type, count(*) from usage_events e join usage_raw_events r on r.raw_event_id=e.raw_event_id where r.source_system='codex' group by 1,2,3 order by 1,2,3;"
```

Claude Code:

```bash
sqlite3 ~/.local/state/openusage/telemetry.db "select r.source_system, r.source_channel, e.event_type, count(*) from usage_events e join usage_raw_events r on r.raw_event_id=e.raw_event_id where r.source_system='claude_code' group by 1,2,3 order by 1,2,3;"
```

Inspect latest canonical metrics:

```bash
sqlite3 ~/.local/state/openusage/telemetry.db <<'SQL'
select
  e.occurred_at,
  r.source_system,
  r.source_channel,
  e.event_type,
  e.provider_id,
  e.account_id,
  e.model_raw,
  e.input_tokens,
  e.output_tokens,
  e.reasoning_tokens,
  e.cache_read_tokens,
  e.cache_write_tokens,
  e.total_tokens,
  e.cost_usd,
  e.requests,
  e.session_id,
  e.turn_id,
  e.message_id,
  e.tool_call_id,
  e.tool_name
from usage_events e
join usage_raw_events r on r.raw_event_id = e.raw_event_id
order by e.occurred_at desc
limit 100;
SQL
```
