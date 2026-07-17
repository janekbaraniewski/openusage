package daemon

import (
	"context"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func (s *Service) computeReadModel(
	ctx context.Context,
	req ReadModelRequest,
) (map[string]core.UsageSnapshot, error) {
	start := time.Now()
	templates := ReadModelTemplatesFromRequest(req, DisabledAccountsFromConfig())
	if len(templates) == 0 {
		return map[string]core.UsageSnapshot{}, nil
	}
	tw := normalizeReadModelTimeWindow(req.TimeWindow)
	result, err := telemetry.ApplyCanonicalTelemetryViewWithOptions(ctx, s.cfg.DBPath, templates, telemetry.ReadModelOptions{
		ProviderLinks: req.ProviderLinks,
		Since:         tw.Since(),
		TodaySince:    core.LocalMidnight(),
		TimeWindow:    tw,
	})
	core.Tracef("[read_model_perf] computeReadModel TOTAL: %dms (window=%s, accounts=%d, results=%d)",
		time.Since(start).Milliseconds(), tw, len(req.Accounts), len(result))
	return result, err
}

func shouldRefreshCachedReadModel(cachedAt time.Time, cachedVersion, currentVersion uint64, now time.Time) bool {
	return currentVersion > cachedVersion && now.Sub(cachedAt) > 2*time.Second
}

func (s *Service) refreshReadModelCacheAsync(
	parent context.Context,
	cacheKey string,
	req ReadModelRequest,
	timeout time.Duration,
) {
	if !s.rmCache.beginRefresh(cacheKey) {
		return
	}
	refreshVersion := s.dataVersion.Load()
	go func() {
		defer s.rmCache.endRefresh(cacheKey)
		refreshCtx, cancel := context.WithTimeout(parent, timeout)
		defer cancel()
		snapshots, err := s.computeReadModel(refreshCtx, req)
		if err != nil {
			if s.shouldLog("read_model_cache_refresh_error", 8*time.Second) {
				s.warnf("read_model_cache_refresh_error", "error=%v", err)
			}
			return
		}
		s.rmCache.set(cacheKey, snapshots, refreshVersion)
		s.pushToExporter(refreshCtx, snapshots)
	}()
}

func (s *Service) serviceContext(fallback context.Context) context.Context {
	if s != nil && s.ctx != nil {
		return s.ctx
	}
	if fallback != nil {
		return fallback
	}
	return context.Background()
}

func (s *Service) runReadModelCacheLoop(ctx context.Context) {
	if s == nil {
		return
	}
	if !s.readModelCacheLoopEnabled() {
		s.infof("read_model_cache_loop_skip", "reason=no_exporter_on_demand_http_cache")
		return
	}

	interval := readModelCacheInterval(s.cfg.PollInterval)

	s.infof("read_model_cache_loop_start", "interval=%s", interval)
	s.dataIngested.Store(true) // ensure first boot always computes
	s.refreshReadModelCacheFromConfig(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.infof("read_model_cache_loop_stop", "reason=context_done")
			return
		case <-ticker.C:
			if !s.dataIngested.Swap(false) {
				continue // no new data ingested since last refresh
			}
			s.refreshReadModelCacheFromConfig(ctx)
		}
	}
}

// Local dashboard clients populate and refresh the cache through handleReadModel.
// Only a remote exporter needs proactive refreshes without an HTTP reader.
func (s *Service) readModelCacheLoopEnabled() bool {
	return s != nil && s.exp != nil
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

func readModelCacheInterval(pollInterval time.Duration) time.Duration {
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}
	if pollInterval < 5*time.Second {
		return 5 * time.Second
	}
	return pollInterval
}
