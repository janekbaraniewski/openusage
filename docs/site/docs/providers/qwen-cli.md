---
title: Qwen CLI
description: Track local Qwen CLI chat transcripts, per-model tokens, and reasoning usage in OpenUsage.
sidebar_label: Qwen CLI
keywords: [qwen cli usage tracker, qwen cli quota tracking, qwen cli cost tracking, qwen cli token usage, track qwen cli spend locally]
---

# Qwen CLI

Local-file provider for the [Qwen CLI](https://github.com/QwenLM/qwen-cli). Reads per-project chat transcripts under `~/.qwen/projects/<project>/chats/*.jsonl` and aggregates per-model token totals. No network calls and no authentication.

## At a glance

- **Provider ID** — `qwen_cli`
- **Detection** — `~/.qwen/projects/` exists, or a `qwen` binary on `PATH`
- **Auth** — local file
- **Type** — coding agent
- **Tracks**:
  - Total sessions, sessions today, sessions in the last 7 days
  - Total input, output, cache-read, and reasoning tokens
  - Per-model token totals with upstream provider hint
  - Daily series for sessions and tokens

## Setup

### Auto-detection

OpenUsage registers the provider when `~/.qwen/projects/` exists or `qwen` is found on `PATH`. Run a Qwen CLI chat at least once to create the directory.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "qwen_cli",
      "provider": "qwen_cli",
      "extra": {
        "projects_dir": "~/.qwen/projects"
      }
    }
  ]
}
```

`projects_dir` replaces the default search with an explicit per-project root. The provider only counts `.jsonl` files that live under a `chats` segment of the path, so the override must point at the parent that contains `<project>/chats/*.jsonl`.

## Data sources & how each metric is computed

The provider walks `projects_dir` recursively, picking up files whose name ends in `.jsonl` and whose path includes a `chats` segment. Each transcript is parsed as JSON-lines; only records with `type: "assistant"` and a non-empty `usageMetadata` block are counted.

### Field mapping

Upstream `usageMetadata` fields → openusage metrics:

| Upstream                        | openusage metric           |
| ------------------------------- | -------------------------- |
| `promptTokenCount`              | `total_input_tokens`       |
| `candidatesTokenCount`          | `total_output_tokens`      |
| `thoughtsTokenCount`            | `total_reasoning_tokens`   |
| `cachedContentTokenCount`       | `total_cache_read`         |

`total_cache_write` is not exposed: Qwen CLI does not separate cache-creation tokens, so cache write is always 0. `total_tokens` is the sum of input, output, and reasoning tokens (cached are tracked separately).

### Model

`message.model` is used when present. When missing, the record falls back to the literal string `unknown` so the per-model row still appears on the detail view. The upstream provider hint is hard-coded to `qwen`.

### Session ID

When a record carries `sessionId`, that is used. Otherwise the provider derives a session ID from the file path: `<project>-<filename-stem>`. The project segment is the nearest ancestor directory that is not `chats`. Two transcripts under the same project that omit `sessionId` will keep separate identities as long as their file basenames differ.

### Session counts

- `total_sessions` — distinct session IDs across all parsed transcripts
- `sessions_today` — sessions with at least one assistant turn whose `timestamp` lands on the current UTC day
- `sessions_7d` — sessions with at least one assistant turn in the last 7 days

Timestamps are parsed as RFC 3339 (nano or second precision). When a turn carries no timestamp the file's mtime is used as fallback.

### Daily series

`DailySeries["sessions"]` and `DailySeries["tokens"]` are populated by day for the analytics chart.

### What's NOT tracked

- **Cost in USD.** Qwen CLI transcripts don't carry pricing and the provider does not run a pricing lookup, so there is no `total_cost_usd` metric.

## Caveats

- Only files whose path contains a `chats` segment are scanned. Stray `.jsonl` files placed elsewhere under `~/.qwen/projects/` are ignored.
- The default model string `unknown` is intentional. If you see it on the detail view it means Qwen CLI wrote assistant turns without a `model` field.

## Troubleshooting

- **Tile is empty** — run a Qwen CLI chat so a transcript lands under `~/.qwen/projects/<project>/chats/`. Confirm with `openusage detect`.
- **All usage attributed to `unknown` model** — open one of the JSONL transcripts and check whether assistant lines include a top-level `model` field. If not, the CLI version installed does not emit per-message model metadata.
- **Wrong directory walked** — set `extra.projects_dir` to the directory whose subtree contains `<project>/chats/*.jsonl`.

## Related

- [Codex CLI](./codex.md) — sibling local-file coding-agent provider
- [Gemini CLI](./gemini-cli.md) — sibling local-file coding-agent provider
