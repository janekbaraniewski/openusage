package main

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

const (
	defaultCodexSessionsDir     = "~/.codex/sessions"
	defaultClaudeProjectsDir    = "~/.claude/projects"
	defaultClaudeProjectsAltDir = "~/.config/claude/projects"
	defaultOpenCodeDBPath       = "~/.local/share/opencode/opencode.db"
)

type appTelemetryRuntime struct {
	dbPath        string
	quotaIngestor *telemetry.QuotaSnapshotIngestor
	providerLinks map[string]string

	templateMu          sync.RWMutex
	lastTemplates       map[string]core.UsageSnapshot
	readModelMu         sync.RWMutex
	lastReadModelGood   map[string]core.UsageSnapshot
	lastReadModelErrLog time.Time
}

func startAppTelemetryRuntime(
	ctx context.Context,
	refreshInterval time.Duration,
	providerLinks map[string]string,
) (*appTelemetryRuntime, error) {
	dbPath, err := telemetry.DefaultDBPath()
	if err != nil {
		dbPath = filepath.Join(".", "telemetry.db")
	}
	spoolDir, err := telemetry.DefaultSpoolDir()
	if err != nil {
		spoolDir = filepath.Join(".", "telemetry-spool")
	}

	store, err := telemetry.OpenStore(dbPath)
	if err != nil {
		return nil, err
	}

	spool := telemetry.NewSpool(spoolDir)
	pipeline := telemetry.NewPipeline(store, spool)

	collectors := make([]telemetry.Collector, 0)
	for _, source := range providers.AllTelemetrySources() {
		opts := defaultTelemetryOptionsForSource(source.System())
		collectors = append(collectors, telemetry.NewSourceCollector(source, opts, ""))
	}
	autoCollector := telemetry.NewAutoCollector(collectors, pipeline, 0)
	quotaIngestor := telemetry.NewQuotaSnapshotIngestor(store)

	interval := refreshInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if interval > 20*time.Second {
		interval = 20 * time.Second
	}
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	go func() {
		autoCollector.Run(ctx, interval, func(result telemetry.AutoCollectResult, collectErr error) {
			if collectErr != nil {
				log.Printf("telemetry auto-collect error: %v", collectErr)
				return
			}
			if len(result.CollectorErr) > 0 {
				for _, warning := range result.CollectorErr {
					log.Printf("telemetry collector warning: %s", warning)
				}
			}
			if len(result.FlushErr) > 0 {
				for _, warning := range result.FlushErr {
					log.Printf("telemetry flush warning: %s", warning)
				}
			}
		})
	}()
	go func() {
		<-ctx.Done()
		_ = store.Close()
	}()

	return &appTelemetryRuntime{
		dbPath:            dbPath,
		quotaIngestor:     quotaIngestor,
		providerLinks:     providerLinks,
		lastTemplates:     map[string]core.UsageSnapshot{},
		lastReadModelGood: map[string]core.UsageSnapshot{},
	}, nil
}

func defaultTelemetryOptionsForSource(sourceSystem string) shared.TelemetryCollectOptions {
	return telemetryOptionsForSource(
		sourceSystem,
		defaultCodexSessionsDir,
		defaultClaudeProjectsDir,
		defaultClaudeProjectsAltDir,
		nil,
		"",
		defaultOpenCodeDBPath,
	)
}

func applyTelemetryReadModel(
	ctx context.Context,
	dbPath string,
	snaps map[string]core.UsageSnapshot,
	providerLinks map[string]string,
) (map[string]core.UsageSnapshot, error) {
	if len(snaps) == 0 {
		return snaps, nil
	}
	readModelCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	merged, err := telemetry.ApplyCanonicalTelemetryViewWithOptions(readModelCtx, dbPath, snaps, telemetry.ReadModelOptions{
		ProviderLinks: providerLinks,
	})
	if err != nil {
		return snaps, err
	}
	return merged, nil
}

