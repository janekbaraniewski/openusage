package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

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
		if processed >= hookSpoolBatchLimit || ctx.Err() != nil {
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

		parsed, parseErr := ParseHookRequests(raw.Source, strings.TrimSpace(raw.AccountID), raw.Payload)
		if parseErr != nil || len(parsed.Requests) == 0 {
			_ = os.Remove(path)
			processed++
			continue
		}

		tally, _ := s.ingestBatch(ctx, parsed.Requests)
		_ = os.Remove(path)
		processed++

		s.infof(
			"hook_spool_ingest",
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
