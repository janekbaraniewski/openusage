# Design: Cursor Team Budget Stacked Gauge

## Problem

The Cursor provider's tile shows two gauge bars: "Credit Limit" (team pooled budget) and "Credits" (total plan spend). The "Credits" bar is redundant with "Credit Limit" for team accounts — both show spend against a limit. Users on team plans want to see how much of the team budget **they** consumed vs the rest of the team, at a glance.

## Solution

Replace the second gauge bar ("Credits" / `plan_spend`) with a **stacked "Team Budget" gauge** that shows two colored segments within a single bar:

```
Credit Limit  ███▓▓░░░░░░░░  13.6%    (self=green, others=yellow, empty=track)
Team Budget   ██████████████  100.0%   (self=teal, others=peach, empty=track)
```

- **Segment 1 (self)**: `individual_spend` / `spend_limit` — the current user's contribution
- **Segment 2 (others)**: (`spend_limit.Used` - `individual_spend`) / `spend_limit` — the rest of the team
- **Empty track**: remaining budget

When team data is unavailable (`PooledLimit <= 0`), fall back to the existing `plan_spend` single gauge.

## Goals

- Show self vs team spend breakdown in the tile's top usage section
- Reuse existing data from the Cursor API (no new API calls needed)
- Keep the change minimal — only affect the Cursor provider tile

## Non-Goals

- Changing the detail view or analytics tab
- Adding stacked gauge support to other providers
- Modifying the daemon/telemetry pipeline
- New config options for this feature

## Data Available

The Cursor `GetCurrentPeriodUsage` API already returns all needed fields:

```json
{
  "spendLimitUsage": {
    "pooledLimit": 360000,      // team budget in cents
    "pooledUsed": 48800,        // total team spend in cents
    "pooledRemaining": 311200,  // team budget remaining
    "individualUsed": 20000     // current user's spend in cents
  }
}
```

Current metrics created from this data:
- `spend_limit`: Limit=pooledLimit, Used=pooledUsed, Remaining=pooledRemaining (USD)
- `individual_spend`: Used=individualUsed (USD) — **no Limit set, so can't render as gauge today**

## Design

### 1. New metric: `team_budget`

Instead of modifying existing metrics, create a new composite metric in the Cursor provider's Fetch that carries both segments:

```go
// in cursor.go, after creating spend_limit and individual_spend metrics
if su.PooledLimit > 0 {
    selfDollars := su.IndividualUsed / 100.0
    othersDollars := (su.PooledUsed - su.IndividualUsed) / 100.0
    totalUsedDollars := su.PooledUsed / 100.0
    pooledLimitDollars := su.PooledLimit / 100.0

    snap.Metrics["team_budget"] = core.Metric{
        Limit: &pooledLimitDollars,
        Used:  &totalUsedDollars,
        Unit:  "USD",
        Window: "billing-cycle",
    }
    // Store segment data in Raw for the stacked gauge renderer
    snap.Raw["team_budget_self"] = fmt.Sprintf("%.2f", selfDollars)
    snap.Raw["team_budget_others"] = fmt.Sprintf("%.2f", othersDollars)
}
```

### 2. New TUI function: `RenderStackedUsageGauge`

Add a new rendering function in `gauge.go` that draws a stacked bar with two segments:

```go
func RenderStackedUsageGauge(segments []GaugeSegment, width int) string
```

Where `GaugeSegment` is:

```go
type GaugeSegment struct {
    Percent float64
    Color   lipgloss.Color
    Label   string  // for legend (optional)
}
```

The function:
1. Divides the bar width proportionally between segments
2. Renders each segment with its own color using full block chars
3. Fills remaining width with track chars (`░`)
4. Appends the total percentage at the end

### 3. Widget configuration: `StackedGaugeKeys`

Add a new field to `DashboardWidget` that tells the tile renderer which metrics should use stacked rendering:

```go
// In core/widget.go, add to DashboardWidget:
StackedGaugeKeys map[string]StackedGaugeConfig
```

```go
type StackedGaugeConfig struct {
    SegmentRawKeys []string // Raw keys containing segment USD values
    SegmentLabels  []string // Labels for each segment
    SegmentColors  []string // Color names: "teal", "peach", etc.
}
```

Cursor configures it as:

```go
cfg.StackedGaugeKeys = map[string]StackedGaugeConfig{
    "team_budget": {
        SegmentRawKeys: []string{"team_budget_self", "team_budget_others"},
        SegmentLabels:  []string{"You", "Team"},
        SegmentColors:  []string{"teal", "peach"},
    },
}
```

### 4. Tile renderer integration

In `tiles.go:buildTileGaugeLines()`, before the standard gauge path, check if the metric key is in `StackedGaugeKeys`. If so, parse segment values from `snap.Raw`, compute percentages against the metric's Limit, and call `RenderStackedUsageGauge`.

