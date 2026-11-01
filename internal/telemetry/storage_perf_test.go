package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func openPerfTestStore(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return db, store
}

func TestShouldRecordBalanceMetric(t *testing.T) {
	cases := []struct {
		key, unit string
		declared  bool
		want      bool
	}{
		{"all_time_input_tokens", "tokens", false, false},
		{"all_time_sessions", "sessions", false, false},
		{"all_time_api_cost", "USD", false, true},
		{"available_balance", "cny", false, true},
		{"credit_balance", "", false, true},      // priority key, any unit
		{"extra_credits", "credits", true, true}, // declared in CreditMetrics
		{"requests", "requests", false, false},
	}
	for _, tc := range cases {
		if got := ShouldRecordBalanceMetric(tc.key, tc.unit, tc.declared); got != tc.want {
			t.Errorf("ShouldRecordBalanceMetric(%q,%q,%v)=%v want %v", tc.key, tc.unit, tc.declared, got, tc.want)
		}
	}
}

func TestMigrationDropsNonMoneyBalanceObservations(t *testing.T) {
	db, store := openPerfTestStore(t)
	ctx := context.Background()
	v := 1.0
	now := time.Now().UTC()
	obs := []BalanceObservation{
		{MetricKey: "all_time_input_tokens", ObservedAt: now, Used: &v, Unit: "tokens", Semantics: "cumulative"},
		{MetricKey: "all_time_sessions", ObservedAt: now, Used: &v, Unit: "sessions", Semantics: "cumulative"},
		{MetricKey: "all_time_api_cost", ObservedAt: now, Used: &v, Unit: "USD", Semantics: "cumulative"},
		{MetricKey: "credit_balance", ObservedAt: now, Remaining: &v, Unit: "", Semantics: "balance"},
	}
	if err := store.RecordBalanceObservations(ctx, "p", "a", obs); err != nil {
		t.Fatalf("record: %v", err)
	}
	for i := 0; i < 2; i++ { // idempotent
		if err := store.RunMigrations(ctx); err != nil {
			t.Fatalf("migrations: %v", err)
		}
	}
	rows, err := db.Query(`SELECT metric_key FROM balance_observations ORDER BY metric_key`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		got = append(got, k)
	}
	if fmt.Sprint(got) != "[all_time_api_cost credit_balance]" {
		t.Fatalf("remaining keys = %v", got)
	}
	var idx int
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('idx_balance_obs_lookup','idx_usage_event_changes_event')`).Scan(&idx)
	if idx != 0 {
		t.Fatalf("redundant indexes still present: %d", idx)
	}
}

// The change log's seq never restarts, so a long-lived DB has MIN(seq) far
// above its row count. The old count-based cutoff deleted nothing there.
func TestPruneUsageEventChangesToSize_HighSeq(t *testing.T) {
	db, store := openPerfTestStore(t)
	if _, err := db.Exec(`INSERT INTO sqlite_sequence(name, seq) VALUES ('usage_event_changes', 650000)`); err != nil {
		t.Fatalf("seed sequence: %v", err)
	}
	for i := 0; i < 500; i++ {
		if _, err := db.Exec(`INSERT INTO usage_event_changes(event_id, operation) VALUES (?, 'insert')`, fmt.Sprintf("evt-%d", i)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	removed, done, err := store.PruneUsageEventChangesToSize(context.Background(), 100, 50)
	if err != nil || !done {
		t.Fatalf("prune: removed=%d done=%v err=%v", removed, done, err)
	}
	var count, minSeq, maxSeq int64
	_ = db.QueryRow(`SELECT COUNT(*), MIN(seq), MAX(seq) FROM usage_event_changes`).Scan(&count, &minSeq, &maxSeq)
	if count > 100 || count < 50 {
		t.Fatalf("expected 50..100 rows after prune, got %d (removed %d)", count, removed)
	}
	if maxSeq-minSeq+1 != count {
		t.Fatalf("expected a contiguous newest tail, got min=%d max=%d count=%d", minSeq, maxSeq, count)
	}
}

func batchTestRequests(n int) []IngestRequest {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	reqs := make([]IngestRequest, 0, n)
	for i := 0; i < n; i++ {
		in := int64(100 + i)
		reqs = append(reqs, IngestRequest{
			SourceSystem:  "claude_code",
			SourceChannel: SourceChannelJSONL,
			OccurredAt:    base.Add(time.Duration(i) * time.Second),
			ProviderID:    "claude_code",
			AccountID:     "acct",
			SessionID:     fmt.Sprintf("s-%d", i%7),
			MessageID:     fmt.Sprintf("m-%d", i),
			EventType:     EventTypeMessageUsage,
			ModelRaw:      "claude-x",
			TokenUsage:    core.TokenUsage{InputTokens: &in},
			Payload:       map[string]any{"i": i},
		})
	}
	return reqs
}

func dumpEvents(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT dedup_key, occurred_at, COALESCE(input_tokens,0), COALESCE(output_tokens,-1), COALESCE(message_id,''), status FROM usage_events ORDER BY dedup_key`)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k, at, msg, st string
		var in, outTok int64
		_ = rows.Scan(&k, &at, &in, &outTok, &msg, &st)
		out = append(out, fmt.Sprintf("%s|%s|%d|%d|%s|%s", k, at, in, outTok, msg, st))
	}
	return out
}

