package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// Exporter periodically pushes usage snapshots to a remote hub.
type Exporter struct {
	target   string
	machine  string
	interval time.Duration
	http     *http.Client
	mu       sync.RWMutex
	latest   []core.UsageSnapshot
}

// New creates a new Exporter from the given ExportConfig.
// Returns an error if cfg.Target is empty.
func New(cfg config.ExportConfig) (*Exporter, error) {
	if cfg.Target == "" {
		return nil, fmt.Errorf("exporter: target URL must not be empty")
	}

	machine := cfg.MachineName
	if machine == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("exporter: resolving hostname: %w", err)
		}
		machine = hostname
	}

	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	if cfg.IntervalSeconds <= 0 {
		interval = 60 * time.Second
	}

	return &Exporter{
		target:   cfg.Target,
		machine:  machine,
		interval: interval,
		http:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Ingest replaces the latest snapshot list with the values from the given map.
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
func (e *Exporter) Start(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.mu.RLock()
			snaps := make([]core.UsageSnapshot, len(e.latest))
			copy(snaps, e.latest)
			e.mu.RUnlock()

			if len(snaps) == 0 {
				continue
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
