package tui

import (
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func hideSnapshotCosts(snap core.UsageSnapshot) core.UsageSnapshot {
	out := snap.DeepClone()
	for key, metric := range out.Metrics {
		if isMonetaryMetric(key, metric) {
			delete(out.Metrics, key)
		}
	}
	for key := range out.DailySeries {
		if isMonetarySeriesKey(key) {
			delete(out.DailySeries, key)
		}
	}
	for i := range out.ModelUsage {
		out.ModelUsage[i].CostUSD = nil
	}
	for key := range out.Raw {
		if isMonetaryKey(key) {
			delete(out.Raw, key)
		}
	}
	for key := range out.Attributes {
		if isMonetaryKey(key) {
			delete(out.Attributes, key)
		}
	}
	return out
}

func isMonetaryMetric(key string, metric core.Metric) bool {
	unit := strings.ToLower(strings.TrimSpace(metric.Unit))
	switch unit {
	case "usd", "$", "cny", "eur", "gbp":
		return true
	}
	return isMonetaryKey(key)
}

func isMonetarySeriesKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	return k == "cost" || strings.Contains(k, "cost") || strings.Contains(k, "spend")
}

func isMonetaryKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, part := range []string{
		"cost",
		"spend",
		"credit",
		"balance",
		"budget",
		"burn_rate",
		"projected_daily",
		"projected_weekly",
		"projected_monthly",
		"reload_amount",
		"reload_trigger",
		"payment_method",
	} {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}
