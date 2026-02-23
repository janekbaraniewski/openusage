package daemon

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type ViewRuntime struct {
	clientMu sync.RWMutex
	client   *Client

	socketPath string
	verbose    bool

	ensureMu          sync.Mutex
	lastEnsureAttempt time.Time

	readModelMu         sync.Mutex
	lastReadModelErrLog time.Time

	stateMu sync.RWMutex
	state   DaemonState
}

func NewViewRuntime(
	client *Client,
	socketPath string,
	verbose bool,
) *ViewRuntime {
	return &ViewRuntime{
		client:     client,
		socketPath: strings.TrimSpace(socketPath),
		verbose:    verbose,
		state:      DaemonState{Status: DaemonStatusConnecting},
	}
}

func (r *ViewRuntime) CurrentClient() *Client {
	if r == nil {
		return nil
	}
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	return r.client
}

func (r *ViewRuntime) SetClient(client *Client) {
	if r == nil {
		return
	}
	r.clientMu.Lock()
	r.client = client
	r.clientMu.Unlock()
}

func (r *ViewRuntime) EnsureClient(ctx context.Context) *Client {
	if r == nil {
		return nil
	}
	if client := r.CurrentClient(); client != nil {
		return client
	}
	if strings.TrimSpace(r.socketPath) == "" {
		r.setState(DaemonState{Status: DaemonStatusError, Message: "Socket path not configured"})
		return nil
	}

	r.ensureMu.Lock()
	defer r.ensureMu.Unlock()

	if client := r.CurrentClient(); client != nil {
		return client
	}
	if !r.lastEnsureAttempt.IsZero() && time.Since(r.lastEnsureAttempt) < 1200*time.Millisecond {
		return nil
	}
	r.lastEnsureAttempt = time.Now()

	ensureCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	client, err := EnsureRunning(ensureCtx, r.socketPath, r.verbose)
	if err != nil {
		r.setState(ClassifyEnsureError(err))
		return nil
	}
	r.setState(DaemonState{Status: DaemonStatusRunning})
	r.SetClient(client)
	return client
}

func (r *ViewRuntime) setState(state DaemonState) {
	if r == nil {
		return
	}
	r.stateMu.Lock()
	r.state = state
	r.stateMu.Unlock()
}

func (r *ViewRuntime) State() DaemonState {
	if r == nil {
		return DaemonState{Status: DaemonStatusUnknown}
	}
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	return r.state
}

func (r *ViewRuntime) ResetEnsureThrottle() {
	if r == nil {
		return
	}
	r.ensureMu.Lock()
	r.lastEnsureAttempt = time.Time{}
	r.ensureMu.Unlock()
	r.SetClient(nil)
}

func (r *ViewRuntime) ReadWithFallback(ctx context.Context) map[string]core.UsageSnapshot {
	if r == nil {
		return nil
	}

	client := r.CurrentClient()
	if client == nil {
		client = r.EnsureClient(ctx)
	}

	snaps, err := r.fetchReadModel(ctx, client, ReadModelRequest{})
	if err != nil {
		r.throttledLogError(err)
		return nil
	}
	return snaps
}

func (r *ViewRuntime) fetchReadModel(
	ctx context.Context,
	client *Client,
	request ReadModelRequest,
) (map[string]core.UsageSnapshot, error) {
	if client == nil {
		return nil, errDaemonUnavailable
	}

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	snaps, err := client.ReadModel(readCtx, request)
	cancel()

	if err == nil {
		r.setState(DaemonState{Status: DaemonStatusRunning})
		return snaps, nil
	}

	r.SetClient(nil)
	recovered := r.EnsureClient(ctx)
	if recovered == nil {
		return nil, err
	}

	retryCtx, retryCancel := context.WithTimeout(ctx, 5*time.Second)
	snaps, err = recovered.ReadModel(retryCtx, request)
	retryCancel()
	return snaps, err
}

func (r *ViewRuntime) throttledLogError(err error) {
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

func StartBroadcaster(
	ctx context.Context,
	rt *ViewRuntime,
	refreshInterval time.Duration,
	handler SnapshotHandler,
	stateHandler StateHandler,
) {
	interval := refreshInterval / 3
	if interval <= 0 {
		interval = 4 * time.Second
	}
	interval = max(1*time.Second, min(5*time.Second, interval))

	emitState := func() {
		if stateHandler != nil {
			stateHandler(rt.State())
		}
	}

	go func() {
		emitState()
		if warmUp(ctx, rt, handler, emitState) {
			return
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				snaps := rt.ReadWithFallback(ctx)
				emitState()
				if len(snaps) > 0 {
					handler(snaps)
				}
			}
		}
	}()
}

func warmUp(ctx context.Context, rt *ViewRuntime, handler SnapshotHandler, emitState func()) (cancelled bool) {
	snaps := rt.ReadWithFallback(ctx)
	emitState()
	if len(snaps) > 0 {
		handler(snaps)
		if SnapshotsHaveUsableData(snaps) {
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
			snaps := rt.ReadWithFallback(ctx)
			emitState()
			if len(snaps) == 0 {
				continue
			}
			handler(snaps)
			if SnapshotsHaveUsableData(snaps) {
				return false
			}
		}
	}
	return false
}
