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
		ProviderLinks:   req.ProviderLinks,
		TimeWindowHours: tw.Hours(),
		TimeWindow:      tw,
	})
	core.Tracef("[read_model_perf] computeReadModel TOTAL: %dms (window=%s, accounts=%d, results=%d)",
		time.Since(start).Milliseconds(), tw, len(req.Accounts), len(result))
	return result, err
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
		s.rmCache.set(cacheKey, snapshots)
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
