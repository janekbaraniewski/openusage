---
title: Claude Code statusline
description: Show live session cost, today's spend, the active 5-hour block, burn rate, and context-window usage right in the Claude Code status bar — installed and customized with one interactive command.
sidebar_label: Claude Code statusline
keywords: [claude code statusline, claude code status bar, claude code cost in status bar, claude code context window usage, claude code burn rate, openusage statusline]
---

# Claude Code statusline

OpenUsage can render a one-line status bar for [Claude Code](https://docs.claude.com/en/docs/claude-code), shown at the bottom of the editor. It summarizes the model, your session and today's cost, the active 5-hour billing block, the burn rate, and how full the context window is — all from your local conversation logs, with no network call by default.

![OpenUsage statusline in Claude Code](/img/claudecodestatus.png)

```
🤖 Opus 4.8 | 💰 $12.40 sess / $6.79 today / $3.40 block (2h41m left) | 🔥 $1.20/hr | 🕔 5h 15% | 🧠 96k (48%)
```

:::note
Costs are **API-equivalent estimates** derived from your local logs, not your subscription charge. The session figure is the whole conversation's lifetime; "today" is across all your Claude Code sessions since local midnight.
:::

## Install

The fastest path is the interactive installer. On a terminal, run:

```bash
openusage statusline install
```

This opens a one-screen, **live-preview configurator** (the same style as `openusage tmux install`). Toggle which segments you want, flip the options, and watch the preview update; press **enter** on **Apply** to write it into `~/.claude/settings.json` (your previous settings are backed up to `settings.json.bak`).

![Installing the Claude Code statusline with the live-preview configurator](/img/statusline-install.gif)

Then **restart Claude Code** (or open a new session) so it re-reads `settings.json`.

## What each segment shows

| Segment | Example | Notes |
|---|---|---|
| Model name | `🤖 Opus 4.8` | From the session Claude Code pipes in. |
| Session cost | `$12.40 sess` | The whole conversation's API-equivalent cost. |
| Today's cost | `$6.79 today` | All Claude Code spend since local midnight. |
| 5h block | `$3.40 block (2h41m left)` | The active 5-hour billing block and time remaining. |
| Burn rate | `🔥 $1.20/hr` | Spend rate for the active block. |
| 5h usage window | `🕔 5h 15%` | Rate-limit utilization of the rolling 5-hour window. This isn't in the local logs — it's read from the running [OpenUsage daemon](../daemon/overview.md) over the local socket (no network), and is shown only when the daemon is running and has it. |
| Context % | `🧠 96k (48%)` | Tokens in the context window vs. the model's limit; turns yellow then red as it fills. |

:::tip
The **5h usage window** segment needs the [telemetry daemon](../daemon/overview.md) (`openusage telemetry daemon install`), which is what polls Claude Code's rate-limit quota. The value is cached locally for ~30s (a single small file shared across sessions), so renders stay instant and the daemon isn't hit on every keystroke. The other segments work purely from local logs with no daemon.
:::

:::note
Claude Code re-runs the statusline on every message, so each render reads only the conversation logs modified since local midnight or in the last 10 hours, whichever reaches further back, plus every log of the current session. Older history can't change today's cost, the active block, or the context window, so skipping it keeps each render well under a second, even with gigabytes of history. The session figure still covers the whole conversation.

On top of that, the last rendered line is cached per session in `~/.cache/openusage/statusline-render-*`. If the session input, the transcript file (its modification time and size) and the statusline flags are unchanged and the cached line is less than 10 seconds old, it is printed as is, without reading logs or contacting the daemon. A new message changes the transcript, so it always triggers a fresh render. Cache files for sessions idle for a day are removed automatically.
:::

## Customize

Everything the configurator sets can also be passed as flags — useful for scripting or for editing the `command` in `settings.json` directly.

```bash
# Only show today's cost and the context gauge:
openusage statusline install --segments today,context

# Drop color (e.g. for a terminal that mishandles ANSI), keep all segments:
openusage statusline install --color=false

# Fetch live pricing instead of the embedded table (slightly slower):
openusage statusline install --offline=false
```

Passing any of `--segments`, `--color`, `--offline`, or `--mode` makes the install non-interactive and bakes those choices into the installed command.

Other flags:

- `--context-medium <pct>` / `--context-high <pct>` — thresholds where the context gauge turns yellow / red (defaults: 50 / 80).
- `--mode calculate|display|auto` — `calculate` (default) recomputes cost from tokens and local pricing; `display` uses the cost Claude Code reports; `auto` prefers logged cost and falls back to calculation.

## Manual configuration

If you'd rather edit `~/.claude/settings.json` by hand, add a `statusLine` block. The installed command carries your options as flags:

```json
{
  "statusLine": {
    "type": "command",
    "command": "openusage statusline --segments today,context",
    "padding": 0
  }
}
```

A bare `openusage statusline` (no flags) shows every segment in full color.

## Uninstall

```bash
openusage statusline uninstall
```

This removes only the OpenUsage `statusLine` block (it leaves a third-party statusline alone) and backs up the file first.

## See also

- [tmux integration](./tmux-integration.md) — the same usage data in your tmux status bar.
- [Headless reports](./cli-reports.md) — `daily` / `weekly` / `monthly` / `session` / `blocks` for scripts and CI.
