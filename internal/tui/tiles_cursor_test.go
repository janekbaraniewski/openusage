package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func cursorPlanSnap(now time.Time) core.UsageSnapshot {
	pct := func(used float64) core.Metric {
		remaining := 100 - used
		return core.Metric{
			Limit:     core.Float64Ptr(100),
			Used:      core.Float64Ptr(used),
			Remaining: core.Float64Ptr(remaining),
			Unit:      "%",
			Window:    "monthly",
		}
	}
	reset := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	return core.UsageSnapshot{
		ProviderID: "cursor",
		AccountID:  "cursor",
		Status:     core.StatusOK,
		Timestamp:  now,
		Metrics: map[string]core.Metric{
			"plan_percent_used":      pct(7),
			"plan_auto_percent_used": pct(6),
			"plan_api_percent_used":  pct(29),
			"context_window": {
				Used:      core.Float64Ptr(38.5),
				Remaining: core.Float64Ptr(61.5),
				Limit:     core.Float64Ptr(100),
				Unit:      "%",
			},
		},
		Resets: map[string]time.Time{
			"plan_percent_used": reset,
		},
		Attributes: map[string]string{
			"plan_tier": "Pro",
			"ondemand":  "disabled",
		},
	}
}

func TestBuildCursorTileGaugeLines_ShowsAutoAndAPILikeOverlay(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	m := tileGaugeTestModel(now)
	m.usageMode = config.UsageModeUsed

	lines := m.buildCursorTileGaugeLines(cursorPlanSnap(now), 56)
	out := strings.Join(lines, "\n")

	for _, want := range []string{"Included", "Auto", "API", "On-Demand", "Pro", "Sep 27", "7% used", "6% used", "29% used", "Disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Plan Limit") {
		t.Errorf("must not show Plan Limit stand-in, got:\n%s", out)
	}
	if strings.Contains(out, "Context Window") {
		t.Errorf("context window must not crowd out Auto/API on the tile, got:\n%s", out)
	}
	if !strings.Contains(out, "Resets") {
		t.Errorf("tile must show billing cycle reset time, got:\n%s", out)
	}

	joined := strings.Join(lines, " ")
	inc := strings.Index(joined, "Included")
	auto := strings.Index(joined, "Auto")
	api := strings.Index(joined, "API")
	ondemand := strings.Index(joined, "On-Demand")
	if inc < 0 || auto < inc || api < auto || ondemand < api {
		t.Errorf("expected Included → Auto → API → On-Demand order, got:\n%s", out)
	}
}

func TestBuildCursorTileGaugeLines_OmitsMissingBuckets(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	m := tileGaugeTestModel(now)
	m.usageMode = config.UsageModeUsed
	snap := cursorPlanSnap(now)
	delete(snap.Metrics, "plan_auto_percent_used")
	delete(snap.Attributes, "ondemand")

	out := strings.Join(m.buildCursorTileGaugeLines(snap, 56), "\n")
	if !strings.Contains(out, "Included") || !strings.Contains(out, "API") {
		t.Fatalf("expected remaining buckets, got:\n%s", out)
	}
	if strings.Contains(out, "Auto") {
		t.Fatalf("missing Auto metric must not invent a row, got:\n%s", out)
	}
	if strings.Contains(out, "On-Demand") {
		t.Fatalf("unset on-demand must not invent a row, got:\n%s", out)
	}
}

func TestBuildCursorTileGaugeLines_ContextOnlyDoesNotFakePlan(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	m := tileGaugeTestModel(now)
	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		Metrics: map[string]core.Metric{
			"context_window": {
				Used:      core.Float64Ptr(38.5),
				Remaining: core.Float64Ptr(61.5),
				Limit:     core.Float64Ptr(100),
				Unit:      "%",
			},
		},
	}
	lines := m.buildCursorTileGaugeLines(snap, 56)
	if len(lines) != 0 {
		t.Fatalf("context-only snapshot must not draw a plan overlay, got %v", lines)
	}
}
