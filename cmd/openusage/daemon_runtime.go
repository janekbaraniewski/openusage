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

		// Return seeded snapshots with a progress/error hint so splash can render
		// an actionable status while the daemon recovers.
		return seedSnapshotsForAccounts(request.Accounts, syncStatusMessage(err))
	}

	r.readModelMu.RLock()
	snaps = stabilizeReadModelSnapshots(snaps, r.lastReadModelGood)
	r.readModelMu.RUnlock()

	r.readModelMu.Lock()
	r.lastReadModelGood = cloneSnapshotsMap(snaps)
	r.readModelMu.Unlock()
	return snaps
}

func syncStatusMessage(err error) string {
	if err == nil {
		return "Connecting to telemetry daemon..."
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "Connecting to telemetry daemon..."
	}
	if idx := strings.Index(msg, "\n"); idx > 0 {
		msg = strings.TrimSpace(msg[:idx])
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "not installed"):
		return "Telemetry daemon not installed. Run: openusage telemetry daemon install"
	case strings.Contains(lower, "declined"):
		return "Telemetry daemon installation declined."
	case strings.Contains(lower, "out of date"), strings.Contains(lower, "upgrade"):
		return "Upgrading telemetry daemon..."
	case strings.Contains(lower, "did not become ready"), strings.Contains(lower, "unavailable"):
		return "Waiting for telemetry daemon..."
	default:
		return "Connecting to telemetry daemon..."
	}
}

func startDaemonViewBroadcaster(
	ctx context.Context,
	program *tea.Program,
	runtime *daemonViewRuntime,
	refreshInterval time.Duration,
) {
	interval := refreshInterval / 3
	if interval <= 0 {
		interval = 4 * time.Second
	}
	if interval < 1*time.Second {
		interval = 1 * time.Second
	}
	if interval > 5*time.Second {
		interval = 5 * time.Second
	}

	go func() {
		initial := runtime.readWithFallback(ctx)
		if len(initial) > 0 {
			program.Send(tui.SnapshotsMsg(initial))
		}
		if !snapshotsHaveUsableData(initial) {
			warmTicker := time.NewTicker(1 * time.Second)
			defer warmTicker.Stop()
			for attempts := 0; attempts < 8; attempts++ {
				select {
				case <-ctx.Done():
					return
				case <-warmTicker.C:
					snaps := runtime.readWithFallback(ctx)
					if len(snaps) == 0 {
						continue
					}
					program.Send(tui.SnapshotsMsg(snaps))
					if snapshotsHaveUsableData(snaps) {
						attempts = 8
					}
				}
			}
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

func snapshotsHaveUsableData(snaps map[string]core.UsageSnapshot) bool {
	if len(snaps) == 0 {
		return false
	}
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
