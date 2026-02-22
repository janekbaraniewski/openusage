package main

import (
	"context"
	"log"
	"path/filepath"
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
}

func startAppTelemetryRuntime(ctx context.Context, refreshInterval time.Duration) (*appTelemetryRuntime, error) {
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
		dbPath:        dbPath,
		quotaIngestor: quotaIngestor,
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

func applyTelemetryOverlay(ctx context.Context, dbPath string, snaps map[string]core.UsageSnapshot) map[string]core.UsageSnapshot {
	if len(snaps) == 0 {
		return snaps
	}
	overlayCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	merged, err := telemetry.ApplyCanonicalTelemetryView(overlayCtx, dbPath, snaps)
	if err != nil {
		log.Printf("telemetry read-model error: %v", err)
		return snaps
	}
	return merged
}

func startTelemetryOverlayBroadcaster(
	ctx context.Context,
	engine *core.Engine,
	program *tea.Program,
	dbPath string,
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
				snaps := engine.Snapshots()
				if len(snaps) == 0 {
					continue
				}
				program.Send(tui.SnapshotsMsg(applyTelemetryOverlay(ctx, dbPath, snaps)))
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
