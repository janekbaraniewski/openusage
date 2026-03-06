package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"

	_ "github.com/mattn/go-sqlite3"
)

type telemetryModelAgg struct {
	Model        string
	InputTokens  float64
	OutputTokens float64
	CachedTokens float64
	Reasoning    float64
	TotalTokens  float64
	CostUSD      float64
	Requests     float64
	Requests1d   float64
}

type telemetrySourceAgg struct {
	Source     string
	Requests   float64
	Requests1d float64
	Tokens     float64
	Input      float64
	Output     float64
	Cached     float64
	Reasoning  float64
	Sessions   float64
}

type telemetryToolAgg struct {
	Tool           string
	Calls          float64
	Calls1d        float64
	CallsOK        float64
	CallsOK1d      float64
	CallsError     float64
	CallsError1d   float64
	CallsAborted   float64
	CallsAborted1d float64
}

type telemetryMCPFunctionAgg struct {
	Function string
	Calls    float64
	Calls1d  float64
}

type telemetryMCPServerAgg struct {
	Server    string
	Calls     float64
	Calls1d   float64
	Functions []telemetryMCPFunctionAgg
}

type telemetryLanguageAgg struct {
	Language string
	Requests float64
}

type telemetryProviderAgg struct {
	Provider string
	CostUSD  float64
	Requests float64
	Input    float64
	Output   float64
}

type telemetryDayPoint struct {
	Day      string
	CostUSD  float64
	Requests float64
	Tokens   float64
}

type telemetryActivityAgg struct {
	Messages     float64
	Sessions     float64
	ToolCalls    float64
	InputTokens  float64
	OutputTokens float64
	CachedTokens float64
	ReasonTokens float64
	TotalTokens  float64
	TotalCost    float64
}

type telemetryCodeStatsAgg struct {
	FilesChanged float64
	LinesAdded   float64
	LinesRemoved float64
}

type telemetryUsageAgg struct {
	LastOccurred string
	EventCount   int64
	Scope        string
	AccountID    string
	Models       []telemetryModelAgg
	Providers    []telemetryProviderAgg
	Sources      []telemetrySourceAgg
	Tools        []telemetryToolAgg
	MCPServers   []telemetryMCPServerAgg
	Languages    []telemetryLanguageAgg
	Activity     telemetryActivityAgg
	CodeStats    telemetryCodeStatsAgg
	Daily        []telemetryDayPoint
	ModelDaily   map[string][]core.TimePoint
	SourceDaily  map[string][]core.TimePoint
	ClientDaily  map[string][]core.TimePoint
	ClientTokens map[string][]core.TimePoint
}

type usageFilter struct {
	ProviderIDs     []string
	AccountID       string
	TimeWindowHours int    // 0 = no filter
	materializedTbl string // if set, queries read from this temp table instead of rebuilding the CTE
}

func clientDimensionExpr() string {
	return `COALESCE(
		NULLIF(TRIM(
			COALESCE(
				json_extract(source_payload, '$.client'),
				json_extract(source_payload, '$.payload.client'),
				json_extract(source_payload, '$._normalized.client'),
				json_extract(source_payload, '$.cursor_source'),
				json_extract(source_payload, '$.source.client'),
				''
			)
		), ''),
		CASE
			WHEN LOWER(TRIM(source_system)) = 'codex' THEN 'CLI'
			ELSE NULL
		END,
		COALESCE(NULLIF(TRIM(source_system), ''), NULLIF(TRIM(workspace_id), ''), 'unknown')
	)`
}

func applyCanonicalUsageViewWithDB(
	ctx context.Context,
	db *sql.DB,
	snaps map[string]core.UsageSnapshot,
	providerLinks map[string]string,
	timeWindowHours int,
	timeWindow string,
) (map[string]core.UsageSnapshot, error) {
	if db == nil {
		return snaps, nil
	}

	out := make(map[string]core.UsageSnapshot, len(snaps))
	cache := make(map[string]*telemetryUsageAgg)

	activeStart := time.Now()
	telemetryActiveProviders := queryTelemetryActiveProviders(ctx, db)
	core.Tracef("[usage_view_perf] queryTelemetryActiveProviders: %dms", time.Since(activeStart).Milliseconds())

	for accountID, snap := range snaps {
		s := snap
		providerID := strings.TrimSpace(s.ProviderID)
		if providerID == "" {
			out[accountID] = s
			continue
		}
		accountScope := strings.TrimSpace(s.AccountID)
		if accountScope == "" {
			accountScope = strings.TrimSpace(accountID)
		}
		sourceProviders := telemetrySourceProvidersForTarget(providerID, providerLinks)
		if len(sourceProviders) == 0 {
			out[accountID] = s
			continue
		}

		cacheKey := strings.Join(sourceProviders, ",") + "|" + accountScope
		agg, ok := cache[cacheKey]
		if !ok {
			loaded, loadErr := loadUsageViewForProviderWithSources(ctx, db, sourceProviders, accountScope, timeWindowHours)
			if loadErr != nil {
				return snaps, loadErr
			}
			cache[cacheKey] = loaded
			agg = loaded
		}
		if agg == nil || agg.EventCount == 0 {
			// Check if telemetry is active for this provider (has ANY events, just not in this window).
			hasTelemetry := false
			for _, sp := range sourceProviders {
				if telemetryActiveProviders[sp] {
					hasTelemetry = true
					break
				}
			}
			if hasTelemetry && agg != nil {
				// Telemetry is active but no events in this time window.
				// Strip stale all-time metrics so TUI shows "no data" placeholders.
				windowLabel := "all"
				if timeWindowHours > 0 && timeWindow != "" {
					windowLabel = timeWindow
				}
				applyUsageViewToSnapshot(&s, agg, windowLabel)
				out[accountID] = s
			} else {
				out[accountID] = s
			}
			continue
		}

		windowLabel := "all"
		if timeWindowHours > 0 && timeWindow != "" {
			windowLabel = timeWindow
		}
		applyUsageViewToSnapshot(&s, agg, windowLabel)
		out[accountID] = s
	}

	return out, nil
}

