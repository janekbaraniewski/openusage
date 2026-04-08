package claude_code

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func (p *Provider) readConversationJSONL(projectsDir, altProjectsDir string, snap *core.UsageSnapshot) error {
	// Collect files with stat info for cache-aware parsing.
	fileInfos := collectJSONLFilesWithStat(projectsDir)
	if altProjectsDir != "" {
		for k, v := range collectJSONLFilesWithStat(altProjectsDir) {
			fileInfos[k] = v
		}
	}

	jsonlFiles := make([]string, 0, len(fileInfos))
	for path := range fileInfos {
		jsonlFiles = append(jsonlFiles, path)
	}
	sort.Strings(jsonlFiles)

	if len(jsonlFiles) == 0 {
		return fmt.Errorf("no JSONL conversation files found")
	}

	snap.Raw["jsonl_files_found"] = fmt.Sprintf("%d", len(jsonlFiles))

	now := time.Now()
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
		allUsages = append(allUsages, p.cachedParseConversationRecords(fpath, fileInfos[fpath])...)
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

	applyConversationUsageProjection(snap, conversationUsageProjection{
		now:                  now,
		inCurrentBlock:       inCurrentBlock,
		currentBlockStart:    currentBlockStart,
		currentBlockEnd:      currentBlockEnd,
		blockCostUSD:         blockCostUSD,
		blockInputTokens:     blockInputTokens,
		blockOutputTokens:    blockOutputTokens,
		blockCacheRead:       blockCacheRead,
		blockCacheCreate:     blockCacheCreate,
		blockMessages:        blockMessages,
		blockModels:          blockModels,
		blockStartCandidates: blockStartCandidates,
		todayCostUSD:         todayCostUSD,
		todayInputTokens:     todayInputTokens,
		todayOutputTokens:    todayOutputTokens,
		todayCacheRead:       todayCacheRead,
		todayCacheCreate:     todayCacheCreate,
		todayMessages:        todayMessages,
		todayModels:          todayModels,
		todaySessions:        todaySessions,
		todayCacheCreate5m:   todayCacheCreate5m,
		todayCacheCreate1h:   todayCacheCreate1h,
		todayReasoning:       todayReasoning,
		todayToolCalls:       todayToolCalls,
		todayWebSearch:       todayWebSearch,
		todayWebFetch:        todayWebFetch,
		weeklyCostUSD:        weeklyCostUSD,
		weeklyInputTokens:    weeklyInputTokens,
		weeklyOutputTokens:   weeklyOutputTokens,
		weeklyMessages:       weeklyMessages,
		weeklySessions:       weeklySessions,
		weeklyCacheRead:      weeklyCacheRead,
		weeklyCacheCreate:    weeklyCacheCreate,
		weeklyCacheCreate5m:  weeklyCacheCreate5m,
		weeklyCacheCreate1h:  weeklyCacheCreate1h,
		weeklyReasoning:      weeklyReasoning,
		weeklyToolCalls:      weeklyToolCalls,
		weeklyWebSearch:      weeklyWebSearch,
		weeklyWebFetch:       weeklyWebFetch,
		allTimeCostUSD:       allTimeCostUSD,
		allTimeEntries:       allTimeEntries,
		allTimeInputTokens:   allTimeInputTokens,
		allTimeOutputTokens:  allTimeOutputTokens,
		allTimeCacheRead:     allTimeCacheRead,
		allTimeCacheCreate:   allTimeCacheCreate,
		allTimeCacheCreate5m: allTimeCacheCreate5m,
		allTimeCacheCreate1h: allTimeCacheCreate1h,
		allTimeReasoning:     allTimeReasoning,
		allTimeToolCalls:     allTimeToolCalls,
		allTimeWebSearch:     allTimeWebSearch,
		allTimeWebFetch:      allTimeWebFetch,
		allTimeLinesAdded:    allTimeLinesAdded,
		allTimeLinesRemoved:  allTimeLinesRemoved,
		allTimeCommitCount:   allTimeCommitCount,
		modelTotals:          modelTotals,
		clientTotals:         clientTotals,
		projectTotals:        projectTotals,
		agentTotals:          agentTotals,
		serviceTierTotals:    serviceTierTotals,
		inferenceGeoTotals:   inferenceGeoTotals,
		toolUsageCounts:      toolUsageCounts,
		languageUsageCounts:  languageUsageCounts,
		changedFiles:         changedFiles,
		seenUsageKeys:        seenUsageKeys,
		dailyClientTokens:    dailyClientTokens,
		dailyTokenTotals:     dailyTokenTotals,
		dailyMessages:        dailyMessages,
		dailyCost:            dailyCost,
		dailyModelTokens:     dailyModelTokens,
	})
	return nil
}

// cachedParseConversationRecords returns cached records for a file if the mtime and size
// match, otherwise re-parses the file and updates the cache.
func (p *Provider) cachedParseConversationRecords(path string, info os.FileInfo) []conversationRecord {
	if info == nil {
		return parseConversationRecords(path)
	}

	p.jsonlCacheMu.Lock()
	defer p.jsonlCacheMu.Unlock()

	if p.jsonlCache == nil {
		p.jsonlCache = make(map[string]*jsonlCacheEntry)
	}

	if entry, ok := p.jsonlCache[path]; ok {
		if entry.modTime.Equal(info.ModTime()) && entry.size == info.Size() {
			return entry.records
		}
	}

	records := parseConversationRecords(path)
	p.jsonlCache[path] = &jsonlCacheEntry{
		modTime: info.ModTime(),
		size:    info.Size(),
		records: records,
	}
	return records
}
