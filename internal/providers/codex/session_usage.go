package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func codexSessionUsageBreakdownsEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("OPENUSAGE_CODEX_SKIP_SESSION_BREAKDOWNS")))
	switch value {
	case "1", "true", "yes", "on":
		return false
	default:
		return true
	}
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
	modelCost := make(map[string]float64)
	dailyCost := make(map[string]float64)
	var totalCostUSD float64
	var todayCostUSD float64
	today := time.Now().UTC().Format("2006-01-02")
	totalRequests := 0
	requestsToday := 0
	promptCount := 0
	commits := 0
	completedWithoutCallID := 0

	sessionFiles, err := shared.CollectFilesByExt([]string{sessionsDir}, map[string]bool{".jsonl": true})
	if err != nil {
		return fmt.Errorf("collect codex session files: %w", err)
	}
	for _, path := range sessionFiles {
		defaultDay := dayFromSessionPath(path, sessionsDir)
		sessionClient := "Other"
		// sessionDefaultModel is the model_id captured from the session header
		// (or refreshed by a turn_context line); it acts as the fallback when
		// a token_count event does not carry its own model field. currentModel
		// is the resolved value used for the immediately-following accounting.
		sessionDefaultModel := "unknown"
		currentModel := "unknown"
		var previous tokenUsage
		var hasPrevious bool
		var countedSession bool
		if err := walkSessionFile(path, func(record sessionLine) error {
			switch {
			case record.SessionMeta != nil:
				sessionClient = classifyClient(record.SessionMeta.Source, record.SessionMeta.Originator)
				if m := core.FirstNonEmpty(record.SessionMeta.Model, record.SessionMeta.ModelID); m != "" {
					sessionDefaultModel = m
					currentModel = m
				}
			case record.TurnContext != nil:
				if m := core.FirstNonEmpty(record.TurnContext.Model, record.TurnContext.ModelID); strings.TrimSpace(m) != "" {
					sessionDefaultModel = m
					currentModel = m
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
				// Per-message override: when a token_count event carries its
				// own model, prefer it. Otherwise fall back to the session
				// default captured from the header / last turn_context.
				if perEvent := core.FirstNonEmpty(payload.Model, payload.ModelID); perEvent != "" {
					currentModel = perEvent
				} else {
					currentModel = sessionDefaultModel
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

				cost := estimateUsageCost(currentModel, delta)
				if cost > 0 {
					modelCost[modelName] += cost
					totalCostUSD += cost
					if day != "" {
						dailyCost[day] += cost
					}
					if day == today {
						todayCostUSD += cost
					}
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
		}); err != nil {
			return fmt.Errorf("read codex session file %s: %w", path, err)
		}
	}

	emitBreakdownMetrics("model", modelTotals, modelDaily, snap)
	emitBreakdownMetrics("client", clientTotals, clientDaily, snap)
	emitClientSessionMetrics(clientSessions, snap)
	emitClientRequestMetrics(clientRequests, snap)
	emitToolMetrics(toolCalls, callTool, callOutcome, completedWithoutCallID, snap)
	emitLanguageMetrics(langRequests, snap)
	emitProductivityMetrics(stats, promptCount, commits, totalRequests, requestsToday, clientSessions, snap)
	emitDailyUsageSeries(dailyTokenTotals, dailyRequestTotals, interfaceDaily, snap)
	emitCostMetrics(modelCost, dailyCost, totalCostUSD, todayCostUSD, snap)

	return nil
}
