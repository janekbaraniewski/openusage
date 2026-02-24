# Detail Page Redesign Design

Date: 2026-02-24
Status: Proposed
Author: janekbaraniewski

## 1. Problem Statement

The detail panel is a flat, text-heavy wall of metrics with minimal visual hierarchy — it doesn't use the rich charting toolkit already in the codebase (braille charts, horizontal bar charts, budget gauges, token breakdowns, heatmaps) and completely ignores available data like `ModelUsageRecord` and the full depth of `DailySeries`, making it uninformative at a glance.

## 2. Goals

1. Replace the flat metric lists with graphical representations: bar charts for model costs, token breakdowns with visual bars, budget gauges with burn projections, and braille time-series charts for trends.
2. Surface all available data: add a dedicated Models section using `ModelUsageRecord`, show full `DailySeries` as charts (not just sparklines), and display `Attributes`/`Diagnostics` cleanly separated from `Raw`.
3. Create clear visual hierarchy with distinct section cards, consistent spacing, and information density that adapts to terminal width.
4. Reduce noise: hide zero-value metrics, collapse sparse sections, use smart defaults so providers with little data look clean rather than empty.

## 3. Non-Goals

1. **Changing the tile grid view.** Dashboard tiles are untouched.
2. **Changing the analytics tab.** Analytics stays as-is.
3. **Adding new data collection.** No daemon/telemetry/provider changes — this uses existing data only.
4. **Adding interactivity beyond scrolling/tabs.** No clickable elements, expandable rows, or sub-navigation. Keep the read-only scroll model.
5. **Changing keyboard navigation.** Enter/Esc/scroll/tab-switch stay the same.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | Add 2 new `DetailSectionStyle` constants |
| providers | minor | Providers with rich data (claude_code, cursor, openrouter) get updated `DetailWidget()` returns |
| TUI | major | Rewrite `detail.go` section renderers to use chart components from `charts.go` |
| config | none | No config changes |
| detect | none | No detection changes |
| daemon | none | No collection changes |
| telemetry | none | No pipeline changes |
| CLI | none | No command changes |

### Existing Design Doc Overlap

- **DATA_TIME_FRAMES_DESIGN**: Complementary. Detail page should show the active time window label in the header. No conflict.
- **MODEL_NORMALIZATION_DESIGN**: Complementary. The new Models section uses `ModelUsageRecord` which carries canonical IDs and confidence scores from this design.
- **UNIFIED_AGENT_USAGE_TRACKING_DESIGN**: Complementary. Event-derived data flows into `ModelUsageRecord` and `DailySeries` which this redesign will surface.
- **MULTI_ACCOUNT_DESIGN**: Complementary. Account identity shown in header already; no additional changes needed here.

This design is standalone and does not extend or supersede any existing doc.

## 5. Detailed Design

### 5.1 New Detail Layout

The redesigned detail panel has this structure (top to bottom):

```
┌─────────────────────────────────────────────────────┐
│  Hero Header Card (existing, cleaned up)            │
│  Account name · Status pill · Provider tag          │
│  Meta tags (email, plan, org)                       │
│  Hero gauge + summary                               │
│  Timestamp · Time window badge                      │
└─────────────────────────────────────────────────────┘

  [All] [Usage] [Models] [Spending] [Trends] [Info]
  ─────────────────────────────────────────────────

  ⚡ Usage ─────────────────────────────────────────
  ┌ Budget gauges with burn-rate projections         │
  │ Plan Used    ████████████░░░  $180 / $300  60%  │
  │              🟡 ~12 days until limit at $5/day   │
  │ Spend Limit  ██████░░░░░░░░░  $45 / $100  45%  │
  └──────────────────────────────────────────────────┘
  Rate limits (usage table, existing)

  🤖 Models ────────────────────────────────────────
  Horizontal bar chart of model costs (top 8):
    claude-opus-4   ██████████████░░  $125.00
    gpt-4-turbo     ██████████░░░░░░   $85.32
    claude-sonnet   ████░░░░░░░░░░░░   $32.10

  Token breakdown per model:
    Input   ████████████████░░░░  125.3K tok
    Output  ██████████░░░░░░░░░░   85.1K tok

  💰 Spending ──────────────────────────────────────
  Cost summary (key metrics, cleaned up)
  Model cost table (existing, improved formatting)

  📈 Trends ────────────────────────────────────────
  Braille line chart: Daily Cost (7 data points)
    $12 ┤⠀⠀⣀⠤⠒⠉
     $8 ┤⠀⡔⠁
     $4 ┤⡠⠃
     $0 ┤⠁
        └──────────────────
         Feb 18    Feb 21    Feb 24

  Sparklines for tokens/messages/sessions

  📊 Tokens ────────────────────────────────────────
  Token usage table (existing)
  Sparklines (existing, kept)

  📈 Activity ──────────────────────────────────────
  Activity metrics + sparklines (existing)

  ⏰ Timers ────────────────────────────────────────
  Reset timers (existing, unchanged)

  › Info ───────────────────────────────────────────
  Attributes (clean section)
  Diagnostics (if any, with warning styling)
  Raw metadata (grouped, existing)
```

