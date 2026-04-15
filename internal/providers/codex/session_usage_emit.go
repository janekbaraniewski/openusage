package codex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func emitClientRequestMetrics(clientRequests map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		count int
	}
	var all []entry
	interfaceTotals := make(map[string]float64)
	for name, count := range clientRequests {
		if count > 0 {
			all = append(all, entry{name: name, count: count})
			interfaceTotals[clientInterfaceBucket(name)] += float64(count)
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
		snap.Metrics["client_"+sanitizeMetricName(item.name)+"_requests"] = core.Metric{Used: &value, Unit: "requests", Window: defaultUsageWindowLabel}
	}
	for bucket, value := range interfaceTotals {
		v := value
		snap.Metrics["interface_"+sanitizeMetricName(bucket)] = core.Metric{Used: &v, Unit: "requests", Window: defaultUsageWindowLabel}
	}
}

func clientInterfaceBucket(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lower, "desktop"):
		return "desktop_app"
	case strings.Contains(lower, "cli"), strings.Contains(lower, "exec"), strings.Contains(lower, "terminal"):
		return "cli_agents"
	case strings.Contains(lower, "ide"), strings.Contains(lower, "vscode"), strings.Contains(lower, "editor"):
		return "ide"
	case strings.Contains(lower, "cloud"), strings.Contains(lower, "web"):
		return "cloud_agents"
	case strings.Contains(lower, "human"), strings.Contains(lower, "other"):
		return "human"
	default:
		return sanitizeMetricName(name)
	}
}

