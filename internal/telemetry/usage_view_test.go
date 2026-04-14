package telemetry

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"

	_ "github.com/mattn/go-sqlite3"
)

func float64Ptr(v float64) *float64 { return &v }

func TestValidateMaterializedTable(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid constant", input: "_deduped_tmp", wantErr: false},
		{name: "empty string", input: "", wantErr: true},
		{name: "SQL injection attempt", input: "t; DROP TABLE usage_events; --", wantErr: true},
		{name: "uppercase letters", input: "_Deduped_Tmp", wantErr: true},
		{name: "digits", input: "_deduped_tmp1", wantErr: true},
		{name: "valid pattern but not in allowlist", input: "_other_table", wantErr: true},
		{name: "spaces", input: "_deduped tmp", wantErr: true},
		{name: "special characters", input: "_deduped$tmp", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMaterializedTable(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMaterializedTable(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestTodayExpr(t *testing.T) {
	t.Run("zero TodaySince falls back to UTC date('now')", func(t *testing.T) {
		f := usageFilter{}
		got := f.todayExpr("occurred_at")
		want := "date(occurred_at) = date('now')"
		if got != want {
			t.Errorf("todayExpr() = %q, want %q", got, want)
		}
	})

	t.Run("non-zero TodaySince uses formatted timestamp", func(t *testing.T) {
		midnight := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
		f := usageFilter{TodaySince: midnight}
		got := f.todayExpr("occurred_at")
		if !strings.Contains(got, "occurred_at >= '2026-04-08T00:00:00Z'") {
			t.Errorf("todayExpr() = %q, want occurred_at >= timestamp", got)
		}
	})

	t.Run("uses UTC regardless of input timezone", func(t *testing.T) {
		loc, _ := time.LoadLocation("America/New_York")
		midnight := time.Date(2026, 4, 8, 0, 0, 0, 0, loc)
		f := usageFilter{TodaySince: midnight}
		got := f.todayExpr("occurred_at")
		// New York midnight = 2026-04-08T04:00:00Z in UTC
		if !strings.Contains(got, "2026-04-08T04:00:00Z") {
			t.Errorf("todayExpr() = %q, want UTC-converted timestamp", got)
		}
	})
}

func TestMaterializedTableNameConstant(t *testing.T) {
	// Verify the constant is what we expect, so any future change is deliberate.
	if materializedTableName != "_deduped_tmp" {
		t.Errorf("materializedTableName = %q, want %q", materializedTableName, "_deduped_tmp")
	}
	// Verify the constant passes validation.
	if err := validateMaterializedTable(materializedTableName); err != nil {
		t.Errorf("materializedTableName failed validation: %v", err)
	}
}

func TestApplyCanonicalUsageView_MergesTelemetryWithoutReplacingRootMetrics(t *testing.T) {
	dbPath, store := openUsageViewTestStore(t)

	occurredAt := time.Date(2026, 2, 22, 12, 0, 0, 0, time.UTC)
	mustIngestUsageEvent(t, store, IngestRequest{
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
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(120),
			OutputTokens: int64Ptr(40),
			TotalTokens:  int64Ptr(160),
			CostUSD:      float64Ptr(0.012),
			Requests:     int64Ptr(1),
		},
	}, "ingest message event")

	mustIngestUsageEvent(t, store, IngestRequest{
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
		TokenUsage: core.TokenUsage{
			Requests: int64Ptr(1),
		},
	}, "ingest tool event")

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
	dbPath, db, store := openUsageViewRawTestStore(t)

	occurredAt := time.Date(2026, 2, 22, 12, 0, 0, 0, time.UTC)
	_, err := store.Ingest(context.Background(), IngestRequest{
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
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(120),
			OutputTokens: int64Ptr(40),
			TotalTokens:  int64Ptr(160),
			CostUSD:      float64Ptr(0.012),
			Requests:     int64Ptr(1),
		},
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
	dbPath, store := openUsageViewTestStore(t)

	occurredAt := time.Date(2026, 2, 22, 12, 0, 0, 0, time.UTC)
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
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(120),
			OutputTokens: int64Ptr(40),
			TotalTokens:  int64Ptr(160),
			CostUSD:      float64Ptr(9.99),
			Requests:     int64Ptr(1),
		},
	}); err != nil {
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

func TestApplyCanonicalUsageView_FallsBackToProviderScopeForAccountView(t *testing.T) {
	dbPath, store := openUsageViewTestStore(t)

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
		TokenUsage: core.TokenUsage{
			InputTokens: &input,
			TotalTokens: &total,
			Requests:    int64Ptr(1),
		},
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
	// With provider-scope fallback, telemetry data should now be applied when
	// account-scoped query returns 0 events but provider-scoped data exists.
	if _, ok := snap.Metrics["client_opencode_requests"]; !ok {
		t.Fatalf("expected provider-scope fallback metric client_opencode_requests in cursor view")
	}
	if got := snap.Attributes["telemetry_view"]; got != "canonical" {
		t.Fatalf("telemetry_view = %q, want %q (provider-scope fallback should apply canonical view)", got, "canonical")
	}
}

func TestApplyCanonicalUsageView_ClearsStalePrefixedAttributeAndDiagnosticKeys(t *testing.T) {
	dbPath, store := openUsageViewTestStore(t)

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
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(120),
			OutputTokens: int64Ptr(40),
			TotalTokens:  int64Ptr(160),
			CostUSD:      float64Ptr(0.012),
			Requests:     int64Ptr(1),
		},
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
	dbPath, store := openUsageViewTestStore(t)

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
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(120),
			OutputTokens: int64Ptr(40),
			TotalTokens:  int64Ptr(160),
			CostUSD:      float64Ptr(0.012),
			Requests:     int64Ptr(1),
		},
		Payload: map[string]any{
			"_normalized": map[string]any{
				"upstream_provider": "deepinfra",
			},
		},
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
				"provider_hyperbolic_cost_usd":  {Used: &providerB, Unit: "USD"},
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

	// Telemetry always overwrites model breakdown — native values are replaced
	if got := metricUsed(snap.Metrics["model_qwen_qwen3_coder_flash_cost_usd"]); got != 0.012 {
		t.Fatalf("model_qwen_qwen3_coder_flash_cost_usd = %v, want 0.012 from telemetry", got)
	}
	// Native-only model keys are cleared
	if _, ok := snap.Metrics["model_moonshot_cost_usd"]; ok {
		t.Fatal("model_moonshot_cost_usd should be cleared by telemetry overwrite")
	}
	// Native provider_* metrics are cleared and replaced by telemetry-derived
	// upstream hosting providers from hook payload enrichment.
	if _, ok := snap.Metrics["provider_alibaba_cost_usd"]; ok {
		t.Fatal("provider_alibaba_cost_usd should be cleared by telemetry overwrite")
	}
	if _, ok := snap.Metrics["provider_hyperbolic_cost_usd"]; ok {
		t.Fatal("provider_hyperbolic_cost_usd should be cleared by telemetry overwrite")
	}
	if got := metricUsed(snap.Metrics["provider_deepinfra_cost_usd"]); got != 0.012 {
		t.Fatalf(
			"provider_deepinfra_cost_usd = %v, want 0.012 from telemetry upstream provider (provider_openrouter_cost_usd=%v)",
			got,
			metricUsed(snap.Metrics["provider_openrouter_cost_usd"]),
		)
	}
	// Provider ID grouping should not be used when upstream provider exists.
	if _, ok := snap.Metrics["provider_openrouter_cost_usd"]; ok {
		t.Fatal("provider_openrouter_cost_usd should not exist when upstream provider is available")
	}
	if _, ok := snap.Attributes["provider_legacy_cost"]; ok {
		t.Fatal("stale provider_* attribute should be cleared")
	}
	if got := metricUsed(snap.Metrics["client_opencode_requests"]); got != 1 {
		t.Fatalf("client_opencode_requests = %v, want 1 from canonical telemetry", got)
	}
}

func TestApplyCanonicalUsageView_ProviderFallbackUsesProviderIDWhenUpstreamMissing(t *testing.T) {
	dbPath, store := openUsageViewTestStore(t)

	occurredAt := time.Date(2026, 2, 23, 10, 30, 0, 0, time.UTC)
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
		ModelRaw:      "qwen-qwen3-coder-flash",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(120),
			OutputTokens: int64Ptr(40),
			TotalTokens:  int64Ptr(160),
			CostUSD:      float64Ptr(0.012),
			Requests:     int64Ptr(1),
		},
	}); err != nil {
		t.Fatalf("ingest message event: %v", err)
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

	if got := metricUsed(snap.Metrics["provider_openrouter_cost_usd"]); got != 0.012 {
		t.Fatalf("provider_openrouter_cost_usd = %v, want 0.012 from provider_id fallback", got)
	}
	if _, ok := snap.Metrics["provider_qwen_cost_usd"]; ok {
		t.Fatal("provider_qwen_cost_usd should not exist without explicit upstream provider")
	}
}

