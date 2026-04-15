package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func ptr(v float64) *float64 { return &v }

type demoPatternPoint struct {
	DaysAgo int
	Weight  float64
}

func demoPoint(daysAgo int, weight float64) demoPatternPoint {
	return demoPatternPoint{DaysAgo: daysAgo, Weight: weight}
}

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

func demoPatternSeries(now time.Time, peak float64, pattern ...demoPatternPoint) []core.TimePoint {
	if len(pattern) == 0 {
		return nil
	}
	series := make([]core.TimePoint, 0, len(pattern))
	for _, point := range pattern {
		if point.Weight <= 0 {
			continue
		}
		day := now.UTC().AddDate(0, 0, -point.DaysAgo)
		series = append(series, core.TimePoint{
			Date:  day.Format("2006-01-02"),
			Value: roundDemoSeriesValue(peak * point.Weight),
		})
	}
	sort.Slice(series, func(i, j int) bool { return series[i].Date < series[j].Date })
	return series
}

func roundDemoSeriesValue(v float64) float64 {
	switch {
	case v >= 1000:
		return math.Round(v)
	case v >= 100:
		return math.Round(v*10) / 10
	default:
		return math.Round(v*100) / 100
	}
}

var (
	demoPatternClaudeWindow = []demoPatternPoint{
		demoPoint(15, 0.39),
		demoPoint(14, 0.19),
		demoPoint(13, 0.36),
		demoPoint(12, 0.75),
		demoPoint(8, 0.41),
		demoPoint(7, 0.81),
		demoPoint(6, 0.65),
		demoPoint(5, 0.35),
		demoPoint(2, 0.44),
		demoPoint(1, 1.00),
		demoPoint(0, 0.43),
	}
	demoPatternClaudeSupport = []demoPatternPoint{
		demoPoint(15, 0.23),
		demoPoint(14, 0.54),
		demoPoint(13, 0.87),
		demoPoint(12, 1.00),
		demoPoint(8, 0.64),
		demoPoint(7, 0.84),
		demoPoint(6, 0.92),
		demoPoint(5, 0.22),
		demoPoint(2, 0.05),
		demoPoint(1, 0.03),
		demoPoint(0, 0.07),
	}
	demoPatternClaudeLate = []demoPatternPoint{
		demoPoint(2, 0.16),
		demoPoint(1, 1.00),
		demoPoint(0, 0.09),
	}
	demoPatternCodexSparse = []demoPatternPoint{
		demoPoint(6, 0.22),
		demoPoint(1, 1.00),
		demoPoint(0, 0.22),
	}
	demoPatternCursorSpike = []demoPatternPoint{
		demoPoint(8, 0.01),
		demoPoint(7, 1.00),
		demoPoint(6, 0.03),
		demoPoint(5, 0.03),
		demoPoint(1, 0.005),
	}
	demoPatternWorkflow = []demoPatternPoint{
		demoPoint(18, 0.14),
		demoPoint(17, 0.28),
		demoPoint(15, 0.45),
		demoPoint(13, 0.34),
		demoPoint(11, 0.62),
		demoPoint(10, 0.87),
		demoPoint(9, 1.00),
		demoPoint(8, 0.73),
		demoPoint(6, 0.58),
		demoPoint(5, 0.29),
		demoPoint(2, 0.71),
		demoPoint(1, 0.48),
	}
	demoPatternCompact = []demoPatternPoint{
		demoPoint(12, 0.18),
		demoPoint(11, 0.42),
		demoPoint(9, 0.58),
		demoPoint(8, 0.51),
		demoPoint(6, 0.74),
		demoPoint(5, 0.83),
		demoPoint(3, 1.00),
		demoPoint(2, 0.76),
		demoPoint(0, 0.61),
	}
)

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
