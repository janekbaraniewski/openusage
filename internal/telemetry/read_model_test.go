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
	got, err := applyCanonicalTelemetryViewForTest(context.Background(), dbPath, base)
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
		t.Fatalf("missing canonical usage model metric, got %+v", modelMetric)
	}
	if snap.Attributes["telemetry_view"] != "canonical" {
		t.Fatalf("telemetry_view = %q, want canonical", snap.Attributes["telemetry_view"])
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

	got, err := applyCanonicalTelemetryViewForTest(context.Background(), dbPath, base)
	if err != nil {
		t.Fatalf("apply canonical telemetry view: %v", err)
	}
	snap := got["openrouter"]
	if metric := snap.Metrics["credit_used"]; metric.Used == nil || *metric.Used != 5 {
		t.Fatalf("credit_used changed unexpectedly: %+v", metric)
	}
}

func TestApplyCanonicalTelemetryView_UsesLatestSnapshotOnlyForRoot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quotaIngestor := NewQuotaSnapshotIngestor(store)

	olderLimit := 3600.0
	olderUsed := 407.0
	olderRemaining := olderLimit - olderUsed
	older := map[string]core.UsageSnapshot{
		"cursor-ide": {
			ProviderID: "cursor",
			AccountID:  "cursor-ide",
			Timestamp:  time.Date(2026, 2, 23, 8, 50, 0, 0, time.UTC),
			Status:     core.StatusOK,
			Metrics: map[string]core.Metric{
				"spend_limit": {
					Limit:     &olderLimit,
					Used:      &olderUsed,
					Remaining: &olderRemaining,
					Unit:      "USD",
					Window:    "billing-cycle",
				},
			},
		},
	}
	if err := quotaIngestor.Ingest(context.Background(), older); err != nil {
		t.Fatalf("ingest older quota snapshot: %v", err)
	}

	totalReq := 59800.0
	latest := map[string]core.UsageSnapshot{
		"cursor-ide": {
			ProviderID: "cursor",
			AccountID:  "cursor-ide",
			Timestamp:  time.Date(2026, 2, 23, 8, 57, 0, 0, time.UTC),
			Status:     core.StatusOK,
			Message:    "Local Cursor IDE usage tracking (API unavailable)",
			Metrics: map[string]core.Metric{
				"total_ai_requests": {Used: &totalReq, Unit: "requests", Window: "all"},
			},
		},
	}
	if err := quotaIngestor.Ingest(context.Background(), latest); err != nil {
		t.Fatalf("ingest latest quota snapshot: %v", err)
	}

	base := map[string]core.UsageSnapshot{
		"cursor-ide": {
			ProviderID: "cursor",
			AccountID:  "cursor-ide",
			Status:     core.StatusOK,
			Metrics:    map[string]core.Metric{},
		},
	}
	got, err := applyCanonicalTelemetryViewForTest(context.Background(), dbPath, base)
	if err != nil {
		t.Fatalf("apply canonical telemetry view: %v", err)
	}

	snap := got["cursor-ide"]
	if metric := snap.Metrics["total_ai_requests"]; metric.Used == nil || *metric.Used != 59800 {
		t.Fatalf("total_ai_requests missing from latest snapshot: %+v", metric)
	}
	if _, ok := snap.Metrics["spend_limit"]; ok {
		t.Fatalf("spend_limit should not be carried forward from older snapshots")
	}
	if gotAttr := snap.Attributes["telemetry_root_usage_fallback"]; gotAttr != "" {
		t.Fatalf("telemetry_root_usage_fallback = %q, want empty", gotAttr)
	}
}

func TestApplyCanonicalTelemetryView_FlagsUnmappedTelemetryProviders(t *testing.T) {
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
		AccountID:     "opencode",
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

	got, err := applyCanonicalTelemetryViewForTest(context.Background(), dbPath, base)
	if err != nil {
		t.Fatalf("apply canonical telemetry view: %v", err)
	}

	openrouterSnap := got["openrouter"]
	if gotDiag := openrouterSnap.Diagnostics["telemetry_unmapped_providers"]; gotDiag != "anthropic" {
		t.Fatalf("telemetry_unmapped_providers = %q, want anthropic", gotDiag)
	}
}

func TestApplyCanonicalTelemetryView_UsesProviderLinksForCanonicalUsage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	input := int64(44)
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 2, 22, 15, 20, 0, 0, time.UTC),
		ProviderID:    "anthropic",
		AccountID:     "claude-code",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		MessageID:     "msg-link-1",
		ModelRaw:      "claude-opus-4-6",
		InputTokens:   &input,
		TotalTokens:   &input,
		Requests:      int64Ptr(1),
	}); err != nil {
		t.Fatalf("ingest linked usage: %v", err)
	}

	base := map[string]core.UsageSnapshot{
		"claude": {
			ProviderID: "claude_code",
			AccountID:  "claude-code",
			Status:     core.StatusOK,
			Metrics:    map[string]core.Metric{},
		},
	}

	got, err := ApplyCanonicalTelemetryViewWithOptions(context.Background(), dbPath, base, ReadModelOptions{
		ProviderLinks: map[string]string{
			"anthropic": "claude_code",
		},
	})
	if err != nil {
		t.Fatalf("apply canonical telemetry view: %v", err)
	}

	snap := got["claude"]
	modelMetric, ok := snap.Metrics["model_claude_opus_4_6_input_tokens"]
	if !ok || modelMetric.Used == nil || *modelMetric.Used != 44 {
		t.Fatalf("missing canonical usage model metric for linked provider: %+v", modelMetric)
	}
	if gotDiag := snap.Diagnostics["telemetry_unmapped_providers"]; gotDiag != "" {
		t.Fatalf("unexpected telemetry_unmapped_providers = %q", gotDiag)
	}
}