func (r *appTelemetryRuntime) viewWithFallback(ctx context.Context, snaps map[string]core.UsageSnapshot) map[string]core.UsageSnapshot {
	if r == nil {
		return snaps
	}

	if len(snaps) > 0 {
		r.templateMu.Lock()
		r.lastTemplates = telemetrySeedSnapshots(snaps)
		r.templateMu.Unlock()
	}

	base := r.currentTemplates(snaps)
	if len(base) == 0 {
		return base
	}

	merged, err := applyTelemetryReadModel(ctx, r.dbPath, base, r.providerLinks)
	if err != nil {
		shouldLog := false
		r.readModelMu.Lock()
		if time.Since(r.lastReadModelErrLog) > 2*time.Second {
			r.lastReadModelErrLog = time.Now()
			shouldLog = true
		}
		r.readModelMu.Unlock()
		if shouldLog {
			log.Printf("telemetry read-model error: %v", err)
		}
		r.readModelMu.RLock()
		if len(r.lastReadModelGood) > 0 {
			cached := cloneSnapshotsMap(r.lastReadModelGood)
			r.readModelMu.RUnlock()
			return cached
		}
		r.readModelMu.RUnlock()
		return base
	}

	r.readModelMu.Lock()
	r.lastReadModelGood = cloneSnapshotsMap(merged)
	r.readModelMu.Unlock()
	return merged
}

func (r *appTelemetryRuntime) currentTemplates(fallback map[string]core.UsageSnapshot) map[string]core.UsageSnapshot {
	if r == nil {
		return telemetrySeedSnapshots(fallback)
	}

	r.templateMu.RLock()
	defer r.templateMu.RUnlock()
	if len(r.lastTemplates) > 0 {
		return cloneSnapshotsMap(r.lastTemplates)
	}
	return telemetrySeedSnapshots(fallback)
}

func telemetrySeedSnapshots(snaps map[string]core.UsageSnapshot) map[string]core.UsageSnapshot {
	if len(snaps) == 0 {
		return map[string]core.UsageSnapshot{}
	}
	out := make(map[string]core.UsageSnapshot, len(snaps))
	now := time.Now().UTC()

	for accountID, snap := range snaps {
		providerID := strings.TrimSpace(snap.ProviderID)
		effectiveAccountID := strings.TrimSpace(snap.AccountID)
		if effectiveAccountID == "" {
			effectiveAccountID = strings.TrimSpace(accountID)
		}
		out[accountID] = core.UsageSnapshot{
			ProviderID:  providerID,
			AccountID:   effectiveAccountID,
			Timestamp:   now,
			Status:      core.StatusUnknown,
			Metrics:     map[string]core.Metric{},
			Resets:      map[string]time.Time{},
			Attributes:  map[string]string{},
			Diagnostics: map[string]string{},
			Raw:         map[string]string{},
			DailySeries: map[string][]core.TimePoint{},
		}
	}
	return out
}

func startTelemetryViewBroadcaster(
	ctx context.Context,
	engine *core.Engine,
	program *tea.Program,
	runtime *appTelemetryRuntime,
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
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if runtime == nil {
					continue
				}
				snaps := runtime.viewWithFallback(ctx, engine.Snapshots())
				if len(snaps) == 0 {
					continue
				}
				program.Send(tui.SnapshotsMsg(snaps))
			}
		}
	}()
}

func (r *appTelemetryRuntime) ingestProviderSnapshots(ctx context.Context, snaps map[string]core.UsageSnapshot) {
	if r == nil || r.quotaIngestor == nil || len(snaps) == 0 {
		return
	}
	ingestCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()

	if err := r.quotaIngestor.Ingest(ingestCtx, snaps); err != nil {
		log.Printf("telemetry limit snapshot ingest error: %v", err)
	}
}

func cloneSnapshotsMap(in map[string]core.UsageSnapshot) map[string]core.UsageSnapshot {
	out := make(map[string]core.UsageSnapshot, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
