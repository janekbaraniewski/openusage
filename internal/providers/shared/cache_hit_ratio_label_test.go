package shared

import "testing"

func TestCacheHitRatioLabelsPresent(t *testing.T) {
	if got := CodeStatsMetricLabels["cache_hit_ratio"]; got != "Cache Hit" {
		t.Errorf("CodeStatsMetricLabels[cache_hit_ratio] = %q, want %q", got, "Cache Hit")
	}
	if got := CodeStatsCompactLabels["cache_hit_ratio"]; got != "cache hit" {
		t.Errorf("CodeStatsCompactLabels[cache_hit_ratio] = %q, want %q", got, "cache hit")
	}
}
