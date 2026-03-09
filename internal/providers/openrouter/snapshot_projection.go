package openrouter

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func emitPerModelMetrics(modelStatsMap map[string]*modelStats, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		stats *modelStats
	}
	sorted := make([]entry, 0, len(modelStatsMap))
	for name, stats := range modelStatsMap {
		sorted = append(sorted, entry{name, stats})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].stats.TotalCost > sorted[j].stats.TotalCost
	})

	for _, entry := range sorted {
		safeName := sanitizeName(entry.name)
		prefix := "model_" + safeName
		rec := core.ModelUsageRecord{RawModelID: entry.name, RawSource: "api", Window: "30d"}

		inputTokens := float64(entry.stats.PromptTokens)
		snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &inputTokens, Unit: "tokens", Window: "30d"}
		rec.InputTokens = core.Float64Ptr(inputTokens)

		outputTokens := float64(entry.stats.CompletionTokens)
		snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &outputTokens, Unit: "tokens", Window: "30d"}
		rec.OutputTokens = core.Float64Ptr(outputTokens)

		if entry.stats.ReasoningTokens > 0 {
			reasoningTokens := float64(entry.stats.ReasoningTokens)
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &reasoningTokens, Unit: "tokens", Window: "30d"}
			rec.ReasoningTokens = core.Float64Ptr(reasoningTokens)
		}
		if entry.stats.CachedTokens > 0 {
			cachedTokens := float64(entry.stats.CachedTokens)
			snap.Metrics[prefix+"_cached_tokens"] = core.Metric{Used: &cachedTokens, Unit: "tokens", Window: "30d"}
			rec.CachedTokens = core.Float64Ptr(cachedTokens)
		}
		totalTokens := float64(entry.stats.PromptTokens + entry.stats.CompletionTokens + entry.stats.ReasoningTokens + entry.stats.CachedTokens)
		if totalTokens > 0 {
			snap.Metrics[prefix+"_total_tokens"] = core.Metric{Used: &totalTokens, Unit: "tokens", Window: "30d"}
			rec.TotalTokens = core.Float64Ptr(totalTokens)
		}
		if entry.stats.ImageTokens > 0 {
			imageTokens := float64(entry.stats.ImageTokens)
			snap.Metrics[prefix+"_image_tokens"] = core.Metric{Used: &imageTokens, Unit: "tokens", Window: "30d"}
		}

		costUSD := entry.stats.TotalCost
		snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &costUSD, Unit: "USD", Window: "30d"}
		rec.CostUSD = core.Float64Ptr(costUSD)
		requests := float64(entry.stats.Requests)
		snap.Metrics[prefix+"_requests"] = core.Metric{Used: &requests, Unit: "requests", Window: "30d"}
		rec.Requests = core.Float64Ptr(requests)
		if entry.stats.NativePrompt > 0 {
			nativeInput := float64(entry.stats.NativePrompt)
			snap.Metrics[prefix+"_native_input_tokens"] = core.Metric{Used: &nativeInput, Unit: "tokens", Window: "30d"}
		}
		if entry.stats.NativeCompletion > 0 {
			nativeOutput := float64(entry.stats.NativeCompletion)
			snap.Metrics[prefix+"_native_output_tokens"] = core.Metric{Used: &nativeOutput, Unit: "tokens", Window: "30d"}
		}

		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%d", entry.stats.Requests)

		if entry.stats.LatencyCount > 0 {
			avgMs := float64(entry.stats.TotalLatencyMs) / float64(entry.stats.LatencyCount)
			snap.Raw[prefix+"_avg_latency_ms"] = fmt.Sprintf("%.0f", avgMs)
			avgSeconds := avgMs / 1000.0
			snap.Metrics[prefix+"_avg_latency"] = core.Metric{Used: &avgSeconds, Unit: "seconds", Window: "30d"}
		}
		if entry.stats.GenerationCount > 0 {
			avgMs := float64(entry.stats.TotalGenMs) / float64(entry.stats.GenerationCount)
			avgSeconds := avgMs / 1000.0
			snap.Metrics[prefix+"_avg_generation_time"] = core.Metric{Used: &avgSeconds, Unit: "seconds", Window: "30d"}
		}
		if entry.stats.ModerationCount > 0 {
			avgMs := float64(entry.stats.TotalModeration) / float64(entry.stats.ModerationCount)
			avgSeconds := avgMs / 1000.0
			snap.Metrics[prefix+"_avg_moderation_latency"] = core.Metric{Used: &avgSeconds, Unit: "seconds", Window: "30d"}
		}

		if entry.stats.CacheDiscountUSD > 0 {
			snap.Raw[prefix+"_cache_savings"] = fmt.Sprintf("$%.6f", entry.stats.CacheDiscountUSD)
		}

		if len(entry.stats.Providers) > 0 {
			var provList []string
			for prov := range entry.stats.Providers {
				provList = append(provList, prov)
			}
			sort.Strings(provList)
			snap.Raw[prefix+"_providers"] = strings.Join(provList, ", ")
			if len(provList) > 0 {
				rec.SetDimension("upstream_providers", strings.Join(provList, ","))
			}
		}
		if rec.InputTokens != nil || rec.OutputTokens != nil || rec.CostUSD != nil || rec.Requests != nil || rec.ReasoningTokens != nil || rec.CachedTokens != nil {
			snap.AppendModelUsage(rec)
		}
	}
}