func applyUsageViewToSnapshot(snap *core.UsageSnapshot, agg *telemetryUsageAgg, timeWindow string) {
	if snap == nil || agg == nil {
		return
	}
	authoritativeCost := usageAuthoritativeCost(*snap)
	snap.EnsureMaps()
	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}

	// Save API-sourced model cost metrics (billing-cycle) before cleanup.
	// These will be restored if telemetry events lack sufficient cost attribution.
	savedAPIModelCosts := make(map[string]core.Metric)
	for key, m := range snap.Metrics {
		if strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost") && m.Window == "billing-cycle" {
			savedAPIModelCosts[key] = m
		}
	}

	metricsBefore := len(snap.Metrics)
	_, hadFiveHourBefore := snap.Metrics["usage_five_hour"]
	stripAllTime := timeWindow != "" && timeWindow != "all"
	deletedCount := 0
	for key, m := range snap.Metrics {
		if strings.HasPrefix(key, "source_") ||
			strings.HasPrefix(key, "client_") ||
			strings.HasPrefix(key, "tool_") ||
			strings.HasPrefix(key, "model_") ||
			strings.HasPrefix(key, "provider_") ||
			strings.HasPrefix(key, "lang_") ||
			strings.HasPrefix(key, "interface_") ||
			isStaleActivityMetric(key) {
			delete(snap.Metrics, key)
			deletedCount++
		} else if stripAllTime && m.Window == "all-time" && !isCurrentStateMetric(key) {
			// Strip cumulative "all-time" metrics from the limit_snapshot so the
			// TUI doesn't show misleading all-time counts under a windowed badge.
			// Current-state metrics (plan quotas, billing, team) are preserved.
			delete(snap.Metrics, key)
			deletedCount++
		}
	}
	_, hasFiveHourAfter := snap.Metrics["usage_five_hour"]
	core.Tracef("[usage_view] %s: cleanup deleted %d/%d metrics, usage_five_hour before=%v after=%v",
		snap.ProviderID, deletedCount, metricsBefore, hadFiveHourBefore, hasFiveHourAfter)
	for key := range snap.Raw {
		if strings.HasPrefix(key, "source_") ||
			strings.HasPrefix(key, "client_") ||
			strings.HasPrefix(key, "tool_") ||
			strings.HasPrefix(key, "model_") ||
			strings.HasPrefix(key, "provider_") ||
			strings.HasPrefix(key, "lang_") ||
			strings.HasPrefix(key, "jsonl_") ||
			strings.HasPrefix(key, "usage_") ||
			strings.HasPrefix(key, "analytics_") {
			delete(snap.Raw, key)
		}
	}
	for key := range snap.Attributes {
		if strings.HasPrefix(key, "source_") ||
			strings.HasPrefix(key, "client_") ||
			strings.HasPrefix(key, "tool_") ||
			strings.HasPrefix(key, "model_") ||
			strings.HasPrefix(key, "provider_") ||
			strings.HasPrefix(key, "usage_") ||
			strings.HasPrefix(key, "analytics_") {
			delete(snap.Attributes, key)
		}
	}
	for key := range snap.Diagnostics {
		if strings.HasPrefix(key, "source_") ||
			strings.HasPrefix(key, "client_") ||
			strings.HasPrefix(key, "tool_") ||
			strings.HasPrefix(key, "model_") ||
			strings.HasPrefix(key, "provider_") ||
			strings.HasPrefix(key, "usage_") ||
			strings.HasPrefix(key, "analytics_") {
			delete(snap.Diagnostics, key)
		}
	}
	for key := range snap.DailySeries {
		if strings.HasPrefix(key, "usage_model_") ||
			strings.HasPrefix(key, "usage_source_") ||
			strings.HasPrefix(key, "usage_client_") ||
			strings.HasPrefix(key, "tokens_client_") ||
			key == "analytics_cost" ||
			key == "analytics_requests" ||
			key == "analytics_tokens" {
			delete(snap.DailySeries, key)
		}
	}

	// Replace stale template ModelUsage with time-windowed records from
	// telemetry. The template's ModelUsage represents the full billing cycle
	// and would be misleading for shorter time windows.
	snap.ModelUsage = nil
	modelCostTotal := 0.0
	for _, model := range agg.Models {
		mk := sanitizeMetricID(model.Model)
		snap.Metrics["model_"+mk+"_input_tokens"] = core.Metric{Used: core.Float64Ptr(model.InputTokens), Unit: "tokens", Window: timeWindow}
		snap.Metrics["model_"+mk+"_output_tokens"] = core.Metric{Used: core.Float64Ptr(model.OutputTokens), Unit: "tokens", Window: timeWindow}
		snap.Metrics["model_"+mk+"_cached_tokens"] = core.Metric{Used: core.Float64Ptr(model.CachedTokens), Unit: "tokens", Window: timeWindow}
		snap.Metrics["model_"+mk+"_reasoning_tokens"] = core.Metric{Used: core.Float64Ptr(model.Reasoning), Unit: "tokens", Window: timeWindow}
		snap.Metrics["model_"+mk+"_cost_usd"] = core.Metric{Used: core.Float64Ptr(model.CostUSD), Unit: "USD", Window: timeWindow}
		snap.Metrics["model_"+mk+"_requests"] = core.Metric{Used: core.Float64Ptr(model.Requests), Unit: "requests", Window: timeWindow}
		snap.Metrics["model_"+mk+"_requests_today"] = core.Metric{Used: core.Float64Ptr(model.Requests1d), Unit: "requests", Window: "1d"}
		modelCostTotal += model.CostUSD
		snap.ModelUsage = append(snap.ModelUsage, core.ModelUsageRecord{
			RawModelID:      model.Model,
			RawSource:       "telemetry",
			Window:          timeWindow,
			InputTokens:     core.Float64Ptr(model.InputTokens),
			OutputTokens:    core.Float64Ptr(model.OutputTokens),
			CachedTokens:    core.Float64Ptr(model.CachedTokens),
			ReasoningTokens: core.Float64Ptr(model.Reasoning),
			TotalTokens:     core.Float64Ptr(model.TotalTokens),
			CostUSD:         core.Float64Ptr(model.CostUSD),
			Requests:        core.Float64Ptr(model.Requests),
		})
	}
	// When telemetry events lack cost attribution but the provider's API
	// supplied per-model cost data (e.g. Cursor's GetAggregatedUsageEvents),
	// restore the API model costs so the Model Burn section shows the real
	// per-model breakdown instead of a single "unattributed" entry.
	telemetryCostInsufficient := authoritativeCost > 0 && modelCostTotal < authoritativeCost*0.1
	if telemetryCostInsufficient && len(savedAPIModelCosts) > 0 {
		for key, m := range savedAPIModelCosts {
			snap.Metrics[key] = m
		}
		core.Tracef("[usage_view] %s: restored %d API model cost metrics (telemetry cost %.2f << authoritative %.2f)",
			snap.ProviderID, len(savedAPIModelCosts), modelCostTotal, authoritativeCost)
	} else if len(agg.Models) > 0 {
		// Only compute unattributed model cost when telemetry has meaningful
		// cost data. When agg.Models is empty (no events in window), the
		// authoritativeCost represents the full billing cycle — attributing
		// it as "unattributed" would be misleading for the selected time range.
		if delta := authoritativeCost - modelCostTotal; authoritativeCost > 0 && delta > 0.000001 {
			uk := "model_unattributed"
			snap.Metrics[uk+"_cost_usd"] = core.Metric{Used: core.Float64Ptr(delta), Unit: "USD", Window: timeWindow}
			snap.SetDiagnostic("telemetry_unattributed_model_cost_usd", fmt.Sprintf("%.6f", delta))
		}
	}

	if !strings.EqualFold(strings.TrimSpace(snap.ProviderID), "codex") {
		providerCostTotal := 0.0
		for _, provider := range agg.Providers {
			pk := sanitizeMetricID(provider.Provider)
			snap.Metrics["provider_"+pk+"_cost_usd"] = core.Metric{Used: core.Float64Ptr(provider.CostUSD), Unit: "USD", Window: timeWindow}
			snap.Metrics["provider_"+pk+"_input_tokens"] = core.Metric{Used: core.Float64Ptr(provider.Input), Unit: "tokens", Window: timeWindow}
			snap.Metrics["provider_"+pk+"_output_tokens"] = core.Metric{Used: core.Float64Ptr(provider.Output), Unit: "tokens", Window: timeWindow}
			snap.Metrics["provider_"+pk+"_requests"] = core.Metric{Used: core.Float64Ptr(provider.Requests), Unit: "requests", Window: timeWindow}
			providerCostTotal += provider.CostUSD
		}
		if delta := authoritativeCost - providerCostTotal; authoritativeCost > 0 && delta > 0.000001 {
			uk := "provider_unattributed"
			snap.Metrics[uk+"_cost_usd"] = core.Metric{Used: core.Float64Ptr(delta), Unit: "USD", Window: timeWindow}
			snap.SetDiagnostic("telemetry_unattributed_provider_cost_usd", fmt.Sprintf("%.6f", delta))
		}
	}

	for _, source := range agg.Sources {
		sk := sanitizeMetricID(source.Source)
		// Only emit source_*_requests_today (used by TUI's today-fallback path).
		// source_*_requests is intentionally omitted: client_*_requests covers the
		// same data, and emitting both causes the TUI to double-count requests due
		// to Go's random map iteration order.
		snap.Metrics["source_"+sk+"_requests_today"] = core.Metric{Used: core.Float64Ptr(source.Requests1d), Unit: "requests", Window: "1d"}

		snap.Metrics["client_"+sk+"_total_tokens"] = core.Metric{Used: core.Float64Ptr(source.Tokens), Unit: "tokens", Window: timeWindow}
		snap.Metrics["client_"+sk+"_input_tokens"] = core.Metric{Used: core.Float64Ptr(source.Input), Unit: "tokens", Window: timeWindow}
		snap.Metrics["client_"+sk+"_output_tokens"] = core.Metric{Used: core.Float64Ptr(source.Output), Unit: "tokens", Window: timeWindow}
		snap.Metrics["client_"+sk+"_cached_tokens"] = core.Metric{Used: core.Float64Ptr(source.Cached), Unit: "tokens", Window: timeWindow}
		snap.Metrics["client_"+sk+"_reasoning_tokens"] = core.Metric{Used: core.Float64Ptr(source.Reasoning), Unit: "tokens", Window: timeWindow}
		snap.Metrics["client_"+sk+"_requests"] = core.Metric{Used: core.Float64Ptr(source.Requests), Unit: "requests", Window: timeWindow}
		snap.Metrics["client_"+sk+"_sessions"] = core.Metric{Used: core.Float64Ptr(source.Sessions), Unit: "sessions", Window: timeWindow}
	}

	var totalToolCalls float64
	var totalToolCallsOK float64
	var totalToolCallsError float64
	var totalToolCallsAborted float64
	for _, tool := range agg.Tools {
		tk := sanitizeMetricID(tool.Tool)
		snap.Metrics["tool_"+tk] = core.Metric{Used: core.Float64Ptr(tool.Calls), Unit: "calls", Window: timeWindow}
		snap.Metrics["tool_"+tk+"_today"] = core.Metric{Used: core.Float64Ptr(tool.Calls1d), Unit: "calls", Window: "1d"}
		totalToolCalls += tool.Calls
		totalToolCallsOK += tool.CallsOK
		totalToolCallsError += tool.CallsError
		totalToolCallsAborted += tool.CallsAborted
	}
	if totalToolCalls > 0 {
		snap.Metrics["tool_calls_total"] = core.Metric{Used: core.Float64Ptr(totalToolCalls), Unit: "calls", Window: timeWindow}
		snap.Metrics["tool_completed"] = core.Metric{Used: core.Float64Ptr(totalToolCallsOK), Unit: "calls", Window: timeWindow}
		snap.Metrics["tool_errored"] = core.Metric{Used: core.Float64Ptr(totalToolCallsError), Unit: "calls", Window: timeWindow}
		snap.Metrics["tool_cancelled"] = core.Metric{Used: core.Float64Ptr(totalToolCallsAborted), Unit: "calls", Window: timeWindow}
		successRate := 0.0
		if totalToolCalls > 0 {
			successRate = (totalToolCallsOK / totalToolCalls) * 100
		}
		snap.Metrics["tool_success_rate"] = core.Metric{Used: core.Float64Ptr(successRate), Unit: "%", Window: timeWindow}
	}

	// MCP server metrics.
	var mcpTotalCalls, mcpTotalCalls1d float64
	for _, srv := range agg.MCPServers {
		sk := sanitizeMetricID(srv.Server)
		snap.Metrics["mcp_"+sk+"_total"] = core.Metric{Used: core.Float64Ptr(srv.Calls), Unit: "calls", Window: timeWindow}
		snap.Metrics["mcp_"+sk+"_total_today"] = core.Metric{Used: core.Float64Ptr(srv.Calls1d), Unit: "calls", Window: "1d"}
		mcpTotalCalls += srv.Calls
		mcpTotalCalls1d += srv.Calls1d
		for _, fn := range srv.Functions {
			fk := sanitizeMetricID(fn.Function)
			snap.Metrics["mcp_"+sk+"_"+fk] = core.Metric{Used: core.Float64Ptr(fn.Calls), Unit: "calls", Window: timeWindow}
		}
	}
	if mcpTotalCalls > 0 {
		snap.Metrics["mcp_calls_total"] = core.Metric{Used: core.Float64Ptr(mcpTotalCalls), Unit: "calls", Window: timeWindow}
		snap.Metrics["mcp_calls_total_today"] = core.Metric{Used: core.Float64Ptr(mcpTotalCalls1d), Unit: "calls", Window: "1d"}
		snap.Metrics["mcp_servers_active"] = core.Metric{Used: core.Float64Ptr(float64(len(agg.MCPServers))), Unit: "servers", Window: timeWindow}
	}

	for _, lang := range agg.Languages {
		lk := sanitizeMetricID(lang.Language)
		snap.Metrics["lang_"+lk] = core.Metric{Used: core.Float64Ptr(lang.Requests), Unit: "requests", Window: timeWindow}
	}

	// Emit windowed activity metrics.
	act := agg.Activity
	if act.Messages > 0 {
		snap.Metrics["messages_today"] = core.Metric{Used: core.Float64Ptr(act.Messages), Unit: "messages", Window: timeWindow}
	}
	if act.Sessions > 0 {
		snap.Metrics["sessions_today"] = core.Metric{Used: core.Float64Ptr(act.Sessions), Unit: "sessions", Window: timeWindow}
	}
	if act.ToolCalls > 0 {
		snap.Metrics["tool_calls_today"] = core.Metric{Used: core.Float64Ptr(act.ToolCalls), Unit: "calls", Window: timeWindow}
		snap.Metrics["7d_tool_calls"] = core.Metric{Used: core.Float64Ptr(act.ToolCalls), Unit: "calls", Window: timeWindow}
	}
	if act.InputTokens > 0 {
		snap.Metrics["today_input_tokens"] = core.Metric{Used: core.Float64Ptr(act.InputTokens), Unit: "tokens", Window: timeWindow}
	}
	if act.OutputTokens > 0 {
		snap.Metrics["today_output_tokens"] = core.Metric{Used: core.Float64Ptr(act.OutputTokens), Unit: "tokens", Window: timeWindow}
	}
	if act.TotalCost > 0 {
		snap.Metrics["today_api_cost"] = core.Metric{Used: core.Float64Ptr(act.TotalCost), Unit: "USD", Window: timeWindow}
	}

	// Emit windowed code stats.
	cs := agg.CodeStats
	if cs.FilesChanged > 0 {
		snap.Metrics["composer_files_changed"] = core.Metric{Used: core.Float64Ptr(cs.FilesChanged), Unit: "files", Window: timeWindow}
	}
	if cs.LinesAdded > 0 {
		snap.Metrics["composer_lines_added"] = core.Metric{Used: core.Float64Ptr(cs.LinesAdded), Unit: "lines", Window: timeWindow}
	}
	if cs.LinesRemoved > 0 {
		snap.Metrics["composer_lines_removed"] = core.Metric{Used: core.Float64Ptr(cs.LinesRemoved), Unit: "lines", Window: timeWindow}
	}

	// Emit window-level aggregate metrics for the TUI header/tile display.
	var windowRequests, windowCost, windowTokens float64
	for _, model := range agg.Models {
		windowRequests += model.Requests
		windowCost += model.CostUSD
		windowTokens += model.TotalTokens
	}
	if windowRequests > 0 {
		snap.Metrics["window_requests"] = core.Metric{Used: core.Float64Ptr(windowRequests), Unit: "requests", Window: timeWindow}
	}
	if windowCost > 0 {
		snap.Metrics["window_cost"] = core.Metric{Used: core.Float64Ptr(windowCost), Unit: "USD", Window: timeWindow}
	}
	if windowTokens > 0 {
		snap.Metrics["window_tokens"] = core.Metric{Used: core.Float64Ptr(windowTokens), Unit: "tokens", Window: timeWindow}
	}

	snap.DailySeries["analytics_cost"] = pointsFromDaily(agg.Daily, func(v telemetryDayPoint) float64 { return v.CostUSD })
	snap.DailySeries["analytics_requests"] = pointsFromDaily(agg.Daily, func(v telemetryDayPoint) float64 { return v.Requests })
	snap.DailySeries["analytics_tokens"] = pointsFromDaily(agg.Daily, func(v telemetryDayPoint) float64 { return v.Tokens })
	// Fixed-window cost metrics (7d_api_cost, 5h_block_cost, all_time_api_cost,
	// usage_daily, usage_weekly) are preserved from the provider template — they
	// come from the provider's Fetch() with real API data. We do NOT re-emit
	// them here because agg.Daily is already filtered to the selected time
	// window, so usageCostWindowsUTC would produce incorrect values (e.g.
	// "7d cost" would equal "3d cost" when the user picks a 3-day window).

	for model, series := range agg.ModelDaily {
		snap.DailySeries["usage_model_"+sanitizeMetricID(model)] = series
	}
	for source, series := range agg.SourceDaily {
		snap.DailySeries["usage_source_"+sanitizeMetricID(source)] = series
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

// queryTelemetryActiveProviders returns the set of provider IDs that have at least
// one telemetry event in the database, regardless of time window. This is used to
// distinguish providers that have a telemetry adapter (but may have no events in the
// current time window) from providers that have no telemetry at all.
func queryTelemetryActiveProviders(ctx context.Context, db *sql.DB) map[string]bool {
	out := make(map[string]bool)
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT LOWER(TRIM(provider_id))
		FROM usage_events
		WHERE event_type IN ('message_usage', 'tool_usage')
		  AND provider_id IS NOT NULL AND provider_id != ''
	`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var pid string
		if rows.Scan(&pid) == nil && pid != "" {
			out[pid] = true
		}
	}
	return out
}

func loadUsageViewForProviderWithSources(ctx context.Context, db *sql.DB, providerIDs []string, accountID string, timeWindowHours int) (*telemetryUsageAgg, error) {
	providerIDs = normalizeProviderIDs(providerIDs)
	if len(providerIDs) == 0 {
		return &telemetryUsageAgg{}, nil
	}
	accountID = strings.TrimSpace(accountID)

	if accountID != "" {
		scoped, err := loadUsageViewForFilter(ctx, db, usageFilter{
			ProviderIDs:     providerIDs,
			AccountID:       accountID,
			TimeWindowHours: timeWindowHours,
		})
		if err != nil {
			return nil, err
		}
		if scoped == nil {
			scoped = &telemetryUsageAgg{}
		}
		// If account-scoped query found events, use it.
		if scoped.EventCount > 0 {
			scoped.Scope = "account"
			scoped.AccountID = accountID
			return scoped, nil
		}
		// Fall through to provider-scoped query if no account-scoped events found.
	}

	fallback, err := loadUsageViewForFilter(ctx, db, usageFilter{
		ProviderIDs:     providerIDs,
		TimeWindowHours: timeWindowHours,
	})
	if err != nil {
		return nil, err
	}
	if fallback == nil {
		fallback = &telemetryUsageAgg{}
	}
	fallback.Scope = "provider"
	return fallback, nil
}

func loadUsageViewForFilter(ctx context.Context, db *sql.DB, filter usageFilter) (*telemetryUsageAgg, error) {
	filterStart := time.Now()
	agg := &telemetryUsageAgg{
		ModelDaily:   make(map[string][]core.TimePoint),
		SourceDaily:  make(map[string][]core.TimePoint),
		ClientDaily:  make(map[string][]core.TimePoint),
		ClientTokens: make(map[string][]core.TimePoint),
	}

	// Materialize the deduped CTE into a temp table so subsequent queries
	// read from a flat table instead of rebuilding the 3-level CTE each time.
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	tempTable := "_deduped_tmp"

	matStart := time.Now()
	_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTable))
	materializeSQL := fmt.Sprintf("CREATE TEMP TABLE %s AS %s SELECT * FROM deduped_usage", tempTable, usageCTE)
	if _, err := db.ExecContext(ctx, materializeSQL, whereArgs...); err != nil {
		return nil, fmt.Errorf("materialize deduped usage: %w", err)
	}
	core.Tracef("[usage_view_perf] materialize temp table: %dms (providers=%v, windowHours=%d)",
		time.Since(matStart).Milliseconds(), filter.ProviderIDs, filter.TimeWindowHours)
	defer func() {
		_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTable))
	}()

	// Create indexes on the temp table for the aggregation queries.
	_, _ = db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_deduped_event_type ON %s(event_type)", tempTable))
	_, _ = db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_deduped_occurred ON %s(occurred_at)", tempTable))

	// Count from the materialized table.
	countStart := time.Now()
	countQuery := fmt.Sprintf(`
		SELECT COALESCE(MAX(occurred_at), ''), COUNT(*)
		FROM %s
		WHERE event_type IN ('message_usage', 'tool_usage')
	`, tempTable)
	if err := db.QueryRowContext(ctx, countQuery).Scan(&agg.LastOccurred, &agg.EventCount); err != nil {
		return nil, fmt.Errorf("canonical usage count query: %w", err)
	}
	core.Tracef("[usage_view_perf] countQuery: %dms (events=%d, providers=%v, windowHours=%d)",
		time.Since(countStart).Milliseconds(), agg.EventCount, filter.ProviderIDs, filter.TimeWindowHours)
	if agg.EventCount == 0 {
		return agg, nil
	}

	// All subsequent queries use the materialized temp table.
	matFilter := filter
	matFilter.materializedTbl = tempTable

	trace := func(label string) func() {
		start := time.Now()
		return func() { core.Tracef("[usage_view_perf]   %s: %dms", label, time.Since(start).Milliseconds()) }
	}

	done := trace("queryModelAgg")
	models, err := queryModelAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("querySourceAgg")
	sources, err := querySourceAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryToolAgg")
	tools, err := queryToolAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryProviderAgg")
	providers, err := queryProviderAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryLanguageAgg")
	languages, err := queryLanguageAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryActivityAgg")
	activity, err := queryActivityAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryCodeStatsAgg")
	codeStats, err := queryCodeStatsAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyTotals")
	daily, err := queryDailyTotals(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyByDimension(model)")
	modelDaily, err := queryDailyByDimension(ctx, db, matFilter, "model")
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyByDimension(source)")
	sourceDaily, err := queryDailyByDimension(ctx, db, matFilter, "source")
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyByDimension(client)")
	clientDaily, err := queryDailyByDimension(ctx, db, matFilter, "client")
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyClientTokens")
	clientTokens, err := queryDailyClientTokens(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}

	agg.Models = models
	agg.Providers = providers
	agg.Sources = sources
	agg.Tools = tools
	agg.MCPServers = buildMCPAgg(tools)
	agg.Languages = languages
	agg.Activity = activity
	agg.CodeStats = codeStats
	agg.Daily = daily
	agg.ModelDaily = modelDaily
	agg.SourceDaily = sourceDaily
	agg.ClientDaily = clientDaily
	agg.ClientTokens = clientTokens
	core.Tracef("[usage_view_perf] loadUsageViewForFilter TOTAL: %dms (providers=%v)", time.Since(filterStart).Milliseconds(), filter.ProviderIDs)
	return agg, nil
}

func queryModelAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryModelAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			COALESCE(NULLIF(TRIM(COALESCE(model_canonical, model_raw)), ''), 'unknown') AS model_key,
			SUM(COALESCE(input_tokens, 0)) AS input_tokens,
			SUM(COALESCE(output_tokens, 0)) AS output_tokens,
			SUM(COALESCE(cache_read_tokens, 0) + COALESCE(cache_write_tokens, 0)) AS cached_tokens,
			SUM(COALESCE(reasoning_tokens, 0)) AS reasoning_tokens,
			SUM(COALESCE(total_tokens,
				COALESCE(input_tokens, 0) +
				COALESCE(output_tokens, 0) +
				COALESCE(reasoning_tokens, 0) +
				COALESCE(cache_read_tokens, 0) +
				COALESCE(cache_write_tokens, 0))) AS total_tokens,
			SUM(COALESCE(cost_usd, 0)) AS cost_usd,
			SUM(COALESCE(requests, 1)) AS requests,
			SUM(CASE WHEN date(occurred_at) = date('now') THEN COALESCE(requests, 1) ELSE 0 END) AS requests_today
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'
		GROUP BY model_key
		ORDER BY total_tokens DESC, requests DESC
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage model query: %w", err)
	}
	defer rows.Close()

	var out []telemetryModelAgg
	for rows.Next() {
		var row telemetryModelAgg
		if err := rows.Scan(
			&row.Model,
			&row.InputTokens,
			&row.OutputTokens,
			&row.CachedTokens,
			&row.Reasoning,
			&row.TotalTokens,
			&row.CostUSD,
			&row.Requests,
			&row.Requests1d,
		); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func querySourceAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetrySourceAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			` + clientDimensionExpr() + ` AS source_name,
			SUM(COALESCE(requests, 1)) AS requests,
			SUM(CASE WHEN date(occurred_at) = date('now') THEN COALESCE(requests, 1) ELSE 0 END) AS requests_today,
			SUM(COALESCE(total_tokens,
				COALESCE(input_tokens, 0) +
				COALESCE(output_tokens, 0) +
				COALESCE(reasoning_tokens, 0) +
				COALESCE(cache_read_tokens, 0) +
				COALESCE(cache_write_tokens, 0))) AS total_tokens,
			SUM(COALESCE(input_tokens, 0)) AS input_tokens,
			SUM(COALESCE(output_tokens, 0)) AS output_tokens,
			SUM(COALESCE(cache_read_tokens, 0) + COALESCE(cache_write_tokens, 0)) AS cached_tokens,
			SUM(COALESCE(reasoning_tokens, 0)) AS reasoning_tokens,
			COUNT(DISTINCT COALESCE(NULLIF(TRIM(session_id), ''), 'unknown')) AS sessions
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'
		GROUP BY source_name
		ORDER BY requests DESC
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage source query: %w", err)
	}
	defer rows.Close()

	var out []telemetrySourceAgg
	for rows.Next() {
		var row telemetrySourceAgg
		if err := rows.Scan(
			&row.Source,
			&row.Requests,
			&row.Requests1d,
			&row.Tokens,
			&row.Input,
			&row.Output,
			&row.Cached,
			&row.Reasoning,
			&row.Sessions,
		); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func queryToolAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryToolAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			COALESCE(NULLIF(TRIM(LOWER(tool_name)), ''), 'unknown') AS tool_name,
			SUM(COALESCE(requests, 1)) AS calls,
			SUM(CASE WHEN date(occurred_at) = date('now') THEN COALESCE(requests, 1) ELSE 0 END) AS calls_today,
			SUM(CASE WHEN status = 'ok' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_ok,
			SUM(CASE WHEN date(occurred_at) = date('now') AND status = 'ok' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_ok_today,
			SUM(CASE WHEN status = 'error' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_error,
			SUM(CASE WHEN date(occurred_at) = date('now') AND status = 'error' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_error_today,
			SUM(CASE WHEN status = 'aborted' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_aborted,
			SUM(CASE WHEN date(occurred_at) = date('now') AND status = 'aborted' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_aborted_today
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'tool_usage'
		GROUP BY tool_name
		ORDER BY calls DESC
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage tool query: %w", err)
	}
	defer rows.Close()

	var out []telemetryToolAgg
	for rows.Next() {
		var row telemetryToolAgg
		if err := rows.Scan(
			&row.Tool,
			&row.Calls,
			&row.Calls1d,
			&row.CallsOK,
			&row.CallsOK1d,
			&row.CallsError,
			&row.CallsError1d,
			&row.CallsAborted,
			&row.CallsAborted1d,
		); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func queryLanguageAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryLanguageAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	// Query file paths from usage events. Language is inferred in Go
	// from the file extension since SQLite lacks convenient path functions.
	//
	// File paths live in different locations depending on the source:
	//   - JSONL collector:  $.file or $.payload.file
	//   - Hook events:      $.tool_input.file_path (Read/Edit/Write)
	//                       $.tool_input.path (Grep/Glob)
	//   - Hook response:    $.tool_response.file.filePath (Read response)
	//   - Cursor tracking:  $.file or $.file_extension (message_usage events)
	query := usageCTE + `
		SELECT
			COALESCE(
				NULLIF(TRIM(json_extract(source_payload, '$.file')), ''),
				NULLIF(TRIM(json_extract(source_payload, '$.payload.file')), ''),
				NULLIF(TRIM(json_extract(source_payload, '$.tool_input.file_path')), ''),
				NULLIF(TRIM(json_extract(source_payload, '$.tool_input.path')), ''),
				NULLIF(TRIM(json_extract(source_payload, '$.tool_response.file.filePath')), ''),
				NULLIF(TRIM(json_extract(source_payload, '$.file_extension')), ''),
				''
			) AS file_path,
			COALESCE(requests, 1) AS requests
		FROM deduped_usage
		WHERE event_type IN ('tool_usage', 'message_usage')
		  AND status != 'error'
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage language query: %w", err)
	}
	defer rows.Close()

	langCounts := make(map[string]float64)
	for rows.Next() {
		var filePath string
		var requests float64
		if err := rows.Scan(&filePath, &requests); err != nil {
			continue
		}
		lang := inferLanguageFromFilePath(filePath)
		if lang != "" {
			langCounts[lang] += requests
		}
	}

	out := make([]telemetryLanguageAgg, 0, len(langCounts))
	for lang, count := range langCounts {
		out = append(out, telemetryLanguageAgg{Language: lang, Requests: count})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Requests > out[j].Requests
	})
	return out, nil
}

