package telemetry

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"

	_ "github.com/mattn/go-sqlite3"
)

func TestApplyCanonicalUsageView_MergesTelemetryWithoutReplacingRootMetrics(t *testing.T) {
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
		AccountID:     "openrouter",
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
		AccountID:     "openrouter",
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

	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}

	snap := merged["openrouter"]
	root := snap.Metrics["credit_balance"]
	if root.Used == nil || *root.Used != 7.92 {
		t.Fatalf("credit_balance changed unexpectedly: %+v", root)
	}

	if metric, ok := snap.Metrics["model_qwen_qwen3_coder_flash_input_tokens"]; !ok || metric.Used == nil || *metric.Used != 120 {
		t.Fatalf("missing/invalid model input metric: %+v", metric)
	}
	if metric, ok := snap.Metrics["client_opencode_requests"]; !ok || metric.Used == nil || *metric.Used != 1 {
		t.Fatalf("missing/invalid client requests metric: %+v", metric)
	}
	if metric, ok := snap.Metrics["tool_shell"]; !ok || metric.Used == nil || *metric.Used != 1 {
		t.Fatalf("missing/invalid tool metric: %+v", metric)
	}

	if got := snap.Attributes["telemetry_view"]; got != "canonical" {
		t.Fatalf("telemetry_view attribute = %q, want canonical", got)
	}
}

