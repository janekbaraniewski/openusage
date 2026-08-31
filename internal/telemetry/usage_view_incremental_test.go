package telemetry

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestUsageViewIncrementalState_AppliesInsertAndWinnerReplacement(t *testing.T) {
	globalUsageViewCache.reset()
	_, db, store := openUsageViewRawTestStore(t)
	ctx := context.Background()
	filter := usageFilter{ProviderIDs: []string{"codex"}}
	namespace := usageViewCacheNamespace("incremental-test")

	mustIngestUsageEvent(t, store, IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		ProviderID:    "codex",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "session-incremental",
		TurnID:        "turn-1",
		ModelRaw:      "gpt-5",
		TokenUsage: core.TokenUsage{
			InputTokens: int64Ptr(10),
			TotalTokens: int64Ptr(10),
			Requests:    int64Ptr(1),
		},
	}, "ingest initial winner")

	first, err := loadUsageViewForFilter(ctx, db, namespace, filter)
	if err != nil {
		t.Fatalf("cold usage view: %v", err)
	}
	if first.EventCount != 1 || first.Activity.InputTokens != 10 {
		t.Fatalf("unexpected cold aggregate: %+v", first.Activity)
	}
	entry, ok := globalUsageViewCache.lookup(usageViewCacheKey(namespace, filter))
	if !ok || entry.state == nil {
		t.Fatal("expected cold build to retain incremental winner state")
	}
	state := entry.state

	// Same Codex logical turn from a higher-priority source must replace the
	// JSONL winner instead of increasing the logical event count.
	mustIngestUsageEvent(t, store, IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 7, 17, 12, 0, 1, 0, time.UTC),
		ProviderID:    "codex",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "session-incremental",
		TurnID:        "turn-1",
		MessageID:     "hook-message-turn-1",
		ModelRaw:      "gpt-5",
		TokenUsage: core.TokenUsage{
			InputTokens: int64Ptr(25),
			TotalTokens: int64Ptr(25),
			Requests:    int64Ptr(1),
		},
	}, "ingest replacement winner")

	second, err := loadUsageViewForFilter(ctx, db, namespace, filter)
	if err != nil {
		t.Fatalf("incremental replacement: %v", err)
	}
	if second.EventCount != 1 || second.Activity.InputTokens != 25 {
		t.Fatalf("replacement was not applied incrementally: count=%d activity=%+v", second.EventCount, second.Activity)
	}
	entry, ok = globalUsageViewCache.lookup(usageViewCacheKey(namespace, filter))
	if !ok || entry.state != state {
		t.Fatal("incremental refresh rebuilt winner state")
	}
	if entry.state.appliedChanges == 0 {
		t.Fatal("expected the cached state to consume change-log rows")
	}

	mustIngestUsageEvent(t, store, IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    time.Date(2026, 7, 17, 12, 1, 0, 0, time.UTC),
		ProviderID:    "codex",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "session-incremental",
		TurnID:        "turn-2",
		ModelRaw:      "gpt-5",
		TokenUsage: core.TokenUsage{
			InputTokens: int64Ptr(5),
			TotalTokens: int64Ptr(5),
			Requests:    int64Ptr(1),
		},
	}, "ingest new logical event")

	third, err := loadUsageViewForFilter(ctx, db, namespace, filter)
	if err != nil {
		t.Fatalf("incremental insert: %v", err)
	}
	if third.EventCount != 2 || third.Activity.InputTokens != 30 {
		t.Fatalf("new logical event was not applied: count=%d activity=%+v", third.EventCount, third.Activity)
	}
}

