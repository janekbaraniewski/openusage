---
title: Claude Code
description: Track Claude Code CLI sessions, billing blocks, burn rate, and per-model token usage in OpenUsage.
sidebar_label: Claude Code
---

# Claude Code

Local-first tracking for the Claude Code CLI. Reads on-disk session logs, billing blocks, and OAuth state to surface daily activity, per-model token costs, and 5-hour burn rate.

## At a glance

- **Provider ID** — `claude_code`
- **Detection** — `claude` binary on `PATH` plus `~/.claude` (or `~/.config/claude` on Linux)
- **Auth** — local OAuth in `~/.claude.json`; no API key required
- **Type** — coding agent
- **Tracks**:
  - Daily activity: messages, sessions, tool calls
  - Per-model tokens: input, output, cache read, cache create
  - Cost estimates (API-equivalent)
  - Sessions and billing blocks (5-hour windows)
  - Burn rate
  - Skill usage counts
  - Subscription status

## Setup

### Auto-detection

OpenUsage looks for the `claude` binary and the config directory. On macOS and Windows that's `~/.claude`; on Linux it falls back to `~/.config/claude`. If both are present the provider is registered automatically.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "claude_code",
      "provider": "claude_code",
      "binary": "/usr/local/bin/claude",
      "extra": {
        "claude_dir": "~/.claude",
        "stats_cache": "~/.claude/stats-cache.json"
      }
    }
  ]
}
```

The `binary` field is optional; OpenUsage resolves `claude` via `PATH` if omitted.

## Data sources & how each metric is computed

Claude Code is the most data-rich provider in OpenUsage. Everything except the optional Usage API call is derived locally — there is no Anthropic billing endpoint behind a Claude subscription.

Local data sources, all under `~/.claude/`:

| File | Purpose |
|---|---|
| `~/.claude/projects/**/*.jsonl` | Per-conversation transcripts. Authoritative source for tokens, tool calls, billing blocks. |
| `~/.claude/stats-cache.json` (or `stats.json`) | Daily activity rollups Claude Code computes itself: messages, sessions, tool calls. |
| `~/.claude.json` | OAuth state, subscription metadata, organization UUID. |
| `~/.claude/settings.json` | Active model and `alwaysThinkingEnabled` flag. |

Optional remote source: `GET https://claude.ai/api/organizations/{org_uuid}/usage` — only when [browser-session auth](../daemon/integrations.md) is configured. Provides organization-level rolled-up usage (the same numbers the Anthropic admin console shows).

### Pricing tables

Costs are computed locally by multiplying token counts by hard-coded per-million USD rates baked into the binary:

| Model family | Input | Output | Cache read | Cache create |
|---|---|---|---|---|
| Opus | $15.00 | $75.00 | $1.50 | $18.75 |
| Sonnet | $3.00 | $15.00 | $0.30 | $3.75 |
| Haiku | $0.80 | $4.00 | $0.08 | $1.00 |

Family is matched by substring on the model name (e.g. `claude-3-5-sonnet-…` → Sonnet). Unknown models fall back to Sonnet pricing.

`cost = input × inputRate + output × outputRate + cacheRead × cacheReadRate + cacheCreate × cacheCreateRate` (all per 1M tokens).

### Today's tokens & cost

- Source: every JSONL turn whose `timestamp` falls in the local-time current day.
- Transform: per-turn input/output/cacheRead/cacheCreate are summed; per-turn cost from the pricing table is summed. Surfaces:
  - `today_cost_usd` — sum of per-turn costs in $.
  - `today_input_tokens`, `today_output_tokens`, `today_cache_read_tokens`, `today_cache_create_tokens` — token totals.
  - `today_messages`, `today_sessions` (distinct session IDs).
  - Tool counts and per-tool usage from `content[].tool_use` entries.

### Weekly / all-time rollups

- Source: same JSONL records, filtered by trailing 7 days (weekly) or no filter (all-time).
- Transform: per-window sums of cost and tokens. Stored as `weekly_*` and `all_time_*` metrics. The all-time numbers are unbounded — they cover everything in `~/.claude/projects/`.

### 5h billing block (`5h_block_*`, `block_progress_pct`)

- Source: chronologically sorted JSONL turns. Each turn is dedup'd by `(messageID, requestID, sessionID, model)` to avoid double-counting.
- Transform: when a turn arrives whose timestamp is past the prior block's end, a **new block opens at `floor(turn.timestamp, 1h)`** and ends 5 hours later. The current block is the one that contains `now`.
  - `5h_block_input`, `5h_block_output`, `5h_block_msgs`, `5h_block_cache_read_tokens`, `5h_block_cache_create_tokens` — sums for turns inside the current block.
  - `Resets["billing_block"]` — the block end timestamp.
  - `Raw["block_progress_pct"]` — `(elapsed / 5h) × 100`, capped at 100.
  - `Raw["block_time_remaining"]` — `block_end - now` rounded to the minute.

### `burn_rate` — USD per hour

- Source: same current block as above.
- Transform: `block_cost_usd / elapsed_hours`. Only emitted once `elapsed > 1 minute` and `block_cost > 0` to avoid divide-by-noise.
- Window: `current 5h block`.

### Daily series for the chart

