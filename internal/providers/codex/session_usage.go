package codex

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
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

// sessionUsageTotals holds the breakdown aggregates. One instance holds a
// single session file's contribution (cached across polls); merge folds the
// per-file values into the totals that are emitted.
type sessionUsageTotals struct {
	modelTotals            map[string]tokenUsage
	clientTotals           map[string]tokenUsage
	modelDaily             map[string]map[string]float64
	clientDaily            map[string]map[string]float64
	interfaceDaily         map[string]map[string]float64
	dailyTokenTotals       map[string]float64
	dailyRequestTotals     map[string]float64
	clientSessions         map[string]int
	clientRequests         map[string]int
	toolCalls              map[string]int
	langRequests           map[string]int
	callTool               map[string]string
	callOutcome            map[string]int
	stats                  patchStats
	modelCost              map[string]float64
	dailyCost              map[string]float64
	totalCostUSD           float64
	totalRequests          int
	promptCount            int
	commits                int
	completedWithoutCallID int
}

func newSessionUsageTotals() sessionUsageTotals {
	return sessionUsageTotals{
		modelTotals:        make(map[string]tokenUsage),
		clientTotals:       make(map[string]tokenUsage),
		modelDaily:         make(map[string]map[string]float64),
		clientDaily:        make(map[string]map[string]float64),
		interfaceDaily:     make(map[string]map[string]float64),
		dailyTokenTotals:   make(map[string]float64),
		dailyRequestTotals: make(map[string]float64),
		clientSessions:     make(map[string]int),
		clientRequests:     make(map[string]int),
		toolCalls:          make(map[string]int),
		langRequests:       make(map[string]int),
		callTool:           make(map[string]string),
		callOutcome:        make(map[string]int),
		stats: patchStats{
			Files:   make(map[string]struct{}),
			Deleted: make(map[string]struct{}),
		},
		modelCost: make(map[string]float64),
		dailyCost: make(map[string]float64),
	}
}

// costRecord is one priced usage delta. Records are kept so a pricing table
// reload can re-price a file without reading it again.
type costRecord struct {
	model string // raw model id passed to the price lookup
	name  string // normalized model name the cost is attributed to
	day   string
	delta tokenUsage
}

// sessionFileParser accumulates one session file. It carries the walker
// state between polls so bytes appended to the file can be applied on top of
// the previous result without re-reading what came before.
type sessionFileParser struct {
	defaultDay string
	// sessionDefaultModel is the model_id captured from the session header
	// (or refreshed by a turn_context line); it acts as the fallback when a
	// token_count event does not carry its own model field. currentModel is
	// the resolved value used for the immediately-following accounting.
	sessionClient       string
	sessionDefaultModel string
	currentModel        string
	firstRealModel      string
	previous            tokenUsage
	hasPrevious         bool
	countedSession      bool
	// Early token_counts that occur before any model is known (e.g. the
	// first token_count at ordinal 5 precedes turn_context at 8) are
	// buffered and flushed to the first real model, so Model Burn doesn't
	// show them as unknown.
	pendingDeltas []tokenUsage
	pendingDays   []string

	totals      sessionUsageTotals
	costRecords []costRecord
	priceGen    uint64
}

func newSessionFileParser(defaultDay string) *sessionFileParser {
	return &sessionFileParser{
		defaultDay:          defaultDay,
		sessionClient:       "Other",
		sessionDefaultModel: "unknown",
		currentModel:        "unknown",
		totals:              newSessionUsageTotals(),
	}
}

func (s *sessionFileParser) addCost(model, name, day string, delta tokenUsage) {
	rec := costRecord{model: model, name: name, day: day, delta: delta}
	s.costRecords = append(s.costRecords, rec)
	s.applyCost(rec)
}

func (s *sessionFileParser) applyCost(rec costRecord) {
	cost := estimateUsageCost(rec.model, rec.delta)
	if cost <= 0 {
		return
	}
	s.totals.modelCost[rec.name] += cost
	s.totals.totalCostUSD += cost
	if rec.day != "" {
		s.totals.dailyCost[rec.day] += cost
	}
}

// reprice recomputes the file's cost aggregates from its recorded deltas.
func (s *sessionFileParser) reprice(gen uint64) {
	s.totals.modelCost = make(map[string]float64)
	s.totals.dailyCost = make(map[string]float64)
	s.totals.totalCostUSD = 0
	for _, rec := range s.costRecords {
		s.applyCost(rec)
	}
	s.priceGen = gen
}

func (s *sessionFileParser) flushPending(realModel string) {
	if len(s.pendingDeltas) == 0 || realModel == "" || realModel == "unknown" {
		return
	}
	realName := normalizeModelName(realModel)
	for i, d := range s.pendingDeltas {
		day := s.pendingDays[i]
		addUsage(s.totals.modelTotals, realName, d)
		addDailyUsage(s.totals.modelDaily, realName, day, float64(d.TotalTokens))
		s.addCost(realModel, realName, day, d)
	}
	s.pendingDeltas = nil
	s.pendingDays = nil
}

