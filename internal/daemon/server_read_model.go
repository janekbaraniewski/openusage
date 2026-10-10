package daemon

import (
	"context"
	"maps"
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
	return ageKimiQuotaSnapshots(result, s.now()), err
}

func shouldRefreshCachedReadModel(cachedAt time.Time, cachedVersion, currentVersion uint64, now time.Time) bool {
	age := now.Sub(cachedAt)
	return age > 2*time.Second && (currentVersion > cachedVersion || age >= time.Minute)
}

// Cached reads must not describe an old observation as live quota while an
// asynchronous refresh is pending. Clone only changed metadata; cache entries
// and other provider snapshots remain immutable.
func ageKimiQuotaSnapshots(snapshots map[string]core.UsageSnapshot, now time.Time) map[string]core.UsageSnapshot {
	out := snapshots
	cloned := false
	for id, snap := range snapshots {
		if snap.ProviderID != "kimi_cli" || snap.Attributes["quota_state"] != "fresh" {
			continue
		}
		observed, err := time.Parse(time.RFC3339Nano, snap.Attributes["quota_fetched_at"])
		if err != nil {
			observed = snap.Timestamp
		}
		if !observed.IsZero() && now.Sub(observed) < 2*time.Minute {
			continue
		}
		if !cloned {
			out = maps.Clone(snapshots)
			cloned = true
		}
		snap.Attributes = maps.Clone(snap.Attributes)
		snap.Diagnostics = maps.Clone(snap.Diagnostics)
		snap.SetAttribute("quota_state", "stale")
		if snap.Diagnostics["quota"] == "" && snap.Diagnostics["quota_error"] == "" {
			snap.SetDiagnostic("quota", "Waiting for updated Kimi quota")
		}
		out[id] = snap
	}
	return out
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
	s.markDataIngested() // ensure first boot always computes
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
