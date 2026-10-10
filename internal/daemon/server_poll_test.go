package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/kimi_cli"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func TestPollSnapshotsForIngestExcludesUnchangedCachedResults(t *testing.T) {
	results := []pollProviderResult{
		{
			accountID:    "cached",
			snapshot:     core.UsageSnapshot{ProviderID: "codex", AccountID: "cached"},
			shouldIngest: false,
		},
		{
			accountID:    "changed",
			snapshot:     core.UsageSnapshot{ProviderID: "openai", AccountID: "changed"},
			shouldIngest: true,
		},
	}

	got := pollSnapshotsForIngest(results)
	if _, ok := got["cached"]; ok {
		t.Fatal("unchanged cached snapshot should not be re-ingested")
	}
	if _, ok := got["changed"]; !ok {
		t.Fatal("changed snapshot should be ingested")
	}
}

type dueKimiProvider struct {
	core.UsageProvider
	changed bool
	calls   int
}

func (p *dueKimiProvider) HasChanged(core.AccountConfig, time.Time) (bool, error) {
	return p.changed, nil
}

func (p *dueKimiProvider) Fetch(_ context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	p.calls++
	snap := core.NewUsageSnapshot("kimi_cli", acct.ID)
	snap.Timestamp, snap.Status = time.Now(), core.StatusOK
	snap.Attributes["quota_state"] = "unavailable"
	snap.Diagnostics["quota"] = "access token expired"
	return snap, nil
}

func TestKimiDuePollBypassesLocalBackoff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := config.DefaultConfig()
	cfg.AutoDetect = false
	cfg.Accounts = []core.AccountConfig{{ID: "kimi", Provider: "kimi_cli", Auth: "local"}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	store, err := telemetry.OpenStore(filepath.Join(home, "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := &dueKimiProvider{UsageProvider: kimi_cli.New(), changed: true}
	s := &Service{
		store: store, quotaIngest: telemetry.NewQuotaSnapshotIngestor(store),
		providerByID:  map[string]core.UsageProvider{"kimi_cli": p},
		pollScheduler: newPollScheduler(30 * time.Second),
		pollState: map[string]*providerPollState{"kimi": {
			lastFetchAt: time.Now(), hasSnap: true,
			lastSnap: core.NewUsageSnapshot("kimi_cli", "kimi"),
		}},
		logThrottle: core.NewLogThrottle(10, time.Minute),
	}
	s.pollScheduler.ShouldPoll("kimi", true)
	for i := 0; i < 25; i++ {
		s.pollScheduler.RecordPoll("kimi", false)
	}
	if s.pollScheduler.ShouldPoll("kimi", true) {
		t.Fatal("fixture should be in adaptive backoff")
	}
	s.pollProviders(context.Background())
	if p.calls != 1 {
		t.Fatalf("due Kimi quota was skipped: fetches=%d", p.calls)
	}
	p.changed = false
	s.pollProviders(context.Background())
	if p.calls != 1 {
		t.Fatal("unchanged Kimi quota should stay cached")
	}
}
