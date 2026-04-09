package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func (s *Service) runCollectLoop(ctx context.Context) {
	interval := s.cfg.CollectInterval
	maxInterval := 5 * time.Minute
	consecutiveEmpty := 0

	s.infof("collect_loop_start", "interval=%s", interval)
	s.collectAndFlush(ctx)
	for {
		select {
		case <-ctx.Done():
			s.infof("collect_loop_stop", "reason=context_done")
			return
		case <-time.After(interval):
			collected := s.collectAndFlush(ctx)
			if collected == 0 {
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
	collectors, collectorWarnings := buildCollectors(accounts)
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
		s.pruneTelemetryOrphans(ctx)
		return totalCollected
	}

	if durationMs >= 1500 && s.shouldLog("collect_slow", 30*time.Second) {
		s.infof("collect_idle_slow", "duration_ms=%d", durationMs)
	}

	s.pruneTelemetryOrphans(ctx)
	return totalCollected
}

func (s *Service) pruneTelemetryOrphans(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	if !s.shouldLog("prune_orphan_raw_events_tick", 45*time.Second) {
		return
	}

	const pruneBatchSize = 10000
	pruneCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	removed, err := s.store.PruneOrphanRawEvents(pruneCtx, pruneBatchSize)
	if err != nil {
		if s.shouldLog("prune_orphan_raw_events_error", 20*time.Second) {
			s.warnf("prune_orphan_raw_events_error", "error=%v", err)
		}
		return
	}
	if removed > 0 {
		s.infof("prune_orphan_raw_events", "removed=%d batch_size=%d", removed, pruneBatchSize)
	}

	payloadCtx, payloadCancel := context.WithTimeout(ctx, 4*time.Second)
	defer payloadCancel()
	pruned, pruneErr := s.store.PruneRawEventPayloads(payloadCtx, 1, pruneBatchSize)
	if pruneErr == nil && pruned > 0 {
		s.infof("prune_raw_payloads", "pruned=%d", pruned)
	}
}

func (s *Service) runRetentionLoop(ctx context.Context) {
	s.pruneOldData(ctx)
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.infof("retention_loop_stop", "reason=context_done")
			return
		case <-ticker.C:
			s.pruneOldData(ctx)
		}
	}
}

func (s *Service) pruneOldData(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		if s.shouldLog("retention_config_error", 30*time.Second) {
			s.warnf("retention_config_error", "error=%v", err)
		}
		return
	}
	retentionDays := cfg.Data.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}

	pruneCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	deleted, err := s.store.PruneOldEvents(pruneCtx, retentionDays)
	if err != nil {
		if s.shouldLog("retention_prune_error", 30*time.Second) {
			s.warnf("retention_prune_error", "error=%v", err)
		}
		return
	}
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

		// Reclaim disk space after significant deletions.
		if deleted > 1000 {
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
}
