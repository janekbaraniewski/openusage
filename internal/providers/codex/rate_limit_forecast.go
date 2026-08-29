package codex

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func (p *Provider) applyRateLimitForecast(snap *core.UsageSnapshot) {
	if p == nil || snap == nil {
		return
	}

	metric, ok := snap.Metrics["rate_limit_primary"]
	if !ok || metric.Limit == nil || metric.Used == nil || *metric.Limit <= 0 {
		return
	}

	resetAt, ok := snap.Resets["rate_limit_primary"]
	if !ok {
		return
	}
	periodStart, ok := inferRateLimitPeriodStart(resetAt, snap.Timestamp, metric.Window)
	if !ok {
		return
	}

	used := *metric.Used
	limit := *metric.Limit
	if math.IsNaN(used) || math.IsInf(used, 0) || math.IsNaN(limit) || math.IsInf(limit, 0) {
		return
	}
	used = math.Max(0, math.Min(used, limit))
	elapsed := snap.Timestamp.Sub(periodStart)
	if elapsed <= time.Minute || used <= 0 {
		return
	}

	rate := used / elapsed.Hours()
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return
	}

	remaining := math.Max(limit-used, 0)
	snap.Metrics["codex_rate_limit_burn_rate"] = core.Metric{
		Used:   &rate,
		Unit:   "%/hour",
		Window: "current-period average",
	}
	runout := remaining / rate
	snap.Metrics["codex_rate_limit_runout_hours"] = core.Metric{
		Used:   &runout,
		Unit:   "h",
		Window: "at current rate",
	}

	snap.Raw["rate_limit_forecast_source"] = "inferred_period_start"
	snap.Raw["rate_limit_forecast_period_start"] = periodStart.UTC().Format(time.RFC3339)
	snap.Raw["rate_limit_forecast_summary"] = fmt.Sprintf("%.2f percentage points/hour; %.2f hours remaining", rate, runout)
}

func inferRateLimitPeriodStart(resetAt, observedAt time.Time, window string) (time.Time, bool) {
	resetAt = resetAt.UTC()
	observedAt = observedAt.UTC()
	if resetAt.IsZero() || observedAt.IsZero() || !resetAt.After(observedAt) {
		return time.Time{}, false
	}

	duration, ok := parseRateLimitWindow(window)
	if !ok {
		return time.Time{}, false
	}
	start := resetAt.Add(-duration)
	if !start.Before(observedAt) {
		return time.Time{}, false
	}
	return start, true
}

func parseRateLimitWindow(window string) (time.Duration, bool) {
	window = strings.ToLower(strings.TrimSpace(window))
	window = strings.ReplaceAll(window, " ", "")
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
