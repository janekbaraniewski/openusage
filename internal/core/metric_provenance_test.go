package core

import "testing"

func TestMetricSourceLabel(t *testing.T) {
	tests := map[string]string{
		MetricSourceProviderNative: "native",
		MetricSourceLocalObserved:  "local",
		MetricSourceGitCommits:     "git",
		MetricSourceTrackedEdits:   "tracked",
		MetricSourceEstimated:      "estimated",
		MetricSourceInferred:       "inferred",
		MetricSourceSynthetic:      "synthetic",
	}

	for source, want := range tests {
		if got := MetricSourceLabel(source); got != want {
			t.Fatalf("MetricSourceLabel(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestUsageSnapshotSetMissingMetricSource(t *testing.T) {
	snap := UsageSnapshot{
		Metrics: map[string]Metric{
			"a": {Unit: "requests"},
			"b": {Unit: "tokens", Source: MetricSourceProviderNative},
		},
	}

	snap.SetMissingMetricSource(MetricSourceLocalObserved)

	if got := snap.Metrics["a"].Source; got != MetricSourceLocalObserved {
		t.Fatalf("metric a source = %q, want %q", got, MetricSourceLocalObserved)
	}
	if got := snap.Metrics["b"].Source; got != MetricSourceProviderNative {
		t.Fatalf("metric b source = %q, want %q", got, MetricSourceProviderNative)
	}
}
