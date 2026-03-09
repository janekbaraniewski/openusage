package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func (s *Service) runCollectLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.CollectInterval)
	defer ticker.Stop()

	s.infof("collect_loop_start", "interval=%s", s.cfg.CollectInterval)
	s.collectAndFlush(ctx)
	for {
		select {
		case <-ctx.Done():
			s.infof("collect_loop_stop", "reason=context_done")
			return
		case <-ticker.C:
			s.collectAndFlush(ctx)
		}
	}
}

func (s *Service) runSpoolMaintenanceLoop(ctx context.Context) {
	if s == nil {
		return
	}
	flushTicker := time.NewTicker(5 * time.Second)
	cleanupTicker := time.NewTicker(60 * time.Second)
	defer flushTicker.Stop()
	defer cleanupTicker.Stop()

	s.infof("spool_loop_start", "flush_interval=%s cleanup_interval=%s", 5*time.Second, 60*time.Second)
	s.flushSpoolBacklog(ctx, 10000)
	s.cleanupSpool()

	for {
		select {
		case <-ctx.Done():
			s.infof("spool_loop_stop", "reason=context_done")
			return
		case <-flushTicker.C:
			s.flushSpoolBacklog(ctx, 10000)
		case <-cleanupTicker.C:
			s.cleanupSpool()
		}
	}
}

func (s *Service) flushSpoolBacklog(ctx context.Context, maxTotal int) {
	if s == nil || s.pipeline == nil {
		return
	}

	flush, warnings := FlushInBatches(ctx, s.pipeline, maxTotal)
	if flush.Processed > 0 || len(warnings) > 0 {
		s.infof(
			"spool_flush",
			"processed=%d ingested=%d deduped=%d failed=%d warnings=%d",
			flush.Processed, flush.Ingested, flush.Deduped, flush.Failed, len(warnings),
		)
		for _, warning := range warnings {
			s.warnf("spool_flush_warning", "message=%q", warning)
		}
	}
}

func (s *Service) cleanupSpool() {
	if s == nil || strings.TrimSpace(s.cfg.SpoolDir) == "" {
		return
	}

	policy := telemetry.SpoolCleanupPolicy{
		MaxAge:   96 * time.Hour,
		MaxFiles: 25000,
		MaxBytes: 768 << 20,
	}

	s.spoolMu.Lock()
	result, err := telemetry.NewSpool(s.cfg.SpoolDir).Cleanup(policy)
	s.spoolMu.Unlock()
	if err != nil {
		if s.shouldLog("spool_cleanup_error", 20*time.Second) {
			s.warnf("spool_cleanup_error", "error=%v", err)
		}
		return
	}
	if result.RemovedFiles > 0 {
		s.infof(
			"spool_cleanup",
			"removed_files=%d removed_bytes=%d remaining_files=%d remaining_bytes=%d",
			result.RemovedFiles,
			result.RemovedBytes,
			result.RemainingFiles,
			result.RemainingBytes,
		)
		return
	}
	if s.shouldLog("spool_cleanup_steady", 30*time.Minute) {
		s.infof(
			"spool_cleanup_steady",
			"remaining_files=%d remaining_bytes=%d",
			result.RemainingFiles,
			result.RemainingBytes,
		)
	}
}

func (s *Service) runHookSpoolLoop(ctx context.Context) {
	if s == nil {
		return
	}
	hookSpoolDir, err := telemetry.DefaultHookSpoolDir()
	if err != nil {
		s.warnf("hook_spool_loop", "resolve dir error=%v", err)
		return
	}

	processInterval := 5 * time.Second
	cleanupInterval := 5 * time.Minute
	processTicker := time.NewTicker(processInterval)
	cleanupTicker := time.NewTicker(cleanupInterval)
	defer processTicker.Stop()
	defer cleanupTicker.Stop()

	s.infof(
		"hook_spool_loop_start",
		"dir=%s process_interval=%s cleanup_interval=%s",
		hookSpoolDir,
		processInterval,
		cleanupInterval,
	)
	s.processHookSpool(ctx, hookSpoolDir)
	s.cleanupHookSpool(hookSpoolDir)

	for {
		select {
		case <-ctx.Done():
			s.infof("hook_spool_loop_stop", "reason=context_done")
			return
		case <-processTicker.C:
			s.processHookSpool(ctx, hookSpoolDir)
		case <-cleanupTicker.C:
			s.cleanupHookSpool(hookSpoolDir)
		}
	}
}

