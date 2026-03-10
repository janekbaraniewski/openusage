package core

import (
	"sort"
	"strings"

	"github.com/samber/lo"
)

type AnalyticsModelUsageEntry struct {
	Name         string
	CostUSD      float64
	InputTokens  float64
	OutputTokens float64
	Confidence   float64
	Window       string
}

type NamedSeries struct {
	Name   string
	Points []TimePoint
}

func ExtractAnalyticsModelUsage(s UsageSnapshot) []AnalyticsModelUsageEntry {
	records := s.ModelUsage
	if len(records) == 0 {
		records = BuildModelUsageFromSnapshotMetrics(s)
	}
	if len(records) == 0 {
		return nil
	}

	type agg struct {
		cost       float64
		input      float64
		output     float64
		confidence float64
		window     string
	}

	byModel := make(map[string]*agg)
	order := make([]string, 0, len(records))
	ensure := func(name string) *agg {
		if entry, ok := byModel[name]; ok {
			return entry
		}
		entry := &agg{}
		byModel[name] = entry
		order = append(order, name)
		return entry
	}

	for _, rec := range records {
		name := analyticsModelDisplayName(rec)
		if name == "" {
			continue
		}
		entry := ensure(name)
		if rec.CostUSD != nil && *rec.CostUSD > 0 {
			entry.cost += *rec.CostUSD
		}
		if rec.InputTokens != nil {
			entry.input += *rec.InputTokens
		}
		if rec.OutputTokens != nil {
			entry.output += *rec.OutputTokens
		}
		if rec.TotalTokens != nil && rec.InputTokens == nil && rec.OutputTokens == nil {
			entry.input += *rec.TotalTokens
		}
		if rec.Confidence > entry.confidence {
			entry.confidence = rec.Confidence
		}
		if entry.window == "" {
			entry.window = rec.Window
		}
	}

	out := make([]AnalyticsModelUsageEntry, 0, len(order))
	for _, name := range order {
		entry := byModel[name]
		if entry.cost <= 0 && entry.input <= 0 && entry.output <= 0 {
			continue
		}
		out = append(out, AnalyticsModelUsageEntry{
			Name:         name,
			CostUSD:      entry.cost,
			InputTokens:  entry.input,
			OutputTokens: entry.output,
			Confidence:   entry.confidence,
			Window:       entry.window,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ti := out[i].InputTokens + out[i].OutputTokens
		tj := out[j].InputTokens + out[j].OutputTokens
		if ti != tj {
			return ti > tj
		}
		if out[i].CostUSD != out[j].CostUSD {
			return out[i].CostUSD > out[j].CostUSD
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func ExtractAnalyticsModelSeries(series map[string][]TimePoint) []NamedSeries {
	hasTokenSeries := hasAnalyticsTokenSeries(series)
	keys := lo.Filter(SortedStringKeys(series), func(key string, _ int) bool {
		switch {
		case strings.HasPrefix(key, "tokens_"):
			return true
		case strings.HasPrefix(key, "usage_model_"):
			return !hasTokenSeries
		default:
			return false
		}
	})

	out := make([]NamedSeries, 0, len(keys))
	for _, key := range keys {
		name := strings.TrimPrefix(key, "tokens_")
		name = strings.TrimPrefix(name, "usage_model_")
		if name == "" || len(series[key]) == 0 {
			continue
		}
		out = append(out, NamedSeries{Name: name, Points: series[key]})
	}
	return out
}

func SelectAnalyticsWeightSeries(series map[string][]TimePoint) []TimePoint {
	for _, key := range []string{
		"tokens_total",
		"messages",
		"sessions",
		"tool_calls",
		"requests",
		"tab_accepted",
		"composer_accepted",
	} {
		if pts := series[key]; len(pts) > 0 {
			return pts
		}
	}
	for _, named := range ExtractAnalyticsModelSeries(series) {
		if len(named.Points) > 0 {
			return named.Points
		}
	}
	keys := lo.Filter(SortedStringKeys(series), func(key string, _ int) bool {
		return strings.HasPrefix(key, "usage_client_")
	})
	for _, key := range keys {
		if len(series[key]) > 0 {
			return series[key]
		}
	}
	return nil
}

func hasAnalyticsTokenSeries(series map[string][]TimePoint) bool {
	for key, points := range series {
		if strings.HasPrefix(key, "tokens_") && len(points) > 0 {
			return true
		}
	}
	return false
}

func analyticsModelDisplayName(rec ModelUsageRecord) string {
	if rec.Dimensions != nil {
		if groupID := strings.TrimSpace(rec.Dimensions["canonical_group_id"]); groupID != "" {
			return groupID
		}
	}
	if raw := strings.TrimSpace(rec.RawModelID); raw != "" {
		return raw
	}
	if canonical := strings.TrimSpace(rec.CanonicalLineageID); canonical != "" {
		return canonical
	}
	return "unknown"
}
