package openrouter

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestSynthesizeLanguageMetricsFromModelRequestsMarksInferredSource(t *testing.T) {
	snap := core.NewUsageSnapshot("openrouter", "acct")
	reqs := 12.0
	snap.Metrics["model_gpt_4_1_requests"] = core.Metric{Used: &reqs, Unit: "requests", Window: "30d"}

	synthesizeLanguageMetricsFromModelRequests(&snap)

	metric, ok := snap.Metrics["lang_general"]
	if !ok {
		t.Fatal("expected lang_general metric")
	}
	if metric.Source != core.MetricSourceInferred {
		t.Fatalf("lang_general source = %q, want %q", metric.Source, core.MetricSourceInferred)
	}
}

func TestEmitModelDerivedToolUsageMetricsMarksInferredSource(t *testing.T) {
	snap := core.NewUsageSnapshot("openrouter", "acct")

	emitModelDerivedToolUsageMetrics(&snap, map[string]float64{
		"gpt-4.1": 4,
	}, "30d inferred", "inferred_from_model_requests")

	total, ok := snap.Metrics["tool_calls_total"]
	if !ok {
		t.Fatal("expected tool_calls_total metric")
	}
	if total.Source != core.MetricSourceInferred {
		t.Fatalf("tool_calls_total source = %q, want %q", total.Source, core.MetricSourceInferred)
	}

	foundToolMetric := false
	for key, metric := range snap.Metrics {
		if key == "tool_calls_total" || len(key) < len("tool_") || key[:5] != "tool_" {
			continue
		}
		foundToolMetric = true
		if metric.Source != core.MetricSourceInferred {
			t.Fatalf("%s source = %q, want %q", key, metric.Source, core.MetricSourceInferred)
		}
	}
	if !foundToolMetric {
		t.Fatal("expected at least one inferred tool_* metric")
	}
}
