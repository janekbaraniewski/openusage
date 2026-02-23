package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

type telemetryDaemonConfig struct {
	DBPath          string
	SpoolDir        string
	SocketPath      string
	CollectInterval time.Duration
	PollInterval    time.Duration
	Verbose         bool
}

type telemetryDaemonService struct {
	cfg telemetryDaemonConfig

	store        *telemetry.Store
	pipeline     *telemetry.Pipeline
	quotaIngest  *telemetry.QuotaSnapshotIngestor
	collectors   []telemetry.Collector
	providerByID map[string]core.UsageProvider

	pipelineMu sync.Mutex
	logMu      sync.Mutex
	lastLogAt  map[string]time.Time

	readModelMu       sync.RWMutex
	readModelCache    map[string]cachedReadModelEntry
	readModelInFlight map[string]bool
}

type cachedReadModelEntry struct {
	snapshots map[string]core.UsageSnapshot
	updatedAt time.Time
}

func runTelemetryDaemon(args []string) error {
	if len(args) > 0 && !strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "install":
			return runTelemetryDaemonInstall(args[1:])
		case "uninstall":
			return runTelemetryDaemonUninstall(args[1:])
		case "status":
			return runTelemetryDaemonStatus(args[1:])
		default:
			return fmt.Errorf("unknown daemon subcommand %q", args[0])
		}
	}
	return runTelemetryDaemonServe(args)
}

