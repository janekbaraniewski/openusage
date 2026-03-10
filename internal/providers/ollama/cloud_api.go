package ollama

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
)

func (p *Provider) fetchCloudAPI(ctx context.Context, acct core.AccountConfig, apiKey string, snap *core.UsageSnapshot) (hasData, authFailed, limited bool, err error) {
	cloudBaseURL := resolveCloudBaseURL(acct)

	var me map[string]any
	status, headers, reqErr := doJSONRequest(ctx, http.MethodPost, cloudEndpointURL(cloudBaseURL, "/api/me"), apiKey, &me, p.Client())
	if reqErr != nil {
		return false, false, false, fmt.Errorf("ollama: cloud account request failed: %w", reqErr)
	}

	for k, v := range parsers.RedactHeaders(headers, "authorization") {
		if strings.EqualFold(k, "X-Request-Id") {
			snap.Raw["cloud_me_"+normalizeHeaderKey(k)] = v
		}
	}

	switch status {
	case http.StatusOK:
		snap.SetAttribute("auth_type", "api_key")
		if applyCloudUserPayload(me, snap, p.now()) {
			hasData = true
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		authFailed = true
	case http.StatusTooManyRequests:
		limited = true
	default:
		snap.SetDiagnostic("cloud_me_status", fmt.Sprintf("HTTP %d", status))
	}

	var tags tagsResponse
	tagsStatus, _, tagsErr := doJSONRequest(ctx, http.MethodGet, cloudEndpointURL(cloudBaseURL, "/api/tags"), apiKey, &tags, p.Client())
	if tagsErr != nil {
		if !hasData {
			return hasData, authFailed, limited, fmt.Errorf("ollama: cloud tags request failed: %w", tagsErr)
		}
		snap.SetDiagnostic("cloud_tags_error", tagsErr.Error())
		return hasData, authFailed, limited, nil
	}

	switch tagsStatus {
	case http.StatusOK:
		setValueMetric(snap, "cloud_catalog_models", float64(len(tags.Models)), "models", "current")
		hasData = true
	case http.StatusUnauthorized, http.StatusForbidden:
		authFailed = true
	case http.StatusTooManyRequests:
		limited = true
	default:
		snap.SetDiagnostic("cloud_tags_status", fmt.Sprintf("HTTP %d", tagsStatus))
	}

	if _, ok := snap.Metrics["usage_five_hour"]; !ok {
		if parsed, parseErr := fetchCloudUsageFromSettingsPage(ctx, cloudBaseURL, apiKey, acct, snap, p.Client()); parseErr != nil {
			snap.SetDiagnostic("cloud_usage_settings_error", parseErr.Error())
		} else if parsed {
			hasData = true
		}
	}

	return hasData, authFailed, limited, nil
}

func applyCloudUserPayload(payload map[string]any, snap *core.UsageSnapshot, now time.Time) bool {
	if len(payload) == 0 {
		return false
	}

	var hasData bool

	if id := anyStringCaseInsensitive(payload, "id", "ID"); id != "" {
		snap.SetAttribute("account_id", id)
		hasData = true
	}
	if email := anyStringCaseInsensitive(payload, "email", "Email"); email != "" {
		snap.SetAttribute("account_email", email)
		hasData = true
	}
	if name := anyStringCaseInsensitive(payload, "name", "Name"); name != "" {
		snap.SetAttribute("account_name", name)
		hasData = true
	}
	if plan := anyStringCaseInsensitive(payload, "plan", "Plan"); plan != "" {
		snap.SetAttribute("plan_name", plan)
		hasData = true
	}

	if customerID := anyNullStringCaseInsensitive(payload, "customerid", "customer_id", "CustomerID"); customerID != "" {
		snap.SetAttribute("customer_id", customerID)
	}
	if subscriptionID := anyNullStringCaseInsensitive(payload, "subscriptionid", "subscription_id", "SubscriptionID"); subscriptionID != "" {
		snap.SetAttribute("subscription_id", subscriptionID)
	}
	if workOSUserID := anyNullStringCaseInsensitive(payload, "workosuserid", "workos_user_id", "WorkOSUserID"); workOSUserID != "" {
		snap.SetAttribute("workos_user_id", workOSUserID)
	}

	if billingStart, ok := anyNullTimeCaseInsensitive(payload, "subscriptionperiodstart", "subscription_period_start", "SubscriptionPeriodStart"); ok {
		snap.SetAttribute("billing_cycle_start", billingStart.Format(time.RFC3339))
	}
	if billingEnd, ok := anyNullTimeCaseInsensitive(payload, "subscriptionperiodend", "subscription_period_end", "SubscriptionPeriodEnd"); ok {
		snap.SetAttribute("billing_cycle_end", billingEnd.Format(time.RFC3339))
	}

	if extractCloudUsageWindows(payload, snap, now) {
		hasData = true
	}

	return hasData
}

func extractCloudUsageWindows(payload map[string]any, snap *core.UsageSnapshot, now time.Time) bool {
	var found bool

	sessionKeys := []string{
		"session_usage", "sessionusage", "usage_5h", "usagefivehour", "five_hour_usage", "fivehourusage",
	}
	if metric, resetAt, ok := findUsageWindow(payload, sessionKeys, "5h", now); ok {
		snap.Metrics["usage_five_hour"] = metric
		if !resetAt.IsZero() {
			snap.Resets["usage_five_hour"] = resetAt
			snap.SetAttribute("block_end", resetAt.Format(time.RFC3339))
			if metric.Window == "5h" {
				start := resetAt.Add(-5 * time.Hour)
				snap.SetAttribute("block_start", start.Format(time.RFC3339))
			}
		}
		found = true
	}

	dayKeys := []string{
		"weekly_usage", "weeklyusage", "usage_1d", "usageoneday", "one_day_usage", "daily_usage", "dailyusage",
	}
	if metric, resetAt, ok := findUsageWindow(payload, dayKeys, "1d", now); ok {
		snap.Metrics["usage_weekly"] = core.Metric{
			Limit:     metric.Limit,
			Remaining: metric.Remaining,
			Used:      metric.Used,
			Unit:      metric.Unit,
			Window:    "1w",
		}
		snap.Metrics["usage_one_day"] = metric
		if !resetAt.IsZero() {
			snap.Resets["usage_weekly"] = resetAt
			snap.Resets["usage_one_day"] = resetAt
		}
		found = true
	}

	return found
}

func findUsageWindow(payload map[string]any, keys []string, fallbackWindow string, now time.Time) (core.Metric, time.Time, bool) {
	sources := []map[string]any{
		payload,
		anyMapCaseInsensitive(payload, "usage"),
		anyMapCaseInsensitive(payload, "cloud_usage"),
		anyMapCaseInsensitive(payload, "quota"),
	}

	for _, src := range sources {
		if len(src) == 0 {
			continue
		}
		for _, key := range keys {
			v, ok := anyValueCaseInsensitive(src, key)
			if !ok {
				continue
			}
			if metric, resetAt, ok := parseUsageWindowValue(v, fallbackWindow, now); ok {
				return metric, resetAt, true
			}
		}
	}

	return core.Metric{}, time.Time{}, false
}

func parseUsageWindowValue(v any, fallbackWindow string, now time.Time) (core.Metric, time.Time, bool) {
	if pct, ok := anyFloat(v); ok {
		return core.Metric{
			Used:   core.Float64Ptr(pct),
			Unit:   "%",
			Window: fallbackWindow,
		}, time.Time{}, true
	}

	switch raw := v.(type) {
	case string:
		s := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return core.Metric{
				Used:   core.Float64Ptr(f),
				Unit:   "%",
				Window: fallbackWindow,
			}, time.Time{}, true
		}
	case map[string]any:
		var metric core.Metric
		metric.Window = fallbackWindow
		metric.Unit = anyStringCaseInsensitive(raw, "unit")
		if metric.Unit == "" {
			metric.Unit = "%"
		}

		if window := anyStringCaseInsensitive(raw, "window"); window != "" {
			metric.Window = strings.TrimSpace(window)
		}

		if used, ok := anyFloatCaseInsensitive(raw, "used", "usage", "value"); ok {
			metric.Used = core.Float64Ptr(used)
		}
		if limit, ok := anyFloatCaseInsensitive(raw, "limit", "max"); ok {
			metric.Limit = core.Float64Ptr(limit)
		}
		if remaining, ok := anyFloatCaseInsensitive(raw, "remaining", "left"); ok {
			metric.Remaining = core.Float64Ptr(remaining)
		}
		if pct, ok := anyFloatCaseInsensitive(raw, "percent", "pct", "used_percent", "usage_percent"); ok {
			metric.Unit = "%"
			metric.Used = core.Float64Ptr(pct)
			metric.Limit = nil
			metric.Remaining = nil
		}

		var resetAt time.Time
		if resetRaw := anyStringCaseInsensitive(raw, "reset_at", "resets_at", "reset_time", "reset"); resetRaw != "" {
			if t, ok := parseAnyTime(resetRaw); ok {
				resetAt = t
			}
		}
		if resetAt.IsZero() {
			if seconds, ok := anyFloatCaseInsensitive(raw, "reset_in", "reset_in_seconds", "resets_in", "seconds_to_reset"); ok && seconds > 0 {
				resetAt = now.Add(time.Duration(seconds * float64(time.Second)))
			}
		}

		if metric.Used != nil || metric.Limit != nil || metric.Remaining != nil {
			return metric, resetAt, true
		}
	}

	return core.Metric{}, time.Time{}, false
}

