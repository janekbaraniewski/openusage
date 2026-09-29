package claude_code

import (
	"container/heap"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// readConversationJSONL aggregates usage across every conversation JSONL file.
//
// Work is split so a poll where only the live session file grew does not
// re-aggregate the whole history:
//
//  1. Per file (cached on mtime+size): parsed records sorted by timestamp and
//     their precomputed dedup keys.
//  2. Per poll, only when some file changed: a k-way merge of the per-file
//     record streams in global timestamp order. It applies first-wins dedup
//     across files (recording which records lose) and walks the 5h billing
//     block chain. This visits each record once but only does map probes.
//  3. Per file, only when its records or lost set changed: the file's
//     contribution to the all-time aggregates. The per-file partials are then
//     reduced into the all-time totals.
//  4. Every poll: the today / 7d / current-block windows, scanning only
//     records newer than the widest window.
func (p *Provider) readConversationJSONL(projectsDir, altProjectsDir string, snap *core.UsageSnapshot) error {
	// Collect files with stat info for cache-aware parsing.
	fileInfos, err := collectJSONLFilesWithStatAcross(projectsDir, altProjectsDir)
	if err != nil {
		return err
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
	if p.nowFn != nil {
		now = p.nowFn()
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekStart := now.Add(-7 * 24 * time.Hour)

	p.jsonlCacheMu.Lock()
	defer p.jsonlCacheMu.Unlock()

	entries := make([]*jsonlCacheEntry, len(jsonlFiles))
	for i, fpath := range jsonlFiles {
		entries[i] = p.cachedConversationEntry(fpath, fileInfos[fpath])
	}
	agg := p.conversationAggregate(entries)

	inCurrentBlock := false
	if !agg.blockEnd.IsZero() && now.Before(agg.blockEnd) && (now.Equal(agg.blockStart) || now.After(agg.blockStart)) {
		inCurrentBlock = true
	}

	w := conversationWindows{
		todayStart:     todayStart,
		weekStart:      weekStart,
		inCurrentBlock: inCurrentBlock,
		blockStart:     agg.blockStart,
		blockEnd:       agg.blockEnd,
		todayModels:    make(map[string]bool),
		todaySessions:  make(map[string]bool),
		weeklySessions: make(map[string]bool),
		blockModels:    make(map[string]bool),
	}
	// The current block (when active) starts within the last 5h and today
	// within the last 24h, so the 7d window bounds every windowed sum.
	windowStart := weekStart
	if todayStart.Before(windowStart) {
		windowStart = todayStart
	}
	for _, e := range entries {
		w.addEntry(e, windowStart)
	}

	t := agg.total
	applyConversationUsageProjection(snap, conversationUsageProjection{
		now:                  now,
		inCurrentBlock:       inCurrentBlock,
		currentBlockStart:    agg.blockStart,
		currentBlockEnd:      agg.blockEnd,
		blockCostUSD:         w.blockCostUSD,
		blockInputTokens:     w.blockInputTokens,
		blockOutputTokens:    w.blockOutputTokens,
		blockCacheRead:       w.blockCacheRead,
		blockCacheCreate:     w.blockCacheCreate,
		blockMessages:        w.blockMessages,
		blockModels:          w.blockModels,
		blockStartCandidates: agg.blockStarts,
		todayCostUSD:         w.todayCostUSD,
		todayInputTokens:     w.todayInputTokens,
		todayOutputTokens:    w.todayOutputTokens,
		todayCacheRead:       w.todayCacheRead,
		todayCacheCreate:     w.todayCacheCreate,
		todayMessages:        w.todayMessages,
		todayModels:          w.todayModels,
		todaySessions:        w.todaySessions,
		todayCacheCreate5m:   w.todayCacheCreate5m,
		todayCacheCreate1h:   w.todayCacheCreate1h,
		todayReasoning:       w.todayReasoning,
		todayToolCalls:       w.todayToolCalls,
		todayWebSearch:       w.todayWebSearch,
		todayWebFetch:        w.todayWebFetch,
		weeklyCostUSD:        w.weeklyCostUSD,
		weeklyInputTokens:    w.weeklyInputTokens,
		weeklyOutputTokens:   w.weeklyOutputTokens,
		weeklyMessages:       w.weeklyMessages,
		weeklySessions:       w.weeklySessions,
		weeklyCacheRead:      w.weeklyCacheRead,
		weeklyCacheCreate:    w.weeklyCacheCreate,
		weeklyCacheCreate5m:  w.weeklyCacheCreate5m,
		weeklyCacheCreate1h:  w.weeklyCacheCreate1h,
		weeklyReasoning:      w.weeklyReasoning,
		weeklyToolCalls:      w.weeklyToolCalls,
		weeklyWebSearch:      w.weeklyWebSearch,
		weeklyWebFetch:       w.weeklyWebFetch,
		allTimeCostUSD:       t.costUSD,
		allTimeEntries:       t.entries,
		allTimeInputTokens:   t.input,
		allTimeOutputTokens:  t.output,
		allTimeCacheRead:     t.cacheRead,
		allTimeCacheCreate:   t.cacheCreate,
		allTimeCacheCreate5m: t.cacheCreate5m,
		allTimeCacheCreate1h: t.cacheCreate1h,
		allTimeReasoning:     t.reasoning,
		allTimeToolCalls:     t.toolCalls,
		allTimeWebSearch:     t.webSearch,
		allTimeWebFetch:      t.webFetch,
		allTimeLinesAdded:    t.linesAdded,
		allTimeLinesRemoved:  t.linesRemoved,
		allTimeCommitCount:   len(t.commitCommands),
		modelTotals:          t.modelTotals,
		clientTotals:         t.clientTotals,
		projectTotals:        t.projectTotals,
		agentTotals:          t.agentTotals,
		serviceTierTotals:    t.serviceTierTotals,
		inferenceGeoTotals:   t.inferenceGeoTotals,
		toolUsageCounts:      t.toolUsageCounts,
		languageUsageCounts:  t.languageUsageCounts,
		changedFiles:         t.changedFiles,
		seenUsageKeys:        agg.seenUsageKeys,
		dailyClientTokens:    t.dailyClientTokens,
		dailyTokenTotals:     t.dailyTokenTotals,
		dailyMessages:        t.dailyMessages,
		dailyCost:            t.dailyCost,
		dailyModelTokens:     t.dailyModelTokens,
	})
	return nil
}

// toolSlot addresses content item `item` of record `rec` within one file.
type toolSlot struct {
	rec, item int
}

// conversationAggCache is the merge result for one ordered list of file
// entries. An entry is replaced (new pointer) whenever its file changes, so
// an identical pointer list means nothing needs recomputing.
type conversationAggCache struct {
	entries  []*jsonlCacheEntry
	priceGen uint64

	total         *conversationAllTime
	seenUsageKeys map[string]bool
	blockStarts   []time.Time
	blockStart    time.Time
	blockEnd      time.Time
}

// conversationAggregate returns the cross-file merge and the reduced all-time
// totals for entries (in path order), reusing the previous result when no
// entry changed. Caller must hold jsonlCacheMu.
func (p *Provider) conversationAggregate(entries []*jsonlCacheEntry) *conversationAggCache {
	gen := priceGeneration()
	if c := p.convAgg; c != nil && c.priceGen == gen && slices.Equal(c.entries, entries) {
		return c
	}

	agg := &conversationAggCache{
		entries:       entries,
		priceGen:      gen,
		seenUsageKeys: make(map[string]bool),
		blockStarts:   []time.Time{},
	}
	lostUsage := make([][]int, len(entries))
	lostTools := make([][]toolSlot, len(entries))
	seenToolKeys := make(map[string]bool)

	// Global timestamp order; ties broken by file path order, then in-file
	// order. The first occurrence of a dedup key wins, as before.
	h := make(conversationMergeHeap, 0, len(entries))
	for i, e := range entries {
		if len(e.records) > 0 {
			h = append(h, conversationCursor{entries: entries, file: i})
		}
	}
	heap.Init(&h)
	for len(h) > 0 {
		c := &h[0]
		e := entries[c.file]
		i := c.pos
		for idx, key := range e.toolKeys[i] {
			if key == "" {
				continue
			}
			if seenToolKeys[key] {
				lostTools[c.file] = append(lostTools[c.file], toolSlot{rec: i, item: idx})
				continue
			}
			seenToolKeys[key] = true
		}
		if key := e.usageKeys[i]; key != "" {
			if agg.seenUsageKeys[key] {
				lostUsage[c.file] = append(lostUsage[c.file], i)
			} else {
				agg.seenUsageKeys[key] = true
				ts := e.records[i].timestamp
				if agg.blockEnd.IsZero() || ts.After(agg.blockEnd) {
					agg.blockStart = floorToHour(ts)
					agg.blockEnd = agg.blockStart.Add(billingBlockDuration)
					agg.blockStarts = append(agg.blockStarts, agg.blockStart)
				}
			}
		}
		c.pos++
		if c.pos < len(e.records) {
			heap.Fix(&h, 0)
		} else {
			heap.Pop(&h)
		}
	}

	agg.total = newConversationAllTime()
	for i, e := range entries {
		if e.allTime == nil || e.priceGen != gen ||
			!slices.Equal(e.lostUsage, lostUsage[i]) || !slices.Equal(e.lostTools, lostTools[i]) {
			e.buildAllTime(lostUsage[i], lostTools[i], gen)
		}
		agg.total.merge(e.allTime)
	}
	agg.total.finalizeSessions()

	p.convAgg = agg
	return agg
}

type conversationCursor struct {
	entries []*jsonlCacheEntry
	file    int
	pos     int
}

type conversationMergeHeap []conversationCursor

func (h conversationMergeHeap) Len() int { return len(h) }
func (h conversationMergeHeap) Less(i, j int) bool {
	ti := h[i].entries[h[i].file].records[h[i].pos].timestamp
	tj := h[j].entries[h[j].file].records[h[j].pos].timestamp
	if ti.Equal(tj) {
		return h[i].file < h[j].file
	}
	return ti.Before(tj)
}
func (h conversationMergeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *conversationMergeHeap) Push(x any)   { *h = append(*h, x.(conversationCursor)) }
func (h *conversationMergeHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// conversationAllTime holds the aggregates that do not depend on the clock,
// either for one file or reduced across all files.
type conversationAllTime struct {
	costUSD       float64
	entries       int
	input         int
	output        int
	cacheRead     int
	cacheCreate   int
	cacheCreate5m int
	cacheCreate1h int
	reasoning     int
	toolCalls     int
	webSearch     int
	webFetch      int
	linesAdded    int
	linesRemoved  int

	commitCommands      map[string]bool
	modelTotals         map[string]*modelUsageTotals
	clientTotals        map[string]*modelUsageTotals
	projectTotals       map[string]*modelUsageTotals
	agentTotals         map[string]*modelUsageTotals
	clientSessions      map[string]map[string]bool
	projectSessions     map[string]map[string]bool
	agentSessions       map[string]map[string]bool
	serviceTierTotals   map[string]float64
	inferenceGeoTotals  map[string]float64
	toolUsageCounts     map[string]int
	languageUsageCounts map[string]int
	changedFiles        map[string]bool
	dailyClientTokens   map[string]map[string]float64
	dailyTokenTotals    map[string]int
	dailyMessages       map[string]int
	dailyCost           map[string]float64
	dailyModelTokens    map[string]map[string]int
}

func newConversationAllTime() *conversationAllTime {
	return &conversationAllTime{
		commitCommands:      make(map[string]bool),
		modelTotals:         make(map[string]*modelUsageTotals),
		clientTotals:        make(map[string]*modelUsageTotals),
		projectTotals:       make(map[string]*modelUsageTotals),
		agentTotals:         make(map[string]*modelUsageTotals),
		clientSessions:      make(map[string]map[string]bool),
		projectSessions:     make(map[string]map[string]bool),
		agentSessions:       make(map[string]map[string]bool),
		serviceTierTotals:   make(map[string]float64),
		inferenceGeoTotals:  make(map[string]float64),
		toolUsageCounts:     make(map[string]int),
		languageUsageCounts: make(map[string]int),
		changedFiles:        make(map[string]bool),
		dailyClientTokens:   make(map[string]map[string]float64),
		dailyTokenTotals:    make(map[string]int),
		dailyMessages:       make(map[string]int),
		dailyCost:           make(map[string]float64),
		dailyModelTokens:    make(map[string]map[string]int),
	}
}

func ensureTotals(m map[string]*modelUsageTotals, key string) *modelUsageTotals {
	if _, ok := m[key]; !ok {
		m[key] = &modelUsageTotals{}
	}
	return m[key]
}

func ensureSessionSet(m map[string]map[string]bool, key string) map[string]bool {
	if _, ok := m[key]; !ok {
		m[key] = make(map[string]bool)
	}
	return m[key]
}

func normalizeConversationAgent(record conversationRecord) string {
	// Prefer the agent attribution resolved at parse time by the three-tier
	// detector. Empty values are coerced to the legacy "main"/"subagents"
	// labels so older fixtures continue to look the same.
	if label := strings.TrimSpace(record.agentID); label != "" {
		return label
	}
	if strings.Contains(record.sourcePath, string(filepath.Separator)+"subagents"+string(filepath.Separator)) {
		return "subagents"
	}
	return "main"
}

// addTool records one deduplicated tool_use item.
func (a *conversationAllTime) addTool(item jsonlContent) {
	toolName := strings.ToLower(strings.TrimSpace(item.Name))
	if toolName == "" {
		toolName = "unknown"
	}
	a.toolUsageCounts[toolName]++
	a.toolCalls++

	for _, candidate := range extractToolPathCandidates(item.Input) {
		if lang := inferLanguageFromPath(candidate); lang != "" {
			a.languageUsageCounts[lang]++
		}
		if isMutatingTool(toolName) {
			a.changedFiles[candidate] = true
		}
	}
	if isMutatingTool(toolName) {
		added, removed := estimateToolLineDelta(toolName, item.Input)
		a.linesAdded += added
		a.linesRemoved += removed
	}
	if cmd := extractToolCommand(item.Input); cmd != "" && strings.Contains(strings.ToLower(cmd), "git commit") {
		a.commitCommands[cmd] = true
	}
}

// addUsage records one deduplicated usage record.
func (a *conversationAllTime) addUsage(u conversationRecord, cost float64) {
	modelID := sanitizeModelName(u.model)
	projectID := conversationProjectLabel(u.cwd, u.sourcePath)
	clientID := projectID
	agentID := normalizeConversationAgent(u)

	if u.sessionID != "" {
		ensureSessionSet(a.clientSessions, clientID)[u.sessionID] = true
		ensureSessionSet(a.projectSessions, projectID)[u.sessionID] = true
		ensureSessionSet(a.agentSessions, agentID)[u.sessionID] = true
	}

	a.costUSD += cost
	a.entries++
	modelTotalsEntry := ensureTotals(a.modelTotals, modelID)
	modelTotalsEntry.input += float64(u.usage.InputTokens)
	modelTotalsEntry.output += float64(u.usage.OutputTokens)
	modelTotalsEntry.cached += float64(u.usage.CacheReadInputTokens)
	modelTotalsEntry.cacheCreate += float64(u.usage.CacheCreationInputTokens)
	modelTotalsEntry.reasoning += float64(u.usage.ReasoningTokens)
	modelTotalsEntry.cost += cost
	if u.usage.CacheCreation != nil {
		modelTotalsEntry.cache5m += float64(u.usage.CacheCreation.Ephemeral5mInputTokens)
		modelTotalsEntry.cache1h += float64(u.usage.CacheCreation.Ephemeral1hInputTokens)
		a.cacheCreate5m += u.usage.CacheCreation.Ephemeral5mInputTokens
		a.cacheCreate1h += u.usage.CacheCreation.Ephemeral1hInputTokens
	}
	if u.usage.ServerToolUse != nil {
		modelTotalsEntry.webSearch += float64(u.usage.ServerToolUse.WebSearchRequests)
		modelTotalsEntry.webFetch += float64(u.usage.ServerToolUse.WebFetchRequests)
	}

	tokenVolume := float64(u.usage.InputTokens + u.usage.OutputTokens + u.usage.CacheReadInputTokens + u.usage.CacheCreationInputTokens + u.usage.ReasoningTokens)
	for _, entry := range []*modelUsageTotals{
		ensureTotals(a.clientTotals, clientID),
		ensureTotals(a.projectTotals, projectID),
		ensureTotals(a.agentTotals, agentID),
	} {
		entry.input += float64(u.usage.InputTokens)
		entry.output += float64(u.usage.OutputTokens)
		entry.cached += float64(u.usage.CacheReadInputTokens)
		entry.cacheCreate += float64(u.usage.CacheCreationInputTokens)
		entry.reasoning += float64(u.usage.ReasoningTokens)
		entry.cost += cost
	}

	a.input += u.usage.InputTokens
	a.output += u.usage.OutputTokens
	a.cacheRead += u.usage.CacheReadInputTokens
	a.cacheCreate += u.usage.CacheCreationInputTokens
	a.reasoning += u.usage.ReasoningTokens
	if u.usage.ServerToolUse != nil {
		a.webSearch += u.usage.ServerToolUse.WebSearchRequests
		a.webFetch += u.usage.ServerToolUse.WebFetchRequests
	}

	day := u.timestamp.Format("2006-01-02")
	a.dailyTokenTotals[day] += u.usage.InputTokens + u.usage.OutputTokens
	a.dailyMessages[day]++
	a.dailyCost[day] += cost
	if a.dailyModelTokens[day] == nil {
		a.dailyModelTokens[day] = make(map[string]int)
	}
	a.dailyModelTokens[day][u.model] += u.usage.InputTokens + u.usage.OutputTokens
	if a.dailyClientTokens[day] == nil {
		a.dailyClientTokens[day] = make(map[string]float64)
	}
	a.dailyClientTokens[day][clientID] += tokenVolume

	if tier := strings.ToLower(strings.TrimSpace(u.usage.ServiceTier)); tier != "" {
		a.serviceTierTotals[tier] += tokenVolume
	}
	if geo := strings.ToLower(strings.TrimSpace(u.usage.InferenceGeo)); geo != "" {
		a.inferenceGeoTotals[geo] += tokenVolume
	}
}

// merge adds o into a without sharing any maps or pointers with o.
func (a *conversationAllTime) merge(o *conversationAllTime) {
	a.costUSD += o.costUSD
	a.entries += o.entries
	a.input += o.input
	a.output += o.output
	a.cacheRead += o.cacheRead
	a.cacheCreate += o.cacheCreate
	a.cacheCreate5m += o.cacheCreate5m
	a.cacheCreate1h += o.cacheCreate1h
	a.reasoning += o.reasoning
	a.toolCalls += o.toolCalls
	a.webSearch += o.webSearch
	a.webFetch += o.webFetch
	a.linesAdded += o.linesAdded
	a.linesRemoved += o.linesRemoved

	mergeTotals := func(dst, src map[string]*modelUsageTotals) {
		for k, s := range src {
			d := ensureTotals(dst, k)
			d.input += s.input
			d.output += s.output
			d.cached += s.cached
			d.cacheCreate += s.cacheCreate
			d.cache5m += s.cache5m
			d.cache1h += s.cache1h
			d.reasoning += s.reasoning
			d.cost += s.cost
			d.webSearch += s.webSearch
			d.webFetch += s.webFetch
		}
	}
	mergeTotals(a.modelTotals, o.modelTotals)
	mergeTotals(a.clientTotals, o.clientTotals)
	mergeTotals(a.projectTotals, o.projectTotals)
	mergeTotals(a.agentTotals, o.agentTotals)

	mergeSessions := func(dst, src map[string]map[string]bool) {
		for k, set := range src {
			d := ensureSessionSet(dst, k)
			for s := range set {
				d[s] = true
			}
		}
	}
	mergeSessions(a.clientSessions, o.clientSessions)
	mergeSessions(a.projectSessions, o.projectSessions)
	mergeSessions(a.agentSessions, o.agentSessions)

	for k := range o.commitCommands {
		a.commitCommands[k] = true
	}
	for k := range o.changedFiles {
		a.changedFiles[k] = true
	}
	for k, v := range o.serviceTierTotals {
		a.serviceTierTotals[k] += v
	}
	for k, v := range o.inferenceGeoTotals {
		a.inferenceGeoTotals[k] += v
	}
	for k, v := range o.toolUsageCounts {
		a.toolUsageCounts[k] += v
	}
	for k, v := range o.languageUsageCounts {
		a.languageUsageCounts[k] += v
	}
	for k, v := range o.dailyTokenTotals {
		a.dailyTokenTotals[k] += v
	}
	for k, v := range o.dailyMessages {
		a.dailyMessages[k] += v
	}
	for k, v := range o.dailyCost {
		a.dailyCost[k] += v
	}
	for day, byClient := range o.dailyClientTokens {
		if a.dailyClientTokens[day] == nil {
			a.dailyClientTokens[day] = make(map[string]float64)
		}
		for k, v := range byClient {
			a.dailyClientTokens[day][k] += v
		}
	}
	for day, byModel := range o.dailyModelTokens {
		if a.dailyModelTokens[day] == nil {
			a.dailyModelTokens[day] = make(map[string]int)
		}
		for k, v := range byModel {
			a.dailyModelTokens[day][k] += v
		}
	}
}

// finalizeSessions sets the per-client/project/agent session counts from the
// merged session sets.
func (a *conversationAllTime) finalizeSessions() {
	for k, t := range a.clientTotals {
		t.sessions = float64(len(a.clientSessions[k]))
	}
	for k, t := range a.projectTotals {
		t.sessions = float64(len(a.projectSessions[k]))
	}
	for k, t := range a.agentTotals {
		t.sessions = float64(len(a.agentSessions[k]))
	}
}

// buildAllTime recomputes the file's all-time partial, skipping the records
// and tool items that lost global dedup.
func (e *jsonlCacheEntry) buildAllTime(lostUsage []int, lostTools []toolSlot, gen uint64) {
	a := newConversationAllTime()
	costs := make([]float64, len(e.records))
	lu, lt := 0, 0
	for i, rec := range e.records {
		for idx, key := range e.toolKeys[i] {
			if key == "" {
				continue
			}
			if lt < len(lostTools) && lostTools[lt] == (toolSlot{rec: i, item: idx}) {
				lt++
				continue
			}
			a.addTool(rec.content[idx])
		}
		if e.usageKeys[i] == "" {
			continue
		}
		if lu < len(lostUsage) && lostUsage[lu] == i {
			lu++
			continue
		}
		costs[i] = estimateCost(rec.model, rec.usage)
		a.addUsage(rec, costs[i])
	}
	e.allTime = a
	e.costs = costs
	e.lostUsage = lostUsage
	e.lostTools = lostTools
	e.priceGen = gen
}

// conversationWindows accumulates the clock-dependent (today / 7d / current
// 5h block) aggregates.
type conversationWindows struct {
	todayStart     time.Time
	weekStart      time.Time
	inCurrentBlock bool
	blockStart     time.Time
	blockEnd       time.Time

	todayCostUSD       float64
	todayInputTokens   int
	todayOutputTokens  int
	todayCacheRead     int
	todayCacheCreate   int
	todayCacheCreate5m int
	todayCacheCreate1h int
	todayReasoning     int
	todayToolCalls     int
	todayWebSearch     int
	todayWebFetch      int
	todayMessages      int
	todayModels        map[string]bool
	todaySessions      map[string]bool

	weeklyCostUSD       float64
	weeklyInputTokens   int
	weeklyOutputTokens  int
	weeklyCacheRead     int
	weeklyCacheCreate   int
	weeklyCacheCreate5m int
	weeklyCacheCreate1h int
	weeklyReasoning     int
	weeklyToolCalls     int
	weeklyWebSearch     int
	weeklyWebFetch      int
	weeklyMessages      int
	weeklySessions      map[string]bool

	blockCostUSD      float64
	blockInputTokens  int
	blockOutputTokens int
	blockCacheRead    int
	blockCacheCreate  int
	blockMessages     int
	blockModels       map[string]bool
}

// addEntry folds in e's dedup-winning records with timestamp >= from.
func (w *conversationWindows) addEntry(e *jsonlCacheEntry, from time.Time) {
	start := sort.Search(len(e.records), func(i int) bool { return !e.records[i].timestamp.Before(from) })
	lu := sort.SearchInts(e.lostUsage, start)
	lt := sort.Search(len(e.lostTools), func(i int) bool { return e.lostTools[i].rec >= start })
	for i := start; i < len(e.records); i++ {
		u := e.records[i]
		inToday := !u.timestamp.Before(w.todayStart)
		inWeek := !u.timestamp.Before(w.weekStart)

		for idx, key := range e.toolKeys[i] {
			if key == "" {
				continue
			}
			if lt < len(e.lostTools) && e.lostTools[lt] == (toolSlot{rec: i, item: idx}) {
				lt++
				continue
			}
			if inToday {
				w.todayToolCalls++
			}
			if inWeek {
				w.weeklyToolCalls++
			}
		}

		if e.usageKeys[i] == "" {
			continue
		}
		if lu < len(e.lostUsage) && e.lostUsage[lu] == i {
			lu++
			continue
		}
		cost := e.costs[i]
		modelID := sanitizeModelName(u.model)

		if u.sessionID != "" {
			if inToday {
				w.todaySessions[u.sessionID] = true
			}
			if inWeek {
				w.weeklySessions[u.sessionID] = true
			}
		}

		if inToday {
			w.todayCostUSD += cost
			w.todayInputTokens += u.usage.InputTokens
			w.todayOutputTokens += u.usage.OutputTokens
			w.todayCacheRead += u.usage.CacheReadInputTokens
			w.todayCacheCreate += u.usage.CacheCreationInputTokens
			w.todayReasoning += u.usage.ReasoningTokens
			if u.usage.CacheCreation != nil {
				w.todayCacheCreate5m += u.usage.CacheCreation.Ephemeral5mInputTokens
				w.todayCacheCreate1h += u.usage.CacheCreation.Ephemeral1hInputTokens
			}
			if u.usage.ServerToolUse != nil {
				w.todayWebSearch += u.usage.ServerToolUse.WebSearchRequests
				w.todayWebFetch += u.usage.ServerToolUse.WebFetchRequests
			}
			w.todayMessages++
			w.todayModels[modelID] = true
		}

		if inWeek {
			w.weeklyCostUSD += cost
			w.weeklyInputTokens += u.usage.InputTokens
			w.weeklyOutputTokens += u.usage.OutputTokens
			w.weeklyCacheRead += u.usage.CacheReadInputTokens
			w.weeklyCacheCreate += u.usage.CacheCreationInputTokens
			w.weeklyReasoning += u.usage.ReasoningTokens
			if u.usage.CacheCreation != nil {
				w.weeklyCacheCreate5m += u.usage.CacheCreation.Ephemeral5mInputTokens
				w.weeklyCacheCreate1h += u.usage.CacheCreation.Ephemeral1hInputTokens
			}
			if u.usage.ServerToolUse != nil {
				w.weeklyWebSearch += u.usage.ServerToolUse.WebSearchRequests
				w.weeklyWebFetch += u.usage.ServerToolUse.WebFetchRequests
			}
			w.weeklyMessages++
		}

		if w.inCurrentBlock && !u.timestamp.Before(w.blockStart) && u.timestamp.Before(w.blockEnd) {
			w.blockCostUSD += cost
			w.blockInputTokens += u.usage.InputTokens
			w.blockOutputTokens += u.usage.OutputTokens
			w.blockCacheRead += u.usage.CacheReadInputTokens
			w.blockCacheCreate += u.usage.CacheCreationInputTokens
			w.blockMessages++
			w.blockModels[modelID] = true
		}
	}
}

// cachedConversationEntry returns the cached entry for a file if its mtime and
// size match, otherwise re-parses the file and replaces the entry. Caller
// must hold jsonlCacheMu.
func (p *Provider) cachedConversationEntry(path string, info os.FileInfo) *jsonlCacheEntry {
	if info == nil {
		return newConversationEntry(parseConversationRecords(path))
	}
	if p.jsonlCache == nil {
		p.jsonlCache = make(map[string]*jsonlCacheEntry)
	}
	if entry, ok := p.jsonlCache[path]; ok {
		if entry.modTime.Equal(info.ModTime()) && entry.size == info.Size() {
			return entry
		}
	}
	entry := newConversationEntry(parseConversationRecords(path))
	entry.modTime = info.ModTime()
	entry.size = info.Size()
	p.jsonlCache[path] = entry
	return entry
}

// newConversationEntry sorts a file's records by timestamp and precomputes
// their dedup keys.
func newConversationEntry(records []conversationRecord) *jsonlCacheEntry {
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].timestamp.Before(records[j].timestamp)
	})
	e := &jsonlCacheEntry{
		records:   records,
		usageKeys: make([]string, len(records)),
		toolKeys:  make([][]string, len(records)),
	}
	for i, rec := range records {
		if rec.usage != nil {
			e.usageKeys[i] = conversationUsageDedupKey(rec)
		}
		for idx, item := range rec.content {
			if item.Type != "tool_use" {
				continue
			}
			if e.toolKeys[i] == nil {
				e.toolKeys[i] = make([]string, len(rec.content))
			}
			e.toolKeys[i][idx] = conversationToolDedupKey(rec, idx, item)
		}
	}
	return e
}
