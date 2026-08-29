---
title: Ways to use OpenUsage
description: The surfaces OpenUsage exposes — live terminal dashboard, headless CLI reports, the Claude Code statusline, tmux and SketchyBar status bars, an always-on background daemon, multi-machine aggregation, and machine-readable export.
sidebar_position: 2
sidebar_label: Ways to use it
---

OpenUsage is more than the dashboard. The same usage data is available through
several surfaces — pick whichever fits how you work. They all read the same
providers and (optionally) the same local history.

| Surface | Command | Use it when |
| --- | --- | --- |
| [Live dashboard](#live-terminal-dashboard) | `openusage` | You want the full interactive view |
| [CLI reports](#headless-cli-reports) | `openusage daily` (`--json`) | Scripting, CI, a quick check |
| [Claude Code statusline](#claude-code-statusline) | `openusage statusline --install` | You live in Claude Code |
| [tmux status bar](#tmux-status-bar) | `openusage tmux install` | You live in tmux |
| [SketchyBar status bar](#sketchybar-status-bar) | `openusage sketchybar install --write` | You want provider usage in SketchyBar on macOS |
| [Background daemon](#always-on-background-daemon) | `openusage telemetry daemon install` | You want history over time |
| [Multiple machines](#across-multiple-machines) | `openusage hub` / `hub-view` | You work across several machines |
| [Export](#export--scripting) | `openusage export --json` | You want to pipe data into your own tools |

## Live terminal dashboard

The default. Auto-detects your tools and shows live tiles with a master/detail
panel and an Analytics screen (Tab).

```bash
openusage
```

See [first run](./first-run.md).

## Headless CLI reports

The same parsing and pricing as the dashboard, printed once and exited — handy
for scripts, CI, cron, and quick checks. Add `--json` for machine-readable
output.

```bash
openusage daily          # also: weekly, monthly
openusage session        # grouped by session
openusage blocks         # by 5-hour billing block, with burn rate + projection
openusage daily --json
```

See the [headless reports & statusline guide](../guides/cli-reports.md).

## Claude Code statusline

Put session/today/block cost, burn rate, and context usage right in Claude
Code's own status line.

```bash
openusage statusline --install
```

See the [reports & statusline guide](../guides/cli-reports.md).

## tmux status bar

A one-line provider-usage segment in your tmux status bar, updated on tmux's
interval. The interactive installer also offers real provider icons.

```bash
openusage tmux install
```

See the [tmux integration guide](../guides/tmux-integration.md).

## SketchyBar status bar

Show the active provider and quota in SketchyBar, with a detail popup and a
provider/account switcher. This is a macOS integration and requires `jq` plus
the OpenUsage telemetry daemon for event-backed selection.

```bash
openusage sketchybar install --write
sketchybar --reload
```

See the [SketchyBar integration guide](../guides/sketchybar-integration.md).

## Always-on background daemon

Run a background collector that ingests snapshots into a local SQLite store, so
reports and analytics span time even when the app isn't open. Local-first — the
daemon listens only on a Unix socket.

```bash
openusage telemetry daemon install
```

See [Daemon & Telemetry](../daemon/overview.md).

## Across multiple machines

Aggregate usage from several machines into one view: run a hub that collects
from each, and view the combined picture.

```bash
openusage hub          # aggregate snapshots from multiple machines
openusage hub-view     # view a remote hub's aggregated data in the TUI
```

See the [multi-machine guide](../guides/multi-machine.md).

## Export & scripting

Emit current usage snapshots to a file or stdout for your own tooling.

```bash
openusage export --json
```

See the [CLI reference](../reference/cli.md).
