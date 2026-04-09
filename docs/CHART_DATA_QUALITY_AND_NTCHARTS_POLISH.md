# Chart Data Quality & ntcharts Polish

Date: 2026-04-09
Status: Complete
Author: Claude (follow-up to `NTCHARTS_TUI_MIGRATION_DESIGN.md`)
Depends on: ntcharts migration (Tasks 1-5, all completed)

## 1. Problem Statement

The ntcharts backend migration (Tasks 1-5) is complete and working. However, the charts display **incorrect data** and miss opportunities to leverage ntcharts' full capabilities:

1. **Negative values in cost/token charts** ($-41.36, -22M tokens) — distorts Y-axis range and makes charts unreadable.
2. **Non-zero usage during inactive periods** — the chart shows ~$140/day during a 2-week vacation when actual usage was zero.
3. **Single-series aggregate charts only** — model/client/project breakdown trends exist but could be richer.
4. **Y-axis floor at negative values** — charts auto-scale to include negative outliers, wasting most of the chart area on empty space.

These are data-pipeline and chart-configuration issues, not ntcharts integration issues.

## 2. Root Cause Analysis

### 2.1 Negative Values

**Source:** Raw `usage_events.cost_usd` can contain negative values (refunds, reconciliation adjustments, provider billing corrections). The SQL aggregation:

```sql
-- internal/telemetry/usage_view_queries.go:403
SUM(COALESCE(cost_usd, 0)) AS cost_usd
```

…faithfully sums all values, including negatives. For tokens, a similar pattern applies:

```sql
SUM(COALESCE(total_tokens,
    COALESCE(input_tokens, 0) +
    COALESCE(output_tokens, 0) +
    COALESCE(reasoning_tokens, 0) +
    COALESCE(cache_read_tokens, 0) +
    COALESCE(cache_write_tokens, 0))) AS tokens
```

Negative token counts are likely data quality issues from provider APIs (e.g., corrections, delta-encoding artifacts).

**Impact:** A single day with -$41 makes the Y-axis span from -$41 to $319 instead of $0 to $319, compressing all real data into the upper 88% of the chart area.

### 2.2 Flat Vacation Data (binSeriesValues averaging)

**Root cause:** `binSeriesValues()` in `charts.go:486` divides summed values by the bin span:

```go
// charts.go:486
binned[si][col] = sum / span
```

This is called by `renderNTTimeBars()` (charts_ntcharts.go:245) when rendering bar-mode charts. The flow:

1. `alignSeriesByDate(series, true)` calls `fillContinuousDates()` which inserts zero-value entries for every calendar day between min and max date
2. `binSeriesValues()` groups multiple days into bins when chart width is limited
3. **The division by `span` averages actual-usage days with zero-usage days**, producing a flat ~$140/day appearance during vacation

**Example:**
- Real data: $700 on Monday, $0 Tue-Sun (vacation)
- After fill: 7 entries: [700, 0, 0, 0, 0, 0, 0]
- If binned into 1 column: sum=700 / span=7 = **$100/day** (misleading)
- Correct behavior: **sum only** (total $700 in that bin period), or skip zero days entirely

**Note:** This only affects `TimeChartBars` and `TimeChartStacked` modes. The default `RenderBrailleChart` (line chart mode) does NOT use binning — it plots raw points via `renderNTBrailleChart`, which is correct.

### 2.3 chartSeriesBounds includes negative values

`chartSeriesBounds()` in `charts_ntcharts.go:514` tracks `minY` and `maxY` across all data points. When negative values exist, `minY` becomes negative, which is passed to ntcharts as `WithYRange(minY, maxY)`. This makes the chart render the negative range even though it's just noise.

## 3. Fixes Required

### Fix 1: Clamp negative values at the data layer

**File:** `internal/tui/charts_ntcharts.go`

