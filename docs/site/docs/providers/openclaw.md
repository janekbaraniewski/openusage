---
title: OpenClaw
description: Track local OpenClaw agent sessions, per-model tokens, and vendor-reported cost in OpenUsage.
sidebar_label: OpenClaw
keywords: [openclaw usage tracker, openclaw quota tracking, openclaw cost tracking, openclaw token usage, track openclaw spend locally]
---

# OpenClaw

Local-file provider for the [OpenClaw](https://openclaw.ai/) AI coding agent. Reads transcript JSONL files written under `~/.openclaw/agents/` and aggregates per-model token totals. Vendor-reported USD cost is surfaced when the transcript includes it. No network calls and no authentication.

## At a glance

- **Provider ID** — `openclaw`
- **Detection** — any of `~/.openclaw/`, `~/.clawdbot/`, `~/.moltbot/`, `~/.moldbot/` exists, or an `openclaw` binary on `PATH`
- **Auth** — local file
- **Type** — coding agent
- **Tracks**:
  - Total sessions, sessions today, sessions in the last 7 days
  - Total input, output, cache-read, and cache-write tokens
  - Per-model token totals with upstream provider hint
  - Vendor-reported cost in USD (when present in the transcript)
  - Daily series for sessions, tokens, and cost

## Setup

### Auto-detection

OpenUsage registers the provider when any of the install directories exist. The canonical path is `~/.openclaw/`; the legacy aliases `~/.clawdbot/`, `~/.moltbot/`, and `~/.moldbot/` are also walked when present. Auto-detection picks the first matching install location to set `data_dir`, then the provider walks every existing agents directory at fetch time and de-dupes transcripts by absolute path.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "openclaw",
      "provider": "openclaw",
      "extra": {
        "agents_dir": "~/.openclaw/agents"
      }
    }
  ]
}
```

Setting `agents_dir` replaces the default search with a single directory. When the override is set but the directory does not exist, no fallback to defaults happens — the tile will report `OpenClaw agents directory not found`.

## Data sources & how each metric is computed

The provider supports two on-disk layouts. For each resolved agents directory:

1. **Index layout.** If `sessions.json` exists in the directory root, it is parsed as an object whose values look like `{"sessionId": "...", "sessionFile": "path/to/transcript.jsonl"}`. Listed transcripts are loaded; missing files are dropped silently.
2. **Flat layout.** Otherwise the directory is walked recursively and every `.jsonl` file is treated as a transcript.

Transcripts are de-duped by absolute path across all directories so an index-layout install plus a legacy flat-layout directory will not double-count.

### Transcript format

Each transcript is JSON-lines. Two record types matter:

- `customType: "model"` (or `type: "model"`) — declares the current provider and model for subsequent messages
- `type: "message"` with `role: "assistant"` and a `usage` block — a billable turn

### Field mapping

| Upstream                                            | openusage metric              |
| --------------------------------------------------- | ----------------------------- |
| `message.usage.input`                               | `total_input_tokens`          |
| `message.usage.output`                              | `total_output_tokens`         |
| `message.usage.cacheRead`                           | `total_cache_read`            |
| `message.usage.cacheWrite`                          | `total_cache_write`           |
| `message.model` / `modelId` / model declaration     | `RawModelID` on `ModelUsage`  |
| `message.provider` / model declaration              | `upstream_provider` dimension |
| `message.usage.cost.total` or `message.cost.total`  | `total_cost_usd`              |

When a turn has no explicit model and no prior `model` declaration in the file, the per-model row falls back to the literal string `unknown`.

### Cost

`total_cost_usd` is emitted only when at least one transcript record carries a non-zero `cost.total` (either nested inside `usage` or as a sibling). The provider does **not** run a pricing lookup. Sessions without vendor-reported cost contribute tokens but no dollars; in that case `total_cost_usd` is absent.

### Session counts

- `total_sessions` — distinct session IDs across all parsed transcripts. Index entries supply the session ID; flat-layout transcripts derive it from the file basename.
- `sessions_today` — sessions with at least one assistant turn whose timestamp lands on the current UTC day
- `sessions_7d` — sessions with at least one assistant turn in the last 7 days

Timestamps are parsed as either Unix milliseconds (when the JSON value is a number) or RFC 3339 (when it is a string).

### Daily series

`DailySeries["sessions"]`, `DailySeries["tokens"]`, and (when any vendor cost is present) `DailySeries["cost"]` are populated by day.

## Caveats

- Cost values come straight from the upstream transcript when present. The provider does not estimate cost from token counts.
- The three legacy directory aliases exist for historical builds of the agent and are scanned for compatibility only. If you run a single modern install nothing changes.
- When both `sessions.json` and loose `.jsonl` files live in the same agents directory, the index is preferred for that directory and the loose files are ignored — unless they are also referenced by `sessionFile` entries in the index.

## Troubleshooting

- **Tile is empty** — run an OpenClaw session so a transcript lands under `~/.openclaw/agents/`. Confirm with `openusage detect`.
- **Cost column is blank** — the installed OpenClaw build is not writing `cost.total` into the transcript. Tokens still aggregate; the dollar column will stay empty until the upstream emits cost.
- **Wrong directory walked** — set `extra.agents_dir` in your account config. Verify the directory exists; an override pointing at a non-existent path disables the default search and leaves the tile blank.

## Related

- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
- [Codex CLI](./codex.md) — sibling local-file coding-agent provider
