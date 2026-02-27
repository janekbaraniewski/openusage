package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestIngestHookLocally_IngestsHookPayload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	spoolDir := filepath.Join(t.TempDir(), "spool")
	payload := []byte(`{"hook":"chat.message","timestamp":"2026-02-26T20:00:00Z","input":{"sessionID":"sess-1","agent":"main","messageID":"turn-1","variant":"default","model":{"providerID":"openrouter","modelID":"openai/gpt-oss-20b"}},"output":{"message":{"id":"msg-1","sessionID":"sess-1","role":"assistant"},"route":{"provider_name":"DeepInfra"},"usage":{"input_tokens":12,"output_tokens":4,"total_tokens":16,"cost_usd":0.00012}}}`)

	resp, err := ingestHookLocally(context.Background(), "opencode", "openrouter", payload, dbPath, spoolDir, false)
	if err != nil {
		t.Fatalf("ingest hook locally: %v", err)
	}
	if resp.Ingested == 0 {
		t.Fatalf("ingested = %d, want >0", resp.Ingested)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var hookRawCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_raw_events WHERE source_system='opencode' AND source_channel='hook'`).Scan(&hookRawCount); err != nil {
		t.Fatalf("query hook raw count: %v", err)
	}
	if hookRawCount == 0 {
		t.Fatalf("hook raw rows = %d, want >0", hookRawCount)
	}

	var upstream string
	if err := db.QueryRow(`SELECT COALESCE(json_extract(source_payload, '$._normalized.upstream_provider'), '') FROM usage_raw_events WHERE source_system='opencode' AND source_channel='hook' ORDER BY ingested_at DESC LIMIT 1`).Scan(&upstream); err != nil {
		t.Fatalf("query upstream provider: %v", err)
	}
	if upstream != "deepinfra" {
		t.Fatalf("upstream provider = %q, want deepinfra", upstream)
	}
}

func TestIngestHookLocally_SpoolOnly(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	spoolDir := filepath.Join(t.TempDir(), "spool")
	payload := []byte(`{"hook":"tool.execute.after","timestamp":"2026-02-26T20:00:00Z","input":{"tool":"glob","sessionID":"sess-1","callID":"tool-1"},"output":{"title":"Glob"}}`)

	resp, err := ingestHookLocally(context.Background(), "opencode", "openrouter", payload, dbPath, spoolDir, true)
	if err != nil {
		t.Fatalf("spool-only ingest hook locally: %v", err)
	}
	if resp.Enqueued == 0 {
		t.Fatalf("enqueued = %d, want >0", resp.Enqueued)
	}
	if resp.Ingested != 0 {
		t.Fatalf("ingested = %d, want 0 in spool-only mode", resp.Ingested)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var eventCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&eventCount); err != nil {
		t.Fatalf("query usage events count: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("usage events count = %d, want 0 in spool-only mode", eventCount)
	}
}
