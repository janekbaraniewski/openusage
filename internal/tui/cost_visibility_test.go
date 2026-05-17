package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestVisibleSnapshotsHideCostsForConfiguredAccount(t *testing.T) {
	cost := 12.34
	tokens := 1000.0
	snap := core.NewUsageSnapshot("claude_code", "claude_code")
	snap.EnsureMaps()
	snap.DailySeries = make(map[string][]core.TimePoint)
	snap.Metrics["today_cost"] = core.Metric{Used: &cost, Unit: "USD"}
	snap.Metrics["usage_five_hour"] = core.Metric{Used: &tokens, Limit: core.Float64Ptr(5000), Unit: "tokens", Window: "5h"}
	snap.DailySeries["cost"] = []core.TimePoint{{Date: "2026-05-17", Value: cost}}
	snap.ModelUsage = []core.ModelUsageRecord{{RawModelID: "claude-sonnet", CostUSD: &cost, TotalTokens: &tokens}}

	model := NewModel(0.2, 0.05, false, config.DashboardConfig{
		Providers: []config.DashboardProviderConfig{{AccountID: "claude_code", Enabled: true, HideCosts: true}},
	}, []core.AccountConfig{{ID: "claude_code", Provider: "claude_code"}}, core.TimeWindow7d)
	model.snapshots["claude_code"] = snap

	visible := model.visibleSnapshots()["claude_code"]
	if _, ok := visible.Metrics["today_cost"]; ok {
		t.Fatal("expected cost metric to be hidden")
	}
	if _, ok := visible.DailySeries["cost"]; ok {
		t.Fatal("expected cost series to be hidden")
	}
	if len(visible.ModelUsage) != 1 || visible.ModelUsage[0].CostUSD != nil {
		t.Fatalf("expected model cost to be hidden, got %+v", visible.ModelUsage)
	}
	if _, ok := visible.Metrics["usage_five_hour"]; !ok {
		t.Fatal("expected non-monetary usage metric to remain visible")
	}
}

func TestUsageGaugeForecastLineProjectsLimitHit(t *testing.T) {
	resetAt := time.Now().Add(4 * time.Hour)
	line := usageGaugeForecastLine("usage_five_hour", core.Metric{
		Used:   core.Float64Ptr(40),
		Limit:  core.Float64Ptr(100),
		Unit:   "%",
		Window: "5h",
	}, 40, map[string]time.Time{"usage_five_hour": resetAt})

	if !strings.Contains(line, "100% in") {
		t.Fatalf("expected forecast hit line, got %q", line)
	}
}
