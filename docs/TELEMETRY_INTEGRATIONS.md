# Telemetry Integrations (Native Setup)

This repository supports three native coding-agent telemetry streams:

1. OpenCode plugin hooks
2. Codex `notify` hook
3. Claude Code command hooks

All streams emit normalized telemetry events into the same SQLite store:

- `~/.local/state/openusage/telemetry.db`

When the OpenUsage app is running, background collection and canonical telemetry read-model updates are automatic.
You do not need to run `openusage telemetry collect` manually for normal operation.
OpenUsage does not auto-create synthetic providers from telemetry. Unmapped telemetry provider IDs are flagged for explicit user action.

## Provider Linking (Explicit Control)

If telemetry emits a provider ID that is not configured locally (for example `anthropic` from OpenCode while you track that spend under `claude_code`), add explicit links in `~/.config/openusage/settings.json`:

```json
{
  "telemetry": {
    "provider_links": {
      "anthropic": "claude_code"
    }
  }
}
```

Behavior:

1. No automatic telemetry-only providers are created.
2. If provider is unmapped, snapshots include diagnostics:
   - `telemetry_unmapped_providers`
   - `telemetry_provider_link_hint`
3. Canonical telemetry usage metrics are applied only to configured providers (or explicitly linked providers).

## 1) OpenCode (Plugin)

Install:

```bash
./plugins/openusage-telemetry/install.sh
```

This installs:

- `~/.config/opencode/plugins/openusage-telemetry.ts`
- plugin entry in `~/.config/opencode/opencode.json`

## 2) Codex (Native notify)

Install:

```bash
./plugins/codex-telemetry/install.sh
```

This installs:

- `~/.config/openusage/hooks/codex-notify.sh`
- `notify = ["~/.config/openusage/hooks/codex-notify.sh"]` in `~/.codex/config.toml`

## 3) Claude Code (Native hooks)

Install:

```bash
./plugins/claude-code-telemetry/install.sh
```

This installs:

- `~/.config/openusage/hooks/claude-hook.sh`
- command hooks in `~/.claude/settings.json` for:
  - `Stop`
  - `SubagentStop`
  - `PostToolUse`

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