func finalizeUsageWindows(snap *core.UsageSnapshot, now time.Time) {
	now = now.In(time.Local)
	blockStart, blockEnd := currentFiveHourBlock(now)

	if _, ok := snap.Metrics["usage_five_hour"]; ok {
		if _, ok := snap.Resets["usage_five_hour"]; !ok {
			snap.Resets["usage_five_hour"] = blockEnd
		}
		if _, ok := snap.Attributes["block_start"]; !ok {
			snap.SetAttribute("block_start", blockStart.Format(time.RFC3339))
		}
		if _, ok := snap.Attributes["block_end"]; !ok {
			snap.SetAttribute("block_end", blockEnd.Format(time.RFC3339))
		}
	}

	hundred := 100.0
	for _, key := range []string{"usage_five_hour", "usage_weekly", "usage_one_day"} {
		if m, ok := snap.Metrics[key]; ok && m.Unit == "%" && m.Limit == nil {
			m.Limit = core.Float64Ptr(hundred)
			if m.Used != nil && m.Remaining == nil {
				rem := hundred - *m.Used
				m.Remaining = core.Float64Ptr(rem)
			}
			snap.Metrics[key] = m
		}
	}
}

func currentFiveHourBlock(now time.Time) (time.Time, time.Time) {
	startHour := (now.Hour() / 5) * 5
	start := time.Date(now.Year(), now.Month(), now.Day(), startHour, 0, 0, 0, now.Location())
	end := start.Add(5 * time.Hour)
	return start, end
}

func resolveCloudBaseURL(acct core.AccountConfig) string {
	normalize := func(raw string) string {
		raw = strings.TrimSpace(strings.TrimRight(raw, "/"))
		if raw == "" {
			return ""
		}
		u, err := url.Parse(raw)
		if err != nil {
			return raw
		}
		switch strings.TrimSpace(strings.ToLower(u.Path)) {
		case "", "/":
			u.Path = ""
		case "/api", "/api/v1":
			u.Path = ""
		}
		u.RawQuery = ""
		u.Fragment = ""
		return strings.TrimRight(u.String(), "/")
	}

	if v := strings.TrimSpace(acct.Hint("cloud_base_url", "")); v != "" {
		return normalize(v)
	}
	if strings.HasPrefix(strings.ToLower(acct.BaseURL), "https://") && strings.Contains(strings.ToLower(acct.BaseURL), "ollama.com") {
		return normalize(acct.BaseURL)
	}
	return normalize(defaultCloudBaseURL)
}
