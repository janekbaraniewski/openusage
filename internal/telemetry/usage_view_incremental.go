package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// incrementalUsageState is the process-local projection of logical winners.
// The durable usage_event_changes sequence lets it advance from one consistent
// point to the next without rescanning history. A daemon restart performs one
// cold winner build and then resumes incremental processing.
type incrementalUsageState struct {
	winners         map[string]string
	candidates      map[string]usageViewEvent
	candidatesByKey map[string][]string
	changeSeq       int64
	appliedChanges  int64
	filter          usageFilter
	acc             *usageAccumulator
}

type usageViewEvent struct {
	EventID, OccurredAt, ProviderID, AccountID, WorkspaceID string
	SessionID, TurnID, MessageID, ToolCallID                string
	EventType, ModelRaw, ModelCanonical                     string
	ToolName, Status, DedupKey                              string
	SourceSystem, SourceChannel, SourcePayload              string
	Input, Output, Reasoning, CacheRead, CacheWrite         float64
	Total, ActivityTotal, Cost, Requests                    float64
	logicalKey                                              string
	sourcePriority, qualityScore                            int
}

func buildIncrementalUsageState(ctx context.Context, db *sql.DB, filter usageFilter) (*incrementalUsageState, *telemetryUsageAgg, error) {
	var startSeq int64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM usage_event_changes`).Scan(&startSeq); err != nil {
		return nil, nil, fmt.Errorf("read usage change watermark: %w", err)
	}

	where, args := usageWhereClause("", filter)
	query := usageViewEventSelect + ` FROM (
		SELECT e.*, r.source_system, r.source_channel, r.source_payload
		FROM usage_events e JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
	) AS deduped_usage WHERE ` + where + ` AND event_type IN ('message_usage', 'tool_usage')`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("load incremental usage winners: %w", err)
	}
	defer rows.Close()

	state := &incrementalUsageState{
		winners:         make(map[string]string),
		candidates:      make(map[string]usageViewEvent),
		candidatesByKey: make(map[string][]string),
		changeSeq:       startSeq,
		filter:          filter,
		acc:             newUsageAccumulator(filter),
	}
	for rows.Next() {
		event, scanErr := scanUsageViewEvent(rows)
		if scanErr != nil {
			return nil, nil, scanErr
		}
		state.addCandidate(event)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate incremental usage winners: %w", err)
	}
	return state, state.acc.snapshot(), nil
}

const usageViewEventSelect = `
	SELECT event_id, occurred_at, COALESCE(provider_id, ''), COALESCE(account_id, ''),
	       COALESCE(workspace_id, ''), COALESCE(session_id, ''), COALESCE(turn_id, ''),
	       COALESCE(message_id, ''), COALESCE(tool_call_id, ''), event_type,
	       COALESCE(model_raw, ''), COALESCE(model_canonical, ''),
	       COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
	       COALESCE(reasoning_tokens, 0), COALESCE(cache_read_tokens, 0),
	       COALESCE(cache_write_tokens, 0),
	       COALESCE(total_tokens, COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0) +
	           COALESCE(reasoning_tokens, 0) + COALESCE(cache_read_tokens, 0) + COALESCE(cache_write_tokens, 0)),
	       COALESCE(total_tokens, 0), COALESCE(cost_usd, 0), COALESCE(requests, 1),
	       COALESCE(tool_name, ''), status, dedup_key,
	       COALESCE(source_system, ''), COALESCE(source_channel, ''), COALESCE(source_payload, '{}')`

type usageEventRowScanner interface{ Scan(...any) error }

func scanUsageViewEvent(row usageEventRowScanner) (usageViewEvent, error) {
	var e usageViewEvent
	if err := row.Scan(
		&e.EventID, &e.OccurredAt, &e.ProviderID, &e.AccountID, &e.WorkspaceID,
		&e.SessionID, &e.TurnID, &e.MessageID, &e.ToolCallID, &e.EventType,
		&e.ModelRaw, &e.ModelCanonical, &e.Input, &e.Output, &e.Reasoning,
		&e.CacheRead, &e.CacheWrite, &e.Total, &e.ActivityTotal, &e.Cost,
		&e.Requests, &e.ToolName, &e.Status, &e.DedupKey, &e.SourceSystem,
		&e.SourceChannel, &e.SourcePayload,
	); err != nil {
		return e, fmt.Errorf("scan incremental usage event: %w", err)
	}
	e.prepare()
	return e, nil
}

func (e *usageViewEvent) prepare() {
	source := strings.ToLower(strings.TrimSpace(e.SourceSystem))
	eventType := strings.ToLower(strings.TrimSpace(e.EventType))
	session := strings.ToLower(strings.TrimSpace(e.SessionID))
	logicalID := "fallback:" + e.DedupKey
	if v := strings.ToLower(strings.TrimSpace(e.ToolCallID)); v != "" {
		logicalID = "tool:" + v
	} else if eventType == string(EventTypeMessageUsage) && source == "codex" && strings.TrimSpace(e.TurnID) != "" {
		logicalID = "message_turn:" + strings.ToLower(strings.TrimSpace(e.TurnID))
	} else if v := strings.ToLower(strings.TrimSpace(e.MessageID)); v != "" {
		logicalID = "message:" + v
	} else if v := strings.ToLower(strings.TrimSpace(e.TurnID)); v != "" {
		logicalID = "turn:" + v
	}
	e.logicalKey = source + "\x00" + eventType + "\x00" + session + "\x00" + logicalID
	e.sourcePriority = sourceChannelPriority(SourceChannel(strings.TrimSpace(e.SourceChannel)))
	if e.ActivityTotal > 0 {
		e.qualityScore += 4
	}
	if e.Cost > 0 {
		e.qualityScore += 2
	}
	if strings.TrimSpace(firstNonEmpty(e.ModelCanonical, e.ModelRaw)) != "" {
		e.qualityScore++
	}
	provider := strings.ToLower(strings.TrimSpace(e.ProviderID))
	if provider != "" && provider != "unknown" && provider != "opencode" {
		e.qualityScore++
	}
}

func (e usageViewEvent) outranks(other usageViewEvent) bool {
	if e.sourcePriority != other.sourcePriority {
		return e.sourcePriority > other.sourcePriority
	}
	if e.qualityScore != other.qualityScore {
		return e.qualityScore > other.qualityScore
	}
	if e.OccurredAt != other.OccurredAt {
		return e.OccurredAt > other.OccurredAt
	}
	return e.EventID > other.EventID
}

func (s *incrementalUsageState) addCandidate(event usageViewEvent) {
	s.candidates[event.EventID] = event
	s.candidatesByKey[event.logicalKey] = append(s.candidatesByKey[event.logicalKey], event.EventID)
	currentID, exists := s.winners[event.logicalKey]
	current := s.candidates[currentID]
	if exists && !event.outranks(current) {
		return
	}
	if exists {
		s.acc.apply(current, -1)
	}
	s.winners[event.logicalKey] = event.EventID
	s.acc.apply(event, 1)
}

func (s *incrementalUsageState) removeCandidate(eventID string) {
	event, exists := s.candidates[eventID]
	if !exists {
		return
	}
	delete(s.candidates, eventID)
	ids := s.candidatesByKey[event.logicalKey]
	for i, candidateID := range ids {
		if candidateID == eventID {
			ids = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(ids) == 0 {
		delete(s.candidatesByKey, event.logicalKey)
	} else {
		s.candidatesByKey[event.logicalKey] = ids
	}
	currentID, wasWinner := s.winners[event.logicalKey]
	if !wasWinner || currentID != eventID {
		return
	}
	current := event
	s.acc.apply(current, -1)
	delete(s.winners, event.logicalKey)
	var promoted usageViewEvent
	hasPromoted := false
	for _, candidateID := range ids {
		candidate := s.candidates[candidateID]
		if !hasPromoted || candidate.outranks(promoted) {
			promoted, hasPromoted = candidate, true
		}
	}
	if hasPromoted {
		s.winners[event.logicalKey] = promoted.EventID
		s.acc.apply(promoted, 1)
	}
}

func (s *incrementalUsageState) advanceFilter(next usageFilter) bool {
	if next.Since.Before(s.filter.Since) {
		return false
	}
	if next.TodaySince != s.filter.TodaySince {
		return false
	}
	if next.Since.After(s.filter.Since) {
		for eventID, event := range s.candidates {
			occurred, err := time.Parse(time.RFC3339Nano, event.OccurredAt)
			if err != nil || occurred.Before(next.Since) {
				s.removeCandidate(eventID)
			}
		}
	}
	s.filter = next
	s.acc.filter = next
	return true
}

func (s *incrementalUsageState) applyChanges(ctx context.Context, db *sql.DB, filter usageFilter) (*telemetryUsageAgg, bool, error) {
	if !s.advanceFilter(filter) {
		return nil, false, nil
	}
	var minSeq, maxSeq int64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MIN(seq), 0), COALESCE(MAX(seq), 0) FROM usage_event_changes`).Scan(&minSeq, &maxSeq); err != nil {
		return nil, false, fmt.Errorf("probe incremental usage changes: %w", err)
	}
	if minSeq > 0 && s.changeSeq < minSeq-1 {
		// Maintenance pruned changes this dormant cache never consumed. A cold
		// rebuild is required; replaying only the remaining suffix would be wrong.
		return nil, false, nil
	}
	if maxSeq <= s.changeSeq {
		return s.acc.snapshot(), true, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT seq, event_id, operation FROM usage_event_changes WHERE seq > ? ORDER BY seq`, s.changeSeq)
	if err != nil {
		return nil, false, fmt.Errorf("load incremental usage changes: %w", err)
	}
	type change struct {
		seq                int64
		eventID, operation string
	}
	var changes []change
	for rows.Next() {
		var c change
		if err := rows.Scan(&c.seq, &c.eventID, &c.operation); err != nil {
			rows.Close()
			return nil, false, err
		}
		changes = append(changes, c)
	}
	if err := rows.Close(); err != nil {
		return nil, false, err
	}
	if len(changes) == 0 {
		return s.acc.snapshot(), true, nil
	}

	for _, c := range changes {
		s.removeCandidate(c.eventID)
		if c.operation == "delete" {
			s.changeSeq = c.seq
			s.appliedChanges++
			continue
		}

		event, found, loadErr := loadUsageViewEventByID(ctx, db, c.eventID)
		if loadErr != nil {
			return nil, false, loadErr
		}
		if !found {
			s.changeSeq = c.seq
			continue
		}
		if !usageViewEventMatchesFilter(event, s.filter) {
			s.changeSeq = c.seq
			continue
		}
		s.addCandidate(event)
		s.changeSeq = c.seq
		s.appliedChanges++
	}
	return s.acc.snapshot(), true, nil
}

func loadUsageViewEventByID(ctx context.Context, db *sql.DB, eventID string) (usageViewEvent, bool, error) {
	query := usageViewEventSelect + `
		FROM (
			SELECT e.*, r.source_system, r.source_channel, r.source_payload
			FROM usage_events e JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
		) AS deduped_usage
		WHERE event_id = ?`
	e, err := scanUsageViewEvent(db.QueryRowContext(ctx, query, eventID))
	if err != nil {
		if strings.Contains(err.Error(), sql.ErrNoRows.Error()) {
			return usageViewEvent{}, false, nil
		}
		return usageViewEvent{}, false, err
	}
	return e, true, nil
}

func usageViewEventMatchesFilter(e usageViewEvent, filter usageFilter) bool {
	providers := normalizeProviderIDs(filter.ProviderIDs)
	found := false
	for _, provider := range providers {
		if strings.EqualFold(strings.TrimSpace(e.ProviderID), provider) {
			found = true
			break
		}
	}
	if !found || (strings.TrimSpace(filter.AccountID) != "" && strings.TrimSpace(e.AccountID) != strings.TrimSpace(filter.AccountID)) {
		return false
	}
	if !filter.Since.IsZero() {
		occurred, err := time.Parse(time.RFC3339Nano, e.OccurredAt)
		if err != nil || occurred.Before(filter.Since) {
			return false
		}
	}
	return e.EventType == string(EventTypeMessageUsage) || e.EventType == string(EventTypeToolUsage)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type usageAccumulator struct {
	filter                                                                     usageFilter
	models                                                                     map[string]*telemetryModelAgg
	sources                                                                    map[string]*telemetrySourceAgg
	projects                                                                   map[string]*telemetryProjectAgg
	tools                                                                      map[string]*telemetryToolAgg
	providers                                                                  map[string]*telemetryProviderAgg
	languages                                                                  map[string]float64
	daily                                                                      map[string]*telemetryDayPoint
	modelDaily, sourceDaily, projectDaily, mcpDaily, clientDaily, clientTokens map[string]map[string]float64
	sourceSessions                                                             map[string]map[string]int
	messageRefs, sessionRefs, changedFileRefs                                  map[string]int
	lastOccurredRefs                                                           map[string]int
	activity                                                                   telemetryActivityAgg
	codeStats                                                                  telemetryCodeStatsAgg
	eventCount                                                                 int64
}

func newUsageAccumulator(filter usageFilter) *usageAccumulator {
	return &usageAccumulator{
		filter: filter,
		models: make(map[string]*telemetryModelAgg), sources: make(map[string]*telemetrySourceAgg),
		projects: make(map[string]*telemetryProjectAgg), tools: make(map[string]*telemetryToolAgg),
		providers: make(map[string]*telemetryProviderAgg), languages: make(map[string]float64),
		daily: make(map[string]*telemetryDayPoint), modelDaily: make(map[string]map[string]float64),
		sourceDaily: make(map[string]map[string]float64), projectDaily: make(map[string]map[string]float64),
		mcpDaily: make(map[string]map[string]float64), clientDaily: make(map[string]map[string]float64),
		clientTokens: make(map[string]map[string]float64), sourceSessions: make(map[string]map[string]int),
		messageRefs: make(map[string]int), sessionRefs: make(map[string]int), changedFileRefs: make(map[string]int),
		lastOccurredRefs: make(map[string]int),
	}
}

func (a *usageAccumulator) apply(e usageViewEvent, sign float64) {
	if sign == 0 {
		return
	}
	if sign > 0 {
		a.eventCount++
		a.lastOccurredRefs[e.OccurredAt]++
	} else {
		a.eventCount--
		decRef(a.lastOccurredRefs, e.OccurredAt)
	}

	payload := decodeUsagePayload(e.SourcePayload)
	requests := e.Requests
	day := usageEventDay(e.OccurredAt)
	today := a.isToday(e.OccurredAt)
	messageOK := e.EventType == string(EventTypeMessageUsage) && e.Status != string(EventStatusError)
	tool := e.EventType == string(EventTypeToolUsage)

	if messageOK {
		model := strings.TrimSpace(firstNonEmpty(e.ModelCanonical, e.ModelRaw))
		if model == "" {
			model = "unknown"
		}
		m := ensureMapValue(a.models, model, func() *telemetryModelAgg { return &telemetryModelAgg{Model: model} })
		m.InputTokens += sign * e.Input
		m.OutputTokens += sign * e.Output
		m.CacheReadTokens += sign * e.CacheRead
		m.CacheWriteTokens += sign * e.CacheWrite
		m.Reasoning += sign * e.Reasoning
		m.TotalTokens += sign * e.Total
		m.BillableTokens += sign * (e.Input + e.Output + e.Reasoning + e.CacheWrite)
		m.CostUSD += sign * e.Cost
		m.Requests += sign * requests
		if today {
			m.Requests1d += sign * requests
		}

		client := usageClient(e, payload)
		s := ensureMapValue(a.sources, client, func() *telemetrySourceAgg { return &telemetrySourceAgg{Source: client} })
		s.Requests += sign * requests
		s.Tokens += sign * e.Total
		s.Input += sign * e.Input
		s.Output += sign * e.Output
		s.Cached += sign * (e.CacheRead + e.CacheWrite)
		s.Reasoning += sign * e.Reasoning
		if today {
			s.Requests1d += sign * requests
		}
		session := strings.TrimSpace(e.SessionID)
		if session == "" {
			session = "unknown"
		}
		if a.sourceSessions[client] == nil {
			a.sourceSessions[client] = make(map[string]int)
		}
		adjustDistinct(a.sourceSessions[client], session, int(sign), &s.Sessions)

		if project := strings.TrimSpace(e.WorkspaceID); project != "" {
			p := ensureMapValue(a.projects, project, func() *telemetryProjectAgg { return &telemetryProjectAgg{Project: project} })
			p.Requests += sign * requests
			if today {
				p.Requests1d += sign * requests
			}
			if projectKey := sanitizeMetricID(project); projectKey != "unknown" {
				addDaily(a.projectDaily, projectKey, day, sign*requests)
			}
		}

		provider := usageUpstreamProvider(e, payload)
		p := ensureMapValue(a.providers, provider, func() *telemetryProviderAgg { return &telemetryProviderAgg{Provider: provider} })
		p.CostUSD += sign * e.Cost
		p.Requests += sign * requests
		p.Input += sign * e.Input
		p.Output += sign * e.Output

		messageKey := strings.TrimSpace(firstNonEmpty(e.MessageID, e.TurnID, e.DedupKey))
		adjustDistinct(a.messageRefs, messageKey, int(sign), &a.activity.Messages)
		if sessionID := strings.TrimSpace(e.SessionID); sessionID != "" {
			adjustDistinct(a.sessionRefs, sessionID, int(sign), &a.activity.Sessions)
		}
		a.activity.InputTokens += sign * e.Input
		a.activity.OutputTokens += sign * e.Output
		a.activity.CachedTokens += sign * e.CacheRead
		a.activity.ReasonTokens += sign * e.Reasoning
		a.activity.TotalTokens += sign * e.ActivityTotal
		a.activity.TotalCost += sign * e.Cost
		if today {
			a.activity.TotalCostToday += sign * e.Cost
		}

		d := ensureMapValue(a.daily, day, func() *telemetryDayPoint { return &telemetryDayPoint{Day: day} })
		d.CostUSD += sign * e.Cost
		d.Requests += sign * requests
		d.Tokens += sign * e.Total
		addDaily(a.modelDaily, sanitizeMetricID(model), day, sign*requests)
		addDaily(a.sourceDaily, sanitizeMetricID(client), day, sign*requests)
		addDaily(a.clientDaily, sanitizeMetricID(client), day, sign*requests)
		addDaily(a.clientTokens, sanitizeMetricID(client), day, sign*e.Total)
	}

	if tool {
		toolName := strings.ToLower(strings.TrimSpace(e.ToolName))
		if toolName == "" {
			toolName = "unknown"
		}
		t := ensureMapValue(a.tools, toolName, func() *telemetryToolAgg { return &telemetryToolAgg{Tool: toolName} })
		t.Calls += sign * requests
		if today {
			t.Calls1d += sign * requests
		}
		switch e.Status {
		case string(EventStatusOK):
			t.CallsOK += sign * requests
			if today {
				t.CallsOK1d += sign * requests
			}
		case string(EventStatusError):
			t.CallsError += sign * requests
			if today {
				t.CallsError1d += sign * requests
			}
		case string(EventStatusAborted):
			t.CallsAborted += sign * requests
			if today {
				t.CallsAborted1d += sign * requests
			}
		}
		a.activity.ToolCalls += sign * requests
		if server, _, ok := parseMCPToolName(toolName); ok && strings.TrimSpace(server) != "" {
			addDaily(a.mcpDaily, sanitizeMetricID(server), day, sign*requests)
		}
	}

	if messageOK || (tool && e.Status != string(EventStatusError)) {
		file := usageFilePath(payload)
		if lang := inferLanguageFromFilePath(file); lang != "" {
			a.languages[lang] += sign * requests
		}
		if tool && isMutationTool(e.ToolName) && strings.TrimSpace(file) != "" {
			adjustDistinct(a.changedFileRefs, strings.TrimSpace(file), int(sign), &a.codeStats.FilesChanged)
		}
		a.codeStats.LinesAdded += sign * jsonNumber(payload["lines_added"])
		a.codeStats.LinesRemoved += sign * jsonNumber(payload["lines_removed"])
	}
}

func (a *usageAccumulator) isToday(occurred string) bool {
	t, err := time.Parse(time.RFC3339Nano, occurred)
	if err != nil {
		return false
	}
	if !a.filter.TodaySince.IsZero() {
		return !t.Before(a.filter.TodaySince)
	}
	now := time.Now().UTC()
	y, m, d := now.Date()
	return !t.Before(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

func (a *usageAccumulator) snapshot() *telemetryUsageAgg {
	agg := newTelemetryUsageAgg()
	agg.EventCount = a.eventCount
	for occurred := range a.lastOccurredRefs {
		if occurred > agg.LastOccurred {
			agg.LastOccurred = occurred
		}
	}
	for _, v := range a.models {
		if v.Requests != 0 {
			agg.Models = append(agg.Models, *v)
		}
	}
	sort.Slice(agg.Models, func(i, j int) bool {
		if agg.Models[i].BillableTokens == agg.Models[j].BillableTokens {
			return agg.Models[i].Requests > agg.Models[j].Requests
		}
		return agg.Models[i].BillableTokens > agg.Models[j].BillableTokens
	})
	if len(agg.Models) > 500 {
		agg.Models = agg.Models[:500]
	}
	for _, v := range a.sources {
		if v.Requests != 0 {
			agg.Sources = append(agg.Sources, *v)
		}
	}
	sort.Slice(agg.Sources, func(i, j int) bool { return agg.Sources[i].Requests > agg.Sources[j].Requests })
	if len(agg.Sources) > 500 {
		agg.Sources = agg.Sources[:500]
	}
	for _, v := range a.projects {
		if v.Requests != 0 {
			agg.Projects = append(agg.Projects, *v)
		}
	}
	sort.Slice(agg.Projects, func(i, j int) bool { return agg.Projects[i].Requests > agg.Projects[j].Requests })
	if len(agg.Projects) > 500 {
		agg.Projects = agg.Projects[:500]
	}
	for _, v := range a.tools {
		if v.Calls != 0 {
			agg.Tools = append(agg.Tools, *v)
		}
	}
	sort.Slice(agg.Tools, func(i, j int) bool { return agg.Tools[i].Calls > agg.Tools[j].Calls })
	if len(agg.Tools) > 500 {
		agg.Tools = agg.Tools[:500]
	}
	for _, v := range a.providers {
		if v.Requests != 0 {
			agg.Providers = append(agg.Providers, *v)
		}
	}
	sort.Slice(agg.Providers, func(i, j int) bool {
		if agg.Providers[i].CostUSD == agg.Providers[j].CostUSD {
			return agg.Providers[i].Requests > agg.Providers[j].Requests
		}
		return agg.Providers[i].CostUSD > agg.Providers[j].CostUSD
	})
	if len(agg.Providers) > 200 {
		agg.Providers = agg.Providers[:200]
	}
	for k, v := range a.languages {
		if v != 0 {
			agg.Languages = append(agg.Languages, telemetryLanguageAgg{Language: k, Requests: v})
		}
	}
	sort.Slice(agg.Languages, func(i, j int) bool { return agg.Languages[i].Requests > agg.Languages[j].Requests })
	for _, v := range a.daily {
		if v.Requests != 0 {
			agg.Daily = append(agg.Daily, *v)
		}
	}
	sort.Slice(agg.Daily, func(i, j int) bool { return agg.Daily[i].Day < agg.Daily[j].Day })
	agg.Activity = a.activity
	agg.CodeStats = a.codeStats
	agg.MCPServers = buildMCPAgg(agg.Tools)
	agg.ModelDaily = finalizeDaily(a.modelDaily)
	agg.SourceDaily = finalizeDaily(a.sourceDaily)
	agg.ProjectDaily = finalizeDaily(a.projectDaily)
	agg.MCPDaily = finalizeDaily(a.mcpDaily)
	agg.ClientDaily = finalizeDaily(a.clientDaily)
	agg.ClientTokens = finalizeDaily(a.clientTokens)
	return agg
}

func ensureMapValue[T any](m map[string]*T, key string, makeValue func() *T) *T {
	if m[key] == nil {
		m[key] = makeValue()
	}
	return m[key]
}
func decRef(m map[string]int, key string) {
	if m[key] <= 1 {
		delete(m, key)
	} else {
		m[key]--
	}
}
func adjustDistinct(m map[string]int, key string, delta int, total *float64) {
	before := m[key]
	after := before + delta
	if after <= 0 {
		delete(m, key)
		after = 0
	} else {
		m[key] = after
	}
	if before == 0 && after > 0 {
		*total++
	} else if before > 0 && after == 0 {
		*total--
	}
}
func addDaily(m map[string]map[string]float64, key, day string, value float64) {
	if key == "" {
		key = "unknown"
	}
	if m[key] == nil {
		m[key] = make(map[string]float64)
	}
	m[key][day] += value
	if m[key][day] == 0 {
		delete(m[key], day)
	}
}
func finalizeDaily(in map[string]map[string]float64) map[string][]core.TimePoint {
	out := make(map[string][]core.TimePoint, len(in))
	for key, days := range in {
		if len(days) > 0 {
			out[key] = core.SortedTimePoints(days)
		}
	}
	return out
}
func usageEventDay(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func decodeUsagePayload(raw string) map[string]any {
	var out map[string]any
	if json.Unmarshal([]byte(raw), &out) != nil || out == nil {
		return map[string]any{}
	}
	return out
}
func jsonPath(m map[string]any, path ...string) any {
	var cur any = m
	for _, key := range path {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = next[key]
	}
	return cur
}
func jsonString(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }
func jsonNumber(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	case bool:
		if n {
			return 1
		}
		return 0
	default:
		return 0
	}
}
func usageClient(e usageViewEvent, p map[string]any) string {
	client := firstNonEmpty(jsonString(p["client"]), jsonString(jsonPath(p, "payload", "client")), jsonString(jsonPath(p, "_normalized", "client")), jsonString(p["cursor_source"]), jsonString(jsonPath(p, "source", "client")))
	if client != "" {
		return client
	}
	if strings.EqualFold(strings.TrimSpace(e.SourceSystem), "codex") {
		return "CLI"
	}
	return firstNonEmpty(strings.TrimSpace(e.SourceSystem), strings.TrimSpace(e.WorkspaceID), "unknown")
}
func usageUpstreamProvider(e usageViewEvent, p map[string]any) string {
	provider := firstNonEmpty(jsonString(jsonPath(p, "_normalized", "upstream_provider")), jsonString(p["upstream_provider"]), jsonString(jsonPath(p, "payload", "_normalized", "upstream_provider")), jsonString(jsonPath(p, "payload", "upstream_provider")))
	if provider != "" {
		return provider
	}
	provider = strings.TrimSpace(e.ProviderID)
	if provider == "" {
		return "unknown"
	}
	return provider
}
func usageFilePath(p map[string]any) string {
	return firstNonEmpty(jsonString(p["file"]), jsonString(jsonPath(p, "payload", "file")), jsonString(jsonPath(p, "tool_input", "file_path")), jsonString(jsonPath(p, "tool_input", "path")), jsonString(jsonPath(p, "tool_response", "file", "filePath")), jsonString(p["file_extension"]))
}
func isMutationTool(name string) bool {
	n := strings.ToLower(name)
	for _, part := range []string{"edit", "write", "create", "delete", "rename", "move"} {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}
