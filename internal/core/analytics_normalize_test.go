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
	if len(got.DailySeries["tokens_model_gpt5"]) == 0 {
		t.Fatal("expected canonical tokens_model_gpt5 from usage_model alias")
	}
	if len(got.DailySeries["tokens_gpt_5"]) == 0 {
		t.Fatal("expected normalized model series from ModelUsage")
	}
	if len(got.DailySeries["tokens_model_gpt_5"]) == 0 {
		t.Fatal("expected canonical normalized model series from ModelUsage")
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

func TestNormalizeUsageSnapshotWithConfig_SynthesizesProviderSelfMetrics(t *testing.T) {
	snap := UsageSnapshot{
		ProviderID: "codex",
		AccountID:  "codex-cli",
		Metrics: map[string]Metric{
			"model_gpt_5_codex_input_tokens":  {Used: Float64Ptr(1200), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_codex_output_tokens": {Used: Float64Ptr(300), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_codex_requests":      {Used: Float64Ptr(12), Unit: "requests", Window: "all-time"},
		},
		ModelUsage: []ModelUsageRecord{
			{
				RawModelID:   "gpt-5-codex",
				InputTokens:  Float64Ptr(1200),
				OutputTokens: Float64Ptr(300),
				Requests:     Float64Ptr(12),
			},
		},
	}

	got := NormalizeUsageSnapshotWithConfig(snap, DefaultModelNormalizationConfig())

	if metric, ok := got.Metrics["provider_codex_input_tokens"]; !ok || metric.Used == nil || *metric.Used != 1200 {
		t.Fatalf("provider_codex_input_tokens = %+v", metric)
	}
	if metric, ok := got.Metrics["provider_codex_output_tokens"]; !ok || metric.Used == nil || *metric.Used != 300 {
		t.Fatalf("provider_codex_output_tokens = %+v", metric)
	}
	if metric, ok := got.Metrics["provider_codex_requests"]; !ok || metric.Used == nil || *metric.Used != 12 {
		t.Fatalf("provider_codex_requests = %+v", metric)
	}
}
