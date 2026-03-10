package gemini_cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func emitBreakdownMetrics(prefix string, totals map[string]tokenUsage, daily map[string]map[string]float64, snap *core.UsageSnapshot) {
	entries := sortUsageEntries(totals)
	if len(entries) == 0 {
		return
	}

	for i, entry := range entries {
		if i >= maxBreakdownMetrics {
			break
		}
		keyPrefix := prefix + "_" + sanitizeMetricName(entry.Name)
		setUsageMetric(snap, keyPrefix+"_total_tokens", float64(entry.Data.TotalTokens))
		setUsageMetric(snap, keyPrefix+"_input_tokens", float64(entry.Data.InputTokens))
		setUsageMetric(snap, keyPrefix+"_output_tokens", float64(entry.Data.OutputTokens))

		if entry.Data.CachedInputTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_cached_tokens", float64(entry.Data.CachedInputTokens))
		}
		if entry.Data.ReasoningTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_reasoning_tokens", float64(entry.Data.ReasoningTokens))
		}

		if byDay, ok := daily[entry.Name]; ok {
			seriesKey := "tokens_" + prefix + "_" + sanitizeMetricName(entry.Name)
			snap.DailySeries[seriesKey] = core.SortedTimePoints(byDay)
		}

		if prefix == "model" {
			rec := core.ModelUsageRecord{
				RawModelID:   entry.Name,
				RawSource:    "json",
				Window:       defaultUsageWindowLabel,
				InputTokens:  core.Float64Ptr(float64(entry.Data.InputTokens)),
				OutputTokens: core.Float64Ptr(float64(entry.Data.OutputTokens)),
				TotalTokens:  core.Float64Ptr(float64(entry.Data.TotalTokens)),
			}
			if entry.Data.CachedInputTokens > 0 {
				rec.CachedTokens = core.Float64Ptr(float64(entry.Data.CachedInputTokens))
			}
			if entry.Data.ReasoningTokens > 0 {
				rec.ReasoningTokens = core.Float64Ptr(float64(entry.Data.ReasoningTokens))
			}
			snap.AppendModelUsage(rec)
		}
	}

	snap.Raw[prefix+"_usage"] = formatUsageSummary(entries, maxBreakdownRaw)
}

func emitClientSessionMetrics(clientSessions map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		count int
	}
	var all []entry
	for name, count := range clientSessions {
		if count > 0 {
			all = append(all, entry{name: name, count: count})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})

	for i, item := range all {
		if i >= maxBreakdownMetrics {
			break
		}
		value := float64(item.count)
		snap.Metrics["client_"+sanitizeMetricName(item.name)+"_sessions"] = core.Metric{
			Used:   &value,
			Unit:   "sessions",
			Window: defaultUsageWindowLabel,
		}
	}
}

func emitModelRequestMetrics(modelRequests, modelSessions map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name     string
		requests int
		sessions int
	}

	all := make([]entry, 0, len(modelRequests))
	for name, requests := range modelRequests {
		if requests <= 0 {
			continue
		}
		all = append(all, entry{name: name, requests: requests, sessions: modelSessions[name]})
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].requests == all[j].requests {
			return all[i].name < all[j].name
		}
		return all[i].requests > all[j].requests
	})

	for i, item := range all {
		if i >= maxBreakdownMetrics {
			break
		}
		keyPrefix := "model_" + sanitizeMetricName(item.name)
		req := float64(item.requests)
		sess := float64(item.sessions)
		snap.Metrics[keyPrefix+"_requests"] = core.Metric{
			Used:   &req,
			Unit:   "requests",
			Window: defaultUsageWindowLabel,
		}
		if item.sessions > 0 {
			snap.Metrics[keyPrefix+"_sessions"] = core.Metric{
				Used:   &sess,
				Unit:   "sessions",
				Window: defaultUsageWindowLabel,
			}
		}
	}
}

func emitToolMetrics(toolTotals map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		count int
	}
	var all []entry
	for name, count := range toolTotals {
		if count > 0 {
			all = append(all, entry{name: name, count: count})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})

	var parts []string
	limit := maxBreakdownRaw
	for i, item := range all {
		if i < limit {
			parts = append(parts, fmt.Sprintf("%s (%d)", item.name, item.count))
		}

		val := float64(item.count)
		snap.Metrics["tool_"+sanitizeMetricName(item.name)] = core.Metric{
			Used:   &val,
			Unit:   "calls",
			Window: defaultUsageWindowLabel,
		}
	}

	if len(all) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(all)-limit))
	}

	if len(parts) > 0 {
		snap.Raw["tool_usage"] = strings.Join(parts, ", ")
	}
}