// inferLanguageFromFilePath maps a file path, file extension, or bare
// extension string to a programming language name.
func inferLanguageFromFilePath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	// Check base name for special files.
	base := p
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		base = p[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx >= 0 {
		base = base[idx+1:]
	}
	switch strings.ToLower(base) {
	case "dockerfile":
		return "docker"
	case "makefile":
		return "make"
	}
	// Check file extension.
	idx := strings.LastIndex(p, ".")
	if idx < 0 {
		// Handle bare extension without dot (e.g., "go", "py" from file_extension fields).
		if lang := extToLanguage("." + strings.ToLower(p)); lang != "" {
			return lang
		}
		return ""
	}
	ext := strings.ToLower(p[idx:])
	return extToLanguage(ext)
}

// extToLanguage maps a dotted file extension to a language name.
func extToLanguage(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".tf", ".tfvars", ".hcl":
		return "terraform"
	case ".sh", ".bash", ".zsh", ".fish":
		return "shell"
	case ".md", ".mdx":
		return "markdown"
	case ".json":
		return "json"
	case ".yml", ".yaml":
		return "yaml"
	case ".sql":
		return "sql"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".cs":
		return "csharp"
	case ".vue":
		return "vue"
	case ".svelte":
		return "svelte"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	case ".css", ".scss", ".less":
		return "css"
	case ".html", ".htm":
		return "html"
	case ".dart":
		return "dart"
	case ".zig":
		return "zig"
	case ".lua":
		return "lua"
	case ".r":
		return "r"
	case ".proto":
		return "protobuf"
	case ".ex", ".exs":
		return "elixir"
	case ".graphql", ".gql":
		return "graphql"
	}
	return ""
}

func queryProviderAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryProviderAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	// Provider resolution order:
	// 1) hook-enriched upstream provider from source payload (if present),
	// 2) fallback to provider_id.
	//
	// Provider hosting names must come from real payload fields, not inferred
	// model-id heuristics.
	query := usageCTE + `
		SELECT
			COALESCE(
				NULLIF(TRIM(
					COALESCE(
						json_extract(source_payload, '$._normalized.upstream_provider'),
						json_extract(source_payload, '$.upstream_provider'),
						json_extract(source_payload, '$.payload._normalized.upstream_provider'),
						json_extract(source_payload, '$.payload.upstream_provider'),
						''
					)
				), ''),
				COALESCE(NULLIF(TRIM(provider_id), ''), 'unknown')
			) AS provider_name,
			SUM(COALESCE(cost_usd, 0)) AS cost_usd,
			SUM(COALESCE(requests, 1)) AS requests,
			SUM(COALESCE(input_tokens, 0)) AS input_tokens,
			SUM(COALESCE(output_tokens, 0)) AS output_tokens
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'
		GROUP BY provider_name
		ORDER BY cost_usd DESC, requests DESC
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage provider query: %w", err)
	}
	defer rows.Close()

	var out []telemetryProviderAgg
	for rows.Next() {
		var row telemetryProviderAgg
		if err := rows.Scan(&row.Provider, &row.CostUSD, &row.Requests, &row.Input, &row.Output); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func queryActivityAgg(ctx context.Context, db *sql.DB, filter usageFilter) (telemetryActivityAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			COUNT(DISTINCT CASE WHEN event_type = 'message_usage' AND status != 'error' THEN
				COALESCE(NULLIF(TRIM(message_id), ''), COALESCE(NULLIF(TRIM(turn_id), ''), dedup_key))
			END) AS messages,
			COUNT(DISTINCT CASE WHEN event_type = 'message_usage' AND status != 'error' THEN
				NULLIF(TRIM(session_id), '')
			END) AS sessions,
			SUM(CASE WHEN event_type = 'tool_usage' THEN COALESCE(requests, 1) ELSE 0 END) AS tool_calls,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(input_tokens, 0) ELSE 0 END) AS input_tokens,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(output_tokens, 0) ELSE 0 END) AS output_tokens,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(cache_read_tokens, 0) ELSE 0 END) AS cached_tokens,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(reasoning_tokens, 0) ELSE 0 END) AS reasoning_tokens,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(total_tokens, 0) ELSE 0 END) AS total_tokens,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(cost_usd, 0) ELSE 0 END) AS total_cost
		FROM deduped_usage
		WHERE 1=1
	`
	var out telemetryActivityAgg
	err := db.QueryRowContext(ctx, query, whereArgs...).Scan(
		&out.Messages, &out.Sessions, &out.ToolCalls,
		&out.InputTokens, &out.OutputTokens, &out.CachedTokens,
		&out.ReasonTokens, &out.TotalTokens, &out.TotalCost,
	)
	if err != nil {
		return out, fmt.Errorf("canonical usage activity query: %w", err)
	}
	return out, nil
}