func emitPerProviderMetrics(providerStatsMap map[string]*providerStats, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		stats *providerStats
	}
	sorted := make([]entry, 0, len(providerStatsMap))
	for name, stats := range providerStatsMap {
		sorted = append(sorted, entry{name, stats})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].stats.TotalCost > sorted[j].stats.TotalCost
	})

	for _, entry := range sorted {
		prefix := "provider_" + sanitizeName(strings.ToLower(entry.name))
		requests := float64(entry.stats.Requests)
		snap.Metrics[prefix+"_requests"] = core.Metric{Used: &requests, Unit: "requests", Window: "30d"}
		if entry.stats.TotalCost > 0 {
			v := entry.stats.TotalCost
			snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: "30d"}
		}
		if entry.stats.ByokCost > 0 {
			v := entry.stats.ByokCost
			snap.Metrics[prefix+"_byok_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "30d"}
		}
		if entry.stats.PromptTokens > 0 {
			v := float64(entry.stats.PromptTokens)
			snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "30d"}
		}
		if entry.stats.CompletionTokens > 0 {
			v := float64(entry.stats.CompletionTokens)
			snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "30d"}
		}
		if entry.stats.ReasoningTokens > 0 {
			v := float64(entry.stats.ReasoningTokens)
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "30d"}
		}
		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%d", entry.stats.Requests)
		snap.Raw[prefix+"_cost"] = fmt.Sprintf("$%.6f", entry.stats.TotalCost)
		if entry.stats.ByokCost > 0 {
			snap.Raw[prefix+"_byok_cost"] = fmt.Sprintf("$%.6f", entry.stats.ByokCost)
		}
		snap.Raw[prefix+"_prompt_tokens"] = fmt.Sprintf("%d", entry.stats.PromptTokens)
		snap.Raw[prefix+"_completion_tokens"] = fmt.Sprintf("%d", entry.stats.CompletionTokens)
		if entry.stats.ReasoningTokens > 0 {
			snap.Raw[prefix+"_reasoning_tokens"] = fmt.Sprintf("%d", entry.stats.ReasoningTokens)
		}
	}
}

func emitClientDailySeries(snap *core.UsageSnapshot, tokensByClient, requestsByClient map[string]map[string]float64) {
	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	for client, byDate := range tokensByClient {
		if client == "" || len(byDate) == 0 {
			continue
		}
		snap.DailySeries["tokens_client_"+client] = core.SortedTimePoints(byDate)
	}
	for client, byDate := range requestsByClient {
		if client == "" || len(byDate) == 0 {
			continue
		}
		snap.DailySeries["usage_client_"+client] = core.SortedTimePoints(byDate)
	}
}