func aggregateTokenTotals(modelTotals map[string]tokenUsage) tokenUsage {
	var total tokenUsage
	for _, usage := range modelTotals {
		total.InputTokens += usage.InputTokens
		total.CachedInputTokens += usage.CachedInputTokens
		total.OutputTokens += usage.OutputTokens
		total.ReasoningTokens += usage.ReasoningTokens
		total.ToolTokens += usage.ToolTokens
		total.TotalTokens += usage.TotalTokens
	}
	return total
}

func setUsageMetric(snap *core.UsageSnapshot, key string, value float64) {
	if value <= 0 {
		return
	}
	snap.Metrics[key] = core.Metric{
		Used:   &value,
		Unit:   "tokens",
		Window: defaultUsageWindowLabel,
	}
}

func addUsage(target map[string]tokenUsage, name string, delta tokenUsage) {
	current := target[name]
	current.InputTokens += delta.InputTokens
	current.CachedInputTokens += delta.CachedInputTokens
	current.OutputTokens += delta.OutputTokens
	current.ReasoningTokens += delta.ReasoningTokens
	current.ToolTokens += delta.ToolTokens
	current.TotalTokens += delta.TotalTokens
	target[name] = current
}

func addDailyUsage(target map[string]map[string]float64, name, day string, value float64) {
	if day == "" || value <= 0 {
		return
	}
	if target[name] == nil {
		target[name] = make(map[string]float64)
	}
	target[name][day] += value
}

func sortUsageEntries(values map[string]tokenUsage) []usageEntry {
	out := make([]usageEntry, 0, len(values))
	for name, data := range values {
		out = append(out, usageEntry{Name: name, Data: data})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Data.TotalTokens == out[j].Data.TotalTokens {
			return out[i].Name < out[j].Name
		}
		return out[i].Data.TotalTokens > out[j].Data.TotalTokens
	})
	return out
}

func formatUsageSummary(entries []usageEntry, max int) string {
	total := 0
	for _, entry := range entries {
		total += entry.Data.TotalTokens
	}
	if total <= 0 {
		return ""
	}

	limit := max
	if limit > len(entries) {
		limit = len(entries)
	}

	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		entry := entries[i]
		pct := float64(entry.Data.TotalTokens) / float64(total) * 100
		parts = append(parts, fmt.Sprintf("%s %s (%.0f%%)", entry.Name, shared.FormatTokenCount(entry.Data.TotalTokens), pct))
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func storeSeries(snap *core.UsageSnapshot, key string, values map[string]float64) {
	if len(values) == 0 {
		return
	}
	snap.DailySeries[key] = core.SortedTimePoints(values)
}

func latestSeriesValue(values map[string]float64) (string, float64) {
	if len(values) == 0 {
		return "", 0
	}
	dates := core.SortedStringKeys(values)
	last := dates[len(dates)-1]
	return last, values[last]
}

func sumLastNDays(values map[string]float64, days int) float64 {
	if len(values) == 0 || days <= 0 {
		return 0
	}
	lastDate, _ := latestSeriesValue(values)
	if lastDate == "" {
		return 0
	}
	end, err := time.Parse("2006-01-02", lastDate)
	if err != nil {
		return 0
	}
	start := end.AddDate(0, 0, -(days - 1))

	total := 0.0
	for date, value := range values {
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}
		if !t.Before(start) && !t.After(end) {
			total += value
		}
	}
	return total
}

func setUsedMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if value <= 0 {
		return
	}
	v := value
	snap.Metrics[key] = core.Metric{
		Used:   &v,
		Unit:   unit,
		Window: window,
	}
}

func setPercentMetric(snap *core.UsageSnapshot, key string, value float64, window string) {
	if value < 0 {
		return
	}
	if value > 100 {
		value = 100
	}
	v := value
	limit := 100.0
	remaining := 100 - value
	snap.Metrics[key] = core.Metric{
		Used:      &v,
		Limit:     &limit,
		Remaining: &remaining,
		Unit:      "%",
		Window:    window,
	}
}
