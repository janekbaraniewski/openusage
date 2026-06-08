---
title: Roo Code
description: Track Roo Code VS Code extension tasks, tokens, and cost in OpenUsage.
sidebar_label: Roo Code
keywords: [roo code usage tracker, roo code quota tracking, roo code cost tracking, roo code token usage, track roo code spend locally]
---

# Roo Code

Local-data provider for the Roo Code VS Code extension. Reads per-task event logs the extension writes under VS Code's globalStorage and aggregates tasks, tokens, and cost. No network calls, no auth.

Roo Code shares its on-disk schema with [Kilo Code](./kilocode.md); the same parser handles both. This page covers only the Roo Code extension (`rooveterinaryinc.roo-cline`).

## At a glance

- **Provider ID** — `roocode`
- **Detection** — `rooveterinaryinc.roo-cline` globalStorage subdirectory present under any known VS Code variant
- **Auth** — none (local files only)
- **Type** — coding agent
- **Tracks**:
  - All-time tasks, plus tasks today and tasks in the last 7 days
  - Total API requests
  - Input / output / cache-read / cache-write tokens
  - All-time and today cost in USD (when the extension recorded it)
  - Per-model breakdown with token totals and request counts
  - Daily series for tasks, tokens, and cost

## Setup

### Auto-detection

OpenUsage walks every VS Code-family install root and registers the provider as soon as `<root>/User/globalStorage/rooveterinaryinc.roo-cline/` exists. The "extension dir exists but no tasks yet" case still counts; the tile renders a quiet "No Roo Code usage recorded yet" message until the first task is parsed.

The probed variants are: VS Code, VS Code Insiders, VSCodium, VSCodium Insiders, Cursor, and Windsurf. On Linux, OpenUsage also probes Windows-side AppData under `/mnt/c/Users/<user>/AppData/Roaming/` when running inside WSL. `XDG_CONFIG_HOME` is honoured for the Linux config root.

VS Code Server (Remote / Codespaces / dev containers) writes globalStorage under `~/.vscode-server/data/User/globalStorage/`, which is not currently in the auto-probe list. Use the manual override below to point at it directly.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "roocode",
      "provider": "roocode",
      "extra": {
        "tasks_dir": "/absolute/path/to/User/globalStorage/rooveterinaryinc.roo-cline/tasks"
      }
    }
  ]
}
```

The only path hint the provider honours is `tasks_dir`. Point it at the `tasks/` directory under the extension's globalStorage (not at the extension dir itself). When set, the provider reads only that directory and skips cross-variant discovery.

## Data sources & how each metric is computed

Each Roo Code task is one subdirectory under `tasks/`. The provider reads two files per task:

- `ui_messages.json` — JSON array of UI events. Entries whose `say` field equals `api_req_started` carry a nested JSON blob in `text` with `cost`, `tokensIn`, `tokensOut`, `cacheReads`, `cacheWrites`, and `apiProtocol`. The discriminator is tolerated under `entry_type` or `type` for older schema versions.
- `api_conversation_history.json` — full conversation transcript. The provider extracts the last `<model>...</model>` tag from embedded environment metadata to attribute the task to a model. `<slug>` and `<name>` tags are used as fallbacks.

Tasks without a `ui_messages.json` are silently skipped (treated as "not ready"). Malformed event rows are skipped per-row rather than failing the whole task. UTF-8 BOMs at the start of either file are stripped before parsing. The result is a flat list of API calls deduplicated across VS Code variants (the same task may appear in multiple variants if the user copied state across them).

Timestamps may be Unix milliseconds, Unix seconds, or RFC3339 strings; the parser accepts all three. Negative token counts and cost values are clamped to zero so a corrupt row cannot pull the totals backwards.

### Tasks and requests

- `total_tasks` — distinct task IDs that produced at least one parsed call.
- `tasks_today` — distinct task IDs with at least one call timestamped today (UTC).
- `tasks_7d` — distinct task IDs with calls in the last 7 days (UTC).
- `total_requests` — count of every parsed `api_req_started` event after dedup.

### Tokens

- `total_input_tokens` / `total_output_tokens` — sum of `tokensIn` / `tokensOut` across calls.
- `total_cache_read_tokens` / `total_cache_write_tokens` — sum of `cacheReads` / `cacheWrites`.
- `total_tokens` — input + output + cache-read + cache-write.

### Cost

- `total_cost_usd` — sum of the `cost` field across calls.
- `today_cost_usd` — same sum, restricted to calls timestamped today (UTC).

Cost only appears when the extension recorded a non-zero `cost` value, which depends on the upstream provider Roo Code is calling.

### Per-model breakdown

- Each model becomes one `ModelUsageRecord` with input/output/cached/total tokens and request count. The first non-empty `apiProtocol` leading segment (split on `/` or `:`) is attached as the `upstream_provider` dimension, so `bedrock/anthropic` becomes `bedrock`.

### How fresh is the data?

- Polling: every 30 s by default. The provider stat()s every probed extension dir and short-circuits when no globalStorage entry has changed since the last poll.

## Caveats

- Cost numbers come from whatever Roo Code chose to record. If the upstream provider doesn't return per-call pricing, `total_cost_usd` will be zero even though tokens are accurate.
- Tasks copied across multiple VS Code variants are deduplicated by task ID, but the dedup is best-effort and assumes task IDs are stable across copies.
- VS Code Server installs are not auto-detected. Use `tasks_dir` to point at the remote globalStorage path explicitly.
- The Roo Code provider is independent from the Kilo Code provider even though they share parser code; install one, the other, or both and openusage will surface each as its own tile.

## Troubleshooting

- **Tile shows "extension data not found"** — confirm `<root>/User/globalStorage/rooveterinaryinc.roo-cline/` exists. Check the variant list above; if you use VS Code Server, set `tasks_dir` manually.
- **Tile reports 0 tasks despite usage** — open one of the task directories and check that `ui_messages.json` is present and non-empty. The provider silently skips tasks where the file is missing.
- **`roocode_task_parse_errors` diagnostic** — at least one task file failed to decode. The provider counts the failures but continues; inspect the offending `ui_messages.json` for corruption.

## Related

- [Kilo Code](./kilocode.md) — sibling VS Code extension that shares the same on-disk schema
