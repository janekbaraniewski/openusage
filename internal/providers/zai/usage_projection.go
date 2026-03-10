package zai

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func projectModelUsageSamples(samples []usageSample, snap *core.UsageSnapshot) {
	today := time.Now().UTC().Format("2006-01-02")
	hasNamedModelRows := false
	for _, sample := range samples {
		if strings.TrimSpace(sample.Name) != "" {
			hasNamedModelRows = true
			break
		}
	}

	total := usageRollup{}
	todayRollup := usageRollup{}
	modelTotals := make(map[string]*usageRollup)
	clientTotals := make(map[string]*usageRollup)
	sourceTotals := make(map[string]*usageRollup)
	providerTotals := make(map[string]*usageRollup)
	interfaceTotals := make(map[string]*usageRollup)
	endpointTotals := make(map[string]*usageRollup)
	languageTotals := make(map[string]*usageRollup)
	dailyCost := make(map[string]float64)
	dailyReq := make(map[string]float64)
	dailyTokens := make(map[string]float64)
	modelDailyTokens := make(map[string]map[string]float64)
	clientDailyReq := make(map[string]map[string]float64)
	sourceDailyReq := make(map[string]map[string]float64)
	sourceTodayReq := make(map[string]float64)

	for _, sample := range samples {
		modelName := strings.TrimSpace(sample.Name)
		useRow := !hasNamedModelRows || modelName != ""
		if !useRow {
			if lang := normalizeUsageDimension(sample.Language); lang != "" {
				accumulateUsageRollup(languageTotals, lang, sample)
			}
			if client := normalizeUsageDimension(sample.Client); client != "" {
				accumulateUsageRollup(clientTotals, client, sample)
				if sample.Date != "" {
					if _, ok := clientDailyReq[client]; !ok {
						clientDailyReq[client] = make(map[string]float64)
					}
					clientDailyReq[client][sample.Date] += sample.Requests
				}
			}
			if source := normalizeUsageDimension(sample.Source); source != "" {
				accumulateUsageRollup(sourceTotals, source, sample)
				if sample.Date == today {
					sourceTodayReq[source] += sample.Requests
				}
				if sample.Date != "" {
					if _, ok := sourceDailyReq[source]; !ok {
						sourceDailyReq[source] = make(map[string]float64)
					}
					sourceDailyReq[source][sample.Date] += sample.Requests
				}
			}
			if provider := normalizeUsageDimension(sample.Provider); provider != "" {
				accumulateUsageRollup(providerTotals, provider, sample)
			}
			if iface := normalizeUsageDimension(sample.Interface); iface != "" {
				accumulateUsageRollup(interfaceTotals, iface, sample)
			}
			if endpoint := normalizeUsageDimension(sample.Endpoint); endpoint != "" {
				accumulateUsageRollup(endpointTotals, endpoint, sample)
			}
			continue
		}
		accumulateRollupValues(&total, sample)
		if modelName != "" {
			accumulateUsageRollup(modelTotals, modelName, sample)
		}

		if sample.Date == today {
			accumulateRollupValues(&todayRollup, sample)
		}

		if sample.Date != "" && modelName != "" {
			dailyCost[sample.Date] += sample.CostUSD
			dailyReq[sample.Date] += sample.Requests
			dailyTokens[sample.Date] += sample.Total
			if _, ok := modelDailyTokens[modelName]; !ok {
				modelDailyTokens[modelName] = make(map[string]float64)
			}
			modelDailyTokens[modelName][sample.Date] += sample.Total
		}

		if client := normalizeUsageDimension(sample.Client); client != "" {
			accumulateUsageRollup(clientTotals, client, sample)
			if sample.Date != "" {
				if _, ok := clientDailyReq[client]; !ok {
					clientDailyReq[client] = make(map[string]float64)
				}
				clientDailyReq[client][sample.Date] += sample.Requests
			}
		}

		if source := normalizeUsageDimension(sample.Source); source != "" {
			accumulateUsageRollup(sourceTotals, source, sample)
			if sample.Date == today {
				sourceTodayReq[source] += sample.Requests
			}
			if sample.Date != "" {
				if _, ok := sourceDailyReq[source]; !ok {
					sourceDailyReq[source] = make(map[string]float64)
				}
				sourceDailyReq[source][sample.Date] += sample.Requests
			}
		}

		if provider := normalizeUsageDimension(sample.Provider); provider != "" {
			accumulateUsageRollup(providerTotals, provider, sample)
		}
		if iface := normalizeUsageDimension(sample.Interface); iface != "" {
			accumulateUsageRollup(interfaceTotals, iface, sample)
		}
		if endpoint := normalizeUsageDimension(sample.Endpoint); endpoint != "" {
			accumulateUsageRollup(endpointTotals, endpoint, sample)
		}
		lang := normalizeUsageDimension(sample.Language)
		if lang == "" {
			lang = inferModelUsageLanguage(modelName)
		}
		if lang != "" {
			accumulateUsageRollup(languageTotals, lang, sample)
		}
	}

	setUsedMetric(snap, "today_requests", todayRollup.Requests, "requests", "today")
	setUsedMetric(snap, "requests_today", todayRollup.Requests, "requests", "today")
	setUsedMetric(snap, "today_input_tokens", todayRollup.Input, "tokens", "today")
	setUsedMetric(snap, "today_output_tokens", todayRollup.Output, "tokens", "today")
	setUsedMetric(snap, "today_reasoning_tokens", todayRollup.Reasoning, "tokens", "today")
	setUsedMetric(snap, "today_tokens", todayRollup.Total, "tokens", "today")
	setUsedMetric(snap, "today_api_cost", todayRollup.CostUSD, "USD", "today")
	setUsedMetric(snap, "today_cost", todayRollup.CostUSD, "USD", "today")

	setUsedMetric(snap, "7d_requests", total.Requests, "requests", "7d")
	setUsedMetric(snap, "7d_tokens", total.Total, "tokens", "7d")
	setUsedMetric(snap, "7d_api_cost", total.CostUSD, "USD", "7d")
	setUsedMetric(snap, "window_requests", total.Requests, "requests", "7d")
	setUsedMetric(snap, "window_tokens", total.Total, "tokens", "7d")
	setUsedMetric(snap, "window_cost", total.CostUSD, "USD", "7d")

	setUsedMetric(snap, "active_models", float64(len(modelTotals)), "models", "7d")
	snap.Raw["model_usage_window"] = "7d"
	snap.Raw["activity_models"] = strconv.Itoa(len(modelTotals))
	snap.SetAttribute("activity_models", strconv.Itoa(len(modelTotals)))

	modelKeys := core.SortedStringKeys(modelTotals)
	for _, model := range modelKeys {
		stats := modelTotals[model]
		slug := sanitizeMetricSlug(model)
		setUsedMetric(snap, "model_"+slug+"_requests", stats.Requests, "requests", "7d")
		setUsedMetric(snap, "model_"+slug+"_input_tokens", stats.Input, "tokens", "7d")
		setUsedMetric(snap, "model_"+slug+"_output_tokens", stats.Output, "tokens", "7d")
		setUsedMetric(snap, "model_"+slug+"_total_tokens", stats.Total, "tokens", "7d")
		setUsedMetric(snap, "model_"+slug+"_cost_usd", stats.CostUSD, "USD", "7d")
		snap.Raw["model_"+slug+"_name"] = model

		rec := core.ModelUsageRecord{RawModelID: model, RawSource: "api", Window: "7d"}
		if stats.Input > 0 {
			rec.InputTokens = core.Float64Ptr(stats.Input)
		}
		if stats.Output > 0 {
			rec.OutputTokens = core.Float64Ptr(stats.Output)
		}
		if stats.Reasoning > 0 {
			rec.ReasoningTokens = core.Float64Ptr(stats.Reasoning)
		}
		if stats.Total > 0 {
			rec.TotalTokens = core.Float64Ptr(stats.Total)
		}
		if stats.CostUSD > 0 {
			rec.CostUSD = core.Float64Ptr(stats.CostUSD)
		}
		if stats.Requests > 0 {
			rec.Requests = core.Float64Ptr(stats.Requests)
		}
		snap.AppendModelUsage(rec)
	}

	for _, client := range sortedUsageRollupKeys(clientTotals) {
		stats := clientTotals[client]
		slug := sanitizeMetricSlug(client)
		setUsedMetric(snap, "client_"+slug+"_total_tokens", stats.Total, "tokens", "7d")
		setUsedMetric(snap, "client_"+slug+"_input_tokens", stats.Input, "tokens", "7d")
		setUsedMetric(snap, "client_"+slug+"_output_tokens", stats.Output, "tokens", "7d")
		setUsedMetric(snap, "client_"+slug+"_reasoning_tokens", stats.Reasoning, "tokens", "7d")
		setUsedMetric(snap, "client_"+slug+"_requests", stats.Requests, "requests", "7d")
		snap.Raw["client_"+slug+"_name"] = client
	}

	for _, source := range sortedUsageRollupKeys(sourceTotals) {
		stats := sourceTotals[source]
		slug := sanitizeMetricSlug(source)
		setUsedMetric(snap, "source_"+slug+"_requests", stats.Requests, "requests", "7d")
		if reqToday := sourceTodayReq[source]; reqToday > 0 {
			setUsedMetric(snap, "source_"+slug+"_requests_today", reqToday, "requests", "1d")
		}
	}

	for _, provider := range sortedUsageRollupKeys(providerTotals) {
		stats := providerTotals[provider]
		slug := sanitizeMetricSlug(provider)
		setUsedMetric(snap, "provider_"+slug+"_cost_usd", stats.CostUSD, "USD", "7d")
		setUsedMetric(snap, "provider_"+slug+"_requests", stats.Requests, "requests", "7d")
		setUsedMetric(snap, "provider_"+slug+"_input_tokens", stats.Input, "tokens", "7d")
		setUsedMetric(snap, "provider_"+slug+"_output_tokens", stats.Output, "tokens", "7d")
		snap.Raw["provider_"+slug+"_name"] = provider
	}

	for _, iface := range sortedUsageRollupKeys(interfaceTotals) {
		stats := interfaceTotals[iface]
		setUsedMetric(snap, "interface_"+sanitizeMetricSlug(iface), stats.Requests, "calls", "7d")
	}

	for _, endpoint := range sortedUsageRollupKeys(endpointTotals) {
		stats := endpointTotals[endpoint]
		setUsedMetric(snap, "endpoint_"+sanitizeMetricSlug(endpoint)+"_requests", stats.Requests, "requests", "7d")
	}

	languageReqSummary := make(map[string]float64, len(languageTotals))
	for _, lang := range sortedUsageRollupKeys(languageTotals) {
		stats := languageTotals[lang]
		slug := sanitizeMetricSlug(lang)
		value := stats.Requests
		if value <= 0 {
			value = stats.Total
		}
		setUsedMetric(snap, "lang_"+slug, value, "requests", "7d")
		languageReqSummary[lang] = stats.Requests
	}
	setUsedMetric(snap, "active_languages", float64(len(languageTotals)), "languages", "7d")
	setUsedMetric(snap, "activity_providers", float64(len(providerTotals)), "providers", "7d")

	snap.DailySeries["cost"] = core.SortedTimePoints(dailyCost)
	snap.DailySeries["requests"] = core.SortedTimePoints(dailyReq)
	snap.DailySeries["tokens"] = core.SortedTimePoints(dailyTokens)

	type modelTotal struct {
		name   string
		tokens float64
	}
	var ranked []modelTotal
	for model, stats := range modelTotals {
		ranked = append(ranked, modelTotal{name: model, tokens: stats.Total})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].tokens > ranked[j].tokens })
	if len(ranked) > 3 {
		ranked = ranked[:3]
	}
	for _, entry := range ranked {
		if dayMap, ok := modelDailyTokens[entry.name]; ok {
			snap.DailySeries["tokens_"+sanitizeMetricSlug(entry.name)] = core.SortedTimePoints(dayMap)
		}
	}

	for client, dayMap := range clientDailyReq {
		if len(dayMap) > 0 {
			snap.DailySeries["usage_client_"+sanitizeMetricSlug(client)] = core.SortedTimePoints(dayMap)
		}
	}
	for source, dayMap := range sourceDailyReq {
		if len(dayMap) > 0 {
			snap.DailySeries["usage_source_"+sanitizeMetricSlug(source)] = core.SortedTimePoints(dayMap)
		}
	}

	modelShare := make(map[string]float64, len(modelTotals))
	modelUnit := "tok"
	for model, stats := range modelTotals {
		if stats.Total > 0 {
			modelShare[model] = stats.Total
		} else if stats.Requests > 0 {
			modelShare[model] = stats.Requests
			modelUnit = "req"
		}
	}
	if summary := summarizeShareUsage(modelShare, 6); summary != "" {
		snap.Raw["model_usage"] = summary
		snap.Raw["model_usage_unit"] = modelUnit
	}

	clientShare := make(map[string]float64, len(clientTotals))
	for client, stats := range clientTotals {
		if stats.Total > 0 {
			clientShare[client] = stats.Total
		} else if stats.Requests > 0 {
			clientShare[client] = stats.Requests
		}
	}
	if summary := summarizeShareUsage(clientShare, 6); summary != "" {
		snap.Raw["client_usage"] = summary
	}

	sourceShare := make(map[string]float64, len(sourceTotals))
	for source, stats := range sourceTotals {
		if stats.Requests > 0 {
			sourceShare[source] = stats.Requests
		}
	}
	if summary := summarizeCountUsage(sourceShare, "req", 6); summary != "" {
		snap.Raw["source_usage"] = summary
	}

	providerShare := make(map[string]float64, len(providerTotals))
	for provider, stats := range providerTotals {
		if stats.CostUSD > 0 {
			providerShare[provider] = stats.CostUSD
		} else if stats.Requests > 0 {
			providerShare[provider] = stats.Requests
		}
	}
	if summary := summarizeShareUsage(providerShare, 6); summary != "" {
		snap.Raw["provider_usage"] = summary
	}
	if summary := summarizeCountUsage(languageReqSummary, "req", 8); summary != "" {
		snap.Raw["language_usage"] = summary
	}

	snap.Raw["activity_days"] = strconv.Itoa(len(dailyReq))
	snap.Raw["activity_clients"] = strconv.Itoa(len(clientTotals))
	snap.Raw["activity_sources"] = strconv.Itoa(len(sourceTotals))
	snap.Raw["activity_providers"] = strconv.Itoa(len(providerTotals))
	snap.Raw["activity_languages"] = strconv.Itoa(len(languageTotals))
	snap.Raw["activity_endpoints"] = strconv.Itoa(len(endpointTotals))
	snap.SetAttribute("activity_days", snap.Raw["activity_days"])
	snap.SetAttribute("activity_clients", snap.Raw["activity_clients"])
	snap.SetAttribute("activity_sources", snap.Raw["activity_sources"])
	snap.SetAttribute("activity_providers", snap.Raw["activity_providers"])
	snap.SetAttribute("activity_languages", snap.Raw["activity_languages"])
	snap.SetAttribute("activity_endpoints", snap.Raw["activity_endpoints"])
}