func TestApplyCanonicalUsageView_IncludesErroredToolCallsAndMCPBreakdown(t *testing.T) {
	dbPath, store := openUsageViewTestStore(t)

	occurredAt := time.Now().UTC().Add(-2 * time.Minute)
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    occurredAt,
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		AgentName:     "codex",
		EventType:     EventTypeToolUsage,
		SessionID:     "sess-codex-1",
		ToolCallID:    "tool-ok-1",
		ToolName:      "exec_command",
		TokenUsage: core.TokenUsage{
			Requests: int64Ptr(1),
		},
		Status: EventStatusOK,
	}); err != nil {
		t.Fatalf("ingest ok tool event: %v", err)
	}
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    occurredAt.Add(1 * time.Second),
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		AgentName:     "codex",
		EventType:     EventTypeToolUsage,
		SessionID:     "sess-codex-1",
		ToolCallID:    "tool-err-1",
		ToolName:      "mcp__gopls__go_workspace",
		TokenUsage: core.TokenUsage{
			Requests: int64Ptr(1),
		},
		Status: EventStatusError,
	}); err != nil {
		t.Fatalf("ingest errored mcp tool event: %v", err)
	}

	snaps := map[string]core.UsageSnapshot{
		"codex-cli": {
			ProviderID: "codex",
			AccountID:  "codex-cli",
			Metrics:    map[string]core.Metric{},
		},
	}
	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}
	snap := merged["codex-cli"]

	if got := metricUsed(snap.Metrics["tool_exec_command"]); got != 1 {
		t.Fatalf("tool_exec_command = %v, want 1", got)
	}
	if got := metricUsed(snap.Metrics["tool_mcp_gopls_go_workspace"]); got != 1 {
		t.Fatalf("tool_mcp_gopls_go_workspace = %v, want 1", got)
	}
	if got := metricUsed(snap.Metrics["tool_calls_total"]); got != 2 {
		t.Fatalf("tool_calls_total = %v, want 2", got)
	}
	if got := metricUsed(snap.Metrics["tool_completed"]); got != 1 {
		t.Fatalf("tool_completed = %v, want 1", got)
	}
	if got := metricUsed(snap.Metrics["tool_errored"]); got != 1 {
		t.Fatalf("tool_errored = %v, want 1", got)
	}
	if got := metricUsed(snap.Metrics["tool_success_rate"]); got != 50 {
		t.Fatalf("tool_success_rate = %v, want 50", got)
	}

	if got := metricUsed(snap.Metrics["mcp_calls_total"]); got != 1 {
		t.Fatalf("mcp_calls_total = %v, want 1", got)
	}
	if got := metricUsed(snap.Metrics["mcp_gopls_total"]); got != 1 {
		t.Fatalf("mcp_gopls_total = %v, want 1", got)
	}
	if pts := snap.DailySeries["usage_mcp_gopls"]; len(pts) != 1 || pts[0].Value != 1 {
		t.Fatalf("usage_mcp_gopls = %+v, want single point with value 1", pts)
	}
}

