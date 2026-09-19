package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestCodexQuotaDisplay(t *testing.T) {
	now := time.Unix(1789805712, 0).Add(-2 * time.Hour)

	snap := core.NewUsageSnapshot("codex", "test")
	snap.Metrics["cache_hit_ratio"] = core.Metric{Used: core.Float64Ptr(12), Unit: "%"}
	if got := strings.Join(buildCodexQuotaLines(snap, 70, .2, .05, now), "\n"); !strings.Contains(got, "Quotas unavailable") || strings.Contains(got, "12") {
		t.Fatalf("cache must not replace quota: %s", got)
	}
	snap.Metrics["rate_limit_primary"] = core.Metric{Used: core.Float64Ptr(34), Remaining: core.Float64Ptr(66), Limit: core.Float64Ptr(100), Unit: "%", Window: "7d"}
	snap.Resets["rate_limit_primary"] = time.Unix(1789805712, 0)
	snap.Attributes["rate_limit_primary_bucket"] = "codex"
	snap.Metrics["rate_limit_codex_other_primary"] = core.Metric{Used: core.Float64Ptr(0), Remaining: core.Float64Ptr(100), Limit: core.Float64Ptr(100), Unit: "%", Window: "5h"}
	snap.Attributes["rate_limit_codex_other_primary_bucket"] = "Special pool"
	for _, width := range []int{32, 72, 120} {
		got := strings.Join(buildCodexQuotaLines(snap, width, .2, .05, now), "\n")
		for _, want := range []string{"Usage 7d", "34.0%", "5h: window unavailable", "Special pool", "Usage 5h", "0.0%", "resets 2h"} {
			if !strings.Contains(got, want) {
				t.Fatalf("width=%d missing %q in %s", width, want, got)
			}
		}
		if strings.Contains(got, "Cache") {
			t.Fatalf("unrelated gauge: %s", got)
		}
	}
}