type providerClientAggregate struct {
	InputTokens     float64
	OutputTokens    float64
	ReasoningTokens float64
	Requests        float64
	CostUSD         float64
	Window          string
}

type modelUsageCount struct {
	name  string
	count float64
}

func enrichDashboardRepresentations(snap *core.UsageSnapshot) {
	if snap == nil || len(snap.Metrics) == 0 {
		return
	}
	synthesizeClientMetricsFromProviderMetrics(snap)
	synthesizeLanguageMetricsFromModelRequests(snap)
	synthesizeUsageSummaries(snap)
}

func synthesizeClientMetricsFromProviderMetrics(snap *core.UsageSnapshot) {
	byClient := make(map[string]*providerClientAggregate)
	for key, metric := range snap.Metrics {
		if metric.Used == nil {
			continue
		}
		client, field, ok := parseProviderMetricKey(key)
		if !ok || client == "" {
			continue
		}
		agg := byClient[client]
		if agg == nil {
			agg = &providerClientAggregate{}
			byClient[client] = agg
		}
		if agg.Window == "" && metric.Window != "" {
			agg.Window = metric.Window
		}
		switch field {
		case "input_tokens":
			agg.InputTokens = *metric.Used
		case "output_tokens":
			agg.OutputTokens = *metric.Used
		case "reasoning_tokens":
			agg.ReasoningTokens = *metric.Used
		case "requests":
			agg.Requests = *metric.Used
		case "cost_usd":
			agg.CostUSD = *metric.Used
		}
	}

	for client, agg := range byClient {
		window := strings.TrimSpace(agg.Window)
		if window == "" {
			window = "30d"
		}
		clientPrefix := "client_" + client

		if agg.InputTokens > 0 {
			v := agg.InputTokens
			snap.Metrics[clientPrefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: window}
		}
		if agg.OutputTokens > 0 {
			v := agg.OutputTokens
			snap.Metrics[clientPrefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: window}
		}
		if agg.ReasoningTokens > 0 {
			v := agg.ReasoningTokens
			snap.Metrics[clientPrefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: window}
		}
		totalTokens := agg.InputTokens + agg.OutputTokens + agg.ReasoningTokens
		if totalTokens > 0 {
			v := totalTokens
			snap.Metrics[clientPrefix+"_total_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: window}
		}
		if agg.Requests > 0 {
			v := agg.Requests
			snap.Metrics[clientPrefix+"_requests"] = core.Metric{Used: &v, Unit: "requests", Window: window}
		}
		if agg.CostUSD > 0 {
			v := agg.CostUSD
			snap.Metrics[clientPrefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: window}
		}
	}
}

func parseProviderMetricKey(key string) (name, field string, ok bool) {
	const prefix = "provider_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	for _, suffix := range []string{"_input_tokens", "_output_tokens", "_reasoning_tokens", "_requests", "_cost_usd"} {
		if strings.HasSuffix(rest, suffix) {
			return strings.TrimSuffix(rest, suffix), strings.TrimPrefix(suffix, "_"), true
		}
	}
	return "", "", false
}

func synthesizeLanguageMetricsFromModelRequests(snap *core.UsageSnapshot) {
	byLanguage := make(map[string]float64)
	window := ""
	for key, metric := range snap.Metrics {
		if metric.Used == nil {
			continue
		}
		model, field, ok := parseModelMetricKey(key)
		if !ok || field != "requests" {
			continue
		}
		if window == "" && strings.TrimSpace(metric.Window) != "" {
			window = strings.TrimSpace(metric.Window)
		}
		lang := inferModelWorkloadLanguage(model)
		byLanguage[lang] += *metric.Used
	}
	if len(byLanguage) == 0 {
		return
	}
	if window == "" {
		window = "30d inferred"
	}
	for lang, count := range byLanguage {
		if count <= 0 {
			continue
		}
		v := count
		snap.Metrics["lang_"+sanitizeName(lang)] = core.Metric{Used: &v, Unit: "requests", Window: window}
	}
	if summary := summarizeCountUsage(byLanguage, "req", 6); summary != "" {
		snap.Raw["language_usage"] = summary
		snap.Raw["language_usage_source"] = "inferred_from_model_ids"
	}
}

