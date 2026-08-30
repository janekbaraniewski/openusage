package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestComputeDisplayInfo_CursorListUsesSimpleRemainingSummary(t *testing.T) {
	pct := func(used float64) core.Metric {
		rem := 100 - used
		return core.Metric{Used: core.Float64Ptr(used), Remaining: core.Float64Ptr(rem), Limit: core.Float64Ptr(100), Unit: "%"}
	}
	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		AccountID:  "cursor-nurulz",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"plan_percent_used":      pct(7),
			"plan_auto_percent_used": pct(6),
			"plan_api_percent_used":  pct(29),
			"context_window":         pct(24.5),
		},
		Attributes: map[string]string{"plan_tier": "Pro", "ondemand": "disabled"},
	}

	info := computeDisplayInfo(snap, core.DashboardWidget{}, false, config.UsageModeRemaining)
	if info.summary != "93.00% remaining" {
		t.Fatalf("list summary = %q, want 93.00%% remaining", info.summary)
	}
	if info.gaugePercent != 93 {
		t.Fatalf("gaugePercent = %v, want 93", info.gaugePercent)
	}
	if strings.Contains(info.summary, "Included") || strings.Contains(info.summary, "Auto") {
		t.Fatalf("compact list must not show bucket overlay, got %q", info.summary)
	}
}

func TestRenderListItem_CursorCompactSummary(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.usageMode = config.UsageModeRemaining

	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		AccountID:  "cursor-nurulz",
		Status:     core.StatusOK,
		Timestamp:  now,
		Metrics: map[string]core.Metric{
			"plan_percent_used":      {Used: core.Float64Ptr(15), Remaining: core.Float64Ptr(85)},
			"plan_auto_percent_used": {Used: core.Float64Ptr(12), Remaining: core.Float64Ptr(88)},
			"plan_api_percent_used":  {Used: core.Float64Ptr(34), Remaining: core.Float64Ptr(66)},
		},
	}

	item := m.renderListItem(snap, true, 50)
	if !strings.Contains(item, "85.00% remaining") {
		t.Fatalf("expected simple remaining summary, got:\n%s", item)
	}
	if strings.Contains(item, "Included 15") || strings.Contains(item, "Auto 12") {
		t.Fatalf("list item must not show bucket overlay, got:\n%s", item)
	}
	if !strings.Contains(item, "cursor-nurulz") {
		t.Fatalf("expected account name in list item, got:\n%s", item)
	}
}

func TestRenderDetailContent_CursorShowsPlanBuckets(t *testing.T) {
	pct := func(used float64) core.Metric {
		rem := 100 - used
		return core.Metric{Used: core.Float64Ptr(used), Remaining: core.Float64Ptr(rem), Limit: core.Float64Ptr(100), Unit: "%"}
	}
	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		AccountID:  "cursor-nurulz",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"plan_percent_used":      pct(7),
			"plan_auto_percent_used": pct(6),
			"plan_api_percent_used":  pct(29),
			"context_window":         pct(24.5),
		},
		Resets: map[string]time.Time{
			"plan_percent_used": time.Date(2026, 9, 27, 15, 4, 0, 0, time.UTC),
		},
		Attributes: map[string]string{"plan_tier": "Pro", "ondemand": "disabled"},
	}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	snap.Timestamp = now
	out := RenderDetailContent(snap, now, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeUsed)
	for _, want := range []string{"CURSOR PRO", "Included Used", "Auto Used", "API Used", "On-Demand", "Disabled", "7.00% used", "6.00% used", "29.00% used", "Resets in"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q in:\n%s", want, out)
		}
	}
}
