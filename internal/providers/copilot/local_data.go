package copilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func (p *Provider) readSessions(copilotDir string, snap *core.UsageSnapshot, logs logSummary) {
	sessionDir := filepath.Join(copilotDir, "session-state")
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return
	}

	snap.Raw["total_sessions"] = fmt.Sprintf("%d", len(entries))

	type sessionInfo struct {
		id                      string
		createdAt               time.Time
		updatedAt               time.Time
		cwd                     string
		repo                    string
		branch                  string
		client                  string
		summary                 string
		messages                int
		turns                   int
		model                   string
		responseChars           int
		reasoningChars          int
		toolCalls               int
		tokenUsed               int
		tokenTotal              int
		tokenBurn               float64
		usageCost               float64
		premiumRequests         int
		shutdownPremiumRequests int
		linesAdded              int
		linesRemoved            int
		filesModified           int
	}

	var sessions []sessionInfo
	dailyMessages := make(map[string]float64)
	dailySessions := make(map[string]float64)
	dailyToolCalls := make(map[string]float64)
	dailyTokens := make(map[string]float64)
	modelMessages := make(map[string]int)
	modelTurns := make(map[string]int)
	modelSessions := make(map[string]int)
	modelResponseChars := make(map[string]int)
	modelReasoningChars := make(map[string]int)
	modelToolCalls := make(map[string]int)
	dailyModelMessages := make(map[string]map[string]float64)
	dailyModelTokens := make(map[string]map[string]float64)
	modelInputTokens := make(map[string]float64)
	usageInputTokens := make(map[string]float64)
	usageOutputTokens := make(map[string]float64)
	usageCacheReadTokens := make(map[string]float64)
	usageCacheWriteTokens := make(map[string]float64)
	usageCost := make(map[string]float64)
	usageRequests := make(map[string]int)
	usageDuration := make(map[string]int64)
	dailyCost := make(map[string]float64)
	var latestQuotaSnapshots map[string]quotaSnapshotEntry
	var shutdownPremiumRequests int
	var shutdownLinesAdded, shutdownLinesRemoved, shutdownFilesModified int
	shutdownModelCost := make(map[string]float64)
	shutdownModelRequests := make(map[string]int)
	shutdownModelInputTokens := make(map[string]float64)
	shutdownModelOutputTokens := make(map[string]float64)
	toolUsageCounts := make(map[string]int)
	languageUsageCounts := make(map[string]int)
	changedFiles := make(map[string]bool)
	commitCommands := make(map[string]bool)
	clientLabels := make(map[string]string)
	clientTokens := make(map[string]float64)
	clientSessions := make(map[string]int)
	clientMessages := make(map[string]int)
	dailyClientTokens := make(map[string]map[string]float64)
	var inferredLinesAdded, inferredLinesRemoved int
	var inferredCommitCount int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		si := sessionInfo{id: entry.Name()}
		sessPath := filepath.Join(sessionDir, entry.Name())

		if wsData, err := os.ReadFile(filepath.Join(sessPath, "workspace.yaml")); err == nil {
			ws := parseSimpleYAML(string(wsData))
			si.cwd = ws["cwd"]
			si.repo = ws["repository"]
			si.branch = ws["branch"]
			si.summary = ws["summary"]
			si.createdAt = flexParseTime(ws["created_at"])
			si.updatedAt = flexParseTime(ws["updated_at"])
		}

		if te, ok := logs.SessionTokens[si.id]; ok {
			si.tokenUsed = te.Used
			si.tokenTotal = te.Total
			if !te.Timestamp.IsZero() {
				if si.createdAt.IsZero() {
					si.createdAt = te.Timestamp
				}
				if si.updatedAt.IsZero() || te.Timestamp.After(si.updatedAt) {
					si.updatedAt = te.Timestamp
				}
			}
		}
		if burn, ok := logs.SessionBurn[si.id]; ok {
			si.tokenBurn = burn
		}

		if evtData, err := os.ReadFile(filepath.Join(sessPath, "events.jsonl")); err == nil {
			currentModel := logs.DefaultModel
			var firstEventAt, lastEventAt time.Time
			lines := strings.Split(string(evtData), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				var evt sessionEvent
				if json.Unmarshal([]byte(line), &evt) != nil {
					continue
				}
				evtTime := flexParseTime(evt.Timestamp)
				if !evtTime.IsZero() {
					if firstEventAt.IsZero() || evtTime.Before(firstEventAt) {
						firstEventAt = evtTime
					}
					if lastEventAt.IsZero() || evtTime.After(lastEventAt) {
						lastEventAt = evtTime
					}
				}

				switch evt.Type {
				case "session.start":
					var start sessionStartData
					if json.Unmarshal(evt.Data, &start) == nil {
						if si.cwd == "" {
							si.cwd = start.Context.CWD
						}
						if si.repo == "" {
							si.repo = start.Context.Repository
						}
						if si.branch == "" {
							si.branch = start.Context.Branch
						}
						if si.createdAt.IsZero() {
							si.createdAt = flexParseTime(start.StartTime)
						}
						if currentModel == "" && start.SelectedModel != "" {
							currentModel = start.SelectedModel
						}
					}

				case "session.model_change":
					var mc modelChangeData
					if json.Unmarshal(evt.Data, &mc) == nil && mc.NewModel != "" {
						currentModel = mc.NewModel
					}

				case "session.info":
					var info sessionInfoData
					if json.Unmarshal(evt.Data, &info) == nil && info.InfoType == "model" {
						if m := extractModelFromInfoMsg(info.Message); m != "" {
							currentModel = m
						}
					}

				case "user.message":
					si.messages++
					day := parseDayFromTimestamp(evt.Timestamp)
					if day != "" {
						dailyMessages[day]++
					}
					if currentModel != "" {
						modelMessages[currentModel]++
						if day != "" {
							if dailyModelMessages[currentModel] == nil {
								dailyModelMessages[currentModel] = make(map[string]float64)
							}
							dailyModelMessages[currentModel][day]++
						}
					}

				case "assistant.turn_start":
					si.turns++
					if currentModel != "" {
						modelTurns[currentModel]++
					}

				case "assistant.message":
					var msg assistantMsgData
					if json.Unmarshal(evt.Data, &msg) == nil {
						si.responseChars += len(msg.Content)
						si.reasoningChars += len(msg.ReasoningTxt)
						if currentModel != "" {
							modelResponseChars[currentModel] += len(msg.Content)
							modelReasoningChars[currentModel] += len(msg.ReasoningTxt)
						}
						var tools []json.RawMessage
						if json.Unmarshal(msg.ToolRequests, &tools) == nil && len(tools) > 0 {
							si.toolCalls += len(tools)
							if currentModel != "" {
								modelToolCalls[currentModel] += len(tools)
							}
							for _, toolReq := range tools {
								toolName := extractCopilotToolName(toolReq)
								if toolName == "" {
									toolName = "unknown"
								}
								toolUsageCounts[toolName]++
								toolLower := strings.ToLower(strings.TrimSpace(toolName))
								paths := extractCopilotToolPaths(toolReq)
								for _, path := range paths {
									if lang := inferCopilotLanguageFromPath(path); lang != "" {
										languageUsageCounts[lang]++
									}
									if isCopilotMutatingTool(toolLower) {
										changedFiles[path] = true
									}
								}
								if isCopilotMutatingTool(toolLower) {
									added, removed := estimateCopilotToolLineDelta(toolReq)
									inferredLinesAdded += added
									inferredLinesRemoved += removed
								}
								cmd := extractCopilotToolCommand(toolReq)
								if cmd != "" {
									if strings.Contains(strings.ToLower(cmd), "git commit") && !commitCommands[cmd] {
										commitCommands[cmd] = true
										inferredCommitCount++
									}
								} else if strings.Contains(toolLower, "commit") {
									inferredCommitCount++
								}
							}
							day := parseDayFromTimestamp(evt.Timestamp)
							if day != "" {
								dailyToolCalls[day] += float64(len(tools))
							}
						}
					}

				case "assistant.usage":
					var usage assistantUsageData
					if json.Unmarshal(evt.Data, &usage) == nil && usage.Model != "" {
						usageInputTokens[usage.Model] += usage.InputTokens
						usageOutputTokens[usage.Model] += usage.OutputTokens
						usageCacheReadTokens[usage.Model] += usage.CacheReadTokens
						usageCacheWriteTokens[usage.Model] += usage.CacheWriteTokens
						usageCost[usage.Model] += usage.Cost
						usageRequests[usage.Model]++
						usageDuration[usage.Model] += usage.Duration

						si.usageCost += usage.Cost
						si.premiumRequests++

						day := parseDayFromTimestamp(evt.Timestamp)
						if day != "" {
							dailyCost[day] += usage.Cost
						}

						if len(usage.QuotaSnapshots) > 0 {
							latestQuotaSnapshots = usage.QuotaSnapshots
						}
					}

				case "session.shutdown":
					var shutdown sessionShutdownData
					if json.Unmarshal(evt.Data, &shutdown) == nil {
						shutdownPremiumRequests += shutdown.TotalPremiumRequests
						si.shutdownPremiumRequests += shutdown.TotalPremiumRequests

						si.linesAdded += shutdown.CodeChanges.LinesAdded
						si.linesRemoved += shutdown.CodeChanges.LinesRemoved
						si.filesModified += shutdown.CodeChanges.FilesModified
						shutdownLinesAdded += shutdown.CodeChanges.LinesAdded
						shutdownLinesRemoved += shutdown.CodeChanges.LinesRemoved
						shutdownFilesModified += shutdown.CodeChanges.FilesModified

						for model, metrics := range shutdown.ModelMetrics {
							shutdownModelCost[model] += metrics.Requests.Cost
							shutdownModelRequests[model] += metrics.Requests.Count
							shutdownModelInputTokens[model] += metrics.Usage.InputTokens
							shutdownModelOutputTokens[model] += metrics.Usage.OutputTokens
						}
					}
				}
			}
			if !firstEventAt.IsZero() && si.createdAt.IsZero() {
				si.createdAt = firstEventAt
			}
			if !lastEventAt.IsZero() && (si.updatedAt.IsZero() || lastEventAt.After(si.updatedAt)) {
				si.updatedAt = lastEventAt
			}
			si.model = currentModel
		}

		day := dayForSession(si.createdAt, si.updatedAt)
		if si.model != "" {
			modelSessions[si.model]++
		}
		if day != "" {
			dailySessions[day]++
		}

		clientLabel := normalizeCopilotClient(si.repo, si.cwd)
		clientKey := sanitizeMetricName(clientLabel)
		if clientKey == "" {
			clientKey = "cli"
		}
		si.client = clientLabel
		if _, ok := clientLabels[clientKey]; !ok {
			clientLabels[clientKey] = clientLabel
		}
		clientSessions[clientKey]++
		clientMessages[clientKey] += si.messages

		sessionTokens := float64(si.tokenUsed)
		if si.tokenBurn > 0 {
			sessionTokens = si.tokenBurn
		}
		if sessionTokens > 0 {
			clientTokens[clientKey] += sessionTokens
			if day != "" {
				dailyTokens[day] += sessionTokens
				if dailyClientTokens[clientKey] == nil {
					dailyClientTokens[clientKey] = make(map[string]float64)
				}
				dailyClientTokens[clientKey][day] += sessionTokens
			}
			if si.model != "" {
				modelInputTokens[si.model] += sessionTokens
				if day != "" {
					if dailyModelTokens[si.model] == nil {
						dailyModelTokens[si.model] = make(map[string]float64)
					}
					dailyModelTokens[si.model][day] += sessionTokens
				}
			}
		}
		sessions = append(sessions, si)
	}

	storeSeries(snap, "messages", dailyMessages)
	storeSeries(snap, "sessions", dailySessions)
	storeSeries(snap, "tool_calls", dailyToolCalls)
	storeSeries(snap, "tokens_total", dailyTokens)
	storeSeries(snap, "cli_messages", dailyMessages)
	storeSeries(snap, "cli_sessions", dailySessions)
	storeSeries(snap, "cli_tool_calls", dailyToolCalls)
	if len(dailyCost) > 0 {
		storeSeries(snap, "cost", dailyCost)
	}
	for model, dayCounts := range dailyModelMessages {
		safe := sanitizeMetricName(model)
		storeSeries(snap, "cli_messages_"+safe, dayCounts)
	}
	for model, dayCounts := range dailyModelTokens {
		safe := sanitizeMetricName(model)
		storeSeries(snap, "tokens_"+safe, dayCounts)
		storeSeries(snap, "cli_tokens_"+safe, dayCounts)
	}

	setRawStr(snap, "model_usage", formatModelMap(modelMessages, "msgs"))
	setRawStr(snap, "model_turns", formatModelMap(modelTurns, "turns"))
	setRawStr(snap, "model_sessions", formatModelMapPlain(modelSessions))
	setRawStr(snap, "model_response_chars", formatModelMap(modelResponseChars, "chars"))
	setRawStr(snap, "model_reasoning_chars", formatModelMap(modelReasoningChars, "chars"))
	setRawStr(snap, "model_tool_calls", formatModelMap(modelToolCalls, "calls"))

	sort.Slice(sessions, func(i, j int) bool {
		ti := sessions[i].updatedAt
		if ti.IsZero() {
			ti = sessions[i].createdAt
		}
		tj := sessions[j].updatedAt
		if tj.IsZero() {
			tj = sessions[j].createdAt
		}
		return ti.After(tj)
	})

	var totalMessages, totalTurns, totalResponse, totalReasoning, totalTools int
	totalTokens := 0.0
	for _, s := range sessions {
		totalMessages += s.messages
		totalTurns += s.turns
		totalResponse += s.responseChars
		totalReasoning += s.reasoningChars
		totalTools += s.toolCalls
		tokens := float64(s.tokenUsed)
		if s.tokenBurn > 0 {
			tokens = s.tokenBurn
		}
		totalTokens += tokens
	}
	setRawInt(snap, "total_cli_messages", totalMessages)
	setRawInt(snap, "total_cli_turns", totalTurns)
	setRawInt(snap, "total_response_chars", totalResponse)
	setRawInt(snap, "total_reasoning_chars", totalReasoning)
	setRawInt(snap, "total_tool_calls", totalTools)

	setUsedMetric(snap, "total_messages", float64(totalMessages), "messages", copilotAllTimeWindow)
	setUsedMetric(snap, "total_sessions", float64(len(sessions)), "sessions", copilotAllTimeWindow)
	setUsedMetric(snap, "total_turns", float64(totalTurns), "turns", copilotAllTimeWindow)
	setUsedMetric(snap, "total_tool_calls", float64(totalTools), "calls", copilotAllTimeWindow)
	setUsedMetric(snap, "tool_calls_total", float64(totalTools), "calls", copilotAllTimeWindow)
	if totalTools > 0 {
		setUsedMetric(snap, "tool_completed", float64(totalTools), "calls", copilotAllTimeWindow)
		setUsedMetric(snap, "tool_success_rate", 100.0, "%", copilotAllTimeWindow)
	}
	setUsedMetric(snap, "total_response_chars", float64(totalResponse), "chars", copilotAllTimeWindow)
	setUsedMetric(snap, "total_reasoning_chars", float64(totalReasoning), "chars", copilotAllTimeWindow)
	setUsedMetric(snap, "total_conversations", float64(len(sessions)), "sessions", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_messages", float64(totalMessages), "messages", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_turns", float64(totalTurns), "turns", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_sessions", float64(len(sessions)), "sessions", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_tool_calls", float64(totalTools), "calls", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_response_chars", float64(totalResponse), "chars", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_reasoning_chars", float64(totalReasoning), "chars", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_input_tokens", totalTokens, "tokens", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_total_tokens", totalTokens, "tokens", copilotAllTimeWindow)

	var totalUsageOutputTokens, totalUsageCacheRead, totalUsageCacheWrite, totalUsageCost float64
	var totalUsageRequests int
	for _, v := range usageOutputTokens {
		totalUsageOutputTokens += v
	}
	for _, v := range usageCacheReadTokens {
		totalUsageCacheRead += v
	}
	for _, v := range usageCacheWriteTokens {
		totalUsageCacheWrite += v
	}
	for _, v := range usageCost {
		totalUsageCost += v
	}
	for _, v := range usageRequests {
		totalUsageRequests += v
	}
	if totalUsageOutputTokens > 0 {
		setUsedMetric(snap, "cli_output_tokens", totalUsageOutputTokens, "tokens", copilotAllTimeWindow)
	}
	if totalUsageCacheRead > 0 {
		setUsedMetric(snap, "cli_cache_read_tokens", totalUsageCacheRead, "tokens", copilotAllTimeWindow)
	}
	if totalUsageCacheWrite > 0 {
		setUsedMetric(snap, "cli_cache_write_tokens", totalUsageCacheWrite, "tokens", copilotAllTimeWindow)
	}
	if totalUsageCost > 0 {
		setUsedMetric(snap, "cli_cost", totalUsageCost, "USD", copilotAllTimeWindow)
	}
	if totalUsageRequests > 0 {
		setUsedMetric(snap, "cli_premium_requests", float64(totalUsageRequests), "requests", copilotAllTimeWindow)
	} else if shutdownPremiumRequests > 0 {
		setUsedMetric(snap, "cli_premium_requests", float64(shutdownPremiumRequests), "requests", copilotAllTimeWindow)
	}
	if shutdownLinesAdded > 0 || shutdownLinesRemoved > 0 {
		setUsedMetric(snap, "cli_lines_added", float64(shutdownLinesAdded), "lines", copilotAllTimeWindow)
		setUsedMetric(snap, "cli_lines_removed", float64(shutdownLinesRemoved), "lines", copilotAllTimeWindow)
	}
	if shutdownFilesModified > 0 {
		setUsedMetric(snap, "cli_files_modified", float64(shutdownFilesModified), "files", copilotAllTimeWindow)
	}
	if totalUsageRequests > 0 {
		var totalDuration int64
		for _, d := range usageDuration {
			totalDuration += d
		}
		avgMs := float64(totalDuration) / float64(totalUsageRequests)
		setUsedMetric(snap, "cli_avg_latency_ms", avgMs, "ms", copilotAllTimeWindow)
	}

	if qs, ok := latestQuotaSnapshots["premium_interactions"]; ok {
		if _, exists := snap.Metrics["premium_interactions_quota"]; !exists {
			entitlement := float64(qs.EntitlementRequests)
			used := float64(qs.UsedRequests)
			remaining := entitlement - used
			if remaining < 0 {
				remaining = 0
			}
			snap.Metrics["premium_interactions_quota"] = core.Metric{
				Limit:     &entitlement,
				Used:      core.Float64Ptr(used),
				Remaining: core.Float64Ptr(remaining),
				Unit:      "requests",
				Window:    "billing-cycle",
			}
		}
	}

	if _, v := latestSeriesValue(dailyCost); v > 0 {
		setUsedMetric(snap, "cost_today", v, "USD", "today")
	}
	setUsedMetric(snap, "7d_cost", sumLastNDays(dailyCost, 7), "USD", "7d")

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
	setUsedMetric(snap, "7d_messages", sumLastNDays(dailyMessages, 7), "messages", "7d")
	setUsedMetric(snap, "7d_sessions", sumLastNDays(dailySessions, 7), "sessions", "7d")
	setUsedMetric(snap, "7d_tool_calls", sumLastNDays(dailyToolCalls, 7), "calls", "7d")
	setUsedMetric(snap, "7d_tokens", sumLastNDays(dailyTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "total_prompts", float64(totalMessages), "prompts", copilotAllTimeWindow)

	allModelTokens := make(map[string]float64, len(modelInputTokens))
	for k, v := range modelInputTokens {
		allModelTokens[k] = v
	}
	for k, v := range usageInputTokens {
		if allModelTokens[k] < v {
			allModelTokens[k] = v
		}
	}
	allModelMessages := make(map[string]int, len(modelMessages))
	for k, v := range modelMessages {
		allModelMessages[k] = v
	}
	for k, v := range usageRequests {
		if allModelMessages[k] < v {
			allModelMessages[k] = v
		}
	}
	topModels := topModelNames(allModelTokens, allModelMessages, maxCopilotModels)
	for _, model := range topModels {
		prefix := "model_" + sanitizeMetricName(model)
		rec := core.ModelUsageRecord{RawModelID: model, RawSource: "json", Window: copilotAllTimeWindow}

		inputTok := modelInputTokens[model]
		if v := usageInputTokens[model]; v > 0 {
			inputTok = v
		}
		outputTok := usageOutputTokens[model]
		cacheTok := usageCacheReadTokens[model] + usageCacheWriteTokens[model]

		setUsedMetric(snap, prefix+"_input_tokens", inputTok, "tokens", copilotAllTimeWindow)
		if inputTok > 0 {
			rec.InputTokens = core.Float64Ptr(inputTok)
		}
		if outputTok > 0 {
			setUsedMetric(snap, prefix+"_output_tokens", outputTok, "tokens", copilotAllTimeWindow)
			rec.OutputTokens = core.Float64Ptr(outputTok)
		}
		if cacheTok > 0 {
			rec.CachedTokens = core.Float64Ptr(cacheTok)
		}
		totalTok := inputTok + outputTok
		if totalTok > 0 {
			rec.TotalTokens = core.Float64Ptr(totalTok)
		}

		modelCost := usageCost[model]
		if modelCost == 0 {
			modelCost = shutdownModelCost[model]
		}
		if modelCost > 0 {
			rec.CostUSD = core.Float64Ptr(modelCost)
			setUsedMetric(snap, prefix+"_cost", modelCost, "USD", copilotAllTimeWindow)
		}

		if reqs := usageRequests[model]; reqs > 0 {
			rec.Requests = core.Float64Ptr(float64(reqs))
		}

		setUsedMetric(snap, prefix+"_messages", float64(modelMessages[model]), "messages", copilotAllTimeWindow)
		setUsedMetric(snap, prefix+"_turns", float64(modelTurns[model]), "turns", copilotAllTimeWindow)
		setUsedMetric(snap, prefix+"_sessions", float64(modelSessions[model]), "sessions", copilotAllTimeWindow)
		setUsedMetric(snap, prefix+"_tool_calls", float64(modelToolCalls[model]), "calls", copilotAllTimeWindow)
		setUsedMetric(snap, prefix+"_response_chars", float64(modelResponseChars[model]), "chars", copilotAllTimeWindow)
		setUsedMetric(snap, prefix+"_reasoning_chars", float64(modelReasoningChars[model]), "chars", copilotAllTimeWindow)
		snap.AppendModelUsage(rec)
	}

	topClients := topCopilotClientNames(clientTokens, clientSessions, clientMessages, maxCopilotClients)
	for _, client := range topClients {
		clientPrefix := "client_" + client
		setUsedMetric(snap, clientPrefix+"_total_tokens", clientTokens[client], "tokens", copilotAllTimeWindow)
		setUsedMetric(snap, clientPrefix+"_input_tokens", clientTokens[client], "tokens", copilotAllTimeWindow)
		setUsedMetric(snap, clientPrefix+"_sessions", float64(clientSessions[client]), "sessions", copilotAllTimeWindow)
		if byDay := dailyClientTokens[client]; len(byDay) > 0 {
			storeSeries(snap, "tokens_client_"+client, byDay)
		}
	}
	setRawStr(snap, "client_usage", formatCopilotClientUsage(topClients, clientLabels, clientTokens, clientSessions))
	setRawStr(snap, "tool_usage", formatModelMap(toolUsageCounts, "calls"))
	setRawStr(snap, "language_usage", formatModelMap(languageUsageCounts, "req"))
	for toolName, count := range toolUsageCounts {
		if count <= 0 {
			continue
		}
		setUsedMetric(snap, "tool_"+sanitizeMetricName(toolName), float64(count), "calls", copilotAllTimeWindow)
	}
	for lang, count := range languageUsageCounts {
		if count <= 0 {
			continue
		}
		setUsedMetric(snap, "lang_"+sanitizeMetricName(lang), float64(count), "requests", copilotAllTimeWindow)
	}

	linesAdded := shutdownLinesAdded
	if inferredLinesAdded > linesAdded {
		linesAdded = inferredLinesAdded
	}
	linesRemoved := shutdownLinesRemoved
	if inferredLinesRemoved > linesRemoved {
		linesRemoved = inferredLinesRemoved
	}
	filesChanged := shutdownFilesModified
	if len(changedFiles) > filesChanged {
		filesChanged = len(changedFiles)
	}
	if linesAdded > 0 {
		setUsedMetric(snap, "composer_lines_added", float64(linesAdded), "lines", copilotAllTimeWindow)
	}
	if linesRemoved > 0 {
		setUsedMetric(snap, "composer_lines_removed", float64(linesRemoved), "lines", copilotAllTimeWindow)
	}
	if filesChanged > 0 {
		setUsedMetric(snap, "composer_files_changed", float64(filesChanged), "files", copilotAllTimeWindow)
	}
	if inferredCommitCount > 0 {
		setUsedMetric(snap, "scored_commits", float64(inferredCommitCount), "commits", copilotAllTimeWindow)
	}
	if linesAdded > 0 || linesRemoved > 0 {
		hundred := 100.0
		zero := 0.0
		snap.Metrics["ai_code_percentage"] = core.Metric{
			Used:      &hundred,
			Remaining: &zero,
			Limit:     &hundred,
			Unit:      "%",
			Window:    copilotAllTimeWindow,
		}
	}

	if len(sessions) > 0 {
		r := sessions[0]
		if r.client != "" {
			snap.Raw["last_session_client"] = r.client
		}
		snap.Raw["last_session_repo"] = r.repo
		snap.Raw["last_session_branch"] = r.branch
		if r.summary != "" {
			snap.Raw["last_session_summary"] = r.summary
		}
		if !r.updatedAt.IsZero() {
			snap.Raw["last_session_time"] = r.updatedAt.Format(time.RFC3339)
		}
		if r.model != "" {
			snap.Raw["last_session_model"] = r.model
		}
		sessionTokens := float64(r.tokenUsed)
		if r.tokenBurn > 0 {
			sessionTokens = r.tokenBurn
		}
		if sessionTokens > 0 {
			snap.Raw["last_session_tokens"] = fmt.Sprintf("%.0f/%d", sessionTokens, r.tokenTotal)
			setUsedMetric(snap, "session_input_tokens", sessionTokens, "tokens", "session")
			setUsedMetric(snap, "session_total_tokens", sessionTokens, "tokens", "session")
			if r.tokenTotal > 0 {
				limit := float64(r.tokenTotal)
				snap.Metrics["context_window"] = core.Metric{
					Limit:     &limit,
					Used:      core.Float64Ptr(sessionTokens),
					Remaining: core.Float64Ptr(max(limit-sessionTokens, 0)),
					Unit:      "tokens",
					Window:    "session",
				}
			}
		}
	}
}
