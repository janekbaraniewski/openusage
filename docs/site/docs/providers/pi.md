---
title: Pi
description: Track local Pi / Oh My Pi agent sessions, per-model tokens, and daily activity in OpenUsage.
sidebar_label: Pi
keywords: [pi usage tracker, pi quota tracking, pi cost tracking, pi token usage, track pi spend locally]
---

# Pi

Local-file provider for the [Pi](https://github.com/badlogic/pi-mono) coding agent and its Oh My Pi fork. Walks both install layouts, parses per-session JSONL transcripts, and aggregates per-model token totals. No network calls and no authentication.

## At a glance

- **Provider ID** — `pi`
- **Detection** — `~/.pi/agent/sessions/` exists, `~/.omp/agent/sessions/` exists, or a `pi` binary on `PATH`
- **Auth** — local file
- **Type** — coding agent
- **Tracks**:
  - Total sessions, sessions today, sessions in the last 7 days
  - Total input, output, cache-read, and cache-write tokens
  - Per-model token totals with upstream provider hint and workspace label
  - Daily series for sessions and tokens

## Setup

### Auto-detection

OpenUsage registers the provider when any of the following are true:

- `~/.pi/agent/sessions/` exists (Pi)
- `~/.omp/agent/sessions/` exists (Oh My Pi)
- A `pi` binary is found on `PATH`

Both directories are walked when present and entries are de-duped by canonical (symlink-resolved) path, so a layout where one is a symlink to the other will not double-count sessions.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "pi",
      "provider": "pi",
      "extra": {
        "sessions_dir": "~/.pi/agent/sessions"
      }
    }
  ]
}
```

Setting `sessions_dir` replaces the default search with a single directory. Use this only when Pi writes sessions to a non-default location. There is no separate override for the Oh My Pi directory; point `sessions_dir` at whichever single tree you want walked.

## Data sources & how each metric is computed

The provider walks every `.jsonl` file under the resolved sessions directories and parses each as a Pi session transcript.

### Session file format

Each transcript is a JSON-lines file. The first line must be a session header:

```json
{"type": "session", "id": "...", "timestamp": "...", "cwd": "..."}
```

Files whose first line is not a `session` header are silently skipped. Subsequent lines are `message` records; only those with `role: "assistant"` and a `usage` block are counted. Per-line decode errors are dropped individually so partial corruption never poisons a whole session.

### Field mapping

Upstream fields on each assistant message → openusage:

| Upstream                        | openusage metric              |
| ------------------------------- | ----------------------------- |
| `message.usage.input`           | `total_input_tokens`          |
| `message.usage.output`          | `total_output_tokens`         |
| `message.usage.cacheRead`       | `total_cache_read`            |
| `message.usage.cacheWrite`      | `total_cache_write`           |
| `message.model`                 | `RawModelID` on `ModelUsage`  |
| `message.provider`              | `upstream_provider` dimension |

Workspace label is derived from the session header's `cwd` (final path segment) and attached as the `workspace_label` dimension on the per-model record.

### Session counts

- `total_sessions` — distinct session IDs across all parsed transcripts
- `sessions_today` — sessions with at least one assistant turn whose `timestamp` lands on the current UTC day
- `sessions_7d` — sessions with at least one assistant turn in the last 7 days

Timestamps are parsed as RFC 3339. When a turn carries no timestamp the file's mtime is used as fallback.

### Daily series

`DailySeries["sessions"]` and `DailySeries["tokens"]` are populated by day. The dashboard tile and the analytics page draw from these.

### What's NOT tracked

- **Cost in USD.** Pi sessions don't carry pricing and the provider does not run a pricing lookup, so there is no `total_cost_usd` metric.
- **Per-tool, per-language, or per-file detail.** Only assistant-turn token counts are extracted.

## Caveats

- Sessions with zero tokens across all four buckets are dropped to keep noise off per-model rows.
- When both `~/.pi/agent/sessions/` and `~/.omp/agent/sessions/` exist they are walked separately, then de-duped by canonical path. A bind-mounted or symlinked overlap is safe.
- The "workspace label" is the basename of the `cwd` recorded at session start. Repos checked out under different paths will produce different labels.

## Troubleshooting

- **Tile is empty** — run Pi (or Oh My Pi) at least once so that a JSONL session file lands under `~/.pi/agent/sessions/` or `~/.omp/agent/sessions/`. Confirm with `openusage detect`.
- **Sessions present on disk but not on the tile** — open the first line of one of the transcripts and confirm it is a `{"type":"session",...}` header. Pi versions that omit the header are skipped.
- **Wrong directory walked** — set `extra.sessions_dir` in your account config to the exact directory containing session subfolders.

## Related

- [Codex CLI](./codex.md) — sibling local-file coding-agent provider
- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
