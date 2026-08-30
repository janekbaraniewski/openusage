package shared

import (
	"context"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestOverlayLiveFetch_PreservesTelemetryCollections(t *testing.T) {
	base := core.UsageSnapshot{
		ProviderID: "opencode",
		AccountID:  "opencode",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"tokens_total": {Used: core.Float64Ptr(42), Unit: "tokens"},
		},
		ModelUsage: []core.ModelUsageRecord{{RawModelID: "gpt-4", TotalTokens: core.Float64Ptr(42)}},
		DailySeries: map[string][]core.TimePoint{
			"cost": {{Date: "2026-01-01", Value: 1.5}},
		},
	}
	fresh := core.UsageSnapshot{
		ProviderID: "opencode",
		AccountID:  "opencode",
		Status:     core.StatusOK,
		Message:    "live",
		Metrics: map[string]core.Metric{
			"rolling_usage": {Used: core.Float64Ptr(10), Unit: "percent"},
		},
	}

	got := OverlayLiveFetch(base, fresh)
	if len(got.ModelUsage) != 1 {
		t.Fatalf("ModelUsage len = %d, want 1 preserved", len(got.ModelUsage))
	}
	if len(got.DailySeries) != 1 {
		t.Fatalf("DailySeries len = %d, want 1 preserved", len(got.DailySeries))
	}
	if got.Metrics["tokens_total"].Used == nil || *got.Metrics["tokens_total"].Used != 42 {
		t.Fatalf("base metric lost: %+v", got.Metrics["tokens_total"])
	}
	if got.Metrics["rolling_usage"].Used == nil || *got.Metrics["rolling_usage"].Used != 10 {
		t.Fatalf("fresh metric missing: %+v", got.Metrics["rolling_usage"])
	}
	if got.Message != "live" {
		t.Fatalf("message = %q, want live", got.Message)
	}
}

func TestEnrichSnapshotsWithFetch_ReplacesWhenNoMerge(t *testing.T) {
	snaps := map[string]core.UsageSnapshot{
		"cursor-main": {
			ProviderID: "cursor",
			AccountID:  "cursor-main",
			Status:     core.StatusUnknown,
		},
	}
	fetch := func(_ context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
		snap := core.NewUsageSnapshot("cursor", acct.ID)
		snap.Status = core.StatusOK
		snap.Metrics["plan_percent_used"] = core.Metric{Used: core.Float64Ptr(9), Unit: "%"}
		return snap, nil
	}

	EnrichSnapshotsWithFetch(context.Background(), "cursor", fetch, nil, snaps, nil)

	got := snaps["cursor-main"]
	if got.Status != core.StatusOK {
		t.Fatalf("status = %q, want ok", got.Status)
	}
	if got.Metrics["plan_percent_used"].Used == nil || *got.Metrics["plan_percent_used"].Used != 9 {
		t.Fatalf("metric = %+v, want 9", got.Metrics["plan_percent_used"])
	}
}