func emitToolMetrics(toolCalls map[string]int, callTool map[string]string, callOutcome map[string]int, completedWithoutCallID int, snap *core.UsageSnapshot) {
	var all []countEntry
	totalCalls := 0
	for name, count := range toolCalls {
		if count <= 0 {
			continue
		}
		all = append(all, countEntry{name: name, count: count})
		totalCalls += count
		v := float64(count)
		snap.Metrics["tool_"+sanitizeMetricName(name)] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if totalCalls <= 0 {
		return
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})

	completed := completedWithoutCallID
	errored := 0
	cancelled := 0
	for callID := range callTool {
		switch callOutcome[callID] {
		case 2:
			errored++
		case 3:
			cancelled++
		default:
			completed++
		}
	}
	if completed+errored+cancelled < totalCalls {
		completed += totalCalls - (completed + errored + cancelled)
	}

	totalV := float64(totalCalls)
	snap.Metrics["tool_calls_total"] = core.Metric{Used: &totalV, Unit: "calls", Window: defaultUsageWindowLabel}
	if completed > 0 {
		v := float64(completed)
		snap.Metrics["tool_completed"] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if errored > 0 {
		v := float64(errored)
		snap.Metrics["tool_errored"] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if cancelled > 0 {
		v := float64(cancelled)
		snap.Metrics["tool_cancelled"] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if totalCalls > 0 {
		success := float64(completed) / float64(totalCalls) * 100
		snap.Metrics["tool_success_rate"] = core.Metric{Used: &success, Unit: "%", Window: defaultUsageWindowLabel}
	}
	snap.Raw["tool_usage"] = formatCountSummary(all, maxBreakdownRaw)
}

func emitLanguageMetrics(langRequests map[string]int, snap *core.UsageSnapshot) {
	var all []countEntry
	for language, count := range langRequests {
		if count <= 0 {
			continue
		}
		all = append(all, countEntry{name: language, count: count})
		v := float64(count)
		snap.Metrics["lang_"+sanitizeMetricName(language)] = core.Metric{Used: &v, Unit: "requests", Window: defaultUsageWindowLabel}
	}
	if len(all) == 0 {
		return
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})
	snap.Raw["language_usage"] = formatCountSummary(all, maxBreakdownRaw)
}

func emitProductivityMetrics(stats patchStats, promptCount, commits, totalRequests, requestsToday int, clientSessions map[string]int, snap *core.UsageSnapshot) {
	if totalRequests > 0 {
		v := float64(totalRequests)
		snap.Metrics["total_ai_requests"] = core.Metric{Used: &v, Unit: "requests", Window: defaultUsageWindowLabel}
		snap.Metrics["composer_requests"] = core.Metric{Used: &v, Unit: "requests", Window: defaultUsageWindowLabel}
	}
	if requestsToday > 0 {
		v := float64(requestsToday)
		snap.Metrics["requests_today"] = core.Metric{Used: &v, Unit: "requests", Window: "today"}
		snap.Metrics["today_composer_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "today"}
	}

	totalSessions := 0
	for _, count := range clientSessions {
		totalSessions += count
	}
	if totalSessions > 0 {
		v := float64(totalSessions)
		snap.Metrics["composer_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: defaultUsageWindowLabel}
	}

	if metric, ok := snap.Metrics["context_window"]; ok && metric.Used != nil && metric.Limit != nil && *metric.Limit > 0 {
		pct := *metric.Used / *metric.Limit * 100
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		snap.Metrics["composer_context_pct"] = core.Metric{Used: &pct, Unit: "%", Window: metric.Window}
	}

	if stats.Added > 0 {
		v := float64(stats.Added)
		snap.Metrics["composer_lines_added"] = core.Metric{Used: &v, Unit: "lines", Window: defaultUsageWindowLabel}
	}
	if stats.Removed > 0 {
		v := float64(stats.Removed)
		snap.Metrics["composer_lines_removed"] = core.Metric{Used: &v, Unit: "lines", Window: defaultUsageWindowLabel}
	}
	if filesChanged := len(stats.Files); filesChanged > 0 {
		v := float64(filesChanged)
		snap.Metrics["composer_files_changed"] = core.Metric{Used: &v, Unit: "files", Window: defaultUsageWindowLabel}
		snap.Metrics["ai_tracked_files"] = core.Metric{Used: &v, Unit: "files", Window: defaultUsageWindowLabel}
	}
	if deleted := len(stats.Deleted); deleted > 0 {
		v := float64(deleted)
		snap.Metrics["ai_deleted_files"] = core.Metric{Used: &v, Unit: "files", Window: defaultUsageWindowLabel}
	}
	if commits > 0 {
		v := float64(commits)
		snap.Metrics["scored_commits"] = core.Metric{Used: &v, Unit: "commits", Window: defaultUsageWindowLabel}
	}
	if promptCount > 0 {
		v := float64(promptCount)
		snap.Metrics["total_prompts"] = core.Metric{Used: &v, Unit: "prompts", Window: defaultUsageWindowLabel}
	}
	if stats.PatchCalls > 0 {
		base := totalRequests
		if base < stats.PatchCalls {
			base = stats.PatchCalls
		}
		if base > 0 {
			pct := float64(stats.PatchCalls) / float64(base) * 100
			snap.Metrics["ai_code_percentage"] = core.Metric{Used: &pct, Unit: "%", Window: defaultUsageWindowLabel}
		}
	}
}

func emitDailyUsageSeries(dailyTokenTotals, dailyRequestTotals map[string]float64, interfaceDaily map[string]map[string]float64, snap *core.UsageSnapshot) {
	if len(dailyTokenTotals) > 0 {
		points := core.SortedTimePoints(dailyTokenTotals)
		snap.DailySeries["analytics_tokens"] = points
		snap.DailySeries["tokens_total"] = points
	}
	if len(dailyRequestTotals) > 0 {
		points := core.SortedTimePoints(dailyRequestTotals)
		snap.DailySeries["analytics_requests"] = points
		snap.DailySeries["requests"] = points
	}
	for name, byDay := range interfaceDaily {
		if len(byDay) == 0 {
			continue
		}
		key := sanitizeMetricName(name)
		snap.DailySeries["usage_client_"+key] = core.SortedTimePoints(byDay)
		snap.DailySeries["usage_source_"+key] = core.SortedTimePoints(byDay)
	}
}

func formatCountSummary(entries []countEntry, max int) string {
	if len(entries) == 0 || max <= 0 {
		return ""
	}
	total := 0
	for _, entry := range entries {
		total += entry.count
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
		pct := float64(entries[i].count) / float64(total) * 100
		parts = append(parts, fmt.Sprintf("%s %s (%.0f%%)", entries[i].name, shared.FormatTokenCount(entries[i].count), pct))
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

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
		if entry.Data.ReasoningOutputTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_reasoning_tokens", float64(entry.Data.ReasoningOutputTokens))
		}

		if byDay, ok := daily[entry.Name]; ok {
			series := core.SortedTimePoints(byDay)
			snap.DailySeries["tokens_"+prefix+"_"+sanitizeMetricName(entry.Name)] = series
			snap.DailySeries["usage_"+prefix+"_"+sanitizeMetricName(entry.Name)] = series
		}

		if prefix == "model" {
			rec := core.ModelUsageRecord{
				RawModelID:   entry.Name,
				RawSource:    "jsonl",
				Window:       defaultUsageWindowLabel,
				InputTokens:  core.Float64Ptr(float64(entry.Data.InputTokens)),
				OutputTokens: core.Float64Ptr(float64(entry.Data.OutputTokens)),
				TotalTokens:  core.Float64Ptr(float64(entry.Data.TotalTokens)),
			}
			if entry.Data.CachedInputTokens > 0 {
				rec.CachedTokens = core.Float64Ptr(float64(entry.Data.CachedInputTokens))
			}
			if entry.Data.ReasoningOutputTokens > 0 {
				rec.ReasoningTokens = core.Float64Ptr(float64(entry.Data.ReasoningOutputTokens))
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
		snap.Metrics["client_"+sanitizeMetricName(item.name)+"_sessions"] = core.Metric{Used: &value, Unit: "sessions", Window: defaultUsageWindowLabel}
	}
}

func setUsageMetric(snap *core.UsageSnapshot, key string, value float64) {
	if value <= 0 {
		return
	}
	snap.Metrics[key] = core.Metric{Used: &value, Unit: "tokens", Window: defaultUsageWindowLabel}
}

func addUsage(target map[string]tokenUsage, name string, delta tokenUsage) {
	current := target[name]
	current.InputTokens += delta.InputTokens
	current.CachedInputTokens += delta.CachedInputTokens
	current.OutputTokens += delta.OutputTokens
	current.ReasoningOutputTokens += delta.ReasoningOutputTokens
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

func usageDelta(current, previous tokenUsage) tokenUsage {
	return tokenUsage{
		InputTokens:           current.InputTokens - previous.InputTokens,
		CachedInputTokens:     current.CachedInputTokens - previous.CachedInputTokens,
		OutputTokens:          current.OutputTokens - previous.OutputTokens,
		ReasoningOutputTokens: current.ReasoningOutputTokens - previous.ReasoningOutputTokens,
		TotalTokens:           current.TotalTokens - previous.TotalTokens,
	}
}

func validUsageDelta(delta tokenUsage) bool {
	return delta.InputTokens >= 0 &&
		delta.CachedInputTokens >= 0 &&
		delta.OutputTokens >= 0 &&
		delta.ReasoningOutputTokens >= 0 &&
		delta.TotalTokens >= 0
}
