package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func (p *Provider) readLatestSession(sessionsDir string, snap *core.UsageSnapshot) error {
	latestFile, err := findLatestSessionFile(sessionsDir)
	if err != nil {
		return fmt.Errorf("finding latest session: %w", err)
	}

	snap.Raw["latest_session_file"] = filepath.Base(latestFile)

	lastPayload, err := findLastTokenCount(latestFile)
	if err != nil {
		return fmt.Errorf("reading session: %w", err)
	}

	if lastPayload == nil {
		return fmt.Errorf("no token_count events in latest session")
	}

	if lastPayload.Info != nil {
		info := lastPayload.Info
		total := info.TotalTokenUsage

		inputTokens := float64(total.InputTokens)
		snap.Metrics["session_input_tokens"] = core.Metric{Used: &inputTokens, Unit: "tokens", Window: "session"}

		outputTokens := float64(total.OutputTokens)
		snap.Metrics["session_output_tokens"] = core.Metric{Used: &outputTokens, Unit: "tokens", Window: "session"}

		cachedTokens := float64(total.CachedInputTokens)
		snap.Metrics["session_cached_tokens"] = core.Metric{Used: &cachedTokens, Unit: "tokens", Window: "session"}

		if total.ReasoningOutputTokens > 0 {
			reasoning := float64(total.ReasoningOutputTokens)
			snap.Metrics["session_reasoning_tokens"] = core.Metric{Used: &reasoning, Unit: "tokens", Window: "session"}
		}

		totalTokens := float64(total.TotalTokens)
		snap.Metrics["session_total_tokens"] = core.Metric{Used: &totalTokens, Unit: "tokens", Window: "session"}

		if info.ModelContextWindow > 0 {
			ctxWindow := float64(info.ModelContextWindow)
			ctxUsed := float64(total.InputTokens)
			snap.Metrics["context_window"] = core.Metric{Limit: &ctxWindow, Used: &ctxUsed, Unit: "tokens"}
		}
	}

	if lastPayload.RateLimits != nil {
		rl := lastPayload.RateLimits
		rateLimitSet := false

		if rl.Primary != nil {
			limit := float64(100)
			used := rl.Primary.UsedPercent
			remaining := 100 - used
			windowStr := formatWindow(rl.Primary.WindowMinutes)
			snap.Metrics["rate_limit_primary"] = core.Metric{Limit: &limit, Used: &used, Remaining: &remaining, Unit: "%", Window: windowStr}

			if rl.Primary.ResetsAt > 0 {
				snap.Resets["rate_limit_primary"] = time.Unix(rl.Primary.ResetsAt, 0)
			}
			rateLimitSet = true
		}

		if rl.Secondary != nil {
			limit := float64(100)
			used := rl.Secondary.UsedPercent
			remaining := 100 - used
			windowStr := formatWindow(rl.Secondary.WindowMinutes)
			snap.Metrics["rate_limit_secondary"] = core.Metric{Limit: &limit, Used: &used, Remaining: &remaining, Unit: "%", Window: windowStr}

			if rl.Secondary.ResetsAt > 0 {
				snap.Resets["rate_limit_secondary"] = time.Unix(rl.Secondary.ResetsAt, 0)
			}
			rateLimitSet = true
		}

		if rl.Credits != nil {
			if rl.Credits.Unlimited {
				snap.Raw["credits"] = "unlimited"
			} else if rl.Credits.HasCredits {
				snap.Raw["credits"] = "available"
				if rl.Credits.Balance != nil {
					snap.Raw["credit_balance"] = fmt.Sprintf("$%.2f", *rl.Credits.Balance)
				}
			} else {
				snap.Raw["credits"] = "none"
			}
		}

		if rl.PlanType != nil {
			snap.Raw["plan_type"] = *rl.PlanType
		}
		if rateLimitSet && snap.Raw["rate_limit_source"] == "" {
			snap.Raw["rate_limit_source"] = "session"
		}
	}

	return nil
}

