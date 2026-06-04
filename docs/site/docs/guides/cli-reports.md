---
title: Headless reports & statusline
description: Script usage and cost from the command line, and show a live status line in Claude Code.
---

# Headless reports & statusline

Not every workflow wants a full-screen dashboard. OpenUsage ships scriptable
report subcommands and a one-line Claude Code status bar that reuse the exact
same parsing and pricing as the TUI.

## Reports

```bash
openusage daily        # token usage and cost by day
openusage weekly       # by week (Monday start by default)
openusage monthly      # by month
openusage session      # by Claude Code session
openusage blocks       # by 5-hour billing block, with burn rate + projection
```

`daily`, `weekly` and `monthly` aggregate **every configured provider**. Local
tools are read from their on-disk logs at full fidelity; remote API platforms
are folded in from their daily spend (or current total).

`session` and `blocks` need per-turn (or per-session) timestamps, so they cover
every local provider that records them: Claude Code, Codex, Gemini CLI, Copilot,
Cursor, OpenCode, Ollama (via their telemetry logs) and Amp, Codebuff, OpenClaw,
Roo Code, Kilo Code, Crush, Goose, Hermes, Zed, Droid and Kiro (via their
session files/DBs). Remote API platforms have no per-turn data, so they appear
only in the periodic reports.

Where a tool records tokens but no cost, the cost is computed from token counts
via the pricing layer (online); pass `--offline` to skip that.

### Common flags

```bash
openusage daily --json                      # machine-readable output
openusage daily --breakdown                 # per-model rows under each day
openusage monthly --since 2026-01-01        # bound the date range
openusage daily --provider claude_code --offline   # local-only, no network
```

- `--json` emits a stable JSON document (`{ kind, rows, totals, note }`) you can
  pipe into `jq`.
- `--breakdown` / `-b` adds a per-model breakdown beneath each row.
- `--since` / `--until` take `YYYY-MM-DD` and are inclusive.
- `--provider` and `--project` filter the stream.
- `--mode` chooses how cost is derived:
  - `calculate` (default) recomputes from tokens at current rates,
  - `display` trusts the cost recorded in the logs,
  - `auto` uses the logged cost when present and recomputes otherwise.
- `--offline` skips network pricing lookups and uses embedded rates.

Costs are API-equivalent estimates derived from token counts, not subscription
charges.

### Example: today's spend in CI

```bash
openusage daily --json --since "$(date +%F)" \
  | jq '.totals.cost_usd'
```

### Long-context accuracy

Cost is computed per turn at the correct context tier. Requests whose prompt
exceeds a model's long-context breakpoint (for example 200k tokens for Claude)
are billed at the higher tier rate, so big sessions report their true cost.

## Statusline

`openusage statusline` renders a single line for the Claude Code status bar:

```
🤖 Opus 4.8 | 💰 $0.23 sess / $1.23 today / $0.45 block (2h45m left) | 🔥 $0.12/hr | 🧠 25k (12%)
```

It shows the active model, session/today/active-block cost, the burn rate, and
context-window usage. It runs offline by default so it responds instantly.

### Install

```bash
openusage statusline --install
```

This adds a `statusLine` block to `~/.claude/settings.json`, backing up the
original as `settings.json.bak` and preserving your other settings. Remove it
again with `openusage statusline --uninstall`.

To wire it by hand instead:

```json
{
  "statusLine": {
    "type": "command",
    "command": "openusage statusline",
    "padding": 0
  }
}
```

### Tuning

```bash
openusage statusline --offline=false           # fetch live pricing
openusage statusline --context-medium 60 --context-high 85
openusage statusline --color=false             # plain output
```

The context indicator turns yellow past `--context-medium` and red past
`--context-high`.

## Provider coverage

What each report can show depends on the data a provider keeps on disk. The
periodic reports need any cost or token signal; `session` and `blocks` need
per-turn (or per-session) timestamps; `statusline` needs the Claude Code
status-bar hook.

Legend: ✅ full · ▪ session-level (blocks bucket per session, not per turn) ·
✗ no data substrate.

| Provider | daily/weekly/monthly | session | blocks | statusline |
|---|---|---|---|---|
| Claude Code | ✅ | ✅ | ✅ | ✅ |
| Codex | ✅ | ✅ | ✅ | ✗ |
| Gemini CLI | ✅ | ✅ | ✅ | ✗ |
| Copilot | ✅ | ✅ | ✅ | ✗ |
| Cursor | ✅ | ✅ | ✅ | ✗ |
| OpenCode | ✅ | ✅ | ✅ | ✗ |
| Ollama | ✅ | ✅ | ✅ | ✗ |
| Amp | ✅ | ✅ | ✅ | ✗ |
| Codebuff | ✅ | ✅ | ✅ | ✗ |
| OpenClaw | ✅ | ✅ | ✅ | ✗ |
| Roo Code | ✅ | ✅ | ✅ | ✗ |
| Kilo Code | ✅ | ✅ | ✅ | ✗ |
| Crush | ✅ | ✅ | ▪ | ✗ |
| Goose | ✅ | ✅ | ▪ | ✗ |
| Hermes | ✅ | ✅ | ▪ | ✗ |
| Zed | ✅ | ✅ | ▪ | ✗ |
| Droid | ✅ | ✅ | ▪ | ✗ |
| Kiro | ✅ | ✅ | ▪ | ✗ |
| OpenRouter, Z.AI | ✅ | ✗ | ✗ | ✗ |
| Mistral, DeepSeek, Moonshot, Perplexity, Alibaba Cloud | ✅ (current total) | ✗ | ✗ | ✗ |
| OpenAI, Anthropic, Groq, xAI, Gemini API | ✗ (rate-limit only) | ✗ | ✗ | ✗ |

Tools that record tokens but no cost (for example Copilot, Gemini CLI, Zed,
Droid, Kiro) have their cost computed from token counts via the pricing layer
when online; pass `--offline` to skip that.

## See also

- [CLI reference](../reference/cli.md) — every flag for these commands
- [Claude Code provider](../providers/claude-code.md) — where the conversation
  logs live and how cost is estimated
- [Usage projections](./usage-projections.md) — burn rate and billing blocks in
  the dashboard
