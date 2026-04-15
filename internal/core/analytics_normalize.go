package core

import (
	"strings"
	"time"
)

func normalizeAnalyticsMetrics(s *UsageSnapshot) {
	if s == nil {
		return
	}
	s.EnsureMaps()
	normalizeAnalyticsCostMetrics(s)
	normalizeAnalyticsBreakdownMetrics(s)
}

func normalizeAnalyticsCostMetrics(s *UsageSnapshot) {
	aliasMetricInto(s, "today_api_cost", "today_cost", "daily_cost_usd", "usage_daily")
	aliasMetricInto(s, "7d_api_cost", "7d_cost", "usage_weekly")
	aliasMetricInto(s, "30d_api_cost", "monthly_cost")
	aliasMetricInto(s, "all_time_api_cost", "total_cost_usd", "billing_total_cost", "composer_cost", "cli_cost", "total_cost")

	if _, ok := s.Metrics["window_cost"]; !ok {
		if metric, ok := bestWindowCostMetric(s); ok {
			s.Metrics["window_cost"] = metric
		}
	}
	if _, ok := s.Metrics["window_tokens"]; !ok {
		if total := sumAnalyticsModelTokens(*s); total > 0 {
			s.Metrics["window_tokens"] = Metric{Used: Float64Ptr(total), Unit: "tokens", Window: inferredAnalyticsWindow(*s)}
		}
	}
	if _, ok := s.Metrics["window_requests"]; !ok {
		if total := sumAnalyticsModelRequests(*s); total > 0 {
			s.Metrics["window_requests"] = Metric{Used: Float64Ptr(total), Unit: "requests", Window: inferredAnalyticsWindow(*s)}
		}
	}
}

func normalizeAnalyticsBreakdownMetrics(s *UsageSnapshot) {
	for key, metric := range s.Metrics {
		switch {
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost"):
			aliasMetricKey(s, key, strings.TrimSuffix(key, "_cost")+"_cost_usd", metric)
		case strings.HasPrefix(key, "provider_") && strings.HasSuffix(key, "_cost") && !strings.HasSuffix(key, "_byok_cost"):
			aliasMetricKey(s, key, strings.TrimSuffix(key, "_cost")+"_cost_usd", metric)
		case strings.HasPrefix(key, "provider_") && strings.HasSuffix(key, "_prompt_tokens"):
			aliasMetricKey(s, key, strings.TrimSuffix(key, "_prompt_tokens")+"_input_tokens", metric)
		case strings.HasPrefix(key, "provider_") && strings.HasSuffix(key, "_completion_tokens"):
			aliasMetricKey(s, key, strings.TrimSuffix(key, "_completion_tokens")+"_output_tokens", metric)
		}
	}

	synthesizeSelfProviderBreakdown(s)
}

func aliasMetricInto(s *UsageSnapshot, canonical string, aliases ...string) {
	if s == nil || canonical == "" {
		return
	}
	if _, exists := s.Metrics[canonical]; exists {
		return
	}
	for _, alias := range aliases {
		if metric, ok := s.Metrics[alias]; ok {
			s.Metrics[canonical] = metric
			return
		}
	}
}

func aliasMetricKey(s *UsageSnapshot, source, target string, metric Metric) {
	if s == nil || source == "" || target == "" {
		return
	}
	if _, exists := s.Metrics[target]; exists {
		return
	}
	s.Metrics[target] = metric
}

func bestWindowCostMetric(s *UsageSnapshot) (Metric, bool) {
	if s == nil {
		return Metric{}, false
	}
	for _, key := range []string{
		"window_cost",
		"today_api_cost",
		"7d_api_cost",
		"30d_api_cost",
		"all_time_api_cost",
		"billing_total_cost",
		"composer_cost",
		"total_cost_usd",
		"total_cost",
		"cli_cost",
		"plan_total_spend_usd",
		"individual_spend",
	} {
		if metric, ok := s.Metrics[key]; ok && metric.Used != nil && *metric.Used > 0 {
			return metric, true
		}
	}
	modelCost := sumAnalyticsModelCost(*s)
	if modelCost > 0 {
		return Metric{Used: Float64Ptr(modelCost), Unit: "USD", Window: inferredAnalyticsWindow(*s)}, true
	}
	return Metric{}, false
}

