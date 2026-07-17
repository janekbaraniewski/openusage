package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// makeUsageMetric returns a Metric with the given used+limit+window suitable
// for driving buildTileGaugeLines through metricUsedPercent.
func makeUsageMetric(used, limit float64, window string) core.Metric {
	u, l := used, limit
	return core.Metric{
		Used:   &u,
		Limit:  &l,
		Window: window,
		Unit:   "requests",
	}
}

func tileGaugeTestModel(now time.Time) Model {
	return Model{
		warnThreshold: 0.30,
		critThreshold: 0.15,
		referenceTime: now,
	}
}

func tileGaugeTestWidget() core.DashboardWidget {
	return core.DashboardWidget{
		GaugeMaxLines: 2,
		GaugePriority: []string{"rate_limit_5h"},
	}
}

func TestBuildTileGaugeLines_ProjectionShownWhenExpected(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	// 50% used, 2.5h remaining in a 5h window → elapsed 2.5h. Pace projects
	// reaching 100% in roughly another 2.5h.
	met := makeUsageMetric(50, 100, "5h")
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Metrics:    map[string]core.Metric{"rate_limit_5h": met},
		Resets:     map[string]time.Time{"rate_limit_5h": now.Add(2*time.Hour + 30*time.Minute)},
	}

	m := tileGaugeTestModel(now)
	widget := tileGaugeTestWidget()

	lines := m.buildTileGaugeLines(snap, widget, 60)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines (gauge + annotation), got %d: %v", len(lines), lines)
	}
	// Gauge is first, annotation is second.
	annotation := lines[1]
	if !strings.Contains(annotation, "resets") {
		t.Errorf("expected annotation to contain 'resets', got %q", annotation)
	}
	if !strings.Contains(annotation, "100%") {
		t.Errorf("expected annotation to contain '100%%' projection, got %q", annotation)
	}
}

func TestBuildTileGaugeLines_SuppressedWhenNoReset(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	met := makeUsageMetric(50, 100, "5h")
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Metrics:    map[string]core.Metric{"rate_limit_5h": met},
		// Resets intentionally empty: no reset timestamp known.
	}

	m := tileGaugeTestModel(now)
	widget := tileGaugeTestWidget()

	lines := m.buildTileGaugeLines(snap, widget, 60)
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 line (gauge only), got %d: %v", len(lines), lines)
	}
	if strings.Contains(lines[0], "resets") || strings.Contains(lines[0], "100% in") {
		t.Errorf("expected no projection annotation, got %q", lines[0])
	}
}

func TestBuildTileGaugeLines_SuppressedWhenWindowUnrecognized(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	// "1m" is not in gaugeWindowDuration's allowlist (5h/1d/24h/today/7d/30d).
	met := makeUsageMetric(50, 100, "1m")
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Metrics:    map[string]core.Metric{"rate_limit_5h": met},
		Resets:     map[string]time.Time{"rate_limit_5h": now.Add(30 * time.Second)},
	}

	m := tileGaugeTestModel(now)
	widget := tileGaugeTestWidget()

	lines := m.buildTileGaugeLines(snap, widget, 60)
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 line (gauge only) for unrecognized window, got %d: %v", len(lines), lines)
	}
}

func TestBuildTileGaugeLines_SuppressedWhenUsageZero(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	// Pace cannot be computed when used == 0; but we still expect the
	// reset half of the annotation when a reset is known.
	met := makeUsageMetric(0, 100, "5h")
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Metrics:    map[string]core.Metric{"rate_limit_5h": met},
		Resets:     map[string]time.Time{"rate_limit_5h": now.Add(2 * time.Hour)},
	}

	m := tileGaugeTestModel(now)
	widget := tileGaugeTestWidget()

	lines := m.buildTileGaugeLines(snap, widget, 60)
	if len(lines) != 2 {
		t.Fatalf("expected gauge + reset-only annotation, got %d: %v", len(lines), lines)
	}
	annot := lines[1]
	if !strings.Contains(annot, "resets") {
		t.Errorf("expected reset half to render even with 0%% usage, got %q", annot)
	}
	if strings.Contains(annot, "100% in") {
		t.Errorf("expected NO projection half when usage is zero, got %q", annot)
	}
}

func TestBuildTileGaugeLines_SuppressedWhenAtOrOverLimit(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	met := makeUsageMetric(100, 100, "5h")
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Metrics:    map[string]core.Metric{"rate_limit_5h": met},
		Resets:     map[string]time.Time{"rate_limit_5h": now.Add(1 * time.Hour)},
	}

	m := tileGaugeTestModel(now)
	widget := tileGaugeTestWidget()

	lines := m.buildTileGaugeLines(snap, widget, 60)
	if len(lines) != 2 {
		t.Fatalf("expected gauge + reset-only annotation at 100%%, got %d: %v", len(lines), lines)
	}
	annot := lines[1]
	if strings.Contains(annot, "100% in") {
		t.Errorf("expected NO projection annotation when already at limit, got %q", annot)
	}
	if !strings.Contains(annot, "resets") {
		t.Errorf("expected reset annotation at limit, got %q", annot)
	}
}

