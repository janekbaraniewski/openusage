package telemetry

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestPipeline_EnqueueAndFlush(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}

	spool := NewSpool(t.TempDir())
	pipeline := NewPipeline(store, spool)

	requests := []IngestRequest{
		{
			SourceSystem:  SourceSystem("codex"),
			SourceChannel: SourceChannelJSONL,
			SessionID:     "sess-1",
			TurnID:        "turn-1",
			EventType:     EventTypeMessageUsage,
			InputTokens:   int64Ptr(10),
			OutputTokens:  int64Ptr(2),
		},
		{
			SourceSystem:  SourceSystem("codex"),
			SourceChannel: SourceChannelJSONL,
			SessionID:     "sess-1",
			TurnID:        "turn-1",
			EventType:     EventTypeMessageUsage,
			InputTokens:   int64Ptr(10),
			OutputTokens:  int64Ptr(2),
		},
	}

	enqueued, err := pipeline.EnqueueRequests(requests)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if enqueued != 2 {
		t.Fatalf("enqueued = %d, want 2", enqueued)
	}

	result, err := pipeline.Flush(context.Background(), 100)
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if result.Processed != 2 {
		t.Fatalf("processed = %d, want 2", result.Processed)
	}
	if result.Ingested != 1 {
		t.Fatalf("ingested = %d, want 1", result.Ingested)
	}
	if result.Deduped != 1 {
		t.Fatalf("deduped = %d, want 1", result.Deduped)
	}

	var rawCount int64
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM usage_raw_events`).Scan(&rawCount); err != nil {
		t.Fatalf("count raw events: %v", err)
	}
	if rawCount != 2 {
		t.Fatalf("raw events = %d, want 2", rawCount)
	}
	var canonicalCount int64
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM usage_events`).Scan(&canonicalCount); err != nil {
		t.Fatalf("count canonical events: %v", err)
	}
	if canonicalCount != 1 {
		t.Fatalf("canonical events = %d, want 1", canonicalCount)
	}
}
