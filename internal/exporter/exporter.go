package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	defaultPushInterval = 60 * time.Second
	defaultPushTimeout  = 10 * time.Second
	// envAuthToken is the environment variable fallback for the hub auth token,
	// used when ExportConfig.AuthToken is empty. Applies symmetrically on the
	// hub side to simplify deployments where the same value is shared.
	envAuthToken = "OPENUSAGE_HUB_TOKEN"
)

// Exporter periodically pushes usage snapshots to a remote hub.
type Exporter struct {
	target    string // trailing slash trimmed
	machine   string
	interval  time.Duration
	authToken string // empty disables Authorization header
	http      *http.Client
	mu        sync.RWMutex
	latest    []core.UsageSnapshot
}

// New creates a new Exporter from the given ExportConfig.
// Returns an error if cfg.Target is empty.
//
// If cfg.AuthToken is empty and the OPENUSAGE_HUB_TOKEN env var is set, the
// env var is used as the Bearer token. This matches the hub-side convention.
func New(cfg config.ExportConfig) (*Exporter, error) {
	target := strings.TrimRight(strings.TrimSpace(cfg.Target), "/")
	if target == "" {
		return nil, fmt.Errorf("exporter: target URL must not be empty")
	}

	machine := strings.TrimSpace(cfg.MachineName)
	if machine == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("exporter: resolving hostname: %w", err)
		}
		machine = hostname
	}

	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	if cfg.IntervalSeconds <= 0 {
		interval = defaultPushInterval
	}

	authToken := strings.TrimSpace(cfg.AuthToken)
	if authToken == "" {
		authToken = strings.TrimSpace(os.Getenv(envAuthToken))
	}

	return &Exporter{
		target:    target,
		machine:   machine,
		interval:  interval,
		authToken: authToken,
		http:      &http.Client{Timeout: defaultPushTimeout},
	}, nil
}

// Ingest replaces the latest snapshot list with the values from the given map.
//
// Calling Ingest with an empty map effectively pauses pushes: the next ticker
// cycle will find len(latest)==0 and skip the HTTP request, leaving the hub's
// last-known state in place until a non-empty Ingest resumes the stream.
func (e *Exporter) Ingest(snaps map[string]core.UsageSnapshot) {
	cloned := make([]core.UsageSnapshot, 0, len(snaps))
	for _, s := range snaps {
		cloned = append(cloned, s)
	}

	e.mu.Lock()
	e.latest = cloned
	e.mu.Unlock()
}

// Start runs the push loop, blocking until ctx is cancelled.
//
// Start performs an immediate push attempt (if latest is non-empty) before
// entering the ticker loop, so users do not have to wait a full interval to
// verify that the hub connection works.
func (e *Exporter) Start(ctx context.Context) {
	e.tick(ctx)

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.tick(ctx)
		}
	}
}

// tick captures a snapshot of latest and pushes it if non-empty. Errors are
// logged and swallowed — the exporter is best-effort and never aborts the loop.
func (e *Exporter) tick(ctx context.Context) {
	e.mu.RLock()
	snaps := make([]core.UsageSnapshot, len(e.latest))
	copy(snaps, e.latest)
	e.mu.RUnlock()

	if len(snaps) == 0 {
		return
	}

	envelope := core.RemoteEnvelope{
		Machine:   e.machine,
		SentAt:    time.Now(),
		Snapshots: snaps,
	}

	if err := e.push(ctx, envelope); err != nil {
		log.Printf("exporter: push: %v", err)
	}
}

func (e *Exporter) push(ctx context.Context, envelope core.RemoteEnvelope) error {
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("exporter: marshaling envelope: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.target+"/v1/push", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("exporter: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.authToken)
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return fmt.Errorf("exporter: sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("exporter: server returned %d", resp.StatusCode)
	}

	return nil
}
