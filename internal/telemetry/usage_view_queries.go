package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

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
		LIMIT 500
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
		LIMIT 500
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

func queryProjectAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryProjectAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			COALESCE(NULLIF(TRIM(workspace_id), ''), '') AS project_name,
			SUM(COALESCE(requests, 1)) AS requests,
			SUM(CASE WHEN date(occurred_at) = date('now') THEN COALESCE(requests, 1) ELSE 0 END) AS requests_today
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'
		  AND NULLIF(TRIM(workspace_id), '') IS NOT NULL
		GROUP BY project_name
		ORDER BY requests DESC
		LIMIT 500
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage project query: %w", err)
	}
	defer rows.Close()

	var out []telemetryProjectAgg
	for rows.Next() {
		var row telemetryProjectAgg
		if err := rows.Scan(&row.Project, &row.Requests, &row.Requests1d); err != nil {
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
		LIMIT 500
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

func queryProviderAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryProviderAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
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
		LIMIT 200
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
	case "project":
		query = usageCTE + fmt.Sprintf(`
			SELECT date(occurred_at) AS day,
			       COALESCE(NULLIF(TRIM(workspace_id), ''), '') AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'
			  AND NULLIF(TRIM(workspace_id), '') IS NOT NULL%s
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
		if dimension == "project" && key == "unknown" {
			continue
		}
		if byDim[key] == nil {
			byDim[key] = make(map[string]float64)
		}
		byDim[key][day] += value
	}

	out := make(map[string][]core.TimePoint, len(byDim))
	for key, dayMap := range byDim {
		out[key] = core.SortedTimePoints(dayMap)
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
		out[key] = core.SortedTimePoints(dayMap)
	}
	return out, nil
}

func dedupedUsageCTE(filter usageFilter) (string, []any) {
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
	return core.SortedCompactStrings(normalized)
}
