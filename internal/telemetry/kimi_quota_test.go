package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestKimiQuotaRecoveryLifecycle(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	t0 := time.Date(2026, 10, 5, 8, 18, 48, 0, time.UTC)
	good := core.NewUsageSnapshot("kimi_cli", "kimi")
	good.Timestamp, good.Status = t0, core.StatusOK
	good.Metrics["usage_monthly"] = core.Metric{Used: float64Ptr(69.8), Limit: float64Ptr(100), Unit: "%", Window: "30d"}
	good.Metrics["usage_five_hour"] = core.Metric{Used: float64Ptr(46), Limit: float64Ptr(100), Unit: "%", Window: "5h"}
	good.Metrics["total_sessions"] = core.Metric{Used: float64Ptr(177)}
	good.Resets["usage_five_hour"] = t0.Add(time.Minute)
	good.Resets["usage_monthly"] = t0.Add(14 * 24 * time.Hour)
	write := func(snap core.UsageSnapshot) {
		t.Helper()
		if err := NewQuotaSnapshotIngestor(store).Ingest(ctx, map[string]core.UsageSnapshot{snap.AccountID: snap}); err != nil {
			t.Fatal(err)
		}
	}
	read := func(account string) core.UsageSnapshot {
		t.Helper()
		snap, err := loadLatestLimitSnapshot(ctx, store.db, "kimi_cli", account)
		if err != nil || snap == nil {
			t.Fatalf("load snapshot: snap=%v err=%v", snap, err)
		}
		return *snap
	}
	write(good) // Legacy successful snapshots have no freshness attributes.
	if snap := read("kimi"); snap.Attributes["quota_state"] != "fresh" {
		t.Fatalf("successful quota not fresh: %v", snap.Attributes)
	}

	// More recent gauges from another account or provider must never leak in.
	other := core.NewUsageSnapshot("kimi_cli", "other")
	other.Timestamp, other.Status = t0.Add(time.Minute), core.StatusOK
	other.Metrics["usage_monthly"] = core.Metric{Used: float64Ptr(99), Limit: float64Ptr(100), Unit: "%", Window: "30d"}
	write(other)
	other.ProviderID, other.AccountID = "openai", "kimi"
	write(other)
	failure := core.NewUsageSnapshot("kimi_cli", "kimi")
	failure.Timestamp, failure.Status = t0.Add(2*time.Minute), core.StatusOK
	failure.Metrics["total_sessions"] = core.Metric{Used: float64Ptr(180)}
	failure.Diagnostics["quota_error"] = "context deadline exceeded"
	failure.Attributes["quota_state"] = "unavailable"
	write(failure)

	// Maintenance must keep the successful payload needed after this failure.
	if _, err := store.PruneRawEventPayloads(ctx, 0, 1000); err != nil {
		t.Fatal(err)
	}

	// Reopen the database to prove recovery survives a daemon restart.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	snap := read("kimi")
	if snap.Attributes["quota_state"] != "stale" || snap.Attributes["quota_fetched_at"] != t0.Format(time.RFC3339Nano) {
		t.Fatalf("missing stale metadata: %v", snap.Attributes)
	}
	if *snap.Metrics["usage_monthly"].Used != 69.8 || *snap.Metrics["usage_five_hour"].Used != 46 {
		t.Fatalf("lost historical quota, including the expired window: %v", snap.Metrics)
	}
	if *snap.Metrics["total_sessions"].Used != 180 || !snap.Timestamp.Equal(failure.Timestamp) || snap.Diagnostics["quota_error"] == "" {
		t.Fatalf("recovery overwrote latest activity or diagnostics: %+v", snap)
	}
	if !snap.Resets["usage_five_hour"].Equal(good.Resets["usage_five_hour"]) {
		t.Fatal("reset boundary lost")
	}

	failure.Timestamp = failure.Timestamp.Add(time.Minute)
	failure.Diagnostics = map[string]string{"quota": "access token expired"}
	write(failure)
	if snap := read("kimi"); snap.Attributes["quota_state"] != "stale" || snap.Diagnostics["quota"] != "access token expired" {
		t.Fatalf("expired token lost fallback: %+v", snap)
	}
	failure.AccountID = "no-history"
	write(failure)
	if snap := read("no-history"); snap.Attributes["quota_state"] != "unavailable" || snap.Metrics["usage_monthly"].Used != nil {
		t.Fatalf("invented quota without history: %+v", snap)
	}
	failure.AccountID = "kimi"
	failure.Timestamp = t0.Add(4 * time.Minute)
	failure.Status, failure.Message = core.StatusError, "context deadline exceeded"
	failure.Metrics = map[string]core.Metric{}
	failure.Diagnostics = map[string]string{}
	write(failure)
	if snap := read("kimi"); snap.Status != core.StatusError || snap.Attributes["quota_state"] != "stale" || snap.Metrics["usage_monthly"].Used == nil || snap.Metrics["total_sessions"].Used != nil {
		t.Fatalf("fatal poll must preserve only quota and the current error: %+v", snap)
	}

	// Equal percentages after recovery are current observations, not stale.
	good.Timestamp = t0.Add(5 * time.Minute)
	good.Attributes["quota_state"] = "fresh"
	good.Attributes["quota_fetched_at"] = good.Timestamp.Format(time.RFC3339Nano)
	write(good)
	if snap := read("kimi"); snap.Attributes["quota_state"] != "fresh" || len(snap.Diagnostics) != 0 || snap.Attributes["quota_fetched_at"] != good.Timestamp.Format(time.RFC3339Nano) {
		t.Fatalf("successful recovery kept old freshness: %+v", snap)
	}
}

