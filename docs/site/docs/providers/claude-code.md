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
  - MCP server and skill counts
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

## What you'll see

- Dashboard tile shows the active billing block, burn rate, and today's message/session counts.
- Detail view breaks down per-model token usage (input, output, cache read, cache create) with cost estimates.
- A 5-hour billing block gauge tracks how much of the current window has been consumed.
- Sub-sections list MCP servers, skills, and recent sessions.

## Files read

- `~/.claude/stats-cache.json` — daily activity rollups
- `~/.claude/projects/**/*.jsonl` — session transcripts used to derive token and tool counts
- `~/.claude.json` — OAuth state and subscription metadata
- `~/.claude/settings.json` — MCP and skill configuration

## API endpoints used

- Optional: `POST admin.anthropic.com` Usage API when session cookies are present (browser-session auth, see [Daemon integrations](../daemon/integrations.md))

## Caveats

:::note
Costs are API-equivalent estimates derived from token counts and public pricing. They do not reflect Pro/Max subscription billing.
:::

- Cache read and cache create tokens are counted separately from input/output.
- The Usage API call is optional; without browser-session auth the tile still works using local files.
- Billing blocks are 5-hour rolling windows starting from your first message in the window.

## Troubleshooting

- **Tile is empty** — confirm `claude` is on `PATH` and `~/.claude/stats-cache.json` exists. Run a Claude Code session to populate it.
- **Cost looks wrong** — cost is an estimate; subscription users will see API-equivalent dollars, not actual charges.
- **No billing block** — billing blocks only appear after the first message; the window is local to your machine.

## Related

- [Codex CLI](./codex.md) — sibling local-file provider for OpenAI's Codex
- [Anthropic](./anthropic.md) — direct API rate limits for the same backend models