func queryCodeStatsAgg(ctx context.Context, db *sql.DB, filter usageFilter) (telemetryCodeStatsAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	// Count distinct file paths from tool_usage events to estimate files changed.
	// Only count mutating tools (edit, write, create, delete, rename, move).
	// Also sum lines_added/lines_removed from message_usage event payloads
	// (e.g. Cursor composer sessions store these).
	query := usageCTE + `
		SELECT
			COUNT(DISTINCT CASE
				WHEN event_type = 'tool_usage'
				  AND (LOWER(tool_name) LIKE '%edit%'
				  OR LOWER(tool_name) LIKE '%write%'
				  OR LOWER(tool_name) LIKE '%create%'
				  OR LOWER(tool_name) LIKE '%delete%'
				  OR LOWER(tool_name) LIKE '%rename%'
				  OR LOWER(tool_name) LIKE '%move%')
				THEN NULLIF(TRIM(COALESCE(
					json_extract(source_payload, '$.file'),
					json_extract(source_payload, '$.payload.file'),
					json_extract(source_payload, '$.tool_input.file_path'),
					json_extract(source_payload, '$.tool_input.path'),
					''
				)), '')
			END) AS files_changed,
			SUM(COALESCE(CAST(json_extract(source_payload, '$.lines_added') AS REAL), 0)) AS lines_added,
			SUM(COALESCE(CAST(json_extract(source_payload, '$.lines_removed') AS REAL), 0)) AS lines_removed
		FROM deduped_usage
		WHERE event_type IN ('tool_usage', 'message_usage')
		  AND status != 'error'
	`
	var out telemetryCodeStatsAgg
	err := db.QueryRowContext(ctx, query, whereArgs...).Scan(&out.FilesChanged, &out.LinesAdded, &out.LinesRemoved)
	if err != nil {
		return out, fmt.Errorf("canonical usage code stats query: %w", err)
	}
	return out, nil
}