func (p *Provider) readSessionUsageBreakdowns(sessionsDir string, snap *core.UsageSnapshot) error {
	modelTotals := make(map[string]tokenUsage)
	clientTotals := make(map[string]tokenUsage)
	modelDaily := make(map[string]map[string]float64)
	clientDaily := make(map[string]map[string]float64)
	interfaceDaily := make(map[string]map[string]float64)
	dailyTokenTotals := make(map[string]float64)
	dailyRequestTotals := make(map[string]float64)
	clientSessions := make(map[string]int)
	clientRequests := make(map[string]int)
	toolCalls := make(map[string]int)
	langRequests := make(map[string]int)
	callTool := make(map[string]string)
	callOutcome := make(map[string]int)
	stats := patchStats{
		Files:   make(map[string]struct{}),
		Deleted: make(map[string]struct{}),
	}
	today := time.Now().UTC().Format("2006-01-02")
	totalRequests := 0
	requestsToday := 0
	promptCount := 0
	commits := 0
	completedWithoutCallID := 0

	walkErr := filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		defaultDay := dayFromSessionPath(path, sessionsDir)
		sessionClient := "Other"
		currentModel := "unknown"
		var previous tokenUsage
		var hasPrevious bool
		var countedSession bool
		return walkSessionFile(path, func(record sessionLine) error {
			switch {
			case record.SessionMeta != nil:
				sessionClient = classifyClient(record.SessionMeta.Source, record.SessionMeta.Originator)
				if record.SessionMeta.Model != "" {
					currentModel = record.SessionMeta.Model
				}
			case record.TurnContext != nil:
				if strings.TrimSpace(record.TurnContext.Model) != "" {
					currentModel = record.TurnContext.Model
				}
			case record.EventPayload != nil:
				payload := record.EventPayload
				if payload.Type == "user_message" {
					promptCount++
					return nil
				}
				if payload.Type != "token_count" || payload.Info == nil {
					return nil
				}

				total := payload.Info.TotalTokenUsage
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
					return nil
				}

				modelName := normalizeModelName(currentModel)
				clientName := normalizeClientName(sessionClient)
				day := dayFromTimestamp(record.Timestamp)
				if day == "" {
					day = defaultDay
				}

				addUsage(modelTotals, modelName, delta)
				addUsage(clientTotals, clientName, delta)
				addDailyUsage(modelDaily, modelName, day, float64(delta.TotalTokens))
				addDailyUsage(clientDaily, clientName, day, float64(delta.TotalTokens))
				addDailyUsage(interfaceDaily, clientInterfaceBucket(clientName), day, 1)
				dailyTokenTotals[day] += float64(delta.TotalTokens)
				dailyRequestTotals[day]++
				clientRequests[clientName]++
				totalRequests++
				if day == today {
					requestsToday++
				}

				if !countedSession {
					clientSessions[clientName]++
					countedSession = true
				}
			case record.ResponseItem != nil:
				item := record.ResponseItem
				switch item.Type {
				case "function_call":
					tool := normalizeToolName(item.Name)
					recordToolCall(toolCalls, callTool, item.CallID, tool)
					if strings.EqualFold(tool, "exec_command") {
						var args commandArgs
						if json.Unmarshal(item.Arguments, &args) == nil {
							recordCommandLanguage(args.Cmd, langRequests)
							if commandContainsGitCommit(args.Cmd) {
								commits++
							}
						}
					}
				case "custom_tool_call":
					tool := normalizeToolName(item.Name)
					recordToolCall(toolCalls, callTool, item.CallID, tool)
					if strings.EqualFold(tool, "apply_patch") {
						stats.PatchCalls++
						accumulatePatchStats(item.Input, &stats, langRequests)
					}
				case "web_search_call":
					recordToolCall(toolCalls, callTool, "", "web_search")
					completedWithoutCallID++
				case "function_call_output", "custom_tool_call_output":
					setToolCallOutcome(item.CallID, item.Output, callOutcome)
				}
			}

			return nil
		})
	})
	if walkErr != nil {
		return fmt.Errorf("walking session files: %w", walkErr)
	}

	emitBreakdownMetrics("model", modelTotals, modelDaily, snap)
	emitBreakdownMetrics("client", clientTotals, clientDaily, snap)
	emitClientSessionMetrics(clientSessions, snap)
	emitClientRequestMetrics(clientRequests, snap)
	emitToolMetrics(toolCalls, callTool, callOutcome, completedWithoutCallID, snap)
	emitLanguageMetrics(langRequests, snap)
	emitProductivityMetrics(stats, promptCount, commits, totalRequests, requestsToday, clientSessions, snap)
	emitDailyUsageSeries(dailyTokenTotals, dailyRequestTotals, interfaceDaily, snap)

	return nil
}

