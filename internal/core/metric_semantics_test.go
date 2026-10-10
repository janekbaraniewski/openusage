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

func TestIsRatioMetricKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"cache_hit_ratio", true},
		{"tool_success_rate", true},
		{"reasoning_share", true},
		{"tool_token_share", true},
		{"today_streamed_percent", true},
		{"rate_limit_requests", false},
		{"rate_limit_error_rate", false},
		{"plan_percent_used", false},
		{"monthly_usage_pct", false},
		{"rolling_usage", false},
		{"burn_rate", false},
		{"rpm", false},
	}
	for _, tt := range tests {
		if got := IsRatioMetricKey(tt.key); got != tt.want {
			t.Errorf("IsRatioMetricKey(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}