func synthesizeSelfProviderBreakdown(s *UsageSnapshot) {
	if s == nil {
		return
	}
	if hasAnalyticsProviderMetrics(*s) {
		return
	}

	cost := sumAnalyticsModelCost(*s)
	input := 0.0
	output := 0.0
	requests := 0.0
	for _, rec := range ExtractAnalyticsModelUsage(*s) {
		input += rec.InputTokens
		output += rec.OutputTokens
	}
	for _, rec := range s.ModelUsage {
		if rec.Requests != nil {
			requests += *rec.Requests
		}
	}
	if requests <= 0 {
		if metric, ok := s.Metrics["window_requests"]; ok && metric.Used != nil {
			requests = *metric.Used
		}
	}
	if cost <= 0 && input <= 0 && output <= 0 && requests <= 0 {
		return
	}

	providerKey := sanitizeAnalyticsMetricID(s.ProviderID)
	if providerKey == "" {
		providerKey = "unknown"
	}
	window := inferredAnalyticsWindow(*s)
	if cost > 0 {
		s.Metrics["provider_"+providerKey+"_cost_usd"] = Metric{Used: Float64Ptr(cost), Unit: "USD", Window: window}
	}
	if input > 0 {
		s.Metrics["provider_"+providerKey+"_input_tokens"] = Metric{Used: Float64Ptr(input), Unit: "tokens", Window: window}
	}
	if output > 0 {
		s.Metrics["provider_"+providerKey+"_output_tokens"] = Metric{Used: Float64Ptr(output), Unit: "tokens", Window: window}
	}
	if requests > 0 {
		s.Metrics["provider_"+providerKey+"_requests"] = Metric{Used: Float64Ptr(requests), Unit: "requests", Window: window}
	}
}

func hasAnalyticsProviderMetrics(s UsageSnapshot) bool {
	for key := range s.Metrics {
		if strings.HasPrefix(key, "provider_") {
			return true
		}
	}
	return false
}

func inferredAnalyticsWindow(s UsageSnapshot) string {
	for _, key := range []string{"window_cost", "window_tokens", "window_requests", "today_api_cost", "7d_api_cost", "30d_api_cost", "all_time_api_cost"} {
		if metric, ok := s.Metrics[key]; ok && strings.TrimSpace(metric.Window) != "" {
			return metric.Window
		}
	}
	return "all-time"
}

func sumAnalyticsModelTokens(s UsageSnapshot) float64 {
	total := 0.0
	for _, model := range ExtractAnalyticsModelUsage(s) {
		total += model.InputTokens + model.OutputTokens
	}
	return total
}

func sumAnalyticsModelRequests(s UsageSnapshot) float64 {
	total := 0.0
	for _, rec := range s.ModelUsage {
		if rec.Requests != nil {
			total += *rec.Requests
		}
	}
	return total
}

func sanitizeAnalyticsMetricID(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	replacer := strings.NewReplacer("/", "_", "-", "_", " ", "_", ".", "_", ":", "_")
	value = replacer.Replace(value)
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	value = strings.Trim(value, "_")
	if value == "" {
		return ""
	}
	return value
}

func normalizeAnalyticsDailySeries(s *UsageSnapshot) {
	if s == nil {
		return
	}
	s.EnsureMaps()
	if s.DailySeries == nil {
		s.DailySeries = make(map[string][]TimePoint)
	}

	normalizeExistingSeriesAliases(s)
	synthesizeCoreSeriesFromMetrics(s)
	synthesizeModelSeriesFromRecords(s)

	for key, points := range s.DailySeries {
		s.DailySeries[key] = normalizeSeriesPoints(points)
	}
}

func normalizeExistingSeriesAliases(s *UsageSnapshot) {
	aliasInto(s, "cost", "analytics_cost", "daily_cost")
	aliasInto(s, "tokens_total", "analytics_tokens", "tokens")
	aliasInto(s, "requests", "analytics_requests")

	for key, points := range s.DailySeries {
		switch {
		case strings.HasPrefix(key, "tokens_model_"):
			model := strings.TrimPrefix(key, "tokens_model_")
			mergeSeries(s, "tokens_model_"+model, points)
			mergeSeries(s, "tokens_"+model, points)
		case strings.HasPrefix(key, "usage_model_"):
			model := strings.TrimPrefix(key, "usage_model_")
			mergeSeries(s, "tokens_model_"+model, points)
			mergeSeries(s, "tokens_"+model, points)
		}
	}
}