func recordToolCall(toolCalls map[string]int, callTool map[string]string, callID, tool string) {
	tool = normalizeToolName(tool)
	toolCalls[tool]++
	if strings.TrimSpace(callID) != "" {
		callTool[callID] = tool
	}
}

func normalizeToolName(tool string) string {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "unknown"
	}
	return tool
}

func setToolCallOutcome(callID, output string, outcomes map[string]int) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	outcomes[callID] = inferToolCallOutcome(output)
}

func inferToolCallOutcome(output string) int {
	lower := strings.ToLower(strings.TrimSpace(output))
	if lower == "" {
		return 1
	}
	if strings.Contains(lower, `"exit_code":0`) || strings.Contains(lower, "process exited with code 0") {
		return 1
	}
	if strings.Contains(lower, "cancelled") || strings.Contains(lower, "canceled") || strings.Contains(lower, "aborted") {
		return 3
	}
	if idx := strings.Index(lower, "process exited with code "); idx >= 0 {
		rest := lower[idx+len("process exited with code "):]
		n := 0
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		if n == 0 {
			return 1
		}
		return 2
	}
	if idx := strings.Index(lower, "exit code "); idx >= 0 {
		rest := lower[idx+len("exit code "):]
		n := 0
		foundDigit := false
		for _, r := range rest {
			if r < '0' || r > '9' {
				if foundDigit {
					break
				}
				continue
			}
			foundDigit = true
			n = n*10 + int(r-'0')
		}
		if !foundDigit || n == 0 {
			return 1
		}
		return 2
	}
	if strings.Contains(lower, `"exit_code":`) && !strings.Contains(lower, `"exit_code":0`) {
		return 2
	}
	if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
		return 2
	}
	return 1
}

func recordCommandLanguage(cmd string, langs map[string]int) {
	if language := detectCommandLanguage(cmd); language != "" {
		langs[language]++
	}
}

func detectCommandLanguage(cmd string) string {
	trimmed := strings.TrimSpace(strings.ToLower(cmd))
	if trimmed == "" {
		return ""
	}
	switch {
	case strings.Contains(trimmed, " go ") || strings.HasPrefix(trimmed, "go ") || strings.Contains(trimmed, "gofmt ") || strings.Contains(trimmed, "golangci-lint"):
		return "go"
	case strings.Contains(trimmed, " terraform ") || strings.HasPrefix(trimmed, "terraform "):
		return "terraform"
	case strings.Contains(trimmed, " python ") || strings.HasPrefix(trimmed, "python ") || strings.HasPrefix(trimmed, "python3 "):
		return "python"
	case strings.Contains(trimmed, " npm ") || strings.HasPrefix(trimmed, "npm ") || strings.Contains(trimmed, " yarn ") || strings.HasPrefix(trimmed, "pnpm ") || strings.Contains(trimmed, " node "):
		return "ts"
	case strings.Contains(trimmed, " cargo ") || strings.HasPrefix(trimmed, "cargo ") || strings.Contains(trimmed, " rustc "):
		return "rust"
	case strings.Contains(trimmed, " java ") || strings.HasPrefix(trimmed, "java ") || strings.Contains(trimmed, " gradle ") || strings.Contains(trimmed, " mvn "):
		return "java"
	case strings.Contains(trimmed, ".log"):
		return "log"
	case strings.Contains(trimmed, ".txt"):
		return "txt"
	default:
		return "shell"
	}
}

