package core

import "testing"

func TestExtractAnalyticsCostSummary_PrefersModelUsage(t *testing.T) {
	cost := 3.5
	snap := UsageSnapshot{
		ModelUsage: []ModelUsageRecord{
			{RawModelID: "gpt-4.1", CostUSD: &cost},
		},
		Metrics: map[string]Metric{
			"total_cost_usd": {Used: Float64Ptr(1.2)},
			"today_api_cost": {Used: Float64Ptr(0.4)},
			"7d_api_cost":    {Used: Float64Ptr(2.4)},
		},
	}

	got := ExtractAnalyticsCostSummary(snap)
	if got.TotalCostUSD != 3.5 {
		t.Fatalf("total = %v, want 3.5", got.TotalCostUSD)
	}
	if got.TodayCostUSD != 0.4 {
		t.Fatalf("today = %v, want 0.4", got.TodayCostUSD)
	}
	if got.WeekCostUSD != 2.4 {
		t.Fatalf("week = %v, want 2.4", got.WeekCostUSD)
	}
}

func TestExtractAnalyticsCostSummary_FallsBackToMetrics(t *testing.T) {
	snap := UsageSnapshot{
		Metrics: map[string]Metric{
			"credits":        {Used: Float64Ptr(8)},
			"usage_daily":    {Used: Float64Ptr(1.5)},
			"usage_weekly":   {Used: Float64Ptr(6)},
			"total_cost_usd": {Used: Float64Ptr(4)},
		},
	}

	got := ExtractAnalyticsCostSummary(snap)
	if got.TotalCostUSD != 4 {
		t.Fatalf("total = %v, want 4", got.TotalCostUSD)
	}
	if got.TodayCostUSD != 1.5 {
		t.Fatalf("today = %v, want 1.5", got.TodayCostUSD)
	}
	if got.WeekCostUSD != 6 {
		t.Fatalf("week = %v, want 6", got.WeekCostUSD)
	}
}