func TestIngestBatchMatchesPerEventIngest(t *testing.T) {
	ctx := context.Background()
	reqs := batchTestRequests(1203) // spans three chunks
	// Re-send some events in the same run, one with richer data, so both the
	// in-transaction dedup lookup and the enrich path are exercised.
	for i := 0; i < 40; i++ {
		dup := reqs[i*7]
		if i%2 == 0 {
			more := int64(9999)
			dup.TokenUsage.OutputTokens = &more
		}
		reqs = append(reqs, dup)
	}

	dbA, storeA := openPerfTestStore(t)
	var wantDeduped int
	for _, r := range reqs {
		res, err := storeA.Ingest(ctx, r)
		if err != nil {
			t.Fatalf("ingest: %v", err)
		}
		if res.Deduped {
			wantDeduped++
		}
	}

	dbB, storeB := openPerfTestStore(t)
	outcomes := storeB.IngestBatch(ctx, reqs)
	gotDeduped := 0
	for i, o := range outcomes {
		if o.Err != nil {
			t.Fatalf("batch event %d: %v", i, o.Err)
		}
		if o.Result.Deduped {
			gotDeduped++
		}
	}
	if gotDeduped != wantDeduped || wantDeduped == 0 {
		t.Fatalf("deduped: batch=%d per-event=%d", gotDeduped, wantDeduped)
	}
	a, b := dumpEvents(t, dbA), dumpEvents(t, dbB)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("batch rows differ from per-event rows (%d vs %d)", len(b), len(a))
	}

	// A second pass is all dedup and writes nothing new.
	for i, o := range storeB.IngestBatch(ctx, reqs) {
		if o.Err != nil || !o.Result.Deduped {
			t.Fatalf("second pass event %d: deduped=%v err=%v", i, o.Result.Deduped, o.Err)
		}
	}
	if got := dumpEvents(t, dbB); len(got) != len(b) {
		t.Fatalf("second pass changed row count: %d -> %d", len(b), len(got))
	}
}

func TestIngestBatchSavepointIsolatesBadEvent(t *testing.T) {
	ctx := context.Background()
	db, store := openPerfTestStore(t)
	reqs := batchTestRequests(10)
	// An unmarshalable payload fails inside the per-event body.
	reqs[4].Payload = map[string]any{"bad": make(chan int)}
	// A trigger failure fails after the raw row was written, so only the
	// savepoint rollback keeps that raw row out.
	reqs[7].MessageID = "poison"
	if _, err := db.Exec(`CREATE TRIGGER poison BEFORE INSERT ON usage_events
		WHEN NEW.message_id = 'poison' BEGIN SELECT RAISE(ABORT, 'poison'); END`); err != nil {
		t.Fatalf("trigger: %v", err)
	}

	outcomes := store.IngestBatch(ctx, reqs)
	for i, o := range outcomes {
		bad := i == 4 || i == 7
		if bad && o.Err == nil {
			t.Fatalf("event %d: expected error", i)
		}
		if !bad && o.Err != nil {
			t.Fatalf("event %d: unexpected error %v", i, o.Err)
		}
	}
	var events, raws int
	_ = db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&events)
	_ = db.QueryRow(`SELECT COUNT(*) FROM usage_raw_events`).Scan(&raws)
	if events != 8 || raws != 8 {
		t.Fatalf("expected 8 events and 8 raw rows, got %d/%d", events, raws)
	}
}

