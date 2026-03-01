package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/integrations"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
	"github.com/janekbaraniewski/openusage/internal/version"
)

const (
	defaultCodexSessionsDir     = "~/.codex/sessions"
	defaultClaudeProjectsDir    = "~/.claude/projects"
	defaultClaudeProjectsAltDir = "~/.config/claude/projects"
	defaultOpenCodeDBPath       = "~/.local/share/opencode/opencode.db"
)

type Service struct {
	cfg Config

	store        *telemetry.Store
	pipeline     *telemetry.Pipeline
	quotaIngest  *telemetry.QuotaSnapshotIngestor
	collectors   []telemetry.Collector
	providerByID map[string]core.UsageProvider

	pipelineMu sync.Mutex
	ingestMu   sync.Mutex
	logMu      sync.Mutex
	lastLogAt  map[string]time.Time

	readModelMu       sync.RWMutex
	readModelCache    map[string]cachedReadModelEntry
	readModelInFlight map[string]bool
}

func RunServer(cfg Config) error {
	if !cfg.Verbose {
		log.SetOutput(io.Discard)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	svc, err := startService(ctx, cfg)
	if err != nil {
		return err
	}
	defer svc.Close()

	<-ctx.Done()
	svc.infof("daemon_stop", "reason=signal")
	return nil
}

func startService(ctx context.Context, cfg Config) (*Service, error) {
	if strings.TrimSpace(cfg.DBPath) == "" {
		defaultDBPath, err := telemetry.DefaultDBPath()
		if err != nil {
			return nil, err
		}
		cfg.DBPath = defaultDBPath
	}
	if strings.TrimSpace(cfg.SpoolDir) == "" {
		defaultSpoolDir, err := telemetry.DefaultSpoolDir()
		if err != nil {
			return nil, err
		}
		cfg.SpoolDir = defaultSpoolDir
	}
	if strings.TrimSpace(cfg.SocketPath) == "" {
		defaultSocketPath, err := telemetry.DefaultSocketPath()
		if err != nil {
			return nil, err
		}
		cfg.SocketPath = defaultSocketPath
	}
	if cfg.CollectInterval <= 0 {
		cfg.CollectInterval = 20 * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}

	store, err := telemetry.OpenStore(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open daemon telemetry store: %w", err)
	}

	svc := &Service{
		cfg:               cfg,
		store:             store,
		pipeline:          telemetry.NewPipeline(store, telemetry.NewSpool(cfg.SpoolDir)),
		quotaIngest:       telemetry.NewQuotaSnapshotIngestor(store),
		collectors:        buildCollectors(),
		providerByID:      providersByID(),
		lastLogAt:         map[string]time.Time{},
		readModelCache:    map[string]cachedReadModelEntry{},
		readModelInFlight: map[string]bool{},
	}

	svc.infof(
		"daemon_start",
		"socket=%s db=%s spool=%s collect_interval=%s poll_interval=%s collectors=%d providers=%d",
		svc.cfg.SocketPath,
		svc.cfg.DBPath,
		svc.cfg.SpoolDir,
		svc.cfg.CollectInterval,
		svc.cfg.PollInterval,
		len(svc.collectors),
		len(svc.providerByID),
	)

	if err := svc.startSocketServer(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}

	go svc.runCollectLoop(ctx)
	go svc.runPollLoop(ctx)
	go svc.runReadModelCacheLoop(ctx)
	go svc.runSpoolMaintenanceLoop(ctx)
	go svc.runRetentionLoop(ctx)

	return svc, nil
}

func (s *Service) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

// --- Ingest helpers ---

func (s *Service) ingestRequest(ctx context.Context, req telemetry.IngestRequest) (telemetry.IngestResult, error) {
	if s == nil || s.store == nil {
		return telemetry.IngestResult{}, fmt.Errorf("telemetry store unavailable")
	}
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	return s.store.Ingest(ctx, req)
}

func (s *Service) ingestQuotaSnapshots(ctx context.Context, snapshots map[string]core.UsageSnapshot) error {
	if s == nil || s.quotaIngest == nil {
		return fmt.Errorf("quota ingestor unavailable")
	}
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	return s.quotaIngest.Ingest(ctx, snapshots)
}

func (s *Service) ingestBatch(ctx context.Context, reqs []telemetry.IngestRequest) (ingestTally, []telemetry.IngestRequest) {
	var tally ingestTally
	var retries []telemetry.IngestRequest
	for _, req := range reqs {
		tally.processed++
		result, err := s.ingestRequest(ctx, req)
		if err != nil {
			tally.failed++
			retries = append(retries, req)
			continue
		}
		if result.Deduped {
			tally.deduped++
		} else {
			tally.ingested++
		}
	}
	return tally, retries
}

func (s *Service) flushBacklog(ctx context.Context, retryReqs []telemetry.IngestRequest, limit int) (telemetry.FlushResult, int, []string) {
	var warnings []string
	enqueued := 0

	s.pipelineMu.Lock()
	if len(retryReqs) > 0 {
		n, err := s.pipeline.EnqueueRequests(retryReqs)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("retry enqueue: %v", err))
		} else {
			enqueued = n
		}
	}
	s.ingestMu.Lock()
	flush, flushWarnings := FlushInBatches(ctx, s.pipeline, limit)
	s.ingestMu.Unlock()
	s.pipelineMu.Unlock()

	return flush, enqueued, append(warnings, flushWarnings...)
}

