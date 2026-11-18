package kimi_cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func writeKimiCodeTelemetryFixture(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "group-a", "session-1", "agents", "main", "wire.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "" +
		`{"type":"metadata","created_at":"2026-01-01T00:00:00Z"}` + "\n" +
		`{"type":"usage.record","time":1767225600000,"model":"kimi-code/k3","usage":{"inputOther":100,"output":50,"inputCacheRead":10,"inputCacheCreation":6}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCollectTelemetry_ReadsKimiCodeUsageIncrementally(t *testing.T) {
	root := t.TempDir()
	path := writeKimiCodeTelemetryFixture(t, root)
	p := New()
	opts := shared.TelemetryCollectOptions{Paths: map[string]string{
		PathHintSessionsDirKey: root,
		"account_id":           "kimi-work",
	}}

	events, err := p.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	event := events[0]
	if event.AccountID != "kimi-work" || event.ProviderID != ID || event.SessionID != "group-a/session-1" {
		t.Errorf("identity = account %q provider %q session %q", event.AccountID, event.ProviderID, event.SessionID)
	}
	if event.ModelRaw != "kimi-code/k3" || event.EventType != shared.TelemetryEventTypeMessageUsage {
		t.Errorf("event = model %q type %q", event.ModelRaw, event.EventType)
	}
	if event.InputTokens == nil || *event.InputTokens != 100 ||
		event.OutputTokens == nil || *event.OutputTokens != 50 ||
		event.CacheReadTokens == nil || *event.CacheReadTokens != 10 ||
		event.CacheWriteTokens == nil || *event.CacheWriteTokens != 6 ||
		event.TotalTokens == nil || *event.TotalTokens != 166 ||
		event.Requests == nil || *event.Requests != 1 {
		t.Errorf("token usage = %+v", event.TokenUsage)
	}
	if want := time.UnixMilli(1767225600000).UTC(); !event.OccurredAt.Equal(want) {
		t.Errorf("occurred_at = %s, want %s", event.OccurredAt, want)
	}

	events, err = p.Collect(context.Background(), opts)
	if err != nil || len(events) != 0 {
		t.Fatalf("unchanged Collect: events=%d err=%v", len(events), err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString(`{"type":"usage.record","time":1767225601000,"model":"kimi-code/k3","usage":{"output":7}}` + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append: write=%v close=%v", writeErr, closeErr)
	}
	events, err = p.Collect(context.Background(), opts)
	if err != nil || len(events) != 1 {
		t.Fatalf("appended Collect: events=%d err=%v", len(events), err)
	}
	if events[0].OutputTokens == nil || *events[0].OutputTokens != 7 {
		t.Errorf("appended output = %+v, want 7", events[0].OutputTokens)
	}
}

func TestKimiTelemetryPopulatesCanonicalReadModel(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeKimiCodeTelemetryFixture(t, root)
	opts := shared.TelemetryCollectOptions{Paths: map[string]string{
		PathHintSessionsDirKey: root,
		"account_id":           DefaultAccountID,
	}}
	collector := telemetry.NewSourceCollector(New(), opts, DefaultAccountID)
	reqs, err := collector.Collect(ctx)
	if err != nil || len(reqs) != 1 {
		t.Fatalf("Collect: requests=%d err=%v", len(reqs), err)
	}
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := telemetry.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	for _, req := range reqs {
		if _, err := store.Ingest(ctx, req); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
	}

	snaps, err := telemetry.ApplyCanonicalTelemetryViewWithOptions(ctx, dbPath, map[string]core.UsageSnapshot{
		DefaultAccountID: core.NewUsageSnapshot(ID, DefaultAccountID),
	}, telemetry.ReadModelOptions{TimeWindow: core.TimeWindowAll})
	if err != nil {
		t.Fatalf("ApplyCanonicalTelemetryViewWithOptions: %v", err)
	}
	snap := snaps[DefaultAccountID]
	if len(snap.ModelUsage) != 1 || snap.ModelUsage[0].RawModelID != "kimi-code/k3" {
		t.Fatalf("model usage = %+v", snap.ModelUsage)
	}
	if snap.ModelUsage[0].TotalTokens == nil || *snap.ModelUsage[0].TotalTokens != 166 {
		t.Errorf("model total tokens = %+v, want 166", snap.ModelUsage[0].TotalTokens)
	}
	if len(snap.DailySeries["analytics_tokens"]) != 1 {
		t.Errorf("analytics token series = %+v", snap.DailySeries["analytics_tokens"])
	}
	if metric := snap.Metrics["cache_hit_ratio"]; metric.Used == nil || *metric.Used <= 0 {
		t.Errorf("cache hit ratio = %+v", metric)
	}
}