func TestParseMCPToolName_CopilotLegacyWrapper(t *testing.T) {
	server, function, ok := parseMCPToolName("github_mcp_server_list_issues")
	if !ok {
		t.Fatal("parseMCPToolName should parse copilot wrapper pattern")
	}
	if server != "github" {
		t.Fatalf("server = %q, want github", server)
	}
	if function != "list_issues" {
		t.Fatalf("function = %q, want list_issues", function)
	}
}

func TestApplyCanonicalUsageView_SkipsProviderBurnMetricsForCodex(t *testing.T) {
	dbPath, store := openUsageViewTestStore(t)

	occurredAt := time.Now().UTC()
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    occurredAt,
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-codex-2",
		MessageID:     "msg-1",
		ModelRaw:      "gpt-5-codex",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(10),
			OutputTokens: int64Ptr(5),
			TotalTokens:  int64Ptr(15),
			CostUSD:      float64Ptr(0.01),
			Requests:     int64Ptr(1),
		},
		Payload: map[string]any{
			"upstream_provider": "openai",
		},
	}); err != nil {
		t.Fatalf("ingest codex message event: %v", err)
	}

	snaps := map[string]core.UsageSnapshot{
		"codex-cli": {
			ProviderID: "codex",
			AccountID:  "codex-cli",
			Metrics:    map[string]core.Metric{},
		},
	}
	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}
	snap := merged["codex-cli"]

	for key := range snap.Metrics {
		if strings.HasPrefix(key, "provider_") {
			t.Fatalf("unexpected codex provider burn metric: %s", key)
		}
	}
}