func queryDailyTotals(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryDayPoint, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	dailyTimeFilter := ""
	if filter.TimeWindowHours <= 0 {
		dailyTimeFilter = "\n\t\t\t  AND occurred_at >= datetime('now', '-30 day')"
	}
	query := usageCTE + fmt.Sprintf(`
		SELECT
			date(occurred_at) AS day,
			SUM(COALESCE(cost_usd, 0)) AS cost_usd,
			SUM(COALESCE(requests, 1)) AS requests,
			SUM(COALESCE(total_tokens,
				COALESCE(input_tokens, 0) +
				COALESCE(output_tokens, 0) +
				COALESCE(reasoning_tokens, 0) +
				COALESCE(cache_read_tokens, 0) +
				COALESCE(cache_write_tokens, 0))) AS tokens
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'%s
		GROUP BY day
		ORDER BY day ASC
	`, dailyTimeFilter)
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage daily query: %w", err)
	}
	defer rows.Close()

	var out []telemetryDayPoint
	for rows.Next() {
		var row telemetryDayPoint
		if err := rows.Scan(&row.Day, &row.CostUSD, &row.Requests, &row.Tokens); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func queryDailyByDimension(ctx context.Context, db *sql.DB, filter usageFilter, dimension string) (map[string][]core.TimePoint, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	dailyTimeFilter := ""
	if filter.TimeWindowHours <= 0 {
		dailyTimeFilter = "\n\t\t\t  AND occurred_at >= datetime('now', '-30 day')"
	}
	var query string

	switch dimension {
	case "model":
		query = usageCTE + fmt.Sprintf(`
			SELECT date(occurred_at) AS day,
			       COALESCE(NULLIF(TRIM(COALESCE(model_canonical, model_raw)), ''), 'unknown') AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'%s
			GROUP BY day, dim_key
		`, dailyTimeFilter)
	case "source":
		query = usageCTE + fmt.Sprintf(`
			SELECT date(occurred_at) AS day,
			       COALESCE(NULLIF(TRIM(workspace_id), ''), COALESCE(NULLIF(TRIM(source_system), ''), 'unknown')) AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'%s
			GROUP BY day, dim_key
		`, dailyTimeFilter)
	case "client":
		query = usageCTE + fmt.Sprintf(`
			SELECT date(occurred_at) AS day,
			       %s AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'%s
			GROUP BY day, dim_key
		`, clientDimensionExpr(), dailyTimeFilter)
	default:
		return map[string][]core.TimePoint{}, nil
	}

	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage daily dimension query (%s): %w", dimension, err)
	}
	defer rows.Close()

	byDim := make(map[string]map[string]float64)
	for rows.Next() {
		var day, key string
		var value float64
		if err := rows.Scan(&day, &key, &value); err != nil {
			continue
		}
		key = sanitizeMetricID(key)
		if key == "" {
			key = "unknown"
		}
		if byDim[key] == nil {
			byDim[key] = make(map[string]float64)
		}
		byDim[key][day] += value
	}

	out := make(map[string][]core.TimePoint, len(byDim))
	for key, dayMap := range byDim {
		out[key] = sortedSeriesFromByDay(dayMap)
	}
	return out, nil
}

