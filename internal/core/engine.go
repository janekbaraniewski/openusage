package core

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type Engine struct {
	mu        sync.RWMutex
	providers map[string]UsageProvider // keyed by provider ID
	accounts  []AccountConfig
	snapshots map[string]UsageSnapshot // keyed by account ID
	interval  time.Duration
	timeout   time.Duration
	modelNorm ModelNormalizationConfig

	onUpdate func(map[string]UsageSnapshot)
}

func NewEngine(interval time.Duration) *Engine {
	return &Engine{
		providers: make(map[string]UsageProvider),
		snapshots: make(map[string]UsageSnapshot),
		interval:  interval,
		timeout:   5 * time.Second,
		modelNorm: DefaultModelNormalizationConfig(),
	}
}

func (e *Engine) RegisterProvider(p UsageProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providers[p.ID()] = p
}

func (e *Engine) SetAccounts(accounts []AccountConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.accounts = accounts
}

func (e *Engine) SetModelNormalizationConfig(cfg ModelNormalizationConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.modelNorm = NormalizeModelNormalizationConfig(cfg)
}

// AddAccount appends an account (if not already present) and triggers a refresh for it.
func (e *Engine) AddAccount(acct AccountConfig) {
	e.mu.Lock()
	for _, existing := range e.accounts {
		if existing.ID == acct.ID {
			// Update token for existing account
			for i := range e.accounts {
				if e.accounts[i].ID == acct.ID {
					e.accounts[i].Token = acct.Token
					break
				}
			}
			e.mu.Unlock()
			go e.RefreshAll(context.Background())
			return
		}
	}
	e.accounts = append(e.accounts, acct)
	e.mu.Unlock()
	go e.RefreshAll(context.Background())
}

func (e *Engine) OnUpdate(fn func(map[string]UsageSnapshot)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onUpdate = fn
}

func (e *Engine) Snapshots() map[string]UsageSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]UsageSnapshot, len(e.snapshots))
	for k, v := range e.snapshots {
		out[k] = v
	}
	return out
}

func (e *Engine) RefreshAll(ctx context.Context) {
	e.mu.RLock()
	accounts := make([]AccountConfig, len(e.accounts))
	copy(accounts, e.accounts)
	e.mu.RUnlock()

	var wg sync.WaitGroup
	results := make(chan struct {
		id       string
		snapshot UsageSnapshot
	}, len(accounts))

	for _, acct := range accounts {
		wg.Add(1)
		go func(a AccountConfig) {
			defer wg.Done()

			e.mu.RLock()
			provider, ok := e.providers[a.Provider]
			modelNorm := e.modelNorm
			e.mu.RUnlock()

			var snap UsageSnapshot
			if !ok {
				snap = UsageSnapshot{
					ProviderID: a.Provider,
					AccountID:  a.ID,
					Timestamp:  time.Now(),
					Status:     StatusError,
					Message:    fmt.Sprintf("no provider adapter registered for %q", a.Provider),
				}
			} else {
				fetchCtx, cancel := context.WithTimeout(ctx, e.timeout)
				defer cancel()

				var err error
				snap, err = provider.Fetch(fetchCtx, a)
				if err != nil {
					snap = UsageSnapshot{
						ProviderID: a.Provider,
						AccountID:  a.ID,
						Timestamp:  time.Now(),
						Status:     StatusError,
						Message:    err.Error(),
					}
				}
				snap = NormalizeUsageSnapshotWithConfig(snap, modelNorm)
				missing := provider.DashboardWidget().MissingMetrics(snap)
				if len(missing) > 0 {
					snap.SetDiagnostic("widget_missing_metrics", fmt.Sprintf("%v", missing))
				}
			}
			snap = NormalizeUsageSnapshotWithConfig(snap, modelNorm)

			results <- struct {
				id       string
				snapshot UsageSnapshot
			}{a.ID, snap}
		}(acct)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		e.mu.Lock()
		e.snapshots[r.id] = r.snapshot
		e.mu.Unlock()
	}

	e.mu.RLock()
	fn := e.onUpdate
	snaps := make(map[string]UsageSnapshot, len(e.snapshots))
	for k, v := range e.snapshots {
		snaps[k] = v
	}
	e.mu.RUnlock()

	if fn != nil {
		fn(snaps)
	}
}

func (e *Engine) Run(ctx context.Context) {
	e.RefreshAll(ctx)

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("engine: context cancelled, stopping refresh loop")
			return
		case <-ticker.C:
			e.RefreshAll(ctx)
		}
	}
}