func parseModelMetricKey(key string) (name, field string, ok bool) {
	const prefix = "model_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	if strings.HasSuffix(rest, "_requests") {
		return strings.TrimSuffix(rest, "_requests"), "requests", true
	}
	return "", "", false
}

func inferModelWorkloadLanguage(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return "general"
	}
	switch {
	case strings.Contains(model, "coder"), strings.Contains(model, "codestral"), strings.Contains(model, "devstral"), strings.Contains(model, "code"):
		return "code"
	case strings.Contains(model, "vision"), strings.Contains(model, "image"), strings.Contains(model, "multimodal"), strings.Contains(model, "omni"), strings.Contains(model, "vl"):
		return "multimodal"
	case strings.Contains(model, "audio"), strings.Contains(model, "speech"), strings.Contains(model, "voice"), strings.Contains(model, "whisper"), strings.Contains(model, "tts"), strings.Contains(model, "stt"):
		return "audio"
	case strings.Contains(model, "reason"), strings.Contains(model, "thinking"):
		return "reasoning"
	default:
		return "general"
	}
}

func synthesizeUsageSummaries(snap *core.UsageSnapshot) {
	modelTotals := make(map[string]float64)
	modelWindow := ""
	modelUnit := "tok"
	for key, metric := range snap.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "model_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_total_tokens"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_total_tokens")
			modelTotals[name] = *metric.Used
			if modelWindow == "" && strings.TrimSpace(metric.Window) != "" {
				modelWindow = strings.TrimSpace(metric.Window)
			}
		case strings.HasSuffix(key, "_cost_usd"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost_usd")
			if _, ok := modelTotals[name]; !ok {
				modelTotals[name] = *metric.Used
				modelUnit = "usd"
				if modelWindow == "" && strings.TrimSpace(metric.Window) != "" {
					modelWindow = strings.TrimSpace(metric.Window)
				}
			}
		}
	}
	if summary := summarizeShareUsage(modelTotals, 6); summary != "" {
		snap.Raw["model_usage"] = summary
		if modelWindow != "" {
			snap.Raw["model_usage_window"] = modelWindow
		}
		snap.Raw["model_usage_unit"] = modelUnit
	}

	clientTotals := make(map[string]float64)
	for key, metric := range snap.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "client_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_total_tokens"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "client_"), "_total_tokens")
			clientTotals[name] = *metric.Used
		case strings.HasSuffix(key, "_requests"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "client_"), "_requests")
			if _, ok := clientTotals[name]; !ok {
				clientTotals[name] = *metric.Used
			}
		}
	}
	if summary := summarizeShareUsage(clientTotals, 6); summary != "" {
		snap.Raw["client_usage"] = summary
	}
}

func summarizeShareUsage(values map[string]float64, maxItems int) string {
	type item struct {
		name  string
		value float64
	}
	list := make([]item, 0, len(values))
	total := 0.0
	for name, value := range values {
		if value <= 0 {
			continue
		}
		list = append(list, item{name: name, value: value})
		total += value
	}
	if len(list) == 0 || total <= 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].value != list[j].value {
			return list[i].value > list[j].value
		}
		return list[i].name < list[j].name
	})
	if maxItems > 0 && len(list) > maxItems {
		list = list[:maxItems]
	}
	parts := make([]string, 0, len(list))
	for _, entry := range list {
		parts = append(parts, fmt.Sprintf("%s: %.0f%%", normalizeUsageLabel(entry.name), entry.value/total*100))
	}
	return strings.Join(parts, ", ")
}

