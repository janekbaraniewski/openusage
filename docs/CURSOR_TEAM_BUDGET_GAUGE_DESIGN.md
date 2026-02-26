# Design: Cursor Provider Detail View Overhaul

## Problem

The Cursor provider's detail view was text-heavy, lacked graphical representations, and left significant data unexposed. Specific issues:

1. **Redundant gauge bars** — "Credits" and "Credit Limit" overlapped for team accounts
2. **No billing cycle indicator** — no way to see where you are in the billing period
3. **Missing interface breakdown** — the "tool usage" section was reporting interface types (composer, cli, human, tab) instead of actual agent tool calls
4. **No actual tool usage** — Cursor's `bubbleId:*` entries in `state.vscdb` contain granular tool call data that was never surfaced
5. **No language breakdown** — file extension data from `ai_code_hashes` was unused
6. **Non-graphical code stats** — lines added/removed, commits, and AI contribution were plain text rows
7. **Inaccurate AI percentage** — `LIMIT 50` in the scored_commits query skewed the average toward 100%
8. **Unexposed data** — agentic session counts, force modes, file creation/removal stats, and billing token breakdowns were available but hidden

## Solution

A comprehensive overhaul of the Cursor provider's data extraction and TUI rendering, adding six new graphical sections and fixing data accuracy issues.

## Final State

### Gauge Section (top of tile)

Two gauges rendered in priority order:

```
Team Budget   ██▓░░░░░░░░░░  14.8%    (self=teal, others=peach)
Billing Cycle ████████░░░░░  56.9%
$531 / $3600 spent
you $427 · team $104 · $3069 remaining
```

- **Team Budget**: Stacked gauge via `RenderStackedUsageGauge` with `team_budget_self` + `team_budget_others` segments
- **Billing Cycle**: Standard gauge from `billing_cycle_progress` metric, computed as `elapsed / total_cycle_days * 100`
- Fallback: When team data is unavailable, falls through to `plan_auto_percent_used` / `plan_api_percent_used`

### Model Burn (credits)

Standard model composition section from `model_*` metrics. Shows horizontal bar chart with per-model cost and token breakdown. Models sorted by cost descending.

### Clients

Merged section combining interface-level breakdown into the client composition panel. Enabled via `ClientCompositionIncludeInterfaces = true` on the `DashboardWidget`.

```
Clients
████████████████████████████████░░░░
1 Composer ........................ 87% 67.4k req
2 CLI Agents ...................... 13% 10.1k req
3 Human ...........................  0% 251 req
4 Tab Completion ..................  0% 97 req
```

Data source: `interface_*` metrics from `readTrackingSourceBreakdown`, which reads the `subagentInfo.subagentTypeName` field from `composerData` JSON in the `cursorDiskKV` table of `state.vscdb`.

Label mapping in `prettifyClientName`:
- `composer` → "Composer"
- `cli` → "CLI Agents"
- `human` → "Human"
- `tab` → "Tab Completion"

### Tool Usage

New section showing actual agent tool calls extracted from Cursor's bubble data. Enabled via `ShowActualToolUsage = true`.

```
Tool Usage  30.4k calls · 95% ok
████████████████████████████████████
1 run_terminal_command ........... 30% 9.0k
2 read_file ...................... 20% 6.2k
3 run_terminal_cmd ...............  9% 2.8k
4 search_replace .................  8% 2.4k
5 edit_file ......................  5% 1.5k
6 write ..........................  4% 1.2k
+ 92 more tools (Ctrl+O)
```

Data source: `readToolUsage` function queries `bubbleId:*` entries in `cursorDiskKV` where `$.type = 2` (AI response bubbles), extracting `toolFormerData.name` and `toolFormerData.status`.

Tool name normalization (`normalizeToolName`):
- MCP tools: `mcp-*-user-*-tool` shortened to `tool (mcp)`
- Version suffixes: `_v2`, `_v3` stripped

