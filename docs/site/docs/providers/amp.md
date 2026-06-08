---
title: Amp
description: Track Amp / AmpCode threads, credits, and per-model token usage in OpenUsage.
sidebar_label: Amp
keywords: [amp usage tracker, amp quota tracking, amp cost tracking, amp token usage, track amp spend locally]
---

# Amp

Local-file provider for the Amp coding agent. Reads per-thread JSON snapshots and the credit ledger from Amp's per-user data directory. No network calls are made and no API key is required.

## At a glance

- **Provider ID** — `amp`
- **Detection** — `amp` binary on `PATH` or the Amp data directory exists on disk
- **Auth** — none (local files only)
- **Type** — coding agent
- **Tracks**:
  - Credits spent (total and today)
  - Per-model input / output / cache-read / cache-write tokens
  - Sessions (one per thread)
  - Messages today and all-time
  - Per-day cost and message series

## Setup

### Auto-detection

OpenUsage registers the provider as soon as either the `amp` binary is on `PATH` or the data directory exists. Run Amp at least once to create the threads directory and ledger.

The data directory is resolved in this order:

1. `$XDG_DATA_HOME/amp` when `XDG_DATA_HOME` is set
2. macOS: `~/Library/Application Support/amp`
3. Linux / other Unix: `~/.local/share/amp`
4. Windows: `%APPDATA%/amp` (or `~/AppData/Roaming/amp`)

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "amp",
      "provider": "amp",
      "extra": {
        "data_dir": "~/.local/share/amp",
        "threads_dir": "~/.local/share/amp/threads",
        "ledger_path": "~/.local/share/amp/ledger.jsonl"
      }
    }
  ]
}
```

All three keys are optional. The provider honours `data_dir`, `threads_dir`, and `ledger_path` as `extra` path hints; `data_dir` may also be supplied via the top-level `binary` field for compatibility with other local-file providers.

## Data sources & how each metric is computed

Amp keeps two parallel records of every assistant turn:

1. **Per-thread JSON** at `<data_dir>/threads/<thread_id>.json` — full message log including token usage on each assistant message.
2. **Credit ledger** at `<data_dir>/ledger.jsonl` — one JSON line per billed assistant response, keyed by `toMessageId` and carrying the authoritative credit cost.

On every poll the provider parses every `*.json` file in the threads directory, loads the ledger, and reconciles the two by message id.

### Thread parser

- Source: `<threads_dir>/*.json`. Each file is one thread; `messages[]` is walked and only `role == "assistant"` entries with non-zero token usage are kept.
- Field tolerance: snake_case and camelCase aliases are both accepted (`input` / `input_tokens` / `inputTokens`, `cache_read` / `cacheReadInputTokens` / `cache_read_input_tokens`, etc.). Negative token counts are clamped to zero.
- Timestamp fallback: message `timestamp` / `created_at` / `createdAt` → thread `created_at` / `createdAt` → file mtime.

### Ledger reconciliation

- Source: `<data_dir>/ledger.jsonl`. Each line is a record keyed by `toMessageId` (or `to_message_id`) with a `credits` value (or `cost` fallback) and an optional token bag.
- Merge rule when a thread message and a ledger row share an id:
  - **cost** comes from the ledger (authoritative billing unit)
  - **tokens** are per-field max-merged across both sources
  - **timestamp** prefers the ledger's explicit timestamp, otherwise the thread's
  - **model** prefers the ledger value when present
- Ledger rows that never match a thread message are still included as synthetic events so totals don't lag when the ledger advances ahead of the thread JSON.
- Duplicate ledger rows for the same id are folded by keeping the higher credit value.

### Cross-file dedup

The same message id can appear in multiple thread files (forks, re-saves). After reconciliation, events are deduped by `MessageID` with per-field max-merge on tokens and max on credit cost. Events without a message id pass through verbatim.

### Cache-creation vs cache-read

Amp records both. They are surfaced as separate metrics:

- `total_cache_read_tokens` — input tokens served from cache
- `total_cache_write_tokens` — input tokens written into cache (the "create" half)

The two are accumulated independently and shown side-by-side in the `Tokens` compact row on the tile.

### Surfaced metrics

| Metric | Window | Source |
|---|---|---|
| `total_cost` | all-time | sum of ledger credits across all events |
| `today_cost` | 1d | sum of credits for events whose timestamp falls in today (local time) |
| `total_input_tokens` / `total_output_tokens` | all-time | per-event sum |
| `total_cache_read_tokens` / `total_cache_write_tokens` | all-time | per-event sum |
| `total_messages` / `messages_today` | all-time / 1d | event count |
| `total_sessions` | all-time | distinct thread ids |

Per-model rows (`ModelUsage`) are emitted with raw model id, per-field token totals, cost, and a request count.

### What's NOT tracked

- **Subscription billing.** The cost figure is denominated in Amp credits taken from the ledger; whether your account is on a flat-rate plan is not surfaced.
- **MCP tool counts.** Amp's local payloads do not expose MCP call breakdowns, so the detail-view MCP section is hidden.

### How fresh is the data?

- Polling: every 30 s by default.
- The provider uses a `HasChanged` hook: it stats the threads directory and the ledger and skips re-parsing when neither has been touched since the last poll.

## Files read

- `<data_dir>/threads/*.json` — per-thread assistant message log
- `<data_dir>/ledger.jsonl` — authoritative credit ledger

## Caveats

- Token fields appear under multiple aliases in real-world Amp payloads. The parser folds them all into the canonical four (`input`, `output`, `cache_read`, `cache_write`), so a missing alias is not a failure.
- A truly broken ledger (unparseable open) is recorded as a diagnostic (`amp_ledger_error`) and does not block thread-only reporting; tokens still flow, only the cost figure is degraded.
- Skipped malformed ledger lines are counted under `amp_ledger_skipped_lines` for diagnosis.

## Troubleshooting

- **Tile shows "No Amp thread files found"** — confirm `<data_dir>/threads/` exists and contains `*.json` files. Run an Amp thread to populate it.
- **Cost is zero but tokens are non-zero** — `ledger.jsonl` is missing or malformed. Check the `amp_ledger_error` and `amp_ledger_skipped_lines` diagnostics on the tile.
- **Wrong data directory** — the provider logs the resolved `data_dir`, `threads_dir`, and `ledger_path` in the `Raw` block of the snapshot. Override via the `data_dir` / `threads_dir` / `ledger_path` `extra` keys in `settings.json`.

## Related

- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
- [Crush](./crush.md) — sibling per-project SQLite agent