- Source: same JSONL records, grouped by `timestamp.format("2006-01-02")`.
- Transform: `dailyTokenTotals[day]` (sum of input + output), `dailyMessages[day]`, `dailyCost[day]`. Emitted as `DailySeries["tokens"]`, `DailySeries["messages"]`, `DailySeries["cost"]`.

### Per-model breakdown

- Source: each JSONL turn carries the model name. Aggregations are bucketed by sanitized family.
- Transform: detail rows with input/output/cacheRead/cacheCreate/reasoning tokens, ephemeral 5m/1h cache split, web-search/web-fetch counts, and computed cost.

### Tool / language / file usage

- Source: `content[].tool_use` and the tool's input map (e.g. `file_path`, `path`, `command`).
- Transform:
  - Tool counts by tool name (`Edit`, `Read`, `Bash`, etc.) → `Metrics["tool_*"]`.
  - File extensions inferred from path candidates → language histogram.
  - Mutating tools (Edit, Write, NotebookEdit, etc.) feed `composer_lines_added` / `composer_lines_removed` and `composer_files_changed`.
  - `Bash` commands containing `git commit` are dedup'd and counted as `scored_commits`.

### Sessions today, sessions all-time

- Source: distinct `sessionId` values from the JSONL turns, scoped per window.
- Transform: a `total_prompts` metric counts unique `(messageID, requestID)` keys.

### Skills, subscription, account email, active model

- Source:
  - Active model and `alwaysThinkingEnabled` from `~/.claude/settings.json`.
  - Skill usage counts from `~/.claude.json` → `skillUsage[name].usageCount`.
  - Subscription status from `~/.claude.json` → `hasAvailableSubscription`, `oauthAccount.billingType`, `subscriptionCreatedAt`.
  - Account email from `oauthAccount.emailAddress`.
- Transform: each is stored as a snapshot attribute.

### Optional Usage API (organization-wide)

- Source: `GET https://claude.ai/api/organizations/{org_uuid}/usage` with session cookies imported via Settings → 5 KEYS. Returns aggregate per-day usage for the entire organization.
- Transform: when available, the response is cached in memory and applied on top of the local data. Errors fall back to the cached response (if any) so transient failures don't blank the tile.

### Auth status

- Source: derived from data presence. If neither `stats-cache.json`, `~/.claude.json`, nor any JSONL produced data, status becomes `error` (`No Claude Code stats data accessible`). Otherwise `ok` with the message `Claude Code CLI · costs are API-equivalent estimates, not subscription charges`.

### What's NOT tracked

- **Subscription billing.** Claude Code's costs are local **API-equivalent estimates** — what your usage would have cost on the API at published pricing. Pro and Max plans bill flat-rate; the dollar number on the tile is **not** what your card is charged.
- **Real-time push from the CLI without the integration.** Install the `claude_code` integration (see [Daemon integrations](../daemon/integrations.md)) for per-turn events.

### How fresh is the data?

- Polling: every 30 s by default. JSONL files are re-parsed only when their mtime/size changes; otherwise served from cache.
- Hook (when integration is installed): real-time per turn.

## Files read

- `~/.claude/projects/**/*.jsonl` — per-turn transcripts (authoritative for tokens, cost, blocks)
- `~/.claude/stats-cache.json` (or `stats.json`, with legacy fallbacks) — daily activity rollups
- `~/.claude.json` — OAuth state, subscription metadata, organization UUID, skill usage
- `~/.claude/settings.json` — active model, `alwaysThinkingEnabled` flag

On Linux the provider also probes `~/.config/claude/projects/` as a fallback.

## API endpoints used

- Optional: `GET https://claude.ai/api/organizations/{org_uuid}/usage` — only when browser-session cookies are imported. See [Daemon integrations](../daemon/integrations.md).

## Caveats

:::note
Costs are API-equivalent estimates derived from token counts and public pricing tables baked into the binary. They do not reflect Pro/Max subscription billing.
:::

- Cache read and cache create tokens are counted separately from input/output.
- The Usage API call is optional; without browser-session auth the tile still works using local files.
- Billing blocks are 5-hour rolling windows starting from your first message in the window.

## Troubleshooting

- **Tile is empty** — confirm `claude` is on `PATH` and `~/.claude/projects/` contains `*.jsonl` files. Run a Claude Code session to populate it.
- **Cost looks wrong** — cost is an estimate; subscription users will see API-equivalent dollars, not actual charges.
- **No billing block** — billing blocks only appear after the first message; the window is local to your machine.

### Why is the dollar number bigger than what my Claude subscription charged?

The Cost tile is an **API-equivalent estimate**: the provider takes input/output/cache token counts from your local conversation logs and multiplies by Anthropic's published per-million rates. That's what the same usage would cost on the API. A Pro / Max subscription bills flat-rate, so the local estimate often exceeds your actual subscription charge — that's a feature, not a bug; it's the leverage you get from the subscription.

### Why does the 5-hour block reset at a weird time?

A block starts at `floor(timestamp_of_first_message, 1h)` and ends 5 hours later. The window is local to your machine and rolls forward only when a turn lands after the prior block's end. Quiet periods don't slide it; a single late-night turn opens a new block aligned to that hour.

## Related

- [Codex CLI](./codex.md) — sibling local-file provider for OpenAI's Codex
- [Anthropic](./anthropic.md) — direct API rate limits for the same backend models
