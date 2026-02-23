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

	readModelMu         sync.Mutex
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
		client:     client,
		socketPath: strings.TrimSpace(socketPath),
		verbose:    verbose,
		request:    buildReadModelRequest(accounts, providerLinks),
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
	if strings.TrimSpace(r.socketPath) == "" {
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

	client, err := ensureTelemetryDaemonRunning(ensureCtx, r.socketPath, r.verbose)
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
	r.request = buildReadModelRequest(accounts, providerLinks)
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

func (r *daemonViewRuntime) readWithFallback(ctx context.Context) map[string]core.UsageSnapshot {
	if r == nil {
		return nil
	}
	request := r.readRequest()
	if len(request.Accounts) == 0 {
		return nil
	}

	client := r.currentClient()
	if client == nil {
		client = r.ensureClient(ctx)
	}

	snaps, err := r.fetchReadModel(ctx, client, request)
	if err != nil {
		r.throttledLogError(err)
		return nil
	}
	return snaps
}

func (r *daemonViewRuntime) fetchReadModel(
	ctx context.Context,
	client *telemetryDaemonClient,
	request daemonReadModelRequest,
) (map[string]core.UsageSnapshot, error) {
	if client == nil {
		return nil, fmt.Errorf("telemetry daemon unavailable")
	}

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	snaps, err := client.ReadModel(readCtx, request)
	cancel()

	if err == nil {
		return snaps, nil
	}

	r.setClient(nil)
	recovered := r.ensureClient(ctx)
	if recovered == nil {
		return nil, err
	}

	retryCtx, retryCancel := context.WithTimeout(ctx, 5*time.Second)
	snaps, err = recovered.ReadModel(retryCtx, request)
	retryCancel()
	return snaps, err
}

func (r *daemonViewRuntime) throttledLogError(err error) {
	r.readModelMu.Lock()
	shouldLog := time.Since(r.lastReadModelErrLog) > 2*time.Second
	if shouldLog {
		r.lastReadModelErrLog = time.Now()
	}
	r.readModelMu.Unlock()
	if shouldLog {
		log.Printf("daemon read-model error: %v", err)
	}
}

func buildReadModelRequest(
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
		if source != "" && target != "" {
			links[source] = target
		}
	}
	return daemonReadModelRequest{Accounts: outAccounts, ProviderLinks: links}
}

func startDaemonViewBroadcaster(
	ctx context.Context,
	program *tea.Program,
	rt *daemonViewRuntime,
	refreshInterval time.Duration,
) {
	interval := refreshInterval / 3
	if interval <= 0 {
		interval = 4 * time.Second
	}
	interval = max(1*time.Second, min(5*time.Second, interval))

	go func() {
		if warmUp(ctx, program, rt) {
			return
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if snaps := rt.readWithFallback(ctx); len(snaps) > 0 {
					program.Send(tui.SnapshotsMsg(snaps))
				}
			}
		}
	}()
}

func warmUp(ctx context.Context, program *tea.Program, rt *daemonViewRuntime) (cancelled bool) {
	if snaps := rt.readWithFallback(ctx); len(snaps) > 0 {
		program.Send(tui.SnapshotsMsg(snaps))
		if snapshotsHaveUsableData(snaps) {
			return false
		}
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for attempts := 0; attempts < 8; attempts++ {
		select {
		case <-ctx.Done():
			return true
		case <-ticker.C:
			snaps := rt.readWithFallback(ctx)
			if len(snaps) == 0 {
				continue
			}
			program.Send(tui.SnapshotsMsg(snaps))
			if snapshotsHaveUsableData(snaps) {
				return false
			}
		}
	}
	return false
}

func snapshotsHaveUsableData(snaps map[string]core.UsageSnapshot) bool {
	for _, snap := range snaps {
		if snap.Status != core.StatusUnknown {
			return true
		}
		if len(snap.Metrics) > 0 || len(snap.Resets) > 0 || len(snap.DailySeries) > 0 || len(snap.ModelUsage) > 0 {
			return true
		}
	}
	return false
}