**Change:** Add a `sanitizeSeriesPoints()` helper that clamps negative values to 0 for metrics where negatives are not meaningful (cost, tokens, requests). Apply it in both `renderNTBrailleChart` and `renderNTTimeBars` before any processing.

```go
// sanitizeSeriesPoints clamps negative values to zero for metrics where
// negatives represent data quality issues (refunds, corrections) rather than
// meaningful data. Preserves the original slice.
func sanitizeSeriesPoints(pts []core.TimePoint) []core.TimePoint {
    out := make([]core.TimePoint, len(pts))
    for i, p := range pts {
        out[i] = p
        if p.Value < 0 {
            out[i].Value = 0
        }
    }
    return out
}
```

Apply in `renderNTBrailleChart`:
```go
for _, s := range filtered {
    style := lipgloss.NewStyle().Foreground(s.Color)
    ts.SetDataSetStyle(s.Label, style)
    for _, p := range dedupeSeriesPoints(sanitizeSeriesPoints(s.Points)) { // <-- added
        ...
    }
}
```

Apply similarly in `renderNTTimeBars` for the bar chart path.

**Scope:** Chart rendering only. The raw data in `DailySeries` and SQLite is preserved for accuracy — we only clamp at display time.

### Fix 2: Force Y-axis floor to zero

**File:** `internal/tui/charts_ntcharts.go`

**Change:** In `chartSeriesBounds()`, clamp `minY` to 0 when all values are non-negative after sanitization:

```go
if minY < 0 {
    minY = 0
}
```

This ensures the chart Y-range starts at $0, giving maximum resolution to the actual data range.

### Fix 3: Fix binSeriesValues to SUM instead of average

**File:** `internal/tui/charts.go`

**Change:** Replace the averaging with direct summing. For daily data shown as bars, each bar should represent the **total** for its bin period, not the average:

```go
// Before (line 486):
binned[si][col] = sum / span

// After:
binned[si][col] = sum
```

This makes bar charts show actual totals per period. A vacation week with $700 on Monday and $0 the rest correctly shows $700 for that bar (or $0 for any bar covering only the zero days).

**Alternative considered:** Skip zero-fill days entirely. Rejected because it would create irregular bin widths and misleading visual spacing. Summing with zero-fill is the correct approach for bar charts showing period totals.

### Fix 4: Strip zero-only days from line chart endpoints

**File:** `internal/tui/charts_ntcharts.go`

**Change:** In `renderNTBrailleChart`, after dedup and sanitize, trim leading/trailing zero-value points that don't contribute meaningful data. Keep interior zeros (they represent legitimate zero-usage days).

```go
func trimLeadingTrailingZeros(pts []core.TimePoint) []core.TimePoint {
    if len(pts) <= 2 {
        return pts
    }
    start := 0
    for start < len(pts)-1 && pts[start].Value == 0 {
        start++
    }
    end := len(pts) - 1
    for end > start && pts[end].Value == 0 {
        end--
    }
    // Keep one zero on each side for visual context.
    if start > 0 {
        start--
    }
    if end < len(pts)-1 {
        end++
    }
    return pts[start : end+1]
}
```

## 4. ntcharts Capabilities Not Yet Leveraged

The current integration uses ntcharts as a drop-in replacement for the old braille renderer. Several ntcharts features could significantly improve chart quality:

### 4.1 Multi-series overlay charts

**Current:** Each metric (cost, requests, tokens) gets its own separate chart. Model/client/project breakdowns also get separate charts.

**Available:** ntcharts `timeserieslinechart` supports multiple named datasets on a single chart with `PushDataSet(name, point)`. Each dataset gets its own color via `SetDataSetStyle(name, style)`.

**Recommendation:** Keep separate charts for metrics with different units (cost in $, requests in count, tokens in count). But for same-unit breakdowns (e.g., per-model cost), render them as overlaid lines on a single chart. This is already done for the model/client/project breakdown trend charts — no change needed.

### 4.2 Braille vs Arc rendering modes