### 5.2 New Section Styles

Add two new `DetailSectionStyle` constants to `internal/core/detail_widget.go`:

```go
const (
    // existing...
    DetailSectionStyleModels DetailSectionStyle = "models"
    DetailSectionStyleTrends DetailSectionStyle = "trends"
)
```

**`DetailSectionStyleModels`**: Renders `ModelUsageRecord` data as:
1. Horizontal bar chart of model costs (using existing `RenderHBarChart`)
2. Token breakdown per top model (using existing `RenderTokenBreakdown`)
3. Falls back to the existing model cost table if `ModelUsageRecord` is empty but metric-key-based model costs exist

**`DetailSectionStyleTrends`**: Renders `DailySeries` data as:
1. Braille line chart for the primary series (cost or tokens) using existing `RenderBrailleChart`
2. Sparklines for secondary series below the chart
3. Hidden entirely if no `DailySeries` data exists

### 5.3 Models Section Renderer

**Architecture note**: The existing `renderMetricGroup` dispatches to section renderers with only `group.entries` (not the full snapshot). The Models and Trends sections need `snap.ModelUsage` and `snap.DailySeries` respectively, which are not metric-group entries. These two sections are dispatched **directly from `RenderDetailContent`**, not through `renderMetricGroup`. They render when their tab is active (or "All" tab), checking data availability inline.

New function in `detail.go`:

```go
func renderModelsSection(sb *strings.Builder, snap core.UsageSnapshot, widget core.DashboardWidget, w int) {
    // 1. If snap.ModelUsage has records, render from structured data
    // 2. Build chartItems from ModelUsageRecord sorted by CostUSD desc
    // 3. Render top 8 as RenderHBarChart
    // 4. For the top model with token data, render RenderTokenBreakdown
    // 5. Fallback: if no ModelUsage, delegate to existing renderModelCostsTable
}
```

Data flow:
- `snap.ModelUsage` → sort by `CostUSD` descending → take top 8
- Each record becomes a `chartItem{Label: record.Canonical, Value: *record.CostUSD, Color: colorForModel(...)}`
- If a record has `InputTokens` and `OutputTokens`, render `RenderTokenBreakdown` below the bar chart

### 5.4 Trends Section Renderer

New function in `detail.go`:

```go
func renderTrendsSection(sb *strings.Builder, snap core.UsageSnapshot, widget core.DashboardWidget, w int) {
    // 1. Find primary series: prefer "cost", fallback "tokens_total", "messages"
    // 2. Render as RenderBrailleChart (height=6, compact)
    // 3. Render remaining candidate series as sparklines below
}
```

Data flow:
- `snap.DailySeries` → pick primary key → build `BrailleSeries`
- Use `RenderBrailleChart` with `h=6` for a compact but readable chart
- Width adapts: `w - 4` for the chart area
- If fewer than 3 data points, skip the chart and show sparklines only

### 5.5 Spending Section Improvements

