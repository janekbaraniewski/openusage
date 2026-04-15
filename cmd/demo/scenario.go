package main

import (
	"math"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const demoRefreshInterval = 5 * time.Second

var demoPhaseShares = []float64{0.24, 0.36, 0.49, 0.63, 0.76, 0.87, 0.95, 1.0}

type demoScenario struct {
	mu     sync.RWMutex
	phase  int
	frames []map[string]core.UsageSnapshot
}

func newDemoScenario(startedAt time.Time) *demoScenario {
	anchor := startedAt.UTC().Truncate(time.Second)
	if anchor.IsZero() {
		anchor = time.Now().UTC().Truncate(time.Second)
	}

	frames := make([]map[string]core.UsageSnapshot, len(demoPhaseShares))
	for phase := range demoPhaseShares {
		frames[phase] = buildDemoSnapshotsForPhase(anchor, phase)
	}

	return &demoScenario{frames: frames}
}

func (s *demoScenario) CurrentPhase() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.phase
}

func (s *demoScenario) Advance() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	last := len(s.frames) - 1
	if s.phase >= last {
		return false
	}
	s.phase++
	return true
}

func (s *demoScenario) Snapshot(accountID, providerID string) (core.UsageSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.frames) == 0 {
		return core.UsageSnapshot{}, false
	}

	frame := s.frames[s.phase]
	if snap, ok := frame[accountID]; ok && snap.ProviderID == providerID {
		return snap.DeepClone(), true
	}

	for _, snap := range frame {
		if snap.ProviderID == providerID {
			return snap.DeepClone(), true
		}
	}

	return core.UsageSnapshot{}, false
}

func buildDemoSnapshotsForPhase(anchor time.Time, phase int) map[string]core.UsageSnapshot {
	phase = clampDemoPhase(phase)
	share := demoPhaseShares[phase]
	phaseTime := anchor.Add(time.Duration(phase) * demoRefreshInterval)
	base := buildDemoSnapshotsAt(anchor)

	for accountID, snap := range base {
		snap.Timestamp = phaseTime

		for key, metric := range snap.Metrics {
			snap.Metrics[key] = scaleDemoMetric(key, metric, share)
		}
		snap.ModelUsage = scaleDemoModelUsage(snap.ModelUsage, share)
		for key, pts := range snap.DailySeries {
			snap.DailySeries[key] = scaleDemoSeries(pts, share)
		}

		snap.Status = demoStatusForSnapshot(snap)
		snap.Message = demoMessageForSnapshot(snap)
		base[accountID] = snap
	}

	return base
}

func clampDemoPhase(phase int) int {
	switch {
	case phase < 0:
		return 0
	case phase >= len(demoPhaseShares):
		return len(demoPhaseShares) - 1
	default:
		return phase
	}
}

func scaleDemoMetric(key string, metric core.Metric, share float64) core.Metric {
	if shouldKeepDemoMetricConstant(key, metric) {
		return metric
	}

	if metric.Limit != nil && *metric.Limit > 0 {
		used, hasUsed := demoMetricUsed(metric)
		if hasUsed {
			original := used
			if metric.Used != nil {
				original = *metric.Used
			}
			scaledUsed := scaleDemoValue(original, used, share)
			if metric.Used != nil {
				metric.Used = ptr(scaledUsed)
			}
			if metric.Remaining != nil {
				remaining := *metric.Limit - scaledUsed
				if remaining < 0 {
					remaining = 0
				}
				metric.Remaining = ptr(roundLike(*metric.Remaining, remaining))
			}
			return metric
		}
	}

	if metric.Used != nil {
		metric.Used = ptr(scaleDemoValue(*metric.Used, *metric.Used, share))
	}

	if metric.Remaining != nil {
		metric.Remaining = ptr(scaleDemoRemaining(metric, share))
	}

	return metric
}

func shouldKeepDemoMetricConstant(key string, metric core.Metric) bool {
	switch key {
	case "context_window", "composer_context_pct", "quota_models_tracked", "quota_models_low", "quota_models_exhausted", "mcp_servers_active":
		return true
	}

	if strings.HasSuffix(key, "_servers_active") {
		return true
	}

	if metric.Unit == "servers" && strings.Contains(key, "active") {
		return true
	}

	return false
}

func demoMetricUsed(metric core.Metric) (float64, bool) {
	if metric.Used != nil {
		return *metric.Used, true
	}
	if metric.Limit != nil && metric.Remaining != nil {
		return *metric.Limit - *metric.Remaining, true
	}
	return 0, false
}

func scaleDemoValue(original, final, share float64) float64 {
	if final == 0 {
		return 0
	}
	return roundLike(original, final*share)
}

func scaleDemoRemaining(metric core.Metric, share float64) float64 {
	if metric.Remaining == nil {
		return 0
	}
	if metric.Limit != nil && *metric.Limit > 0 {
		used := *metric.Limit - *metric.Remaining
		remaining := *metric.Limit - (used * share)
		if remaining < 0 {
			remaining = 0
		}
		return roundLike(*metric.Remaining, remaining)
	}
	return *metric.Remaining
}

func scaleDemoModelUsage(records []core.ModelUsageRecord, share float64) []core.ModelUsageRecord {
	for i := range records {
		records[i].InputTokens = scaleDemoFloatPtr(records[i].InputTokens, share)
		records[i].OutputTokens = scaleDemoFloatPtr(records[i].OutputTokens, share)
		records[i].CachedTokens = scaleDemoFloatPtr(records[i].CachedTokens, share)
		records[i].ReasoningTokens = scaleDemoFloatPtr(records[i].ReasoningTokens, share)
		records[i].TotalTokens = scaleDemoFloatPtr(records[i].TotalTokens, share)
		records[i].CostUSD = scaleDemoFloatPtr(records[i].CostUSD, share)
		records[i].Requests = scaleDemoFloatPtr(records[i].Requests, share)
	}
	return records
}

func scaleDemoFloatPtr(v *float64, share float64) *float64 {
	if v == nil {
		return nil
	}
	scaled := roundLike(*v, *v*share)
	return &scaled
}

func scaleDemoSeries(points []core.TimePoint, share float64) []core.TimePoint {
	if len(points) == 0 {
		return nil
	}

	scaled := make([]core.TimePoint, len(points))
	copy(scaled, points)
	last := len(scaled) - 1
	scaled[last].Value = roundDemoSeriesValue(scaled[last].Value * share)
	return scaled
}

func demoStatusForSnapshot(snap core.UsageSnapshot) core.Status {
	switch snap.ProviderID {
	case "codex":
		if used, ok := metricUsed(snap.Metrics, "plan_percent_used"); ok {
			switch {
			case used >= 100:
				return core.StatusLimited
			case used >= 85:
				return core.StatusNearLimit
			default:
				return core.StatusOK
			}
		}
	}
	return snap.Status
}

func roundLike(original, value float64) float64 {
	if math.Abs(original-math.Round(original)) < 1e-9 {
		return math.Round(value)
	}
	return math.Round(value*100) / 100
}
