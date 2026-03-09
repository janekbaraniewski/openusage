package core

import "testing"

func TestExtractRateLimitDisplayMetrics(t *testing.T) {
	remaining := 60.0
	limit := 100.0
	used := 25.0
	metrics := map[string]Metric{
		"rate_limit_primary": {Remaining: &remaining, Unit: "%"},
		"rpm":                {Limit: &limit, Used: &used},
		"tokens_total":       {Used: Float64Ptr(12)},
	}

	got := ExtractRateLimitDisplayMetrics(metrics)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].LabelKey != "primary" || !got[0].UsesRemainingPercent {
		t.Fatalf("first = %+v, want primary remaining metric", got[0])
	}
	if got[1].LabelKey != "rpm" || got[1].UsedPercent != 25 {
		t.Fatalf("second = %+v, want rpm used=25", got[1])
	}
}

func TestFallbackDisplayMetricKeys(t *testing.T) {
	metrics := map[string]Metric{
		"usage_model_sonnet": {Used: Float64Ptr(1)},
		"messages_today":     {Used: Float64Ptr(2)},
		"analytics_score":    {Used: Float64Ptr(3)},
	}

	got := FallbackDisplayMetricKeys(metrics)
	if len(got) != 1 || got[0] != "messages_today" {
		t.Fatalf("got = %#v, want [messages_today]", got)
	}
}