func (s *sessionFileParser) setModel(m string) {
	if s.firstRealModel == "" {
		s.firstRealModel = m
		s.flushPending(m)
	}
	s.sessionDefaultModel = m
	s.currentModel = m
}

// countRequest records the per-request aggregates shared by known and
// still-unknown models.
func (s *sessionFileParser) countRequest(clientName, day string, delta tokenUsage) {
	t := &s.totals
	addUsage(t.clientTotals, clientName, delta)
	addDailyUsage(t.clientDaily, clientName, day, float64(delta.TotalTokens))
	addDailyUsage(t.interfaceDaily, clientInterfaceBucket(clientName), day, 1)
	t.dailyTokenTotals[day] += float64(delta.TotalTokens)
	t.dailyRequestTotals[day]++
	t.clientRequests[clientName]++
	t.totalRequests++
}

func (s *sessionFileParser) apply(record sessionLine) {
	t := &s.totals
	switch {
	case record.SessionMeta != nil:
		s.sessionClient = classifyClient(record.SessionMeta.Source, record.SessionMeta.Originator)
		// ProvenanceModel is the fallback for CLI versions that no longer
		// write an explicit model to the header; the explicit fields still
		// win when present.
		if m := core.FirstNonEmpty(record.SessionMeta.Model, record.SessionMeta.ModelID, record.SessionMeta.ProvenanceModel()); m != "" {
			s.setModel(m)
		}
	case record.TurnContext != nil:
		if m := core.FirstNonEmpty(record.TurnContext.Model, record.TurnContext.ModelID); strings.TrimSpace(m) != "" {
			s.setModel(m)
		}
	case record.EventPayload != nil:
		payload := record.EventPayload
		if payload.Type == "user_message" {
			t.promptCount++
			return
		}
		if payload.Type == "thread_settings_applied" {
			if ts := payload.ThreadSettings; ts != nil {
				if m := core.FirstNonEmpty(ts.Model, ts.ModelID); strings.TrimSpace(m) != "" {
					s.setModel(m)
				}
			}
			return
		}
		if payload.Type != "token_count" || payload.Info == nil {
			return
		}
		// Per-message override: when a token_count event carries its own
		// model, prefer it. Otherwise fall back to the session default
		// captured from the header / last turn_context.
		if perEvent := core.FirstNonEmpty(payload.Model, payload.ModelID); perEvent != "" {
			s.currentModel = perEvent
		} else {
			s.currentModel = s.sessionDefaultModel
		}

		total := payload.Info.TotalTokenUsage
		delta := total
		if s.hasPrevious {
			delta = usageDelta(total, s.previous)
			if !validUsageDelta(delta) {
				delta = total
			}
		}
		s.previous = total
		s.hasPrevious = true

		if delta.TotalTokens <= 0 {
			return
		}

		modelName := normalizeModelName(s.currentModel)
		clientName := normalizeClientName(s.sessionClient)
		day := dayFromTimestamp(record.Timestamp)
		if day == "" {
			day = s.defaultDay
		}

		if modelName == "unknown" {
			s.pendingDeltas = append(s.pendingDeltas, delta)
			s.pendingDays = append(s.pendingDays, day)
			if s.firstRealModel != "" {
				// Already have a real model: attribute this one to it.
				s.flushPending(s.firstRealModel)
			}
		} else {
			if len(s.pendingDeltas) > 0 && s.firstRealModel == "" {
				s.firstRealModel = s.currentModel
				s.flushPending(s.currentModel)
			}
			addUsage(t.modelTotals, modelName, delta)
			addDailyUsage(t.modelDaily, modelName, day, float64(delta.TotalTokens))
		}
		s.countRequest(clientName, day, delta)
		if modelName != "unknown" {
			s.addCost(s.currentModel, modelName, day, delta)
		}
		if !s.countedSession {
			t.clientSessions[clientName]++
			s.countedSession = true
		}
	case record.ResponseItem != nil:
		item := record.ResponseItem
		switch item.Type {
		case "function_call":
			tool := normalizeToolName(item.Name)
			recordToolCall(t.toolCalls, t.callTool, item.CallID, tool)
			if strings.EqualFold(tool, "exec_command") {
				var args commandArgs
				if json.Unmarshal(item.Arguments, &args) == nil {
					recordCommandLanguage(args.Cmd, t.langRequests)
					if commandContainsGitCommit(args.Cmd) {
						t.commits++
					}
				}
			}
		case "custom_tool_call":
			tool := normalizeToolName(item.Name)
			recordToolCall(t.toolCalls, t.callTool, item.CallID, tool)
			if strings.EqualFold(tool, "apply_patch") {
				t.stats.PatchCalls++
				accumulatePatchStats(item.Input, &t.stats, t.langRequests)
			}
		case "web_search_call":
			recordToolCall(t.toolCalls, t.callTool, "", "web_search")
			t.completedWithoutCallID++
		case "function_call_output", "custom_tool_call_output":
			setToolCallOutcome(item.CallID, item.Output, t.callOutcome)
		}
	}
}

