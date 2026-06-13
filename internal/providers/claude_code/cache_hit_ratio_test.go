package claude_code

import (
	"math"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestApplyConversationUsageProjectionCacheHitRatio(t *testing.T) {
	t.Run("emitted from weekly token split", func(t *testing.T) {
		snap := core.UsageSnapshot{}
		snap.EnsureMaps()
		p := conversationUsageProjection{
			weeklyMessages:    10,
			weeklyInputTokens: 100,
			weeklyCacheRead:   700,
			weeklyCacheCreate: 200,
		}
		applyConversationUsageProjection(&snap, p)

		m, ok := snap.Metrics["cache_hit_ratio"]
		if !ok {
			t.Fatal("cache_hit_ratio missing")
		}
		// read=700, denom = 100+700+200 = 1000 -> 70%.
		if m.Used == nil || math.Abs(*m.Used-70) > 1e-9 {
			t.Fatalf("cache_hit_ratio = %v, want 70", m.Used)
		}
		if m.Unit != "%" {
			t.Fatalf("unit = %q, want %%", m.Unit)
		}
	})

	t.Run("absent without cache activity", func(t *testing.T) {
		snap := core.UsageSnapshot{}
		snap.EnsureMaps()
		applyConversationUsageProjection(&snap, conversationUsageProjection{})
		if _, ok := snap.Metrics["cache_hit_ratio"]; ok {
			t.Fatal("cache_hit_ratio should be absent without prompt tokens")
		}
	})
}