### 5. GaugePriority update

In Cursor's widget config, replace `"plan_spend"` with `"team_budget"` in the GaugePriority list:

```go
cfg.GaugePriority = []string{
    "spend_limit", "team_budget", "plan_percent_used",
    "plan_auto_percent_used", "plan_api_percent_used",
}
```

### 6. Fallback behavior

When `PooledLimit <= 0` (solo account, no team budget), `team_budget` metric is not created. The GaugePriority allowlist falls through to `plan_percent_used` as the second gauge, which is the existing behavior for non-team accounts.

### 7. Summary text update

The summary text below gauges in `model.go` (`computeDisplayInfo`) already reads `spend_limit` first — it shows "$488 / $3600 spent" and "$3112 remaining". Add the self-spend detail: "$200 by you · $288 by team".

## Impact Analysis

| File | Change |
|------|--------|
| `internal/providers/cursor/cursor.go` | Add `team_budget` metric + Raw segment values |
| `internal/providers/cursor/widget.go` | Update GaugePriority, add StackedGaugeKeys config |
| `internal/core/widget.go` | Add `StackedGaugeKeys` field + `StackedGaugeConfig` type |
| `internal/tui/gauge.go` | Add `GaugeSegment` type + `RenderStackedUsageGauge` function |
| `internal/tui/tiles.go` | Integrate stacked gauge in `buildTileGaugeLines` |
| `internal/tui/model.go` | Update summary text to show self vs team detail |
| `internal/providers/cursor/cursor_test.go` | Test new metric creation |
| `internal/tui/gauge_test.go` | Test stacked gauge rendering |

## Implementation Tasks

### Task 1: Add StackedGaugeConfig types to core/widget.go

Files: `internal/core/widget.go`
Depends on: none
Description: Add `GaugeSegment` struct (not TUI-specific, just the config), `StackedGaugeConfig` struct, and `StackedGaugeKeys map[string]StackedGaugeConfig` field to `DashboardWidget`. Initialize to empty map in `DefaultDashboardWidget()`.
Tests: None needed (type-only change).

### Task 2: Add RenderStackedUsageGauge to gauge.go

Files: `internal/tui/gauge.go`, `internal/tui/gauge_test.go`
Depends on: none
Description: Add the `GaugeSegment` type (with Percent + Color fields) and `RenderStackedUsageGauge(segments []GaugeSegment, totalPercent float64, width int)` function. The function divides bar width proportionally across segments, renders each with its color, fills empty track, and appends total percentage. Use the same Unicode block char approach as `RenderUsageGauge`.
Tests: Test with 2 segments at various percentages; test edge cases (0%, 100%, single segment, empty segments).

### Task 3: Create team_budget metric in Cursor provider

Files: `internal/providers/cursor/cursor.go`, `internal/providers/cursor/cursor_test.go`
Depends on: none
Description: In the Fetch function, when `su.PooledLimit > 0`, create `snap.Metrics["team_budget"]` with Limit=pooledLimit and Used=pooledUsed (both in USD). Store `team_budget_self` and `team_budget_others` in `snap.Raw` as formatted dollar strings. Also add `team_budget` to the metric label override in gaugeLabel.
Tests: Extend existing test that verifies metric creation to check `team_budget` metric and Raw values.

### Task 4: Configure Cursor widget for stacked gauge

Files: `internal/providers/cursor/widget.go`
Depends on: Task 1
Description: Replace `"plan_spend"` with `"team_budget"` in `GaugePriority`. Add `StackedGaugeKeys` configuration mapping `"team_budget"` to its segment Raw keys, labels, and colors.
Tests: None needed (configuration-only change verified by integration).

### Task 5: Integrate stacked gauge in tile renderer

Files: `internal/tui/tiles.go`
Depends on: Task 1, Task 2
Description: In `buildTileGaugeLines()`, before calling `RenderUsageGauge`, check if the metric key exists in `widget.StackedGaugeKeys`. If so, parse segment values from `snap.Raw`, compute each segment's percentage against the metric's Limit, build `GaugeSegment` slices with the configured colors, and call `RenderStackedUsageGauge`. Add "Team Budget" to the `gaugeLabel` overrides.
Tests: Covered by integration (visual rendering — difficult to unit test meaningfully).

### Task 6: Update summary text in model.go

Files: `internal/tui/model.go`
Depends on: Task 3
Description: In `computeDisplayInfo`, when `spend_limit` is found and `individual_spend` is also present, update the detail line to show "you $X · team $Y" breakdown. The summary line ("$488 / $3600 spent") stays the same.
Tests: Covered by existing display info tests (extend with individual_spend case).

### Dependency Graph

- Tasks 1, 2, 3: parallel group (no mutual dependencies)
- Task 4: depends on Task 1
- Task 5: depends on Tasks 1, 2
- Task 6: depends on Task 3
- After all: build + manual visual verification
