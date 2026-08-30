package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestDecodeStoredLimitSnapshot_RejectsEmptyPayload(t *testing.T) {
	_, ok := decodeStoredLimitSnapshot("antigravity", "antigravity-mohammed", "{}", time.Now().UTC().Format(time.RFC3339Nano))
	if ok {
		t.Fatal("expected empty {} payload to be rejected")
	}
}

func TestQuotaSnapshotIngest_RepairsEmptyDedupedPayload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ingestor := NewQuotaSnapshotIngestor(store)
	ts := time.Date(2026, 8, 29, 17, 13, 13, 0, time.UTC)

	// Simulate the historical bug: first ingest stores an empty payload under the
	// same turn/dedup key as later real polls.
	empty := map[string]core.UsageSnapshot{
		"antigravity-mohammed": {
			ProviderID: "antigravity",
			AccountID:  "antigravity-mohammed",
			Timestamp:  ts,
			Status:     core.StatusOK,
			Metrics:    map[string]core.Metric{},
		},
	}
	if err := ingestor.Ingest(context.Background(), empty); err != nil {
		t.Fatalf("empty ingest: %v", err)
	}

	// Force the stored payload to {} the way the broken rows look in production.
	if _, err := store.db.Exec(`UPDATE usage_raw_events SET source_payload='{}', source_payload_hash='00'`); err != nil {
		t.Fatalf("force empty payload: %v", err)
	}

	rem := 96.0
	full := map[string]core.UsageSnapshot{
		"antigravity-mohammed": {
			ProviderID: "antigravity",
			AccountID:  "antigravity-mohammed",
			Timestamp:  ts,
			Status:     core.StatusOK,
			Metrics: map[string]core.Metric{
				"quota_gemini_weekly": {Remaining: &rem, Unit: "%", Window: "7d"},
			},
		},
	}
	if err := ingestor.Ingest(context.Background(), full); err != nil {
		t.Fatalf("repair ingest: %v", err)
	}

	templates := map[string]core.UsageSnapshot{
		"antigravity-mohammed": {
			ProviderID: "antigravity",
			AccountID:  "antigravity-mohammed",
			Status:     core.StatusUnknown,
		},
	}
	got, err := ApplyCanonicalTelemetryViewWithOptions(context.Background(), dbPath, templates, ReadModelOptions{})
	if err != nil {
		t.Fatalf("read model: %v", err)
	}
	snap := got["antigravity-mohammed"]
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %q, want OK after payload repair", snap.Status)
	}
	if _, ok := snap.Metrics["quota_gemini_weekly"]; !ok {
		t.Fatalf("expected quota metrics after payload repair, got %#v", snap.Metrics)
	}
}