func TestUsageViewIncrementalState_MatchesLegacyAggregates(t *testing.T) {
	globalUsageViewCache.reset()
	_, db, store := openUsageViewRawTestStore(t)
	ctx := context.Background()
	filter := usageFilter{
		ProviderIDs: []string{"codex"},
		TodaySince:  time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
	}
	events := []IngestRequest{
		{
			SourceSystem: SourceSystem("codex"), SourceChannel: SourceChannelJSONL,
			OccurredAt: time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC), ProviderID: "codex", AgentName: "codex",
			EventType: EventTypeMessageUsage, SessionID: "session-a", TurnID: "turn-a", WorkspaceID: "/repo/a",
			ModelRaw: "gpt-5", TokenUsage: core.TokenUsage{InputTokens: int64Ptr(10), OutputTokens: int64Ptr(2), CacheReadTokens: int64Ptr(3), TotalTokens: int64Ptr(15), Requests: int64Ptr(1), CostUSD: float64Ptr(0.1)},
			Payload: map[string]any{"client": "Codex CLI", "upstream_provider": "openai", "file": "main.go", "lines_added": 4},
		},
		{
			SourceSystem: SourceSystem("codex"), SourceChannel: SourceChannelHook,
			OccurredAt: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC), ProviderID: "codex", AgentName: "codex",
			EventType: EventTypeMessageUsage, SessionID: "session-b", TurnID: "turn-b", MessageID: "message-b", WorkspaceID: "/repo/b",
			ModelCanonical: "gpt-5.5", TokenUsage: core.TokenUsage{InputTokens: int64Ptr(20), OutputTokens: int64Ptr(5), ReasoningTokens: int64Ptr(2), TotalTokens: int64Ptr(27), Requests: int64Ptr(2), CostUSD: float64Ptr(0.2)},
			Payload: map[string]any{"payload": map[string]any{"client": "Desktop", "upstream_provider": "azure"}, "file_extension": ".ts", "lines_removed": 2},
		},
		{
			SourceSystem: SourceSystem("codex"), SourceChannel: SourceChannelHook,
			OccurredAt: time.Date(2026, 7, 17, 12, 1, 0, 0, time.UTC), ProviderID: "codex", AgentName: "codex",
			EventType: EventTypeToolUsage, SessionID: "session-b", TurnID: "turn-b", ToolCallID: "tool-b", WorkspaceID: "/repo/b",
			ToolName: "mcp__filesystem__write_file", Status: EventStatusOK, TokenUsage: core.TokenUsage{Requests: int64Ptr(1)},
			Payload: map[string]any{"tool_input": map[string]any{"file_path": "src/app.ts"}, "lines_added": 7, "lines_removed": 1},
		},
	}
	for i, event := range events {
		mustIngestUsageEvent(t, store, event, "ingest aggregate fixture "+string(rune('a'+i)))
	}

	legacy := newTelemetryUsageAgg()
	matFilter, cleanup, err := materializeUsageFilter(ctx, db, filter)
	if err != nil {
		t.Fatalf("materialize legacy view: %v", err)
	}
	defer cleanup()
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(occurred_at), ''), COUNT(*) FROM _deduped_tmp`).Scan(&legacy.LastOccurred, &legacy.EventCount); err != nil {
		t.Fatalf("count legacy view: %v", err)
	}
	if err := loadMaterializedUsageAgg(ctx, db, matFilter, legacy); err != nil {
		t.Fatalf("load legacy aggregates: %v", err)
	}

	_, incremental, err := buildIncrementalUsageState(ctx, db, filter)
	if err != nil {
		t.Fatalf("build incremental aggregates: %v", err)
	}
	if !reflect.DeepEqual(incremental, legacy) {
		t.Fatalf("incremental aggregate differs from legacy\nincremental: %#v\nlegacy:      %#v", incremental, legacy)
	}
}

func TestUsageViewIncrementalState_PromotesCandidateAfterWinnerDelete(t *testing.T) {
	globalUsageViewCache.reset()
	_, db, store := openUsageViewRawTestStore(t)
	ctx := context.Background()
	filter := usageFilter{ProviderIDs: []string{"codex"}, WindowKey: core.TimeWindow30d}
	namespace := usageViewCacheNamespace("delete-promotion")
	base := IngestRequest{SourceSystem: "codex", OccurredAt: time.Now().UTC(), ProviderID: "codex", AgentName: "codex", EventType: EventTypeMessageUsage, SessionID: "promotion-session", TurnID: "promotion-turn", ModelRaw: "gpt-5"}
	low := base
	low.SourceChannel = SourceChannelJSONL
	low.TokenUsage = core.TokenUsage{InputTokens: int64Ptr(10), TotalTokens: int64Ptr(10), Requests: int64Ptr(1)}
	mustIngestUsageEvent(t, store, low, "ingest lower-priority candidate")
	high := base
	high.SourceChannel = SourceChannelHook
	high.MessageID = "promotion-hook"
	high.TokenUsage = core.TokenUsage{InputTokens: int64Ptr(20), TotalTokens: int64Ptr(20), Requests: int64Ptr(1)}
	highResult, err := store.Ingest(ctx, high)
	if err != nil {
		t.Fatalf("ingest winner candidate: %v", err)
	}

	before, err := loadUsageViewForFilter(ctx, db, namespace, filter)
	if err != nil || before.Activity.InputTokens != 20 {
		t.Fatalf("unexpected winner before delete: agg=%+v err=%v", before, err)
	}
	if _, err := db.Exec(`DELETE FROM usage_events WHERE event_id = ?`, highResult.EventID); err != nil {
		t.Fatalf("delete winner: %v", err)
	}
	after, err := loadUsageViewForFilter(ctx, db, namespace, filter)
	if err != nil {
		t.Fatalf("refresh after winner delete: %v", err)
	}
	if after.EventCount != 1 || after.Activity.InputTokens != 10 {
		t.Fatalf("lower-priority candidate was not promoted: count=%d input=%v", after.EventCount, after.Activity.InputTokens)
	}
}

func TestUsageViewIncrementalState_AdvancesRollingCutoffWithoutColdBuild(t *testing.T) {
	globalUsageViewCache.reset()
	_, db, store := openUsageViewRawTestStore(t)
	ctx := context.Background()
	namespace := usageViewCacheNamespace("rolling-cutoff")
	baseTime := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	for i, occurred := range []time.Time{baseTime, baseTime.Add(2 * time.Minute)} {
		mustIngestUsageEvent(t, store, IngestRequest{SourceSystem: "codex", SourceChannel: SourceChannelJSONL, OccurredAt: occurred, ProviderID: "codex", AgentName: "codex", EventType: EventTypeMessageUsage, SessionID: "rolling-session", TurnID: string(rune('a' + i)), TokenUsage: core.TokenUsage{InputTokens: int64Ptr(1), TotalTokens: int64Ptr(1), Requests: int64Ptr(1)}}, "ingest rolling fixture")
	}
	firstFilter := usageFilter{ProviderIDs: []string{"codex"}, Since: baseTime.Add(-time.Minute), WindowKey: core.TimeWindow30d}
	first, err := loadUsageViewForFilter(ctx, db, namespace, firstFilter)
	if err != nil || first.EventCount != 2 {
		t.Fatalf("cold rolling view: count=%d err=%v", first.EventCount, err)
	}
	entry, _ := globalUsageViewCache.lookup(usageViewCacheKey(namespace, firstFilter))
	state := entry.state

	secondFilter := firstFilter
	secondFilter.Since = baseTime.Add(time.Minute)
	second, err := loadUsageViewForFilter(ctx, db, namespace, secondFilter)
	if err != nil {
		t.Fatalf("advance rolling cutoff: %v", err)
	}
	if second.EventCount != 1 {
		t.Fatalf("expected expired event to leave window, got count=%d", second.EventCount)
	}
	entry, _ = globalUsageViewCache.lookup(usageViewCacheKey(namespace, secondFilter))
	if entry.state != state {
		t.Fatal("rolling cutoff movement cold-rebuilt the incremental state")
	}
}
