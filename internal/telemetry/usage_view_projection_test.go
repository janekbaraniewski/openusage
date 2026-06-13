package telemetry

import (
	"math"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestApplyUsageViewCacheHitRatio(t *testing.T) {
	t.Run("computed from window token split", func(t *testing.T) {
		snap := core.UsageSnapshot{}
		agg := &telemetryUsageAgg{
			EventCount: 2,
			Models: []telemetryModelAgg{
				{Model: "a", InputTokens: 100, CacheReadTokens: 600, CacheWriteTokens: 100, Requests: 1},
				{Model: "b", InputTokens: 100, CacheReadTokens: 100, CacheWriteTokens: 0, Requests: 1},
			},
		}
		applyUsageViewToSnapshot(&snap, agg, core.TimeWindow("7d"))

		m, ok := snap.Metrics["cache_hit_ratio"]
		if !ok {
			t.Fatal("cache_hit_ratio metric missing")
		}
		// read=700, denom = input(200)+read(700)+write(100) = 1000 -> 70%.
		if m.Used == nil || math.Abs(*m.Used-70) > 1e-9 {
			t.Fatalf("cache_hit_ratio = %v, want 70", m.Used)
		}
		if m.Unit != "%" {
			t.Fatalf("unit = %q, want %%", m.Unit)
		}
	})

	t.Run("absent when no cache activity", func(t *testing.T) {
		snap := core.UsageSnapshot{}
		agg := &telemetryUsageAgg{
			EventCount: 1,
			Models: []telemetryModelAgg{
				{Model: "a", InputTokens: 0, CacheReadTokens: 0, CacheWriteTokens: 0, Requests: 1},
			},
		}
		applyUsageViewToSnapshot(&snap, agg, core.TimeWindow("7d"))
		if _, ok := snap.Metrics["cache_hit_ratio"]; ok {
			t.Fatal("cache_hit_ratio should be absent when prompt-token volume is zero")
		}
	})
}