// --- Logging ---

func (s *Service) infof(event, format string, args ...any) {
	if s == nil || !s.cfg.Verbose {
		return
	}
	if strings.TrimSpace(format) == "" {
		log.Printf("daemon level=info event=%s", event)
		return
	}
	log.Printf("daemon level=info event=%s "+format, append([]any{event}, args...)...)
}

func (s *Service) warnf(event, format string, args ...any) {
	if s == nil || !s.cfg.Verbose {
		return
	}
	if strings.TrimSpace(format) == "" {
		log.Printf("daemon level=warn event=%s", event)
		return
	}
	log.Printf("daemon level=warn event=%s "+format, append([]any{event}, args...)...)
}

func (s *Service) shouldLog(key string, interval time.Duration) bool {
	if s == nil {
		return false
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	now := time.Now()
	if interval > 0 {
		if last, ok := s.lastLogAt[key]; ok && now.Sub(last) < interval {
			return false
		}
	}
	s.lastLogAt[key] = now
	return true
}

// --- Read-model cache ---

func (s *Service) readModelCacheGet(cacheKey string, timeWindow string) (map[string]core.UsageSnapshot, time.Time, bool) {
	if s == nil || strings.TrimSpace(cacheKey) == "" {
		return nil, time.Time{}, false
	}
	s.readModelMu.RLock()
	entry, ok := s.readModelCache[cacheKey]
	s.readModelMu.RUnlock()
	if !ok || len(entry.snapshots) == 0 {
		return nil, time.Time{}, false
	}
	// Time window mismatch → treat as cache miss so gauge data is always fresh.
	if entry.timeWindow != timeWindow {
		return nil, time.Time{}, false
	}
	return maps.Clone(entry.snapshots), entry.updatedAt, true
}

func (s *Service) readModelCacheSet(cacheKey string, snapshots map[string]core.UsageSnapshot, timeWindow string) {
	if s == nil || strings.TrimSpace(cacheKey) == "" || len(snapshots) == 0 {
		return
	}
	s.readModelMu.Lock()
	s.readModelCache[cacheKey] = cachedReadModelEntry{
		snapshots:  maps.Clone(snapshots),
		updatedAt:  time.Now().UTC(),
		timeWindow: timeWindow,
	}
	s.readModelMu.Unlock()
}

func (s *Service) beginReadModelRefresh(cacheKey string) bool {
	if s == nil || strings.TrimSpace(cacheKey) == "" {
		return false
	}
	s.readModelMu.Lock()
	defer s.readModelMu.Unlock()
	if s.readModelInFlight[cacheKey] {
		return false
	}
	s.readModelInFlight[cacheKey] = true
	return true
}

func (s *Service) endReadModelRefresh(cacheKey string) {
	if s == nil || strings.TrimSpace(cacheKey) == "" {
		return
	}
	s.readModelMu.Lock()
	delete(s.readModelInFlight, cacheKey)
	s.readModelMu.Unlock()
}

func (s *Service) computeReadModel(
	ctx context.Context,
	req ReadModelRequest,
) (map[string]core.UsageSnapshot, error) {
	templates := ReadModelTemplatesFromRequest(req, DisabledAccountsFromConfig())
	if len(templates) == 0 {
		return map[string]core.UsageSnapshot{}, nil
	}
	tw := core.ParseTimeWindow(req.TimeWindow)
	return telemetry.ApplyCanonicalTelemetryViewWithOptions(ctx, s.cfg.DBPath, templates, telemetry.ReadModelOptions{
		ProviderLinks:   req.ProviderLinks,
		TimeWindowHours: tw.Hours(),
		TimeWindow:      req.TimeWindow,
	})
}

func (s *Service) refreshReadModelCacheAsync(
	parent context.Context,
	cacheKey string,
	req ReadModelRequest,
	timeout time.Duration,
) {
	if !s.beginReadModelRefresh(cacheKey) {
		return
	}
	go func() {
		defer s.endReadModelRefresh(cacheKey)
		refreshCtx, cancel := context.WithTimeout(parent, timeout)
		defer cancel()
		snapshots, err := s.computeReadModel(refreshCtx, req)
		if err != nil {
			if s.shouldLog("read_model_cache_refresh_error", 8*time.Second) {
				s.warnf("read_model_cache_refresh_error", "error=%v", err)
			}
			return
		}
		s.readModelCacheSet(cacheKey, snapshots, req.TimeWindow)
	}()
}

func (s *Service) runReadModelCacheLoop(ctx context.Context) {
	if s == nil {
		return
	}

	interval := s.cfg.PollInterval / 2
	interval = max(5*time.Second, min(30*time.Second, interval))

	s.infof("read_model_cache_loop_start", "interval=%s", interval)
	s.refreshReadModelCacheFromConfig(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.infof("read_model_cache_loop_stop", "reason=context_done")
			return
		case <-ticker.C:
			s.refreshReadModelCacheFromConfig(ctx)
		}
	}
}