**Architecture note**: The current `renderSpendingSection` signature is `func renderSpendingSection(sb *strings.Builder, entries []metricEntry, w int)` — it does not receive `snap`. To support burn-rate extraction, the signature must be expanded to accept a burn rate value. The full call chain is: `RenderDetailContent` (has `snap`) extracts `burnRate` from `snap.Metrics["burn_rate"].Used` (0 if absent), passes it to `renderMetricGroup` (new `burnRate float64` parameter), which forwards it to `renderSpendingSection`.

Upgrade `renderSpendingSection` to use `RenderBudgetGauge` for cost metrics that have both `Used` and `Limit`:

```go
// New signature: func renderSpendingSection(sb *strings.Builder, entries []metricEntry, w int, burnRate float64)
// burnRate is extracted by the caller from snap.Metrics["burn_rate"].Used (0 if absent)
if e.metric.Used != nil && e.metric.Limit != nil && *e.metric.Limit > 0 {
    line := RenderBudgetGauge(e.label, *e.metric.Used, *e.metric.Limit, gaugeW, labelW, color, burnRate)
    sb.WriteString(line + "\n")
}
```

This replaces the current flat `label: value` rendering with a visual gauge + burn-rate projection line.

### 5.6 Usage Section Improvements

Upgrade `renderUsageSection` to better handle the gauge entries:

- Use consistent gauge widths (adapt to available width, min 12, max 32)
- Show percentage text inline with the gauge (already done)
- Add a sub-line with the actual values (`45,000 / 100,000 tokens`) in dim style
- Group rate-limit gauges visually (they currently scatter)

### 5.7 Info Section Cleanup

Split the current monolithic "Info" section into three distinct sub-sections:

```go
func renderInfoSection(sb *strings.Builder, snap core.UsageSnapshot, widget core.DashboardWidget, w int) {
    // 1. Attributes: clean key-value with highlight color
    if len(snap.Attributes) > 0 {
        renderDetailSectionHeader(sb, "Attributes", w)
        renderKeyValuePairs(sb, snap.Attributes, widget, w, valueStyle)
    }
    // 2. Diagnostics: warning-styled key-value (new style)
    if len(snap.Diagnostics) > 0 {
        renderDetailSectionHeader(sb, "Diagnostics", w)
        // warnValueStyle is a new style to create in styles.go:
        //   warnValueStyle = lipgloss.NewStyle().Foreground(colorYellow)
        renderKeyValuePairs(sb, snap.Diagnostics, widget, w, warnValueStyle)
    }
    // 3. Raw: grouped as before (existing renderRawData)
    // Note: pass snap.Raw directly, NOT snapshotMetaEntries(snap) which merges
    // Attributes+Diagnostics+Raw and would duplicate the entries already rendered above.
    if len(snap.Raw) > 0 {
        renderDetailSectionHeader(sb, "Raw Data", w)
        renderRawData(sb, snap.Raw, widget, w)
    }
}
```

### 5.8 Zero-Value Suppression

Apply smart filtering in `renderMetricGroup` before delegating to section renderers:

```go
// Filter out zero-value non-quota metrics when the provider opts in
if widget.SuppressZeroNonUsageMetrics {
    entries = filterNonZeroEntries(entries)
}
```

This uses the existing `SuppressZeroNonUsageMetrics` and `SuppressZeroMetricKeys` fields from `DashboardWidget` — currently only applied in tiles, now also in detail.

### 5.9 Tab Generation Updates

Update `DetailTabs()` to include the new sections:

```go
func DetailTabs(snap core.UsageSnapshot) []string {
    tabs := []string{"All"}
    // existing metric group tabs...
    if len(snap.ModelUsage) > 0 || hasModelCostMetrics(snap) {
        tabs = append(tabs, "Models")
    }
    if len(snap.DailySeries) >= 2 { // need at least 2 series for a meaningful chart
        tabs = append(tabs, "Trends")
    }
    // existing Timers and Info tabs...
}
```

The "Models" and "Trends" tabs only appear when relevant data exists — sparse providers (like OpenAI with just rate limits) won't show these tabs at all.

