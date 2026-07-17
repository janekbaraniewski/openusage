package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func (s *Service) runCollectLoop(ctx context.Context) {
	interval := s.cfg.CollectInterval
	maxInterval := 5 * time.Minute
	consecutiveEmpty := 0

	s.infof("collect_loop_start", "interval=%s", interval)
	s.collectAndFlush(ctx)
	for {
		triggeredByChange := false
		select {
		case <-ctx.Done():
			s.infof("collect_loop_stop", "reason=context_done")
			return
		case <-time.After(interval):
		case <-s.collectNow:
			triggeredByChange = true
		}

		collected := s.collectAndFlush(ctx)
		if collected == 0 {
			// Noisy filesystem notifications must not accelerate periodic
			// backoff; they already received an immediate collection attempt.
			if triggeredByChange {
				continue
			}
			consecutiveEmpty++
			if consecutiveEmpty >= 3 {
				newInterval := interval * 2
				if newInterval > maxInterval {
					newInterval = maxInterval
				}
				if newInterval != interval {
					interval = newInterval
					s.infof("collect_backoff", "interval=%s empty_cycles=%d", interval, consecutiveEmpty)
				}
			}
		} else {
			if consecutiveEmpty > 0 && interval != s.cfg.CollectInterval {
				s.infof("collect_reset", "interval=%s→%s collected=%d", interval, s.cfg.CollectInterval, collected)
			}
			consecutiveEmpty = 0
			interval = s.cfg.CollectInterval
		}
	}
}

// pushToExporter forwards a freshly-computed snapshot set to the remote-hub
// exporter, if one is configured.
//
// Intentionally called from the read-model cache refresh path
// (server_read_model.go), NOT from collectAndFlush. Rationale:
//
//   - collectAndFlush deals with raw telemetry IngestRequests, not the
//     aggregated UsageSnapshot map the hub expects.
//   - The read-model cache already projects raw telemetry into
//     core.UsageSnapshot shape via ApplyCanonicalTelemetryViewWithOptions,
//     which is exactly what the hub consumes.
//   - Piggy-backing on refreshReadModelCacheAsync means the exporter sees the
//     same view the local dashboard would see, with no extra SQL work.
//
// Consequence: daemon-mode exports only flow after the first successful
// read-model refresh, which requires configured accounts. This is documented
// in docs/REMOTE_EXPORTER_DESIGN.md §5.6.
func (s *Service) pushToExporter(_ context.Context, snaps map[string]core.UsageSnapshot) {
	if s.exp == nil || len(snaps) == 0 {
		return
	}
	s.exp.Ingest(snaps)
}

func (s *Service) collectAndFlush(ctx context.Context) int {
	if s == nil {
		return 0
	}
	started := time.Now()
	const backlogFlushLimit = 2000

	var allReqs []telemetry.IngestRequest
	totalCollected := 0
	var warnings []string
	accounts, accountsErr := loadTelemetrySourceAccounts()
	if accountsErr != nil {
		warnings = append(warnings, fmt.Sprintf("collector account config: %v", accountsErr))
	}
	collectors, collectorWarnings := buildCollectorsFromSources(accounts, s.telemetrySources)
	warnings = append(warnings, collectorWarnings...)

	for _, collector := range collectors {
		reqs, err := collector.Collect(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", collector.Name(), err))
			continue
		}
		totalCollected += len(reqs)
		allReqs = append(allReqs, reqs...)
	}

	// No ingest-time age filter: local-file sources import their available
	// history on the first cycle and emit only changed-file events afterward.
	// Detail past the hot window is downsampled into usage_rollup_daily and then
	// pruned (see pruneOldData).
	direct, retries := s.ingestBatch(ctx, allReqs)
	if direct.ingested > 0 {
		s.dataIngested.Store(true)
	}
	flush, enqueued, flushWarnings := s.flushBacklog(ctx, retries, backlogFlushLimit)
	if flush.Ingested > 0 {
		s.dataIngested.Store(true)
	}
	warnings = append(warnings, flushWarnings...)

	durationMs := time.Since(started).Milliseconds()
	if totalCollected > 0 || direct.processed > 0 || enqueued > 0 || flush.Processed > 0 || len(warnings) > 0 {
		s.infof(
			"collect_cycle",
			"duration_ms=%d collected=%d direct_processed=%d direct_ingested=%d direct_deduped=%d direct_failed=%d enqueued=%d flush_processed=%d flush_ingested=%d flush_deduped=%d flush_failed=%d warnings=%d",
			durationMs, totalCollected,
			direct.processed, direct.ingested, direct.deduped, direct.failed,
			enqueued, flush.Processed, flush.Ingested, flush.Deduped, flush.Failed,
			len(warnings),
		)
		for _, warning := range warnings {
			s.warnf("collect_warning", "message=%q", warning)
		}
		return totalCollected
	}

	if durationMs >= 1500 && s.shouldLog("collect_slow", 30*time.Second) {
		s.infof("collect_idle_slow", "duration_ms=%d", durationMs)
	}

	return totalCollected
}

const telemetryPayloadMaintenanceBatchSize = 10000

func nextTelemetryPayloadMaintenanceDelay(backlog bool) time.Duration {
	if backlog {
		return 10 * time.Minute
	}
	return 6 * time.Hour
}

