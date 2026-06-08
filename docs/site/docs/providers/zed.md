---
title: Zed
description: Track Zed Agent threads on the hosted zed.dev provider, with token totals and per-model breakdowns in OpenUsage.
sidebar_label: Zed
keywords: [zed usage tracker, zed quota tracking, zed cost tracking, zed token usage, track zed spend locally]
---

# Zed

Local-data provider that reads Zed's Agent threads from the `threads.db` SQLite store. Only threads owned by the hosted `zed.dev` model provider contribute to the surfaced metrics; threads targeting local or self-hosted backends are skipped because they have no billing implication. No network calls, no auth.

## At a glance

- **Provider ID** — `zed`
- **Detection** — `zed` binary on PATH or `threads.db` present at the OS-appropriate Zed data directory
- **Auth** — none (local files only)
- **Type** — coding agent
- **Tracks**:
  - All-time threads, plus threads today and threads in the last 7 days
  - Input / output / cache-read / cache-write / reasoning tokens
  - Total messages
  - Per-model breakdown with token totals, request count, and a fixed `upstream_provider=zed.dev` dimension
  - Daily series for threads and tokens

## Setup

### Auto-detection

OpenUsage registers the provider as soon as either signal is present:

- The `zed` binary is on PATH.
- `threads.db` exists at the OS-appropriate location.

Default `threads.db` locations:

- macOS — `~/Library/Application Support/Zed/threads/threads.db`
- Linux — `$XDG_DATA_HOME/zed/threads/threads.db` (fallback `~/.local/share/zed/threads/threads.db`)
- Windows — `%LOCALAPPDATA%\Zed\threads\threads.db` (fallback `~/AppData/Local/Zed/threads/threads.db`)

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "zed",
      "provider": "zed",
      "extra": {
        "db_path": "/absolute/path/to/threads.db"
      }
    }
  ]
}
```

The only path hint the provider honours is `db_path`. Point it at the `threads.db` file directly when running a non-standard Zed install.

## Data sources & how each metric is computed

The provider opens `threads.db` in read-only / immutable SQLite mode (`file:<path>?mode=ro&immutable=1`) so the live Zed process is never blocked on the file lock.

### Schema probe

The `threads` table has evolved across Zed releases. The provider runs `PRAGMA table_info(threads)` and adapts the SELECT list to whichever columns are present:

- Required: `data` (blob payload) and `data_type` (`json` or `zstd`). When either is absent, the provider returns zero threads without erroring.
- Optional: `updated_at`, `created_at`, `folder_paths`, `folder_paths_order`. Missing columns are filled in as `NULL`.

### Payload decoding

The `data` column is either raw JSON or zstd-compressed JSON, selected by `data_type`:

- `json` or empty — read as-is.
- `zstd` — inflated, with the output capped at 32 MB. Anything larger is treated as corrupt and skipped.
- Anything else — skipped to stay conservative.

### Provider filter

The inflated JSON is unmarshalled into a permissive shape that exposes the model and the token usage. Rows are kept only when `model.provider` equals `zed.dev` (case-insensitive). Local-runtime and external-ACP rows are silently dropped: openusage's Zed tile is exclusively a hosted-Zed view. Track local models on a separate provider tile (e.g. Ollama) when applicable.

### Tokens

Per-thread token resolution, in order:

- Sum every `request_token_usage[].token_usage` entry.
- Otherwise use the top-level `cumulative_token_usage` block.

Recognised buckets:

- `input_tokens` → `total_input_tokens`
- `output_tokens` → `total_output_tokens`
- `cache_read_input_tokens` → `total_cache_read`
- `cache_creation_input_tokens` → `total_cache_write`
- `reasoning_tokens` → `total_reasoning_tokens`

`total_tokens` = input + output + reasoning. Cache buckets are tracked separately. Threads where all five buckets are zero are dropped so they don't inflate counts.

### Timestamps and workspace

Per-thread timestamp resolution: payload `created_at` → payload `updated_at` → row `created_at` → row `updated_at`. Both RFC3339 and Unix-seconds-as-TEXT are accepted.

The workspace for a thread is picked from `folder_paths` (newline-delimited) indexed by the first entry of `folder_paths_order` (comma-separated indices). When `folder_paths_order` is missing, the first folder wins.

### Metrics

- `total_threads` — distinct threads that passed the `zed.dev` provider filter and had non-zero token data.
- `threads_today` / `threads_7d` — threads whose resolved timestamp is in today (UTC) or the trailing 7 days.
- Token metrics as listed above.
- `total_messages` — sum of `message_count` from the payload, falling back to the length of the `messages` array.
- `DailySeries["threads"]` and `DailySeries["tokens"]` — per-UTC-day series.

The per-model `ModelUsageRecord` always carries `upstream_provider=zed.dev` as a dimension; the model name comes from `model.name`, falling back to `model.id`.

### How fresh is the data?

- Polling: every 30 s by default. The provider stat()s `threads.db` and short-circuits when nothing has changed since the last poll.

## Caveats

- Only hosted `zed.dev` threads are surfaced. Threads targeting Ollama, vLLM, OpenRouter direct, or external ACP agents are intentionally dropped — they appear on their own provider tiles or not at all.
- Cost is not tracked. Zed does not record per-thread USD in `threads.db`; the tile exposes tokens and thread counts only.
- The 32 MB cap on inflated zstd payloads is a safety bound; in practice individual threads are orders of magnitude smaller.
- Threads with zero tokens across all buckets are dropped to keep the count meaningful; a brand-new empty thread will not appear until it has at least one request.

## Troubleshooting

- **Tile shows "Zed threads.db not found"** — confirm `threads.db` exists at the OS-appropriate path above, or set `db_path` manually.
- **Tile shows zero threads even though you have used the Agent** — your threads may be targeting a non-`zed.dev` provider. Switch the Agent panel to a hosted model and verify the tile populates.
- **`query_error` diagnostic** — the SQLite open or read failed. Check the diagnostic text; the most common cause is a stale path after a Zed major upgrade that moved the data dir.
