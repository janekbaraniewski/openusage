package tmux

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
)

func ptr(v float64) *float64 { return &v }

// When the live snapshot carries usage_five_hour, reconcile persists it so a
// later budget-limited render has a warm fallback.
func TestReconcileFiveHourWritesCacheWhenPresent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	c := &Context{
		Provider: "claude_code",
		Snapshot: core.UsageSnapshot{
			ProviderID: "claude_code",
			Metrics:    map[string]core.Metric{"usage_five_hour": {Used: ptr(7), Unit: "%", Window: "5h"}},
		},
	}
	reconcileFiveHourUsage(c)

	if pct, _, ok := claude_code.ReadFiveHourCache(); !ok || pct != 7 {
		t.Fatalf("cache not warmed: ok=%v pct=%v, want ok=true pct=7", ok, pct)
	}
}

// When the live snapshot is missing usage_five_hour (the slow usage API didn't
// resolve within budget), reconcile injects a recent cached value so block_pct
// still renders instead of the 5h segment silently dropping.
func TestReconcileFiveHourInjectsFromCacheWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	claude_code.WriteFiveHourCache(13)

	c := &Context{
		Provider: "claude_code",
		Snapshot: core.UsageSnapshot{
			ProviderID: "claude_code",
			Metrics:    map[string]core.Metric{"today_api_cost": {Used: ptr(98.7), Unit: "USD"}},
		},
	}
	reconcileFiveHourUsage(c)

	m, ok := c.Snapshot.Metrics["usage_five_hour"]
	if !ok || m.Used == nil {
		t.Fatalf("usage_five_hour not injected from cache")
	}
	if *m.Used != 13 {
		t.Errorf("injected pct = %v, want 13", *m.Used)
	}
	// block_pct must now resolve through the normal alias chain.
	r := &renderer{ctx: *c}
	if v, ok := r.resolve("block_pct"); !ok || v != "13" {
		t.Errorf("block_pct resolve = %q ok=%v, want \"13\" ok=true", v, ok)
	}
}

// A cold cache leaves the segment empty rather than fabricating a value.
func TestReconcileFiveHourNoCacheLeavesMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	c := &Context{
		Provider: "claude_code",
		Snapshot: core.UsageSnapshot{ProviderID: "claude_code", Metrics: map[string]core.Metric{}},
	}
	reconcileFiveHourUsage(c)

	if _, ok := c.Snapshot.Metrics["usage_five_hour"]; ok {
		t.Errorf("expected no injection when cache is cold")
	}
}
