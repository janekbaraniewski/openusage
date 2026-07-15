package codex

import (
	"encoding/json"
	"fmt"
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
	ResetsAt           any `json:"resets_at,omitempty"`
	ResetsAtV2         any `json:"resetsAt,omitempty"`
}

type creditUsageObservation struct {
	at    time.Time
	used  float64
	limit float64
}

func firstCreditLimit(primary, alternate *creditLimitDetails) *creditLimitDetails {
	if primary != nil {
		return primary
	}
	return alternate
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
		remainingPercent, hasRemainingPercent := parseFlexibleNumber(details.RemainingPercent)
		if !hasRemainingPercent {
			remainingPercent, hasRemainingPercent = parseFlexibleNumber(details.RemainingPercentV2)
		}
		if !hasRemainingPercent {
			return false
		}
		used = limit * (100 - clampPercent(remainingPercent)) / 100
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
	if hasReset && resetAt > 0 {
		snap.Resets["codex_credit_limit"] = time.Unix(int64(resetAt), 0)
	}
	if source != "" {
		snap.Raw["credit_limit_source"] = source
	}
	return true
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

	// Codex exposes the effective individual limit as a monthly quota and
	// gives us the next reset, but not the current period start. When the next
	// reset is available, use the corresponding calendar-month boundary so the
	// rate includes usage that happened before OpenUsage began observing it.
	if resetAt, ok := snap.Resets["codex_credit_limit"]; ok {
		if periodStart, ok := inferCreditPeriodStart(resetAt, snap.Timestamp); ok {
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

	p.creditHistoryMu.Lock()
	defer p.creditHistoryMu.Unlock()
	if p.creditHistory == nil {
		p.creditHistory = make(map[string][]creditUsageObservation)
	}

	history := p.creditHistory[key]
	if len(history) > 0 {
		last := history[len(history)-1]
		if last.limit != limit || used < last.used {
			history = nil
		}
	}
	observation := creditUsageObservation{at: snap.Timestamp, used: used, limit: limit}
	history = append(history, observation)
	cutoff := snap.Timestamp.Add(-6 * time.Hour)
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

	if len(kept) < 2 {
		return
	}
	first := kept[0]
	last := kept[len(kept)-1]
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