func commandContainsGitCommit(cmd string) bool {
	normalized := " " + strings.ToLower(cmd) + " "
	return strings.Contains(normalized, " git commit ")
}

func accumulatePatchStats(input string, stats *patchStats, langs map[string]int) {
	if stats == nil {
		return
	}
	lines := strings.Split(input, "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "*** Update File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "*** Add File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "*** Delete File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				stats.Deleted[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "*** Move to: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Move to: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "***"):
			continue
		case strings.HasPrefix(line, "+"):
			stats.Added++
		case strings.HasPrefix(line, "-"):
			stats.Removed++
		}
	}
}

func languageFromPath(path string) string {
	lower := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasSuffix(lower, ".go"):
		return "go"
	case strings.HasSuffix(lower, ".tf"):
		return "terraform"
	case strings.HasSuffix(lower, ".ts"), strings.HasSuffix(lower, ".tsx"), strings.HasSuffix(lower, ".js"), strings.HasSuffix(lower, ".jsx"):
		return "ts"
	case strings.HasSuffix(lower, ".py"):
		return "python"
	case strings.HasSuffix(lower, ".rs"):
		return "rust"
	case strings.HasSuffix(lower, ".java"):
		return "java"
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return "yaml"
	case strings.HasSuffix(lower, ".json"):
		return "json"
	case strings.HasSuffix(lower, ".md"):
		return "md"
	case strings.HasSuffix(lower, ".tpl"):
		return "tpl"
	case strings.HasSuffix(lower, ".txt"):
		return "txt"
	case strings.HasSuffix(lower, ".log"):
		return "log"
	case strings.HasSuffix(lower, ".sh"), strings.HasSuffix(lower, ".zsh"), strings.HasSuffix(lower, ".bash"):
		return "shell"
	default:
		return ""
	}
}

func emitClientRequestMetrics(clientRequests map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		count int
	}
	var all []entry
	interfaceTotals := make(map[string]float64)
	for name, count := range clientRequests {
		if count > 0 {
			all = append(all, entry{name: name, count: count})
			interfaceTotals[clientInterfaceBucket(name)] += float64(count)
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
		snap.Metrics["client_"+sanitizeMetricName(item.name)+"_requests"] = core.Metric{Used: &value, Unit: "requests", Window: defaultUsageWindowLabel}
	}
	for bucket, value := range interfaceTotals {
		v := value
		snap.Metrics["interface_"+sanitizeMetricName(bucket)] = core.Metric{Used: &v, Unit: "requests", Window: defaultUsageWindowLabel}
	}
}

func clientInterfaceBucket(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lower, "desktop"):
		return "desktop_app"
	case strings.Contains(lower, "cli"), strings.Contains(lower, "exec"), strings.Contains(lower, "terminal"):
		return "cli_agents"
	case strings.Contains(lower, "ide"), strings.Contains(lower, "vscode"), strings.Contains(lower, "editor"):
		return "ide"
	case strings.Contains(lower, "cloud"), strings.Contains(lower, "web"):
		return "cloud_agents"
	case strings.Contains(lower, "human"), strings.Contains(lower, "other"):
		return "human"
	default:
		return sanitizeMetricName(name)
	}
}

