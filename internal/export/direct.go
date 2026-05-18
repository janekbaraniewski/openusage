package export

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

// fetchTimeout caps a single provider Fetch() call. Matches the daemon's
// poll loop (`server_poll.go`) so export results look the same as a daemon
// snapshot.
const fetchTimeout = 8 * time.Second

// directCollector polls providers synchronously, the same way the daemon's
// poll loop does but without ingesting into the telemetry store.
type directCollector struct {
	providers    []core.UsageProvider
	loadConfig   func() (config.Config, error)
	resolveAccts func(*config.Config) []core.AccountConfig
}

func newDirectCollector() *directCollector {
	return &directCollector{
		providers:    providers.AllProviders(),
		loadConfig:   config.Load,
		resolveAccts: daemon.ResolveAccounts,
	}
}

// collect resolves accounts (manual + auto-detected + credentials) and runs
// one Fetch() per account in parallel. The returned snapshots are sorted by
// (provider_id, account_id) for deterministic output.
func (c *directCollector) collect(ctx context.Context) ([]core.UsageSnapshot, error) {
	cfg, err := c.loadConfig()
	if err != nil {
		return nil, fmt.Errorf("export: loading config: %w", err)
	}

	accounts := c.resolveAccts(&cfg)
	if len(accounts) == 0 {
		return nil, nil
	}

	providerByID := make(map[string]core.UsageProvider, len(c.providers))
	for _, p := range c.providers {
		providerByID[p.ID()] = p
	}

	return collectSnapshots(ctx, accounts, providerByID, cfg.ModelNormalization, time.Now), nil
}

// collectSnapshots is the pure fan-out helper. Exposed so tests can drive it
// with synthetic providers and accounts without touching disk-backed config.
func collectSnapshots(
	ctx context.Context,
	accounts []core.AccountConfig,
	providerByID map[string]core.UsageProvider,
	modelNorm core.ModelNormalizationConfig,
	now func() time.Time,
) []core.UsageSnapshot {
	if now == nil {
		now = time.Now
	}

	type fetchResult struct {
		snap core.UsageSnapshot
	}
	results := make(chan fetchResult, len(accounts))
	var wg sync.WaitGroup

	for _, acct := range accounts {
		wg.Add(1)
		go func(account core.AccountConfig) {
			defer wg.Done()

			provider, ok := providerByID[account.Provider]
			if !ok {
				results <- fetchResult{snap: core.UsageSnapshot{
					ProviderID: account.Provider,
					AccountID:  account.ID,
					Timestamp:  now().UTC(),
					Status:     core.StatusError,
					Message:    fmt.Sprintf("export: no provider adapter registered for %q", account.Provider),
				}}
				return
			}

			fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
			defer cancel()

			snap, fetchErr := provider.Fetch(fetchCtx, account)
			if fetchErr != nil {
				snap = core.UsageSnapshot{
					ProviderID: account.Provider,
					AccountID:  account.ID,
					Timestamp:  now().UTC(),
					Status:     core.StatusError,
					Message:    fetchErr.Error(),
				}
			}
			snap = core.NormalizeUsageSnapshotWithConfig(snap, modelNorm)

			results <- fetchResult{snap: snap}
		}(acct)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]core.UsageSnapshot, 0, len(accounts))
	for r := range results {
		out = append(out, r.snap)
	}
	sortSnapshots(out)
	return out
}

// sortSnapshots orders by (provider_id, account_id) so the encoder produces
// deterministic JSON regardless of fan-out scheduling.
func sortSnapshots(snaps []core.UsageSnapshot) {
	sort.Slice(snaps, func(i, j int) bool {
		if snaps[i].ProviderID != snaps[j].ProviderID {
			return snaps[i].ProviderID < snaps[j].ProviderID
		}
		return snaps[i].AccountID < snaps[j].AccountID
	})
}
