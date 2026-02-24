package telemetry

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestBuildLimitSnapshotRequests(t *testing.T) {
	creditLimit := 10.0
	creditRemaining := 2.08
	snaps := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Timestamp:  time.Date(2026, 2, 22, 14, 0, 0, 0, time.UTC),
			Status:     core.StatusOK,
			Message:    "ok",
			Metrics: map[string]core.Metric{
				"credit_balance": {
					Limit:     &creditLimit,
					Remaining: &creditRemaining,
					Unit:      "USD",
					Window:    "month",
				},
			},
		},
	}

	reqs := BuildLimitSnapshotRequests(snaps)
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	req := reqs[0]
	if req.SourceSystem != SourceSystemPoller {
		t.Fatalf("source_system = %q, want %q", req.SourceSystem, SourceSystemPoller)
	}
	if req.EventType != EventTypeLimitSnapshot {
		t.Fatalf("event_type = %q, want %q", req.EventType, EventTypeLimitSnapshot)
	}
	if req.ProviderID != "openrouter" {
		t.Fatalf("provider_id = %q, want openrouter", req.ProviderID)
	}
	if req.AccountID != "openrouter" {
		t.Fatalf("account_id = %q, want openrouter", req.AccountID)
	}
	if req.TurnID == "" {
		t.Fatalf("turn_id should be set")
	}
}

func TestQuotaSnapshotIngestor_DedupsBySnapshotTurnID(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ingestor := NewQuotaSnapshotIngestor(store)
	remaining := 79.2
	limit := 100.0
	snaps := map[string]core.UsageSnapshot{
		"codex-cli": {
			ProviderID: "codex",
			AccountID:  "codex-cli",
			Timestamp:  time.Date(2026, 2, 22, 14, 20, 30, 0, time.UTC),
			Status:     core.StatusOK,
			Metrics: map[string]core.Metric{
				"usage_percent_remaining": {
					Limit:     &limit,
					Remaining: &remaining,
					Unit:      "percent",
					Window:    "1d",
				},
			},
		},
	}

	if err := ingestor.Ingest(context.Background(), snaps); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if err := ingestor.Ingest(context.Background(), snaps); err != nil {
		t.Fatalf("second ingest: %v", err)
	}

	db := store.db
	var (
		rawCount       int
		canonicalCount int
		eventType      string
		sourceSystem   string
	)
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_raw_events`).Scan(&rawCount); err != nil {
		t.Fatalf("count usage_raw_events: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&canonicalCount); err != nil {
		t.Fatalf("count usage_events: %v", err)
	}
	if rawCount != 1 {
		t.Fatalf("raw count = %d, want 1", rawCount)
	}
	if canonicalCount != 1 {
		t.Fatalf("canonical count = %d, want 1", canonicalCount)
	}

	if err := db.QueryRow(`
		SELECT e.event_type, r.source_system
		FROM usage_events e
		JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
		LIMIT 1
	`).Scan(&eventType, &sourceSystem); err != nil {
		t.Fatalf("query event type/source: %v", err)
	}
	if eventType != string(EventTypeLimitSnapshot) {
		t.Fatalf("event_type = %q, want %q", eventType, EventTypeLimitSnapshot)
	}
	if sourceSystem != string(SourceSystemPoller) {
		t.Fatalf("source_system = %q, want %q", sourceSystem, SourceSystemPoller)
	}
}

func TestQuotaSnapshotIngestor_StoresMetricPayload(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ingestor := NewQuotaSnapshotIngestor(store)
	used := 5.5
	snaps := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Timestamp:  time.Date(2026, 2, 22, 14, 25, 0, 0, time.UTC),
			Status:     core.StatusNearLimit,
			Metrics: map[string]core.Metric{
				"credit_used": {
					Used:   &used,
					Unit:   "USD",
					Window: "month",
				},
			},
		},
	}
	if err := ingestor.Ingest(context.Background(), snaps); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	db := store.db
	var extracted sql.NullFloat64
	if err := db.QueryRow(`
		SELECT json_extract(r.source_payload, '$.snapshot.metrics.credit_used.used')
		FROM usage_events e
		JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
		WHERE e.event_type = ?
		LIMIT 1
	`, string(EventTypeLimitSnapshot)).Scan(&extracted); err != nil {
		t.Fatalf("query extracted metric payload: %v", err)
	}
	if !extracted.Valid || extracted.Float64 != 5.5 {
		t.Fatalf("extracted credit_used.used = %#v, want 5.5", extracted)
	}
}
