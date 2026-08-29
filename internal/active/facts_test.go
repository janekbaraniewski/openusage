package active

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestBuildFactsUsesExplicitResetKey(t *testing.T) {
	remaining := 37.0
	reset := at("2026-08-15T14:00:00Z")
	snap := core.NewUsageSnapshot("claude_code", "default")
	snap.Metrics["usage_five_hour"] = core.Metric{
		Remaining: &remaining,
		Unit:      "%",
		ResetKey:  "billing_block",
	}
	snap.Resets["billing_block"] = reset

	facts := BuildFacts(snap, time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC))
	if facts.PctRemaining == nil || *facts.PctRemaining != remaining {
		t.Fatalf("PctRemaining = %v, want %.1f", facts.PctRemaining, remaining)
	}
	if facts.ResetAt == nil || !facts.ResetAt.Equal(reset) {
		t.Fatalf("ResetAt = %v, want %v", facts.ResetAt, reset)
	}
}

func TestBuildFactsPrefersCodexRateLimitReset(t *testing.T) {
	used := 16.0
	limit := 100.0
	reset := at("2026-08-22T19:10:00Z")
	snap := core.NewUsageSnapshot("codex", "codex-cli")
	snap.Metrics["rate_limit_primary"] = core.Metric{
		Used: &used, Limit: &limit, Unit: "%", Window: "7d",
	}
	snap.Metrics["plan_percent_used"] = core.Metric{
		Used: &used, Limit: &limit, Unit: "%", Window: "7d",
	}
	snap.Resets["rate_limit_primary"] = reset

	facts := BuildFacts(snap, time.Date(2026, 8, 16, 16, 0, 0, 0, time.UTC))
	if facts.ResetAt == nil || !facts.ResetAt.Equal(reset) {
		t.Fatalf("ResetAt = %v, want %v", facts.ResetAt, reset)
	}
}

func TestBuildFactsUsesQuotaForecastMetric(t *testing.T) {
	monthlyUsed := 80.0
	rollingUsed := 10.0
	runoutHours := 2.0
	monthlyReset := at("2026-08-20T12:00:00Z")
	rollingReset := at("2026-08-16T13:00:00Z")
	snap := core.NewUsageSnapshot("opencode", "default")
	snap.Metrics["monthly_usage_pct"] = core.Metric{
		Used: &monthlyUsed, Limit: core.Float64Ptr(100), Unit: "percent",
		Window: "month", ResetKey: "monthly_usage_pct_reset",
	}
	snap.Metrics["rolling_usage"] = core.Metric{
		Used: &rollingUsed, Limit: core.Float64Ptr(100), Unit: "percent",
		Window: "rolling-5h", ResetKey: "rolling_usage_reset",
	}
	snap.Metrics["quota_runout_hours"] = core.Metric{Used: &runoutHours, Unit: "hours"}
	snap.SetAttribute("quota_forecast_metric", "rolling_usage")
	snap.Resets["monthly_usage_pct_reset"] = monthlyReset
	snap.Resets["rolling_usage_reset"] = rollingReset

	facts := BuildFacts(snap, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	if facts.PctRemaining == nil || *facts.PctRemaining != 90 {
		t.Fatalf("PctRemaining = %v, want 90", facts.PctRemaining)
	}
	if facts.ResetAt == nil || !facts.ResetAt.Equal(rollingReset) {
		t.Fatalf("ResetAt = %v, want %v", facts.ResetAt, rollingReset)
	}
	if facts.RunoutBeforeReset {
		t.Fatal("RunoutBeforeReset = true, want false when runout follows the forecast window reset")
	}
}

func TestBuildFactsIgnoresNonQuotaCounters(t *testing.T) {
	used := 1204.0
	snap := core.NewUsageSnapshot("opencode", "default")
	snap.Metrics["weekly_usage"] = core.Metric{Used: &used, Unit: "requests", Window: "7d"}
	snap.Metrics["requests_today"] = core.Metric{Used: &used, Unit: "requests", Window: "1d"}

	facts := BuildFacts(snap, time.Now())
	if facts.PctRemaining != nil {
		t.Fatalf("PctRemaining = %v, want nil", facts.PctRemaining)
	}
	if facts.RequestsToday == nil || *facts.RequestsToday != used {
		t.Fatalf("RequestsToday = %v, want %.0f", facts.RequestsToday, used)
	}
}
