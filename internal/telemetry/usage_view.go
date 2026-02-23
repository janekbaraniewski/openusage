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
	Tool    string
	Calls   float64
	Calls1d float64
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

type telemetryUsageAgg struct {
	LastOccurred string
	EventCount   int64
	Scope        string
	AccountID    string
	Models       []telemetryModelAgg
	Providers    []telemetryProviderAgg
	Sources      []telemetrySourceAgg
	Tools        []telemetryToolAgg
	Daily        []telemetryDayPoint
	ModelDaily   map[string][]core.TimePoint
	SourceDaily  map[string][]core.TimePoint
	ClientDaily  map[string][]core.TimePoint
	ClientTokens map[string][]core.TimePoint
}

type usageFilter struct {
	ProviderIDs []string
	AccountID   string
}

func applyCanonicalUsageViewWithDB(
	ctx context.Context,
	db *sql.DB,
	snaps map[string]core.UsageSnapshot,
	providerLinks map[string]string,
) (map[string]core.UsageSnapshot, error) {
	if db == nil {
		return snaps, nil
	}

	out := make(map[string]core.UsageSnapshot, len(snaps))
	cache := make(map[string]*telemetryUsageAgg)

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
			loaded, loadErr := loadUsageViewForProviderWithSources(ctx, db, sourceProviders, accountScope)
			if loadErr != nil {
				return snaps, loadErr
			}
			cache[cacheKey] = loaded
			agg = loaded
		}
		if agg == nil || agg.EventCount == 0 {
			out[accountID] = s
			continue
		}

		applyUsageViewToSnapshot(&s, agg)
		out[accountID] = s
	}

	return out, nil
}

