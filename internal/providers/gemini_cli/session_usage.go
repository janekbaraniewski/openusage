package gemini_cli

import (
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

func mapKeysSorted(values map[string]bool) []string {
	if len(values) == 0 {
		return nil
	}
	out := core.SortedStringKeys(values)
	return lo.Filter(out, func(key string, _ int) bool { return strings.TrimSpace(key) != "" })
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
