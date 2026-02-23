package core

import (
	"testing"
	"time"
)

func TestNormalizeAnalyticsDailySeries_AliasesAndModelSeries(t *testing.T) {
	now := time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC)
	snap := UsageSnapshot{
		ProviderID: "test",
		AccountID:  "acct",
		Timestamp:  now,
		Metrics:    map[string]Metric{},
		DailySeries: map[string][]TimePoint{
			"analytics_cost":   {{Date: "2026-02-20", Value: 1.2}},
			"analytics_tokens": {{Date: "2026-02-20", Value: 100}},
			"usage_model_gpt5": {{Date: "2026-02-20", Value: 50}},
		},
		ModelUsage: []ModelUsageRecord{
			{RawModelID: "gpt-5", TotalTokens: Float64Ptr(300)},
		},
	}

	got := NormalizeUsageSnapshotWithConfig(snap, DefaultModelNormalizationConfig())

	if len(got.DailySeries["cost"]) == 0 {
		t.Fatal("expected canonical cost series")
	}
	if len(got.DailySeries["tokens_total"]) == 0 {
		t.Fatal("expected canonical tokens_total series")
	}
	if len(got.DailySeries["tokens_gpt5"]) == 0 {
		t.Fatal("expected tokens_gpt5 from usage_model alias")
	}
	if len(got.DailySeries["tokens_gpt_5"]) == 0 {
		t.Fatal("expected normalized model series from ModelUsage")
	}
}

func TestNormalizeAnalyticsDailySeries_DoesNotInventDailyFromWindowTotals(t *testing.T) {
	now := time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC)
	snap := UsageSnapshot{
		ProviderID: "test",
		AccountID:  "acct",
		Timestamp:  now,
		Metrics: map[string]Metric{
			"analytics_7d_cost":     {Used: Float64Ptr(70)},
			"analytics_30d_tokens":  {Used: Float64Ptr(3000)},
			"analytics_7d_requests": {Used: Float64Ptr(70)},
		},
	}

	got := NormalizeUsageSnapshotWithConfig(snap, DefaultModelNormalizationConfig())

	if len(got.DailySeries["cost"]) != 0 {
		t.Fatalf("expected no synthesized cost points from window totals, got %d", len(got.DailySeries["cost"]))
	}
	if len(got.DailySeries["tokens_total"]) != 0 {
		t.Fatalf("expected no synthesized token points from window totals, got %d", len(got.DailySeries["tokens_total"]))
	}
	if len(got.DailySeries["requests"]) != 0 {
		t.Fatalf("expected no synthesized request points from window totals, got %d", len(got.DailySeries["requests"]))
	}
}
