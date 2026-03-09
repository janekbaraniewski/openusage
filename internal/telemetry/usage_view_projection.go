package telemetry

import (
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

func applyUsageViewToSnapshot(snap *core.UsageSnapshot, agg *telemetryUsageAgg, timeWindow core.TimeWindow) {
	if snap == nil || agg == nil {
		return
	}
	authoritativeCost := usageAuthoritativeCost(*snap)
	windowLabel := string(timeWindow)
	snap.EnsureMaps()
	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}

	savedAPIModelCosts := make(map[string]core.Metric)
	for key, metric := range snap.Metrics {
		if strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost") && metric.Window == "billing-cycle" {
			savedAPIModelCosts[key] = metric
		}
	}

	metricsBefore := len(snap.Metrics)
	_, hadFiveHourBefore := snap.Metrics["usage_five_hour"]
	stripAllTime := timeWindow != "" && timeWindow != "all"
	deletedCount := 0
	for key, metric := range snap.Metrics {
		if strings.HasPrefix(key, "source_") ||
			strings.HasPrefix(key, "client_") ||
			strings.HasPrefix(key, "tool_") ||
			strings.HasPrefix(key, "model_") ||
			strings.HasPrefix(key, "project_") ||
			strings.HasPrefix(key, "provider_") ||
			strings.HasPrefix(key, "lang_") ||
			strings.HasPrefix(key, "interface_") ||
			isStaleActivityMetric(key) {
			delete(snap.Metrics, key)
			deletedCount++
		} else if stripAllTime && metric.Window == "all-time" && !isCurrentStateMetric(key) {
			delete(snap.Metrics, key)
			deletedCount++
		}
	}
	_, hasFiveHourAfter := snap.Metrics["usage_five_hour"]
	core.Tracef("[usage_view] %s: cleanup deleted %d/%d metrics, usage_five_hour before=%v after=%v",
		snap.ProviderID, deletedCount, metricsBefore, hadFiveHourBefore, hasFiveHourAfter)
	telemetryPrefixes := []string{"source_", "client_", "tool_", "model_", "project_", "provider_", "usage_", "analytics_"}
	extendedPrefixes := append(telemetryPrefixes, "lang_", "jsonl_")
	deleteByPrefixes(snap.Raw, extendedPrefixes)
	deleteByPrefixes(snap.Attributes, telemetryPrefixes)
	deleteByPrefixes(snap.Diagnostics, telemetryPrefixes)
	for key := range snap.DailySeries {
		if strings.HasPrefix(key, "usage_model_") ||
			strings.HasPrefix(key, "usage_source_") ||
			strings.HasPrefix(key, "usage_project_") ||
			strings.HasPrefix(key, "usage_client_") ||
			strings.HasPrefix(key, "tokens_client_") ||
			key == "analytics_cost" ||
			key == "analytics_requests" ||
			key == "analytics_tokens" {
			delete(snap.DailySeries, key)
		}
	}

	snap.ModelUsage = nil
	modelCostTotal := 0.0
	for _, model := range agg.Models {
		mk := sanitizeMetricID(model.Model)
		snap.Metrics["model_"+mk+"_input_tokens"] = core.Metric{Used: core.Float64Ptr(model.InputTokens), Unit: "tokens", Window: windowLabel}
		snap.Metrics["model_"+mk+"_output_tokens"] = core.Metric{Used: core.Float64Ptr(model.OutputTokens), Unit: "tokens", Window: windowLabel}
		snap.Metrics["model_"+mk+"_cached_tokens"] = core.Metric{Used: core.Float64Ptr(model.CachedTokens), Unit: "tokens", Window: windowLabel}
		snap.Metrics["model_"+mk+"_reasoning_tokens"] = core.Metric{Used: core.Float64Ptr(model.Reasoning), Unit: "tokens", Window: windowLabel}
		snap.Metrics["model_"+mk+"_cost_usd"] = core.Metric{Used: core.Float64Ptr(model.CostUSD), Unit: "USD", Window: windowLabel}
		snap.Metrics["model_"+mk+"_requests"] = core.Metric{Used: core.Float64Ptr(model.Requests), Unit: "requests", Window: windowLabel}
		snap.Metrics["model_"+mk+"_requests_today"] = core.Metric{Used: core.Float64Ptr(model.Requests1d), Unit: "requests", Window: "1d"}
		modelCostTotal += model.CostUSD
		snap.ModelUsage = append(snap.ModelUsage, core.ModelUsageRecord{
			RawModelID:      model.Model,
			RawSource:       "telemetry",
			Window:          windowLabel,
			InputTokens:     core.Float64Ptr(model.InputTokens),
			OutputTokens:    core.Float64Ptr(model.OutputTokens),
			CachedTokens:    core.Float64Ptr(model.CachedTokens),
			ReasoningTokens: core.Float64Ptr(model.Reasoning),
			TotalTokens:     core.Float64Ptr(model.TotalTokens),
			CostUSD:         core.Float64Ptr(model.CostUSD),
			Requests:        core.Float64Ptr(model.Requests),
		})
	}
	telemetryCostInsufficient := authoritativeCost > 0 && modelCostTotal < authoritativeCost*0.1
	if telemetryCostInsufficient && len(savedAPIModelCosts) > 0 {
		for key, metric := range savedAPIModelCosts {
			snap.Metrics[key] = metric
		}
		core.Tracef("[usage_view] %s: restored %d API model cost metrics (telemetry cost %.2f << authoritative %.2f)",
			snap.ProviderID, len(savedAPIModelCosts), modelCostTotal, authoritativeCost)
	} else if len(agg.Models) > 0 {
		if delta := authoritativeCost - modelCostTotal; authoritativeCost > 0 && delta > 0.000001 {
			snap.Metrics["model_unattributed_cost_usd"] = core.Metric{Used: core.Float64Ptr(delta), Unit: "USD", Window: windowLabel}
			snap.SetDiagnostic("telemetry_unattributed_model_cost_usd", fmt.Sprintf("%.6f", delta))
		}
	}

	if !strings.EqualFold(strings.TrimSpace(snap.ProviderID), "codex") {
		providerCostTotal := 0.0
		for _, provider := range agg.Providers {
			pk := sanitizeMetricID(provider.Provider)
			snap.Metrics["provider_"+pk+"_cost_usd"] = core.Metric{Used: core.Float64Ptr(provider.CostUSD), Unit: "USD", Window: windowLabel}
			snap.Metrics["provider_"+pk+"_input_tokens"] = core.Metric{Used: core.Float64Ptr(provider.Input), Unit: "tokens", Window: windowLabel}
			snap.Metrics["provider_"+pk+"_output_tokens"] = core.Metric{Used: core.Float64Ptr(provider.Output), Unit: "tokens", Window: windowLabel}
			snap.Metrics["provider_"+pk+"_requests"] = core.Metric{Used: core.Float64Ptr(provider.Requests), Unit: "requests", Window: windowLabel}
			providerCostTotal += provider.CostUSD
		}
		if delta := authoritativeCost - providerCostTotal; authoritativeCost > 0 && delta > 0.000001 {
			snap.Metrics["provider_unattributed_cost_usd"] = core.Metric{Used: core.Float64Ptr(delta), Unit: "USD", Window: windowLabel}
			snap.SetDiagnostic("telemetry_unattributed_provider_cost_usd", fmt.Sprintf("%.6f", delta))
		}
	}

	for _, source := range agg.Sources {
		sk := sanitizeMetricID(source.Source)
		snap.Metrics["source_"+sk+"_requests_today"] = core.Metric{Used: core.Float64Ptr(source.Requests1d), Unit: "requests", Window: "1d"}
		snap.Metrics["client_"+sk+"_total_tokens"] = core.Metric{Used: core.Float64Ptr(source.Tokens), Unit: "tokens", Window: windowLabel}
		snap.Metrics["client_"+sk+"_input_tokens"] = core.Metric{Used: core.Float64Ptr(source.Input), Unit: "tokens", Window: windowLabel}
		snap.Metrics["client_"+sk+"_output_tokens"] = core.Metric{Used: core.Float64Ptr(source.Output), Unit: "tokens", Window: windowLabel}
		snap.Metrics["client_"+sk+"_cached_tokens"] = core.Metric{Used: core.Float64Ptr(source.Cached), Unit: "tokens", Window: windowLabel}
		snap.Metrics["client_"+sk+"_reasoning_tokens"] = core.Metric{Used: core.Float64Ptr(source.Reasoning), Unit: "tokens", Window: windowLabel}
		snap.Metrics["client_"+sk+"_requests"] = core.Metric{Used: core.Float64Ptr(source.Requests), Unit: "requests", Window: windowLabel}
		snap.Metrics["client_"+sk+"_sessions"] = core.Metric{Used: core.Float64Ptr(source.Sessions), Unit: "sessions", Window: windowLabel}
	}
	for _, project := range agg.Projects {
		pk := sanitizeMetricID(project.Project)
		if pk == "" {
			continue
		}
		snap.Metrics["project_"+pk+"_requests"] = core.Metric{Used: core.Float64Ptr(project.Requests), Unit: "requests", Window: windowLabel}
		snap.Metrics["project_"+pk+"_requests_today"] = core.Metric{Used: core.Float64Ptr(project.Requests1d), Unit: "requests", Window: "1d"}
	}

	var totalToolCalls, totalToolCallsOK, totalToolCallsError, totalToolCallsAborted float64
	for _, tool := range agg.Tools {
		tk := sanitizeMetricID(tool.Tool)
		snap.Metrics["tool_"+tk] = core.Metric{Used: core.Float64Ptr(tool.Calls), Unit: "calls", Window: windowLabel}
		snap.Metrics["tool_"+tk+"_today"] = core.Metric{Used: core.Float64Ptr(tool.Calls1d), Unit: "calls", Window: "1d"}
		totalToolCalls += tool.Calls
		totalToolCallsOK += tool.CallsOK
		totalToolCallsError += tool.CallsError
		totalToolCallsAborted += tool.CallsAborted
	}
	if totalToolCalls > 0 {
		snap.Metrics["tool_calls_total"] = core.Metric{Used: core.Float64Ptr(totalToolCalls), Unit: "calls", Window: windowLabel}
		snap.Metrics["tool_completed"] = core.Metric{Used: core.Float64Ptr(totalToolCallsOK), Unit: "calls", Window: windowLabel}
		snap.Metrics["tool_errored"] = core.Metric{Used: core.Float64Ptr(totalToolCallsError), Unit: "calls", Window: windowLabel}
		snap.Metrics["tool_cancelled"] = core.Metric{Used: core.Float64Ptr(totalToolCallsAborted), Unit: "calls", Window: windowLabel}
		successRate := (totalToolCallsOK / totalToolCalls) * 100
		snap.Metrics["tool_success_rate"] = core.Metric{Used: core.Float64Ptr(successRate), Unit: "%", Window: windowLabel}
	}

	var mcpTotalCalls, mcpTotalCalls1d float64
	for _, server := range agg.MCPServers {
		sk := sanitizeMetricID(server.Server)
		snap.Metrics["mcp_"+sk+"_total"] = core.Metric{Used: core.Float64Ptr(server.Calls), Unit: "calls", Window: windowLabel}
		snap.Metrics["mcp_"+sk+"_total_today"] = core.Metric{Used: core.Float64Ptr(server.Calls1d), Unit: "calls", Window: "1d"}
		mcpTotalCalls += server.Calls
		mcpTotalCalls1d += server.Calls1d
		for _, function := range server.Functions {
			fk := sanitizeMetricID(function.Function)
			snap.Metrics["mcp_"+sk+"_"+fk] = core.Metric{Used: core.Float64Ptr(function.Calls), Unit: "calls", Window: windowLabel}
		}
	}
	if mcpTotalCalls > 0 {
		snap.Metrics["mcp_calls_total"] = core.Metric{Used: core.Float64Ptr(mcpTotalCalls), Unit: "calls", Window: windowLabel}
		snap.Metrics["mcp_calls_total_today"] = core.Metric{Used: core.Float64Ptr(mcpTotalCalls1d), Unit: "calls", Window: "1d"}
		snap.Metrics["mcp_servers_active"] = core.Metric{Used: core.Float64Ptr(float64(len(agg.MCPServers))), Unit: "servers", Window: windowLabel}
	}

	for _, language := range agg.Languages {
		lk := sanitizeMetricID(language.Language)
		snap.Metrics["lang_"+lk] = core.Metric{Used: core.Float64Ptr(language.Requests), Unit: "requests", Window: windowLabel}
	}

	act := agg.Activity
	if act.Messages > 0 {
		snap.Metrics["messages_today"] = core.Metric{Used: core.Float64Ptr(act.Messages), Unit: "messages", Window: windowLabel}
	}
	if act.Sessions > 0 {
		snap.Metrics["sessions_today"] = core.Metric{Used: core.Float64Ptr(act.Sessions), Unit: "sessions", Window: windowLabel}
	}
	if act.ToolCalls > 0 {
		snap.Metrics["tool_calls_today"] = core.Metric{Used: core.Float64Ptr(act.ToolCalls), Unit: "calls", Window: windowLabel}
		snap.Metrics["7d_tool_calls"] = core.Metric{Used: core.Float64Ptr(act.ToolCalls), Unit: "calls", Window: windowLabel}
	}
	if act.InputTokens > 0 {
		snap.Metrics["today_input_tokens"] = core.Metric{Used: core.Float64Ptr(act.InputTokens), Unit: "tokens", Window: windowLabel}
	}
	if act.OutputTokens > 0 {
		snap.Metrics["today_output_tokens"] = core.Metric{Used: core.Float64Ptr(act.OutputTokens), Unit: "tokens", Window: windowLabel}
	}
	if act.TotalCost > 0 {
		snap.Metrics["today_api_cost"] = core.Metric{Used: core.Float64Ptr(act.TotalCost), Unit: "USD", Window: windowLabel}
	}

	codeStats := agg.CodeStats
	if codeStats.FilesChanged > 0 {
		snap.Metrics["composer_files_changed"] = core.Metric{Used: core.Float64Ptr(codeStats.FilesChanged), Unit: "files", Window: windowLabel}
	}
	if codeStats.LinesAdded > 0 {
		snap.Metrics["composer_lines_added"] = core.Metric{Used: core.Float64Ptr(codeStats.LinesAdded), Unit: "lines", Window: windowLabel}
	}
	if codeStats.LinesRemoved > 0 {
		snap.Metrics["composer_lines_removed"] = core.Metric{Used: core.Float64Ptr(codeStats.LinesRemoved), Unit: "lines", Window: windowLabel}
	}

	var windowRequests, windowCost, windowTokens float64
	for _, model := range agg.Models {
		windowRequests += model.Requests
		windowCost += model.CostUSD
		windowTokens += model.TotalTokens
	}
	if windowRequests > 0 {
		snap.Metrics["window_requests"] = core.Metric{Used: core.Float64Ptr(windowRequests), Unit: "requests", Window: windowLabel}
	}
	if windowCost > 0 {
		snap.Metrics["window_cost"] = core.Metric{Used: core.Float64Ptr(windowCost), Unit: "USD", Window: windowLabel}
	}
	if windowTokens > 0 {
		snap.Metrics["window_tokens"] = core.Metric{Used: core.Float64Ptr(windowTokens), Unit: "tokens", Window: windowLabel}
	}

	snap.DailySeries["analytics_cost"] = pointsFromDaily(agg.Daily, func(point telemetryDayPoint) float64 { return point.CostUSD })
	snap.DailySeries["analytics_requests"] = pointsFromDaily(agg.Daily, func(point telemetryDayPoint) float64 { return point.Requests })
	snap.DailySeries["analytics_tokens"] = pointsFromDaily(agg.Daily, func(point telemetryDayPoint) float64 { return point.Tokens })

	for model, series := range agg.ModelDaily {
		snap.DailySeries["usage_model_"+sanitizeMetricID(model)] = series
	}
	for source, series := range agg.SourceDaily {
		snap.DailySeries["usage_source_"+sanitizeMetricID(source)] = series
	}
	for project, series := range agg.ProjectDaily {
		snap.DailySeries["usage_project_"+sanitizeMetricID(project)] = series
	}
	for client, series := range agg.ClientDaily {
		snap.DailySeries["usage_client_"+sanitizeMetricID(client)] = series
	}
	for client, series := range agg.ClientTokens {
		snap.DailySeries["tokens_client_"+sanitizeMetricID(client)] = series
	}

	snap.SetAttribute("telemetry_view", "canonical")
	snap.SetAttribute("telemetry_source_of_truth", "canonical_usage_events")
	snap.SetAttribute("telemetry_last_event_at", agg.LastOccurred)
	if strings.TrimSpace(agg.Scope) != "" {
		snap.SetAttribute("telemetry_scope", agg.Scope)
	}
	if strings.TrimSpace(agg.AccountID) != "" {
		snap.SetAttribute("telemetry_scope_account_id", agg.AccountID)
	}
	snap.SetDiagnostic("telemetry_event_count", fmt.Sprintf("%d", agg.EventCount))
}