func TestApplyCanonicalUsageView_DedupsCodexMessageUsageByTurnID(t *testing.T) {
	dbPath, store := openUsageViewTestStore(t)

	now := time.Now().UTC()
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    now,
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		WorkspaceID:   "openusage",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-dedup-1",
		TurnID:        "turn-dedup-1",
		MessageID:     "sess-dedup-1:101",
		ModelRaw:      "gpt-5-codex",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(10),
			OutputTokens: int64Ptr(5),
			TotalTokens:  int64Ptr(15),
			Requests:     int64Ptr(1),
		},
		Status: EventStatusOK,
	}); err != nil {
		t.Fatalf("ingest first codex message event: %v", err)
	}
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    now.Add(1 * time.Second),
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		WorkspaceID:   "openusage",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-dedup-1",
		TurnID:        "turn-dedup-1",
		MessageID:     "sess-dedup-1:102",
		ModelRaw:      "gpt-5-codex",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(15),
			OutputTokens: int64Ptr(9),
			TotalTokens:  int64Ptr(24),
			Requests:     int64Ptr(1),
		},
		Status: EventStatusOK,
	}); err != nil {
		t.Fatalf("ingest second codex message event: %v", err)
	}

	snaps := map[string]core.UsageSnapshot{
		"codex-cli": {
			ProviderID: "codex",
			AccountID:  "codex-cli",
			Metrics:    map[string]core.Metric{},
		},
	}
	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}
	snap := merged["codex-cli"]

	if got := metricUsed(snap.Metrics["client_cli_requests"]); got != 1 {
		t.Fatalf("client_cli_requests = %v, want 1 (deduped by turn_id)", got)
	}
	if got := metricUsed(snap.Metrics["window_requests"]); got != 1 {
		t.Fatalf("window_requests = %v, want 1 (deduped by turn_id)", got)
	}
	if got := metricUsed(snap.Metrics["window_tokens"]); got != 24 {
		t.Fatalf("window_tokens = %v, want 24 from the newest turn event", got)
	}
}

func TestApplyCanonicalUsageView_UsesClientFromPayloadBeforeWorkspace(t *testing.T) {
	dbPath, store := openUsageViewTestStore(t)

	now := time.Now().UTC()
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    now,
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		WorkspaceID:   "openusage",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-clients-1",
		MessageID:     "msg-clients-1",
		ModelRaw:      "gpt-5-codex",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(10),
			OutputTokens: int64Ptr(5),
			TotalTokens:  int64Ptr(15),
			Requests:     int64Ptr(1),
		},
		Payload: map[string]any{
			"client": "CLI",
		},
	}); err != nil {
		t.Fatalf("ingest cli event: %v", err)
	}
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    now.Add(1 * time.Second),
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		WorkspaceID:   "openusage",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-clients-1",
		MessageID:     "msg-clients-2",
		ModelRaw:      "gpt-5-codex",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(12),
			OutputTokens: int64Ptr(6),
			TotalTokens:  int64Ptr(18),
			Requests:     int64Ptr(1),
		},
		Payload: map[string]any{
			"client": "Desktop App",
		},
	}); err != nil {
		t.Fatalf("ingest desktop event: %v", err)
	}

	snaps := map[string]core.UsageSnapshot{
		"codex-cli": {
			ProviderID: "codex",
			AccountID:  "codex-cli",
			Metrics:    map[string]core.Metric{},
		},
	}
	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}
	snap := merged["codex-cli"]

	if got := metricUsed(snap.Metrics["client_cli_requests"]); got != 1 {
		t.Fatalf("client_cli_requests = %v, want 1", got)
	}
	if got := metricUsed(snap.Metrics["client_desktop_app_requests"]); got != 1 {
		t.Fatalf("client_desktop_app_requests = %v, want 1", got)
	}
	if _, ok := snap.Metrics["client_openusage_requests"]; ok {
		t.Fatalf("unexpected workspace-derived client metric client_openusage_requests present")
	}
}

