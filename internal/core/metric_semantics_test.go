package core

import "testing"

func TestInferMetricGroup(t *testing.T) {
	tests := []struct {
		key  string
		want MetricGroup
	}{
		{key: "rpm", want: MetricGroupUsage},
		{key: "today_api_cost", want: MetricGroupSpending},
		{key: "model_openai_gpt4_input_tokens", want: MetricGroupTokens},
		{key: "messages_today", want: MetricGroupActivity},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := InferMetricGroup(tt.key, Metric{})
			if got != tt.want {
				t.Fatalf("InferMetricGroup(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

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
