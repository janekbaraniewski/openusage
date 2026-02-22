package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestApplyCanonicalTelemetryView_HydratesRootAndUsage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quotaIngestor := NewQuotaSnapshotIngestor(store)
	limit := 10.0
	remaining := 2.08
	rootSnaps := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Timestamp:  time.Date(2026, 2, 22, 15, 0, 0, 0, time.UTC),
			Status:     core.StatusNearLimit,
			Metrics: map[string]core.Metric{
				"credit_balance": {
					Limit:     &limit,
					Remaining: &remaining,
					Unit:      "USD",
					Window:    "month",
				},
			},
			Attributes: map[string]string{"tier": "paid"},
		},
	}
	if err := quotaIngestor.Ingest(context.Background(), rootSnaps); err != nil {
		t.Fatalf("ingest quota snapshot: %v", err)
	}

	input := int64(120)
	output := int64(40)
	total := int64(160)
	cost := 0.012
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 2, 22, 15, 1, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		MessageID:     "msg-1",
		ModelRaw:      "qwen/qwen3-coder-flash",
		InputTokens:   &input,
		OutputTokens:  &output,
		TotalTokens:   &total,
		CostUSD:       &cost,
		Requests:      int64Ptr(1),
	}); err != nil {
		t.Fatalf("ingest usage event: %v", err)
	}

	base := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Status:     core.StatusOK,
			Metrics: map[string]core.Metric{
				"credit_balance": {Used: float64Ptr(999), Unit: "USD", Window: "month"},
			},
		},
	}
	got, err := ApplyCanonicalTelemetryView(context.Background(), dbPath, base)
	if err != nil {
		t.Fatalf("apply canonical telemetry view: %v", err)
	}

	snap := got["openrouter"]
	credit := snap.Metrics["credit_balance"]
	if credit.Remaining == nil || *credit.Remaining != 2.08 {
		t.Fatalf("credit remaining = %+v, want 2.08 from telemetry root", credit.Remaining)
	}
	if snap.Status != core.StatusNearLimit {
		t.Fatalf("status = %q, want %q", snap.Status, core.StatusNearLimit)
	}
	if snap.Attributes["telemetry_root"] != "limit_snapshot" {
		t.Fatalf("telemetry_root = %q, want limit_snapshot", snap.Attributes["telemetry_root"])
	}
	if snap.Attributes["tier"] != "paid" {
		t.Fatalf("tier attribute = %q, want paid", snap.Attributes["tier"])
	}
	modelMetric, ok := snap.Metrics["model_qwen_qwen3_coder_flash_input_tokens"]
	if !ok || modelMetric.Used == nil || *modelMetric.Used != 120 {
		t.Fatalf("missing overlay model metric, got %+v", modelMetric)
	}
	if snap.Attributes["telemetry_overlay"] != "enabled" {
		t.Fatalf("telemetry_overlay = %q, want enabled", snap.Attributes["telemetry_overlay"])
	}
}

func TestApplyCanonicalTelemetryView_UsesBaseWhenNoRootSnapshot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	baseUsed := 5.0
	base := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Status:     core.StatusOK,
			Metrics: map[string]core.Metric{
				"credit_used": {Used: &baseUsed, Unit: "USD", Window: "month"},
			},
		},
	}

	got, err := ApplyCanonicalTelemetryView(context.Background(), dbPath, base)
	if err != nil {
		t.Fatalf("apply canonical telemetry view: %v", err)
	}
	snap := got["openrouter"]
	if metric := snap.Metrics["credit_used"]; metric.Used == nil || *metric.Used != 5 {
		t.Fatalf("credit_used changed unexpectedly: %+v", metric)
	}
}

func TestApplyCanonicalTelemetryView_AddsTelemetryOnlyProviderSnapshot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	input := int64(33)
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 2, 22, 15, 10, 0, 0, time.UTC),
		ProviderID:    "anthropic",
		AccountID:     "zen",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		MessageID:     "msg-telemetry-only-1",
		ModelRaw:      "claude-opus-4-6",
		InputTokens:   &input,
		TotalTokens:   &input,
		Requests:      int64Ptr(1),
	}); err != nil {
		t.Fatalf("ingest telemetry-only usage: %v", err)
	}

	base := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Status:     core.StatusOK,
			Metrics:    map[string]core.Metric{},
		},
	}

	got, err := ApplyCanonicalTelemetryView(context.Background(), dbPath, base)
	if err != nil {
		t.Fatalf("apply canonical telemetry view: %v", err)
	}

	telemetrySnap, ok := got["telemetry:anthropic"]
	if !ok {
		t.Fatalf("missing telemetry-only snapshot for anthropic")
	}
	if telemetrySnap.ProviderID != "anthropic" {
		t.Fatalf("provider_id = %q, want anthropic", telemetrySnap.ProviderID)
	}
	if telemetrySnap.Attributes["telemetry_only"] != "true" {
		t.Fatalf("telemetry_only attribute = %q, want true", telemetrySnap.Attributes["telemetry_only"])
	}
	modelMetric, ok := telemetrySnap.Metrics["model_claude_opus_4_6_input_tokens"]
	if !ok || modelMetric.Used == nil || *modelMetric.Used != 33 {
		t.Fatalf("missing overlay model metric in telemetry-only snapshot: %+v", modelMetric)
	}
}
