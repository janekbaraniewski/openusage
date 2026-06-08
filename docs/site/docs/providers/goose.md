---
title: Goose
description: Track Goose agent sessions, per-model token usage, and accumulated cost in OpenUsage.
sidebar_label: Goose
keywords: [goose usage tracker, goose quota tracking, goose cost tracking, goose token usage, track goose spend locally]
---

# Goose

Local-data provider for Block's Goose agent. Reads sessions out of `sessions.db`, the SQLite store Goose maintains on disk. No network calls are made and no authentication is required.

## At a glance

- **Provider ID** — `goose`
- **Detection** — `goose` binary on `PATH` or `sessions.db` exists at one of the expected locations
- **Auth** — none (local SQLite reads only)
- **Type** — coding agent
- **Tracks**:
  - Sessions (total, today, 7d)
  - Per-model input / output / total / reasoning tokens
  - Accumulated cost (when the upstream schema records it)
  - Daily series for sessions and tokens

## Setup

### Auto-detection

Either signal is sufficient: the `goose` binary on `PATH`, or a `sessions.db` at one of the known locations. The provider's `Fetch` handles a missing DB gracefully, so a freshly installed binary registers a tile that simply reports "No Goose sessions recorded" until the first session.

`sessions.db` location is resolved in this order. The first existing file wins:

1. `$GOOSE_PATH_ROOT/data/sessions/sessions.db` when `GOOSE_PATH_ROOT` is set
2. macOS:
   - `~/Library/Application Support/Block/goose/sessions/sessions.db`
   - `~/Library/Application Support/goose/sessions/sessions.db`
   - `$XDG_DATA_HOME/goose/sessions/sessions.db` (or `~/.local/share/goose/sessions/sessions.db`)
3. Linux:
   - `$XDG_DATA_HOME/goose/sessions/sessions.db` (or `~/.local/share/goose/sessions/sessions.db`)
   - `~/.local/share/Block/goose/sessions/sessions.db` (legacy)
4. Windows:
   - `%APPDATA%/Block/goose/data/sessions/sessions.db`
   - `%APPDATA%/goose/data/sessions/sessions.db`

The "Block" qualifier is what upstream's `etcetera` crate produces on macOS and current Windows builds; the unqualified variants cover older installs.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "goose",
      "provider": "goose",
      "extra": {
        "db_path": "/Users/me/Library/Application Support/Block/goose/sessions/sessions.db"
      }
    }
  ]
}
```

`db_path` is the only path-hint key. The override is used only when the file exists; otherwise the provider falls back to the default candidates above.

## Data sources & how each metric is computed

### Sessions table

The provider opens `sessions.db` read-only and runs a single `SELECT` against the `sessions` table. Goose's schema has evolved across migrations (1 through 9+), so the provider probes `PRAGMA table_info(sessions)` first and falls back column-by-column.

Columns the provider looks for:

| Column | Used as |
|---|---|
| `id` | session id |
| `model_config_json` | source of model name (required) |
| `created_at` | session day attribution |
| `provider_name` | upstream provider hint (newer schemas) |
| `accumulated_input_tokens` / `accumulated_output_tokens` / `accumulated_total_tokens` | preferred token totals |
| `input_tokens` / `output_tokens` / `total_tokens` | fallback when accumulated columns are missing |
| `accumulated_cost` | per-session accumulated cost in USD |

When `model_config_json` is not present at all, the provider returns no sessions (graceful empty) rather than erroring; without a model name no per-model breakdown is possible.

### Model name extraction

`model_config_json` is parsed as JSON and the first non-empty value of `model_name`, `model`, or `name` becomes the model id. Rows with an unrecoverable model name are skipped.

### Token preference

`accumulated_*` columns are preferred over the raw `*_tokens` columns. When both are NULL or negative, the value is 0. `total = input + output` is back-filled when `total_tokens` is missing but the others sum.

`reasoning_tokens = total - input - output`, clamped to zero. Rows where every token category is zero are filtered out.

### Timestamp parsing

`created_at` is accepted in three formats so older releases keep working:

- RFC3339 / RFC3339Nano
- SQLite datetime (`2025-05-18 10:30:00`, with optional fractional seconds)
- date-only (`2025-05-18`, interpreted as `00:00:00 UTC`)

Rows with an unparseable timestamp are skipped silently.

### Surfaced metrics

| Metric | Window | Notes |
|---|---|---|
| `total_sessions` | all-time | distinct sessions |
| `sessions_today` / `sessions_7d` | today / 7d | UTC-day attribution |
| `total_tokens` | all-time | sum of session `total_tokens` |
| `total_input_tokens` / `total_output_tokens` | all-time | per-session sum |
| `total_reasoning_tokens` | all-time | derived from `total - input - output` |
| `total_cost_usd` | all-time | emitted only when at least one session has `accumulated_cost > 0` |

Per-model `ModelUsage` rows carry input / output / total / reasoning tokens, request count (sessions), optional cost, and an `upstream_provider` dimension from `provider_name`.

### What's NOT tracked

- **Per-message detail.** Token columns are session aggregates; the provider does not walk per-message tables.
- **Cost when the upstream schema doesn't record it.** Without `accumulated_cost` populated by the running Goose binary, no cost metric is emitted; OpenUsage does not back-compute it from token counts.

### How fresh is the data?

- Polling: every 30 s by default.
- The provider's `HasChanged` hook stats `sessions.db` and skips Fetch when the file hasn't been touched since the last poll.

## Files read

- `sessions.db` — Goose's per-session SQLite store, opened read-only

## Caveats

:::tip
Set `GOOSE_PATH_ROOT` to point at a non-standard Goose install and OpenUsage will probe `<root>/data/sessions/sessions.db` first.
:::

- Multiple data-dir conventions exist in the wild. The provider probes both the `etcetera`-style "Block" subdirectory and the unqualified path so installs from any era surface.
- A transient SQLite locking error during Fetch is surfaced via the `query_error` diagnostic and the tile reports `error` status; the next poll will retry.
- Cost values are pure-passthrough of what Goose itself recorded in `accumulated_cost`. If Goose has not been given prices for a particular model, that session contributes tokens but no dollars.

## Troubleshooting

- **Tile shows "Goose sessions.db not found"** — none of the candidate paths exist. Confirm the install location, or set `db_path` explicitly. The candidate list above is what the provider walks.
- **Tile says "No Goose sessions recorded"** — the DB exists but every row was filtered out. Most common cause: `model_config_json` is missing on the rows you ran. Run a fresh session and re-check.
- **`query_error` diagnostic present** — the SQLite open or scan failed. The diagnostic text is verbatim from the driver; the most common cause is a stale lock from a crashed Goose process. Restart Goose and re-poll.

## Related

- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
- [Crush](./crush.md) — sibling per-project SQLite agent
- [Hermes](./hermes.md) — sibling SQLite-backed agent
