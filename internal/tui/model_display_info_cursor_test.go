package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestComputeDisplayInfo_CursorListsIncludedAutoAPI(t *testing.T) {
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
	if !strings.Contains(info.summary, "Included") || !strings.Contains(info.summary, "Auto") || !strings.Contains(info.summary, "API") {
		t.Fatalf("split-view summary must show Included/Auto/API, got %q", info.summary)
	}
	if strings.Contains(info.summary, "ctx remaining") {
		t.Fatalf("must not prefer context remaining when plan buckets exist, got %q", info.summary)
	}
	if !strings.Contains(info.summary, "7%") || !strings.Contains(info.summary, "6%") || !strings.Contains(info.summary, "29%") {
		t.Fatalf("summary must include overlay percents, got %q", info.summary)
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
	out := RenderDetailContent(snap, snap.Timestamp, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeUsed)
	for _, want := range []string{"Included", "Auto", "API", "On-Demand", "7% used", "6% used", "29% used", "Resets Sep 27 15:04"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q in:\n%s", want, out)
		}
	}
}
