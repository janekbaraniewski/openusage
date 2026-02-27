package main

import (
	"fmt"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func ptr(v float64) *float64 { return &v }

func demoSeries(now time.Time, values ...float64) []core.TimePoint {
	if len(values) == 0 {
		return nil
	}
	series := make([]core.TimePoint, 0, len(values))
	start := now.UTC().AddDate(0, 0, -(len(values) - 1))
	for i, value := range values {
		day := start.AddDate(0, 0, i)
		series = append(series, core.TimePoint{
			Date:  day.Format("2006-01-02"),
			Value: value,
		})
	}
	return series
}

func demoMessageForSnapshot(snap core.UsageSnapshot) string {
	switch snap.ProviderID {
	case "openrouter":
		if remaining, ok := metricRemaining(snap.Metrics, "credit_balance"); ok {
			return fmt.Sprintf("$%.2f credits remaining", remaining)
		}
	case "cursor":
		spend, spendOK := metricUsed(snap.Metrics, "plan_spend")
		remaining, remainingOK := metricRemaining(snap.Metrics, "spend_limit")
		limit, limitOK := metricLimit(snap.Metrics, "spend_limit")
		if spendOK && remainingOK && limitOK {
			return fmt.Sprintf("Team — $%.2f / $%.0f team spend ($%.2f remaining)", spend, limit, remaining)
		}
	}

	return snap.Message
}

func metricUsed(metrics map[string]core.Metric, key string) (float64, bool) {
	metric, ok := metrics[key]
	if !ok || metric.Used == nil {
		return 0, false
	}
	return *metric.Used, true
}

func metricLimit(metrics map[string]core.Metric, key string) (float64, bool) {
	metric, ok := metrics[key]
	if !ok || metric.Limit == nil {
		return 0, false
	}
	return *metric.Limit, true
}

func metricRemaining(metrics map[string]core.Metric, key string) (float64, bool) {
	metric, ok := metrics[key]
	if !ok || metric.Remaining == nil {
		return 0, false
	}
	return *metric.Remaining, true
}
