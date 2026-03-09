package gemini_cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/samber/lo"
)

func mapKeysSorted(values map[string]bool) []string {
	if len(values) == 0 {
		return nil
	}
	out := slices.Sorted(maps.Keys(values))
	return slices.DeleteFunc(out, func(key string) bool { return strings.TrimSpace(key) == "" })
}

func formatGeminiNameList(values []string, max int) string {
	if len(values) == 0 {
		return ""
	}
	limit := max
	if limit <= 0 || limit > len(values) {
		limit = len(values)
	}
	out := strings.Join(values[:limit], ", ")
	if len(values) > limit {
		out += fmt.Sprintf(", +%d more", len(values)-limit)
	}
	return out
}

func (t geminiMessageToken) toUsage() tokenUsage {
	total := t.Total
	if total <= 0 {
		total = t.Input + t.Output + t.Cached + t.Thoughts + t.Tool
	}
	return tokenUsage{
		InputTokens:       t.Input,
		CachedInputTokens: t.Cached,
		OutputTokens:      t.Output,
		ReasoningTokens:   t.Thoughts,
		ToolTokens:        t.Tool,
		TotalTokens:       total,
	}
}

func (p *Provider) readSessionUsageBreakdowns(tmpDir string, snap *core.UsageSnapshot) (int, error) {
	files, err := findGeminiSessionFiles(tmpDir)
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}

	modelTotals := make(map[string]tokenUsage)
	clientTotals := make(map[string]tokenUsage)
	toolTotals := make(map[string]int)
	languageUsageCounts := make(map[string]int)
	changedFiles := make(map[string]bool)
	commitCommands := make(map[string]bool)
	modelDaily := make(map[string]map[string]float64)
	clientDaily := make(map[string]map[string]float64)
	clientSessions := make(map[string]int)
	modelRequests := make(map[string]int)
	modelSessions := make(map[string]int)

	dailyMessages := make(map[string]float64)
	dailySessions := make(map[string]float64)
	dailyToolCalls := make(map[string]float64)
	dailyTokens := make(map[string]float64)
	dailyInputTokens := make(map[string]float64)
	dailyOutputTokens := make(map[string]float64)
	dailyCachedTokens := make(map[string]float64)
	dailyReasoningTokens := make(map[string]float64)
	dailyToolTokens := make(map[string]float64)

	sessionIDs := make(map[string]bool)
	sessionCount := 0
	totalMessages := 0
	totalTurns := 0
	totalToolCalls := 0
	totalInfoMessages := 0
	totalErrorMessages := 0
	totalAssistantMessages := 0
	totalToolSuccess := 0
	totalToolFailed := 0
	totalToolErrored := 0
	totalToolCancelled := 0
	quotaLimitEvents := 0
	modelLinesAdded := 0
	modelLinesRemoved := 0
	modelCharsAdded := 0
	modelCharsRemoved := 0
	userLinesAdded := 0
	userLinesRemoved := 0
	userCharsAdded := 0
	userCharsRemoved := 0
	diffStatEvents := 0
	inferredCommitCount := 0

	var lastModelName string
	var lastModelTokens int
	foundLatest := false

	for _, path := range files {
		chat, err := readGeminiChatFile(path)
		if err != nil {
			continue
		}

		sessionID := strings.TrimSpace(chat.SessionID)
		if sessionID == "" {
			sessionID = path
		}
		if sessionIDs[sessionID] {
			continue
		}
		sessionIDs[sessionID] = true
		sessionCount++

		clientName := normalizeClientName("CLI")
		clientSessions[clientName]++

		sessionDay := dayFromSession(chat.StartTime, chat.LastUpdated)
		if sessionDay != "" {
			dailySessions[sessionDay]++
		}

		var previous tokenUsage
		var hasPrevious bool
		fileHasUsage := false
		sessionModels := make(map[string]bool)

		for _, msg := range chat.Messages {
			day := dayFromTimestamp(msg.Timestamp)
			if day == "" {
				day = sessionDay
			}

			switch strings.ToLower(strings.TrimSpace(msg.Type)) {
			case "info":
				totalInfoMessages++
			case "error":
				totalErrorMessages++
			case "gemini", "assistant", "model":
				totalAssistantMessages++
			}

			if isQuotaLimitMessage(msg.Content) {
				quotaLimitEvents++
			}

			if strings.EqualFold(msg.Type, "user") {
				totalMessages++
				if day != "" {
					dailyMessages[day]++
				}
			}

			if len(msg.ToolCalls) > 0 {
				totalToolCalls += len(msg.ToolCalls)
				if day != "" {
					dailyToolCalls[day] += float64(len(msg.ToolCalls))
				}
				for _, tc := range msg.ToolCalls {
					toolName := strings.TrimSpace(tc.Name)
					if toolName != "" {
						toolTotals[toolName]++
					}

					status := strings.ToLower(strings.TrimSpace(tc.Status))
					switch {
					case status == "" || status == "success" || status == "succeeded" || status == "ok" || status == "completed":
						totalToolSuccess++
					case status == "cancelled" || status == "canceled":
						totalToolCancelled++
						totalToolFailed++
					default:
						totalToolErrored++
						totalToolFailed++
					}

					toolLower := strings.ToLower(toolName)
					successfulToolCall := isGeminiToolCallSuccessful(status)
					for _, path := range extractGeminiToolPaths(tc.Args) {
						if successfulToolCall {
							if lang := inferGeminiLanguageFromPath(path); lang != "" {
								languageUsageCounts[lang]++
							}
						}
						if successfulToolCall && isGeminiMutatingTool(toolLower) {
							changedFiles[path] = true
						}
					}

					if successfulToolCall && isGeminiMutatingTool(toolLower) {
						if diff, ok := extractGeminiToolDiffStat(tc.ResultDisplay); ok {
							modelLinesAdded += diff.ModelAddedLines
							modelLinesRemoved += diff.ModelRemovedLines
							modelCharsAdded += diff.ModelAddedChars
							modelCharsRemoved += diff.ModelRemovedChars
							userLinesAdded += diff.UserAddedLines
							userLinesRemoved += diff.UserRemovedLines
							userCharsAdded += diff.UserAddedChars
							userCharsRemoved += diff.UserRemovedChars
							diffStatEvents++
						} else {
							added, removed := estimateGeminiToolLineDelta(tc.Args)
							modelLinesAdded += added
							modelLinesRemoved += removed
						}
					}

					if !successfulToolCall {
						continue
					}
					cmd := strings.ToLower(extractGeminiToolCommand(tc.Args))
					if strings.Contains(cmd, "git commit") {
						if !commitCommands[cmd] {
							commitCommands[cmd] = true
							inferredCommitCount++
						}
					} else if strings.Contains(toolLower, "commit") {
						inferredCommitCount++
					}
				}
			}
			if msg.Tokens == nil {
				continue
			}

			modelName := normalizeModelName(msg.Model)
			total := msg.Tokens.toUsage()

			if !foundLatest {
				lastModelName = modelName
				lastModelTokens = total.TotalTokens
				fileHasUsage = true
			}
			modelRequests[modelName]++
			sessionModels[modelName] = true

			delta := total
			if hasPrevious {
				delta = usageDelta(total, previous)
				if !validUsageDelta(delta) {
					delta = total
				}
			}
			previous = total
			hasPrevious = true

			if delta.TotalTokens <= 0 {
				continue
			}

			addUsage(modelTotals, modelName, delta)
			addUsage(clientTotals, clientName, delta)

			if day != "" {
				addDailyUsage(modelDaily, modelName, day, float64(delta.TotalTokens))
				addDailyUsage(clientDaily, clientName, day, float64(delta.TotalTokens))
				dailyTokens[day] += float64(delta.TotalTokens)
				dailyInputTokens[day] += float64(delta.InputTokens)
				dailyOutputTokens[day] += float64(delta.OutputTokens)
				dailyCachedTokens[day] += float64(delta.CachedInputTokens)
				dailyReasoningTokens[day] += float64(delta.ReasoningTokens)
				dailyToolTokens[day] += float64(delta.ToolTokens)
			}

			totalTurns++
		}

		for modelName := range sessionModels {
			modelSessions[modelName]++
		}

		if fileHasUsage {
			foundLatest = true
		}
	}

	if sessionCount == 0 {
		return 0, nil
	}

	if lastModelName != "" && lastModelTokens > 0 {
		limit := getModelContextLimit(lastModelName)
		if limit > 0 {
			used := float64(lastModelTokens)
			lim := float64(limit)
			snap.Metrics["context_window"] = core.Metric{
				Used:   &used,
				Limit:  &lim,
				Unit:   "tokens",
				Window: "current",
			}
			snap.Raw["active_model"] = lastModelName
		}
	}

	emitBreakdownMetrics("model", modelTotals, modelDaily, snap)
	emitBreakdownMetrics("client", clientTotals, clientDaily, snap)
	emitClientSessionMetrics(clientSessions, snap)
	emitModelRequestMetrics(modelRequests, modelSessions, snap)
	emitToolMetrics(toolTotals, snap)
	if languageSummary := formatNamedCountMap(languageUsageCounts, "req"); languageSummary != "" {
		snap.Raw["language_usage"] = languageSummary
	}
	for lang, count := range languageUsageCounts {
		if count <= 0 {
			continue
		}
		setUsedMetric(snap, "lang_"+sanitizeMetricName(lang), float64(count), "requests", defaultUsageWindowLabel)
	}

	storeSeries(snap, "messages", dailyMessages)
	storeSeries(snap, "sessions", dailySessions)
	storeSeries(snap, "tool_calls", dailyToolCalls)
	storeSeries(snap, "tokens_total", dailyTokens)
	storeSeries(snap, "requests", dailyMessages)
	storeSeries(snap, "analytics_requests", dailyMessages)
	storeSeries(snap, "analytics_tokens", dailyTokens)
	storeSeries(snap, "tokens_input", dailyInputTokens)
	storeSeries(snap, "tokens_output", dailyOutputTokens)
	storeSeries(snap, "tokens_cached", dailyCachedTokens)
	storeSeries(snap, "tokens_reasoning", dailyReasoningTokens)
	storeSeries(snap, "tokens_tool", dailyToolTokens)

	setUsedMetric(snap, "total_messages", float64(totalMessages), "messages", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_sessions", float64(sessionCount), "sessions", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_turns", float64(totalTurns), "turns", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_tool_calls", float64(totalToolCalls), "calls", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_info_messages", float64(totalInfoMessages), "messages", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_error_messages", float64(totalErrorMessages), "messages", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_assistant_messages", float64(totalAssistantMessages), "messages", defaultUsageWindowLabel)
	setUsedMetric(snap, "tool_calls_success", float64(totalToolSuccess), "calls", defaultUsageWindowLabel)
	setUsedMetric(snap, "tool_calls_failed", float64(totalToolFailed), "calls", defaultUsageWindowLabel)
	setUsedMetric(snap, "tool_calls_total", float64(totalToolCalls), "calls", defaultUsageWindowLabel)
	setUsedMetric(snap, "tool_completed", float64(totalToolSuccess), "calls", defaultUsageWindowLabel)
	setUsedMetric(snap, "tool_errored", float64(totalToolErrored), "calls", defaultUsageWindowLabel)
	setUsedMetric(snap, "tool_cancelled", float64(totalToolCancelled), "calls", defaultUsageWindowLabel)
	if totalToolCalls > 0 {
		successRate := float64(totalToolSuccess) / float64(totalToolCalls) * 100
		setUsedMetric(snap, "tool_success_rate", successRate, "%", defaultUsageWindowLabel)
	}
	setUsedMetric(snap, "quota_limit_events", float64(quotaLimitEvents), "events", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_prompts", float64(totalMessages), "prompts", defaultUsageWindowLabel)

	if cliUsage, ok := clientTotals["CLI"]; ok {
		setUsedMetric(snap, "client_cli_messages", float64(totalMessages), "messages", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_turns", float64(totalTurns), "turns", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_tool_calls", float64(totalToolCalls), "calls", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_input_tokens", float64(cliUsage.InputTokens), "tokens", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_output_tokens", float64(cliUsage.OutputTokens), "tokens", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_cached_tokens", float64(cliUsage.CachedInputTokens), "tokens", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_reasoning_tokens", float64(cliUsage.ReasoningTokens), "tokens", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_total_tokens", float64(cliUsage.TotalTokens), "tokens", defaultUsageWindowLabel)
	}

	total := aggregateTokenTotals(modelTotals)
	setUsedMetric(snap, "total_input_tokens", float64(total.InputTokens), "tokens", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_output_tokens", float64(total.OutputTokens), "tokens", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_cached_tokens", float64(total.CachedInputTokens), "tokens", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_reasoning_tokens", float64(total.ReasoningTokens), "tokens", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_tool_tokens", float64(total.ToolTokens), "tokens", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_tokens", float64(total.TotalTokens), "tokens", defaultUsageWindowLabel)

	if total.InputTokens > 0 {
		cacheEfficiency := float64(total.CachedInputTokens) / float64(total.InputTokens) * 100
		setPercentMetric(snap, "cache_efficiency", cacheEfficiency, defaultUsageWindowLabel)
	}
	if total.TotalTokens > 0 {
		reasoningShare := float64(total.ReasoningTokens) / float64(total.TotalTokens) * 100
		toolShare := float64(total.ToolTokens) / float64(total.TotalTokens) * 100
		setPercentMetric(snap, "reasoning_share", reasoningShare, defaultUsageWindowLabel)
		setPercentMetric(snap, "tool_token_share", toolShare, defaultUsageWindowLabel)
	}
	if totalTurns > 0 {
		avgTokensPerTurn := float64(total.TotalTokens) / float64(totalTurns)
		setUsedMetric(snap, "avg_tokens_per_turn", avgTokensPerTurn, "tokens", defaultUsageWindowLabel)
	}
	if sessionCount > 0 {
		avgToolsPerSession := float64(totalToolCalls) / float64(sessionCount)
		setUsedMetric(snap, "avg_tools_per_session", avgToolsPerSession, "calls", defaultUsageWindowLabel)
	}

	if _, v := latestSeriesValue(dailyMessages); v > 0 {
		setUsedMetric(snap, "messages_today", v, "messages", "today")
	}
	if _, v := latestSeriesValue(dailySessions); v > 0 {
		setUsedMetric(snap, "sessions_today", v, "sessions", "today")
	}
	if _, v := latestSeriesValue(dailyToolCalls); v > 0 {
		setUsedMetric(snap, "tool_calls_today", v, "calls", "today")
	}
	if _, v := latestSeriesValue(dailyTokens); v > 0 {
		setUsedMetric(snap, "tokens_today", v, "tokens", "today")
	}
	if _, v := latestSeriesValue(dailyInputTokens); v > 0 {
		setUsedMetric(snap, "today_input_tokens", v, "tokens", "today")
	}
	if _, v := latestSeriesValue(dailyOutputTokens); v > 0 {
		setUsedMetric(snap, "today_output_tokens", v, "tokens", "today")
	}
	if _, v := latestSeriesValue(dailyCachedTokens); v > 0 {
		setUsedMetric(snap, "today_cached_tokens", v, "tokens", "today")
	}
	if _, v := latestSeriesValue(dailyReasoningTokens); v > 0 {
		setUsedMetric(snap, "today_reasoning_tokens", v, "tokens", "today")
	}
	if _, v := latestSeriesValue(dailyToolTokens); v > 0 {
		setUsedMetric(snap, "today_tool_tokens", v, "tokens", "today")
	}

	setUsedMetric(snap, "7d_messages", sumLastNDays(dailyMessages, 7), "messages", "7d")
	setUsedMetric(snap, "7d_sessions", sumLastNDays(dailySessions, 7), "sessions", "7d")
	setUsedMetric(snap, "7d_tool_calls", sumLastNDays(dailyToolCalls, 7), "calls", "7d")
	setUsedMetric(snap, "7d_tokens", sumLastNDays(dailyTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "7d_input_tokens", sumLastNDays(dailyInputTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "7d_output_tokens", sumLastNDays(dailyOutputTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "7d_cached_tokens", sumLastNDays(dailyCachedTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "7d_reasoning_tokens", sumLastNDays(dailyReasoningTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "7d_tool_tokens", sumLastNDays(dailyToolTokens, 7), "tokens", "7d")

	if modelLinesAdded > 0 {
		setUsedMetric(snap, "composer_lines_added", float64(modelLinesAdded), "lines", defaultUsageWindowLabel)
	}
	if modelLinesRemoved > 0 {
		setUsedMetric(snap, "composer_lines_removed", float64(modelLinesRemoved), "lines", defaultUsageWindowLabel)
	}
	if len(changedFiles) > 0 {
		setUsedMetric(snap, "composer_files_changed", float64(len(changedFiles)), "files", defaultUsageWindowLabel)
	}
	if inferredCommitCount > 0 {
		setUsedMetric(snap, "scored_commits", float64(inferredCommitCount), "commits", defaultUsageWindowLabel)
	}
	if userLinesAdded > 0 {
		setUsedMetric(snap, "composer_user_lines_added", float64(userLinesAdded), "lines", defaultUsageWindowLabel)
	}
	if userLinesRemoved > 0 {
		setUsedMetric(snap, "composer_user_lines_removed", float64(userLinesRemoved), "lines", defaultUsageWindowLabel)
	}
	if modelCharsAdded > 0 {
		setUsedMetric(snap, "composer_model_chars_added", float64(modelCharsAdded), "chars", defaultUsageWindowLabel)
	}
	if modelCharsRemoved > 0 {
		setUsedMetric(snap, "composer_model_chars_removed", float64(modelCharsRemoved), "chars", defaultUsageWindowLabel)
	}
	if userCharsAdded > 0 {
		setUsedMetric(snap, "composer_user_chars_added", float64(userCharsAdded), "chars", defaultUsageWindowLabel)
	}
	if userCharsRemoved > 0 {
		setUsedMetric(snap, "composer_user_chars_removed", float64(userCharsRemoved), "chars", defaultUsageWindowLabel)
	}
	if diffStatEvents > 0 {
		setUsedMetric(snap, "composer_diffstat_events", float64(diffStatEvents), "calls", defaultUsageWindowLabel)
	}
	totalModelLineDelta := modelLinesAdded + modelLinesRemoved
	totalUserLineDelta := userLinesAdded + userLinesRemoved
	if totalModelLineDelta > 0 || totalUserLineDelta > 0 {
		totalLineDelta := totalModelLineDelta + totalUserLineDelta
		if totalLineDelta > 0 {
			aiPct := float64(totalModelLineDelta) / float64(totalLineDelta) * 100
			setPercentMetric(snap, "ai_code_percentage", aiPct, defaultUsageWindowLabel)
		}
	}

	if quotaLimitEvents > 0 {
		snap.Raw["quota_limit_detected"] = "true"
		if _, hasQuota := snap.Metrics["quota"]; !hasQuota {
			limit := 100.0
			remaining := 0.0
			used := 100.0
			snap.Metrics["quota"] = core.Metric{
				Limit:     &limit,
				Remaining: &remaining,
				Used:      &used,
				Unit:      "%",
				Window:    "daily",
			}
			applyQuotaStatus(snap, 0)
		}
	}

	return sessionCount, nil
}

func findGeminiSessionFiles(tmpDir string) ([]string, error) {
	if strings.TrimSpace(tmpDir) == "" {
		return nil, nil
	}
	if _, err := os.Stat(tmpDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat tmp dir: %w", err)
	}

	type item struct {
		path    string
		modTime time.Time
	}
	var files []item

	walkErr := filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".json") {
			return nil
		}
		files = append(files, item{path: path, modTime: info.ModTime()})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk gemini tmp dir: %w", walkErr)
	}
	if len(files) == 0 {
		return nil, nil
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].path > files[j].path
		}
		return files[i].modTime.After(files[j].modTime)
	})

	return lo.Map(files, func(f item, _ int) string { return f.path }), nil
}

func readGeminiChatFile(path string) (*geminiChatFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var chat geminiChatFile
	if err := json.NewDecoder(f).Decode(&chat); err != nil {
		return nil, err
	}
	return &chat, nil
}

func emitBreakdownMetrics(prefix string, totals map[string]tokenUsage, daily map[string]map[string]float64, snap *core.UsageSnapshot) {
	entries := sortUsageEntries(totals)
	if len(entries) == 0 {
		return
	}

	for i, entry := range entries {
		if i >= maxBreakdownMetrics {
			break
		}
		keyPrefix := prefix + "_" + sanitizeMetricName(entry.Name)
		setUsageMetric(snap, keyPrefix+"_total_tokens", float64(entry.Data.TotalTokens))
		setUsageMetric(snap, keyPrefix+"_input_tokens", float64(entry.Data.InputTokens))
		setUsageMetric(snap, keyPrefix+"_output_tokens", float64(entry.Data.OutputTokens))

		if entry.Data.CachedInputTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_cached_tokens", float64(entry.Data.CachedInputTokens))
		}
		if entry.Data.ReasoningTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_reasoning_tokens", float64(entry.Data.ReasoningTokens))
		}

		if byDay, ok := daily[entry.Name]; ok {
			seriesKey := "tokens_" + prefix + "_" + sanitizeMetricName(entry.Name)
			snap.DailySeries[seriesKey] = core.SortedTimePoints(byDay)
		}

		if prefix == "model" {
			rec := core.ModelUsageRecord{
				RawModelID:   entry.Name,
				RawSource:    "json",
				Window:       defaultUsageWindowLabel,
				InputTokens:  core.Float64Ptr(float64(entry.Data.InputTokens)),
				OutputTokens: core.Float64Ptr(float64(entry.Data.OutputTokens)),
				TotalTokens:  core.Float64Ptr(float64(entry.Data.TotalTokens)),
			}
			if entry.Data.CachedInputTokens > 0 {
				rec.CachedTokens = core.Float64Ptr(float64(entry.Data.CachedInputTokens))
			}
			if entry.Data.ReasoningTokens > 0 {
				rec.ReasoningTokens = core.Float64Ptr(float64(entry.Data.ReasoningTokens))
			}
			snap.AppendModelUsage(rec)
		}
	}

	snap.Raw[prefix+"_usage"] = formatUsageSummary(entries, maxBreakdownRaw)
}

