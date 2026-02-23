package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreCompactUsage_RemovesDuplicateCanonicalRows(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	dupAt := time.Date(2026, 2, 23, 1, 0, 0, 0, time.UTC)

	_, err = store.Ingest(ctx, IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelSQLite,
		OccurredAt:    dupAt,
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		EventType:     EventTypeMessageUsage,
		ModelRaw:      "qwen/qwen3-coder-flash",
		InputTokens:   int64Ptr(100),
		TotalTokens:   int64Ptr(100),
		CostUSD:       float64Ptr(0.01),
	})
	if err != nil {
		t.Fatalf("ingest primary canonical row: %v", err)
	}

	// Legacy duplicate escaped dedup in older builds (same logical event, lower data quality).
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO usage_raw_events (
			raw_event_id, ingested_at, source_system, source_channel, source_schema_version,
			source_payload, source_payload_hash, workspace_id, agent_session_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"raw-dup",
		dupAt.Add(time.Second).Format(time.RFC3339Nano),
		"opencode",
		"sqlite",
		"v1",
		"{}",
		"dup-hash",
		nil,
		"sess-1",
	)
	if err != nil {
		t.Fatalf("insert duplicate raw row: %v", err)
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO usage_events (
			event_id, occurred_at, provider_id, agent_name, account_id, workspace_id, session_id,
			turn_id, message_id, tool_call_id, event_type, model_raw, model_canonical,
			model_lineage_id, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens,
			cache_write_tokens, total_tokens, cost_usd, requests, tool_name, status, dedup_key,
			raw_event_id, normalization_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"event-dup",
		dupAt.Format(time.RFC3339Nano),
		"opencode",
		"opencode",
		"zen",
		nil,
		"sess-1",
		nil,
		"msg-1",
		nil,
		"message_usage",
		"qwen/qwen3-coder-flash",
		nil,
		nil,
		0,
		0,
		0,
		0,
		0,
		0,
		0.0,
		1,
		nil,
		"ok",
		"legacy-dup-key",
		"raw-dup",
		"v1",
	)
	if err != nil {
		t.Fatalf("insert duplicate canonical row: %v", err)
	}

	// Explicit orphan raw row should be removed by compaction.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO usage_raw_events (
			raw_event_id, ingested_at, source_system, source_channel, source_schema_version,
			source_payload, source_payload_hash, workspace_id, agent_session_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"raw-orphan",
		dupAt.Add(2*time.Second).Format(time.RFC3339Nano),
		"opencode",
		"sqlite",
		"v1",
		"{}",
		"orphan-hash",
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("insert orphan raw row: %v", err)
	}

	result, err := store.CompactUsage(ctx)
	if err != nil {
		t.Fatalf("compact usage: %v", err)
	}
	if result.DuplicateEventsRemoved < 1 {
		t.Fatalf("duplicate_events_removed = %d, want >= 1", result.DuplicateEventsRemoved)
	}
	if result.OrphanRawEventsRemoved < 1 {
		t.Fatalf("orphan_raw_events_removed = %d, want >= 1", result.OrphanRawEventsRemoved)
	}

	var remaining int
	if err := store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM usage_events
		WHERE event_type = 'message_usage' AND session_id = ? AND message_id = ?
	`, "sess-1", "msg-1").Scan(&remaining); err != nil {
		t.Fatalf("count remaining usage events: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("remaining usage events = %d, want 1", remaining)
	}

	var providerID string
	if err := store.db.QueryRowContext(ctx, `
		SELECT provider_id
		FROM usage_events
		WHERE event_type = 'message_usage' AND session_id = ? AND message_id = ?
	`, "sess-1", "msg-1").Scan(&providerID); err != nil {
		t.Fatalf("select remaining provider_id: %v", err)
	}
	if providerID != "openrouter" {
		t.Fatalf("remaining provider_id = %q, want openrouter", providerID)
	}
}