func projectToolUsageSamples(samples []usageSample, snap *core.UsageSnapshot) {
	today := time.Now().UTC().Format("2006-01-02")
	totalCalls := 0.0
	todayCalls := 0.0
	toolTotals := make(map[string]*usageRollup)
	dailyCalls := make(map[string]float64)

	for _, sample := range samples {
		tool := sample.Name
		if tool == "" {
			tool = "unknown"
		}
		acc, ok := toolTotals[tool]
		if !ok {
			acc = &usageRollup{}
			toolTotals[tool] = acc
		}
		acc.Requests += sample.Requests
		acc.CostUSD += sample.CostUSD
		totalCalls += sample.Requests
		if sample.Date == today {
			todayCalls += sample.Requests
		}
		if sample.Date != "" {
			dailyCalls[sample.Date] += sample.Requests
		}
	}

	setUsedMetric(snap, "tool_calls_today", todayCalls, "calls", "today")
	setUsedMetric(snap, "today_tool_calls", todayCalls, "calls", "today")
	setUsedMetric(snap, "7d_tool_calls", totalCalls, "calls", "7d")

	for _, tool := range core.SortedStringKeys(toolTotals) {
		stats := toolTotals[tool]
		slug := sanitizeMetricSlug(tool)
		setUsedMetric(snap, "tool_"+slug, stats.Requests, "calls", "7d")
		setUsedMetric(snap, "toolcost_"+slug+"_usd", stats.CostUSD, "USD", "7d")
		snap.Raw["tool_"+slug+"_name"] = tool
	}

	if len(dailyCalls) > 0 {
		snap.DailySeries["tool_calls"] = core.SortedTimePoints(dailyCalls)
	}

	toolSummary := make(map[string]float64, len(toolTotals))
	for tool, stats := range toolTotals {
		if stats.Requests > 0 {
			toolSummary[tool] = stats.Requests
		}
	}
	if summary := summarizeCountUsage(toolSummary, "calls", 8); summary != "" {
		snap.Raw["tool_usage"] = summary
	}
}
