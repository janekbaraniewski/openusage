package ollama

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func queryCount(ctx context.Context, db *sql.DB, query string) (int64, error) {
	var count int64
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func tableHasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	table = strings.TrimSpace(table)
	column = strings.TrimSpace(column)
	if table == "" || column == "" {
		return false, nil
	}
	safeTable := strings.ReplaceAll(table, "'", "''")
	query := fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, safeTable)
	var count int
	if err := db.QueryRowContext(ctx, query, column).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func populateThinkingMetricsFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	hasStart, _ := tableHasColumn(ctx, db, "messages", "thinking_time_start")
	hasEnd, _ := tableHasColumn(ctx, db, "messages", "thinking_time_end")
	if !hasStart || !hasEnd {
		return nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT model_name,
			COUNT(*) as think_count,
			SUM(CAST((julianday(thinking_time_end) - julianday(thinking_time_start)) * 86400 AS REAL)) as total_think_seconds,
			AVG(CAST((julianday(thinking_time_end) - julianday(thinking_time_start)) * 86400 AS REAL)) as avg_think_seconds
		FROM messages
		WHERE thinking_time_start IS NOT NULL AND thinking_time_end IS NOT NULL
			AND thinking_time_start != '' AND thinking_time_end != ''
		GROUP BY model_name`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var totalThinkRequests int64
	var totalThinkSeconds float64
	var totalAvgCount int

	for rows.Next() {
		var rawModel sql.NullString
		var thinkCount int64
		var totalSec sql.NullFloat64
		var avgSec sql.NullFloat64

		if err := rows.Scan(&rawModel, &thinkCount, &totalSec, &avgSec); err != nil {
			return err
		}

		totalThinkRequests += thinkCount
		if totalSec.Valid {
			totalThinkSeconds += totalSec.Float64
		}
		totalAvgCount++

		if rawModel.Valid && strings.TrimSpace(rawModel.String) != "" {
			model := normalizeModelName(rawModel.String)
			if model != "" {
				prefix := "model_" + sanitizeMetricPart(model)
				if totalSec.Valid {
					setValueMetric(snap, prefix+"_thinking_seconds", totalSec.Float64, "seconds", "all-time")
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if totalThinkRequests > 0 {
		setValueMetric(snap, "thinking_requests", float64(totalThinkRequests), "requests", "all-time")
		setValueMetric(snap, "total_thinking_seconds", totalThinkSeconds, "seconds", "all-time")
		if totalAvgCount > 0 {
			setValueMetric(snap, "avg_thinking_seconds", totalThinkSeconds/float64(totalThinkRequests), "seconds", "all-time")
		}
	}

	return nil
}

func populateSettingsFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	var selectedModel sql.NullString
	var contextLength sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT selected_model, context_length FROM settings LIMIT 1`).Scan(&selectedModel, &contextLength)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	if selectedModel.Valid && strings.TrimSpace(selectedModel.String) != "" {
		snap.SetAttribute("selected_model", selectedModel.String)
	}
	if contextLength.Valid && contextLength.Int64 > 0 {
		setValueMetric(snap, "configured_context_length", float64(contextLength.Int64), "tokens", "current")
	}

	type settingsCol struct {
		column string
		attr   string
	}
	extraCols := []settingsCol{
		{"websearch_enabled", "websearch_enabled"},
		{"turbo_enabled", "turbo_enabled"},
		{"agent", "agent_mode"},
		{"tools", "tools_enabled"},
		{"think_enabled", "think_enabled"},
		{"airplane_mode", "airplane_mode"},
		{"device_id", "device_id"},
	}
	for _, col := range extraCols {
		has, _ := tableHasColumn(ctx, db, "settings", col.column)
		if !has {
			continue
		}
		var val sql.NullString
		query := fmt.Sprintf(`SELECT CAST(%s AS TEXT) FROM settings LIMIT 1`, col.column)
		if err := db.QueryRowContext(ctx, query).Scan(&val); err != nil {
			continue
		}
		if val.Valid && strings.TrimSpace(val.String) != "" {
			snap.SetAttribute(col.attr, val.String)
		}
	}

	return nil
}

func populateCachedUserFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	var name sql.NullString
	var email sql.NullString
	var plan sql.NullString
	var cachedAt sql.NullString

	err := db.QueryRowContext(ctx, `SELECT name, email, plan, cached_at FROM users ORDER BY cached_at DESC LIMIT 1`).Scan(&name, &email, &plan, &cachedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	if name.Valid && strings.TrimSpace(name.String) != "" {
		snap.SetAttribute("account_name", name.String)
	}
	if email.Valid && strings.TrimSpace(email.String) != "" {
		snap.SetAttribute("account_email", email.String)
	}
	if plan.Valid && strings.TrimSpace(plan.String) != "" {
		snap.SetAttribute("plan_name", plan.String)
	}
	if cachedAt.Valid && strings.TrimSpace(cachedAt.String) != "" {
		snap.SetAttribute("account_cached_at", cachedAt.String)
	}
	return nil
}
