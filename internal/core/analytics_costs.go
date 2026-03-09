package core

type AnalyticsCostSummary struct {
	TotalCostUSD float64
	TodayCostUSD float64
	WeekCostUSD  float64
	BurnRateUSD  float64
}

func ExtractAnalyticsCostSummary(s UsageSnapshot) AnalyticsCostSummary {
	return AnalyticsCostSummary{
		TotalCostUSD: firstPositiveMetricUsed(s,
			sumAnalyticsModelCost(s),
			"total_cost_usd",
			"plan_total_spend_usd",
			"all_time_api_cost",
			"jsonl_total_cost_usd",
			"today_api_cost",
			"daily_cost_usd",
			"5h_block_cost",
			"block_cost_usd",
			"individual_spend",
			"credits",
		),
		TodayCostUSD: firstPositiveMetricUsed(s,
			0,
			"today_api_cost",
			"daily_cost_usd",
			"today_cost",
			"usage_daily",
		),
		WeekCostUSD: firstPositiveMetricUsed(s,
			0,
			"7d_api_cost",
			"usage_weekly",
		),
		BurnRateUSD: firstPositiveMetricUsed(s,
			0,
			"burn_rate",
		),
	}
}

func sumAnalyticsModelCost(s UsageSnapshot) float64 {
	total := 0.0
	for _, model := range ExtractAnalyticsModelUsage(s) {
		total += model.CostUSD
	}
	return total
}

func firstPositiveMetricUsed(s UsageSnapshot, fallback float64, keys ...string) float64 {
	if fallback > 0 {
		return fallback
	}
	for _, key := range keys {
		if metric, ok := s.Metrics[key]; ok && metric.Used != nil && *metric.Used > 0 {
			return *metric.Used
		}
	}
	return 0
}
