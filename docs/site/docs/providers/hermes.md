---
title: Hermes
description: Track Hermes Agent sessions, per-model token usage, and cost in OpenUsage.
sidebar_label: Hermes
keywords: [hermes usage tracker, hermes quota tracking, hermes cost tracking, hermes token usage, track hermes spend locally]
---

# Hermes

Local-data provider for the Hermes Agent (Nous Research). Reads sessions out of `state.db`, the SQLite store Hermes maintains on disk. No network calls are made and no authentication is required; SQLite is opened read-only with the `immutable` URI flag so the live agent is never blocked.

## At a glance

- **Provider ID** — `hermes`
- **Detection** — `hermes` binary on `PATH` or `state.db` exists at `~/.hermes/state.db` (or `$HERMES_HOME/state.db`)
- **Auth** — none (local SQLite reads only)
- **Type** — coding agent
- **Tracks**:
  - Sessions (total, today, 7d)
  - Messages all-time
  - Per-model input / output / cache-read / cache-write / reasoning tokens
  - Cost (`actual_cost_usd` with `estimated_cost_usd` fallback)
  - Daily series for sessions, tokens, and cost

## Setup

### Auto-detection

Either signal is sufficient: the `hermes` binary on `PATH`, or a `state.db` at one of the candidate paths. The provider's `Fetch` handles a missing DB gracefully, so a freshly installed binary registers a tile that reports "No Hermes sessions recorded" until the first session.

`state.db` location is resolved in this order. The first existing file wins:

1. `$HERMES_HOME/state.db` when `HERMES_HOME` is set
2. `~/.hermes/state.db` (the documented default)

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "hermes",
      "provider": "hermes",
      "extra": {
        "db_path": "/Users/me/work/hermes-profile/state.db"
      }
    }
  ]
}
```

`db_path` is the only path-hint key. The override is used only when the file exists; otherwise the provider falls back to the default candidates. To point OpenUsage at a custom profile directory, either set `$HERMES_HOME` or pin `db_path` directly.

## Data sources & how each metric is computed

### Sessions table

The provider opens `state.db` read-only and runs a single `SELECT` against the `sessions` table. The schema has evolved across releases, so the provider probes `PRAGMA table_info(sessions)` first and falls back per column.

Columns the provider looks for:

| Column | Used as |
|---|---|
| `id` | session id |
| `model` | model name (required) |
| `billing_provider` | upstream provider hint |
| `started_at` | session day attribution (required) |
| `message_count` | messages per session |
| `input_tokens` / `output_tokens` | base token totals |
| `cache_read_tokens` / `cache_write_tokens` | cache token totals |
| `reasoning_tokens` | reasoning / thinking tokens |
| `actual_cost_usd` / `estimated_cost_usd` | per-session cost |

When `model` or `started_at` is not present in the schema at all, the provider returns no sessions (graceful empty) rather than erroring.

### Timestamp encoding

`started_at` is stored as a SQLite REAL. Hermes writes seconds (often fractional), but the provider treats any value greater than `1e12` as already in milliseconds to absorb future schema tweaks or external imports. Values `<= 0` are filtered out.

### Cost preference

`actual_cost_usd` wins when present and positive; otherwise `estimated_cost_usd`. Rows with no tokens AND no positive cost are filtered out.

### Aggregation

Day attribution uses `started_at` in UTC. Sessions are bucketed today / 7d on UTC-day boundaries.

### Surfaced metrics

| Metric | Window | Notes |
|---|---|---|
| `total_sessions` | all-time | distinct sessions |
| `sessions_today` / `sessions_7d` | today / 7d | UTC-day attribution |
| `total_tokens` | all-time | input + output + reasoning |
| `total_input_tokens` / `total_output_tokens` | all-time | per-session sum |
| `total_cache_read` / `total_cache_write` | all-time | per-session sum |
| `total_reasoning_tokens` | all-time | per-session sum |
| `total_messages` | all-time | sum of `message_count` |
| `total_cost_usd` | all-time | emitted only when at least one session has positive cost |

Per-model `ModelUsage` rows carry input / output / cached (cache-read) / reasoning / total tokens, request count (sessions), optional cost, and an `upstream_provider` dimension from `billing_provider`.

### What's NOT tracked

- **Per-message detail.** Token columns are session aggregates; the provider does not walk a per-message table.
- **Multi-profile aggregation.** One Hermes account in OpenUsage points at one `state.db`. To track multiple Hermes profiles, configure one account per profile with distinct `db_path` overrides.

### How fresh is the data?

- Polling: every 30 s by default.
- The provider's `HasChanged` hook stats `state.db` and skips Fetch when the file hasn't been touched since the last poll.

## Files read

- `state.db` — Hermes's per-profile SQLite store, opened read-only with the `immutable` URI flag

## Caveats

- The `actual_cost_usd` column is the source of truth when populated by Hermes. `estimated_cost_usd` is only used as a fallback; both come straight from the upstream agent and OpenUsage does not back-compute cost from token counts.
- Rows with an unparseable or non-positive `started_at` are silently skipped. The `started_at` column is required for day-bucket attribution.
- The `immutable=1` open mode means the provider cannot see writes that haven't yet been flushed to the main database file. In practice this only matters during very high-frequency polling.

## Troubleshooting

- **Tile shows "Hermes state.db not found"** — neither `$HERMES_HOME/state.db` nor `~/.hermes/state.db` exists. Run Hermes at least once, or set `db_path` explicitly to a non-default location.
- **Tile says "No Hermes sessions recorded"** — the DB exists but every row was filtered out. Most common cause: every session has either an empty `model` column or no positive tokens/cost. Run a real session and re-check.
- **`query_error` diagnostic present** — the SQLite open or scan failed. The diagnostic text is verbatim; check whether the file is corrupt or being held exclusively by another process.

## Related

- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
- [Goose](./goose.md) — sibling SQLite-backed agent
- [Crush](./crush.md) — sibling per-project SQLite agent