func emitClientSessionMetrics(clientSessions map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		count int
	}
	var all []entry
	for name, count := range clientSessions {
		if count > 0 {
			all = append(all, entry{name: name, count: count})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})

	for i, item := range all {
		if i >= maxBreakdownMetrics {
			break
		}
		value := float64(item.count)
		snap.Metrics["client_"+sanitizeMetricName(item.name)+"_sessions"] = core.Metric{
			Used:   &value,
			Unit:   "sessions",
			Window: defaultUsageWindowLabel,
		}
	}
}

func emitModelRequestMetrics(modelRequests, modelSessions map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name     string
		requests int
		sessions int
	}

	all := make([]entry, 0, len(modelRequests))
	for name, requests := range modelRequests {
		if requests <= 0 {
			continue
		}
		all = append(all, entry{name: name, requests: requests, sessions: modelSessions[name]})
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].requests == all[j].requests {
			return all[i].name < all[j].name
		}
		return all[i].requests > all[j].requests
	})

	for i, item := range all {
		if i >= maxBreakdownMetrics {
			break
		}
		keyPrefix := "model_" + sanitizeMetricName(item.name)
		req := float64(item.requests)
		sess := float64(item.sessions)
		snap.Metrics[keyPrefix+"_requests"] = core.Metric{
			Used:   &req,
			Unit:   "requests",
			Window: defaultUsageWindowLabel,
		}
		if item.sessions > 0 {
			snap.Metrics[keyPrefix+"_sessions"] = core.Metric{
				Used:   &sess,
				Unit:   "sessions",
				Window: defaultUsageWindowLabel,
			}
		}
	}
}