func emitToolMetrics(toolCalls map[string]int, callTool map[string]string, callOutcome map[string]int, completedWithoutCallID int, snap *core.UsageSnapshot) {
	var all []countEntry
	totalCalls := 0
	for name, count := range toolCalls {
		if count <= 0 {
			continue
		}
		all = append(all, countEntry{name: name, count: count})
		totalCalls += count
		v := float64(count)
		snap.Metrics["tool_"+sanitizeMetricName(name)] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if totalCalls <= 0 {
		return
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})

	completed := completedWithoutCallID
	errored := 0
	cancelled := 0
	for callID := range callTool {
		switch callOutcome[callID] {
		case 2:
			errored++
		case 3:
			cancelled++
		default:
			completed++
		}
	}
	if completed+errored+cancelled < totalCalls {
		completed += totalCalls - (completed + errored + cancelled)
	}

	totalV := float64(totalCalls)
	snap.Metrics["tool_calls_total"] = core.Metric{Used: &totalV, Unit: "calls", Window: defaultUsageWindowLabel}
	if completed > 0 {
		v := float64(completed)
		snap.Metrics["tool_completed"] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if errored > 0 {
		v := float64(errored)
		snap.Metrics["tool_errored"] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if cancelled > 0 {
		v := float64(cancelled)
		snap.Metrics["tool_cancelled"] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if totalCalls > 0 {
		success := float64(completed) / float64(totalCalls) * 100
		snap.Metrics["tool_success_rate"] = core.Metric{Used: &success, Unit: "%", Window: defaultUsageWindowLabel}
	}
	snap.Raw["tool_usage"] = formatCountSummary(all, maxBreakdownRaw)
}

func emitLanguageMetrics(langRequests map[string]int, snap *core.UsageSnapshot) {
	var all []countEntry
	for language, count := range langRequests {
		if count <= 0 {
			continue
		}
		all = append(all, countEntry{name: language, count: count})
		v := float64(count)
		snap.Metrics["lang_"+sanitizeMetricName(language)] = core.Metric{Used: &v, Unit: "requests", Window: defaultUsageWindowLabel}
	}
	if len(all) == 0 {
		return
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})
	snap.Raw["language_usage"] = formatCountSummary(all, maxBreakdownRaw)
}

func emitProductivityMetrics(stats patchStats, promptCount, commits, totalRequests, requestsToday int, clientSessions map[string]int, snap *core.UsageSnapshot) {
	if totalRequests > 0 {
		v := float64(totalRequests)
		snap.Metrics["total_ai_requests"] = core.Metric{Used: &v, Unit: "requests", Window: defaultUsageWindowLabel}
		snap.Metrics["composer_requests"] = core.Metric{Used: &v, Unit: "requests", Window: defaultUsageWindowLabel}
	}
	if requestsToday > 0 {
		v := float64(requestsToday)
		snap.Metrics["requests_today"] = core.Metric{Used: &v, Unit: "requests", Window: "today"}
		snap.Metrics["today_composer_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "today"}
	}

	totalSessions := 0
	for _, count := range clientSessions {
		totalSessions += count
	}
	if totalSessions > 0 {
		v := float64(totalSessions)
		snap.Metrics["composer_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: defaultUsageWindowLabel}
	}

	if metric, ok := snap.Metrics["context_window"]; ok && metric.Used != nil && metric.Limit != nil && *metric.Limit > 0 {
		pct := *metric.Used / *metric.Limit * 100
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		snap.Metrics["composer_context_pct"] = core.Metric{Used: &pct, Unit: "%", Window: metric.Window}
	}

	if stats.Added > 0 {
		v := float64(stats.Added)
		snap.Metrics["composer_lines_added"] = core.Metric{Used: &v, Unit: "lines", Window: defaultUsageWindowLabel}
	}
	if stats.Removed > 0 {
		v := float64(stats.Removed)
		snap.Metrics["composer_lines_removed"] = core.Metric{Used: &v, Unit: "lines", Window: defaultUsageWindowLabel}
	}
	if filesChanged := len(stats.Files); filesChanged > 0 {
		v := float64(filesChanged)
		snap.Metrics["composer_files_changed"] = core.Metric{Used: &v, Unit: "files", Window: defaultUsageWindowLabel}
		snap.Metrics["ai_tracked_files"] = core.Metric{Used: &v, Unit: "files", Window: defaultUsageWindowLabel}
	}
	if deleted := len(stats.Deleted); deleted > 0 {
		v := float64(deleted)
		snap.Metrics["ai_deleted_files"] = core.Metric{Used: &v, Unit: "files", Window: defaultUsageWindowLabel}
	}
	if commits > 0 {
		v := float64(commits)
		snap.Metrics["scored_commits"] = core.Metric{Used: &v, Unit: "commits", Window: defaultUsageWindowLabel}
	}
	if promptCount > 0 {
		v := float64(promptCount)
		snap.Metrics["total_prompts"] = core.Metric{Used: &v, Unit: "prompts", Window: defaultUsageWindowLabel}
	}
	if stats.PatchCalls > 0 {
		base := totalRequests
		if base < stats.PatchCalls {
			base = stats.PatchCalls
		}
		if base > 0 {
			pct := float64(stats.PatchCalls) / float64(base) * 100
			snap.Metrics["ai_code_percentage"] = core.Metric{Used: &pct, Unit: "%", Window: defaultUsageWindowLabel}
		}
	}
}

func emitDailyUsageSeries(dailyTokenTotals, dailyRequestTotals map[string]float64, interfaceDaily map[string]map[string]float64, snap *core.UsageSnapshot) {
	if len(dailyTokenTotals) > 0 {
		points := core.SortedTimePoints(dailyTokenTotals)
		snap.DailySeries["analytics_tokens"] = points
		snap.DailySeries["tokens_total"] = points
	}
	if len(dailyRequestTotals) > 0 {
		points := core.SortedTimePoints(dailyRequestTotals)
		snap.DailySeries["analytics_requests"] = points
		snap.DailySeries["requests"] = points
	}
	for name, byDay := range interfaceDaily {
		if len(byDay) == 0 {
			continue
		}
		key := sanitizeMetricName(name)
		snap.DailySeries["usage_client_"+key] = core.SortedTimePoints(byDay)
		snap.DailySeries["usage_source_"+key] = core.SortedTimePoints(byDay)
	}
}

func formatCountSummary(entries []countEntry, max int) string {
	if len(entries) == 0 || max <= 0 {
		return ""
	}
	total := 0
	for _, entry := range entries {
		total += entry.count
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
		pct := float64(entries[i].count) / float64(total) * 100
		parts = append(parts, fmt.Sprintf("%s %s (%.0f%%)", entries[i].name, shared.FormatTokenCount(entries[i].count), pct))
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
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
		if entry.Data.ReasoningOutputTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_reasoning_tokens", float64(entry.Data.ReasoningOutputTokens))
		}

		if byDay, ok := daily[entry.Name]; ok {
			series := core.SortedTimePoints(byDay)
			snap.DailySeries["tokens_"+prefix+"_"+sanitizeMetricName(entry.Name)] = series
			snap.DailySeries["usage_"+prefix+"_"+sanitizeMetricName(entry.Name)] = series
		}

		if prefix == "model" {
			rec := core.ModelUsageRecord{
				RawModelID:   entry.Name,
				RawSource:    "jsonl",
				Window:       defaultUsageWindowLabel,
				InputTokens:  core.Float64Ptr(float64(entry.Data.InputTokens)),
				OutputTokens: core.Float64Ptr(float64(entry.Data.OutputTokens)),
				TotalTokens:  core.Float64Ptr(float64(entry.Data.TotalTokens)),
			}
			if entry.Data.CachedInputTokens > 0 {
				rec.CachedTokens = core.Float64Ptr(float64(entry.Data.CachedInputTokens))
			}
			if entry.Data.ReasoningOutputTokens > 0 {
				rec.ReasoningTokens = core.Float64Ptr(float64(entry.Data.ReasoningOutputTokens))
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
		snap.Metrics["client_"+sanitizeMetricName(item.name)+"_sessions"] = core.Metric{Used: &value, Unit: "sessions", Window: defaultUsageWindowLabel}
	}
}

func setUsageMetric(snap *core.UsageSnapshot, key string, value float64) {
	if value <= 0 {
		return
	}
	snap.Metrics[key] = core.Metric{Used: &value, Unit: "tokens", Window: defaultUsageWindowLabel}
}

func addUsage(target map[string]tokenUsage, name string, delta tokenUsage) {
	current := target[name]
	current.InputTokens += delta.InputTokens
	current.CachedInputTokens += delta.CachedInputTokens
	current.OutputTokens += delta.OutputTokens
	current.ReasoningOutputTokens += delta.ReasoningOutputTokens
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

func usageDelta(current, previous tokenUsage) tokenUsage {
	return tokenUsage{
		InputTokens:           current.InputTokens - previous.InputTokens,
		CachedInputTokens:     current.CachedInputTokens - previous.CachedInputTokens,
		OutputTokens:          current.OutputTokens - previous.OutputTokens,
		ReasoningOutputTokens: current.ReasoningOutputTokens - previous.ReasoningOutputTokens,
		TotalTokens:           current.TotalTokens - previous.TotalTokens,
	}
}

func validUsageDelta(delta tokenUsage) bool {
	return delta.InputTokens >= 0 &&
		delta.CachedInputTokens >= 0 &&
		delta.OutputTokens >= 0 &&
		delta.ReasoningOutputTokens >= 0 &&
		delta.TotalTokens >= 0
}

func normalizeModelName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}

func classifyClient(source, originator string) string {
	src := strings.ToLower(strings.TrimSpace(source))
	org := strings.ToLower(strings.TrimSpace(originator))

	switch {
	case src == "openusage" || src == "codex":
		return "CLI"
	case strings.Contains(org, "desktop"):
		return "Desktop App"
	case strings.Contains(org, "exec") || src == "exec":
		return "Exec"
	case strings.Contains(org, "cli") || src == "cli":
		return "CLI"
	case src == "vscode" || src == "ide":
		return "IDE"
	case src == "":
		return "Other"
	default:
		return strings.ToUpper(src)
	}
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

func dayFromSessionPath(path, sessionsDir string) string {
	rel, err := filepath.Rel(sessionsDir, path)
	if err != nil {
		return ""
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 {
		return ""
	}

	candidate := fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
	if _, err := time.Parse("2006-01-02", candidate); err != nil {
		return ""
	}
	return candidate
}

func findLatestSessionFile(sessionsDir string) (string, error) {
	var files []string

	err := filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walking sessions dir: %w", err)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no session files found in %s", sessionsDir)
	}

	sort.Slice(files, func(i, j int) bool {
		si, _ := os.Stat(files[i])
		sj, _ := os.Stat(files[j])
		if si == nil || sj == nil {
			return false
		}
		return si.ModTime().After(sj.ModTime())
	})

	return files[0], nil
}

func findLastTokenCount(path string) (*eventPayload, error) {
	var lastPayload *eventPayload
	if err := walkSessionFile(path, func(record sessionLine) error {
		if record.EventPayload == nil || record.EventPayload.Type != "token_count" {
			return nil
		}
		payload := *record.EventPayload
		lastPayload = &payload
		return nil
	}); err != nil {
		return nil, err
	}
	return lastPayload, nil
}

func (p *Provider) readDailySessionCounts(sessionsDir string, snap *core.UsageSnapshot) {
	dayCounts := make(map[string]int)

	_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		rel, relErr := filepath.Rel(sessionsDir, path)
		if relErr != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 3 {
			dateStr := fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
			if _, parseErr := time.Parse("2006-01-02", dateStr); parseErr == nil {
				dayCounts[dateStr]++
			}
		}
		return nil
	})

	if len(dayCounts) == 0 {
		return
	}

	dates := core.SortedStringKeys(dayCounts)

	for _, d := range dates {
		snap.DailySeries["sessions"] = append(snap.DailySeries["sessions"], core.TimePoint{
			Date:  d,
			Value: float64(dayCounts[d]),
		})
	}
}

func formatWindow(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	remaining := minutes % 60
	if remaining == 0 {
		if hours >= 24 {
			days := hours / 24
			leftover := hours % 24
			if leftover == 0 {
				return fmt.Sprintf("%dd", days)
			}
			return fmt.Sprintf("%dd%dh", days, leftover)
		}
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, remaining)
}
