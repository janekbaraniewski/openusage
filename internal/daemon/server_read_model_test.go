package daemon

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
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

func TestCachedReadModelRefreshRequiresNewDataAndMinimumAge(t *testing.T) {
	now := time.Date(2026, time.July, 17, 14, 0, 0, 0, time.UTC)

	if shouldRefreshCachedReadModel(now.Add(-time.Minute), 7, 7, now) {
		t.Fatal("unchanged data version must not refresh an old cache entry")
	}
	if shouldRefreshCachedReadModel(now.Add(-time.Second), 7, 8, now) {
		t.Fatal("new data must respect the refresh debounce window")
	}
	if !shouldRefreshCachedReadModel(now.Add(-3*time.Second), 7, 8, now) {
		t.Fatal("new data should refresh after the debounce window")
	}
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