func emitToolMetrics(toolTotals map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		count int
	}
	var all []entry
	for name, count := range toolTotals {
		if count > 0 {
			all = append(all, entry{name: name, count: count})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})

	var parts []string
	limit := maxBreakdownRaw
	for i, item := range all {
		if i < limit {
			parts = append(parts, fmt.Sprintf("%s (%d)", item.name, item.count))
		}

		val := float64(item.count)
		snap.Metrics["tool_"+sanitizeMetricName(item.name)] = core.Metric{
			Used:   &val,
			Unit:   "calls",
			Window: defaultUsageWindowLabel,
		}
	}

	if len(all) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(all)-limit))
	}

	if len(parts) > 0 {
		snap.Raw["tool_usage"] = strings.Join(parts, ", ")
	}
}

func aggregateTokenTotals(modelTotals map[string]tokenUsage) tokenUsage {
	var total tokenUsage
	for _, usage := range modelTotals {
		total.InputTokens += usage.InputTokens
		total.CachedInputTokens += usage.CachedInputTokens
		total.OutputTokens += usage.OutputTokens
		total.ReasoningTokens += usage.ReasoningTokens
		total.ToolTokens += usage.ToolTokens
		total.TotalTokens += usage.TotalTokens
	}
	return total
}

func setUsageMetric(snap *core.UsageSnapshot, key string, value float64) {
	if value <= 0 {
		return
	}
	snap.Metrics[key] = core.Metric{
		Used:   &value,
		Unit:   "tokens",
		Window: defaultUsageWindowLabel,
	}
}

