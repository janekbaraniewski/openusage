package claude_code

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

func (p *Provider) readConversationJSONL(projectsDir, altProjectsDir string, snap *core.UsageSnapshot) error {
	jsonlFiles := collectJSONLFiles(projectsDir)
	if altProjectsDir != "" {
		jsonlFiles = append(jsonlFiles, collectJSONLFiles(altProjectsDir)...)
	}
	jsonlFiles = lo.Uniq(lo.Compact(jsonlFiles))
	sort.Strings(jsonlFiles)

	if len(jsonlFiles) == 0 {
		return fmt.Errorf("no JSONL conversation files found")
	}

	snap.Raw["jsonl_files_found"] = fmt.Sprintf("%d", len(jsonlFiles))

	now := time.Now()
	today := now.Format("2006-01-02")
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekStart := now.Add(-7 * 24 * time.Hour)

	var (
		todayCostUSD      float64
		todayInputTokens  int
		todayOutputTokens int
		todayCacheRead    int
		todayCacheCreate  int
		todayMessages     int
		todayModels       = make(map[string]bool)

		weeklyCostUSD      float64
		weeklyInputTokens  int
		weeklyOutputTokens int
		weeklyMessages     int

		currentBlockStart time.Time
		currentBlockEnd   time.Time
		blockCostUSD      float64
		blockInputTokens  int
		blockOutputTokens int
		blockCacheRead    int
		blockCacheCreate  int
		blockMessages     int
		blockModels       = make(map[string]bool)
		inCurrentBlock    bool

		allTimeCostUSD float64
		allTimeEntries int
	)

	blockStartCandidates := []time.Time{}

	var allUsages []conversationRecord
	modelTotals := make(map[string]*modelUsageTotals)
	clientTotals := make(map[string]*modelUsageTotals)
	projectTotals := make(map[string]*modelUsageTotals)
	agentTotals := make(map[string]*modelUsageTotals)
	serviceTierTotals := make(map[string]float64)
	inferenceGeoTotals := make(map[string]float64)
	toolUsageCounts := make(map[string]int)
	languageUsageCounts := make(map[string]int)
	changedFiles := make(map[string]bool)
	seenCommitCommands := make(map[string]bool)
	clientSessions := make(map[string]map[string]bool)
	projectSessions := make(map[string]map[string]bool)
	agentSessions := make(map[string]map[string]bool)
	seenUsageKeys := make(map[string]bool)
	seenToolKeys := make(map[string]bool)
	dailyClientTokens := make(map[string]map[string]float64)
	dailyTokenTotals := make(map[string]int)
	dailyMessages := make(map[string]int)
	dailyCost := make(map[string]float64)
	dailyModelTokens := make(map[string]map[string]int)
	todaySessions := make(map[string]bool)
	weeklySessions := make(map[string]bool)
	var (
		todayCacheCreate5m   int
		todayCacheCreate1h   int
		todayReasoning       int
		todayToolCalls       int
		todayWebSearch       int
		todayWebFetch        int
		weeklyCacheRead      int
		weeklyCacheCreate    int
		weeklyCacheCreate5m  int
		weeklyCacheCreate1h  int
		weeklyReasoning      int
		weeklyToolCalls      int
		weeklyWebSearch      int
		weeklyWebFetch       int
		allTimeInputTokens   int
		allTimeOutputTokens  int
		allTimeCacheRead     int
		allTimeCacheCreate   int
		allTimeCacheCreate5m int
		allTimeCacheCreate1h int
		allTimeReasoning     int
		allTimeToolCalls     int
		allTimeWebSearch     int
		allTimeWebFetch      int
		allTimeLinesAdded    int
		allTimeLinesRemoved  int
		allTimeCommitCount   int
	)

	ensureTotals := func(m map[string]*modelUsageTotals, key string) *modelUsageTotals {
		if _, ok := m[key]; !ok {
			m[key] = &modelUsageTotals{}
		}
		return m[key]
	}
	ensureSessionSet := func(m map[string]map[string]bool, key string) map[string]bool {
		if _, ok := m[key]; !ok {
			m[key] = make(map[string]bool)
		}
		return m[key]
	}
	normalizeAgent := func(path string) string {
		if strings.Contains(path, string(filepath.Separator)+"subagents"+string(filepath.Separator)) {
			return "subagents"
		}
		return "main"
	}
	normalizeProject := func(cwd, sourcePath string) string {
		if cwd != "" {
			base := filepath.Base(cwd)
			if base != "" && base != "." && base != string(filepath.Separator) {
				return sanitizeModelName(base)
			}
			return sanitizeModelName(cwd)
		}
		dir := filepath.Base(filepath.Dir(sourcePath))
		if dir == "" || dir == "." {
			return "unknown"
		}
		return sanitizeModelName(dir)
	}
	for _, fpath := range jsonlFiles {
		allUsages = append(allUsages, parseConversationRecords(fpath)...)
	}

	sort.Slice(allUsages, func(i, j int) bool {
		return allUsages[i].timestamp.Before(allUsages[j].timestamp)
	})

	seenForBlock := make(map[string]bool)
	for _, u := range allUsages {
		if u.usage == nil {
			continue
		}
		key := conversationUsageDedupKey(u)
		if key != "" {
			if seenForBlock[key] {
				continue
			}
			seenForBlock[key] = true
		}
		if currentBlockEnd.IsZero() || u.timestamp.After(currentBlockEnd) {
			currentBlockStart = floorToHour(u.timestamp)
			currentBlockEnd = currentBlockStart.Add(billingBlockDuration)
			blockStartCandidates = append(blockStartCandidates, currentBlockStart)
		}
	}

	inCurrentBlock = false
	if !currentBlockEnd.IsZero() && now.Before(currentBlockEnd) && (now.Equal(currentBlockStart) || now.After(currentBlockStart)) {
		inCurrentBlock = true
	}

	for _, u := range allUsages {
		for idx, item := range u.content {
			if item.Type != "tool_use" {
				continue
			}
			toolKey := conversationToolDedupKey(u, idx, item)
			if seenToolKeys[toolKey] {
				continue
			}
			seenToolKeys[toolKey] = true
			toolName := strings.ToLower(strings.TrimSpace(item.Name))
			if toolName == "" {
				toolName = "unknown"
			}
			toolUsageCounts[toolName]++
			allTimeToolCalls++

			pathCandidates := extractToolPathCandidates(item.Input)
			for _, candidate := range pathCandidates {
				if lang := inferLanguageFromPath(candidate); lang != "" {
					languageUsageCounts[lang]++
				}
				if isMutatingTool(toolName) {
					changedFiles[candidate] = true
				}
			}
			if isMutatingTool(toolName) {
				added, removed := estimateToolLineDelta(toolName, item.Input)
				allTimeLinesAdded += added
				allTimeLinesRemoved += removed
			}
			if cmd := extractToolCommand(item.Input); cmd != "" && strings.Contains(strings.ToLower(cmd), "git commit") {
				if !seenCommitCommands[cmd] {
					seenCommitCommands[cmd] = true
					allTimeCommitCount++
				}
			}

			if u.timestamp.After(todayStart) || u.timestamp.Equal(todayStart) {
				todayToolCalls++
			}
			if u.timestamp.After(weekStart) || u.timestamp.Equal(weekStart) {
				weeklyToolCalls++
			}
		}

		if u.usage == nil {
			continue
		}
		usageKey := conversationUsageDedupKey(u)
		if usageKey != "" && seenUsageKeys[usageKey] {
			continue
		}
		if usageKey != "" {
			seenUsageKeys[usageKey] = true
		}

		modelID := sanitizeModelName(u.model)
		modelTotalsEntry := ensureTotals(modelTotals, modelID)
		projectID := normalizeProject(u.cwd, u.sourcePath)
		clientID := projectID
		clientTotalsEntry := ensureTotals(clientTotals, clientID)
		projectTotalsEntry := ensureTotals(projectTotals, projectID)
		agentID := normalizeAgent(u.sourcePath)
		agentTotalsEntry := ensureTotals(agentTotals, agentID)

		if u.sessionID != "" {
			ensureSessionSet(clientSessions, clientID)[u.sessionID] = true
			ensureSessionSet(projectSessions, projectID)[u.sessionID] = true
			ensureSessionSet(agentSessions, agentID)[u.sessionID] = true
			if u.timestamp.After(todayStart) || u.timestamp.Equal(todayStart) {
				todaySessions[u.sessionID] = true
			}
			if u.timestamp.After(weekStart) || u.timestamp.Equal(weekStart) {
				weeklySessions[u.sessionID] = true
			}
		}

		cost := estimateCost(u.model, u.usage)
		allTimeCostUSD += cost
		allTimeEntries++
		modelTotalsEntry.input += float64(u.usage.InputTokens)
		modelTotalsEntry.output += float64(u.usage.OutputTokens)
		modelTotalsEntry.cached += float64(u.usage.CacheReadInputTokens)
		modelTotalsEntry.cacheCreate += float64(u.usage.CacheCreationInputTokens)
		modelTotalsEntry.reasoning += float64(u.usage.ReasoningTokens)
		modelTotalsEntry.cost += cost
		if u.usage.CacheCreation != nil {
			modelTotalsEntry.cache5m += float64(u.usage.CacheCreation.Ephemeral5mInputTokens)
			modelTotalsEntry.cache1h += float64(u.usage.CacheCreation.Ephemeral1hInputTokens)
			allTimeCacheCreate5m += u.usage.CacheCreation.Ephemeral5mInputTokens
			allTimeCacheCreate1h += u.usage.CacheCreation.Ephemeral1hInputTokens
		}
		if u.usage.ServerToolUse != nil {
			modelTotalsEntry.webSearch += float64(u.usage.ServerToolUse.WebSearchRequests)
			modelTotalsEntry.webFetch += float64(u.usage.ServerToolUse.WebFetchRequests)
		}

		tokenVolume := float64(u.usage.InputTokens + u.usage.OutputTokens + u.usage.CacheReadInputTokens + u.usage.CacheCreationInputTokens + u.usage.ReasoningTokens)
		clientTotalsEntry.input += float64(u.usage.InputTokens)
		clientTotalsEntry.output += float64(u.usage.OutputTokens)
		clientTotalsEntry.cached += float64(u.usage.CacheReadInputTokens)
		clientTotalsEntry.cacheCreate += float64(u.usage.CacheCreationInputTokens)
		clientTotalsEntry.reasoning += float64(u.usage.ReasoningTokens)
		clientTotalsEntry.cost += cost
		clientTotalsEntry.sessions = float64(len(clientSessions[clientID]))

		projectTotalsEntry.input += float64(u.usage.InputTokens)
		projectTotalsEntry.output += float64(u.usage.OutputTokens)
		projectTotalsEntry.cached += float64(u.usage.CacheReadInputTokens)
		projectTotalsEntry.cacheCreate += float64(u.usage.CacheCreationInputTokens)
		projectTotalsEntry.reasoning += float64(u.usage.ReasoningTokens)
		projectTotalsEntry.cost += cost
		projectTotalsEntry.sessions = float64(len(projectSessions[projectID]))

		agentTotalsEntry.input += float64(u.usage.InputTokens)
		agentTotalsEntry.output += float64(u.usage.OutputTokens)
		agentTotalsEntry.cached += float64(u.usage.CacheReadInputTokens)
		agentTotalsEntry.cacheCreate += float64(u.usage.CacheCreationInputTokens)
		agentTotalsEntry.reasoning += float64(u.usage.ReasoningTokens)
		agentTotalsEntry.cost += cost
		agentTotalsEntry.sessions = float64(len(agentSessions[agentID]))

		allTimeInputTokens += u.usage.InputTokens
		allTimeOutputTokens += u.usage.OutputTokens
		allTimeCacheRead += u.usage.CacheReadInputTokens
		allTimeCacheCreate += u.usage.CacheCreationInputTokens
		allTimeReasoning += u.usage.ReasoningTokens
		if u.usage.ServerToolUse != nil {
			allTimeWebSearch += u.usage.ServerToolUse.WebSearchRequests
			allTimeWebFetch += u.usage.ServerToolUse.WebFetchRequests
		}

		day := u.timestamp.Format("2006-01-02")
		dailyTokenTotals[day] += u.usage.InputTokens + u.usage.OutputTokens
		dailyMessages[day]++
		dailyCost[day] += cost
		if dailyModelTokens[day] == nil {
			dailyModelTokens[day] = make(map[string]int)
		}
		dailyModelTokens[day][u.model] += u.usage.InputTokens + u.usage.OutputTokens
		if dailyClientTokens[day] == nil {
			dailyClientTokens[day] = make(map[string]float64)
		}
		dailyClientTokens[day][clientID] += tokenVolume

		if tier := strings.ToLower(strings.TrimSpace(u.usage.ServiceTier)); tier != "" {
			serviceTierTotals[tier] += tokenVolume
		}
		if geo := strings.ToLower(strings.TrimSpace(u.usage.InferenceGeo)); geo != "" {
			inferenceGeoTotals[geo] += tokenVolume
		}

		if u.timestamp.After(todayStart) || u.timestamp.Equal(todayStart) {
			todayCostUSD += cost
			todayInputTokens += u.usage.InputTokens
			todayOutputTokens += u.usage.OutputTokens
			todayCacheRead += u.usage.CacheReadInputTokens
			todayCacheCreate += u.usage.CacheCreationInputTokens
			todayReasoning += u.usage.ReasoningTokens
			if u.usage.CacheCreation != nil {
				todayCacheCreate5m += u.usage.CacheCreation.Ephemeral5mInputTokens
				todayCacheCreate1h += u.usage.CacheCreation.Ephemeral1hInputTokens
			}
			if u.usage.ServerToolUse != nil {
				todayWebSearch += u.usage.ServerToolUse.WebSearchRequests
				todayWebFetch += u.usage.ServerToolUse.WebFetchRequests
			}
			todayMessages++
			todayModels[modelID] = true
		}

		if u.timestamp.After(weekStart) || u.timestamp.Equal(weekStart) {
			weeklyCostUSD += cost
			weeklyInputTokens += u.usage.InputTokens
			weeklyOutputTokens += u.usage.OutputTokens
			weeklyCacheRead += u.usage.CacheReadInputTokens
			weeklyCacheCreate += u.usage.CacheCreationInputTokens
			weeklyReasoning += u.usage.ReasoningTokens
			if u.usage.CacheCreation != nil {
				weeklyCacheCreate5m += u.usage.CacheCreation.Ephemeral5mInputTokens
				weeklyCacheCreate1h += u.usage.CacheCreation.Ephemeral1hInputTokens
			}
			if u.usage.ServerToolUse != nil {
				weeklyWebSearch += u.usage.ServerToolUse.WebSearchRequests
				weeklyWebFetch += u.usage.ServerToolUse.WebFetchRequests
			}
			weeklyMessages++
		}

		if inCurrentBlock && (u.timestamp.After(currentBlockStart) || u.timestamp.Equal(currentBlockStart)) && u.timestamp.Before(currentBlockEnd) {
			blockCostUSD += cost
			blockInputTokens += u.usage.InputTokens
			blockOutputTokens += u.usage.OutputTokens
			blockCacheRead += u.usage.CacheReadInputTokens
			blockCacheCreate += u.usage.CacheCreationInputTokens
			blockMessages++
			blockModels[modelID] = true
		}
	}

	for model, totals := range modelTotals {
		modelPrefix := "model_" + model
		setMetricMax(snap, modelPrefix+"_input_tokens", totals.input, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_output_tokens", totals.output, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cached_tokens", totals.cached, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cache_creation_tokens", totals.cacheCreate, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cache_creation_5m_tokens", totals.cache5m, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cache_creation_1h_tokens", totals.cache1h, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_reasoning_tokens", totals.reasoning, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_web_search_requests", totals.webSearch, "requests", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_web_fetch_requests", totals.webFetch, "requests", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cost_usd", totals.cost, "USD", "all-time estimate")
	}

	for client, totals := range clientTotals {
		key := "client_" + client
		setMetricMax(snap, key+"_input_tokens", totals.input, "tokens", "all-time")
		setMetricMax(snap, key+"_output_tokens", totals.output, "tokens", "all-time")
		setMetricMax(snap, key+"_cached_tokens", totals.cached, "tokens", "all-time")
		setMetricMax(snap, key+"_reasoning_tokens", totals.reasoning, "tokens", "all-time")
		setMetricMax(snap, key+"_total_tokens", totals.input+totals.output+totals.cached+totals.cacheCreate+totals.reasoning, "tokens", "all-time")
		setMetricMax(snap, key+"_sessions", totals.sessions, "sessions", "all-time")
	}

	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	dates := core.SortedStringKeys(dailyTokenTotals)

	if len(snap.DailySeries["messages"]) == 0 && len(dates) > 0 {
		for _, d := range dates {
			snap.DailySeries["messages"] = append(snap.DailySeries["messages"], core.TimePoint{Date: d, Value: float64(dailyMessages[d])})
			snap.DailySeries["tokens_total"] = append(snap.DailySeries["tokens_total"], core.TimePoint{Date: d, Value: float64(dailyTokenTotals[d])})
			snap.DailySeries["cost"] = append(snap.DailySeries["cost"], core.TimePoint{Date: d, Value: dailyCost[d]})
		}

		allModels := make(map[string]int64)
		for _, dm := range dailyModelTokens {
			for model, tokens := range dm {
				allModels[model] += int64(tokens)
			}
		}
		type mVol struct {
			name  string
			total int64
		}
		var mv []mVol
		for m, t := range allModels {
			mv = append(mv, mVol{m, t})
		}
		sort.Slice(mv, func(i, j int) bool { return mv[i].total > mv[j].total })
		limit := 5
		if len(mv) < limit {
			limit = len(mv)
		}
		for i := 0; i < limit; i++ {
			model := mv[i].name
			key := fmt.Sprintf("tokens_%s", sanitizeModelName(model))
			for _, d := range dates {
				tokens := dailyModelTokens[d][model]
				snap.DailySeries[key] = append(snap.DailySeries[key],
					core.TimePoint{Date: d, Value: float64(tokens)})
			}
		}
	}

	if len(dates) > 0 {
		clientNames := make(map[string]bool)
		for _, byClient := range dailyClientTokens {
			for client := range byClient {
				clientNames[client] = true
			}
		}
		for client := range clientNames {
			key := "tokens_client_" + client
			for _, d := range dates {
				snap.DailySeries[key] = append(snap.DailySeries[key], core.TimePoint{
					Date:  d,
					Value: dailyClientTokens[d][client],
				})
			}
		}
	}

	if todayCostUSD > 0 {
		snap.Metrics["today_api_cost"] = core.Metric{
			Used:   core.Float64Ptr(todayCostUSD),
			Unit:   "USD",
			Window: "since midnight",
		}
	}
	if todayInputTokens > 0 {
		in := float64(todayInputTokens)
		snap.Metrics["today_input_tokens"] = core.Metric{
			Used:   &in,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayOutputTokens > 0 {
		out := float64(todayOutputTokens)
		snap.Metrics["today_output_tokens"] = core.Metric{
			Used:   &out,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayCacheRead > 0 {
		cacheRead := float64(todayCacheRead)
		snap.Metrics["today_cache_read_tokens"] = core.Metric{
			Used:   &cacheRead,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayCacheCreate > 0 {
		cacheCreate := float64(todayCacheCreate)
		snap.Metrics["today_cache_create_tokens"] = core.Metric{
			Used:   &cacheCreate,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayMessages > 0 {
		msgs := float64(todayMessages)
		setMetricMax(snap, "messages_today", msgs, "messages", "since midnight")
	}
	if len(todaySessions) > 0 {
		setMetricMax(snap, "sessions_today", float64(len(todaySessions)), "sessions", "since midnight")
	}
	if todayToolCalls > 0 {
		setMetricMax(snap, "tool_calls_today", float64(todayToolCalls), "calls", "since midnight")
	}
	if todayReasoning > 0 {
		v := float64(todayReasoning)
		snap.Metrics["today_reasoning_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayCacheCreate5m > 0 {
		v := float64(todayCacheCreate5m)
		snap.Metrics["today_cache_create_5m_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayCacheCreate1h > 0 {
		v := float64(todayCacheCreate1h)
		snap.Metrics["today_cache_create_1h_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayWebSearch > 0 {
		v := float64(todayWebSearch)
		snap.Metrics["today_web_search_requests"] = core.Metric{
			Used:   &v,
			Unit:   "requests",
			Window: "since midnight",
		}
	}
	if todayWebFetch > 0 {
		v := float64(todayWebFetch)
		snap.Metrics["today_web_fetch_requests"] = core.Metric{
			Used:   &v,
			Unit:   "requests",
			Window: "since midnight",
		}
	}

	if weeklyCostUSD > 0 {
		snap.Metrics["7d_api_cost"] = core.Metric{
			Used:   core.Float64Ptr(weeklyCostUSD),
			Unit:   "USD",
			Window: "rolling 7 days",
		}
	}
	if weeklyMessages > 0 {
		wm := float64(weeklyMessages)
		snap.Metrics["7d_messages"] = core.Metric{
			Used:   &wm,
			Unit:   "messages",
			Window: "rolling 7 days",
		}
		wIn := float64(weeklyInputTokens)
		snap.Metrics["7d_input_tokens"] = core.Metric{
			Used:   &wIn,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
		wOut := float64(weeklyOutputTokens)
		snap.Metrics["7d_output_tokens"] = core.Metric{
			Used:   &wOut,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyCacheRead > 0 {
		v := float64(weeklyCacheRead)
		snap.Metrics["7d_cache_read_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyCacheCreate > 0 {
		v := float64(weeklyCacheCreate)
		snap.Metrics["7d_cache_create_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyCacheCreate5m > 0 {
		v := float64(weeklyCacheCreate5m)
		snap.Metrics["7d_cache_create_5m_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyCacheCreate1h > 0 {
		v := float64(weeklyCacheCreate1h)
		snap.Metrics["7d_cache_create_1h_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyReasoning > 0 {
		v := float64(weeklyReasoning)
		snap.Metrics["7d_reasoning_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyToolCalls > 0 {
		setMetricMax(snap, "7d_tool_calls", float64(weeklyToolCalls), "calls", "rolling 7 days")
	}
	if weeklyWebSearch > 0 {
		v := float64(weeklyWebSearch)
		snap.Metrics["7d_web_search_requests"] = core.Metric{
			Used:   &v,
			Unit:   "requests",
			Window: "rolling 7 days",
		}
	}
	if weeklyWebFetch > 0 {
		v := float64(weeklyWebFetch)
		snap.Metrics["7d_web_fetch_requests"] = core.Metric{
			Used:   &v,
			Unit:   "requests",
			Window: "rolling 7 days",
		}
	}
	if len(weeklySessions) > 0 {
		setMetricMax(snap, "7d_sessions", float64(len(weeklySessions)), "sessions", "rolling 7 days")
	}

	if todayMessages > 0 {
		snap.Raw["jsonl_today_date"] = today
		snap.Raw["jsonl_today_messages"] = fmt.Sprintf("%d", todayMessages)
		snap.Raw["jsonl_today_input_tokens"] = fmt.Sprintf("%d", todayInputTokens)
		snap.Raw["jsonl_today_output_tokens"] = fmt.Sprintf("%d", todayOutputTokens)
		snap.Raw["jsonl_today_cache_read_tokens"] = fmt.Sprintf("%d", todayCacheRead)
		snap.Raw["jsonl_today_cache_create_tokens"] = fmt.Sprintf("%d", todayCacheCreate)
		snap.Raw["jsonl_today_reasoning_tokens"] = fmt.Sprintf("%d", todayReasoning)
		snap.Raw["jsonl_today_web_search_requests"] = fmt.Sprintf("%d", todayWebSearch)
		snap.Raw["jsonl_today_web_fetch_requests"] = fmt.Sprintf("%d", todayWebFetch)

		models := core.SortedStringKeys(todayModels)
		snap.Raw["jsonl_today_models"] = strings.Join(models, ", ")
	}

	if inCurrentBlock {
		snap.Metrics["5h_block_cost"] = core.Metric{
			Used:   core.Float64Ptr(blockCostUSD),
			Unit:   "USD",
			Window: fmt.Sprintf("%s – %s", currentBlockStart.Format("15:04"), currentBlockEnd.Format("15:04")),
		}

		blockIn := float64(blockInputTokens)
		snap.Metrics["5h_block_input"] = core.Metric{
			Used:   &blockIn,
			Unit:   "tokens",
			Window: "current 5h block",
		}

		blockOut := float64(blockOutputTokens)
		snap.Metrics["5h_block_output"] = core.Metric{
			Used:   &blockOut,
			Unit:   "tokens",
			Window: "current 5h block",
		}

		blockMsgs := float64(blockMessages)
		snap.Metrics["5h_block_msgs"] = core.Metric{
			Used:   &blockMsgs,
			Unit:   "messages",
			Window: "current 5h block",
		}
		if blockCacheRead > 0 {
			setMetricMax(snap, "5h_block_cache_read_tokens", float64(blockCacheRead), "tokens", "current 5h block")
		}
		if blockCacheCreate > 0 {
			setMetricMax(snap, "5h_block_cache_create_tokens", float64(blockCacheCreate), "tokens", "current 5h block")
		}

		remaining := currentBlockEnd.Sub(now)
		if remaining > 0 {
			snap.Resets["billing_block"] = currentBlockEnd
			snap.Raw["block_time_remaining"] = fmt.Sprintf("%s", remaining.Round(time.Minute))

			elapsed := now.Sub(currentBlockStart)
			progress := math.Min(elapsed.Seconds()/billingBlockDuration.Seconds()*100, 100)
			snap.Raw["block_progress_pct"] = fmt.Sprintf("%.0f", progress)
		}

		snap.Raw["block_start"] = currentBlockStart.Format(time.RFC3339)
		snap.Raw["block_end"] = currentBlockEnd.Format(time.RFC3339)

		blockModelList := core.SortedStringKeys(blockModels)
		snap.Raw["block_models"] = strings.Join(blockModelList, ", ")

		elapsed := now.Sub(currentBlockStart)
		if elapsed > time.Minute && blockCostUSD > 0 {
			burnRate := blockCostUSD / elapsed.Hours()
			snap.Metrics["burn_rate"] = core.Metric{
				Used:   core.Float64Ptr(burnRate),
				Unit:   "USD/h",
				Window: "current 5h block",
			}
			snap.Raw["burn_rate"] = fmt.Sprintf("$%.2f/hour", burnRate)
		}
	}

	if allTimeCostUSD > 0 {
		snap.Metrics["all_time_api_cost"] = core.Metric{
			Used:   core.Float64Ptr(allTimeCostUSD),
			Unit:   "USD",
			Window: "all-time estimate",
		}
	}
	if allTimeInputTokens > 0 {
		setMetricMax(snap, "all_time_input_tokens", float64(allTimeInputTokens), "tokens", "all-time estimate")
	}
	if allTimeOutputTokens > 0 {
		setMetricMax(snap, "all_time_output_tokens", float64(allTimeOutputTokens), "tokens", "all-time estimate")
	}
	if allTimeCacheRead > 0 {
		setMetricMax(snap, "all_time_cache_read_tokens", float64(allTimeCacheRead), "tokens", "all-time estimate")
	}
	if allTimeCacheCreate > 0 {
		setMetricMax(snap, "all_time_cache_create_tokens", float64(allTimeCacheCreate), "tokens", "all-time estimate")
	}
	if allTimeCacheCreate5m > 0 {
		setMetricMax(snap, "all_time_cache_create_5m_tokens", float64(allTimeCacheCreate5m), "tokens", "all-time estimate")
	}
	if allTimeCacheCreate1h > 0 {
		setMetricMax(snap, "all_time_cache_create_1h_tokens", float64(allTimeCacheCreate1h), "tokens", "all-time estimate")
	}
	if allTimeReasoning > 0 {
		setMetricMax(snap, "all_time_reasoning_tokens", float64(allTimeReasoning), "tokens", "all-time estimate")
	}
	if allTimeToolCalls > 0 {
		setMetricMax(snap, "all_time_tool_calls", float64(allTimeToolCalls), "calls", "all-time estimate")
		setMetricMax(snap, "tool_calls_total", float64(allTimeToolCalls), "calls", "all-time estimate")
		setMetricMax(snap, "tool_completed", float64(allTimeToolCalls), "calls", "all-time estimate")
		setMetricMax(snap, "tool_success_rate", 100.0, "%", "all-time estimate")
	}
	if len(seenUsageKeys) > 0 {
		setMetricMax(snap, "total_prompts", float64(len(seenUsageKeys)), "prompts", "all-time estimate")
	}
	if len(changedFiles) > 0 {
		setMetricMax(snap, "composer_files_changed", float64(len(changedFiles)), "files", "all-time estimate")
	}
	if allTimeLinesAdded > 0 {
		setMetricMax(snap, "composer_lines_added", float64(allTimeLinesAdded), "lines", "all-time estimate")
	}
	if allTimeLinesRemoved > 0 {
		setMetricMax(snap, "composer_lines_removed", float64(allTimeLinesRemoved), "lines", "all-time estimate")
	}
	if allTimeCommitCount > 0 {
		setMetricMax(snap, "scored_commits", float64(allTimeCommitCount), "commits", "all-time estimate")
	}
	if allTimeLinesAdded > 0 || allTimeLinesRemoved > 0 {
		hundred := 100.0
		zero := 0.0
		snap.Metrics["ai_code_percentage"] = core.Metric{
			Used:      &hundred,
			Remaining: &zero,
			Limit:     &hundred,
			Unit:      "%",
			Window:    "all-time estimate",
		}
	}
	for lang, count := range languageUsageCounts {
		if count <= 0 {
			continue
		}
		setMetricMax(snap, "lang_"+sanitizeModelName(lang), float64(count), "requests", "all-time estimate")
	}
	for toolName, count := range toolUsageCounts {
		if count <= 0 {
			continue
		}
		setMetricMax(snap, "tool_"+sanitizeModelName(toolName), float64(count), "calls", "all-time estimate")
	}
	if allTimeWebSearch > 0 {
		setMetricMax(snap, "all_time_web_search_requests", float64(allTimeWebSearch), "requests", "all-time estimate")
	}
	if allTimeWebFetch > 0 {
		setMetricMax(snap, "all_time_web_fetch_requests", float64(allTimeWebFetch), "requests", "all-time estimate")
	}

	snap.Raw["tool_usage"] = summarizeCountMap(toolUsageCounts, 6)
	snap.Raw["language_usage"] = summarizeCountMap(languageUsageCounts, 8)
	snap.Raw["project_usage"] = summarizeTotalsMap(projectTotals, true, 6)
	snap.Raw["agent_usage"] = summarizeTotalsMap(agentTotals, false, 4)
	snap.Raw["service_tier_usage"] = summarizeFloatMap(serviceTierTotals, "tok", 4)
	snap.Raw["inference_geo_usage"] = summarizeFloatMap(inferenceGeoTotals, "tok", 4)
	if allTimeCacheRead > 0 || allTimeCacheCreate > 0 {
		snap.Raw["cache_usage"] = fmt.Sprintf("read %s · create %s (1h %s, 5m %s)",
			shortTokenCount(float64(allTimeCacheRead)),
			shortTokenCount(float64(allTimeCacheCreate)),
			shortTokenCount(float64(allTimeCacheCreate1h)),
			shortTokenCount(float64(allTimeCacheCreate5m)),
		)
	}
	snap.Raw["project_count"] = fmt.Sprintf("%d", len(projectTotals))
	snap.Raw["tool_count"] = fmt.Sprintf("%d", len(toolUsageCounts))

	snap.Raw["jsonl_total_entries"] = fmt.Sprintf("%d", allTimeEntries)
	snap.Raw["jsonl_total_blocks"] = fmt.Sprintf("%d", len(blockStartCandidates))
	snap.Raw["jsonl_unique_requests"] = fmt.Sprintf("%d", len(seenUsageKeys))
	buildModelUsageSummaryRaw(snap)

	return nil
}
