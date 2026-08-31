package telemetry

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// totalChanges reports SQLite's cumulative row-modification counter for the
// connection. On a dedup hit the enrich UPDATE is the only statement that can
// move it, which makes it a precise probe for "did we write anything?".
func totalChanges(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(`SELECT total_changes()`).Scan(&n); err != nil {
		t.Fatalf("total_changes(): %v", err)
	}
	return n
}

func usageEventRequest() IngestRequest {
	input := int64(120)
	output := int64(40)
	total := int64(160)
	return IngestRequest{
		SourceSystem:  SourceSystem("claude_code"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC),
		ProviderID:    "claude_code",
		AccountID:     "claude-code",
		AgentName:     "claude_code",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		ModelRaw:      "claude-opus-4-6",
		TokenUsage: core.TokenUsage{
			InputTokens:  &input,
			OutputTokens: &output,
			TotalTokens:  &total,
			Requests:     int64Ptr(1),
		},
	}
}

// Local-file sources re-import their recent history every collection cycle, so
// the same event is ingested over and over. Rewriting twenty columns for a
// merge that changes nothing dirtied the row page and every index covering it
// on every cycle, and all of it landed in the WAL — the write amplification
// reported in #318. A no-op merge must not write.
func TestIngest_DuplicateWithIdenticalFieldsPerformsNoWrite(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	req := usageEventRequest()

	first, err := store.Ingest(ctx, req)
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	if first.Deduped {
		t.Fatal("first ingest reported Deduped = true")
	}

	before := totalChanges(t, store.db)

	for i := 0; i < 3; i++ {
		again, err := store.Ingest(ctx, req)
		if err != nil {
			t.Fatalf("re-Ingest %d: %v", i, err)
		}
		if !again.Deduped {
			t.Fatalf("re-Ingest %d reported Deduped = false", i)
		}
		if again.EventID != first.EventID {
			t.Fatalf("re-Ingest %d event id = %q, want %q", i, again.EventID, first.EventID)
		}
	}

	if after := totalChanges(t, store.db); after != before {
		t.Errorf("re-ingesting an identical event wrote %d row change(s), want 0", after-before)
	}
}

// The skip must not swallow a genuine enrichment: a later event carrying a
// field the stored row lacks still has to be merged in.
func TestIngest_DuplicateWithNewFieldStillWrites(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	sparse := usageEventRequest()
	sparse.ModelRaw = ""
	sparse.CostUSD = nil
	if _, err := store.Ingest(ctx, sparse); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	before := totalChanges(t, store.db)

	enriched := usageEventRequest()
	cost := 0.42
	enriched.CostUSD = &cost
	res, err := store.Ingest(ctx, enriched)
	if err != nil {
		t.Fatalf("enriching Ingest: %v", err)
	}
	if !res.Deduped {
		t.Fatal("enriching ingest reported Deduped = false")
	}
	if after := totalChanges(t, store.db); after == before {
		t.Fatal("enriching ingest wrote nothing; the merge was skipped")
	}

	var (
		model sql.NullString
		got   sql.NullFloat64
	)
	if err := store.db.QueryRow(
		`SELECT model_raw, cost_usd FROM usage_events WHERE event_id = ?`, res.EventID,
	).Scan(&model, &got); err != nil {
		t.Fatalf("read back event: %v", err)
	}
	if model.String != "claude-opus-4-6" {
		t.Errorf("model_raw = %q, want %q", model.String, "claude-opus-4-6")
	}
	if !got.Valid || got.Float64 != cost {
		t.Errorf("cost_usd = %#v, want %v", got, cost)
	}

	// And a second pass over the now-complete row writes nothing again.
	settled := totalChanges(t, store.db)
	if _, err := store.Ingest(ctx, enriched); err != nil {
		t.Fatalf("settled Ingest: %v", err)
	}
	if after := totalChanges(t, store.db); after != settled {
		t.Errorf("re-ingest after enrichment wrote %d row change(s), want 0", after-settled)
	}
}

func TestCanonicalEventDiffers(t *testing.T) {
	base := storedCanonicalEvent{
		EventID:     "e1",
		ProviderID:  sql.NullString{String: "codex", Valid: true},
		InputTokens: sql.NullInt64{Int64: 10, Valid: true},
		CostUSD:     sql.NullFloat64{Float64: 1.5, Valid: true},
		Status:      "ok",
	}
	matching := canonicalEventFields{
		providerID:  "codex",
		inputTokens: int64Ptr(10),
		costUSD:     float64Ptr(1.5),
		status:      EventStatus("ok"),
	}

	tests := []struct {
		name   string
		mutate func(*canonicalEventFields)
		want   bool
	}{
		{name: "identical merge", mutate: func(*canonicalEventFields) {}, want: false},
		{
			name:   "blank string matches a NULL column",
			mutate: func(f *canonicalEventFields) { f.sessionID = "   " },
			want:   false,
		},
		{
			name:   "nil pointer matches a NULL column",
			mutate: func(f *canonicalEventFields) { f.requests = nil },
			want:   false,
		},
		{
			name:   "changed string",
			mutate: func(f *canonicalEventFields) { f.providerID = "claude_code" },
			want:   true,
		},
		{
			name:   "string cleared to NULL",
			mutate: func(f *canonicalEventFields) { f.providerID = "" },
			want:   true,
		},
		{
			name:   "new value for a NULL column",
			mutate: func(f *canonicalEventFields) { f.sessionID = "sess-9" },
			want:   true,
		},
		{
			name:   "changed int",
			mutate: func(f *canonicalEventFields) { f.inputTokens = int64Ptr(11) },
			want:   true,
		},
		{
			name:   "int cleared to NULL",
			mutate: func(f *canonicalEventFields) { f.inputTokens = nil },
			want:   true,
		},
		{
			name:   "changed float",
			mutate: func(f *canonicalEventFields) { f.costUSD = float64Ptr(1.6) },
			want:   true,
		},
		{
			name:   "changed status",
			mutate: func(f *canonicalEventFields) { f.status = EventStatus("error") },
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := matching
			tt.mutate(&merged)
			if got := canonicalEventDiffers(base, merged); got != tt.want {
				t.Fatalf("canonicalEventDiffers() = %v, want %v", got, tt.want)
			}
		})
	}
}
