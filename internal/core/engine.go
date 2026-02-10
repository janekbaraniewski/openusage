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
	providers map[string]QuotaProvider // keyed by provider ID
	accounts  []AccountConfig
	snapshots map[string]QuotaSnapshot // keyed by account ID
	interval  time.Duration
	timeout   time.Duration

	onUpdate func(map[string]QuotaSnapshot)
}

func NewEngine(interval time.Duration) *Engine {
	return &Engine{
		providers: make(map[string]QuotaProvider),
		snapshots: make(map[string]QuotaSnapshot),
		interval:  interval,
		timeout:   5 * time.Second,
	}
}

func (e *Engine) RegisterProvider(p QuotaProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providers[p.ID()] = p
}

func (e *Engine) SetAccounts(accounts []AccountConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.accounts = accounts
}

func (e *Engine) OnUpdate(fn func(map[string]QuotaSnapshot)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onUpdate = fn
}

func (e *Engine) Snapshots() map[string]QuotaSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]QuotaSnapshot, len(e.snapshots))
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
		snapshot QuotaSnapshot
	}, len(accounts))

	for _, acct := range accounts {
		wg.Add(1)
		go func(a AccountConfig) {
			defer wg.Done()

			e.mu.RLock()
			provider, ok := e.providers[a.Provider]
			e.mu.RUnlock()

			var snap QuotaSnapshot
			if !ok {
				snap = QuotaSnapshot{
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
					snap = QuotaSnapshot{
						ProviderID: a.Provider,
						AccountID:  a.ID,
						Timestamp:  time.Now(),
						Status:     StatusError,
						Message:    err.Error(),
					}
				}
			}

			results <- struct {
				id       string
				snapshot QuotaSnapshot
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
	snaps := make(map[string]QuotaSnapshot, len(e.snapshots))
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