func (s *Service) runTelemetryPayloadMaintenanceLoop(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	// Keep large-table cleanup off the hot collection path and away from
	// startup. Orphan cleanup already runs after retention deletes canonical
	// events; this loop only strips old raw payload bodies in bounded batches.
	timer := time.NewTimer(10 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			maintenanceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			pruned, err := s.store.PruneRawEventPayloads(maintenanceCtx, 1, telemetryPayloadMaintenanceBatchSize)
			cancel()
			if err != nil {
				if s.shouldLog("prune_raw_payloads_error", 30*time.Minute) {
					s.warnf("prune_raw_payloads_error", "error=%v", err)
				}
				timer.Reset(10 * time.Minute)
				continue
			}
			if pruned > 0 {
				s.infof("prune_raw_payloads", "pruned=%d batch_size=%d", pruned, telemetryPayloadMaintenanceBatchSize)
			}
			timer.Reset(nextTelemetryPayloadMaintenanceDelay(pruned >= telemetryPayloadMaintenanceBatchSize))
		}
	}
}

func (s *Service) runRetentionLoop(ctx context.Context) {
	// Steady-state cadence once the backlog is drained; a tighter cadence while
	// catching up so a large one-time backlog (e.g. months accumulated while
	// retention was stalled) drains in minutes instead of over many 6h ticks.
	const idleInterval = 6 * time.Hour
	const catchUpInterval = 1 * time.Minute

	timer := time.NewTimer(0) // first run immediately
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			s.infof("retention_loop_stop", "reason=context_done")
			return
		case <-timer.C:
			complete := s.pruneOldData(ctx)
			if complete {
				timer.Reset(idleInterval)
			} else {
				timer.Reset(catchUpInterval)
			}
		}
	}
}

// pruneOldData rolls recent events into the daily downsample, then prunes raw
// events past the hot window that have already been rolled up. It reports
// whether the event backlog is fully drained; a false return means the prune
// stopped early (budget/context) and the caller should reschedule soon.
func (s *Service) pruneOldData(ctx context.Context) (complete bool) {
	if s == nil || s.store == nil {
		return true
	}
	cfg, err := config.Load()
	if err != nil {
		if s.shouldLog("retention_config_error", 30*time.Second) {
			s.warnf("retention_config_error", "error=%v", err)
		}
		return true
	}
	retentionDays := cfg.Data.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}

	pruneCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Thin and trim the balance observation series independently of usage
	// events — it grows on its own poll cadence and has its own retention floor.
	if thinned, berr := s.store.PruneBalanceObservations(pruneCtx, retentionDays); berr != nil {
		if s.shouldLog("balance_prune_error", 30*time.Second) {
			s.warnf("balance_prune_error", "error=%v", berr)
		}
	} else if thinned > 0 {
		s.infof("balance_prune", "thinned=%d retention_days=%d", thinned, retentionDays)
	}

	// Downsample first: roll recent (and, on first run, all) raw events into the
	// daily aggregate before any pruning, so detail is never deleted before its
	// aggregate exists. The watermark advances to the last fully-settled day.
	rollupCtx, rollupCancel := context.WithTimeout(ctx, 2*time.Minute)
	rolledDays, rollErr := s.store.RollupDaily(rollupCtx, s.now())
	rollupCancel()
	if rollErr != nil {
		if s.shouldLog("rollup_error", 30*time.Second) {
			s.warnf("rollup_error", "error=%v", rollErr)
		}
		// Without a fresh rollup we must not prune; try again next pass.
		return false
	}
	if rolledDays > 0 {
		s.infof("rollup_daily", "rows=%d", rolledDays)
	}
	watermark, _ := s.store.RollupWatermark(pruneCtx)

	deleted, drained, err := s.store.PruneOldEvents(pruneCtx, retentionDays, watermark)
	if err != nil {
		if s.shouldLog("retention_prune_error", 30*time.Second) {
			s.warnf("retention_prune_error", "error=%v", err)
		}
		return false
	}
	complete = drained
	if deleted > 0 {
		s.infof("retention_prune", "deleted=%d retention_days=%d", deleted, retentionDays)
		orphanCtx, orphanCancel := context.WithTimeout(ctx, 10*time.Second)
		defer orphanCancel()
		orphaned, orphanErr := s.store.PruneOrphanRawEvents(orphanCtx, 50000)
		if orphanErr != nil {
			s.warnf("retention_orphan_prune_error", "error=%v", orphanErr)
		} else if orphaned > 0 {
			s.infof("retention_orphan_prune", "removed=%d", orphaned)
		}

		// Reclaim disk space only after a large backlog cleanup. A full VACUUM
		// rewrites the whole file under an exclusive lock, blocking every reader
		// for its duration (a source of read-model timeouts), so it must not run
		// on routine daily deletions. Freed pages from small deletes are reused
		// by SQLite in place; the file only needs compacting after a big purge
		// (e.g. a first run that clears months of accumulated backlog).
		const vacuumThreshold = 20000
		if deleted > vacuumThreshold {
			vacCtx, vacCancel := context.WithTimeout(ctx, 5*time.Minute)
			defer vacCancel()
			if err := s.store.Vacuum(vacCtx); err != nil {
				s.warnf("retention_vacuum_error", "error=%v", err)
			} else {
				s.infof("retention_vacuum", "completed after deleting %d events", deleted)
			}
			if err := s.store.Analyze(vacCtx); err != nil {
				s.warnf("retention_analyze_error", "error=%v", err)
			}
		}
	}
	return complete
}
