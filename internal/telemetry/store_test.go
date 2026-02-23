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
	if rawCount != 2 {
		t.Fatalf("raw row count = %d, want 2", rawCount)
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