type rawHookFile struct {
	Source    string          `json:"source"`
	AccountID string          `json:"account_id"`
	Payload   json.RawMessage `json:"payload"`
}

const hookSpoolBatchLimit = 200

func (s *Service) processHookSpool(ctx context.Context, dir string) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		return
	}

	processed := 0
	for _, path := range files {
		if processed >= hookSpoolBatchLimit {
			break
		}
		if ctx.Err() != nil {
			return
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			_ = os.Remove(path)
			processed++
			continue
		}

		var raw rawHookFile
		if json.Unmarshal(data, &raw) != nil || len(raw.Payload) == 0 {
			_ = os.Remove(path)
			processed++
			continue
		}

		source, ok := providers.TelemetrySourceBySystem(raw.Source)
		if !ok {
			_ = os.Remove(path)
			processed++
			continue
		}

		reqs, parseErr := telemetry.ParseSourceHookPayload(
			source, raw.Payload,
			source.DefaultCollectOptions(),
			strings.TrimSpace(raw.AccountID),
		)
		if parseErr != nil || len(reqs) == 0 {
			_ = os.Remove(path)
			processed++
			continue
		}

		tally, _ := s.ingestBatch(ctx, reqs)
		_ = os.Remove(path)
		processed++

		s.infof("hook_spool_ingest",
			"file=%s source=%s processed=%d ingested=%d deduped=%d failed=%d",
			filepath.Base(path), raw.Source,
			tally.processed, tally.ingested, tally.deduped, tally.failed,
		)
	}
}

func (s *Service) cleanupHookSpool(dir string) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		tmps, _ := filepath.Glob(filepath.Join(dir, "*.json.tmp"))
		for _, tmp := range tmps {
			_ = os.Remove(tmp)
		}
		return
	}

	now := time.Now()
	removed := 0
	remaining := make([]string, 0, len(files))
	for _, path := range files {
		info, statErr := os.Stat(path)
		if statErr != nil {
			_ = os.Remove(path)
			removed++
			continue
		}
		if now.Sub(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(path)
			removed++
			continue
		}
		remaining = append(remaining, path)
	}

	if len(remaining) > 500 {
		for _, path := range remaining[:len(remaining)-500] {
			_ = os.Remove(path)
			removed++
		}
		remaining = remaining[len(remaining)-500:]
	}

	tmps, _ := filepath.Glob(filepath.Join(dir, "*.json.tmp"))
	for _, tmp := range tmps {
		_ = os.Remove(tmp)
		removed++
	}

	if removed > 0 {
		s.infof("hook_spool_cleanup", "removed=%d remaining=%d", removed, len(remaining))
	}
}

func (s *Service) collectAndFlush(ctx context.Context) {
	if s == nil {
		return
	}
	started := time.Now()
	const backlogFlushLimit = 2000

	var allReqs []telemetry.IngestRequest
	totalCollected := 0
	var warnings []string

	for _, collector := range s.collectors {
		reqs, err := collector.Collect(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", collector.Name(), err))
			continue
		}
		totalCollected += len(reqs)
		allReqs = append(allReqs, reqs...)
	}

	direct, retries := s.ingestBatch(ctx, allReqs)
	flush, enqueued, flushWarnings := s.flushBacklog(ctx, retries, backlogFlushLimit)
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
		return
	}

	if durationMs >= 1500 && s.shouldLog("collect_slow", 30*time.Second) {
		s.infof("collect_idle_slow", "duration_ms=%d", durationMs)
	}

	s.pruneTelemetryOrphans(ctx)
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
	}
}

