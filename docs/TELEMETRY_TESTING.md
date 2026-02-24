# Telemetry Collection Testing

## What was implemented
- Unified telemetry ingestion store (`usage_raw_events`, `usage_events`, `usage_reconciliation_windows`).
- Idempotent ingest with dedup keying.
- Local spool queue with retry metadata.
- Collectors for:
  - Codex session JSONL (`~/.codex/sessions`)
  - Claude Code conversation JSONL (`~/.claude/projects`, `~/.config/claude/projects`)
  - OpenCode SQLite (`~/.local/share/opencode/opencode.db`) + optional event JSONL/NDJSON (`~/.opencode/events`, `~/.opencode/logs`, or explicit file/dirs)
- CLI entrypoint:
  - `openusage telemetry collect`
  - `openusage telemetry hook opencode`

## Quick start
1. Dry run:
```bash
go run ./cmd/openusage telemetry collect --dry-run --verbose
```

2. Ingest events:
```bash
go run ./cmd/openusage telemetry collect --verbose
```

3. DB path default:
```text
~/.local/state/openusage/telemetry.db
```

4. Spool path default:
```text
~/.local/state/openusage/telemetry-spool
```

## Useful flags
- `--db-path <path>`
- `--spool-dir <path>`
- `--codex-sessions <path>`
- `--claude-projects <path>`
- `--claude-projects-alt <path>`
- `--opencode-events-dirs <dir1,dir2>`
- `--opencode-events-file <path>`
- `--opencode-db <path>`
- `--max-flush <n>` (`0` means no limit)
- `--dry-run`
- `--verbose`

## Hook ingest smoke test (manual payload)
```bash
cat > /tmp/opencode-hook-event.json <<'JSON'
{"hook":"tool.execute.after","timestamp":1771754406000,"input":{"tool":"shell","sessionID":"sess-1","callID":"tool-1","args":{"command":"echo hi"}},"output":{"title":"Shell","output":"hi","metadata":{}}}
JSON
```

```bash
go run ./cmd/openusage telemetry hook opencode --verbose < /tmp/opencode-hook-event.json
```

## OpenCode plugin hook test
1. Install plugin:
```bash
./plugins/openusage-telemetry/install.sh
```

2. Restart OpenCode and run one prompt.

3. Verify OpenCode hook events are ingested:
```bash
sqlite3 ~/.local/state/openusage/telemetry.db "select r.source_system, r.source_channel, e.event_type, count(*) from usage_events e join usage_raw_events r on r.raw_event_id=e.raw_event_id where r.source_system='opencode' group by 1,2,3 order by 1,2,3;"
```
You may also see `raw_envelope` rows for event types we do not normalize yet; these preserve full payloads for later analysis.

4. Collect ground-truth usage from OpenCode SQLite:
```bash
go run ./cmd/openusage telemetry collect --verbose --opencode-db ~/.local/share/opencode/opencode.db
```

5. Verify rich metrics are present:
```bash
sqlite3 ~/.local/state/openusage/telemetry.db <<'SQL'
select
  e.occurred_at,
  e.event_type,
  e.provider_id,
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
  e.tool_name,
  json_extract(r.source_payload, '$.context.parts_total') as context_parts_total,
  json_extract(r.source_payload, '$.context.parts_by_type') as context_parts_by_type
from usage_events e
join usage_raw_events r on r.raw_event_id = e.raw_event_id
where r.source_system = 'opencode'
  and r.source_channel in ('sqlite', 'hook')
order by e.occurred_at desc
limit 50;
SQL
```

6. Inspect full raw payload:
```bash
sqlite3 ~/.local/state/openusage/telemetry.db "select r.source_payload from usage_events e join usage_raw_events r on r.raw_event_id=e.raw_event_id where r.source_system='opencode' order by e.occurred_at desc limit 1;" | jq .
```

## Verifying output
```bash
sqlite3 ~/.local/state/openusage/telemetry.db "select count(*) from usage_raw_events;"
sqlite3 ~/.local/state/openusage/telemetry.db "select count(*) from usage_events;"
```

## OpenCode test with synthetic event file
Create a temporary event file:
```bash
cat > /tmp/opencode-events.jsonl <<'JSONL'
{"type":"message.updated","properties":{"info":{"id":"msg-1","sessionID":"sess-1","role":"assistant","parentID":"turn-1","modelID":"gpt-5-codex","providerID":"zen","cost":0.012,"tokens":{"input":120,"output":40,"reasoning":5,"cache":{"read":10,"write":2}},"time":{"created":1771754400000,"completed":1771754405000},"path":{"cwd":"/tmp/work"}}}}
{"type":"tool.execute.after","payload":{"sessionID":"sess-1","messageID":"msg-1","toolCallID":"tool-1","toolName":"shell","timestamp":1771754406000}}
JSONL
```

Ingest it:
```bash
go run ./cmd/openusage telemetry collect --opencode-events-file /tmp/opencode-events.jsonl --verbose
```

## Notes
- Running collect repeatedly is safe (canonical events dedupe by computed key).
- `usage_raw_events` is append-only by design; duplicates still produce raw rows.
