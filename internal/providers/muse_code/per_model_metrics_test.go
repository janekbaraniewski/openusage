package muse_code

import (
	"context"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// The Model Burn section feeds off model_<slug>_<kind> metrics (the
// codex/cursor convention), not ModelUsage records. Without them the Muse
// detail view has no Models section and the dashboard can't show which
// model did the work.
func TestPopulateSnapshot_EmitsPerModelMetrics(t *testing.T) {
	entries := []museModelEntry{
		{Model: "muse-spark-1.3", Input: 1000, Output: 500, CacheRead: 200, Reasoning: 50, TotalTokens: 1750, SessionID: "s1", Timestamp: time.Now()},
		{Model: "muse-spark-1.3", Input: 1000, Output: 500, TotalTokens: 1500, SessionID: "s1", Timestamp: time.Now()},
	}
	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.DailySeries = make(map[string][]core.TimePoint)
	populateSnapshot(context.Background(), &snap, entries, time.Now())

	slug := "muse_spark_1_3"
	want := map[string]float64{
		"model_" + slug + "_input_tokens":  2000,
		"model_" + slug + "_output_tokens": 1000,
		"model_" + slug + "_cache_read_tokens": 200,
		"model_" + slug + "_reasoning_tokens":  50,
		"model_" + slug + "_requests":          2,
	}
	for key, wantUsed := range want {
		m, ok := snap.Metrics[key]
		if !ok || m.Used == nil || *m.Used != wantUsed {
			t.Errorf("metric %q = %+v, want used %v", key, m, wantUsed)
		}
	}
	// Zero buckets stay sparse: no cache-write writes means no key.
	if _, ok := snap.Metrics["model_"+slug+"_cache_write_tokens"]; ok {
		t.Errorf("unexpected zero-value cache_write metric")
	}
}
