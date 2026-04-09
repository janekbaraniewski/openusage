package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

type Service struct {
	cfg Config
	ctx context.Context

	store        *telemetry.Store
	pipeline     *telemetry.Pipeline
	quotaIngest  *telemetry.QuotaSnapshotIngestor
	providerByID map[string]core.UsageProvider

	spoolMu     sync.Mutex // guards spool filesystem operations (read/write/cleanup)
	logThrottle *core.LogThrottle

	rmCache       *readModelCache
	dataIngested  atomic.Bool // set when new data is ingested; read model loop skips refresh when clean
	pollScheduler *PollScheduler

	dirtyProvidersMu sync.Mutex
	dirtyProviders   map[string]bool // provider IDs that had new data since last read model refresh

	pollStateMu sync.Mutex
	pollState   map[string]*providerPollState // per-account change detection state
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
	if err := store.RunMigrations(ctx); err != nil {
		log.Printf("[daemon] warning: migrations failed: %v", err)
	}

	svc := &Service{
		cfg:           cfg,
		ctx:           ctx,
		store:         store,
		pipeline:      telemetry.NewPipeline(store, telemetry.NewSpool(cfg.SpoolDir)),
		quotaIngest:   telemetry.NewQuotaSnapshotIngestor(store),
		providerByID:  providersByID(),
		logThrottle:   core.NewLogThrottle(200, 10*time.Minute),
		rmCache:       newReadModelCache(),
		pollScheduler:  newPollScheduler(cfg.PollInterval),
		pollState:      make(map[string]*providerPollState),
		dirtyProviders: make(map[string]bool),
	}

	svc.infof(
		"daemon_start",
		"socket=%s db=%s spool=%s collect_interval=%s poll_interval=%s collectors=%d providers=%d",
		svc.cfg.SocketPath,
		svc.cfg.DBPath,
		svc.cfg.SpoolDir,
		svc.cfg.CollectInterval,
		svc.cfg.PollInterval,
		telemetrySourceCount(),
		len(svc.providerByID),
	)

	if err := svc.startSocketServer(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}

	go telemetry.RunWALCheckpointLoop(ctx, store.DB(), cfg.DBPath, func(key, level, msg string) {
		switch level {
		case "error", "warn":
			svc.warnf(key, "%s", msg)
		default:
			svc.infof(key, "%s", msg)
		}
	})
	go svc.runCollectLoop(ctx)
	go svc.runPollLoop(ctx)
	go svc.runReadModelCacheLoop(ctx)
	go svc.runWatchLoop(ctx)
	go svc.runSpoolMaintenanceLoop(ctx)
	go svc.runHookSpoolLoop(ctx)
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
	return s.store.Ingest(ctx, req)
}

func (s *Service) ingestQuotaSnapshots(ctx context.Context, snapshots map[string]core.UsageSnapshot) error {
	if s == nil || s.quotaIngest == nil {
		return fmt.Errorf("quota ingestor unavailable")
	}
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

	s.spoolMu.Lock()
	if len(retryReqs) > 0 {
		n, err := s.pipeline.EnqueueRequests(retryReqs)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("retry enqueue: %v", err))
		} else {
			enqueued = n
		}
	}
	flush, flushWarnings := FlushInBatches(ctx, s.pipeline, limit)
	s.spoolMu.Unlock()

	return flush, enqueued, append(warnings, flushWarnings...)
}

// markProviderDirty records that a provider had new data ingested.
func (s *Service) markProviderDirty(providerID string) {
	if providerID == "" {
		return
	}
	s.dirtyProvidersMu.Lock()
	s.dirtyProviders[providerID] = true
	s.dirtyProvidersMu.Unlock()
}

// drainDirtyProviders returns and clears the set of providers that had new data.
func (s *Service) drainDirtyProviders() map[string]bool {
	s.dirtyProvidersMu.Lock()
	dirty := s.dirtyProviders
	s.dirtyProviders = make(map[string]bool)
	s.dirtyProvidersMu.Unlock()
	return dirty
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
		if owner := SocketOwnerSummary(socketPath); strings.TrimSpace(owner) != "" {
			return fmt.Errorf("telemetry daemon already running on socket %s\nsocket_owner:\n%s", socketPath, owner)
		}
		return fmt.Errorf("telemetry daemon already running on socket %s", socketPath)
	}

	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("remove stale daemon socket %s: %w", socketPath, err)
	}
	return nil
}

// --- Helpers ---

func providersByID() map[string]core.UsageProvider {
	out := make(map[string]core.UsageProvider)
	for _, provider := range providers.AllProviders() {
		out[provider.ID()] = provider
	}
	return out
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
