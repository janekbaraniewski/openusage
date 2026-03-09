package ollama

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func populateEstimatedTokenUsageFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot, now time.Time) error {
	hasThinking, err := tableHasColumn(ctx, db, "messages", "thinking")
	if err != nil {
		return err
	}

	thinkingExpr := `''`
	if hasThinking {
		thinkingExpr = `COALESCE(thinking, '')`
	}

	query := fmt.Sprintf(`SELECT chat_id, id, role, model_name, COALESCE(content, ''), %s, COALESCE(created_at, '')
		FROM messages
		ORDER BY chat_id, datetime(created_at), id`, thinkingExpr)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	type tokenAgg struct {
		input    float64
		output   float64
		requests float64
	}
	ensureAgg := func(m map[string]*tokenAgg, key string) *tokenAgg {
		if m[key] == nil {
			m[key] = &tokenAgg{}
		}
		return m[key]
	}
	ensureDaily := func(m map[string]map[string]float64, key string) map[string]float64 {
		if m[key] == nil {
			m[key] = make(map[string]float64)
		}
		return m[key]
	}

	modelAgg := make(map[string]*tokenAgg)
	sourceAgg := make(map[string]*tokenAgg)
	dailyTokens := make(map[string]float64)
	dailyRequests := make(map[string]float64)
	modelDailyTokens := make(map[string]map[string]float64)
	sourceDailyTokens := make(map[string]map[string]float64)
	sourceDailyRequests := make(map[string]map[string]float64)
	sessionsBySource := make(map[string]float64)

	now = now.In(time.Local)
	start5h := now.Add(-5 * time.Hour)
	start1d := now.Add(-24 * time.Hour)
	start7d := now.Add(-7 * 24 * time.Hour)

	var tokens5h float64
	var tokens1d float64
	var tokens7d float64
	var tokensToday float64

	currentChat := ""
	pendingInputChars := 0
	chatSources := make(map[string]bool)
	flushChat := func() {
		for source := range chatSources {
			sessionsBySource[source]++
		}
		clear(chatSources)
		pendingInputChars = 0
	}

	for rows.Next() {
		var chatID string
		var id int64
		var role sql.NullString
		var modelName sql.NullString
		var content sql.NullString
		var thinking sql.NullString
		var createdAt sql.NullString

		if err := rows.Scan(&chatID, &id, &role, &modelName, &content, &thinking, &createdAt); err != nil {
			return err
		}

		if currentChat == "" {
			currentChat = chatID
		}
		if chatID != currentChat {
			flushChat()
			currentChat = chatID
		}

		roleVal := strings.ToLower(strings.TrimSpace(role.String))
		contentLen := len(content.String)
		thinkingLen := len(thinking.String)

		ts := time.Time{}
		if createdAt.Valid && strings.TrimSpace(createdAt.String) != "" {
			if parsed, ok := parseDesktopDBTime(createdAt.String); ok {
				ts = parsed.In(time.Local)
			}
		}
		day := ""
		if !ts.IsZero() {
			day = ts.Format("2006-01-02")
		} else if createdAt.Valid && len(createdAt.String) >= 10 {
			day = createdAt.String[:10]
		}

		if roleVal == "user" {
			pendingInputChars += contentLen + thinkingLen
			continue
		}
		if roleVal != "assistant" {
			continue
		}

		model := strings.TrimSpace(modelName.String)
		model = normalizeModelName(model)
		if model == "" {
			continue
		}
		modelKey := sanitizeMetricPart(model)
		source := sourceFromModelName(model)
		sourceKey := sanitizeMetricPart(source)

		inputTokens := estimateTokensFromChars(pendingInputChars)
		outputTokens := estimateTokensFromChars(contentLen + thinkingLen)
		totalTokens := inputTokens + outputTokens
		pendingInputChars = 0

		modelTotals := ensureAgg(modelAgg, model)
		modelTotals.input += inputTokens
		modelTotals.output += outputTokens
		modelTotals.requests++

		sourceTotals := ensureAgg(sourceAgg, sourceKey)
		sourceTotals.input += inputTokens
		sourceTotals.output += outputTokens
		sourceTotals.requests++
		chatSources[sourceKey] = true

		if day != "" {
			dailyTokens[day] += totalTokens
			dailyRequests[day]++
			ensureDaily(modelDailyTokens, modelKey)[day] += totalTokens
			ensureDaily(sourceDailyTokens, sourceKey)[day] += totalTokens
			ensureDaily(sourceDailyRequests, sourceKey)[day]++
			if day == now.Format("2006-01-02") {
				tokensToday += totalTokens
			}
		}

		if !ts.IsZero() {
			if ts.After(start5h) {
				tokens5h += totalTokens
			}
			if ts.After(start1d) {
				tokens1d += totalTokens
			}
			if ts.After(start7d) {
				tokens7d += totalTokens
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if currentChat != "" {
		flushChat()
	}

	type modelTotal struct {
		name string
		tok  float64
	}
	var topModels []modelTotal
	for model, totals := range modelAgg {
		modelKey := sanitizeMetricPart(model)
		setValueMetric(snap, "model_"+modelKey+"_input_tokens", totals.input, "tokens", "all-time")
		setValueMetric(snap, "model_"+modelKey+"_output_tokens", totals.output, "tokens", "all-time")
		setValueMetric(snap, "model_"+modelKey+"_total_tokens", totals.input+totals.output, "tokens", "all-time")

		rec := core.ModelUsageRecord{
			RawModelID:   model,
			RawSource:    "sqlite_estimate",
			Window:       "all-time",
			InputTokens:  core.Float64Ptr(totals.input),
			OutputTokens: core.Float64Ptr(totals.output),
			TotalTokens:  core.Float64Ptr(totals.input + totals.output),
			Requests:     core.Float64Ptr(totals.requests),
		}
		rec.SetDimension("provider", "ollama")
		rec.SetDimension("estimation", "chars_div_4")
		snap.AppendModelUsage(rec)

		topModels = append(topModels, modelTotal{name: model, tok: totals.input + totals.output})
	}
	sort.Slice(topModels, func(i, j int) bool {
		if topModels[i].tok == topModels[j].tok {
			return topModels[i].name < topModels[j].name
		}
		return topModels[i].tok > topModels[j].tok
	})
	if len(topModels) > 0 {
		top := make([]string, 0, min(len(topModels), 6))
		for i := 0; i < len(topModels) && i < 6; i++ {
			top = append(top, fmt.Sprintf("%s=%.0f", topModels[i].name, topModels[i].tok))
		}
		snap.Raw["model_tokens_estimated_top"] = strings.Join(top, ", ")
	}

	for sourceKey, totals := range sourceAgg {
		totalTokens := totals.input + totals.output
		setValueMetric(snap, "client_"+sourceKey+"_input_tokens", totals.input, "tokens", "all-time")
		setValueMetric(snap, "client_"+sourceKey+"_output_tokens", totals.output, "tokens", "all-time")
		setValueMetric(snap, "client_"+sourceKey+"_total_tokens", totalTokens, "tokens", "all-time")
		setValueMetric(snap, "client_"+sourceKey+"_requests", totals.requests, "requests", "all-time")
		if sessions := sessionsBySource[sourceKey]; sessions > 0 {
			setValueMetric(snap, "client_"+sourceKey+"_sessions", sessions, "sessions", "all-time")
		}

		setValueMetric(snap, "provider_"+sourceKey+"_input_tokens", totals.input, "tokens", "all-time")
		setValueMetric(snap, "provider_"+sourceKey+"_output_tokens", totals.output, "tokens", "all-time")
		setValueMetric(snap, "provider_"+sourceKey+"_requests", totals.requests, "requests", "all-time")
	}

	for sourceKey, byDay := range sourceDailyTokens {
		if len(byDay) == 0 {
			continue
		}
		snap.DailySeries["tokens_client_"+sourceKey] = core.SortedTimePoints(byDay)
	}
	for sourceKey, byDay := range sourceDailyRequests {
		if len(byDay) == 0 {
			continue
		}
		snap.DailySeries["usage_client_"+sourceKey] = core.SortedTimePoints(byDay)
	}
	for modelKey, byDay := range modelDailyTokens {
		if len(byDay) == 0 {
			continue
		}
		snap.DailySeries["tokens_model_"+modelKey] = core.SortedTimePoints(byDay)
	}
	if len(dailyTokens) > 0 {
		snap.DailySeries["analytics_tokens"] = core.SortedTimePoints(dailyTokens)
	}
	if len(dailyRequests) > 0 {
		snap.DailySeries["analytics_requests"] = core.SortedTimePoints(dailyRequests)
	}

	if tokensToday > 0 {
		setValueMetric(snap, "tokens_today", tokensToday, "tokens", "today")
	}
	if tokens5h > 0 {
		setValueMetric(snap, "tokens_5h", tokens5h, "tokens", "5h")
	}
	if tokens1d > 0 {
		setValueMetric(snap, "tokens_1d", tokens1d, "tokens", "1d")
	}
	if tokens7d > 0 {
		setValueMetric(snap, "7d_tokens", tokens7d, "tokens", "7d")
	}

	snap.SetAttribute("token_estimation", "chars_div_4")
	return nil
}

func estimateTokensFromChars(chars int) float64 {
	if chars <= 0 {
		return 0
	}
	return float64((chars + 3) / 4)
}

func parseDesktopDBTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	} {
		if ts, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return ts, true
		}
	}
	return parseAnyTime(raw)
}