**Current:** All charts use `DrawBrailleAll()` which renders dots.

**Available:** ntcharts supports `DrawAll()` with `ThinLineStyle` or `ArcLineStyle` for smooth connected lines using box-drawing characters. These produce cleaner visuals for time series with many data points.

**Recommendation:** For charts with > 14 data points, switch to `DrawAll()` with `runes.ArcLineStyle` for smoother lines. Keep `DrawBrailleAll()` for sparse data (< 14 points) where individual dots are more informative than connected lines.

```go
import "github.com/NimbleMarkets/ntcharts/canvas/runes"

// In renderNTBrailleChart:
if totalPoints > 14 {
    ts.SetLineStyle(runes.ArcLineStyle)
    ts.DrawAll()
} else {
    ts.DrawBrailleAll()
}
```

### 4.3 Viewport/zoom control

**Current:** Charts show the full time range of available data.

**Available:** ntcharts `timeserieslinechart` supports `SetViewTimeRange(start, end)` for zooming into a time window, and the underlying model supports scrolling.

**Recommendation (future):** Add `+`/`-` keybindings in the detail view to zoom in/out on the time axis. Store the current viewport range in the Model. This requires making the chart an interactive bubbletea component rather than a static string render — a larger refactor best done as a separate project.

### 4.4 Mouse wheel support

**Current:** No mouse interaction with charts.

**Available:** ntcharts supports BubbleZone mouse regions for click/scroll interaction.

**Recommendation (future):** Same prerequisite as 4.3 — requires component-based chart rendering. Not suitable for the current string-based architecture.

### 4.5 Sparkline braille mode

**Current:** `renderNTSparkline` uses braille mode (`sparkline.WithBrailleMode()`).

**Status:** Already leveraged. No change needed.

### 4.6 Heatmap component

**Current:** `renderNTHeatmap` uses `ntheatmap.New()` with custom color scales.

**Status:** Already leveraged via the analytics heatmap. Appears in the analytics screen but not in the detail view.

**Recommendation:** Add heatmap to the detail view for "activity by day of week" or "usage intensity calendar" if the data exists. Requires a new section builder in `detail.go` — scope for a future feature.

### 4.7 Custom X-axis label formatters

**Current:** Uses a custom formatter that calls `formatDateLabel()`.

**Available:** ntcharts provides built-in `DateTimeLabelFormatter()` and `HourTimeLabelFormatter()`.

**Recommendation:** Keep the custom formatter — our `formatDateLabel()` produces more compact output (e.g., "Apr 7" vs "06 04/07") that fits better in narrow terminals.

### 4.8 Bar chart stacking

**Current:** `renderNTTimeBars` supports stacked mode via `ntbarchart.BarData` with multiple values.

**Status:** Already leveraged for `TimeChartStacked` mode. No change needed.

## 5. Implementation Plan

### Phase 1: Data Quality Fixes (immediate)

| # | Fix | File | Effort |
|---|-----|------|--------|
| 1 | Add `sanitizeSeriesPoints()`, apply in chart renderers | `charts_ntcharts.go` | Small |
| 2 | Clamp `minY` to 0 in `chartSeriesBounds()` | `charts_ntcharts.go` | Trivial |
| 3 | Change `binSeriesValues` from average to sum | `charts.go:486` | Trivial |
| 4 | Add `trimLeadingTrailingZeros()` for line charts | `charts_ntcharts.go` | Small |
| 5 | Add tests for negative value handling and zero-trim | `charts_ntcharts_test.go` | Small |

### Phase 2: Visual Quality (short-term)

| # | Enhancement | File | Effort |
|---|-------------|------|--------|
| 6 | Switch to `ArcLineStyle` for charts with >14 points | `charts_ntcharts.go` | Small |
| 7 | Consistent chart heights across all detail sections | `detail.go` | Trivial |
| 8 | Verify legend truncation at narrow widths | `charts.go` legend helpers | Small |