func TestKimiQuotaRetentionKeepsOnlyLatestSuccessAndLatestPoll(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	t0 := time.Now().UTC().Add(-24 * time.Hour)
	write := func(account string, hour int, quota *float64) {
		t.Helper()
		snap := core.NewUsageSnapshot("kimi_cli", account)
		snap.Timestamp, snap.Status = t0.Add(time.Duration(hour)*time.Hour), core.StatusOK
		snap.Metrics["total_sessions"] = core.Metric{Used: float64Ptr(float64(190 + hour))}
		if quota != nil {
			snap.Metrics["usage_monthly"] = core.Metric{Used: quota, Limit: float64Ptr(100), Unit: "%", Window: "30d"}
		} else {
			snap.Diagnostics["quota"] = "access token expired"
		}
		if err := NewQuotaSnapshotIngestor(store).Ingest(ctx, map[string]core.UsageSnapshot{account: snap}); err != nil {
			t.Fatal(err)
		}
	}
	write("kimi", 0, float64Ptr(70))
	write("kimi", 1, float64Ptr(71))
	write("kimi", 2, nil)
	write("kimi", 3, nil)
	write("other", 1, float64Ptr(99))
	prune := func() {
		t.Helper()
		if _, err := store.PruneRawEventPayloads(ctx, 0, 1000); err != nil {
			t.Fatal(err)
		}
		var retained int
		if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM usage_raw_events WHERE source_payload != '{}'`).Scan(&retained); err != nil {
			t.Fatal(err)
		}
		if retained != 3 {
			t.Fatalf("retained payloads = %d, want latest poll + latest success for kimi and one for other", retained)
		}
	}
	prune()
	assertQuota := func(want float64, sessions float64) {
		t.Helper()
		snap, err := loadLatestLimitSnapshot(ctx, store.db, "kimi_cli", "kimi")
		if err != nil || snap == nil || snap.Metrics["usage_monthly"].Used == nil || *snap.Metrics["usage_monthly"].Used != want || *snap.Metrics["total_sessions"].Used != sessions || snap.Attributes["quota_state"] != "stale" {
			t.Fatalf("post-prune quota = %+v err=%v", snap, err)
		}
	}
	assertQuota(71, 193)
	write("kimi", 4, float64Ptr(72))
	write("kimi", 5, nil)
	prune()
	assertQuota(72, 195)
}
