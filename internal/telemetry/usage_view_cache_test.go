package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// TestLoadUsageViewCached_ColdStart verifies that a cold cache reports a
// miss but still returns the current fingerprint so the caller can populate
// the cache after building the aggregate.
func TestLoadUsageViewCached_ColdStart(t *testing.T) {
	globalUsageViewCache.reset()
	_, db, store := openUsageViewRawTestStore(t)
	mustIngestUsageEvent(t, store, IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-cold",
		MessageID:     "msg-cold",
		ModelRaw:      "qwen/qwen3-coder-flash",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(10),
			OutputTokens: int64Ptr(5),
			TotalTokens:  int64Ptr(15),
			Requests:     int64Ptr(1),
		},
	}, "ingest cold event")

	filter := usageFilter{ProviderIDs: []string{"openrouter"}}
	agg, maxRowID, count, hit, err := loadUsageViewCached(context.Background(), db, "test-ns", filter)
	if err != nil {
		t.Fatalf("cache lookup: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss on cold start")
	}
	if agg != nil {
		t.Fatal("expected nil agg on miss")
	}
	if count == 0 || maxRowID == 0 {
		t.Fatalf("expected non-zero fingerprint on cold start, got maxRowID=%d count=%d", maxRowID, count)
	}
}

// TestLoadUsageViewCached_HitWhenFingerprintMatches stores an agg then verifies
// a subsequent lookup with unchanged data returns a cache hit.
func TestLoadUsageViewCached_HitWhenFingerprintMatches(t *testing.T) {
	globalUsageViewCache.reset()
	_, db, store := openUsageViewRawTestStore(t)
	mustIngestUsageEvent(t, store, IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-hit",
		MessageID:     "msg-hit",
		ModelRaw:      "qwen/qwen3-coder-flash",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(10),
			OutputTokens: int64Ptr(5),
			TotalTokens:  int64Ptr(15),
			Requests:     int64Ptr(1),
		},
	}, "ingest hit event")

	filter := usageFilter{ProviderIDs: []string{"openrouter"}}
	_, maxRowID, count, _, err := loadUsageViewCached(context.Background(), db, "test-ns", filter)
	if err != nil {
		t.Fatalf("cache lookup (probe): %v", err)
	}
	expectedAgg := &telemetryUsageAgg{EventCount: 1, LastOccurred: "2026-05-17T12:00:00Z"}
	storeUsageViewCache("test-ns", filter, expectedAgg, maxRowID, count)

	agg, _, _, hit, err := loadUsageViewCached(context.Background(), db, "test-ns", filter)
	if err != nil {
		t.Fatalf("cache lookup (hit): %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit when fingerprint unchanged")
	}
	if agg == nil || agg.EventCount != 1 {
		t.Fatalf("expected stored agg to be returned, got %+v", agg)
	}
}

// TestLoadUsageViewCached_InvalidatedOnNewEvent verifies that ingesting a new
// event after caching reports a miss because MAX(rowid) shifts.
func TestLoadUsageViewCached_InvalidatedOnNewEvent(t *testing.T) {
	globalUsageViewCache.reset()
	_, db, store := openUsageViewRawTestStore(t)
	mustIngestUsageEvent(t, store, IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-inv-1",
		MessageID:     "msg-inv-1",
		ModelRaw:      "qwen/qwen3-coder-flash",
		TokenUsage: core.TokenUsage{
			InputTokens: int64Ptr(10),
			TotalTokens: int64Ptr(10),
			Requests:    int64Ptr(1),
		},
	}, "ingest first event")

	filter := usageFilter{ProviderIDs: []string{"openrouter"}}
	_, maxRowID, count, _, err := loadUsageViewCached(context.Background(), db, "test-ns", filter)
	if err != nil {
		t.Fatalf("cache lookup (probe): %v", err)
	}
	storeUsageViewCache("test-ns", filter, &telemetryUsageAgg{EventCount: 1}, maxRowID, count)

	// Ingest a second event — fingerprint changes.
	mustIngestUsageEvent(t, store, IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 5, 17, 12, 1, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-inv-2",
		MessageID:     "msg-inv-2",
		ModelRaw:      "qwen/qwen3-coder-flash",
		TokenUsage: core.TokenUsage{
			InputTokens: int64Ptr(20),
			TotalTokens: int64Ptr(20),
			Requests:    int64Ptr(1),
		},
	}, "ingest second event")

	_, _, _, hit, err := loadUsageViewCached(context.Background(), db, "test-ns", filter)
	if err != nil {
		t.Fatalf("cache lookup after ingest: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss after new event ingest (fingerprint changed)")
	}
}

// TestLoadUsageViewCached_InvalidatedOnPrune verifies that deleting rows (as
// retention pruning does) drops COUNT(*) and invalidates the cached entry.
func TestLoadUsageViewCached_InvalidatedOnPrune(t *testing.T) {
	globalUsageViewCache.reset()
	_, db, store := openUsageViewRawTestStore(t)
	mustIngestUsageEvent(t, store, IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-prune",
		MessageID:     "msg-prune",
		ModelRaw:      "qwen/qwen3-coder-flash",
		TokenUsage: core.TokenUsage{
			InputTokens: int64Ptr(10),
			TotalTokens: int64Ptr(10),
			Requests:    int64Ptr(1),
		},
	}, "ingest prune event")

	filter := usageFilter{ProviderIDs: []string{"openrouter"}}
	_, maxRowID, count, _, err := loadUsageViewCached(context.Background(), db, "test-ns", filter)
	if err != nil {
		t.Fatalf("cache lookup (probe): %v", err)
	}
	storeUsageViewCache("test-ns", filter, &telemetryUsageAgg{EventCount: 1}, maxRowID, count)

	// Simulate prune: delete the row.
	if _, err := db.Exec("DELETE FROM usage_events WHERE provider_id = 'openrouter'"); err != nil {
		t.Fatalf("prune events: %v", err)
	}

	_, _, _, hit, err := loadUsageViewCached(context.Background(), db, "test-ns", filter)
	if err != nil {
		t.Fatalf("cache lookup after prune: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss after prune (count changed)")
	}
}

// TestLoadUsageViewCached_EmptyNamespaceDisablesCache verifies that callers
// that pass an empty namespace (e.g. tests) bypass the cache entirely.
func TestLoadUsageViewCached_EmptyNamespaceDisablesCache(t *testing.T) {
	globalUsageViewCache.reset()
	_, db, store := openUsageViewRawTestStore(t)
	mustIngestUsageEvent(t, store, IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-bypass",
		MessageID:     "msg-bypass",
		ModelRaw:      "qwen/qwen3-coder-flash",
		TokenUsage: core.TokenUsage{
			InputTokens: int64Ptr(10),
			TotalTokens: int64Ptr(10),
			Requests:    int64Ptr(1),
		},
	}, "ingest bypass event")

	filter := usageFilter{ProviderIDs: []string{"openrouter"}}
	storeUsageViewCache("", filter, &telemetryUsageAgg{EventCount: 999}, 1, 1)
	agg, _, _, hit, err := loadUsageViewCached(context.Background(), db, "", filter)
	if err != nil {
		t.Fatalf("cache lookup with empty namespace: %v", err)
	}
	if hit {
		t.Fatal("empty namespace should always miss")
	}
	if agg != nil {
		t.Fatalf("empty namespace should return nil agg, got %+v", agg)
	}
}
