package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func queryDailyTotals(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryDayPoint, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	dailyTimeFilter := ""
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
			return nil, fmt.Errorf("scan canonical usage daily row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}
	return out, nil
}

func queryDailyByDimension(ctx context.Context, db *sql.DB, filter usageFilter, dimension string) (map[string][]core.TimePoint, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	dailyTimeFilter := ""
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
			       %s AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'%s
			GROUP BY day, dim_key
		`, clientDimensionExpr(), dailyTimeFilter)
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
			return nil, fmt.Errorf("scan canonical usage daily %s row: %w", dimension, err)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
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
			return nil, fmt.Errorf("scan canonical usage daily client token row: %w", err)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	out := make(map[string][]core.TimePoint, len(byClient))
	for key, dayMap := range byClient {
		out[key] = core.SortedTimePoints(dayMap)
	}
	return out, nil
}

func queryDailyMCP(ctx context.Context, db *sql.DB, filter usageFilter) (map[string][]core.TimePoint, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			date(occurred_at) AS day,
			COALESCE(NULLIF(TRIM(LOWER(tool_name)), ''), 'unknown') AS tool_name,
			SUM(COALESCE(requests, 1)) AS calls
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'tool_usage'
		GROUP BY day, tool_name
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage daily mcp query: %w", err)
	}
	defer rows.Close()

	byServer := make(map[string]map[string]float64)
	for rows.Next() {
		var day, toolName string
		var value float64
		if err := rows.Scan(&day, &toolName, &value); err != nil {
			return nil, fmt.Errorf("scan canonical usage daily mcp row: %w", err)
		}
		server, _, ok := parseMCPToolName(toolName)
		if !ok || strings.TrimSpace(server) == "" {
			continue
		}
		server = sanitizeMetricID(server)
		if server == "" {
			server = "unknown"
		}
		if byServer[server] == nil {
			byServer[server] = make(map[string]float64)
		}
		byServer[server][day] += value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	out := make(map[string][]core.TimePoint, len(byServer))
	for key, dayMap := range byServer {
		out[key] = core.SortedTimePoints(dayMap)
	}
	return out, nil
}