Metrics emitted:
- `tool_calls_total`, `tool_completed`, `tool_errored`, `tool_cancelled`, `tool_success_rate` (aggregates)
- `tool_<normalized_name>` (per-tool counts)

Aggregate keys are excluded from the bar chart via `actualToolAggregateKeys` filter map and displayed only in the heading summary.

### Language (requests)

Language breakdown from file extension data in the tracking database. Enabled via `ShowLanguageComposition = true`.

```
Language (requests)
████████████████████████████████████
1 go ............................. 53% 30.4k req
2 terraform ...................... 21% 12.0k req
...
+ 20 more languages (Ctrl+O)
```

Data source: `readTrackingLanguageBreakdown` queries `SELECT fileExtension, COUNT(*) FROM ai_code_hashes GROUP BY fileExtension` from `ai-code-tracking.db`.

Metrics emitted: `lang_<extension>` with unit "requests".

### Code Statistics

Graphical code stats section replacing plain-text rows. Enabled via `ShowCodeStatsComposition = true` with a `CodeStatsConfig` mapping metric keys.

```
Code Statistics
██████████████████████    ██
■ +74.6k added              ■ -18.5k removed
Files Changed .......................... 844 files
Commits ██████████████ 239 commits · 98% AI
Prompts ................................ 898 total
```

Config:
```go
cfg.CodeStatsMetrics = core.CodeStatsConfig{
    LinesAdded:   "composer_lines_added",
    LinesRemoved: "composer_lines_removed",
    FilesChanged: "composer_files_changed",
    Commits:      "scored_commits",
    AIPercent:    "ai_code_percentage",
    Prompts:      "total_prompts",
}
```

Rendered by `buildProviderCodeStatsLines` in `tiles.go`:
- Lines added/removed shown as proportional colored bars with numeric labels
- Commits shown as progress bar with AI% annotation
- Files and prompts as dot-leader rows

### Compact Rows

```
Credits  plan $40.93/$20.00 · cap $531.11/$3600 · mine $427.43 · billing $41.12
Team     members 18 members · owners 4 owners
Usage    used 100% · auto 0% · api 100% · ctx 43%
Activity today 15.1k · all 77.8k · sess 84 sessions · reqs 645
Lines    comp 148 · comp sug 148
```

### Individual Metrics (remaining)

Metrics not consumed by compositions or compact rows render as standard dot-leader rows:
- AI Deleted / AI Tracked files
- Billing Cached / Input / Output Tokens
- Plan Bonus / Plan Included

## Data Sources

### API endpoints (existing)

| Endpoint | Metrics |
|----------|---------|
| `GetCurrentPeriodUsage` | plan_spend, spend_limit, individual_spend, team_budget, billing_cycle_progress |
| `GetUsageBasedPricingV3` | plan_percent_used, plan_auto/api_percent_used, billing tokens |
| Model aggregation | model_* cost and token metrics |

### Local databases (enhanced)

| Database | Table/Query | Metrics |
|----------|-------------|---------|
| `state.vscdb` | `cursorDiskKV` → `composerData` JSON | interface_*, composer_sessions, agentic_sessions, composer_files_created/removed, mode_* |
| `state.vscdb` | `cursorDiskKV` → `bubbleId:*` entries | tool_* (all tool usage metrics) |
| `ai-code-tracking.db` | `ai_code_hashes` | lang_* |
| `ai-code-tracking.db` | `scored_commits` | ai_code_percentage, scored_commits, composer_lines_added/removed |

## Key Bug Fixes

### AI Code Percentage Accuracy

The `readScoredCommits` query had `LIMIT 50` which caused the weighted average to skew toward 100% because recent commits are more likely to be AI-heavy. Removed the limit to compute across all scored commits.

### Stacked Gauge Blank Space

`RenderStackedUsageGauge` in `gauge.go` had rounding that could leave 1-char gaps between segments. Fixed by rounding up intermediate segments to full block characters.

