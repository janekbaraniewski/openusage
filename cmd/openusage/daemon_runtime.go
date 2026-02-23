package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

type daemonViewRuntime struct {
	clientMu sync.RWMutex
	client   *telemetryDaemonClient

	socketPath string
	verbose    bool

	ensureMu          sync.Mutex
	lastEnsureAttempt time.Time

	requestMu sync.RWMutex
	request   daemonReadModelRequest

	readModelMu         sync.RWMutex
	lastReadModelGood   map[string]core.UsageSnapshot
	lastReadModelErrLog time.Time
}

func newDaemonViewRuntime(
	client *telemetryDaemonClient,
	socketPath string,
	verbose bool,
	accounts []core.AccountConfig,
	providerLinks map[string]string,
) *daemonViewRuntime {
	return &daemonViewRuntime{
		client:              client,
		socketPath:          strings.TrimSpace(socketPath),
		verbose:             verbose,
		request:             daemonReadModelRequestFromAccounts(accounts, providerLinks),
		lastReadModelGood:   map[string]core.UsageSnapshot{},
		lastReadModelErrLog: time.Time{},
	}
}

func (r *daemonViewRuntime) currentClient() *telemetryDaemonClient {
	if r == nil {
		return nil
	}
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	return r.client
}

func (r *daemonViewRuntime) setClient(client *telemetryDaemonClient) {
	if r == nil {
		return
	}
	r.clientMu.Lock()
	r.client = client
	r.clientMu.Unlock()
}

func (r *daemonViewRuntime) ensureClient(ctx context.Context) *telemetryDaemonClient {
	if r == nil {
		return nil
	}
	if client := r.currentClient(); client != nil {
		return client
	}

	socketPath := strings.TrimSpace(r.socketPath)
	if socketPath == "" {
		return nil
	}

	r.ensureMu.Lock()
	defer r.ensureMu.Unlock()

	if client := r.currentClient(); client != nil {
		return client
	}

	if !r.lastEnsureAttempt.IsZero() && time.Since(r.lastEnsureAttempt) < 1200*time.Millisecond {
		return nil
	}
	r.lastEnsureAttempt = time.Now()

	ensureCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	client, err := ensureTelemetryDaemonRunning(ensureCtx, socketPath, r.verbose)
	if err != nil {
		return nil
	}
	r.setClient(client)
	return client
}

func (r *daemonViewRuntime) setAccounts(accounts []core.AccountConfig, providerLinks map[string]string) {
	if r == nil {
		return
	}
	r.requestMu.Lock()
	r.request = daemonReadModelRequestFromAccounts(accounts, providerLinks)
	r.requestMu.Unlock()
}

func (r *daemonViewRuntime) readRequest() daemonReadModelRequest {
	if r == nil {
		return daemonReadModelRequest{}
	}
	r.requestMu.RLock()
	defer r.requestMu.RUnlock()
	return r.request
}

func daemonReadModelRequestFromAccounts(
	accounts []core.AccountConfig,
	providerLinks map[string]string,
) daemonReadModelRequest {
	seen := make(map[string]bool, len(accounts))
	outAccounts := make([]daemonReadModelAccount, 0, len(accounts))
	for _, acct := range accounts {
		accountID := strings.TrimSpace(acct.ID)
		providerID := strings.TrimSpace(acct.Provider)
		if accountID == "" || providerID == "" || seen[accountID] {
			continue
		}
		seen[accountID] = true
		outAccounts = append(outAccounts, daemonReadModelAccount{
			AccountID:  accountID,
			ProviderID: providerID,
		})
	}
	links := make(map[string]string, len(providerLinks))
	for source, target := range providerLinks {
		source = strings.ToLower(strings.TrimSpace(source))
		target = strings.ToLower(strings.TrimSpace(target))
		if source == "" || target == "" {
			continue
		}
		links[source] = target
	}
	return daemonReadModelRequest{Accounts: outAccounts, ProviderLinks: links}
}

func (r *daemonViewRuntime) readWithFallback(ctx context.Context) map[string]core.UsageSnapshot {
	if r == nil {
		return map[string]core.UsageSnapshot{}
	}

	request := r.readRequest()
	if len(request.Accounts) == 0 {
		return map[string]core.UsageSnapshot{}
	}

	client := r.currentClient()
	if client == nil {
		client = r.ensureClient(ctx)
	}

	var (
		snaps map[string]core.UsageSnapshot
		err   error
	)

	if client == nil {
		err = fmt.Errorf("telemetry daemon unavailable")
	} else {
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		snaps, err = client.ReadModel(readCtx, request)
		cancel()
		if err != nil {
			r.setClient(nil)
			if recovered := r.ensureClient(ctx); recovered != nil {
				retryCtx, retryCancel := context.WithTimeout(ctx, 5*time.Second)
				snaps, err = recovered.ReadModel(retryCtx, request)
				retryCancel()
				if err == nil {
					client = recovered
				}
			}
		}
	}

	if err != nil {
		shouldLog := false
		r.readModelMu.Lock()
		if time.Since(r.lastReadModelErrLog) > 2*time.Second {
			r.lastReadModelErrLog = time.Now()
			shouldLog = true
		}
		r.readModelMu.Unlock()
		if shouldLog {
			log.Printf("daemon read-model error: %v", err)
		}

		r.readModelMu.RLock()
		if len(r.lastReadModelGood) > 0 {
			cached := cloneSnapshotsMap(r.lastReadModelGood)
			r.readModelMu.RUnlock()
			return cached
		}
		r.readModelMu.RUnlock()

		// Return seeded empty snapshots; UI keeps splash state until real data arrives.
		seed := map[string]core.UsageSnapshot{}
		now := time.Now().UTC()
		for _, account := range request.Accounts {
			seed[account.AccountID] = core.UsageSnapshot{
				ProviderID:  account.ProviderID,
				AccountID:   account.AccountID,
				Timestamp:   now,
				Status:      core.StatusUnknown,
				Message:     "",
				Metrics:     map[string]core.Metric{},
				Resets:      map[string]time.Time{},
				Attributes:  map[string]string{},
				Diagnostics: map[string]string{},
				Raw:         map[string]string{},
				DailySeries: map[string][]core.TimePoint{},
			}
		}
		return seed
	}

	r.readModelMu.RLock()
	snaps = stabilizeReadModelSnapshots(snaps, r.lastReadModelGood)
	r.readModelMu.RUnlock()

	r.readModelMu.Lock()
	r.lastReadModelGood = cloneSnapshotsMap(snaps)
	r.readModelMu.Unlock()
	return snaps
}

func startDaemonViewBroadcaster(
	ctx context.Context,
	program *tea.Program,
	runtime *daemonViewRuntime,
	refreshInterval time.Duration,
) {
	interval := refreshInterval / 3
	if interval <= 0 {
		interval = 8 * time.Second
	}
	if interval < 2*time.Second {
		interval = 2 * time.Second
	}
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}

	go func() {
		initial := runtime.readWithFallback(ctx)
		if len(initial) > 0 {
			program.Send(tui.SnapshotsMsg(initial))
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				snaps := runtime.readWithFallback(ctx)
				if len(snaps) == 0 {
					continue
				}
				program.Send(tui.SnapshotsMsg(snaps))
			}
		}
	}()
}