func TestApplyCanonicalUsageView_EmitsProjectMetricsFromWorkspace(t *testing.T) {
	dbPath, store := openUsageViewTestStore(t)

	now := time.Now().UTC()
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    now,
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		WorkspaceID:   "openusage",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-projects-1",
		MessageID:     "msg-projects-1",
		ModelRaw:      "gpt-5-codex",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(10),
			OutputTokens: int64Ptr(5),
			TotalTokens:  int64Ptr(15),
			Requests:     int64Ptr(1),
		},
		Payload: map[string]any{
			"client": "CLI",
		},
	}); err != nil {
		t.Fatalf("ingest openusage event: %v", err)
	}
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    now.Add(1 * time.Second),
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		WorkspaceID:   "garage-tracker",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-projects-2",
		MessageID:     "msg-projects-2",
		ModelRaw:      "gpt-5-codex",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(12),
			OutputTokens: int64Ptr(6),
			TotalTokens:  int64Ptr(18),
			Requests:     int64Ptr(1),
		},
		Payload: map[string]any{
			"client": "CLI",
		},
	}); err != nil {
		t.Fatalf("ingest garage-tracker event: %v", err)
	}
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    now.Add(2 * time.Second),
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-projects-3",
		MessageID:     "msg-projects-3",
		ModelRaw:      "gpt-5-codex",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(8),
			OutputTokens: int64Ptr(4),
			TotalTokens:  int64Ptr(12),
			Requests:     int64Ptr(1),
		},
		Payload: map[string]any{
			"client": "CLI",
		},
	}); err != nil {
		t.Fatalf("ingest no-workspace event: %v", err)
	}

	snaps := map[string]core.UsageSnapshot{
		"codex-cli": {
			ProviderID: "codex",
			AccountID:  "codex-cli",
			Metrics:    map[string]core.Metric{},
		},
	}
	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, snaps)
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}
	snap := merged["codex-cli"]

	if got := metricUsed(snap.Metrics["project_openusage_requests"]); got != 1 {
		t.Fatalf("project_openusage_requests = %v, want 1", got)
	}
	if got := metricUsed(snap.Metrics["project_garage_tracker_requests"]); got != 1 {
		t.Fatalf("project_garage_tracker_requests = %v, want 1", got)
	}
	if _, ok := snap.Metrics["project_unknown_requests"]; ok {
		t.Fatalf("unexpected unknown project bucket emitted: %+v", snap.Metrics["project_unknown_requests"])
	}

	day := now.Format("2006-01-02")
	if got := seriesValueByDate(snap.DailySeries["usage_project_openusage"], day); got != 1 {
		t.Fatalf("usage_project_openusage[%s] = %v, want 1", day, got)
	}
	if got := seriesValueByDate(snap.DailySeries["usage_project_garage_tracker"], day); got != 1 {
		t.Fatalf("usage_project_garage_tracker[%s] = %v, want 1", day, got)
	}
}

func TestApplyCanonicalUsageView_UsesClientDimensionForSourceDailySeries(t *testing.T) {
	dbPath, store := openUsageViewTestStore(t)

	now := time.Now().UTC()
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    now,
		ProviderID:    "codex",
		AccountID:     "codex-cli",
		WorkspaceID:   "openusage",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "sess-source-daily-1",
		MessageID:     "msg-source-daily-1",
		ModelRaw:      "gpt-5-codex",
		TokenUsage: core.TokenUsage{
			InputTokens:  int64Ptr(10),
			OutputTokens: int64Ptr(5),
			TotalTokens:  int64Ptr(15),
			Requests:     int64Ptr(1),
		},
		Payload: map[string]any{
			"client": "Desktop App",
		},
	}); err != nil {
		t.Fatalf("ingest message event: %v", err)
	}

	merged, err := applyCanonicalUsageViewForTest(context.Background(), dbPath, map[string]core.UsageSnapshot{
		"codex-cli": {
			ProviderID: "codex",
			AccountID:  "codex-cli",
			Metrics:    map[string]core.Metric{},
		},
	})
	if err != nil {
		t.Fatalf("apply canonical usage view: %v", err)
	}

	snap := merged["codex-cli"]
	if got := seriesValueByDate(snap.DailySeries["usage_source_desktop_app"], now.Format("2006-01-02")); got != 1 {
		t.Fatalf("usage_source_desktop_app = %v, want 1", got)
	}
	if _, ok := snap.DailySeries["usage_source_openusage"]; ok {
		t.Fatalf("unexpected workspace-derived source daily series usage_source_openusage present")
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