## Widget Configuration

```go
cfg.ShowClientComposition = true
cfg.ClientCompositionHeading = "Clients"
cfg.ClientCompositionIncludeInterfaces = true
cfg.ShowToolComposition = false              // merged into Clients
cfg.ShowLanguageComposition = true
cfg.ShowCodeStatsComposition = true
cfg.ShowActualToolUsage = true
```

Hidden metric prefixes: `model_`, `source_`, `client_`, `mode_`, `interface_`, `subagent_`, `lang_`, `tool_`.

Hidden metric keys: `plan_total_spend_usd`, `plan_limit_usd`, `plan_included_amount`, `team_budget_self`, `team_budget_others`, `composer_cost`, `agentic_sessions`, `non_agentic_sessions`, `tool_calls_total`, `tool_completed`, `tool_errored`, `tool_cancelled`, `tool_success_rate`, `composer_files_created`, `composer_files_removed`.

## Core Type Additions

| Type/Field | File | Purpose |
|------------|------|---------|
| `DashboardWidget.ClientCompositionHeading` | `core/widget.go` | Override heading for client composition section |
| `DashboardWidget.ClientCompositionIncludeInterfaces` | `core/widget.go` | Fold `interface_` metrics into client composition |
| `DashboardWidget.ShowActualToolUsage` | `core/widget.go` | Enable tool usage section |
| `DashboardWidget.ShowLanguageComposition` | `core/widget.go` | Enable language breakdown section |
| `DashboardWidget.ShowCodeStatsComposition` | `core/widget.go` | Enable code statistics section |
| `CodeStatsConfig` | `core/widget.go` | Maps code stat metric keys to rendering slots |
| `DashboardSectionActualToolUsage` | `core/widget.go` | Standard section constant for ordering |
| `DashboardSectionLanguageBurn` | `core/widget.go` | Standard section constant for ordering |
| `DashboardSectionCodeStats` | `core/widget.go` | Standard section constant for ordering |

## Impact Summary

| File | Changes |
|------|---------|
| `internal/providers/cursor/cursor.go` | `readToolUsage`, `normalizeToolName`, enhanced `readComposerSessions`/`readScoredCommits`, `readTrackingLanguageBreakdown`, billing cycle progress |
| `internal/providers/cursor/widget.go` | Full widget config for all new sections, hide keys/prefixes |
| `internal/core/widget.go` | New fields, section constants, `CodeStatsConfig` type |
| `internal/tui/tiles.go` | `buildActualToolUsageLines`, `collectInterfaceAsClients`, `buildProviderCodeStatsLines`, `buildProviderClientCompositionLinesWithWidget`, updated `prettifyClientName` |
| `internal/tui/gauge.go` | `RenderStackedUsageGauge` fix for segment rounding |
| `internal/core/widget_test.go` | Updated section order expectations |
| `internal/tui/tiles_normalization_test.go` | Added actual_tool section check, interface_ metric fixtures |
| `internal/providers/cursor/cursor_test.go` | Updated to expect `interface_` metrics |
| `cmd/demo/main.go` | Comprehensive cursor-ide demo snapshot with all sections (98 tools, 26 languages, code stats, interface breakdown) |
| `cmd/demo/main_test.go` | Updated assertions for new metric keys |

## Demo Representation

The demo snapshot (`cmd/demo/main.go:buildCursorDemoSnapshot`) produces a 1:1 structural replica of a real Cursor provider tile with:
- 8 models (5 visible + 3 more)
- 4 client interfaces (Composer, CLI Agents, Human, Tab Completion)
- 98 tool entries (6 visible + 92 more) including MCP tools
- 26 language entries (6 visible + 20 more)
- Full code statistics, billing, team, and activity compact rows
- Anonymized account data, numbers randomized per run via `randomizeDemoSnapshots`
