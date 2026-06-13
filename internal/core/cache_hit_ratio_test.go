package core

import (
	"math"
	"testing"
)

func TestCacheHitRatio(t *testing.T) {
	tests := []struct {
		name       string
		input      float64
		cacheRead  float64
		cacheWrite float64
		wantPct    float64
		wantOK     bool
	}{
		{name: "zero denominator", input: 0, cacheRead: 0, cacheWrite: 0, wantOK: false},
		{name: "all cached read", input: 0, cacheRead: 100, cacheWrite: 0, wantPct: 100, wantOK: true},
		{name: "no cache at all", input: 100, cacheRead: 0, cacheWrite: 0, wantOK: false},
		{name: "write only is a miss", input: 0, cacheRead: 0, cacheWrite: 100, wantPct: 0, wantOK: true},
		{name: "half hit read plus input", input: 50, cacheRead: 50, cacheWrite: 0, wantPct: 50, wantOK: true},
		{name: "read plus write denominator", input: 0, cacheRead: 90, cacheWrite: 10, wantPct: 90, wantOK: true},
		{name: "typical mix", input: 200, cacheRead: 700, cacheWrite: 100, wantPct: 70, wantOK: true},
		{name: "negative input clamps low", input: -1000, cacheRead: 10, cacheWrite: 0, wantPct: 0, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pct, ok := CacheHitRatio(tt.input, tt.cacheRead, tt.cacheWrite)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if math.Abs(pct-tt.wantPct) > 1e-9 {
				t.Fatalf("pct = %v, want %v", pct, tt.wantPct)
			}
			if pct < 0 || pct > 100 {
				t.Fatalf("pct %v out of [0,100]", pct)
			}
		})
	}
}

func TestCacheHitRatioMetric(t *testing.T) {
	t.Run("undefined returns not ok", func(t *testing.T) {
		if _, ok := CacheHitRatioMetric(0, 0, 0, "7d"); ok {
			t.Fatal("expected ok=false for zero denominator")
		}
	})

	t.Run("builds percentage gauge", func(t *testing.T) {
		m, ok := CacheHitRatioMetric(200, 700, 100, "rolling 7 days")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if m.Unit != "%" {
			t.Fatalf("unit = %q, want %%", m.Unit)
		}
		if m.Window != "rolling 7 days" {
			t.Fatalf("window = %q", m.Window)
		}
		if m.Used == nil || math.Abs(*m.Used-70) > 1e-9 {
			t.Fatalf("used = %v, want 70", m.Used)
		}
		if m.Remaining == nil || math.Abs(*m.Remaining-30) > 1e-9 {
			t.Fatalf("remaining = %v, want 30", m.Remaining)
		}
		if m.Limit == nil || *m.Limit != 100 {
			t.Fatalf("limit = %v, want 100", m.Limit)
		}
		// Must agree with MetricUsedPercent so the gauge renders the hit ratio.
		if got := MetricUsedPercent("cache_hit_ratio", m); math.Abs(got-70) > 1e-9 {
			t.Fatalf("MetricUsedPercent = %v, want 70", got)
		}
	})
}