### Phase 3: New Visualizations (DONE)

| # | Feature | Status |
|---|---------|--------|
| 9 | Activity heatmap in detail view | DONE — day-of-week heatmap from DailySeries |
| 10 | Chart zoom (+/- keys, Ctrl+scroll) | DONE — 6 zoom levels, keyboard + mouse |
| 11 | Mouse interaction (Ctrl+scroll zoom) | DONE — Ctrl+wheel zooms charts in detail |
| 12 | Dual-axis chart (cost + requests overlay) | DONE — overlay chart in detail view |
| 13 | Fill date gaps with zeros | DONE — inactive days show 0, not interpolated |

### Dependency Graph

```
Phase 1: Fixes 1-5 (all independent, can be done in any order)
Phase 2: 6, 7, 8 (all independent, depend on Phase 1)
Phase 3: 9 standalone; 10, 11, 12 depend on component refactor
```

## 6. Testing Strategy

### Data Quality Tests

```go
func TestSanitizeSeriesPoints_ClampsNegatives(t *testing.T) {
    pts := []core.TimePoint{
        {Date: "2026-01-01", Value: 100},
        {Date: "2026-01-02", Value: -41.36},
        {Date: "2026-01-03", Value: 200},
    }
    sanitized := sanitizeSeriesPoints(pts)
    if sanitized[1].Value != 0 {
        t.Errorf("expected 0, got %f", sanitized[1].Value)
    }
    // Original unchanged
    if pts[1].Value != -41.36 {
        t.Errorf("original modified")
    }
}

func TestBinSeriesValues_SumsNotAverages(t *testing.T) {
    dates := []string{"2026-01-01", "2026-01-02", "2026-01-03", "2026-01-04"}
    values := [][]float64{{700, 0, 0, 0}}
    labels, binned := binSeriesValues(dates, values, 2)
    // First bin: 700+0 = 700 (not 350)
    if binned[0][0] != 700 {
        t.Errorf("expected sum 700, got %f", binned[0][0])
    }
    // Second bin: 0+0 = 0
    if binned[0][1] != 0 {
        t.Errorf("expected 0, got %f", binned[0][1])
    }
}

func TestChartSeriesBounds_FloorsAtZero(t *testing.T) {
    series := []BrailleSeries{{
        Points: []core.TimePoint{
            {Date: "2026-01-01", Value: -50},
            {Date: "2026-01-02", Value: 300},
        },
    }}
    _, _, minY, _, _ := chartSeriesBounds(series)
    if minY < 0 {
        t.Errorf("minY should be >= 0, got %f", minY)
    }
}
```

### Visual Regression Tests

Existing tests in `charts_ntcharts_test.go` already cover sparkline, braille chart, stacked bars, heatmap, and stacked tool bars. Phase 1 fixes should not break any of these — run `go test ./internal/tui/` after each change.

## 7. Files Modified

| File | Changes |
|------|---------|
| `internal/tui/charts_ntcharts.go` | Add `sanitizeSeriesPoints()`, `trimLeadingTrailingZeros()`, clamp minY, apply sanitization in chart renderers |
| `internal/tui/charts.go` | Fix `binSeriesValues` to sum instead of average (line 486) |
| `internal/tui/charts_ntcharts_test.go` | Add tests for negative clamping, zero trimming, Y-floor |

No changes to: core types, providers, telemetry, daemon, config, or CLI.

## 8. Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Clamping negatives hides real refund data | Low | Low | Only applied at display time; raw data preserved in SQLite and `DailySeries` |
| Sum-vs-average change breaks stacked bar charts | Medium | Medium | Test all `TimeChartBars` and `TimeChartStacked` call sites |
| ArcLineStyle rendering artifacts at narrow widths | Low | Low | Keep braille fallback for < 14 data points |
| trimLeadingTrailingZeros removes meaningful data | Low | Medium | Keep 1 zero on each side for visual context |