func (s *Service) refreshReadModelCacheFromConfig(ctx context.Context) {
	req, err := BuildReadModelRequestFromConfig()
	if err != nil {
		if s.shouldLog("read_model_cache_config_error", 15*time.Second) {
			s.warnf("read_model_cache_config_error", "error=%v", err)
		}
		return
	}
	if len(req.Accounts) == 0 {
		return
	}
	cacheKey := ReadModelRequestKey(req)
	s.refreshReadModelCacheAsync(ctx, cacheKey, req, 60*time.Second)
}

// --- Collection loop ---

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
	s.cleanupSpool(ctx)

	for {
		select {
		case <-ctx.Done():
			s.infof("spool_loop_stop", "reason=context_done")
			return
		case <-flushTicker.C:
			s.flushSpoolBacklog(ctx, 10000)
		case <-cleanupTicker.C:
			s.cleanupSpool(ctx)
		}
	}
}

func (s *Service) flushSpoolBacklog(ctx context.Context, maxTotal int) {
	if s == nil || s.pipeline == nil {
		return
	}

	s.pipelineMu.Lock()
	s.ingestMu.Lock()
	flush, warnings := FlushInBatches(ctx, s.pipeline, maxTotal)
	s.ingestMu.Unlock()
	s.pipelineMu.Unlock()

	if flush.Processed > 0 || flush.Failed > 0 || len(warnings) > 0 {
		s.infof(
			"spool_flush",
			"processed=%d ingested=%d deduped=%d failed=%d warnings=%d",
			flush.Processed, flush.Ingested, flush.Deduped, flush.Failed, len(warnings),
		)
		for _, w := range warnings {
			s.warnf("spool_flush_warning", "message=%q", w)
		}
	}
}

func (s *Service) cleanupSpool(ctx context.Context) {
	if s == nil || strings.TrimSpace(s.cfg.SpoolDir) == "" {
		return
	}

	policy := telemetry.SpoolCleanupPolicy{
		MaxAge:   96 * time.Hour,
		MaxFiles: 25000,
		MaxBytes: 768 << 20, // 768 MB
	}

	s.pipelineMu.Lock()
	result, err := telemetry.NewSpool(s.cfg.SpoolDir).Cleanup(policy)
	s.pipelineMu.Unlock()
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
		for _, w := range warnings {
			s.warnf("collect_warning", "message=%q", w)
		}
		s.pruneTelemetryOrphans(ctx)
		return
	}

	if durationMs >= 1500 && s.shouldLog("collect_slow", 30*time.Second) {
		s.infof("collect_idle_slow", "duration_ms=%d", durationMs)
	}

	// Keep raw telemetry storage bounded by pruning orphan raw rows created by
	// historical dedup paths and intermittent duplicate ingestion races.
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
}

// --- Retention loop ---

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
		// Clean up orphaned raw events after pruning
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

