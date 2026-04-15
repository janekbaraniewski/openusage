package core

import "testing"

func TestMetricUsedPercent(t *testing.T) {
	limit := 100.0
	remaining := 60.0
	used := 40.0

	if got := MetricUsedPercent("rpm", Metric{Limit: &limit, Remaining: &remaining}); got != 40 {
		t.Fatalf("remaining form = %v, want 40", got)
	}
	if got := MetricUsedPercent("rpm", Metric{Limit: &limit, Used: &used}); got != 40 {
		t.Fatalf("used form = %v, want 40", got)
	}
}
