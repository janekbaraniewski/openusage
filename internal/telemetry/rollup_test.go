package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func i64(v int64) *int64      { return &v }
func f64p(v float64) *float64 { return &v }

func TestRollupDaily_TotalsAndIdempotency(t *testing.T) {
	_, store := openUsageViewTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

	// Three events across two past days for one provider/model.
	mk := func(day time.Time, id string, in, out int64, cost float64) IngestRequest {
		return IngestRequest{
			SourceSystem:  "codex",
			SourceChannel: SourceChannelHook,
			OccurredAt:    day,
			ProviderID:    "openai",
			AccountID:     "acct",
			SessionID:     "s1",
			MessageID:     id,
			EventType:     EventTypeMessageUsage,
			ModelRaw:      "gpt-5",
			TokenUsage:    core.TokenUsage{InputTokens: i64(in), OutputTokens: i64(out), CostUSD: f64p(cost)},
		}
	}
	d5 := now.AddDate(0, 0, -5)
	d3 := now.AddDate(0, 0, -3)
	mustIngestUsageEvent(t, store, mk(d5, "m1", 100, 10, 1.0), "d5-1")
	mustIngestUsageEvent(t, store, mk(d5, "m2", 200, 20, 2.0), "d5-2")
	mustIngestUsageEvent(t, store, mk(d3, "m3", 50, 5, 0.5), "d3-1")

	rolled, err := store.RollupDaily(ctx, now)
	if err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if rolled != 2 {
		t.Errorf("rolled day-rows = %d, want 2 (two distinct days)", rolled)
	}

	// Day d5 totals: 300 in, 30 out, $3.00, 2 events.
	var in, out, evcount int64
	var cost float64
	row := store.db.QueryRowContext(ctx,
		`SELECT input_tokens, output_tokens, cost_usd, event_count FROM usage_rollup_daily WHERE day = ?`,
		d5.Format("2006-01-02"))
	if err := row.Scan(&in, &out, &cost, &evcount); err != nil {
		t.Fatalf("scan d5: %v", err)
	}
	if in != 300 || out != 30 || cost != 3.0 || evcount != 2 {
		t.Errorf("d5 rollup = in:%d out:%d cost:%.2f n:%d, want 300/30/3.00/2", in, out, cost, evcount)
	}

	// Watermark advanced to yesterday.
	wm, err := store.RollupWatermark(ctx)
	if err != nil || wm != now.AddDate(0, 0, -1).Format("2006-01-02") {
		t.Errorf("watermark = %q (err %v), want yesterday", wm, err)
	}

	// Idempotent: re-running yields identical totals (no doubling).
	if _, err := store.RollupDaily(ctx, now); err != nil {
		t.Fatalf("rollup #2: %v", err)
	}
	row = store.db.QueryRowContext(ctx,
		`SELECT input_tokens, output_tokens, cost_usd, event_count FROM usage_rollup_daily WHERE day = ?`,
		d5.Format("2006-01-02"))
	if err := row.Scan(&in, &out, &cost, &evcount); err != nil {
		t.Fatalf("scan d5 #2: %v", err)
	}
	if in != 300 || out != 30 || cost != 3.0 || evcount != 2 {
		t.Errorf("d5 rollup after re-run = in:%d out:%d cost:%.2f n:%d, want unchanged 300/30/3.00/2", in, out, cost, evcount)
	}
}

func TestRollupDaily_DedupsAcrossChannels(t *testing.T) {
	_, store := openUsageViewTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	day := now.AddDate(0, 0, -2)

	// Same logical event (same message_id) from two source channels. The deduped
	// view must count it once, so the rollup must not double it.
	base := IngestRequest{
		OccurredAt: day, ProviderID: "openai", AccountID: "a", SessionID: "s",
		MessageID: "dup-1", EventType: EventTypeMessageUsage, ModelRaw: "gpt-5",
		TokenUsage: core.TokenUsage{InputTokens: i64(100), OutputTokens: i64(10), CostUSD: f64p(1.0)},
	}
	hook := base
	hook.SourceSystem, hook.SourceChannel = "codex", SourceChannelHook
	api := base
	api.SourceSystem, api.SourceChannel = "codex", SourceChannel("api")
	mustIngestUsageEvent(t, store, hook, "hook")
	mustIngestUsageEvent(t, store, api, "api")

	if _, err := store.RollupDaily(ctx, now); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	var in, evcount int64
	row := store.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(input_tokens),0), COALESCE(SUM(event_count),0) FROM usage_rollup_daily WHERE day = ?`,
		day.Format("2006-01-02"))
	if err := row.Scan(&in, &evcount); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if in != 100 || evcount != 1 {
		t.Errorf("deduped rollup = in:%d n:%d, want 100/1 (counted once across channels)", in, evcount)
	}
}
