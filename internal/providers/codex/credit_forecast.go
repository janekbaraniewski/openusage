package codex

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// creditLimitDetails is the shape returned by the Codex app-server
// individualLimit object. The CLI has returned both numeric JSON values and
// numeric strings across versions, so these fields intentionally stay flexible.
type creditLimitDetails struct {
	Limit              any `json:"limit,omitempty"`
	Used               any `json:"used,omitempty"`
	RemainingPercent   any `json:"remaining_percent,omitempty"`
	RemainingPercentV2 any `json:"remainingPercent,omitempty"`
	UsedPercent        any `json:"used_percent,omitempty"`
	UsedPercentV2      any `json:"usedPercent,omitempty"`
	ResetsAt           any `json:"resets_at,omitempty"`
	ResetsAtV2         any `json:"resetsAt,omitempty"`
	ResetAt            any `json:"reset_at,omitempty"`
	ResetAtV2          any `json:"resetAt,omitempty"`
}

type creditUsageObservation struct {
	at    time.Time
	used  float64
	limit float64
}

func firstCreditLimit(candidates ...*creditLimitDetails) *creditLimitDetails {
	for _, candidate := range candidates {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}

func parseFlexibleNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case nil:
		return 0, false
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func applyCreditLimitDetails(details *creditLimitDetails, snap *core.UsageSnapshot, source string) bool {
	if details == nil || snap == nil {
		return false
	}
	snap.EnsureMaps()

	limit, ok := parseFlexibleNumber(details.Limit)
	if !ok || limit <= 0 {
		return false
	}

	used, hasUsed := parseFlexibleNumber(details.Used)
	if !hasUsed {
		usedPercent, hasUsedPercent := parseFlexibleNumber(details.UsedPercent)
		if !hasUsedPercent {
			usedPercent, hasUsedPercent = parseFlexibleNumber(details.UsedPercentV2)
		}
		if hasUsedPercent {
			used = limit * clampPercent(usedPercent) / 100
		} else {
			remainingPercent, hasRemainingPercent := parseFlexibleNumber(details.RemainingPercent)
			if !hasRemainingPercent {
				remainingPercent, hasRemainingPercent = parseFlexibleNumber(details.RemainingPercentV2)
			}
			if !hasRemainingPercent {
				return false
			}
			used = limit * (100 - clampPercent(remainingPercent)) / 100
		}
	}

	if used < 0 {
		used = 0
	}
	if used > limit {
		used = limit
	}
	remaining := limit - used
	usedPercent := used / limit * 100
	remainingPercent := 100 - usedPercent

	snap.Metrics["codex_credit_limit"] = core.Metric{
		Limit:     &limit,
		Used:      &used,
		Remaining: &remaining,
		Unit:      "credits",
		Window:    "current-period",
	}
	hundred := float64(100)
	snap.Metrics["codex_credit_percent_used"] = core.Metric{
		Limit:     &hundred,
		Used:      &usedPercent,
		Remaining: &remainingPercent,
		Unit:      "%",
		Window:    "current-period",
	}

	resetAt, hasReset := parseFlexibleNumber(details.ResetsAt)
	if !hasReset {
		resetAt, hasReset = parseFlexibleNumber(details.ResetsAtV2)
	}
	if !hasReset {
		resetAt, hasReset = parseFlexibleNumber(details.ResetAt)
	}
	if !hasReset {
		resetAt, hasReset = parseFlexibleNumber(details.ResetAtV2)
	}
	if hasReset && resetAt > 0 {
		snap.Resets["codex_credit_limit"] = time.Unix(int64(resetAt), 0)
	}
	if source != "" {
		snap.Raw["credit_limit_source"] = source
	}
	return true
}

func applyCreditLimitOverride(limitOverride *float64, snap *core.UsageSnapshot) {
	if limitOverride == nil || snap == nil {
		return
	}
	snap.EnsureMaps()
	configured := *limitOverride
	snap.Raw["credit_limit_override_configured"] = strconv.FormatFloat(configured, 'f', -1, 64)
	snap.Raw["credit_limit_override_active"] = "false"
	if configured <= 0 || math.IsNaN(configured) || math.IsInf(configured, 0) {
		snap.Diagnostics["credit_limit_override"] = "ignored invalid personal credit cap"
		return
	}

	metric, ok := snap.Metrics["codex_credit_limit"]
	if !ok || metric.Limit == nil || metric.Used == nil || *metric.Limit <= 0 {
		snap.Diagnostics["credit_limit_override"] = "personal credit cap configured but no reported quota is available"
		return
	}
	reportedLimit := *metric.Limit
	snap.Raw["credit_limit_reported"] = strconv.FormatFloat(reportedLimit, 'f', -1, 64)
	snap.Raw["credit_limit_effective"] = strconv.FormatFloat(reportedLimit, 'f', -1, 64)
	if configured >= reportedLimit {
		return
	}

	reportedMetric := metric
	snap.Metrics["codex_credit_reported_limit"] = reportedMetric
	used := *metric.Used
	remaining := math.Max(configured-used, 0)
	metric.Limit = &configured
	metric.Remaining = &remaining
	snap.Metrics["codex_credit_limit"] = metric

	usedPercent := math.Min(math.Max(used/configured*100, 0), 100)
	remainingPercent := 100 - usedPercent
	hundred := float64(100)
	snap.Metrics["codex_credit_percent_used"] = core.Metric{
		Limit: &hundred, Used: &usedPercent, Remaining: &remainingPercent,
		Unit: "%", Window: metric.Window,
	}
	snap.Raw["credit_limit_override_active"] = "true"
	snap.Raw["credit_limit_effective"] = strconv.FormatFloat(configured, 'f', -1, 64)
}

func (p *Provider) applyCreditForecast(snap *core.UsageSnapshot, accountID string) {
	if p == nil || snap == nil {
		return
	}
	metric, ok := snap.Metrics["codex_credit_limit"]
	if !ok || metric.Limit == nil || metric.Used == nil || *metric.Limit <= 0 {
		return
	}

	limit := *metric.Limit
	used := *metric.Used
	key := accountID
	if key == "" {
		key = snap.AccountID
	}

	// Record every poll before branching. A richer source (account daily
	// history) returns early, and if that source later drops out the
	// observed-usage fallback must already hold samples rather than starting
	// cold and going quiet for two polls.
	history := p.recordCreditObservation(key, creditUsageObservation{at: snap.Timestamp, used: used, limit: limit})

	// Codex exposes the effective individual limit as a monthly quota and
	// gives us the next reset, but not the current period start. When the next
	// reset is available, use the corresponding calendar-month boundary so the
	// rate includes usage that happened before OpenUsage began observing it.
	if resetAt, ok := snap.Resets["codex_credit_limit"]; ok {
		if periodStart, ok := inferCreditPeriodStart(resetAt, snap.Timestamp); ok {
			if daily, ok := buildDailyCreditProjection(snap, periodStart, resetAt); ok {
				remaining := limit - used
				averageDayDuration := resetAt.Sub(periodStart).Hours() / float64(daily.periodDayCount)
				if averageDayDuration > 0 && daily.averageCredits > 0 {
					rate := daily.averageCredits / averageDayDuration
					applyCreditForecastMetrics(snap, rate, remaining, "account daily average")
				}
				applyCreditProjectionMetrics(
					snap,
					daily.projectedCreditsAtReset,
					daily.projectedReserveAtReset,
					daily.averageCredits,
					daily.observedDayCount,
					daily.periodDayCount,
					"account_daily_history",
				)
				snap.Raw["credit_forecast_source"] = "account_daily_history"
				snap.Raw["credit_forecast_period_start"] = periodStart.UTC().Format(time.RFC3339)
				if rateMetric, ok := snap.Metrics["codex_credit_burn_rate"]; ok && rateMetric.Used != nil && *rateMetric.Used > 0 {
					rate := *rateMetric.Used
					if remaining <= 0 {
						snap.Raw["credit_forecast_summary"] = fmt.Sprintf("%.2f credits/hour; 0.00 hours remaining", rate)
					} else {
						snap.Raw["credit_forecast_summary"] = fmt.Sprintf("%.2f credits/hour; %.2f hours remaining", rate, remaining/rate)
					}
				}
				return
			}

			elapsedFraction := float64(snap.Timestamp.Sub(periodStart)) / float64(resetAt.Sub(periodStart))
			if elapsedFraction > 0 {
				elapsedFraction = math.Min(1, elapsedFraction)
				projected := math.Max(0, used/elapsedFraction)
				reserve := math.Min(limit, limit-projected)
				applyCreditProjectionMetrics(snap, projected, reserve, 0, 0, 0, "inferred_period_start")
				snap.Raw["credit_forecast_source"] = "inferred_period_start"
				snap.Raw["credit_forecast_period_start"] = periodStart.UTC().Format(time.RFC3339)
			}

			elapsed := snap.Timestamp.Sub(periodStart)
			if elapsed > time.Minute && used > 0 {
				rate := used / elapsed.Hours()
				if rate > 0 {
					remaining := limit - used
					applyCreditForecastMetrics(snap, rate, remaining, "current-period average")
					snap.Raw["credit_forecast_source"] = "inferred_period_start"
					snap.Raw["credit_forecast_period_start"] = periodStart.UTC().Format(time.RFC3339)
					if remaining <= 0 {
						snap.Raw["credit_forecast_summary"] = fmt.Sprintf("%.2f credits/hour; 0.00 hours remaining", rate)
					} else {
						snap.Raw["credit_forecast_summary"] = fmt.Sprintf("%.2f credits/hour; %.2f hours remaining", rate, remaining/rate)
					}
					return
				}
			}
		}
	}

	if len(history) < 2 {
		return
	}
	first := history[0]
	last := history[len(history)-1]
	duration := last.at.Sub(first.at)
	if duration <= time.Minute || last.used <= first.used {
		return
	}

	rate := (last.used - first.used) / duration.Hours()
	if rate <= 0 {
		return
	}
	applyCreditForecastMetrics(snap, rate, limit-used, "observed")
	snap.Raw["credit_forecast_source"] = "observed_usage"
	snap.Raw["credit_forecast_observation_start"] = first.at.UTC().Format(time.RFC3339)

	remaining := limit - used
	if remaining <= 0 {
		snap.Raw["credit_forecast_summary"] = fmt.Sprintf("%.2f credits/hour; 0.00 hours remaining", rate)
		return
	}
	runout := remaining / rate
	snap.Raw["credit_forecast_summary"] = fmt.Sprintf("%.2f credits/hour; %.2f hours remaining", rate, runout)
}

// inferCreditPeriodStart derives the beginning of the current monthly quota
// period from the next reset returned by Codex. It deliberately returns false
// when the reset is missing, stale, or not safely in the future.
// recordCreditObservation appends this poll's cumulative reading and returns
// the pruned observation window.
func (p *Provider) recordCreditObservation(key string, observation creditUsageObservation) []creditUsageObservation {
	if p == nil {
		return nil
	}
	p.creditHistoryMu.Lock()
	defer p.creditHistoryMu.Unlock()
	if p.creditHistory == nil {
		p.creditHistory = make(map[string][]creditUsageObservation)
	}
	history := p.creditHistory[key]
	if len(history) > 0 {
		last := history[len(history)-1]
		// A lower cumulative total or a changed quota means the period rolled
		// over; the samples either side of that are not comparable.
		if last.limit != observation.limit || observation.used < last.used {
			history = nil
		}
	}
	history = append(history, observation)
	cutoff := observation.at.Add(-6 * time.Hour)
	kept := history[:0]
	for _, sample := range history {
		if !sample.at.Before(cutoff) {
			kept = append(kept, sample)
		}
	}
	if len(kept) > 12 {
		kept = kept[len(kept)-12:]
	}
	p.creditHistory[key] = kept
	return kept
}

func inferCreditPeriodStart(resetAt, observedAt time.Time) (time.Time, bool) {
	resetAt = resetAt.UTC()
	observedAt = observedAt.UTC()
	if resetAt.IsZero() || observedAt.IsZero() || !resetAt.After(observedAt) {
		return time.Time{}, false
	}
	start := resetAt.AddDate(0, -1, 0)
	if !start.Before(observedAt) {
		return time.Time{}, false
	}
	return start, true
}

func applyCreditForecastMetrics(snap *core.UsageSnapshot, rate, remaining float64, window string) {
	snap.Metrics["codex_credit_burn_rate"] = core.Metric{Used: &rate, Unit: "credits/hour", Window: window}
	if remaining <= 0 {
		runout := float64(0)
		snap.Metrics["codex_credit_runout_hours"] = core.Metric{Used: &runout, Unit: "h", Window: "at current rate"}
		return
	}
	runout := remaining / rate
	snap.Metrics["codex_credit_runout_hours"] = core.Metric{Used: &runout, Unit: "h", Window: "at current rate"}
}

type codexDailyCreditProjection struct {
	averageCredits          float64
	observedDayCount        int
	periodDayCount          int
	projectedCreditsAtReset float64
	projectedReserveAtReset float64
}

func buildDailyCreditProjection(snap *core.UsageSnapshot, periodStart, resetAt time.Time) (codexDailyCreditProjection, bool) {
	if snap == nil || snap.DailySeries == nil || len(snap.DailySeries[codexCreditUsageDailySeriesKey]) == 0 {
		return codexDailyCreditProjection{}, false
	}

	location := time.Local
	periodStartDay := startOfCodexDay(periodStart, location)
	periodEndDay := startOfCodexDay(resetAt, location)
	periodDayCount := codexCalendarDayDistance(periodStartDay, periodEndDay)
	if periodDayCount <= 0 {
		return codexDailyCreditProjection{}, false
	}

	currentDay := startOfCodexDay(snap.Timestamp, location)
	if currentDay.Before(periodStartDay) {
		currentDay = periodStartDay
	} else if !currentDay.Before(periodEndDay) {
		currentDay = periodEndDay.AddDate(0, 0, -1)
	}
	observedDayCount := codexCalendarDayDistance(periodStartDay, currentDay) + 1
	if observedDayCount < 1 {
		observedDayCount = 1
	}
	if observedDayCount > periodDayCount {
		observedDayCount = periodDayCount
	}

	byDay := make(map[string]float64, len(snap.DailySeries[codexCreditUsageDailySeriesKey]))
	for _, point := range snap.DailySeries[codexCreditUsageDailySeriesKey] {
		day, err := parseCodexDay(point.Date, location)
		if err != nil || day.Before(periodStartDay) || !day.Before(periodEndDay) {
			continue
		}
		value := point.Value
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			value = 0
		}
		key := formatCodexDay(day)
		byDay[key] += value
	}

	// Keep today's value anchored to the live cumulative quota even when the
	// daily endpoint's response is cached or lags behind the live response.
	historicalCredits := 0.0
	todayKey := formatCodexDay(currentDay)
	for day, value := range byDay {
		if day < todayKey {
			historicalCredits += value
		}
	}
	used := 0.0
	if metric, ok := snap.Metrics["codex_credit_limit"]; ok && metric.Used != nil {
		used = math.Max(0, *metric.Used)
	}
	byDay[todayKey] = math.Max(0, used-historicalCredits)

	observedTotal := 0.0
	for offset := 0; offset < observedDayCount; offset++ {
		day := periodStartDay.AddDate(0, 0, offset)
		observedTotal += math.Max(0, byDay[formatCodexDay(day)])
	}
	averageCredits := observedTotal / float64(observedDayCount)
	projectedCreditsAtReset := math.Max(0, averageCredits*float64(periodDayCount))
	limit := 0.0
	if metric, ok := snap.Metrics["codex_credit_limit"]; ok && metric.Limit != nil {
		limit = math.Max(0, *metric.Limit)
	}
	projectedReserveAtReset := math.Min(limit, limit-projectedCreditsAtReset)

	return codexDailyCreditProjection{
		averageCredits:          averageCredits,
		observedDayCount:        observedDayCount,
		periodDayCount:          periodDayCount,
		projectedCreditsAtReset: projectedCreditsAtReset,
		projectedReserveAtReset: projectedReserveAtReset,
	}, true
}

