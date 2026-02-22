package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestApplyProviderTelemetryOverlay_MergesTelemetryWithoutReplacingRootMetrics(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	occurredAt := time.Date(2026, 2, 22, 12, 0, 0, 0, time.UTC)
	_, err = store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    occurredAt,
		ProviderID:    "openrouter",
		AccountID:     "zen",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		ModelRaw:      "qwen/qwen3-coder-flash",
		InputTokens:   int64Ptr(120),
		OutputTokens:  int64Ptr(40),
		TotalTokens:   int64Ptr(160),
		CostUSD:       float64Ptr(0.012),
		Requests:      int64Ptr(1),
	})
	if err != nil {
		t.Fatalf("ingest message event: %v", err)
	}

	_, err = store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    occurredAt.Add(1 * time.Second),
		ProviderID:    "openrouter",
		AccountID:     "zen",
		AgentName:     "opencode",
		EventType:     EventTypeToolUsage,
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		ToolCallID:    "tool-1",
		ToolName:      "shell",
		Requests:      int64Ptr(1),
	})
	if err != nil {
		t.Fatalf("ingest tool event: %v", err)
	}

	balance := 7.92
	snaps := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Metrics: map[string]core.Metric{
				"credit_balance": {Used: &balance, Unit: "USD", Window: "month"},
			},
		},
	}

	merged, err := ApplyProviderTelemetryOverlay(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply overlay: %v", err)
	}

	snap := merged["openrouter"]
	root := snap.Metrics["credit_balance"]
	if root.Used == nil || *root.Used != 7.92 {
		t.Fatalf("credit_balance changed unexpectedly: %+v", root)
	}

	if metric, ok := snap.Metrics["model_qwen_qwen3_coder_flash_input_tokens"]; !ok || metric.Used == nil || *metric.Used != 120 {
		t.Fatalf("missing/invalid model input metric: %+v", metric)
	}
	if metric, ok := snap.Metrics["source_opencode_requests"]; !ok || metric.Used == nil || *metric.Used != 1 {
		t.Fatalf("missing/invalid source requests metric: %+v", metric)
	}
	if metric, ok := snap.Metrics["tool_shell"]; !ok || metric.Used == nil || *metric.Used != 1 {
		t.Fatalf("missing/invalid tool metric: %+v", metric)
	}

	if got := snap.Attributes["telemetry_overlay"]; got != "enabled" {
		t.Fatalf("telemetry_overlay attribute = %q, want enabled", got)
	}
}
