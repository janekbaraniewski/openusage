package core

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type quotaForecastCandidate struct {
	metricKey   string
	metric      Metric
	periodStart time.Time
	resetAt     time.Time
	rate        float64
	runout      float64
}

// ApplyQuotaForecast derives provider-neutral quota forecast metrics when a
// provider reports a finite quota, a fixed window, and its reset time. A
// provider only needs to emit a normal Metric and a matching Resets entry; no
// provider-specific overlay code is required.
func ApplyQuotaForecast(snap *UsageSnapshot) {
	if snap == nil {
		return
	}
	snap.EnsureMaps()

	var best quotaForecastCandidate
	found := false
	for _, metricKey := range SortedStringKeys(snap.Metrics) {
		metric := snap.Metrics[metricKey]
		if metric.Limit == nil || metric.Used == nil || *metric.Limit <= 0 || metric.Unit == "" {
			continue
		}
		limit := *metric.Limit
		used := *metric.Used
		if math.IsNaN(limit) || math.IsInf(limit, 0) || math.IsNaN(used) || math.IsInf(used, 0) {
			continue
		}
		used = math.Max(0, math.Min(used, limit))
		if used <= 0 {
			continue
		}

		resetAt, ok := resetForMetric(snap.Resets, metricKey)
		if !ok {
			continue
		}
		periodStart, ok := quotaPeriodStart(snap, metricKey, metric.Window, resetAt)
		if !ok {
			continue
		}
		elapsed := snap.Timestamp.Sub(periodStart)
		if elapsed <= time.Minute {
			continue
		}
		rate := used / elapsed.Hours()
		if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			continue
		}
		runout := math.Max(limit-used, 0) / rate
		candidate := quotaForecastCandidate{
			metricKey:   metricKey,
			metric:      metric,
			periodStart: periodStart,
			resetAt:     resetAt,
			rate:        rate,
			runout:      runout,
		}
		if !found || candidate.runout < best.runout {
			best = candidate
			found = true
		}
	}

	if !found {
		return
	}

	rate := best.rate
	snap.Metrics["quota_burn_rate"] = Metric{
		Used:   &rate,
		Unit:   quotaRateUnit(best.metric.Unit),
		Window: "current-period average",
	}
	runout := best.runout
	snap.Metrics["quota_runout_hours"] = Metric{
		Used:   &runout,
		Unit:   "h",
		Window: "at current rate",
	}
	snap.Raw["quota_forecast_source"] = "inferred_period_start"
	snap.Raw["quota_forecast_metric"] = best.metricKey
	snap.Raw["quota_forecast_period_start"] = best.periodStart.UTC().Format(time.RFC3339)
	snap.Raw["quota_forecast_reset_at"] = best.resetAt.UTC().Format(time.RFC3339)
	snap.Raw["quota_forecast_summary"] = fmt.Sprintf("%.2f %s/hour; %.2f hours remaining", best.rate, best.metric.Unit, best.runout)
}

func resetForMetric(resets map[string]time.Time, metricKey string) (time.Time, bool) {
	keys := []string{metricKey, metricKey + "_reset", metricKey + "_end"}
	if strings.HasSuffix(metricKey, "_progress") {
		keys = append(keys, strings.TrimSuffix(metricKey, "_progress")+"_end")
	}
	for _, key := range keys {
		if resetAt, ok := resets[key]; ok && !resetAt.IsZero() {
			return resetAt, true
		}
	}
	return time.Time{}, false
}

func quotaPeriodStart(snap *UsageSnapshot, metricKey, window string, resetAt time.Time) (time.Time, bool) {
	resetAt = resetAt.UTC()
	observedAt := snap.Timestamp.UTC()
	if observedAt.IsZero() || !resetAt.After(observedAt) {
		return time.Time{}, false
	}

	for _, key := range []string{
		metricKey + "_period_start",
		metricKey + "_start",
		"quota_period_start",
		"billing_cycle_start",
		"period_start",
	} {
		if start, ok := parseQuotaTime(snap.Raw[key]); ok && start.Before(observedAt) {
			return start, true
		}
	}

	duration, ok := parseQuotaWindow(window)
	if !ok {
		return time.Time{}, false
	}
	start := resetAt.Add(-duration)
	if !start.Before(observedAt) {
		return time.Time{}, false
	}
	return start, true
}

func parseQuotaTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func parseQuotaWindow(window string) (time.Duration, bool) {
	window = strings.ToLower(strings.TrimSpace(window))
	window = strings.ReplaceAll(window, " ", "")
	window = strings.TrimPrefix(window, "~")
	for _, prefix := range []string{"rolling-", "rolling_", "window-", "window_"} {
		window = strings.TrimPrefix(window, prefix)
	}
	if window == "" {
		return 0, false
	}

	var total time.Duration
	for len(window) > 0 {
		numericEnd := 0
		for numericEnd < len(window) && ((window[numericEnd] >= '0' && window[numericEnd] <= '9') || window[numericEnd] == '.') {
			numericEnd++
		}
		if numericEnd == 0 || numericEnd == len(window) {
			return 0, false
		}
		amount, err := strconv.ParseFloat(window[:numericEnd], 64)
		if err != nil || amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
			return 0, false
		}

		var unit time.Duration
		switch window[numericEnd] {
		case 's':
			unit = time.Second
		case 'm':
			unit = time.Minute
		case 'h':
			unit = time.Hour
		case 'd':
			unit = 24 * time.Hour
		case 'w':
			unit = 7 * 24 * time.Hour
		default:
			return 0, false
		}
		total += time.Duration(amount * float64(unit))
		window = window[numericEnd+1:]
	}

	return total, total > 0
}

func quotaRateUnit(unit string) string {
	if unit == "%" || strings.EqualFold(unit, "percent") || strings.EqualFold(unit, "percentage") {
		return "%/hour"
	}
	return strings.TrimSuffix(unit, "/hour") + "/hour"
}
