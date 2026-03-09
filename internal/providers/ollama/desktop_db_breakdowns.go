package ollama

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func populateModelUsageFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	rows, err := db.QueryContext(ctx, `SELECT model_name, COUNT(*) FROM messages WHERE model_name IS NOT NULL AND trim(model_name) != '' GROUP BY model_name ORDER BY COUNT(*) DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var top []string
	for rows.Next() {
		var rawModel string
		var count float64
		if err := rows.Scan(&rawModel, &count); err != nil {
			return err
		}
		model := normalizeModelName(rawModel)
		if model == "" {
			continue
		}

		metricKey := "model_" + sanitizeMetricPart(model) + "_requests"
		setValueMetric(snap, metricKey, count, "requests", "all-time")

		rec := core.ModelUsageRecord{
			RawModelID: model,
			RawSource:  "sqlite",
			Window:     "all-time",
			Requests:   core.Float64Ptr(count),
		}
		rec.SetDimension("provider", "ollama")
		snap.AppendModelUsage(rec)

		if len(top) < 6 {
			top = append(top, fmt.Sprintf("%s=%.0f", model, count))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(top) > 0 {
		snap.Raw["models_usage_top"] = strings.Join(top, ", ")
	}

	todayRows, err := db.QueryContext(ctx, `SELECT model_name, COUNT(*)
		FROM messages
		WHERE model_name IS NOT NULL AND trim(model_name) != ''
			AND date(created_at) = date('now', 'localtime')
		GROUP BY model_name`)
	if err == nil {
		defer todayRows.Close()
		for todayRows.Next() {
			var rawModel string
			var count float64
			if err := todayRows.Scan(&rawModel, &count); err != nil {
				return err
			}
			model := normalizeModelName(rawModel)
			if model == "" {
				continue
			}

			metricKey := "model_" + sanitizeMetricPart(model) + "_requests_today"
			setValueMetric(snap, metricKey, count, "requests", "today")

			rec := core.ModelUsageRecord{
				RawModelID: model,
				RawSource:  "sqlite",
				Window:     "today",
				Requests:   core.Float64Ptr(count),
			}
			rec.SetDimension("provider", "ollama")
			snap.AppendModelUsage(rec)
		}
		if err := todayRows.Err(); err != nil {
			return err
		}
	}

	perDayRows, err := db.QueryContext(ctx, `SELECT date(created_at), model_name, COUNT(*)
		FROM messages
		WHERE model_name IS NOT NULL AND trim(model_name) != ''
		GROUP BY date(created_at), model_name`)
	if err != nil {
		return nil
	}
	defer perDayRows.Close()

	perModelDaily := make(map[string]map[string]float64)
	for perDayRows.Next() {
		var date string
		var rawModel string
		var count float64
		if err := perDayRows.Scan(&date, &rawModel, &count); err != nil {
			return err
		}
		model := normalizeModelName(rawModel)
		date = strings.TrimSpace(date)
		if model == "" || date == "" {
			continue
		}
		if perModelDaily[model] == nil {
			perModelDaily[model] = make(map[string]float64)
		}
		perModelDaily[model][date] = count
	}
	if err := perDayRows.Err(); err != nil {
		return err
	}

	for model, byDate := range perModelDaily {
		seriesKey := "requests_model_" + sanitizeMetricPart(model)
		snap.DailySeries[seriesKey] = core.SortedTimePoints(byDate)
		usageSeriesKey := "usage_model_" + sanitizeMetricPart(model)
		snap.DailySeries[usageSeriesKey] = core.SortedTimePoints(byDate)
	}

	return nil
}

func populateSourceUsageFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	allTimeRows, err := db.QueryContext(ctx, `SELECT model_name, COUNT(*)
		FROM messages
		WHERE model_name IS NOT NULL AND trim(model_name) != ''
		GROUP BY model_name`)
	if err != nil {
		return err
	}
	defer allTimeRows.Close()

	allTimeBySource := make(map[string]float64)
	for allTimeRows.Next() {
		var rawModel string
		var count float64
		if err := allTimeRows.Scan(&rawModel, &count); err != nil {
			return err
		}
		model := normalizeModelName(rawModel)
		source := sourceFromModelName(model)
		allTimeBySource[source] += count
	}
	if err := allTimeRows.Err(); err != nil {
		return err
	}

	for source, count := range allTimeBySource {
		if count <= 0 {
			continue
		}
		sourceKey := sanitizeMetricPart(source)
		setValueMetric(snap, "source_"+sourceKey+"_requests", count, "requests", "all-time")
	}

	todayRows, err := db.QueryContext(ctx, `SELECT model_name, COUNT(*)
		FROM messages
		WHERE model_name IS NOT NULL AND trim(model_name) != ''
			AND date(created_at) = date('now', 'localtime')
		GROUP BY model_name`)
	if err == nil {
		defer todayRows.Close()
		todayBySource := make(map[string]float64)
		for todayRows.Next() {
			var rawModel string
			var count float64
			if err := todayRows.Scan(&rawModel, &count); err != nil {
				return err
			}
			model := normalizeModelName(rawModel)
			source := sourceFromModelName(model)
			todayBySource[source] += count
		}
		if err := todayRows.Err(); err != nil {
			return err
		}

		for source, count := range todayBySource {
			if count <= 0 {
				continue
			}
			sourceKey := sanitizeMetricPart(source)
			setValueMetric(snap, "source_"+sourceKey+"_requests_today", count, "requests", "today")
		}
	}

	perDayRows, err := db.QueryContext(ctx, `SELECT date(created_at), model_name, COUNT(*)
		FROM messages
		WHERE model_name IS NOT NULL AND trim(model_name) != ''
		GROUP BY date(created_at), model_name`)
	if err != nil {
		return nil
	}
	defer perDayRows.Close()

	perSourceDaily := make(map[string]map[string]float64)
	for perDayRows.Next() {
		var day string
		var rawModel string
		var count float64
		if err := perDayRows.Scan(&day, &rawModel, &count); err != nil {
			return err
		}
		day = strings.TrimSpace(day)
		if day == "" {
			continue
		}
		model := normalizeModelName(rawModel)
		source := sourceFromModelName(model)
		sourceKey := sanitizeMetricPart(source)
		if perSourceDaily[sourceKey] == nil {
			perSourceDaily[sourceKey] = make(map[string]float64)
		}
		perSourceDaily[sourceKey][day] += count
	}
	if err := perDayRows.Err(); err != nil {
		return err
	}

	for sourceKey, byDay := range perSourceDaily {
		if len(byDay) == 0 {
			continue
		}
		snap.DailySeries["usage_source_"+sourceKey] = core.SortedTimePoints(byDay)
	}

	return nil
}

func populateToolUsageFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	hasFunctionName, err := tableHasColumn(ctx, db, "tool_calls", "function_name")
	if err != nil || !hasFunctionName {
		return nil
	}

	rows, err := db.QueryContext(ctx, `SELECT function_name, COUNT(*)
		FROM tool_calls
		WHERE trim(function_name) != ''
		GROUP BY function_name
		ORDER BY COUNT(*) DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var top []string
	for rows.Next() {
		var toolName string
		var count float64
		if err := rows.Scan(&toolName, &count); err != nil {
			return err
		}
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			continue
		}

		setValueMetric(snap, "tool_"+sanitizeMetricPart(toolName), count, "calls", "all-time")
		if len(top) < 6 {
			top = append(top, fmt.Sprintf("%s=%.0f", toolName, count))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(top) > 0 {
		snap.Raw["tool_usage"] = strings.Join(top, ", ")
	}

	perDayRows, err := db.QueryContext(ctx, `SELECT date(m.created_at), tc.function_name, COUNT(*)
		FROM tool_calls tc
		JOIN messages m ON tc.message_id = m.id
		WHERE trim(tc.function_name) != ''
		GROUP BY date(m.created_at), tc.function_name`)
	if err != nil {
		return nil
	}
	defer perDayRows.Close()

	perToolDaily := make(map[string]map[string]float64)
	for perDayRows.Next() {
		var day string
		var toolName string
		var count float64
		if err := perDayRows.Scan(&day, &toolName, &count); err != nil {
			return err
		}
		day = strings.TrimSpace(day)
		toolKey := sanitizeMetricPart(toolName)
		if day == "" || toolKey == "" {
			continue
		}
		if perToolDaily[toolKey] == nil {
			perToolDaily[toolKey] = make(map[string]float64)
		}
		perToolDaily[toolKey][day] += count
	}
	if err := perDayRows.Err(); err != nil {
		return err
	}

	for toolKey, byDay := range perToolDaily {
		if len(byDay) == 0 {
			continue
		}
		snap.DailySeries["usage_tool_"+toolKey] = core.SortedTimePoints(byDay)
	}

	return nil
}

func sourceFromModelName(model string) string {
	normalized := normalizeModelName(model)
	if normalized == "" {
		return "unknown"
	}
	if strings.HasSuffix(normalized, ":cloud") || strings.Contains(normalized, "-cloud") {
		return "cloud"
	}
	return "local"
}

func populateDailySeriesFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	dailyQueries := []struct {
		key   string
		query string
	}{
		{"messages", `SELECT date(created_at), COUNT(*) FROM messages GROUP BY date(created_at)`},
		{"sessions", `SELECT date(created_at), COUNT(*) FROM chats GROUP BY date(created_at)`},
		{"tool_calls", `SELECT date(m.created_at), COUNT(*)
			FROM tool_calls tc
			JOIN messages m ON tc.message_id = m.id
			GROUP BY date(m.created_at)`},
		{"requests_user", `SELECT date(created_at), COUNT(*) FROM messages WHERE role = 'user' GROUP BY date(created_at)`},
	}

	for _, dq := range dailyQueries {
		rows, err := db.QueryContext(ctx, dq.query)
		if err != nil {
			continue
		}

		byDate := make(map[string]float64)
		for rows.Next() {
			var date string
			var count float64
			if err := rows.Scan(&date, &count); err != nil {
				rows.Close()
				return err
			}
			if strings.TrimSpace(date) == "" {
				continue
			}
			byDate[date] = count
		}
		rows.Close()
		if len(byDate) > 0 {
			points := core.SortedTimePoints(byDate)
			snap.DailySeries[dq.key] = points
			if dq.key == "requests_user" {
				if _, exists := snap.DailySeries["requests"]; !exists {
					snap.DailySeries["requests"] = points
				}
			}
		}
	}

	return nil
}
