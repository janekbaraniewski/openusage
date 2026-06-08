---
title: Mux
description: Track Mux per-workspace session usage, model spend, and tokens in OpenUsage.
sidebar_label: Mux
keywords: [mux usage tracker, mux quota tracking, mux cost tracking, mux token usage, track mux spend locally]
---

# Mux

Local-data provider that reads Mux's per-workspace `session-usage.json` files. One file per workspace, one model record per entry in that file. No network calls, no auth.

## At a glance

- **Provider ID** — `mux`
- **Detection** — `mux` binary on PATH or `~/.mux/sessions/` directory present
- **Auth** — none (local files only)
- **Type** — coding agent
- **Tracks**:
  - All-time sessions (one per workspace), plus sessions today and sessions in the last 7 days
  - Input / output / reasoning / cache-read / cache-write tokens
  - All-time cost in USD (when the file includes per-bucket pricing)
  - Per-model breakdown with token totals, request count, and `upstream_provider` dimension
  - Daily series for sessions, tokens, and cost

## Setup

### Auto-detection

OpenUsage registers the provider as soon as either signal is present:

- The `mux` binary is on PATH.
- The directory `~/.mux/sessions/` exists.

Either signal alone is enough so a freshly installed Mux surfaces a tile before its first workspace runs.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "mux",
      "provider": "mux",
      "extra": {
        "sessions_dir": "/absolute/path/to/.mux/sessions"
      }
    }
  ]
}
```

The only path hint the provider honours is `sessions_dir`. It should point at the directory containing the per-workspace subdirectories, not at an individual `session-usage.json`.

## Data sources & how each metric is computed

The provider walks `sessions_dir` recursively and reads every file named `session-usage.json`. The parent directory name is treated as the workspace ID (and used as the session ID for dedup).

### File shape

Each `session-usage.json` carries:

- `version` — schema version (currently unused beyond reading).
- `byModel` — map keyed by `<provider>:<model>`, each value carrying `input`, `cached`, `cacheCreate`, `output`, and `reasoning` buckets. Each bucket has optional `tokens` (int) and `cost_usd` (float).
- `lastRequest` — optional `{ model, timestamp }` where `timestamp` is Unix milliseconds.

The `<provider>:<model>` key is split on the first colon. Entries without a colon are stored with an empty provider and the whole key as the model.

### Timestamps

- Preferred: `lastRequest.timestamp` (Unix milliseconds).
- Fallback: the file's mtime.

The chosen timestamp drives the "today" / "7d" buckets, the daily series, and the `lastRequest`-derived session-by-day count.

### Metrics

- `total_sessions` — distinct workspace IDs that produced at least one non-zero model entry.
- `sessions_today` / `sessions_7d` — workspaces with their resolved timestamp in today (UTC) or the trailing 7 days.
- `total_input_tokens` / `total_output_tokens` — sums of the `input.tokens` / `output.tokens` buckets across every file.
- `total_cache_read` / `total_cache_write` — sums of `cached.tokens` / `cacheCreate.tokens`.
- `total_reasoning_tokens` — sum of `reasoning.tokens`.
- `total_tokens` — input + output + reasoning. Cache buckets are tracked separately and not added into the "total tokens" headline metric.
- `total_cost_usd` — sum of every bucket's `cost_usd` across every file, set only when at least one bucket recorded a cost.

The per-model `ModelUsageRecord` carries token totals (input/output/cached/reasoning, plus a `total_tokens` that does include cache buckets), the request count (one per entry per file), and the parsed provider as the `upstream_provider` dimension.

### Daily series

`DailySeries["sessions"]` is one point per UTC day, deduplicated by workspace ID per day (so multiple model rows from the same workspace on the same day count as one session). `DailySeries["tokens"]` and `DailySeries["cost"]` are summed across every entry on that day.

### How fresh is the data?

- Polling: every 30 s by default. The provider stat()s the sessions directory and short-circuits when nothing has changed since the last poll.

## Caveats

- One workspace equals one session. If a workspace runs many requests, all of them collapse into a single session count; the per-model record's `requests` figure is the better volume signal.
- The `total_tokens` headline excludes cache buckets to match the dashboard's "tokens billed" intuition; the per-model `TotalTokens` includes them.
- Cost is only as accurate as what Mux writes into the file. Missing `cost_usd` fields produce a zero, not an estimate.
- Files are scanned every poll. On very large workspace sets this can be I/O-heavy; the change detector mitigates it on filesystems where directory mtimes propagate from child writes.

## Troubleshooting

- **Tile shows "Mux sessions directory not found"** — confirm `~/.mux/sessions/` exists. If your install writes elsewhere, set `sessions_dir`.
- **Tile shows zero sessions despite running Mux** — check that at least one `session-usage.json` exists under the directory and that its `byModel` map has non-zero token buckets. The provider skips entries whose buckets are all zero.
- **`walk_error` diagnostic** — the directory walk hit a hard filesystem error. Check the diagnostic text; individual unreadable subdirectories are tolerated, only top-level errors propagate.