func applyUsageViewToSnapshot(snap *core.UsageSnapshot, agg *telemetryUsageAgg) {
	if snap == nil || agg == nil {
		return
	}
	authoritativeCost := usageAuthoritativeCost(*snap)
	snap.EnsureMaps()
	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	for key := range snap.Metrics {
		if strings.HasPrefix(key, "source_") ||
			strings.HasPrefix(key, "client_") ||
			strings.HasPrefix(key, "tool_") ||
			strings.HasPrefix(key, "model_") ||
			strings.HasPrefix(key, "provider_") {
			delete(snap.Metrics, key)
		}
	}
	for key := range snap.Raw {
		if strings.HasPrefix(key, "source_") ||
			strings.HasPrefix(key, "client_") ||
			strings.HasPrefix(key, "tool_") ||
			strings.HasPrefix(key, "model_") ||
			strings.HasPrefix(key, "provider_") ||
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

	modelCostTotal := 0.0
	for _, model := range agg.Models {
		mk := sanitizeMetricID(model.Model)
		snap.Metrics["model_"+mk+"_input_tokens"] = core.Metric{Used: float64Ptr(model.InputTokens), Unit: "tokens", Window: "all"}
		snap.Metrics["model_"+mk+"_output_tokens"] = core.Metric{Used: float64Ptr(model.OutputTokens), Unit: "tokens", Window: "all"}
		snap.Metrics["model_"+mk+"_cached_tokens"] = core.Metric{Used: float64Ptr(model.CachedTokens), Unit: "tokens", Window: "all"}
		snap.Metrics["model_"+mk+"_reasoning_tokens"] = core.Metric{Used: float64Ptr(model.Reasoning), Unit: "tokens", Window: "all"}
		snap.Metrics["model_"+mk+"_cost_usd"] = core.Metric{Used: float64Ptr(model.CostUSD), Unit: "USD", Window: "all"}
		snap.Metrics["model_"+mk+"_requests"] = core.Metric{Used: float64Ptr(model.Requests), Unit: "requests", Window: "all"}
		snap.Metrics["model_"+mk+"_requests_today"] = core.Metric{Used: float64Ptr(model.Requests1d), Unit: "requests", Window: "1d"}
		modelCostTotal += model.CostUSD
	}
	if delta := authoritativeCost - modelCostTotal; authoritativeCost > 0 && delta > 0.000001 {
		uk := "model_unattributed"
		snap.Metrics[uk+"_cost_usd"] = core.Metric{Used: float64Ptr(delta), Unit: "USD", Window: "all"}
		snap.SetDiagnostic("telemetry_unattributed_model_cost_usd", fmt.Sprintf("%.6f", delta))
	}

	providerCostTotal := 0.0
	for _, provider := range agg.Providers {
		pk := sanitizeMetricID(provider.Provider)
		snap.Metrics["provider_"+pk+"_cost_usd"] = core.Metric{Used: float64Ptr(provider.CostUSD), Unit: "USD", Window: "all"}
		snap.Metrics["provider_"+pk+"_input_tokens"] = core.Metric{Used: float64Ptr(provider.Input), Unit: "tokens", Window: "all"}
		snap.Metrics["provider_"+pk+"_output_tokens"] = core.Metric{Used: float64Ptr(provider.Output), Unit: "tokens", Window: "all"}
		snap.Metrics["provider_"+pk+"_requests"] = core.Metric{Used: float64Ptr(provider.Requests), Unit: "requests", Window: "all"}
		providerCostTotal += provider.CostUSD
	}
	if delta := authoritativeCost - providerCostTotal; authoritativeCost > 0 && delta > 0.000001 {
		uk := "provider_unattributed"
		snap.Metrics[uk+"_cost_usd"] = core.Metric{Used: float64Ptr(delta), Unit: "USD", Window: "all"}
		snap.SetDiagnostic("telemetry_unattributed_provider_cost_usd", fmt.Sprintf("%.6f", delta))
	}

	for _, source := range agg.Sources {
		sk := sanitizeMetricID(source.Source)
		snap.Metrics["source_"+sk+"_requests"] = core.Metric{Used: float64Ptr(source.Requests), Unit: "requests", Window: "all"}
		snap.Metrics["source_"+sk+"_requests_today"] = core.Metric{Used: float64Ptr(source.Requests1d), Unit: "requests", Window: "1d"}

		snap.Metrics["client_"+sk+"_total_tokens"] = core.Metric{Used: float64Ptr(source.Tokens), Unit: "tokens", Window: "all"}
		snap.Metrics["client_"+sk+"_input_tokens"] = core.Metric{Used: float64Ptr(source.Input), Unit: "tokens", Window: "all"}
		snap.Metrics["client_"+sk+"_output_tokens"] = core.Metric{Used: float64Ptr(source.Output), Unit: "tokens", Window: "all"}
		snap.Metrics["client_"+sk+"_cached_tokens"] = core.Metric{Used: float64Ptr(source.Cached), Unit: "tokens", Window: "all"}
		snap.Metrics["client_"+sk+"_reasoning_tokens"] = core.Metric{Used: float64Ptr(source.Reasoning), Unit: "tokens", Window: "all"}
		snap.Metrics["client_"+sk+"_requests"] = core.Metric{Used: float64Ptr(source.Requests), Unit: "requests", Window: "all"}
		snap.Metrics["client_"+sk+"_sessions"] = core.Metric{Used: float64Ptr(source.Sessions), Unit: "sessions", Window: "all"}
	}

	for _, tool := range agg.Tools {
		tk := sanitizeMetricID(tool.Tool)
		snap.Metrics["tool_"+tk] = core.Metric{Used: float64Ptr(tool.Calls), Unit: "calls", Window: "all"}
		snap.Metrics["tool_"+tk+"_today"] = core.Metric{Used: float64Ptr(tool.Calls1d), Unit: "calls", Window: "1d"}
	}

	snap.DailySeries["analytics_cost"] = pointsFromDaily(agg.Daily, func(v telemetryDayPoint) float64 { return v.CostUSD })
	snap.DailySeries["analytics_requests"] = pointsFromDaily(agg.Daily, func(v telemetryDayPoint) float64 { return v.Requests })
	snap.DailySeries["analytics_tokens"] = pointsFromDaily(agg.Daily, func(v telemetryDayPoint) float64 { return v.Tokens })
	todayCost, weekCost, monthCost := usageCostWindowsUTC(agg.Daily, time.Now().UTC())
	if todayCost > 0 {
		snap.Metrics["today_cost"] = core.Metric{Used: float64Ptr(todayCost), Unit: "USD", Window: "1d"}
		snap.Metrics["usage_daily"] = core.Metric{Used: float64Ptr(todayCost), Unit: "USD", Window: "1d"}
	}
	if weekCost > 0 {
		snap.Metrics["7d_api_cost"] = core.Metric{Used: float64Ptr(weekCost), Unit: "USD", Window: "7d"}
		snap.Metrics["usage_weekly"] = core.Metric{Used: float64Ptr(weekCost), Unit: "USD", Window: "7d"}
	}
	if monthCost > 0 {
		snap.Metrics["analytics_30d_cost"] = core.Metric{Used: float64Ptr(monthCost), Unit: "USD", Window: "30d"}
	}

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

func loadUsageViewForProviderWithSources(ctx context.Context, db *sql.DB, providerIDs []string, accountID string) (*telemetryUsageAgg, error) {
	providerIDs = normalizeProviderIDs(providerIDs)
	if len(providerIDs) == 0 {
		return &telemetryUsageAgg{}, nil
	}
	accountID = strings.TrimSpace(accountID)

	if accountID != "" {
		scoped, err := loadUsageViewForFilter(ctx, db, usageFilter{
			ProviderIDs: providerIDs,
			AccountID:   accountID,
		})
		if err != nil {
			return nil, err
		}
		if scoped == nil {
			scoped = &telemetryUsageAgg{}
		}
		scoped.Scope = "account"
		scoped.AccountID = accountID
		return scoped, nil
	}

	fallback, err := loadUsageViewForFilter(ctx, db, usageFilter{
		ProviderIDs: providerIDs,
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
	agg := &telemetryUsageAgg{
		ModelDaily:   make(map[string][]core.TimePoint),
		SourceDaily:  make(map[string][]core.TimePoint),
		ClientDaily:  make(map[string][]core.TimePoint),
		ClientTokens: make(map[string][]core.TimePoint),
	}

	usageCTE, whereArgs := dedupedUsageCTE(filter)
	countQuery := usageCTE + `
		SELECT COALESCE(MAX(occurred_at), ''), COUNT(*)
		FROM deduped_usage
		WHERE 1=1
		  AND event_type IN ('message_usage', 'tool_usage')
	`
	if err := db.QueryRowContext(ctx, countQuery, whereArgs...).Scan(&agg.LastOccurred, &agg.EventCount); err != nil {
		return nil, fmt.Errorf("canonical usage count query: %w", err)
	}
	if agg.EventCount == 0 {
		return agg, nil
	}

	models, err := queryModelAgg(ctx, db, filter)
	if err != nil {
		return nil, err
	}
	sources, err := querySourceAgg(ctx, db, filter)
	if err != nil {
		return nil, err
	}
	tools, err := queryToolAgg(ctx, db, filter)
	if err != nil {
		return nil, err
	}
	providers, err := queryProviderAgg(ctx, db, filter)
	if err != nil {
		return nil, err
	}
	daily, err := queryDailyTotals(ctx, db, filter)
	if err != nil {
		return nil, err
	}
	modelDaily, err := queryDailyByDimension(ctx, db, filter, "model")
	if err != nil {
		return nil, err
	}
	sourceDaily, err := queryDailyByDimension(ctx, db, filter, "source")
	if err != nil {
		return nil, err
	}
	clientDaily, err := queryDailyByDimension(ctx, db, filter, "client")
	if err != nil {
		return nil, err
	}
	clientTokens, err := queryDailyClientTokens(ctx, db, filter)
	if err != nil {
		return nil, err
	}

	agg.Models = models
	agg.Providers = providers
	agg.Sources = sources
	agg.Tools = tools
	agg.Daily = daily
	agg.ModelDaily = modelDaily
	agg.SourceDaily = sourceDaily
	agg.ClientDaily = clientDaily
	agg.ClientTokens = clientTokens
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
			COALESCE(NULLIF(TRIM(workspace_id), ''), COALESCE(NULLIF(TRIM(source_system), ''), 'unknown')) AS source_name,
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
			SUM(CASE WHEN date(occurred_at) = date('now') THEN COALESCE(requests, 1) ELSE 0 END) AS calls_today
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'tool_usage'
		  AND status != 'error'
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
		if err := rows.Scan(&row.Tool, &row.Calls, &row.Calls1d); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func queryProviderAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryProviderAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			COALESCE(NULLIF(TRIM(provider_id), ''), 'unknown') AS provider_name,
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

func queryDailyTotals(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryDayPoint, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
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
		  AND status != 'error'
		  AND occurred_at >= datetime('now', '-30 day')
		GROUP BY day
		ORDER BY day ASC
	`
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
	var query string

	switch dimension {
	case "model":
		query = usageCTE + `
			SELECT date(occurred_at) AS day,
			       COALESCE(NULLIF(TRIM(COALESCE(model_canonical, model_raw)), ''), 'unknown') AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'
			  AND occurred_at >= datetime('now', '-30 day')
			GROUP BY day, dim_key
		`
	case "source", "client":
		query = usageCTE + `
			SELECT date(occurred_at) AS day,
			       COALESCE(NULLIF(TRIM(workspace_id), ''), COALESCE(NULLIF(TRIM(source_system), ''), 'unknown')) AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'
			  AND occurred_at >= datetime('now', '-30 day')
			GROUP BY day, dim_key
		`
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
	query := usageCTE + `
		SELECT
			date(occurred_at) AS day,
			COALESCE(NULLIF(TRIM(workspace_id), ''), COALESCE(NULLIF(TRIM(source_system), ''), 'unknown')) AS source_name,
			SUM(COALESCE(total_tokens,
				COALESCE(input_tokens, 0) +
				COALESCE(output_tokens, 0) +
				COALESCE(reasoning_tokens, 0) +
				COALESCE(cache_read_tokens, 0) +
				COALESCE(cache_write_tokens, 0))) AS tokens
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'
		  AND occurred_at >= datetime('now', '-30 day')
		GROUP BY day, source_name
	`
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
	where, args := usageWhereClause("e", filter)
	cte := fmt.Sprintf(`
		WITH scoped_usage AS (
			SELECT
				e.*,
				COALESCE(r.source_system, '') AS source_system,
				COALESCE(r.source_channel, '') AS source_channel
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

func usageCostWindowsUTC(daily []telemetryDayPoint, now time.Time) (today float64, week float64, month float64) {
	if len(daily) == 0 {
		return 0, 0, 0
	}
	utcNow := now.UTC()
	todayKey := utcNow.Format("2006-01-02")
	weekStartKey := utcNow.AddDate(0, 0, -6).Format("2006-01-02")
	monthStartKey := utcNow.AddDate(0, 0, -29).Format("2006-01-02")

	for _, row := range daily {
		day := strings.TrimSpace(row.Day)
		if day == "" {
			continue
		}
		if day == todayKey {
			today += row.CostUSD
		}
		if day >= weekStartKey && day <= todayKey {
			week += row.CostUSD
		}
		if day >= monthStartKey && day <= todayKey {
			month += row.CostUSD
		}
	}
	return today, week, month
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
