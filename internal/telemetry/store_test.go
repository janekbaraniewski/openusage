package telemetry

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestStoreInit_CreatesTables(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tables := []string{"usage_raw_events", "usage_events", "usage_reconciliation_windows"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

func TestStoreIngest_IdempotentByDedupKey(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	store.now = func() time.Time {
		return time.Date(2026, time.February, 22, 13, 30, 0, 0, time.UTC)
	}
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	input := int64(120)
	output := int64(30)
	cost := 0.015
	payload := map[string]any{"kind": "notify", "ok": true}

	req := IngestRequest{
		SourceSystem:        SourceSystem("codex"),
		SourceChannel:       SourceChannelHook,
		SourceSchemaVersion: "v1",
		OccurredAt:          time.Date(2026, time.February, 22, 13, 29, 59, 0, time.UTC),
		ProviderID:          "openai",
		AccountID:           "codex-main",
		SessionID:           "sess-1",
		TurnID:              "turn-1",
		MessageID:           "msg-1",
		EventType:           EventTypeMessageUsage,
		ModelRaw:            "gpt-5-codex",
		InputTokens:         &input,
		OutputTokens:        &output,
		CostUSD:             &cost,
		Payload:             payload,
	}

	first, err := store.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.Deduped {
		t.Fatal("first ingest should not be deduped")
	}

	second, err := store.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if !second.Deduped {
		t.Fatal("second ingest should be deduped")
	}
	if second.EventID != first.EventID {
		t.Fatalf("deduped event id = %s, want %s", second.EventID, first.EventID)
	}

	var rawCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_raw_events`).Scan(&rawCount); err != nil {
		t.Fatalf("count raw rows: %v", err)
	}
	if rawCount != 1 {
		t.Fatalf("raw row count = %d, want 1", rawCount)
	}

	var canonicalCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&canonicalCount); err != nil {
		t.Fatalf("count canonical rows: %v", err)
	}
	if canonicalCount != 1 {
		t.Fatalf("canonical row count = %d, want 1", canonicalCount)
	}

	var totalTokens int64
	if err := db.QueryRow(`SELECT total_tokens FROM usage_events WHERE event_id = ?`, first.EventID).Scan(&totalTokens); err != nil {
		t.Fatalf("read total_tokens: %v", err)
	}
	if totalTokens != 150 {
		t.Fatalf("total_tokens = %d, want 150", totalTokens)
	}
}

func TestStoreIngest_DedupEnrichesMissingFields(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	firstReq := IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, time.February, 22, 13, 0, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "opencode",
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		EventType:     EventTypeMessageUsage,
	}
	first, err := store.Ingest(context.Background(), firstReq)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.Deduped {
		t.Fatalf("first ingest unexpectedly deduped")
	}

	in := int64(120)
	out := int64(40)
	total := int64(160)
	secondReq := IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    time.Date(2026, time.February, 22, 13, 0, 1, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "opencode",
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		EventType:     EventTypeMessageUsage,
		ModelRaw:      "qwen/qwen3-coder-flash",
		InputTokens:   &in,
		OutputTokens:  &out,
		TotalTokens:   &total,
	}
	second, err := store.Ingest(context.Background(), secondReq)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if !second.Deduped {
		t.Fatalf("second ingest should be deduped")
	}
	if second.EventID != first.EventID {
		t.Fatalf("deduped event id = %s, want %s", second.EventID, first.EventID)
	}

	var (
		modelRaw    sql.NullString
		inputTokens sql.NullInt64
		totalTokens sql.NullInt64
	)
	if err := db.QueryRow(
		`SELECT model_raw, input_tokens, total_tokens FROM usage_events WHERE event_id = ?`,
		first.EventID,
	).Scan(&modelRaw, &inputTokens, &totalTokens); err != nil {
		t.Fatalf("query enriched canonical row: %v", err)
	}
	if !modelRaw.Valid || modelRaw.String != "qwen/qwen3-coder-flash" {
		t.Fatalf("model_raw = %#v, want qwen/qwen3-coder-flash", modelRaw)
	}
	if !inputTokens.Valid || inputTokens.Int64 != 120 {
		t.Fatalf("input_tokens = %#v, want 120", inputTokens)
	}
	if !totalTokens.Valid || totalTokens.Int64 != 160 {
		t.Fatalf("total_tokens = %#v, want 160", totalTokens)
	}
}

func TestStoreIngest_DedupHookOverridesLowerPriorityAttribution(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	firstIn := int64(120)
	firstOut := int64(40)
	firstTotal := int64(160)
	firstReq := IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelSQLite,
		OccurredAt:    time.Date(2026, time.February, 22, 13, 0, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		EventType:     EventTypeMessageUsage,
		ModelRaw:      "anthropic/claude-sonnet-4.5",
		InputTokens:   &firstIn,
		OutputTokens:  &firstOut,
		TotalTokens:   &firstTotal,
	}
	if _, err := store.Ingest(context.Background(), firstReq); err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	secondIn := int64(100)
	secondOut := int64(30)
	secondTotal := int64(130)
	secondReq := IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, time.February, 22, 13, 0, 1, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		EventType:     EventTypeMessageUsage,
		ModelRaw:      "qwen/qwen3-coder-flash",
		InputTokens:   &secondIn,
		OutputTokens:  &secondOut,
		TotalTokens:   &secondTotal,
	}
	second, err := store.Ingest(context.Background(), secondReq)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if !second.Deduped {
		t.Fatalf("second ingest should be deduped")
	}

	var (
		modelRaw    sql.NullString
		inputTokens sql.NullInt64
		totalTokens sql.NullInt64
	)
	if err := db.QueryRow(
		`SELECT model_raw, input_tokens, total_tokens FROM usage_events WHERE dedup_key = ?`,
		BuildDedupKey(firstReq),
	).Scan(&modelRaw, &inputTokens, &totalTokens); err != nil {
		t.Fatalf("query canonical row: %v", err)
	}
	if !modelRaw.Valid || modelRaw.String != "qwen/qwen3-coder-flash" {
		t.Fatalf("model_raw = %#v, want qwen/qwen3-coder-flash", modelRaw)
	}
	if !inputTokens.Valid || inputTokens.Int64 != 100 {
		t.Fatalf("input_tokens = %#v, want 100", inputTokens)
	}
	if !totalTokens.Valid || totalTokens.Int64 != 130 {
		t.Fatalf("total_tokens = %#v, want 130", totalTokens)
	}
}

func TestStoreIngest_DedupStableIDIgnoresAccountProviderAgentDrift(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	in := int64(100)
	out := int64(50)
	total := int64(150)
	firstReq := IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelSQLite,
		OccurredAt:    time.Date(2026, time.February, 22, 13, 0, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "zen",
		AgentName:     "build",
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		EventType:     EventTypeMessageUsage,
		InputTokens:   &in,
		OutputTokens:  &out,
		TotalTokens:   &total,
	}
	first, err := store.Ingest(context.Background(), firstReq)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.Deduped {
		t.Fatalf("first ingest unexpectedly deduped")
	}

	secondReq := firstReq
	secondReq.SourceChannel = SourceChannelHook
	secondReq.ProviderID = "anthropic"
	secondReq.AccountID = "openrouter"
	secondReq.AgentName = "opencode"
	secondReq.ModelRaw = "qwen/qwen3-coder-flash"

	second, err := store.Ingest(context.Background(), secondReq)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if !second.Deduped {
		t.Fatalf("second ingest should be deduped")
	}
	if second.EventID != first.EventID {
		t.Fatalf("deduped event id = %s, want %s", second.EventID, first.EventID)
	}

	var canonicalCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&canonicalCount); err != nil {
		t.Fatalf("count canonical rows: %v", err)
	}
	if canonicalCount != 1 {
		t.Fatalf("canonical row count = %d, want 1", canonicalCount)
	}
}

func TestStorePruneOldEvents_DeletesExpiredEventsOnly(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	now := time.Now().UTC()
	recentTS := now.Add(-5 * 24 * time.Hour).Format(time.RFC3339Nano)
	oldTS := now.Add(-60 * 24 * time.Hour).Format(time.RFC3339Nano)
	ingestedAt := now.Format(time.RFC3339Nano)

	// Insert raw events (required by foreign key).
	for _, rawID := range []string{"raw-recent-1", "raw-recent-2", "raw-old-1", "raw-old-2"} {
		if _, err := db.Exec(
			`INSERT INTO usage_raw_events (
				raw_event_id, ingested_at, source_system, source_channel, source_schema_version,
				source_payload, source_payload_hash, workspace_id, agent_session_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rawID, ingestedAt, "test", "hook", "v1", `{"x":1}`, "hash-"+rawID, "", "",
		); err != nil {
			t.Fatalf("insert raw event %s: %v", rawID, err)
		}
	}

	// Insert usage_events: 2 recent (5 days old), 2 old (60 days old).
	type eventRow struct {
		eventID    string
		occurredAt string
		rawEventID string
		dedupKey   string
	}
	events := []eventRow{
		{"evt-recent-1", recentTS, "raw-recent-1", "dedup-recent-1"},
		{"evt-recent-2", recentTS, "raw-recent-2", "dedup-recent-2"},
		{"evt-old-1", oldTS, "raw-old-1", "dedup-old-1"},
		{"evt-old-2", oldTS, "raw-old-2", "dedup-old-2"},
	}
	for _, e := range events {
		if _, err := db.Exec(
			`INSERT INTO usage_events (
				event_id, occurred_at, agent_name, event_type, status, dedup_key,
				raw_event_id, normalization_version
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			e.eventID, e.occurredAt, "test-agent", "message_usage", "ok", e.dedupKey,
			e.rawEventID, "v1",
		); err != nil {
			t.Fatalf("insert event %s: %v", e.eventID, err)
		}
	}

	// Prune with 30-day retention: should delete the 2 old events.
	deleted, err := store.PruneOldEvents(context.Background(), 30)
	if err != nil {
		t.Fatalf("PruneOldEvents: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}

	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining events: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("remaining events = %d, want 2", remaining)
	}

	// Orphan raw events should now be prunable.
	orphaned, err := store.PruneOrphanRawEvents(context.Background(), 50000)
	if err != nil {
		t.Fatalf("PruneOrphanRawEvents: %v", err)
	}
	if orphaned != 2 {
		t.Fatalf("orphaned raw events removed = %d, want 2", orphaned)
	}

	var rawRemaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_raw_events`).Scan(&rawRemaining); err != nil {
		t.Fatalf("count remaining raw events: %v", err)
	}
	if rawRemaining != 2 {
		t.Fatalf("remaining raw events = %d, want 2", rawRemaining)
	}

	// Edge case: retentionDays <= 0 should be a no-op.
	noOp, err := store.PruneOldEvents(context.Background(), 0)
	if err != nil {
		t.Fatalf("PruneOldEvents(0): %v", err)
	}
	if noOp != 0 {
		t.Fatalf("PruneOldEvents(0) deleted = %d, want 0", noOp)
	}
}

func TestStorePruneOrphanRawEvents_RemovesOnlyUnreferencedRows(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	req := IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Now().UTC(),
		ProviderID:    "openai",
		AccountID:     "codex",
		AgentName:     "codex",
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		EventType:     EventTypeMessageUsage,
		InputTokens:   int64Ptr(12),
		OutputTokens:  int64Ptr(3),
		TotalTokens:   int64Ptr(15),
		Payload:       map[string]any{"ok": true},
	}
	if _, err := store.Ingest(context.Background(), req); err != nil {
		t.Fatalf("ingest canonical event: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO usage_raw_events (
			raw_event_id, ingested_at, source_system, source_channel, source_schema_version,
			source_payload, source_payload_hash, workspace_id, agent_session_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"raw-orphan-1", now, "opencode", "sqlite", "test", `{"x":1}`, "hash-1", "", "",
	); err != nil {
		t.Fatalf("insert orphan raw-1: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO usage_raw_events (
			raw_event_id, ingested_at, source_system, source_channel, source_schema_version,
			source_payload, source_payload_hash, workspace_id, agent_session_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"raw-orphan-2", now, "opencode", "sqlite", "test", `{"x":2}`, "hash-2", "", "",
	); err != nil {
		t.Fatalf("insert orphan raw-2: %v", err)
	}

	removed, err := store.PruneOrphanRawEvents(context.Background(), 1)
	if err != nil {
		t.Fatalf("prune orphan raw events: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	var rawCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_raw_events`).Scan(&rawCount); err != nil {
		t.Fatalf("count raw rows: %v", err)
	}
	if rawCount != 2 {
		t.Fatalf("raw rows after prune = %d, want 2", rawCount)
	}

	removed, err = store.PruneOrphanRawEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("second prune orphan raw events: %v", err)
	}
	if removed != 1 {
		t.Fatalf("second removed = %d, want 1", removed)
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_raw_events`).Scan(&rawCount); err != nil {
		t.Fatalf("count raw rows after second prune: %v", err)
	}
	if rawCount != 1 {
		t.Fatalf("raw rows after second prune = %d, want 1", rawCount)
	}
}