func addUsage(target map[string]tokenUsage, name string, delta tokenUsage) {
	current := target[name]
	current.InputTokens += delta.InputTokens
	current.CachedInputTokens += delta.CachedInputTokens
	current.OutputTokens += delta.OutputTokens
	current.ReasoningTokens += delta.ReasoningTokens
	current.ToolTokens += delta.ToolTokens
	current.TotalTokens += delta.TotalTokens
	target[name] = current
}

func addDailyUsage(target map[string]map[string]float64, name, day string, value float64) {
	if day == "" || value <= 0 {
		return
	}
	if target[name] == nil {
		target[name] = make(map[string]float64)
	}
	target[name][day] += value
}

func sortUsageEntries(values map[string]tokenUsage) []usageEntry {
	out := make([]usageEntry, 0, len(values))
	for name, data := range values {
		out = append(out, usageEntry{Name: name, Data: data})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Data.TotalTokens == out[j].Data.TotalTokens {
			return out[i].Name < out[j].Name
		}
		return out[i].Data.TotalTokens > out[j].Data.TotalTokens
	})
	return out
}

func formatUsageSummary(entries []usageEntry, max int) string {
	total := 0
	for _, entry := range entries {
		total += entry.Data.TotalTokens
	}
	if total <= 0 {
		return ""
	}

	limit := max
	if limit > len(entries) {
		limit = len(entries)
	}

	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		entry := entries[i]
		pct := float64(entry.Data.TotalTokens) / float64(total) * 100
		parts = append(parts, fmt.Sprintf("%s %s (%.0f%%)", entry.Name, shared.FormatTokenCount(entry.Data.TotalTokens), pct))
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func formatNamedCountMap(m map[string]int, unit string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for name, count := range m {
		if count <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %d %s", name, count, unit))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func isGeminiToolCallSuccessful(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "" || status == "success" || status == "succeeded" || status == "ok" || status == "completed"
}

func isGeminiMutatingTool(toolName string) bool {
	toolName = strings.ToLower(strings.TrimSpace(toolName))
	if toolName == "" {
		return false
	}
	return strings.Contains(toolName, "edit") ||
		strings.Contains(toolName, "write") ||
		strings.Contains(toolName, "create") ||
		strings.Contains(toolName, "delete") ||
		strings.Contains(toolName, "rename") ||
		strings.Contains(toolName, "move") ||
		strings.Contains(toolName, "replace")
}

func extractGeminiToolCommand(raw json.RawMessage) string {
	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	var command string
	var walk func(v any)
	walk = func(v any) {
		if command != "" || v == nil {
			return
		}
		switch value := v.(type) {
		case map[string]any:
			for key, child := range value {
				k := strings.ToLower(strings.TrimSpace(key))
				if k == "command" || k == "cmd" || k == "script" || k == "shell_command" {
					if s, ok := child.(string); ok {
						command = strings.TrimSpace(s)
						return
					}
				}
			}
			for _, child := range value {
				walk(child)
				if command != "" {
					return
				}
			}
		case []any:
			for _, child := range value {
				walk(child)
				if command != "" {
					return
				}
			}
		}
	}
	walk(payload)
	return command
}

func extractGeminiToolPaths(raw json.RawMessage) []string {
	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}

	pathHints := map[string]bool{
		"path": true, "paths": true, "file": true, "files": true, "filepath": true, "file_path": true,
		"cwd": true, "dir": true, "directory": true, "target": true, "pattern": true, "glob": true,
		"from": true, "to": true, "include": true, "exclude": true,
	}

	candidates := make(map[string]bool)
	var walk func(v any, hinted bool)
	walk = func(v any, hinted bool) {
		switch value := v.(type) {
		case map[string]any:
			for key, child := range value {
				k := strings.ToLower(strings.TrimSpace(key))
				childHinted := hinted || pathHints[k] || strings.Contains(k, "path") || strings.Contains(k, "file")
				walk(child, childHinted)
			}
		case []any:
			for _, child := range value {
				walk(child, hinted)
			}
		case string:
			if !hinted {
				return
			}
			for _, token := range extractGeminiPathTokens(value) {
				candidates[token] = true
			}
		}
	}
	walk(payload, false)

	out := make([]string, 0, len(candidates))
	for c := range candidates {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func extractGeminiPathTokens(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		fields = []string{raw}
	}

	var out []string
	for _, field := range fields {
		token := strings.Trim(field, "\"'`()[]{}<>,:;")
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "file://") {
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		if !strings.Contains(token, "/") && !strings.Contains(token, "\\") && !strings.Contains(token, ".") {
			continue
		}
		token = strings.TrimPrefix(token, "./")
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return lo.Uniq(out)
}

func estimateGeminiToolLineDelta(raw json.RawMessage) (added int, removed int) {
	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		return 0, 0
	}
	lineCount := func(text string) int {
		text = strings.TrimSpace(text)
		if text == "" {
			return 0
		}
		return strings.Count(text, "\n") + 1
	}
	var walk func(v any)
	walk = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			var oldText, newText string
			for _, key := range []string{"old_string", "old_text", "from", "replace"} {
				if rawValue, ok := value[key]; ok {
					if s, ok := rawValue.(string); ok {
						oldText = s
						break
					}
				}
			}
			for _, key := range []string{"new_string", "new_text", "to", "with"} {
				if rawValue, ok := value[key]; ok {
					if s, ok := rawValue.(string); ok {
						newText = s
						break
					}
				}
			}
			if oldText != "" || newText != "" {
				removed += lineCount(oldText)
				added += lineCount(newText)
			}
			if rawValue, ok := value["content"]; ok {
				if s, ok := rawValue.(string); ok {
					added += lineCount(s)
				}
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(payload)
	return added, removed
}

func extractGeminiToolDiffStat(raw json.RawMessage) (geminiDiffStat, bool) {
	var empty geminiDiffStat
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return empty, false
	}

	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return empty, false
	}
	diffRaw, ok := root["diffStat"]
	if !ok {
		return empty, false
	}

	var stat geminiDiffStat
	if json.Unmarshal(diffRaw, &stat) != nil {
		return empty, false
	}

	stat.ModelAddedLines = max(0, stat.ModelAddedLines)
	stat.ModelRemovedLines = max(0, stat.ModelRemovedLines)
	stat.ModelAddedChars = max(0, stat.ModelAddedChars)
	stat.ModelRemovedChars = max(0, stat.ModelRemovedChars)
	stat.UserAddedLines = max(0, stat.UserAddedLines)
	stat.UserRemovedLines = max(0, stat.UserRemovedLines)
	stat.UserAddedChars = max(0, stat.UserAddedChars)
	stat.UserRemovedChars = max(0, stat.UserRemovedChars)

	if stat.ModelAddedLines == 0 &&
		stat.ModelRemovedLines == 0 &&
		stat.ModelAddedChars == 0 &&
		stat.ModelRemovedChars == 0 &&
		stat.UserAddedLines == 0 &&
		stat.UserRemovedLines == 0 &&
		stat.UserAddedChars == 0 &&
		stat.UserRemovedChars == 0 {
		return empty, false
	}

	return stat, true
}

func inferGeminiLanguageFromPath(path string) string {
	p := strings.ToLower(strings.TrimSpace(path))
	if p == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(p))
	switch base {
	case "dockerfile":
		return "docker"
	case "makefile":
		return "make"
	}
	switch strings.ToLower(filepath.Ext(p)) {
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
	case ".vue":
		return "vue"
	case ".svelte":
		return "svelte"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	}
	return ""
}