### 5.10 DetailWidget Updates for Rich Providers

Update `DetailWidget()` for providers with rich data:

```go
// claude_code, cursor, openrouter:
func (p *Provider) DetailWidget() core.DetailWidget {
    return core.DetailWidget{
        Sections: []core.DetailSection{
            {Name: "Usage", Order: 1, Style: core.DetailSectionStyleUsage},
            {Name: "Models", Order: 2, Style: core.DetailSectionStyleModels},
            {Name: "Spending", Order: 3, Style: core.DetailSectionStyleSpending},
            {Name: "Trends", Order: 4, Style: core.DetailSectionStyleTrends},
            {Name: "Tokens", Order: 5, Style: core.DetailSectionStyleTokens},
            {Name: "Activity", Order: 6, Style: core.DetailSectionStyleActivity},
        },
    }
}
```

Sparse providers (openai, anthropic, groq) continue using `DefaultDetailWidget()` unchanged — they'll show Usage + whatever metrics they have, no empty Models or Trends tabs.

### 5.11 Backward Compatibility

- **Existing configs**: Unchanged. No new config fields.
- **Existing provider behavior**: All providers continue to work. Default detail widget unchanged.
- **Stored data**: No schema changes. Uses existing `ModelUsageRecord` and `DailySeries` fields.
- **Keyboard navigation**: Unchanged. Same Enter/Esc/scroll/tab model.
- **Visual regressions**: The "All" tab changes layout, but individual section tabs remain comparable. Providers with sparse data see no change since new sections don't render without data.

## 6. Alternatives Considered

### Keep flat text layout, just add colors
Rejected because the core problem is visual hierarchy, not just color. Colored text in a flat list is still a flat list. The charting components already exist in `charts.go` and are battle-tested in the analytics tab.

### Add interactive drill-down (expand/collapse sections)
Rejected per non-goals. Adds complexity to the Bubble Tea model (tracking expanded state per section per provider) for marginal benefit. The tab system already provides section filtering.

### Render detail as a two-column layout
Rejected because terminal width varies too much (80-200+ chars). A single-column scrolling layout with responsive widths is more reliable. The current approach of adapting `labelW` and `gaugeW` based on available width works well.

## 7. Implementation Tasks

### Task 1: Add new DetailSectionStyle constants
Files: `internal/core/detail_widget.go`
Depends on: none
Description: Add `DetailSectionStyleModels` and `DetailSectionStyleTrends` constants. Add corresponding cases to `SectionStyle()` if needed. No behavioral changes yet — these are just type definitions.
Tests: Verify constants exist and `SectionStyle()` returns them correctly. Add cases to any existing detail_widget tests.

### Task 2: Implement Models section renderer
Files: `internal/tui/detail.go`
Depends on: Task 1
Description: Add `renderModelsSection()` that reads `snap.ModelUsage`, sorts by cost, builds `chartItem` slice, and calls `RenderHBarChart` for the top 8 models. Below the chart, call `RenderTokenBreakdown` for the highest-cost model with token data. Fallback to existing `renderModelCostsTable` if `ModelUsage` is empty. Dispatch directly from `RenderDetailContent` (not through `renderMetricGroup`, which lacks the full snapshot).
Tests: Table-driven test with mock snapshots: (a) snapshot with ModelUsage records, (b) snapshot without ModelUsage but with model cost metrics, (c) empty snapshot. Verify output contains bar chart characters and model names.

### Task 3: Implement Trends section renderer
Files: `internal/tui/detail.go`
Depends on: Task 1
Description: Add `renderTrendsSection()` that picks the primary `DailySeries` key (prefer "cost", then "tokens_total", then "messages"), builds a `BrailleSeries`, and calls `RenderBrailleChart` with `h=6`. Render remaining candidate series as sparklines below. Dispatch directly from `RenderDetailContent` (not through `renderMetricGroup`, which lacks the full snapshot).
Tests: Table-driven test: (a) snapshot with cost daily series, (b) snapshot with only token series, (c) snapshot with < 2 data points (should skip chart). Verify braille characters appear in output.