func TestBuildTileGaugeLines_SuppressedWhenWindowJustStarted(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	// resetIn == windowDur → elapsed == 0 → no projection. Reset still renders.
	met := makeUsageMetric(5, 100, "5h")
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Metrics:    map[string]core.Metric{"rate_limit_5h": met},
		Resets:     map[string]time.Time{"rate_limit_5h": now.Add(5 * time.Hour)},
	}

	m := tileGaugeTestModel(now)
	widget := tileGaugeTestWidget()

	lines := m.buildTileGaugeLines(snap, widget, 60)
	if len(lines) != 2 {
		t.Fatalf("expected gauge + reset-only annotation, got %d: %v", len(lines), lines)
	}
	annot := lines[1]
	if strings.Contains(annot, "100% in") {
		t.Errorf("expected NO projection annotation when window just started, got %q", annot)
	}
}

func TestBuildTileGaugeLines_HideCostsDoesNotSuppressProjection(t *testing.T) {
	// Projection is a usage feature, not a cost feature. buildTileGaugeLines
	// does not take hideCosts; this test pins that behavior by asserting the
	// projection still renders regardless of how the caller resolves
	// hideCosts. We simulate the surrounding call by toggling
	// Model.hideCostsGlobal in case future refactors plumb it through.
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	met := makeUsageMetric(50, 100, "5h")
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Metrics:    map[string]core.Metric{"rate_limit_5h": met},
		Resets:     map[string]time.Time{"rate_limit_5h": now.Add(2*time.Hour + 30*time.Minute)},
	}
	widget := tileGaugeTestWidget()

	hidden := true
	mHidden := tileGaugeTestModel(now)
	mHidden.hideCostsGlobal = &hidden
	hiddenLines := mHidden.buildTileGaugeLines(snap, widget, 60)
	if len(hiddenLines) < 2 {
		t.Fatalf("expected projection annotation even with hide_costs=true, got %d lines: %v", len(hiddenLines), hiddenLines)
	}
	if !strings.Contains(hiddenLines[1], "100%") || !strings.Contains(hiddenLines[1], "resets") {
		t.Errorf("expected full projection annotation under hide_costs, got %q", hiddenLines[1])
	}

	// Sanity: same input with hide_costs unset produces an identical
	// gauge+annotation pair.
	mNoHide := tileGaugeTestModel(now)
	noHideLines := mNoHide.buildTileGaugeLines(snap, widget, 60)
	if len(noHideLines) != len(hiddenLines) {
		t.Fatalf("hide_costs changed gauge line count (hidden=%d, default=%d)", len(hiddenLines), len(noHideLines))
	}
}

func TestBuildTileGaugeLines_RespectsMaxLines(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	// Two gauge-eligible metrics with projections. With GaugeMaxLines=1 the
	// second gauge should be skipped entirely (not just its annotation).
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Metrics: map[string]core.Metric{
			"rate_limit_5h": makeUsageMetric(50, 100, "5h"),
			"rate_limit_7d": makeUsageMetric(40, 100, "7d"),
		},
		Resets: map[string]time.Time{
			"rate_limit_5h": now.Add(2*time.Hour + 30*time.Minute),
			"rate_limit_7d": now.Add(3 * 24 * time.Hour),
		},
	}
	m := tileGaugeTestModel(now)
	widget := core.DashboardWidget{
		GaugeMaxLines: 1,
		GaugePriority: []string{"rate_limit_5h", "rate_limit_7d"},
	}

	lines := m.buildTileGaugeLines(snap, widget, 60)
	// One gauge + one annotation = 2 entries. The second gauge must be
	// suppressed because GaugeMaxLines=1 counts gauges, not annotation rows.
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 entries (1 gauge + 1 annotation), got %d: %v", len(lines), lines)
	}
}

func TestTileGaugeProjectionAnnotation_FitsInWindow(t *testing.T) {
	// 5h window, started 1h ago (resetIn=4h), 50% used → pace = 0.5%/min,
	// remaining 50% → 100min to 100%. 100m < 240m to reset → "100% in" branch.
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	met := makeUsageMetric(50, 100, "5h")
	snap := core.UsageSnapshot{
		Resets: map[string]time.Time{"rate_limit_5h": now.Add(4 * time.Hour)},
	}

	out := tileGaugeProjectionAnnotation(snap, "rate_limit_5h", met, 50, now)
	if !strings.Contains(out, "resets") {
		t.Errorf("expected reset half, got %q", out)
	}
	if !strings.Contains(out, "100% in") {
		t.Errorf("expected '100%% in' projection when pace fits in window, got %q", out)
	}
	if strings.Contains(out, "by reset") {
		t.Errorf("did not expect 'by reset' wording when pace fits, got %q", out)
	}
}