func usageDelta(current, previous tokenUsage) tokenUsage {
	return tokenUsage{
		InputTokens:       current.InputTokens - previous.InputTokens,
		CachedInputTokens: current.CachedInputTokens - previous.CachedInputTokens,
		OutputTokens:      current.OutputTokens - previous.OutputTokens,
		ReasoningTokens:   current.ReasoningTokens - previous.ReasoningTokens,
		ToolTokens:        current.ToolTokens - previous.ToolTokens,
		TotalTokens:       current.TotalTokens - previous.TotalTokens,
	}
}

func validUsageDelta(delta tokenUsage) bool {
	return delta.InputTokens >= 0 &&
		delta.CachedInputTokens >= 0 &&
		delta.OutputTokens >= 0 &&
		delta.ReasoningTokens >= 0 &&
		delta.ToolTokens >= 0 &&
		delta.TotalTokens >= 0
}

func normalizeModelName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}

func normalizeClientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Other"
	}
	return name
}

func sanitizeMetricName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "unknown"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func getModelContextLimit(model string) int {
	model = strings.ToLower(model)
	switch {
	case strings.Contains(model, "1.5-pro"), strings.Contains(model, "1.5-flash-8b"):
		return 2_000_000
	case strings.Contains(model, "1.5-flash"):
		return 1_000_000
	case strings.Contains(model, "2.0-flash"):
		return 1_000_000
	case strings.Contains(model, "gemini-3"), strings.Contains(model, "gemini-exp"):
		return 2_000_000
	case strings.Contains(model, "pro"):
		return 32_000
	case strings.Contains(model, "flash"):
		return 32_000
	}
	return 0
}