func (s *Service) runPollLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	s.infof("poll_loop_start", "interval=%s", s.cfg.PollInterval)
	s.pollProviders(ctx)
	for {
		select {
		case <-ctx.Done():
			s.infof("poll_loop_stop", "reason=context_done")
			return
		case <-ticker.C:
			s.pollProviders(ctx)
		}
	}
}

func (s *Service) pollProviders(ctx context.Context) {
	if s == nil || s.quotaIngest == nil {
		return
	}
	started := time.Now()

	accounts, modelNorm, err := LoadAccountsAndNorm()
	if err != nil {
		if s.shouldLog("poll_config_warning", 20*time.Second) {
			s.warnf("poll_config_warning", "error=%v", err)
		}
		return
	}
	if len(accounts) == 0 {
		if s.shouldLog("poll_no_accounts", 30*time.Second) {
			s.infof("poll_skipped", "reason=no_enabled_accounts")
		}
		return
	}

	type providerResult struct {
		accountID string
		snapshot  core.UsageSnapshot
	}

	results := make(chan providerResult, len(accounts))
	var wg sync.WaitGroup

	for _, acct := range accounts {
		wg.Add(1)
		go func(account core.AccountConfig) {
			defer wg.Done()

			provider, ok := s.providerByID[account.Provider]
			if !ok {
				results <- providerResult{
					accountID: account.ID,
					snapshot: core.UsageSnapshot{
						ProviderID: account.Provider,
						AccountID:  account.ID,
						Timestamp:  time.Now().UTC(),
						Status:     core.StatusError,
						Message:    fmt.Sprintf("no provider adapter registered for %q (restart/reinstall telemetry daemon if recently added)", account.Provider),
					},
				}
				return
			}

			fetchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()

			snap, fetchErr := provider.Fetch(fetchCtx, account)
			if fetchErr != nil {
				snap = core.UsageSnapshot{
					ProviderID: account.Provider,
					AccountID:  account.ID,
					Timestamp:  time.Now().UTC(),
					Status:     core.StatusError,
					Message:    fetchErr.Error(),
				}
			}
			snap = core.NormalizeUsageSnapshotWithConfig(snap, modelNorm)
			results <- providerResult{accountID: account.ID, snapshot: snap}
		}(acct)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	snapshots := make(map[string]core.UsageSnapshot, len(accounts))
	statusCounts := map[core.Status]int{}
	errorCount := 0
	for result := range results {
		snapshots[result.accountID] = result.snapshot
		statusCounts[result.snapshot.Status]++
		if result.snapshot.Status == core.StatusError {
			errorCount++
		}
	}
	if len(snapshots) == 0 {
		return
	}

	ingestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	ingestErr := s.ingestQuotaSnapshots(ingestCtx, snapshots)
	if ingestErr != nil && s.shouldLog("poll_ingest_warning", 10*time.Second) {
		s.warnf("poll_ingest_warning", "error=%v", ingestErr)
	}

	durationMs := time.Since(started).Milliseconds()
	if ingestErr != nil || errorCount > 0 || s.shouldLog("poll_cycle_info", 45*time.Second) {
		s.infof(
			"poll_cycle",
			"duration_ms=%d accounts=%d snapshots=%d status_ok=%d status_auth=%d status_limited=%d status_error=%d status_unknown=%d ingest_error=%t",
			durationMs,
			len(accounts),
			len(snapshots),
			statusCounts[core.StatusOK],
			statusCounts[core.StatusAuth],
			statusCounts[core.StatusLimited],
			statusCounts[core.StatusError],
			statusCounts[core.StatusUnknown],
			ingestErr != nil,
		)
	}
}