func TestApplyCanonicalUsageView_DedupsLegacyCrossAccountDuplicates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}

	occurredAt := time.Date(2026, 2, 22, 12, 0, 0, 0, time.UTC)
	_, err = store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    occurredAt,
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
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
		t.Fatalf("ingest canonical event: %v", err)
	}

	// Simulate pre-fix historical duplicate rows that escaped dedup via older dedup-key rules.
	_, err = db.Exec(`
		INSERT INTO usage_raw_events (
			raw_event_id, ingested_at, source_system, source_channel, source_schema_version,
			source_payload, source_payload_hash, workspace_id, agent_session_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"raw-legacy-dup",
		occurredAt.Add(time.Second).Format(time.RFC3339Nano),
		"opencode",
		"sqlite",
		"v1",
		"{}",
		"legacy-hash",
		nil,
		"sess-1",
	)
	if err != nil {
		t.Fatalf("insert legacy raw row: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO usage_events (
			event_id, occurred_at, provider_id, agent_name, account_id, workspace_id, session_id,
			turn_id, message_id, tool_call_id, event_type, model_raw, model_canonical,
			model_lineage_id, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens,
			cache_write_tokens, total_tokens, cost_usd, requests, tool_name, status, dedup_key,
			raw_event_id, normalization_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"event-legacy-dup",
		occurredAt.Format(time.RFC3339Nano),
		"openrouter",
		"build",
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
		900,
		100,
		0,
		0,
		0,
		1000,
		1.11,
		1,
		nil,
		"ok",
		"legacy-dup-key",
		"raw-legacy-dup",
		"v1",
	)
	if err != nil {
		t.Fatalf("insert legacy canonical row: %v", err)
	}

	snaps := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Metrics:    map[string]core.Metric{},
		},
	}

	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}
	snap := merged["openrouter"]

	inp := snap.Metrics["model_qwen_qwen3_coder_flash_input_tokens"]
	if inp.Used == nil || *inp.Used != 120 {
		t.Fatalf("input_tokens = %+v, want 120 (legacy duplicate must be ignored)", inp)
	}
	req := snap.Metrics["client_opencode_requests"]
	if req.Used == nil || *req.Used != 1 {
		t.Fatalf("client_opencode_requests = %+v, want 1", req)
	}
}

func TestApplyCanonicalUsageView_TelemetryOverridesModelAndDailyAnalytics(t *testing.T) {
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
		AccountID:     "openrouter",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-1",
		MessageID:     "msg-1",
		ModelRaw:      "qwen/qwen3-coder-flash",
		InputTokens:   int64Ptr(120),
		OutputTokens:  int64Ptr(40),
		TotalTokens:   int64Ptr(160),
		CostUSD:       float64Ptr(9.99),
		Requests:      int64Ptr(1),
	})
	if err != nil {
		t.Fatalf("ingest message event: %v", err)
	}

	rootModelCost := 2.50
	rootDailyCost := 0.30
	rootDailyReq := 7.0
	rootDailyTokens := 1500.0

	snaps := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Metrics: map[string]core.Metric{
				"model_qwen_qwen3_coder_flash_cost_usd": {Used: &rootModelCost, Unit: "USD", Window: "30d"},
			},
			DailySeries: map[string][]core.TimePoint{
				"analytics_cost":     {{Date: "2026-02-22", Value: rootDailyCost}},
				"analytics_requests": {{Date: "2026-02-22", Value: rootDailyReq}},
				"analytics_tokens":   {{Date: "2026-02-22", Value: rootDailyTokens}},
			},
		},
	}

	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}

	snap := merged["openrouter"]
	modelCost := snap.Metrics["model_qwen_qwen3_coder_flash_cost_usd"]
	if modelCost.Used == nil || *modelCost.Used != 9.99 {
		t.Fatalf("model cost = %+v, want 9.99", modelCost)
	}
	if got := seriesValueByDate(snap.DailySeries["analytics_cost"], "2026-02-22"); got != 9.99 {
		t.Fatalf("analytics_cost = %v, want 9.99", got)
	}
	if got := seriesValueByDate(snap.DailySeries["analytics_requests"], "2026-02-22"); got != 1 {
		t.Fatalf("analytics_requests = %v, want 1", got)
	}
	if got := seriesValueByDate(snap.DailySeries["analytics_tokens"], "2026-02-22"); got != 160 {
		t.Fatalf("analytics_tokens = %v, want 160", got)
	}
}

func TestApplyCanonicalUsageView_DoesNotFallbackToProviderScopeForAccountView(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	occurredAt := time.Date(2026, 2, 23, 7, 30, 0, 0, time.UTC)
	input := int64(77)
	total := int64(77)
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    occurredAt,
		ProviderID:    "cursor",
		AccountID:     "cursor",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-a",
		MessageID:     "msg-a",
		ModelRaw:      "claude-4.6-opus-high-thinking",
		InputTokens:   &input,
		TotalTokens:   &total,
		Requests:      int64Ptr(1),
	}); err != nil {
		t.Fatalf("ingest usage event: %v", err)
	}

	localReq := 10.0
	snaps := map[string]core.UsageSnapshot{
		"cursor-ide": {
			ProviderID: "cursor",
			AccountID:  "cursor-ide",
			Metrics: map[string]core.Metric{
				"total_ai_requests": {Used: &localReq, Unit: "requests", Window: "all"},
			},
		},
	}

	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}

	snap := merged["cursor-ide"]
	if _, ok := snap.Metrics["client_opencode_requests"]; ok {
		t.Fatalf("unexpected provider-scope fallback metric client_opencode_requests in account-scoped cursor view")
	}
	if got := snap.Attributes["telemetry_view"]; got != "" {
		t.Fatalf("telemetry_view = %q, want empty (no account-scoped canonical usage)", got)
	}
	if metric := snap.Metrics["total_ai_requests"]; metric.Used == nil || *metric.Used != 10 {
		t.Fatalf("total_ai_requests changed unexpectedly: %+v", metric)
	}
}

func TestApplyCanonicalUsageView_ClearsStalePrefixedAttributeAndDiagnosticKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	occurredAt := time.Date(2026, 2, 23, 9, 0, 0, 0, time.UTC)
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    occurredAt,
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
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
	}); err != nil {
		t.Fatalf("ingest message event: %v", err)
	}

	snaps := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Metrics:    map[string]core.Metric{},
			Attributes: map[string]string{
				"provider_alibaba_cost": "999.0",
				"model_qwen_cost_usd":   "999.0",
				"telemetry_view":        "root",
			},
			Diagnostics: map[string]string{
				"provider_openrouter_cost": "888.0",
				"analytics_cost":           "777.0",
			},
		},
	}

	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}
	snap := merged["openrouter"]
	for _, key := range []string{
		"provider_alibaba_cost",
		"model_qwen_cost_usd",
		"provider_openrouter_cost",
		"analytics_cost",
	} {
		if _, ok := snap.Attributes[key]; ok {
			t.Fatalf("stale attribute key still present: %s", key)
		}
		if _, ok := snap.Diagnostics[key]; ok {
			t.Fatalf("stale diagnostic key still present: %s", key)
		}
	}
}

func TestApplyCanonicalUsageView_TelemetryOverwritesNativeBreakdown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	occurredAt := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    occurredAt,
		ProviderID:    "openrouter",
		AccountID:     "openrouter",
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
	}); err != nil {
		t.Fatalf("ingest message event: %v", err)
	}

	modelA := 3.21
	modelB := 1.11
	providerA := 2.22
	providerB := 0.55
	snaps := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Metrics: map[string]core.Metric{
				"model_moonshot_cost_usd":       {Used: &modelA, Unit: "USD"},
				"model_qwen_cost_usd":           {Used: &modelB, Unit: "USD"},
				"provider_alibaba_cost_usd":     {Used: &providerA, Unit: "USD"},
				"provider_deepinfra_cost_usd":   {Used: &providerB, Unit: "USD"},
				"source_opencode_requests":      {Used: float64Ptr(999), Unit: "requests"},
				"credit_balance":                {Used: float64Ptr(8.28), Unit: "USD"},
				"model_qwen_input_tokens":       {Used: float64Ptr(777), Unit: "tokens"},
				"provider_alibaba_input_tokens": {Used: float64Ptr(888), Unit: "tokens"},
			},
			Attributes: map[string]string{
				"provider_legacy_cost": "999",
			},
		},
	}

	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}
	snap := merged["openrouter"]

	// Telemetry always overwrites model/provider breakdown — native values are replaced
	if got := metricUsed(snap.Metrics["model_qwen_qwen3_coder_flash_cost_usd"]); got != 0.012 {
		t.Fatalf("model_qwen_qwen3_coder_flash_cost_usd = %v, want 0.012 from telemetry", got)
	}
	// Native-only model keys are cleared
	if _, ok := snap.Metrics["model_moonshot_cost_usd"]; ok {
		t.Fatal("model_moonshot_cost_usd should be cleared by telemetry overwrite")
	}
	// Native-only provider keys are cleared
	if _, ok := snap.Metrics["provider_alibaba_cost_usd"]; ok {
		t.Fatal("provider_alibaba_cost_usd should be cleared by telemetry overwrite")
	}
	if _, ok := snap.Metrics["provider_deepinfra_cost_usd"]; ok {
		t.Fatal("provider_deepinfra_cost_usd should be cleared by telemetry overwrite")
	}
	// Provider breakdown comes from telemetry
	if got := metricUsed(snap.Metrics["provider_openrouter_cost_usd"]); got != 0.012 {
		t.Fatalf("provider_openrouter_cost_usd = %v, want 0.012 from telemetry", got)
	}
	if _, ok := snap.Attributes["provider_legacy_cost"]; ok {
		t.Fatal("stale provider_* attribute should be cleared")
	}
	if got := metricUsed(snap.Metrics["client_opencode_requests"]); got != 1 {
		t.Fatalf("client_opencode_requests = %v, want 1 from canonical telemetry", got)
	}
}

func metricUsed(m core.Metric) float64 {
	if m.Used == nil {
		return 0
	}
	return *m.Used
}

func seriesValueByDate(points []core.TimePoint, date string) float64 {
	for _, point := range points {
		if point.Date == date {
			return point.Value
		}
	}
	return 0
}