func aliasInto(s *UsageSnapshot, canonical string, aliases ...string) {
	if len(s.DailySeries[canonical]) > 0 {
		return
	}
	for _, alias := range aliases {
		if len(s.DailySeries[alias]) > 0 {
			s.DailySeries[canonical] = append([]TimePoint(nil), s.DailySeries[alias]...)
			return
		}
	}
}

func synthesizeCoreSeriesFromMetrics(s *UsageSnapshot) {
	todayDate := analyticsReferenceTime(s).Format("2006-01-02")

	metricUsed := func(keys ...string) float64 {
		for _, k := range keys {
			if m, ok := s.Metrics[k]; ok && m.Used != nil && *m.Used > 0 {
				return *m.Used
			}
		}
		return 0
	}

	cost1 := metricUsed("today_api_cost", "daily_cost_usd", "today_cost", "usage_daily")
	tok1 := metricUsed("analytics_tokens")
	req1 := metricUsed("analytics_requests")

	if len(s.DailySeries["cost"]) == 0 {
		if cost1 > 0 {
			s.DailySeries["cost"] = []TimePoint{{Date: todayDate, Value: cost1}}
		}
	}

	if len(s.DailySeries["tokens_total"]) == 0 {
		if tok1 > 0 {
			s.DailySeries["tokens_total"] = []TimePoint{{Date: todayDate, Value: tok1}}
		}
	}

	if len(s.DailySeries["requests"]) == 0 {
		if req1 > 0 {
			s.DailySeries["requests"] = []TimePoint{{Date: todayDate, Value: req1}}
		}
	}
}

func synthesizeModelSeriesFromRecords(s *UsageSnapshot) {
	if len(s.ModelUsage) == 0 {
		return
	}
	date := analyticsReferenceTime(s).Format("2006-01-02")

	perModel := make(map[string]float64)
	for _, rec := range s.ModelUsage {
		model := strings.TrimSpace(rec.RawModelID)
		if model == "" {
			model = strings.TrimSpace(rec.CanonicalLineageID)
		}
		if model == "" {
			continue
		}
		total := float64(0)
		if rec.TotalTokens != nil {
			total += *rec.TotalTokens
		} else {
			if rec.InputTokens != nil {
				total += *rec.InputTokens
			}
			if rec.OutputTokens != nil {
				total += *rec.OutputTokens
			}
		}
		if total <= 0 {
			continue
		}
		perModel[normalizeSeriesModelKey(model)] += total
	}

	for model, total := range perModel {
		legacyKey := "tokens_" + model
		canonicalKey := "tokens_model_" + model
		if len(s.DailySeries[canonicalKey]) == 0 {
			s.DailySeries[canonicalKey] = []TimePoint{{Date: date, Value: total}}
		}
		if len(s.DailySeries[legacyKey]) == 0 {
			s.DailySeries[legacyKey] = []TimePoint{{Date: date, Value: total}}
		}
	}
}

func mergeSeries(s *UsageSnapshot, key string, points []TimePoint) {
	if key == "" || len(points) == 0 {
		return
	}
	s.DailySeries[key] = normalizeSeriesPoints(append(s.DailySeries[key], points...))
}

func normalizeSeriesPoints(points []TimePoint) []TimePoint {
	if len(points) == 0 {
		return nil
	}
	agg := make(map[string]float64, len(points))
	for _, p := range points {
		date := strings.TrimSpace(p.Date)
		if date == "" || p.Value <= 0 {
			continue
		}
		agg[date] += p.Value
	}
	keys := SortedStringKeys(agg)
	out := make([]TimePoint, 0, len(keys))
	for _, k := range keys {
		out = append(out, TimePoint{Date: k, Value: agg[k]})
	}
	return out
}

func normalizeSeriesModelKey(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	model = strings.ReplaceAll(model, "/", "_")
	model = strings.ReplaceAll(model, ":", "_")
	model = strings.ReplaceAll(model, " ", "_")
	model = strings.ReplaceAll(model, ".", "_")
	model = strings.ReplaceAll(model, "-", "_")
	for strings.Contains(model, "__") {
		model = strings.ReplaceAll(model, "__", "_")
	}
	model = strings.Trim(model, "_")
	if model == "" {
		return "unknown"
	}
	return model
}

func analyticsReferenceTime(s *UsageSnapshot) time.Time {
	if s != nil && !s.Timestamp.IsZero() {
		return s.Timestamp.UTC()
	}
	return time.Now().UTC()
}