func codexCalendarDayDistance(start, end time.Time) int {
	startLocal := start.In(time.Local)
	endLocal := end.In(time.Local)
	startDay := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, time.UTC)
	endDay := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, time.UTC)
	return int(endDay.Sub(startDay) / (24 * time.Hour))
}

func applyCreditProjectionMetrics(
	snap *core.UsageSnapshot,
	projectedCreditsAtReset, projectedReserveAtReset, dailyAverageCredits float64,
	observedDayCount, periodDayCount int,
	source string,
) {
	if snap == nil {
		return
	}
	snap.EnsureMaps()
	snap.Metrics["codex_credit_projected_credits_at_reset"] = core.Metric{
		Used:   core.Float64Ptr(projectedCreditsAtReset),
		Unit:   "credits",
		Window: "at reset",
	}
	snap.Metrics["codex_credit_projected_reserve_at_reset"] = core.Metric{
		Used:   core.Float64Ptr(projectedReserveAtReset),
		Unit:   "credits",
		Window: "at reset",
	}
	if dailyAverageCredits > 0 {
		snap.Metrics["codex_credit_daily_average"] = core.Metric{
			Used:   core.Float64Ptr(dailyAverageCredits),
			Unit:   "credits/day",
			Window: "current-period average",
		}
	}
	if observedDayCount > 0 {
		snap.Raw["credit_forecast_observed_days"] = strconv.Itoa(observedDayCount)
	}
	if periodDayCount > 0 {
		snap.Raw["credit_forecast_period_days"] = strconv.Itoa(periodDayCount)
	}
	if source != "" {
		snap.Raw["credit_forecast_projection_source"] = source
	}
}
