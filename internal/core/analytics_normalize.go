package core

import (
	"sort"
	"strings"
	"time"
)

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
			mergeSeries(s, "tokens_"+model, points)
		case strings.HasPrefix(key, "usage_model_"):
			model := strings.TrimPrefix(key, "usage_model_")
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
	today := s.Timestamp
	if today.IsZero() {
		today = time.Now()
	}
	todayDate := today.Format("2006-01-02")

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
	today := s.Timestamp
	if today.IsZero() {
		today = time.Now()
	}
	date := today.Format("2006-01-02")

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
		key := "tokens_" + model
		if len(s.DailySeries[key]) > 0 {
			continue
		}
		s.DailySeries[key] = []TimePoint{{Date: date, Value: total}}
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
	keys := make([]string, 0, len(agg))
	for k := range agg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
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