func summarizeCountUsage(values map[string]float64, unit string, maxItems int) string {
	type item struct {
		name  string
		value float64
	}
	list := make([]item, 0, len(values))
	for name, value := range values {
		if value <= 0 {
			continue
		}
		list = append(list, item{name: name, value: value})
	}
	if len(list) == 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].value != list[j].value {
			return list[i].value > list[j].value
		}
		return list[i].name < list[j].name
	})
	if maxItems > 0 && len(list) > maxItems {
		list = list[:maxItems]
	}
	parts := make([]string, 0, len(list))
	for _, entry := range list {
		parts = append(parts, fmt.Sprintf("%s: %.0f %s", normalizeUsageLabel(entry.name), entry.value, unit))
	}
	return strings.Join(parts, ", ")
}

func normalizeUsageLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return strings.ReplaceAll(name, "_", " ")
}

func emitModelDerivedToolUsageMetrics(snap *core.UsageSnapshot, modelRequests map[string]float64, window, source string) {
	if snap == nil || len(modelRequests) == 0 {
		return
	}
	if strings.TrimSpace(window) == "" {
		window = "30d inferred"
	}
	counts := make(map[string]int, len(modelRequests))
	rows := make([]modelUsageCount, 0, len(modelRequests))
	totalCalls := 0.0
	for model, requests := range modelRequests {
		if requests <= 0 {
			continue
		}
		key := "tool_" + sanitizeName(model)
		v := requests
		snap.Metrics[key] = core.Metric{Used: &v, Unit: "calls", Window: window}
		totalCalls += requests
		counts[model] = int(math.Round(requests))
		rows = append(rows, modelUsageCount{name: model, count: requests})
	}
	if totalCalls <= 0 {
		return
	}
	if source != "" {
		snap.Raw["tool_usage_source"] = source
	}
	if summary := summarizeModelCountUsage(rows, 6); summary != "" {
		snap.Raw["tool_usage"] = summary
	} else {
		snap.Raw["tool_usage"] = summarizeTopCounts(counts, 6)
	}
	totalV := totalCalls
	snap.Metrics["tool_calls_total"] = core.Metric{Used: &totalV, Unit: "calls", Window: "30d"}
}

func emitToolOutcomeMetrics(snap *core.UsageSnapshot, totalRequests, totalCancelled int, window string) {
	if snap == nil || totalRequests <= 0 {
		return
	}
	if strings.TrimSpace(window) == "" {
		window = "30d"
	}
	totalV := float64(totalRequests)
	snap.Metrics["tool_calls_total"] = core.Metric{Used: &totalV, Unit: "calls", Window: window}
	completed := totalRequests - totalCancelled
	if completed < 0 {
		completed = 0
	}
	completedV := float64(completed)
	snap.Metrics["tool_completed"] = core.Metric{Used: &completedV, Unit: "calls", Window: window}
	if totalCancelled > 0 {
		cancelledV := float64(totalCancelled)
		snap.Metrics["tool_cancelled"] = core.Metric{Used: &cancelledV, Unit: "calls", Window: window}
	}
	successRate := completedV / totalV * 100
	snap.Metrics["tool_success_rate"] = core.Metric{Used: &successRate, Unit: "%", Window: window}
}

func summarizeModelCountUsage(rows []modelUsageCount, limit int) string {
	if len(rows) == 0 {
		return ""
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].name < rows[j].name
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf("%s: %.0f calls", row.name, row.count))
	}
	return strings.Join(parts, ", ")
}

func summarizeTopCounts(counts map[string]int, limit int) string {
	type kv struct {
		name  string
		count int
	}
	items := make([]kv, 0, len(counts))
	for name, count := range counts {
		if count <= 0 {
			continue
		}
		items = append(items, kv{name: name, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].name < items[j].name
	})
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	parts := make([]string, 0, limit)
	for _, item := range items[:limit] {
		parts = append(parts, fmt.Sprintf("%s=%d", item.name, item.count))
	}
	return strings.Join(parts, ", ")
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	var builder strings.Builder
	builder.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	safe := strings.Trim(builder.String(), "_")
	if safe == "" {
		return "unknown"
	}
	return safe
}

func normalizeModelName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.Trim(name, "/")
	name = strings.Join(strings.Fields(name), "-")
	if name == "" {
		return ""
	}
	return name
}
