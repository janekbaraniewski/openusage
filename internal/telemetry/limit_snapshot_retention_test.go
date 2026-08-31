package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func ingestLimitSnapshot(t *testing.T, store *Store, providerID, accountID string, remaining float64, at time.Time) {
	t.Helper()
	limit := 20.0
	snaps := map[string]core.UsageSnapshot{
		accountID: {
			ProviderID: providerID,
			AccountID:  accountID,
			Timestamp:  at,
			Status:     core.StatusOK,
			Metrics: map[string]core.Metric{
				"plan_spend": {
					Limit:     &limit,
					Remaining: &remaining,
					Unit:      "USD",
					Window:    "month",
				},
			},
		},
	}
	if err := NewQuotaSnapshotIngestor(store).Ingest(context.Background(), snaps); err != nil {
		t.Fatalf("ingest limit snapshot: %v", err)
	}
}

func rawPayloadFor(t *testing.T, store *Store, providerID, accountID string) string {
	t.Helper()
	payload, _, found, err := queryLatestLimitSnapshotPayload(context.Background(), store.db, providerID, accountID)
	if err != nil {
		t.Fatalf("query latest limit snapshot: %v", err)
	}
	if !found {
		t.Fatal("no limit_snapshot row found")
	}
	return payload
}

// Retention blanked source_payload for every raw event older than an hour,
// limit_snapshot included. That payload is the read model's only source for a
// provider's quota metrics, so blanking it left the dashboard hydrating from an
// empty envelope — a confident UNKNOWN with no metrics that survived a daemon
// restart because the damage was in the database (#293).
func TestPruneRawEventPayloads_KeepsLatestLimitSnapshot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	ingestLimitSnapshot(t, store, "cursor", "cursor-ide", 12.5, time.Date(2026, 8, 1, 15, 30, 0, 0, time.UTC))

	// retentionHours = 0 makes every row eligible, which is what an old
	// snapshot looks like once the daemon has been up for a while.
	if _, err := store.PruneRawEventPayloads(ctx, 0, 1000); err != nil {
		t.Fatalf("PruneRawEventPayloads: %v", err)
	}

	if got := rawPayloadFor(t, store, "cursor", "cursor-ide"); got == "{}" {
		t.Fatal("latest limit_snapshot payload was blanked by retention")
	}

	// And it still hydrates a real snapshot rather than an UNKNOWN shell.
	base := map[string]core.UsageSnapshot{
		"cursor-ide": {ProviderID: "cursor", AccountID: "cursor-ide"},
	}
	got, err := applyCanonicalTelemetryViewForTest(ctx, dbPath, base)
	if err != nil {
		t.Fatalf("apply canonical telemetry view: %v", err)
	}
	snap := got["cursor-ide"]
	if snap.Status != core.StatusOK {
		t.Errorf("status = %q, want %q", snap.Status, core.StatusOK)
	}
	if m, ok := snap.Metrics["plan_spend"]; !ok || m.Remaining == nil || *m.Remaining != 12.5 {
		t.Errorf("plan_spend = %+v, want remaining 12.5", snap.Metrics["plan_spend"])
	}
}

// Only the newest snapshot per provider/account is protected; the historical
// ones are never read back and are the bulk of the volume, so they must still
// be reclaimed.
func TestPruneRawEventPayloads_StillPrunesSupersededLimitSnapshots(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	ingestLimitSnapshot(t, store, "cursor", "cursor-ide", 5.0, time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))
	ingestLimitSnapshot(t, store, "cursor", "cursor-ide", 7.0, time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC))
	ingestLimitSnapshot(t, store, "cursor", "cursor-ide", 9.0, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))

	pruned, err := store.PruneRawEventPayloads(ctx, 0, 1000)
	if err != nil {
		t.Fatalf("PruneRawEventPayloads: %v", err)
	}
	if pruned != 2 {
		t.Errorf("pruned = %d, want 2 (the two superseded snapshots)", pruned)
	}

	// The survivor is the newest one.
	if got := rawPayloadFor(t, store, "cursor", "cursor-ide"); got == "{}" {
		t.Fatal("newest limit_snapshot payload was blanked")
	}
	var blanked int
	if err := store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM usage_raw_events WHERE source_payload = '{}'`,
	).Scan(&blanked); err != nil {
		t.Fatalf("count blanked: %v", err)
	}
	if blanked != 2 {
		t.Errorf("blanked rows = %d, want 2", blanked)
	}
}

// A payload that was already blanked by an older build must not hydrate. This
// is the half of the fix that repairs existing databases, where the damage is
// already on disk and no code change can un-blank it.
func TestDecodeStoredLimitSnapshot_RejectsBlankedPayload(t *testing.T) {
	for _, payload := range []string{"{}", `{"snapshot":{}}`, `{"snapshot":{"provider_id":"cursor","account_id":"cursor-ide"}}`} {
		if _, ok := decodeStoredLimitSnapshot("cursor", "cursor-ide", payload, "2026-08-01T15:30:08Z"); ok {
			t.Errorf("decodeStoredLimitSnapshot(%q) = ok, want rejected as empty", payload)
		}
	}

	usable := `{"snapshot":{"status":"OK","metrics":{"plan_spend":{"remaining":3}}}}`
	if _, ok := decodeStoredLimitSnapshot("cursor", "cursor-ide", usable, "2026-08-01T15:30:08Z"); !ok {
		t.Error("decodeStoredLimitSnapshot rejected a payload carrying real metrics")
	}
}

// The timestamp columns hold time.RFC3339Nano while the retention horizons were
// built with datetime('now', ...), which renders a space separator. The two
// forms only sort consistently when the dates differ: within the same day the
// 'T' outranks the ' ', so a row ingested earlier today compared as newer than
// the horizon and was never pruned. Retention silently meant "previous calendar
// days" rather than "older than N hours".
func TestPruneRawEventPayloads_PrunesWithinTheSameDay(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// Fix the clock at midday so "six hours ago" is unambiguously the same
	// calendar day. With the old comparison this row survived a 1h horizon.
	noon := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return noon.Add(-6 * time.Hour) }

	ingestLimitSnapshot(t, store, "cursor", "cursor-ide", 5.0, noon.Add(-6*time.Hour))
	ingestLimitSnapshot(t, store, "cursor", "cursor-ide", 7.0, noon.Add(-5*time.Hour))

	store.now = func() time.Time { return noon }

	pruned, err := store.PruneRawEventPayloads(ctx, 1, 1000)
	if err != nil {
		t.Fatalf("PruneRawEventPayloads: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1 — a six-hour-old row on the same calendar day must be past a 1h horizon", pruned)
	}
}
