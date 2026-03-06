# Copilot Telemetry Integration — Design Doc

## Problem

The copilot provider is missing **Model Burn** metrics in the dashboard. Investigation reveals that Copilot CLI v0.0.415 defines `assistant.usage` events in its schema (with model, tokens, cost) but **never emits them** to session `events.jsonl` files. Our telemetry collector has working code to parse these events, but there's zero data to parse.

Current state:
- **11,594** `limit_snapshot` events (from API polling) — working
- **267** `tool_usage` events (from events.jsonl) — working
- **0** `message_usage` events — broken, no source data

As a result, the copilot detail view shows tool usage, language breakdown, MCP usage, and code stats — but has a completely empty Model Burn section.

## Data Sources Available

| Source | What's There | What's Missing |
|--------|-------------|----------------|
| `events.jsonl` | tool events, session.start (selectedModel), model_change, turn_start/turn_end | assistant.usage (never emitted) |
| `session-store.db` | turns (user_message, assistant_response), session_files | No token counts |
| `~/.copilot/logs/*.log` | CompactionProcessor: `Utilization X% (used/limit tokens)` per turn | No per-model breakdown |
| GitHub API | Plan, quota, features, rate limits | No per-model token/cost data |
| `session.shutdown` | modelMetrics (per-model requests/cost), code changes | Sessions rarely shut down cleanly |

## Solution

Two-phase approach that delivers value immediately, then adds richer real-time data.

### Phase 1: Synthesize model metrics from existing data (no plugin)

Generate `message_usage` telemetry events from turn sequences already present in session files:

1. **Model tracking**: `session.start` has `selectedModel`, `session.model_change` tracks switches. We know which model is active for each turn.

2. **Turn counting**: `assistant.turn_start`/`assistant.turn_end` pairs represent LLM calls. Each pair = 1 request for the active model.

3. **Token estimation from logs**: CompactionProcessor log lines show `Utilization X% (used/limit tokens)`. Positive deltas between consecutive entries approximate input tokens consumed per turn. This is imprecise but gives us a reasonable signal.

4. **Session shutdown fallback**: When `session.shutdown` events exist, they contain authoritative `modelMetrics` with per-model request counts and costs.

**Output**: For each turn, emit a synthetic `TelemetryEventTypeMessageUsage` event with:
- `ModelRaw`: active model from session context
- `Requests`: 1
- `InputTokens`: estimated from log delta (or nil if unavailable)
- `OutputTokens`: nil (cannot be estimated)

This gets Model Burn showing immediately — at minimum a "Model Activity" breakdown by request count, with token estimates where available.

### Phase 2: Copilot plugin with hooks (real-time capture)

Copilot CLI supports a **plugin system** with hooks that fire on session events. Create an `openusage` plugin:

```
~/.copilot/pkg/openusage/
  plugin.json
  hooks.json
  hooks/
    post-tool-use.sh    — captures tool context + model
    session-end.sh      — captures session summary with code changes
```

**Hook types to implement:**

| Hook | Fires When | Data Captured |
|------|-----------|---------------|
| `postToolUse` | After each tool execution | toolName, toolArgs (truncated), success, model context, timing |
| `sessionEnd` | Session terminates | reason, totalPremiumRequests, codeChanges, duration |

**Delivery**: Same pattern as claude code hooks — POST to daemon unix socket, fallback to spool directory.

**Delivery**: Same pattern as claude code hooks — POST to daemon unix socket, fallback to spool directory.