func TestTileGaugeProjectionAnnotation_SuffixedResetKeyAndAliasedWindows(t *testing.T) {
	// Some providers (copilot, opencode) store the reset timestamp under a
	// "<key>_reset" suffixed key rather than the bare metric key, and use
	// "rolling-5h"/"month" as documented Window aliases for "5h"/"30d"
	// (see core.Metric's Window doc comment). Both conventions must resolve
	// to a real annotation, not silently render nothing.
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	rolling := makeUsageMetric(50, 100, "rolling-5h")
	snap := core.UsageSnapshot{
		Resets: map[string]time.Time{"rolling_usage_reset": now.Add(4 * time.Hour)},
	}
	out := tileGaugeProjectionAnnotation(snap, "rolling_usage", rolling, 50, now)
	if !strings.Contains(out, "resets") {
		t.Errorf("rolling-5h window with _reset-suffixed key: expected a reset annotation, got %q", out)
	}

	monthly := makeUsageMetric(50, 100, "month")
	snap2 := core.UsageSnapshot{
		Resets: map[string]time.Time{"monthly_usage_pct_reset": now.Add(15 * 24 * time.Hour)},
	}
	out2 := tileGaugeProjectionAnnotation(snap2, "monthly_usage_pct", monthly, 50, now)
	if !strings.Contains(out2, "resets") {
		t.Errorf("month window with _reset-suffixed key: expected a reset annotation, got %q", out2)
	}
}

func TestTileGaugeProjectionAnnotation_OvershootsWindow(t *testing.T) {
	// User's reported scenario: 5h window started 1h 18m ago, 22% used.
	// Pace = 0.22/78 = 0.002820/min → pctPerMinute = 0.2820.
	// remaining 78% → 276m to 100%, but only 222m to reset.
	// projectedPct = 22 + 0.2820*222 ≈ 84.6 → "~85% by reset".
	now := time.Date(2026, 5, 18, 17, 18, 0, 0, time.UTC)
	met := makeUsageMetric(22, 100, "5h")
	resetAt := now.Add(3*time.Hour + 42*time.Minute) // 222 minutes
	snap := core.UsageSnapshot{
		Resets: map[string]time.Time{"rate_limit_5h": resetAt},
	}

	out := tileGaugeProjectionAnnotation(snap, "rate_limit_5h", met, 22, now)
	if !strings.Contains(out, "resets") {
		t.Errorf("expected reset half, got %q", out)
	}
	if !strings.Contains(out, "by reset") {
		t.Errorf("expected 'by reset' wording when projection overshoots window, got %q", out)
	}
	if strings.Contains(out, "100% in") {
		t.Errorf("did not expect '100%% in' wording when projection overshoots, got %q", out)
	}
	// Should report a percent in the 80s. Be specific to lock the math.
	if !strings.Contains(out, "~85%") {
		t.Errorf("expected projected ~85%% at reset, got %q", out)
	}
}

func TestTileGaugeProjectionAnnotation_OvershootCapsBelow100(t *testing.T) {
	// Engineer the rare case where projectedPct rounds up to 100% at
	// reset but we ARE still overshooting the window. The helper must
	// cap the printed value at 99 to avoid wording that contradicts the
	// branch ("we won't hit 100" → "but here is ~100").
	//
	// 5h window, resetIn=2m → elapsed=298m. usedPct=99, pctPerMin =
	// 99/298 ≈ 0.332. minutesTo100 = 1/0.332 ≈ 3.01m > 2m → branch taken.
	// projectedPct = 99 + 0.332*2 ≈ 99.66 → rounds to 100 → capped to 99.
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	met := makeUsageMetric(99, 100, "5h")
	resetAt := now.Add(2 * time.Minute)
	snap := core.UsageSnapshot{
		Resets: map[string]time.Time{"rate_limit_5h": resetAt},
	}

	out := tileGaugeProjectionAnnotation(snap, "rate_limit_5h", met, 99, now)
	if !strings.Contains(out, "by reset") {
		t.Errorf("expected 'by reset' wording, got %q", out)
	}
	if strings.Contains(out, "~100%") {
		t.Errorf("'by reset' branch must cap at ~99%%, got %q", out)
	}
	if !strings.Contains(out, "~99%") {
		t.Errorf("expected ~99%% printed (rounded down from ~99.66), got %q", out)
	}
}

func TestTileGaugeProjectionAnnotation_PaceOnlyWhenResetInPast(t *testing.T) {
	// resetIn < 0 → resetPart suppressed by the helper; projection half
	// still renders because elapsed (= windowDur - resetIn) is positive
	// and pace is meaningful.
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	met := makeUsageMetric(50, 100, "5h")
	snap := core.UsageSnapshot{
		Resets: map[string]time.Time{"rate_limit_5h": now.Add(-10 * time.Minute)},
	}

	out := tileGaugeProjectionAnnotation(snap, "rate_limit_5h", met, 50, now)
	if strings.Contains(out, "resets") {
		t.Errorf("expected no reset half when reset is in the past, got %q", out)
	}
	if !strings.Contains(out, "100% in") {
		t.Errorf("expected pace projection when reset has passed but pace is meaningful, got %q", out)
	}
}