### Task 4: Upgrade Spending section with budget gauges
Files: `internal/tui/detail.go`
Depends on: none
Description: In `renderSpendingSection`, detect metrics with both `Used` and `Limit` and render them using `RenderBudgetGauge` (from `charts.go`) instead of the current flat label+value. Extract burn rate from the "burn_rate" metric if present. Keep model cost table unchanged.
Tests: Test spending section with a mock metric that has Used+Limit, verify budget gauge output contains block characters and the burn-rate projection line.

### Task 5: Split Info section into Attributes/Diagnostics/Raw
Files: `internal/tui/detail.go`
Depends on: none
Description: Replace the monolithic "Info" tab content with three sub-sections. Attributes rendered with `valueStyle`, Diagnostics with a warning color, Raw with existing `renderRawData`. Update `renderInfoSection` to emit separate section headers. The "Info" tab in `DetailTabs()` should still appear when any of the three maps is non-empty.
Tests: Test with snapshot that has all three maps populated, verify three section headers appear. Test with only Raw populated, verify only Raw section renders.

### Task 6: Apply zero-value suppression in detail view
Files: `internal/tui/detail.go`
Depends on: none
Description: In `renderMetricGroup`, filter entries through `widget.SuppressZeroNonUsageMetrics` and `widget.SuppressZeroMetricKeys` before rendering. This matches the tile view behavior. Skip entries where all of Used/Remaining/Limit are nil or zero and the key is in the suppress list.
Tests: Test with a snapshot containing zero-value metrics on a provider with `SuppressZeroNonUsageMetrics=true`, verify they are excluded from output.

### Task 7: Update DetailTabs to include Models and Trends
Files: `internal/tui/detail.go`
Depends on: Task 2, Task 3
Description: Update `DetailTabs()` to dynamically add "Models" tab when `snap.ModelUsage` has records (or model cost metrics exist), and "Trends" tab when `snap.DailySeries` has a series with >= 2 points. Wire the tab names to the correct section renderers in `RenderDetailContent`.
Tests: Test `DetailTabs()` with various snapshot configurations. Verify tabs appear/disappear based on data presence.

### Task 8: Update rich providers' DetailWidget returns
Files: `internal/providers/claude_code/claude_code.go`, `internal/providers/cursor/cursor.go`, `internal/providers/openrouter/openrouter.go`
Depends on: Task 1
Description: Add a `DetailWidget()` method override on the `Provider` struct for claude_code, cursor, and openrouter. Currently these providers inherit `DetailWidget()` from `providerbase.Base` (at `providerbase/base.go:58`) which returns `core.DefaultDetailWidget()`. The override returns a `core.DetailWidget` with the new Models and Trends sections in addition to the standard ones. Other providers keep the inherited default.
Tests: Verify each updated provider's `DetailWidget()` returns sections including Models and Trends. Verify sparse providers still use the default.

### Task 9: Visual polish and width adaptation
Files: `internal/tui/detail.go`, `internal/tui/styles.go`
Depends on: Task 2, Task 3, Task 4, Task 5
Description: Tune spacing between sections (consistent blank line gaps), ensure all charts adapt to narrow terminals (< 60 chars) by falling back to simpler renderers (sparklines instead of braille charts, compact tables instead of bar charts). Add section-specific icon and color for "Models" (🤖, Lavender) and "Trends" (📈, Sapphire). Ensure the "All" tab produces a coherent flow with good visual rhythm.
Tests: Render detail content at various widths (40, 60, 80, 120) and verify no panics or layout breaks. Snapshot-style tests comparing output at different widths.

### Task 10: Integration test and demo verification
Files: `internal/tui/model_display_test.go`, `cmd/demo/main.go`
Depends on: all previous tasks
Description: Add integration tests that render full detail panels for representative providers (claude_code with rich data, openai with sparse data). Update the demo command's dummy data to include `ModelUsageRecord` and `DailySeries` so the redesigned detail is visible in `make demo`. Run `make test` to verify no regressions.
Tests: End-to-end render tests. Manual verification via `make demo`.