**Integration management**: Add `copilotDef` to `internal/integrations/definitions.go`. Note: the current framework is single-file only (one template → one target file). For a multi-file copilot plugin (plugin.json + hooks.json + scripts), we have two options:
1. Use `copilot plugin install /local/path` to install from a rendered directory (preferred — leverages copilot's own plugin manager)
2. Extend the integration framework with multi-file support

Option 1 is simpler: we render the plugin directory, then shell out to `copilot plugin install`. The integration detector checks `copilot plugin list` output.

### Phase 2b: Future — assistant.usage capture

If Copilot CLI starts emitting `assistant.usage` events to `events.jsonl` in a future version, our existing telemetry collector code (telemetry.go lines 576-640) will automatically pick them up with no changes needed. The synthetic turn-based metrics from Phase 1 will be superseded by accurate per-turn token/cost data.

## Implementation Tasks

### Task 1: Synthesize message_usage from turns in telemetry collector

**File**: `internal/providers/copilot/telemetry.go`

Modify `parseCopilotTelemetrySessionFile()`:
- `currentModel` is already tracked via `session.model_change` and `session.info`. Seed it also from `session.start.selectedModel` (Task 3).
- Add case for `assistant.turn_end`: if `assistantUsageSeen` is still false, emit a synthetic `TelemetryEventTypeMessageUsage` with the active model and `Requests: 1`
- Only emit if we have a non-empty model name
- Use existing `copilotTelemetryBasePayload()` helper for the payload
- Mark synthetic events with `payload["synthetic"] = true` so they can be distinguished from real assistant.usage events

**Note**: `assistant.turn_start`/`turn_end` are not currently handled in the switch block — they need new cases. The `turnIndex` variable is already tracked.

**Estimated change**: ~25 lines in the existing switch/case block.

### Task 2: Estimate tokens from CompactionProcessor logs

**File**: `internal/providers/copilot/telemetry.go`

New function `parseCopilotLogTokenDeltas(logsDir string) map[string][]logTokenDelta`:
- Parse all `~/.copilot/logs/*.log` files
- Extract CompactionProcessor lines with timestamps and token counts
- Compute positive deltas between consecutive entries
- Return a time-indexed map of token deltas

In `Collect()`, after parsing session files, cross-reference turn timestamps with log token deltas to attach estimated `InputTokens` to synthetic message_usage events.

**Estimated change**: ~60 lines new function + ~15 lines integration.

### Task 3: Extract selectedModel from session.start

**File**: `internal/providers/copilot/telemetry.go`

The `session.start` event has a `selectedModel` field (confirmed in the copilot schema). The `sessionStartData` struct in `copilot.go` (not telemetry.go) has `SessionID`, `CopilotVersion`, `StartTime`, and `Context` — but no `SelectedModel`. The struct is also used in telemetry.go's `parseCopilotTelemetrySessionFile`.

Add `SelectedModel string \`json:"selectedModel"\`` to `sessionStartData` and seed `currentModel` from it in the `session.start` case (both in copilot.go and telemetry.go).

**Estimated change**: ~5 lines across 2 files.

### Task 4: Create copilot hook scripts

**Files**:
- `internal/integrations/assets/copilot-post-tool-use.sh.tpl`
- `internal/integrations/assets/copilot-session-end.sh.tpl`

Follow the claude-hook.sh.tpl pattern:
- Read JSON from stdin
- Check `OPENUSAGE_TELEMETRY_ENABLED`
- POST to daemon unix socket, fallback to spool
- Payload: `{"source":"copilot","account_id":"copilot","payload":{...}}`

The `postToolUse` hook receives: `sessionId`, `timestamp`, `toolName`, `toolArgs`, `toolResult`, `success`.
The `sessionEnd` hook receives: `sessionId`, `timestamp`, `reason`, and optionally session-level stats.

**Estimated change**: ~70 lines per script.

### Task 5: Create copilot plugin manifest

**Files**:
- `internal/integrations/assets/copilot-plugin.json.tpl`
- `internal/integrations/assets/copilot-hooks.json.tpl`

```json
// plugin.json
{
  "name": "openusage",
  "description": "OpenUsage telemetry integration for GitHub Copilot CLI",
  "version": "__OPENUSAGE_INTEGRATION_VERSION__",
  "hooks": "hooks.json"
}

// hooks.json
{
  "postToolUse": [{ "script": "hooks/post-tool-use.sh", "timeoutSec": 5 }],
  "sessionEnd": [{ "script": "hooks/session-end.sh", "timeoutSec": 5 }]
}
```

**Estimated change**: ~20 lines.

### Task 6: Add copilot integration definition

**File**: `internal/integrations/definitions.go`

The integration framework is single-file only. For the multi-file copilot plugin, use a hybrid approach:
- `Definition` renders a single hook script (the `postToolUse` hook) as the primary target file
- The `ConfigPatcher` renders the full plugin directory (plugin.json + hooks.json + hook scripts) to a temp dir, then runs `copilot plugin install /path/to/dir`
- The `Detector` checks `copilot plugin list` for "openusage" and parses its version

Alternative (simpler for Phase 2): Skip the integration framework entirely and add a dedicated `copilot integration install` CLI subcommand that handles the multi-file setup directly. Register it in the settings modal as a custom action.

**Estimated change**: ~100 lines.

### Task 7: Add ParseHookPayload for copilot

**File**: `internal/providers/copilot/telemetry.go`

Currently `ParseHookPayload` returns `ErrHookUnsupported`. Implement it to parse the hook payloads from Task 4:
- `postToolUse` payloads → `TelemetryEventTypeToolUsage` events (with richer context than events.jsonl)
- `sessionEnd` payloads → `TelemetryEventTypeTurnCompleted` events with code change metadata

**Estimated change**: ~50 lines.

### Task 8: Tests

**File**: `internal/providers/copilot/telemetry_test.go`

- Test synthetic message_usage generation from turn sequences
- Test selectedModel extraction from session.start
- Test log token delta parsing
- Test ParseHookPayload for both hook types
- Test integration definition detection

**Estimated change**: ~150 lines.

## Task Ordering

```
Task 3 (selectedModel extraction)     ← prerequisite for Task 1
Task 1 (synthetic message_usage)      ← core fix, enables model burn
Task 2 (log token estimation)         ← enrichment, can be parallel with Task 1
Task 8 (tests for Tasks 1-3)          ← after Tasks 1-3

Task 4 (hook scripts)                 ← independent
Task 5 (plugin manifest)              ← independent
Task 6 (integration definition)       ← depends on Tasks 4-5
Task 7 (ParseHookPayload)             ← depends on Task 4 payload format
Task 8 (tests for Tasks 4-7)          ← after Tasks 4-7
```

Phase 1 (Tasks 1-3 + tests) can ship independently.
Phase 2 (Tasks 4-7 + tests) can ship as a follow-up.

## Non-goals

- **Capturing assistant.usage at hook time**: These events are ephemeral in the copilot runtime and not exposed to the hook context. We cannot intercept them.
- **Per-turn cost estimation**: Without assistant.usage, we don't know costs. We show request counts and estimated tokens, not costs.
- **Modifying copilot's events.jsonl format**: We work with what copilot gives us.
- **preToolUse hooks**: We don't need to block or modify tool execution, only observe.

## Risks

1. **Token estimation accuracy**: CompactionProcessor log deltas are approximate. Token counts may be off by 10-30%. This is acceptable for a "Model Burn" overview — the metric labels will indicate estimates where applicable.

2. **Log file rotation**: Copilot may rotate log files. We scan all available logs on each collection cycle. Historical data may be lost if logs are cleaned up.

3. **Plugin format stability**: Copilot CLI plugin system is new (GA Feb 2026). The manifest format may change. We pin to a version and detect incompatibilities in the integration status check.

4. **Session state rotation**: Copilot aggressively rotates `session-state/` directories. The session-store.db fallback already handles this for tool events. Synthetic message_usage events may be incomplete for rotated sessions.
