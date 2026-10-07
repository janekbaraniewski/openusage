package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func TestReadModelCacheIntervalRespectsPollInterval(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{name: "default", in: 0, want: 30 * time.Second},
		{name: "minimum", in: time.Second, want: 5 * time.Second},
		{name: "normal", in: 30 * time.Second, want: 30 * time.Second},
		{name: "long", in: time.Hour, want: time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readModelCacheInterval(tt.in); got != tt.want {
				t.Fatalf("readModelCacheInterval(%s) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestCachedReadModelRefreshOnNewDataOrExpiry(t *testing.T) {
	now := time.Date(2026, time.July, 17, 14, 0, 0, 0, time.UTC)

	if shouldRefreshCachedReadModel(now.Add(-30*time.Second), 7, 7, now) {
		t.Fatal("unchanged data should reuse a recent cache entry")
	}
	if !shouldRefreshCachedReadModel(now.Add(-time.Minute), 7, 7, now) {
		t.Fatal("time-dependent state must refresh even without new ingest")
	}
	if shouldRefreshCachedReadModel(now.Add(-time.Second), 7, 8, now) {
		t.Fatal("new data must respect the refresh debounce window")
	}
	if !shouldRefreshCachedReadModel(now.Add(-3*time.Second), 7, 8, now) {
		t.Fatal("new data should refresh after the debounce window")
	}
}

func TestCachedKimiQuotaAgesWithoutMutatingCache(t *testing.T) {
	now := time.Now().UTC()
	old := core.NewUsageSnapshot("kimi_cli", "kimi")
	old.Timestamp = now.Add(-2 * time.Hour)
	old.Attributes["quota_state"] = "fresh"
	old.Attributes["quota_fetched_at"] = old.Timestamp.Format(time.RFC3339Nano)
	old.Metrics["usage_monthly"] = core.Metric{Used: ptr(71), Limit: ptr(100)}
	input := map[string]core.UsageSnapshot{"kimi": old, "codex": {ProviderID: "codex"}}
	got := ageKimiQuotaSnapshots(input, now)
	if got["kimi"].Attributes["quota_state"] != "stale" || got["kimi"].Diagnostics["quota"] == "" || *got["kimi"].Metrics["usage_monthly"].Used != 71 {
		t.Fatalf("old quota still looks live: %+v", got["kimi"])
	}
	if input["kimi"].Attributes["quota_state"] != "fresh" || len(input["kimi"].Diagnostics) != 0 {
		t.Fatal("response aging mutated the immutable cache")
	}
	old.Attributes["quota_fetched_at"] = now.Add(-time.Minute).Format(time.RFC3339Nano)
	input["kimi"] = old
	if got := ageKimiQuotaSnapshots(input, now); got["kimi"].Attributes["quota_state"] != "fresh" {
		t.Fatal("recent valid quota was marked stale")
	}
}

func TestReadModelRefreshAfterQuotaFailureAndPayloadMaintenance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dbPath := filepath.Join(home, "telemetry.db")
	store, err := telemetry.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	good := core.NewUsageSnapshot("kimi_cli", "kimi")
	good.Timestamp, good.Status = now.Add(-3*time.Hour), core.StatusOK
	good.Attributes["quota_state"] = "fresh"
	good.Attributes["quota_fetched_at"] = good.Timestamp.Format(time.RFC3339Nano)
	good.Metrics["usage_monthly"] = core.Metric{Used: ptr(71), Limit: ptr(100), Unit: "%", Window: "30d"}
	failed := core.NewUsageSnapshot("kimi_cli", "kimi")
	failed.Timestamp, failed.Status = now.Add(-time.Hour), core.StatusOK
	failed.Metrics["total_sessions"] = core.Metric{Used: ptr(195), Unit: "sessions"}
	failed.Attributes["quota_state"] = "unavailable"
	failed.Diagnostics["quota"] = "access token expired"
	req := ReadModelRequest{Accounts: []ReadModelAccount{{AccountID: "kimi", ProviderID: "kimi_cli"}}, TimeWindow: core.TimeWindow7d}
	for _, snap := range []core.UsageSnapshot{good, failed} {
		if err := telemetry.NewQuotaSnapshotIngestor(store).Ingest(context.Background(), map[string]core.UsageSnapshot{"kimi": snap}); err != nil {
			t.Fatal(err)
		}
		if snap.Attributes["quota_state"] == "fresh" {
			probe := &Service{cfg: Config{DBPath: dbPath}}
			cold, err := probe.computeReadModel(context.Background(), req)
			if err != nil || cold["kimi"].Attributes["quota_state"] != "stale" {
				t.Fatalf("cold read advertised an old quota as fresh: %+v err=%v", cold, err)
			}
		}
	}
	if _, err := store.PruneRawEventPayloads(context.Background(), 0, 1000); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := &Service{cfg: Config{DBPath: dbPath}, ctx: ctx, rmCache: newReadModelCache(), logThrottle: core.NewLogThrottle(10, time.Minute)}
	key := ReadModelRequestKey(req)
	svc.dataVersion.Store(7)
	svc.rmCache.set(key, map[string]core.UsageSnapshot{"kimi": good}, 7)
	entry := svc.rmCache.entries[key]
	entry.updatedAt = now.Add(-2 * time.Hour)
	svc.rmCache.entries[key] = entry
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	svc.handleReadModel(w, httptest.NewRequest(http.MethodPost, "/v1/read-model", strings.NewReader(string(body))))
	var response ReadModelResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Snapshots["kimi"].Attributes["quota_state"] != "stale" {
		t.Fatal("first cached response falsely advertised old quota as fresh")
	}
	if entry.snapshots["kimi"].Attributes["quota_state"] != "fresh" {
		t.Fatal("serving a response mutated the stored cache entry")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshots, _, _, _ := svc.rmCache.get(key)
		snap := snapshots["kimi"]
		if snap.Diagnostics["quota"] == "access token expired" {
			if snap.Attributes["quota_state"] != "stale" || snap.Metrics["usage_monthly"].Used == nil || *snap.Metrics["usage_monthly"].Used != 71 || *snap.Metrics["total_sessions"].Used != 195 {
				t.Fatalf("post-maintenance refresh lost quota/current activity: %+v", snap)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expired cache did not refresh without a new data version")
}

func TestReadModelCacheTracksDataVersion(t *testing.T) {
	cache := newReadModelCache()
	cache.set("request", map[string]core.UsageSnapshot{
		"codex": {ProviderID: "codex"},
	}, 11)

	_, _, version, ok := cache.get("request")
	if !ok {
		t.Fatal("expected cached read model")
	}
	if version != 11 {
		t.Fatalf("cached data version = %d, want 11", version)
	}
}

func TestMarkDataIngestedAdvancesVersion(t *testing.T) {
	svc := &Service{}
	svc.markDataIngested()
	svc.markDataIngested()

	if !svc.dataIngested.Load() {
		t.Fatal("data ingested flag should be set")
	}
	if got := svc.dataVersion.Load(); got != 2 {
		t.Fatalf("data version = %d, want 2", got)
	}
}