func queryDailyClientTokens(ctx context.Context, db *sql.DB, filter usageFilter) (map[string][]core.TimePoint, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	dailyTimeFilter := ""
	if filter.TimeWindowHours <= 0 {
		dailyTimeFilter = "\n\t\t\t  AND occurred_at >= datetime('now', '-30 day')"
	}
	query := usageCTE + fmt.Sprintf(`
		SELECT
			date(occurred_at) AS day,
			%s AS source_name,
			SUM(COALESCE(total_tokens,
				COALESCE(input_tokens, 0) +
				COALESCE(output_tokens, 0) +
				COALESCE(reasoning_tokens, 0) +
				COALESCE(cache_read_tokens, 0) +
				COALESCE(cache_write_tokens, 0))) AS tokens
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'%s
		GROUP BY day, source_name
	`, clientDimensionExpr(), dailyTimeFilter)
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage daily client token query: %w", err)
	}
	defer rows.Close()

	byClient := make(map[string]map[string]float64)
	for rows.Next() {
		var day, client string
		var value float64
		if err := rows.Scan(&day, &client, &value); err != nil {
			continue
		}
		client = sanitizeMetricID(client)
		if client == "" {
			client = "unknown"
		}
		if byClient[client] == nil {
			byClient[client] = make(map[string]float64)
		}
		byClient[client][day] += value
	}

	out := make(map[string][]core.TimePoint, len(byClient))
	for key, dayMap := range byClient {
		out[key] = sortedSeriesFromByDay(dayMap)
	}
	return out, nil
}

func dedupedUsageCTE(filter usageFilter) (string, []any) {
	// If a materialized temp table exists, just alias it — no CTE rebuild needed.
	if filter.materializedTbl != "" {
		return fmt.Sprintf(`WITH deduped_usage AS (SELECT * FROM %s) `, filter.materializedTbl), nil
	}
	where, args := usageWhereClause("e", filter)
	cte := fmt.Sprintf(`
		WITH scoped_usage AS (
			SELECT
				e.*,
				COALESCE(r.source_system, '') AS source_system,
				COALESCE(r.source_channel, '') AS source_channel,
				COALESCE(r.source_payload, '{}') AS source_payload
			FROM usage_events e
			JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
			WHERE %s
			  AND e.event_type IN ('message_usage', 'tool_usage')
		),
		ranked_usage AS (
			SELECT
				scoped_usage.*,
					CASE
						WHEN COALESCE(NULLIF(TRIM(tool_call_id), ''), '') != '' THEN 'tool:' || LOWER(TRIM(tool_call_id))
						WHEN LOWER(TRIM(event_type)) = 'message_usage'
							AND LOWER(TRIM(source_system)) = 'codex'
							AND COALESCE(NULLIF(TRIM(turn_id), ''), '') != ''
						THEN 'message_turn:' || LOWER(TRIM(turn_id))
						WHEN COALESCE(NULLIF(TRIM(message_id), ''), '') != '' THEN 'message:' || LOWER(TRIM(message_id))
						WHEN COALESCE(NULLIF(TRIM(turn_id), ''), '') != '' THEN 'turn:' || LOWER(TRIM(turn_id))
						ELSE 'fallback:' || dedup_key
					END AS logical_event_id,
				CASE COALESCE(NULLIF(TRIM(source_channel), ''), '')
					WHEN 'hook' THEN 4
					WHEN 'sse' THEN 3
					WHEN 'sqlite' THEN 2
					WHEN 'jsonl' THEN 2
					WHEN 'api' THEN 1
					ELSE 0
				END AS source_priority,
				(
					CASE WHEN COALESCE(total_tokens, 0) > 0 THEN 4 ELSE 0 END +
					CASE WHEN COALESCE(cost_usd, 0) > 0 THEN 2 ELSE 0 END +
					CASE WHEN COALESCE(NULLIF(TRIM(COALESCE(model_canonical, model_raw)), ''), '') != '' THEN 1 ELSE 0 END +
					CASE
						WHEN COALESCE(NULLIF(TRIM(provider_id), ''), '') != ''
							AND LOWER(TRIM(provider_id)) NOT IN ('unknown', 'opencode')
						THEN 1
						ELSE 0
					END
				) AS quality_score
			FROM scoped_usage
		),
		deduped_usage AS (
			SELECT *
			FROM (
				SELECT
					ranked_usage.*,
					ROW_NUMBER() OVER (
						PARTITION BY
							LOWER(TRIM(source_system)),
							LOWER(TRIM(event_type)),
							LOWER(TRIM(COALESCE(session_id, ''))),
							logical_event_id
						ORDER BY source_priority DESC, quality_score DESC, occurred_at DESC, event_id DESC
					) AS rn
				FROM ranked_usage
			)
			WHERE rn = 1
		)
		`, where)
	return cte, args
}

func usageWhereClause(alias string, filter usageFilter) (string, []any) {
	prefix := ""
	if strings.TrimSpace(alias) != "" {
		prefix = strings.TrimSpace(alias) + "."
	}
	providerIDs := normalizeProviderIDs(filter.ProviderIDs)
	if len(providerIDs) == 0 {
		return prefix + "provider_id = ''", nil
	}
	where := ""
	args := make([]any, 0, len(providerIDs)+1)
	if len(providerIDs) == 1 {
		where = prefix + "provider_id = ?"
		args = append(args, providerIDs[0])
	} else {
		placeholders := make([]string, 0, len(providerIDs))
		for _, providerID := range providerIDs {
			placeholders = append(placeholders, "?")
			args = append(args, providerID)
		}
		where = prefix + "provider_id IN (" + strings.Join(placeholders, ",") + ")"
	}
	if strings.TrimSpace(filter.AccountID) != "" {
		where += " AND " + prefix + "account_id = ?"
		args = append(args, strings.TrimSpace(filter.AccountID))
	}
	if filter.TimeWindowHours > 0 {
		where += fmt.Sprintf(" AND %soccurred_at >= datetime('now', '-%d hour')", prefix, filter.TimeWindowHours)
	}
	return where, args
}

func normalizeProviderIDs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	normalized := lo.Map(in, func(s string, _ int) string {
		return strings.ToLower(strings.TrimSpace(s))
	})
	result := lo.Uniq(lo.Compact(normalized))
	sort.Strings(result)
	return result
}

func pointsFromDaily(in []telemetryDayPoint, pick func(telemetryDayPoint) float64) []core.TimePoint {
	return lo.Map(in, func(row telemetryDayPoint, _ int) core.TimePoint {
		return core.TimePoint{Date: row.Day, Value: pick(row)}
	})
}

