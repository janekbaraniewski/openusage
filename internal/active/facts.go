package active

import (
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

var quotaMetricNames = map[string][]string{
	"antigravity": {"quota"},
	"claude_code": {"usage_five_hour", "usage_seven_day", "rate_limit_primary"},
	// Prefer the native rate-limit metric because it carries the authoritative
	// reset entry. plan_percent_used is a compatibility alias and may not carry
	// that relationship in older snapshots.
	"codex":      {"rate_limit_primary", "plan_percent_used"},
	"cursor":     {"plan_percent_used"},
	"gemini_cli": {"quota"},
	"opencode":   {"monthly_usage_pct", "weekly_usage", "usage_five_hour"},
}

var defaultQuotaMetricNames = []string{"plan_percent_used", "rate_limit_primary", "quota"}

// BuildFacts extracts structured quota facts from a provider snapshot.
func BuildFacts(snap core.UsageSnapshot, now time.Time) Facts {
	var facts Facts

	names := quotaMetricNames[snap.ProviderID]
	if len(names) == 0 {
		names = defaultQuotaMetricNames
	}
	// Quota forecasts are calculated against the window that will run out
	// first. Prefer that same metric so its reset belongs to the forecast we
	// are narrating, rather than pairing (for example) a rolling runout with
	// a monthly reset.
	if forecastMetric := strings.TrimSpace(snap.Attributes["quota_forecast_metric"]); forecastMetric != "" {
		ordered := make([]string, 0, len(names)+1)
		ordered = append(ordered, forecastMetric)
		for _, name := range names {
			if name != forecastMetric {
				ordered = append(ordered, name)
			}
		}
		names = ordered
	}
	for _, name := range names {
		metric, ok := snap.Metrics[name]
		if !ok {
			continue
		}
		used, remaining, ok := quotaPercentages(metric)
		if !ok {
			continue
		}
		facts.PctUsed = used
		facts.PctRemaining = remaining
		if remaining != nil && *remaining <= 0 {
			facts.AtCap = true
		}

		resetKey := strings.TrimSpace(metric.ResetKey)
		if resetKey == "" {
			// Preserve compatibility with older snapshots and providers whose
			// metric and reset keys are intentionally identical. New providers
			// should set ResetKey explicitly when they differ.
			resetKey = name
		}
		if reset, ok := snap.Resets[resetKey]; ok {
			r := reset
			facts.ResetAt = &r
		}
		break
	}

	if forecast, ok := snap.Metrics["quota_runout_hours"]; ok && forecast.Used != nil {
		runout := now.Add(time.Duration(*forecast.Used * float64(time.Hour)))
		facts.RunoutAt = &runout
		facts.ForecastSource = "quota_runout_hours"
		facts.RunoutBeforeReset = facts.ResetAt == nil || runout.Before(*facts.ResetAt)
	}

	if requests, ok := snap.Metrics["requests_today"]; ok && requests.Used != nil {
		facts.RequestsToday = requests.Used
	}
	return facts
}

func quotaPercentages(metric core.Metric) (*float64, *float64, bool) {
	unit := strings.ToLower(strings.TrimSpace(metric.Unit))
	isPercent := unit == "%" || unit == "percent" || unit == "percentage"
	if metric.Limit != nil && *metric.Limit > 0 && (metric.Used != nil || metric.Remaining != nil) {
		limit := *metric.Limit
		var used, remaining float64
		switch {
		case metric.Used != nil:
			used = (*metric.Used / limit) * 100
			remaining = 100 - used
		case metric.Remaining != nil:
			remaining = (*metric.Remaining / limit) * 100
			used = 100 - remaining
		}
		return &used, &remaining, true
	}
	if !isPercent {
		return nil, nil, false
	}
	var used, remaining *float64
	if metric.Used != nil {
		v := *metric.Used
		used = &v
	}
	if metric.Remaining != nil {
		v := *metric.Remaining
		remaining = &v
	}
	if remaining == nil && used != nil {
		v := 100 - *used
		remaining = &v
	}
	if used == nil && remaining != nil {
		v := 100 - *remaining
		used = &v
	}
	return used, remaining, used != nil || remaining != nil
}