func pointsFromDaily(in []telemetryDayPoint, pick func(telemetryDayPoint) float64) []core.TimePoint {
	return lo.Map(in, func(row telemetryDayPoint, _ int) core.TimePoint {
		return core.TimePoint{Date: row.Day, Value: pick(row)}
	})
}

func isStaleActivityMetric(key string) bool {
	switch key {
	case "messages_today", "sessions_today", "tool_calls_today",
		"7d_tool_calls", "all_time_tool_calls", "tool_calls_total",
		"tool_completed", "tool_errored", "tool_cancelled", "tool_success_rate",
		"today_input_tokens", "today_output_tokens",
		"7d_input_tokens", "7d_output_tokens",
		"all_time_input_tokens", "all_time_output_tokens",
		"all_time_cache_read_tokens", "all_time_cache_create_tokens",
		"all_time_cache_create_5m_tokens", "all_time_cache_create_1h_tokens",
		"all_time_reasoning_tokens",
		"today_api_cost",
		"burn_rate",
		"composer_lines_added", "composer_lines_removed",
		"composer_files_changed":
		return true
	case "7d_api_cost", "all_time_api_cost", "5h_block_cost":
		return false
	}
	if strings.HasPrefix(key, "tokens_today_") ||
		strings.HasPrefix(key, "input_tokens_") ||
		strings.HasPrefix(key, "output_tokens_") ||
		strings.HasPrefix(key, "today_") ||
		strings.HasPrefix(key, "7d_") ||
		strings.HasPrefix(key, "all_time_") ||
		strings.HasPrefix(key, "5h_block_") ||
		strings.HasPrefix(key, "project_") ||
		strings.HasPrefix(key, "agent_") {
		return true
	}
	return false
}

func isCurrentStateMetric(key string) bool {
	if strings.HasPrefix(key, "plan_") ||
		strings.HasPrefix(key, "billing_") ||
		strings.HasPrefix(key, "team_") ||
		strings.HasPrefix(key, "spend_") ||
		strings.HasPrefix(key, "individual_") {
		return true
	}
	switch key {
	case "today_cost", "7d_api_cost", "all_time_api_cost", "5h_block_cost", "usage_daily", "usage_weekly", "usage_five_hour":
		return true
	}
	return false
}

func usageAuthoritativeCost(snap core.UsageSnapshot) float64 {
	if metric, ok := snap.Metrics["credit_balance"]; ok && metric.Used != nil && *metric.Used > 0 {
		return *metric.Used
	}
	if metric, ok := snap.Metrics["spend_limit"]; ok && metric.Used != nil && *metric.Used > 0 {
		return *metric.Used
	}
	if metric, ok := snap.Metrics["plan_total_spend_usd"]; ok && metric.Used != nil && *metric.Used > 0 {
		return *metric.Used
	}
	if metric, ok := snap.Metrics["credits"]; ok && metric.Used != nil && *metric.Used > 0 {
		return *metric.Used
	}
	return 0
}