func runTelemetryDaemonServe(args []string) error {
	cfgFile, loadErr := config.Load()
	if loadErr != nil {
		cfgFile = config.DefaultConfig()
	}

	defaultDBPath, err := telemetry.DefaultDBPath()
	if err != nil {
		return fmt.Errorf("resolve telemetry db path: %w", err)
	}
	defaultSpoolDir, err := telemetry.DefaultSpoolDir()
	if err != nil {
		return fmt.Errorf("resolve telemetry spool dir: %w", err)
	}
	defaultSocketPath, err := telemetry.DefaultSocketPath()
	if err != nil {
		return fmt.Errorf("resolve telemetry daemon socket path: %w", err)
	}

	defaultInterval := time.Duration(cfgFile.UI.RefreshIntervalSeconds) * time.Second
	if defaultInterval <= 0 {
		defaultInterval = 30 * time.Second
	}

	fs := flag.NewFlagSet("telemetry daemon", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	dbPath := fs.String("db-path", defaultDBPath, "path to telemetry sqlite database")
	spoolDir := fs.String("spool-dir", defaultSpoolDir, "path to telemetry spool directory")
	socketPath := fs.String("socket-path", defaultSocketPath, "path to telemetry unix socket")
	interval := fs.Duration("interval", defaultInterval, "default collector/poller interval")
	collectInterval := fs.Duration("collect-interval", 0, "collector interval override (0 uses --interval)")
	pollInterval := fs.Duration("poll-interval", 0, "provider poll interval override (0 uses --interval)")
	verbose := fs.Bool("verbose", false, "enable daemon logs")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if !*verbose {
		log.SetOutput(io.Discard)
	}

	resolvedCollectInterval := *collectInterval
	if resolvedCollectInterval <= 0 {
		resolvedCollectInterval = *interval
	}
	resolvedPollInterval := *pollInterval
	if resolvedPollInterval <= 0 {
		resolvedPollInterval = *interval
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	svc, err := startTelemetryDaemonService(ctx, telemetryDaemonConfig{
		DBPath:          strings.TrimSpace(*dbPath),
		SpoolDir:        strings.TrimSpace(*spoolDir),
		SocketPath:      strings.TrimSpace(*socketPath),
		CollectInterval: resolvedCollectInterval,
		PollInterval:    resolvedPollInterval,
		Verbose:         *verbose,
	})
	if err != nil {
		return err
	}
	defer svc.Close()

	<-ctx.Done()
	svc.infof("daemon_stop", "reason=signal")
	return nil
}

func startTelemetryDaemonService(ctx context.Context, cfg telemetryDaemonConfig) (*telemetryDaemonService, error) {
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

	svc := &telemetryDaemonService{
		cfg:          cfg,
		store:        store,
		pipeline:     telemetry.NewPipeline(store, telemetry.NewSpool(cfg.SpoolDir)),
		quotaIngest:  telemetry.NewQuotaSnapshotIngestor(store),
		collectors:   buildTelemetryCollectors(),
		providerByID: providersByID(),
		lastLogAt:    map[string]time.Time{},
		readModelCache: map[string]cachedReadModelEntry{},
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

	// Run compaction in background so daemon health is available immediately.
	go func() {
		compactCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		result, compactErr := svc.store.CompactUsage(compactCtx)
		if compactErr != nil {
			svc.warnf("compaction_failed", "error=%v", compactErr)
			return
		}
		svc.infof(
			"compaction_done",
			"duplicate_events_removed=%d orphan_raw_events_removed=%d",
			result.DuplicateEventsRemoved,
			result.OrphanRawEventsRemoved,
		)
	}()

	go svc.runCollectLoop(ctx)
	go svc.runPollLoop(ctx)
	go svc.runReadModelCacheLoop(ctx)

	return svc, nil
}

func (s *telemetryDaemonService) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func buildTelemetryCollectors() []telemetry.Collector {
	collectors := make([]telemetry.Collector, 0)
	for _, source := range providers.AllTelemetrySources() {
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

func (s *telemetryDaemonService) infof(event, format string, args ...any) {
	if s == nil || !s.cfg.Verbose {
		return
	}
	if strings.TrimSpace(format) == "" {
		log.Printf("daemon level=info event=%s", event)
		return
	}
	log.Printf("daemon level=info event=%s "+format, append([]any{event}, args...)...)
}

func (s *telemetryDaemonService) warnf(event, format string, args ...any) {
	if s == nil || !s.cfg.Verbose {
		return
	}
	if strings.TrimSpace(format) == "" {
		log.Printf("daemon level=warn event=%s", event)
		return
	}
	log.Printf("daemon level=warn event=%s "+format, append([]any{event}, args...)...)
}

func (s *telemetryDaemonService) shouldLog(key string, interval time.Duration) bool {
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

func daemonReadModelRequestKey(req daemonReadModelRequest) string {
	accounts := make([]daemonReadModelAccount, 0, len(req.Accounts))
	seenAccounts := make(map[string]bool, len(req.Accounts))
	for _, account := range req.Accounts {
		accountID := strings.TrimSpace(account.AccountID)
		providerID := strings.TrimSpace(account.ProviderID)
		if accountID == "" || providerID == "" {
			continue
		}
		key := accountID + "|" + providerID
		if seenAccounts[key] {
			continue
		}
		seenAccounts[key] = true
		accounts = append(accounts, daemonReadModelAccount{
			AccountID:  accountID,
			ProviderID: providerID,
		})
	}
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].AccountID != accounts[j].AccountID {
			return accounts[i].AccountID < accounts[j].AccountID
		}
		return accounts[i].ProviderID < accounts[j].ProviderID
	})

	linkKeys := make([]string, 0, len(req.ProviderLinks))
	for source, target := range req.ProviderLinks {
		source = strings.ToLower(strings.TrimSpace(source))
		target = strings.ToLower(strings.TrimSpace(target))
		if source == "" || target == "" {
			continue
		}
		linkKeys = append(linkKeys, source+"="+target)
	}
	sort.Strings(linkKeys)

	var b strings.Builder
	b.Grow(128 + len(accounts)*32 + len(linkKeys)*24)
	b.WriteString("accounts:")
	for _, account := range accounts {
		b.WriteString(account.AccountID)
		b.WriteByte(':')
		b.WriteString(account.ProviderID)
		b.WriteByte(';')
	}
	b.WriteString("|links:")
	for _, key := range linkKeys {
		b.WriteString(key)
		b.WriteByte(';')
	}
	return b.String()
}

func (s *telemetryDaemonService) readModelCacheGet(cacheKey string) (map[string]core.UsageSnapshot, time.Time, bool) {
	if s == nil || strings.TrimSpace(cacheKey) == "" {
		return nil, time.Time{}, false
	}
	s.readModelMu.RLock()
	entry, ok := s.readModelCache[cacheKey]
	s.readModelMu.RUnlock()
	if !ok || len(entry.snapshots) == 0 {
		return nil, time.Time{}, false
	}
	return cloneSnapshotsMap(entry.snapshots), entry.updatedAt, true
}

func (s *telemetryDaemonService) readModelCacheSet(cacheKey string, snapshots map[string]core.UsageSnapshot) {
	if s == nil || strings.TrimSpace(cacheKey) == "" || len(snapshots) == 0 {
		return
	}
	s.readModelMu.Lock()
	s.readModelCache[cacheKey] = cachedReadModelEntry{
		snapshots: cloneSnapshotsMap(snapshots),
		updatedAt: time.Now().UTC(),
	}
	s.readModelMu.Unlock()
}

func (s *telemetryDaemonService) beginReadModelRefresh(cacheKey string) bool {
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

func (s *telemetryDaemonService) endReadModelRefresh(cacheKey string) {
	if s == nil || strings.TrimSpace(cacheKey) == "" {
		return
	}
	s.readModelMu.Lock()
	delete(s.readModelInFlight, cacheKey)
	s.readModelMu.Unlock()
}

func (s *telemetryDaemonService) computeReadModel(
	ctx context.Context,
	req daemonReadModelRequest,
) (map[string]core.UsageSnapshot, error) {
	templates := readModelTemplatesFromRequest(req, disabledAccountsFromConfig())
	if len(templates) == 0 {
		return map[string]core.UsageSnapshot{}, nil
	}
	return telemetry.ApplyCanonicalTelemetryViewWithOptions(ctx, s.cfg.DBPath, templates, telemetry.ReadModelOptions{
		ProviderLinks: req.ProviderLinks,
	})
}

func (s *telemetryDaemonService) refreshReadModelCacheAsync(
	parent context.Context,
	cacheKey string,
	req daemonReadModelRequest,
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
		s.readModelCacheSet(cacheKey, snapshots)
	}()
}

func daemonReadModelRequestFromConfig() (daemonReadModelRequest, error) {
	cfg, err := config.Load()
	if err != nil {
		return daemonReadModelRequest{}, err
	}
	accounts := mergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)
	accounts = filterAccountsByDashboardConfig(accounts, cfg.Dashboard)
	credResult := detect.Result{Accounts: accounts}
	detect.ApplyCredentials(&credResult)
	return daemonReadModelRequestFromAccounts(credResult.Accounts, cfg.Telemetry.ProviderLinks), nil
}

func (s *telemetryDaemonService) runReadModelCacheLoop(ctx context.Context) {
	if s == nil {
		return
	}

	interval := s.cfg.PollInterval / 2
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}

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

func (s *telemetryDaemonService) refreshReadModelCacheFromConfig(ctx context.Context) {
	req, err := daemonReadModelRequestFromConfig()
	if err != nil {
		if s.shouldLog("read_model_cache_config_error", 15*time.Second) {
			s.warnf("read_model_cache_config_error", "error=%v", err)
		}
		return
	}
	if len(req.Accounts) == 0 {
		return
	}
	cacheKey := daemonReadModelRequestKey(req)
	s.refreshReadModelCacheAsync(ctx, cacheKey, req, 12*time.Second)
}

func (s *telemetryDaemonService) runCollectLoop(ctx context.Context) {
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

func (s *telemetryDaemonService) collectAndFlush(ctx context.Context) {
	if s == nil {
		return
	}
	started := time.Now()

	type collectorBatch struct {
		name string
		reqs []telemetry.IngestRequest
	}

	totalCollected := 0
	warnings := make([]string, 0)
	enqueued := 0
	batches := make([]collectorBatch, 0, len(s.collectors))

	// File/SQLite scans can be expensive; collect outside the pipeline lock so
	// hook ingestion and read-model requests don't block on collector I/O.
	for _, collector := range s.collectors {
		reqs, err := collector.Collect(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", collector.Name(), err))
			continue
		}
		totalCollected += len(reqs)
		if len(reqs) == 0 {
			continue
		}
		batches = append(batches, collectorBatch{name: collector.Name(), reqs: reqs})
	}

	s.pipelineMu.Lock()
	for _, batch := range batches {
		n, err := s.pipeline.EnqueueRequests(batch.reqs)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s enqueue: %v", batch.name, err))
			continue
		}
		enqueued += n
	}
	flush, flushWarnings := flushInBatches(ctx, s.pipeline, 0)
	s.pipelineMu.Unlock()

	warnings = append(warnings, flushWarnings...)

	durationMs := time.Since(started).Milliseconds()
	if totalCollected > 0 || enqueued > 0 || len(warnings) > 0 {
		s.infof(
			"collect_cycle",
			"duration_ms=%d collected=%d enqueued=%d processed=%d ingested=%d deduped=%d failed=%d warnings=%d",
			durationMs,
			totalCollected,
			enqueued,
			flush.Processed,
			flush.Ingested,
			flush.Deduped,
			flush.Failed,
			len(warnings),
		)
		for _, warning := range warnings {
			s.warnf("collect_warning", "message=%q", warning)
		}
		return
	}

	if durationMs >= 1500 && s.shouldLog("collect_slow", 30*time.Second) {
		s.infof("collect_idle_slow", "duration_ms=%d", durationMs)
	}
}

func (s *telemetryDaemonService) runPollLoop(ctx context.Context) {
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

func (s *telemetryDaemonService) pollProviders(ctx context.Context) {
	if s == nil || s.quotaIngest == nil {
		return
	}
	started := time.Now()

	accounts, modelNorm, err := daemonAccountsAndNorm()
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
						Message:    fmt.Sprintf("no provider adapter registered for %q", a.Provider),
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
	ingestErr := s.quotaIngest.Ingest(ingestCtx, snapshots)
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

func daemonAccountsAndNorm() ([]core.AccountConfig, core.ModelNormalizationConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, core.DefaultModelNormalizationConfig(), err
	}
	accounts := mergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)
	accounts = filterAccountsByDashboardConfig(accounts, cfg.Dashboard)
	credResult := detect.Result{Accounts: accounts}
	detect.ApplyCredentials(&credResult)
	accounts = credResult.Accounts
	return accounts, core.NormalizeModelNormalizationConfig(cfg.ModelNormalization), nil
}

func filterAccountsByDashboardConfig(
	accounts []core.AccountConfig,
	dashboardCfg config.DashboardConfig,
) []core.AccountConfig {
	if len(accounts) == 0 {
		return nil
	}

	enabledByAccountID := make(map[string]bool, len(dashboardCfg.Providers))
	for _, pref := range dashboardCfg.Providers {
		accountID := strings.TrimSpace(pref.AccountID)
		if accountID == "" {
			continue
		}
		enabledByAccountID[accountID] = pref.Enabled
	}

	filtered := make([]core.AccountConfig, 0, len(accounts))
	for _, acct := range accounts {
		accountID := strings.TrimSpace(acct.ID)
		if accountID == "" {
			continue
		}
		enabled, ok := enabledByAccountID[accountID]
		if ok && !enabled {
			continue
		}
		filtered = append(filtered, acct)
	}
	return filtered
}

func disabledAccountsFromDashboard(dashboardCfg config.DashboardConfig) map[string]bool {
	disabled := make(map[string]bool, len(dashboardCfg.Providers))
	for _, pref := range dashboardCfg.Providers {
		accountID := strings.TrimSpace(pref.AccountID)
		if accountID == "" || pref.Enabled {
			continue
		}
		disabled[accountID] = true
	}
	return disabled
}

func disabledAccountsFromConfig() map[string]bool {
	cfg, err := config.Load()
	if err != nil {
		return map[string]bool{}
	}
	return disabledAccountsFromDashboard(cfg.Dashboard)
}

func (s *telemetryDaemonService) startSocketServer(ctx context.Context) error {
	if strings.TrimSpace(s.cfg.SocketPath) == "" {
		return fmt.Errorf("telemetry daemon socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0o755); err != nil {
		return fmt.Errorf("create telemetry daemon socket dir: %w", err)
	}
	if err := ensureSocketPathAvailable(s.cfg.SocketPath); err != nil {
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

func ensureSocketPathAvailable(socketPath string) error {
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

	// Existing non-socket files should never be deleted automatically.
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

	// Socket file exists but no server is accepting connections.
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("remove stale daemon socket %s: %w", socketPath, err)
	}
	return nil
}

func (s *telemetryDaemonService) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *telemetryDaemonService) handleHook(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusOK, daemonHookResponse{Source: sourceName})
		return
	}

	processed := 0
	ingested := 0
	deduped := 0
	failed := 0
	warnings := make([]string, 0)
	for _, req := range reqs {
		processed++
		result, err := s.store.Ingest(r.Context(), req)
		if err != nil {
			failed++
			warnings = append(warnings, err.Error())
			continue
		}
		if result.Deduped {
			deduped++
		} else {
			ingested++
		}
	}

	writeJSON(w, http.StatusOK, daemonHookResponse{
		Source:    sourceName,
		Enqueued:  len(reqs),
		Processed: processed,
		Ingested:  ingested,
		Deduped:   deduped,
		Failed:    failed,
		Warnings:  warnings,
	})

	durationMs := time.Since(started).Milliseconds()
	if failed > 0 || len(warnings) > 0 {
		s.warnf(
			"hook_ingest",
			"source=%s account_id=%q duration_ms=%d enqueued=%d processed=%d ingested=%d deduped=%d failed=%d warnings=%d",
			sourceName,
			accountID,
			durationMs,
			len(reqs),
			processed,
			ingested,
			deduped,
			failed,
			len(warnings),
		)
		return
	}
	if s.shouldLog("hook_ingest_"+sourceName, 3*time.Second) {
		s.infof(
			"hook_ingest",
			"source=%s account_id=%q duration_ms=%d enqueued=%d processed=%d ingested=%d deduped=%d failed=%d",
			sourceName,
			accountID,
			durationMs,
			len(reqs),
			processed,
			ingested,
			deduped,
			failed,
		)
	}
}

func (s *telemetryDaemonService) handleReadModel(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req daemonReadModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("decode read-model request: %v", err))
		return
	}

	if len(req.Accounts) == 0 {
		writeJSON(w, http.StatusOK, daemonReadModelResponse{Snapshots: map[string]core.UsageSnapshot{}})
		return
	}

	cacheKey := daemonReadModelRequestKey(req)
	if cached, cachedAt, ok := s.readModelCacheGet(cacheKey); ok {
		writeJSON(w, http.StatusOK, daemonReadModelResponse{Snapshots: cached})
		if time.Since(cachedAt) > 2*time.Second {
			s.refreshReadModelCacheAsync(context.Background(), cacheKey, req, 8*time.Second)
		}
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 6500*time.Millisecond)
	defer cancel()
	snapshots, err := s.computeReadModel(ctx, req)
	if err != nil {
		// Try one more time from cache if a concurrent refresh finished meanwhile.
		if cached, _, ok := s.readModelCacheGet(cacheKey); ok {
			writeJSON(w, http.StatusOK, daemonReadModelResponse{Snapshots: cached})
			return
		}
		if s.shouldLog("read_model_error", 5*time.Second) {
			s.warnf(
				"read_model_failed",
				"duration_ms=%d accounts=%d error=%v",
				time.Since(started).Milliseconds(),
				len(req.Accounts),
				err,
			)
		}
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("read-model apply failed: %v", err))
		return
	}

	s.readModelCacheSet(cacheKey, snapshots)
	writeJSON(w, http.StatusOK, daemonReadModelResponse{Snapshots: snapshots})
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

func readModelTemplatesFromRequest(
	req daemonReadModelRequest,
	disabledAccounts map[string]bool,
) map[string]core.UsageSnapshot {
	if disabledAccounts == nil {
		disabledAccounts = map[string]bool{}
	}
	accounts := make([]daemonReadModelAccount, 0, len(req.Accounts))
	seen := make(map[string]bool, len(req.Accounts))
	for _, account := range req.Accounts {
		accountID := strings.TrimSpace(account.AccountID)
		providerID := strings.TrimSpace(account.ProviderID)
		if disabledAccounts[accountID] {
			continue
		}
		if accountID == "" || providerID == "" || seen[accountID] {
			continue
		}
		seen[accountID] = true
		accounts = append(accounts, daemonReadModelAccount{AccountID: accountID, ProviderID: providerID})
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].AccountID < accounts[j].AccountID })

	out := make(map[string]core.UsageSnapshot, len(accounts))
	now := time.Now().UTC()
	for _, account := range accounts {
		out[account.AccountID] = core.UsageSnapshot{
			ProviderID:  account.ProviderID,
			AccountID:   account.AccountID,
			Timestamp:   now,
			Status:      core.StatusUnknown,
			Message:     "",
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