// --- Poll loop ---

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
		go func(a core.AccountConfig) {
			defer wg.Done()

			provider, ok := s.providerByID[a.Provider]
			if !ok {
				results <- providerResult{
					accountID: a.ID,
					snapshot: core.UsageSnapshot{
						ProviderID: a.Provider,
						AccountID:  a.ID,
						Timestamp:  time.Now().UTC(),
						Status:     core.StatusError,
						Message:    fmt.Sprintf("no provider adapter registered for %q (restart/reinstall telemetry daemon if recently added)", a.Provider),
					},
				}
				return
			}

			fetchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()

			snap, fetchErr := provider.Fetch(fetchCtx, a)
			if fetchErr != nil {
				snap = core.UsageSnapshot{
					ProviderID: a.Provider,
					AccountID:  a.ID,
					Timestamp:  time.Now().UTC(),
					Status:     core.StatusError,
					Message:    fetchErr.Error(),
				}
			}
			snap = core.NormalizeUsageSnapshotWithConfig(snap, modelNorm)
			results <- providerResult{accountID: a.ID, snapshot: snap}
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

// --- HTTP server ---

func (s *Service) startSocketServer(ctx context.Context) error {
	if strings.TrimSpace(s.cfg.SocketPath) == "" {
		return fmt.Errorf("telemetry daemon socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0o755); err != nil {
		return fmt.Errorf("create telemetry daemon socket dir: %w", err)
	}
	if err := EnsureSocketPathAvailable(s.cfg.SocketPath); err != nil {
		return err
	}

	listener, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen telemetry daemon socket: %w", err)
	}
	_ = os.Chmod(s.cfg.SocketPath, 0o660)
	s.infof("socket_listening", "path=%s", s.cfg.SocketPath)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/hook/", s.handleHook)
	mux.HandleFunc("/v1/read-model", s.handleReadModel)

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       20 * time.Second,
	}

	go func() {
		<-ctx.Done()
		s.infof("socket_shutdown", "reason=context_done")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = listener.Close()
		_ = os.Remove(s.cfg.SocketPath)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.warnf("socket_server_error", "error=%v", err)
		}
	}()

	return nil
}

func EnsureSocketPathAvailable(socketPath string) error {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return fmt.Errorf("socket path is empty")
	}

	info, err := os.Stat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat socket path %s: %w", socketPath, err)
	}

	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("socket path %s already exists and is not a socket", socketPath)
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	defer cancel()
	dialer := net.Dialer{Timeout: 450 * time.Millisecond}
	conn, dialErr := dialer.DialContext(dialCtx, "unix", socketPath)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("telemetry daemon already running on socket %s", socketPath)
	}

	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("remove stale daemon socket %s: %w", socketPath, err)
	}
	return nil
}

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                 "ok",
		"daemon_version":         strings.TrimSpace(version.Version),
		"api_version":            APIVersion,
		"integration_version":    integrations.IntegrationVersion,
		"provider_registry_hash": ProviderRegistryHash(),
	})
}

func (s *Service) handleHook(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sourceName := strings.TrimPrefix(strings.TrimSpace(r.URL.Path), "/v1/hook/")
	sourceName = strings.TrimSpace(strings.Trim(sourceName, "/"))
	if sourceName == "" {
		writeJSONError(w, http.StatusBadRequest, "missing hook source")
		return
	}
	source, ok := providers.TelemetrySourceBySystem(sourceName)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("unknown hook source %q", sourceName))
		return
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read payload failed")
		return
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		writeJSONError(w, http.StatusBadRequest, "empty payload")
		return
	}

	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	reqs, err := telemetry.ParseSourceHookPayload(source, payload, defaultTelemetryOptionsForSource(sourceName), accountID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("parse hook payload: %v", err))
		return
	}
	if len(reqs) == 0 {
		writeJSON(w, http.StatusOK, HookResponse{Source: sourceName})
		return
	}

	tally, _ := s.ingestBatch(r.Context(), reqs)
	var warnings []string
	if tally.failed > 0 {
		warnings = append(warnings, fmt.Sprintf("%d ingest failures", tally.failed))
	}

	writeJSON(w, http.StatusOK, HookResponse{
		Source:    sourceName,
		Enqueued:  len(reqs),
		Processed: tally.processed,
		Ingested:  tally.ingested,
		Deduped:   tally.deduped,
		Failed:    tally.failed,
		Warnings:  warnings,
	})

	durationMs := time.Since(started).Milliseconds()
	logLevel := "hook_ingest"
	shouldLog := tally.failed > 0 || s.shouldLog("hook_ingest_"+sourceName, 3*time.Second)
	if !shouldLog {
		return
	}
	if tally.failed > 0 {
		s.warnf(logLevel,
			"source=%s account_id=%q duration_ms=%d enqueued=%d processed=%d ingested=%d deduped=%d failed=%d",
			sourceName, accountID, durationMs,
			len(reqs), tally.processed, tally.ingested, tally.deduped, tally.failed,
		)
	} else {
		s.infof(logLevel,
			"source=%s account_id=%q duration_ms=%d enqueued=%d processed=%d ingested=%d deduped=%d failed=%d",
			sourceName, accountID, durationMs,
			len(reqs), tally.processed, tally.ingested, tally.deduped, tally.failed,
		)
	}
}