// keepBreakdownLine skips decoding record kinds the breakdown never reads:
// compacted history, chat messages, reasoning and most event types. Together
// they are the bulk of a rollout's bytes.
func keepBreakdownLine(recordType, payloadType []byte) bool {
	switch string(recordType) {
	case "session_meta", "turn_context":
		return true
	case "event_msg":
		if payloadType == nil {
			return true
		}
		switch string(payloadType) {
		case "user_message", "thread_settings_applied", "token_count":
			return true
		}
		return false
	case "response_item":
		if payloadType == nil {
			return true
		}
		switch string(payloadType) {
		case "function_call", "custom_tool_call", "web_search_call", "function_call_output", "custom_tool_call_output":
			return true
		}
		return false
	default:
		return false
	}
}

// mergeFile folds one file's contribution into t. Files must be merged in
// path order: callTool/callOutcome are last-write-wins across files.
func (t *sessionUsageTotals) mergeFile(s *sessionFileParser) {
	f := &s.totals
	for name, u := range f.modelTotals {
		addUsage(t.modelTotals, name, u)
	}
	for name, u := range f.clientTotals {
		addUsage(t.clientTotals, name, u)
	}
	mergeDaily(t.modelDaily, f.modelDaily)
	mergeDaily(t.clientDaily, f.clientDaily)
	mergeDaily(t.interfaceDaily, f.interfaceDaily)
	// Deltas never attributed to a real model stay unknown (no cost). They
	// are applied at merge time because a later append may still resolve
	// them to a model.
	for i, d := range s.pendingDeltas {
		addUsage(t.modelTotals, "unknown", d)
		addDailyUsage(t.modelDaily, "unknown", s.pendingDays[i], float64(d.TotalTokens))
	}
	for day, v := range f.dailyTokenTotals {
		t.dailyTokenTotals[day] += v
	}
	for day, v := range f.dailyRequestTotals {
		t.dailyRequestTotals[day] += v
	}
	mergeCounts(t.clientSessions, f.clientSessions)
	mergeCounts(t.clientRequests, f.clientRequests)
	mergeCounts(t.toolCalls, f.toolCalls)
	mergeCounts(t.langRequests, f.langRequests)
	for id, tool := range f.callTool {
		t.callTool[id] = tool
	}
	for id, outcome := range f.callOutcome {
		t.callOutcome[id] = outcome
	}
	t.stats.Added += f.stats.Added
	t.stats.Removed += f.stats.Removed
	t.stats.PatchCalls += f.stats.PatchCalls
	for file := range f.stats.Files {
		t.stats.Files[file] = struct{}{}
	}
	for file := range f.stats.Deleted {
		t.stats.Deleted[file] = struct{}{}
	}
	for name, v := range f.modelCost {
		t.modelCost[name] += v
	}
	for day, v := range f.dailyCost {
		t.dailyCost[day] += v
	}
	t.totalCostUSD += f.totalCostUSD
	t.totalRequests += f.totalRequests
	t.promptCount += f.promptCount
	t.commits += f.commits
	t.completedWithoutCallID += f.completedWithoutCallID
}

func mergeDaily(dst, src map[string]map[string]float64) {
	for name, byDay := range src {
		for day, v := range byDay {
			addDailyUsage(dst, name, day, v)
		}
	}
}

func mergeCounts(dst, src map[string]int) {
	for k, v := range src {
		dst[k] += v
	}
}

func (p *Provider) readSessionUsageBreakdowns(sessionsDir string, snap *core.UsageSnapshot) error {
	p.sessionFileCacheMu.Lock()
	parsers, err := p.refreshSessionFiles(sessionsDir)
	if err != nil {
		p.sessionFileCacheMu.Unlock()
		return err
	}
	totals := newSessionUsageTotals()
	for _, parser := range parsers {
		totals.mergeFile(parser)
	}
	p.sessionFileCacheMu.Unlock()

	// "Today" is derived at emit time so cached per-file aggregates stay
	// valid across midnight.
	today := time.Now().UTC().Format("2006-01-02")
	requestsToday := int(totals.dailyRequestTotals[today])
	todayCostUSD := totals.dailyCost[today]

	emitBreakdownMetrics("model", totals.modelTotals, totals.modelDaily, snap)
	emitBreakdownMetrics("client", totals.clientTotals, totals.clientDaily, snap)
	emitClientSessionMetrics(totals.clientSessions, snap)
	emitClientRequestMetrics(totals.clientRequests, snap)
	emitToolMetrics(totals.toolCalls, totals.callTool, totals.callOutcome, totals.completedWithoutCallID, snap)
	emitLanguageMetrics(totals.langRequests, snap)
	emitProductivityMetrics(totals.stats, totals.promptCount, totals.commits, totals.totalRequests, requestsToday, totals.clientSessions, snap)
	emitDailyUsageSeries(totals.dailyTokenTotals, totals.dailyRequestTotals, totals.interfaceDaily, snap)
	emitCostMetrics(totals.modelCost, totals.dailyCost, totals.totalCostUSD, todayCostUSD, snap)

	return nil
}