func dayFromTimestamp(timestamp string) string {
	if timestamp == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, timestamp); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	if len(timestamp) >= 10 {
		candidate := timestamp[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func dayFromSession(startTime, lastUpdated string) string {
	if day := dayFromTimestamp(lastUpdated); day != "" {
		return day
	}
	return dayFromTimestamp(startTime)
}

func storeSeries(snap *core.UsageSnapshot, key string, values map[string]float64) {
	if len(values) == 0 {
		return
	}
	snap.DailySeries[key] = core.SortedTimePoints(values)
}

func latestSeriesValue(values map[string]float64) (string, float64) {
	if len(values) == 0 {
		return "", 0
	}
	dates := slices.Sorted(maps.Keys(values))
	last := dates[len(dates)-1]
	return last, values[last]
}

func sumLastNDays(values map[string]float64, days int) float64 {
	if len(values) == 0 || days <= 0 {
		return 0
	}
	lastDate, _ := latestSeriesValue(values)
	if lastDate == "" {
		return 0
	}
	end, err := time.Parse("2006-01-02", lastDate)
	if err != nil {
		return 0
	}
	start := end.AddDate(0, 0, -(days - 1))

	total := 0.0
	for date, value := range values {
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}
		if !t.Before(start) && !t.After(end) {
			total += value
		}
	}
	return total
}

func setUsedMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if value <= 0 {
		return
	}
	v := value
	snap.Metrics[key] = core.Metric{
		Used:   &v,
		Unit:   unit,
		Window: window,
	}
}

func setPercentMetric(snap *core.UsageSnapshot, key string, value float64, window string) {
	if value < 0 {
		return
	}
	if value > 100 {
		value = 100
	}
	v := value
	limit := 100.0
	remaining := 100 - value
	snap.Metrics[key] = core.Metric{
		Used:      &v,
		Limit:     &limit,
		Remaining: &remaining,
		Unit:      "%",
		Window:    window,
	}
}

func isQuotaLimitMessage(content json.RawMessage) bool {
	text := strings.ToLower(parseMessageContentText(content))
	if text == "" {
		return false
	}
	return strings.Contains(text, "usage limit reached") ||
		strings.Contains(text, "all pro models") ||
		strings.Contains(text, "/stats for usage details")
}

func parseMessageContentText(content json.RawMessage) string {
	content = bytes.TrimSpace(content)
	if len(content) == 0 {
		return ""
	}

	var asString string
	if content[0] == '"' && json.Unmarshal(content, &asString) == nil {
		return asString
	}

	var asArray []map[string]any
	if content[0] == '[' && json.Unmarshal(content, &asArray) == nil {
		var parts []string
		for _, item := range asArray {
			if text, ok := item["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}

	return string(content)
}