// isStaleActivityMetric returns true for metrics that are computed by the provider
// with hardcoded time windows (today/7d/all-time) and should be replaced by
// telemetry-windowed equivalents.
func isStaleActivityMetric(key string) bool {
	// Activity counters with hardcoded time windows.
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
	}
	// Fixed-window cost metrics from provider Fetch() are preserved —
	// the telemetry view does NOT re-emit them (it only has windowed data).
	switch key {
	case "7d_api_cost", "all_time_api_cost", "5h_block_cost":
		return false
	}
	// Prefixed tokens/cost metrics from providers.
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

// isCurrentStateMetric returns true for metrics that represent the current state
// of a plan, billing cycle, or team — values that are always "latest" regardless
// of time window. These are preserved even when stripping all-time cumulative
// metrics for windowed views.
func isCurrentStateMetric(key string) bool {
	if strings.HasPrefix(key, "plan_") ||
		strings.HasPrefix(key, "billing_") ||
		strings.HasPrefix(key, "team_") ||
		strings.HasPrefix(key, "spend_") ||
		strings.HasPrefix(key, "individual_") {
		return true
	}
	switch key {
	case "today_cost",
		"7d_api_cost", "all_time_api_cost", "5h_block_cost",
		"usage_daily", "usage_weekly", "usage_five_hour":
		return true
	}
	return false
}

func usageAuthoritativeCost(snap core.UsageSnapshot) float64 {
	if m, ok := snap.Metrics["credit_balance"]; ok && m.Used != nil && *m.Used > 0 {
		return *m.Used
	}
	if m, ok := snap.Metrics["spend_limit"]; ok && m.Used != nil && *m.Used > 0 {
		return *m.Used
	}
	if m, ok := snap.Metrics["plan_total_spend_usd"]; ok && m.Used != nil && *m.Used > 0 {
		return *m.Used
	}
	if m, ok := snap.Metrics["credits"]; ok && m.Used != nil && *m.Used > 0 {
		return *m.Used
	}
	return 0
}

func sortedSeriesFromByDay(byDay map[string]float64) []core.TimePoint {
	days := lo.Keys(byDay)
	sort.Strings(days)

	out := make([]core.TimePoint, 0, len(days))
	for _, day := range days {
		out = append(out, core.TimePoint{
			Date:  day,
			Value: byDay[day],
		})
	}
	return out
}

// parseMCPToolName extracts server and function from an MCP tool name.
// Raw tool names use double underscores: mcp__server__function.
// Returns ("", "", false) for non-MCP tools.
// parseMCPToolName extracts server and function from an MCP tool name.
// Supports two formats:
//   - Canonical: "mcp__server__function" (double underscores, from Claude Code and normalized Cursor)
//   - Legacy:    "server-function (mcp)" or "user-server-function (mcp)" (old Cursor data)
//
// Returns ("", "", false) for non-MCP tools.
func parseMCPToolName(raw string) (server, function string, ok bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))

	// Canonical format: mcp__server__function
	if strings.HasPrefix(raw, "mcp__") {
		rest := raw[5:]
		idx := strings.Index(rest, "__")
		if idx < 0 {
			return rest, "", true
		}
		return rest[:idx], rest[idx+2:], true
	}

	// Copilot legacy wrapper format: "<server>_mcp_server_<function>".
	if strings.Contains(raw, "_mcp_server_") {
		parts := strings.SplitN(raw, "_mcp_server_", 2)
		server = sanitizeMCPToolSegment(parts[0])
		function = sanitizeMCPToolSegment(parts[1])
		if server != "" && function != "" {
			return server, function, true
		}
	}

	// Copilot legacy wrapper format variant: "<server>-mcp-server-<function>".
	if strings.Contains(raw, "-mcp-server-") {
		parts := strings.SplitN(raw, "-mcp-server-", 2)
		server = sanitizeMCPToolSegment(parts[0])
		function = sanitizeMCPToolSegment(parts[1])
		if server != "" && function != "" {
			return server, function, true
		}
	}

	// Legacy format: "something (mcp)" from old Cursor data.
	if strings.HasSuffix(raw, " (mcp)") {
		body := strings.TrimSuffix(raw, " (mcp)")
		body = strings.TrimSpace(body)
		if body == "" {
			return "", "", false
		}

		// Strip "user-" prefix if present.
		body = strings.TrimPrefix(body, "user-")

		// Try to extract server from "server-function" format.
		// e.g., "kubernetes-pods_log" → server=kubernetes, function=pods_log
		// e.g., "gcp-gcloud-run_gcloud_command" → server=gcp-gcloud, function=run_gcloud_command
		// Heuristic: the function part typically contains underscores, so split on the
		// last hyphen that precedes an underscore-containing segment.
		if idx := findServerFunctionSplit(body); idx > 0 {
			return body[:idx], body[idx+1:], true
		}

		// No clear server-function split — treat whole body as function with unknown server.
		return "other", body, true
	}

	return "", "", false
}

func sanitizeMCPToolSegment(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	lastUnderscore := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// findServerFunctionSplit finds the best hyphen position to split "server-function"
// in a Cursor MCP tool name. The function name typically contains underscores
// (e.g., "pods_list", "search_docs") while server names use hyphens.
// Strategy: find the last hyphen where the part AFTER it contains an underscore.
// This handles multi-segment server names like "gcp-gcloud" or "runai-docs".
func findServerFunctionSplit(s string) int {
	bestIdx := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			rest := s[i+1:]
			if strings.Contains(rest, "_") {
				bestIdx = i
			}
		}
	}
	if bestIdx > 0 {
		return bestIdx
	}

	// No underscore-based split found. Fall back to first hyphen if no more hyphens after.
	// e.g., "kubernetes-pods_log" or "smart-query"
	if idx := strings.Index(s, "-"); idx > 0 {
		return idx
	}
	return -1
}

func buildMCPAgg(tools []telemetryToolAgg) []telemetryMCPServerAgg {
	type serverData struct {
		calls   float64
		calls1d float64
		funcs   map[string]*telemetryMCPFunctionAgg
	}
	servers := make(map[string]*serverData)

	for _, tool := range tools {
		server, function, ok := parseMCPToolName(tool.Tool)
		if !ok || server == "" {
			continue
		}
		sd, exists := servers[server]
		if !exists {
			sd = &serverData{funcs: make(map[string]*telemetryMCPFunctionAgg)}
			servers[server] = sd
		}
		sd.calls += tool.Calls
		sd.calls1d += tool.Calls1d
		if function != "" {
			if f, ok := sd.funcs[function]; ok {
				f.Calls += tool.Calls
				f.Calls1d += tool.Calls1d
			} else {
				sd.funcs[function] = &telemetryMCPFunctionAgg{
					Function: function,
					Calls:    tool.Calls,
					Calls1d:  tool.Calls1d,
				}
			}
		}
	}

	result := make([]telemetryMCPServerAgg, 0, len(servers))
	for name, sd := range servers {
		var funcs []telemetryMCPFunctionAgg
		for _, f := range sd.funcs {
			funcs = append(funcs, *f)
		}
		sort.Slice(funcs, func(i, j int) bool {
			if funcs[i].Calls != funcs[j].Calls {
				return funcs[i].Calls > funcs[j].Calls
			}
			return funcs[i].Function < funcs[j].Function
		})
		result = append(result, telemetryMCPServerAgg{
			Server:    name,
			Calls:     sd.calls,
			Calls1d:   sd.calls1d,
			Functions: funcs,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Calls != result[j].Calls {
			return result[i].Calls > result[j].Calls
		}
		return result[i].Server < result[j].Server
	})
	return result
}

func sanitizeMetricID(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(raw))
	lastUnderscore := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}