func TestDropExpiredLocalEvents(t *testing.T) {
	horizon := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	old := horizon.Add(-time.Hour)
	recent := horizon.Add(time.Hour)
	reqs := []IngestRequest{
		{SourceChannel: SourceChannelJSONL, OccurredAt: old},
		{SourceChannel: SourceChannelSQLite, OccurredAt: old},
		{SourceChannel: SourceChannelHook, OccurredAt: old},
		{SourceChannel: SourceChannelJSONL, OccurredAt: recent},
	}
	kept, dropped := DropExpiredLocalEvents(reqs, horizon)
	if dropped != 2 || len(kept) != 2 {
		t.Fatalf("dropped=%d kept=%d", dropped, len(kept))
	}
	if kept2, d := DropExpiredLocalEvents(reqs, time.Time{}); d != 0 || len(kept2) != 4 {
		t.Fatalf("zero horizon must keep everything")
	}
}

func TestLocalIngestHorizon(t *testing.T) {
	ctx := context.Background()
	_, store := openPerfTestStore(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	if h, _ := store.LocalIngestHorizon(ctx, 30); !h.IsZero() {
		t.Fatalf("no watermark must disable dropping, got %v", h)
	}
	_ = store.MetaSet(ctx, rollupWatermarkKey, "2026-10-31")
	if h, _ := store.LocalIngestHorizon(ctx, 30); !h.Equal(now.Add(-30 * 24 * time.Hour)) {
		t.Fatalf("expected retention cutoff, got %v", h)
	}
	// Daemon was down: watermark older than the cutoff bounds the horizon.
	_ = store.MetaSet(ctx, rollupWatermarkKey, "2026-09-01")
	if h, _ := store.LocalIngestHorizon(ctx, 30); !h.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected watermark day, got %v", h)
	}
}

func TestQuotaIngestSkipsIdenticalLimitSnapshot(t *testing.T) {
	ctx := context.Background()
	db, store := openPerfTestStore(t)
	ing := NewQuotaSnapshotIngestor(store)
	used := 5.0
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	snap := func(at time.Time, v float64) map[string]core.UsageSnapshot {
		u := v
		return map[string]core.UsageSnapshot{"acct": {
			ProviderID: "openrouter", AccountID: "acct", Timestamp: at, Status: core.StatusOK,
			Metrics: map[string]core.Metric{"credits": {Used: &u, Unit: "USD"}},
		}}
	}
	for i, s := range []map[string]core.UsageSnapshot{snap(t0, used), snap(t0.Add(time.Minute), used), snap(t0.Add(2*time.Minute), used)} {
		if err := ing.Ingest(ctx, s); err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
	}
	var n int
	var at string
	_ = db.QueryRow(`SELECT COUNT(*), MAX(occurred_at) FROM usage_events WHERE event_type='limit_snapshot'`).Scan(&n, &at)
	if n != 1 {
		t.Fatalf("identical snapshots should collapse to one row, got %d", n)
	}
	if want := t0.Add(2 * time.Minute).Format(time.RFC3339Nano); at != want {
		t.Fatalf("occurred_at not advanced: got %s want %s", at, want)
	}
	if err := ing.Ingest(ctx, snap(t0.Add(3*time.Minute), used+1)); err != nil {
		t.Fatalf("ingest changed: %v", err)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM usage_events WHERE event_type='limit_snapshot'`).Scan(&n)
	if n != 2 {
		t.Fatalf("changed snapshot must be written, got %d rows", n)
	}
}
