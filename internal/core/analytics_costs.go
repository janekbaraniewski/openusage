package core

type AnalyticsCostSummary struct {
	TotalCostUSD float64
	TodayCostUSD float64
	WeekCostUSD  float64
	BurnRateUSD  float64
}

func ExtractAnalyticsCostSummary(s UsageSnapshot) AnalyticsCostSummary {
	metricTotal := firstPositiveMetricUsed(s,
		0,
		"window_cost",
		"total_cost_usd",
		"all_time_api_cost",
		"billing_total_cost",
		"composer_cost",
		"total_cost",
		"cli_cost",
		"plan_total_spend_usd",
		"individual_spend",
		"monthly_cost",
	)
	modelTotal := sumAnalyticsModelCost(s)
	total := metricTotal
	if modelTotal > total {
		total = modelTotal
	}

	return AnalyticsCostSummary{
		TotalCostUSD: total,
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
			"7d_cost",
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