func (s *Service) handleReadModel(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ReadModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("decode read-model request: %v", err))
		return
	}

	if len(req.Accounts) == 0 {
		configReq, configErr := BuildReadModelRequestFromConfig()
		if configErr != nil || len(configReq.Accounts) == 0 {
			writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: map[string]core.UsageSnapshot{}})
			return
		}
		req = configReq
	}

	cacheKey := ReadModelRequestKey(req)
	if cached, cachedAt, ok := s.readModelCacheGet(cacheKey, req.TimeWindow); ok {
		writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: cached})
		if time.Since(cachedAt) > 2*time.Second {
			s.refreshReadModelCacheAsync(context.Background(), cacheKey, req, 60*time.Second)
		}
		return
	}

	computeCtx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
	snapshots, err := s.computeReadModel(computeCtx, req)
	cancel()
	if err == nil && len(snapshots) > 0 {
		s.readModelCacheSet(cacheKey, snapshots, req.TimeWindow)
		writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: snapshots})
		return
	}

	if err != nil && s.shouldLog("read_model_cache_miss_compute_error", 8*time.Second) {
		s.warnf("read_model_cache_miss_compute_error", "error=%v", err)
	}

	s.refreshReadModelCacheAsync(context.Background(), cacheKey, req, 60*time.Second)
	snapshots = ReadModelTemplatesFromRequest(req, DisabledAccountsFromConfig())
	writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: snapshots})
	durationMs := time.Since(started).Milliseconds()
	if durationMs >= 1200 && s.shouldLog("read_model_slow", 30*time.Second) {
		s.infof(
			"read_model_slow",
			"duration_ms=%d requested_accounts=%d returned_snapshots=%d provider_links=%d",
			durationMs,
			len(req.Accounts),
			len(snapshots),
			len(req.ProviderLinks),
		)
	}
}

// --- Helpers ---

func buildCollectors() []telemetry.Collector {
	collectors := make([]telemetry.Collector, 0)
	for _, provider := range providers.AllProviders() {
		source, ok := provider.(shared.TelemetrySource)
		if !ok {
			continue
		}
		opts := defaultTelemetryOptionsForSource(source.System())
		collectors = append(collectors, telemetry.NewSourceCollector(source, opts, ""))
	}
	return collectors
}

func providersByID() map[string]core.UsageProvider {
	out := make(map[string]core.UsageProvider)
	for _, provider := range providers.AllProviders() {
		out[provider.ID()] = provider
	}
	return out
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

func telemetryOptionsForSource(
	sourceSystem string,
	codexSessions string,
	claudeProjects string,
	claudeProjectsAlt string,
	opencodeEventsDirs []string,
	opencodeEventsFile string,
	opencodeDB string,
) shared.TelemetryCollectOptions {
	opts := shared.TelemetryCollectOptions{
		Paths:     map[string]string{},
		PathLists: map[string][]string{},
	}

	switch sourceSystem {
	case "codex":
		opts.Paths["sessions_dir"] = codexSessions
	case "claude_code":
		opts.Paths["projects_dir"] = claudeProjects
		opts.Paths["alt_projects_dir"] = claudeProjectsAlt
	case "opencode":
		opts.Paths["events_file"] = opencodeEventsFile
		opts.Paths["db_path"] = opencodeDB
		opts.PathLists["events_dirs"] = opencodeEventsDirs
	}
	return opts
}

func FlushInBatches(ctx context.Context, pipeline *telemetry.Pipeline, maxTotal int) (telemetry.FlushResult, []string) {
	var (
		accum    telemetry.FlushResult
		warnings []string
	)

	remaining := maxTotal
	for {
		batchLimit := 10000
		if maxTotal > 0 {
			if remaining <= 0 {
				break
			}
			if remaining < batchLimit {
				batchLimit = remaining
			}
		}

		batch, err := pipeline.Flush(ctx, batchLimit)
		accum.Processed += batch.Processed
		accum.Ingested += batch.Ingested
		accum.Deduped += batch.Deduped
		accum.Failed += batch.Failed

		if err != nil {
			warnings = append(warnings, err.Error())
		}
		if maxTotal > 0 {
			remaining -= batch.Processed
		}

		if batch.Processed == 0 || (batch.Ingested == 0 && batch.Deduped == 0) {
			break
		}
	}

	return accum, warnings
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
