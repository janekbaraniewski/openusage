package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// snapshotWithCosts returns a snapshot that exercises the dollar-amount paths:
// today_api_cost, 5h_block_cost, and a burn_rate-derived cost summary.
func snapshotWithCosts() core.UsageSnapshot {
	today := 1.23
	block := 0.45
	return core.UsageSnapshot{
		ProviderID: "claude_code",
		AccountID:  "claude-code",
		Status:     core.StatusOK,
		Timestamp:  time.Now(),
		Metrics: map[string]core.Metric{
			"today_api_cost": {Used: &today, Unit: "USD", Window: "today"},
			"5h_block_cost":  {Used: &block, Unit: "USD", Window: "5h"},
		},
		Raw: map[string]string{"subscription": "active"},
	}
}

// snapshotWithBroadCostSurfaces seeds every $-rendering surface we care about:
// compact rows (today/5h/7d/all-time), window_cost, model & provider mixes
// (CostUSD), and a daily cost series. Exercises the full Dashboard render path.
func snapshotWithBroadCostSurfaces() core.UsageSnapshot {
	today := 670.86
	block := 667.60
	week := 670.86
	allTime := 2750.0
	wcost := 670.86
	wreqs := 1889.0
	wtoks := 5_300_000.0
	burn := 3.26

	mInput := 3_000_000.0
	mOutput := 1_800_000.0
	mReq := 1889.0
	mCost := 667.60

	costSeries := []core.TimePoint{
		{Date: "2026-05-15", Value: 200.0},
		{Date: "2026-05-16", Value: 200.0},
		{Date: "2026-05-17", Value: 270.86},
	}
	reqSeries := []core.TimePoint{
		{Date: "2026-05-15", Value: 600},
		{Date: "2026-05-16", Value: 600},
		{Date: "2026-05-17", Value: 689},
	}
	tokSeries := []core.TimePoint{
		{Date: "2026-05-15", Value: 1_800_000},
		{Date: "2026-05-16", Value: 1_800_000},
		{Date: "2026-05-17", Value: 1_700_000},
	}

	return core.UsageSnapshot{
		ProviderID: "claude_code",
		AccountID:  "claude-code",
		Status:     core.StatusOK,
		Timestamp:  time.Now(),
		Metrics: map[string]core.Metric{
			"today_api_cost":    {Used: &today, Unit: "USD", Window: "3d"},
			"5h_block_cost":     {Used: &block, Unit: "USD", Window: "5h"},
			"7d_api_cost":       {Used: &week, Unit: "USD", Window: "7d"},
			"all_time_api_cost": {Used: &allTime, Unit: "USD", Window: "all-time estimate"},
			"window_cost":       {Used: &wcost, Unit: "USD", Window: "3d"},
			"window_requests":   {Used: &wreqs, Unit: "requests", Window: "3d"},
			"window_tokens":     {Used: &wtoks, Unit: "tokens", Window: "3d"},
			"burn_rate":         {Used: &burn, Unit: "USD"},
		},
		ModelUsage: []core.ModelUsageRecord{{
			RawModelID:   "claude-sonnet-4-5",
			Canonical:    "claude-sonnet-4-5",
			InputTokens:  &mInput,
			OutputTokens: &mOutput,
			Requests:     &mReq,
			CostUSD:      &mCost,
		}},
		DailySeries: map[string][]core.TimePoint{
			"analytics_cost":     costSeries,
			"analytics_requests": reqSeries,
			"analytics_tokens":   tokSeries,
		},
		Raw: map[string]string{"subscription": "active"},
	}
}

func TestRenderDetailContent_HideCostsSuppressesDollars(t *testing.T) {
	snap := snapshotWithCosts()

	shown := RenderDetailContent(snap, time.Now(), 120, 0.20, 0.05, 0, core.TimeWindow30d, false)
	hidden := RenderDetailContent(snap, time.Now(), 120, 0.20, 0.05, 0, core.TimeWindow30d, true)

	if !strings.Contains(shown, "$1.23") {
		t.Errorf("expected $1.23 in shown render, missing")
	}
	if strings.Contains(hidden, "$1.23") {
		t.Errorf("expected $1.23 suppressed when hideCosts=true")
	}
	// The Spending and Forecast cards should not appear when hideCosts is on.
	if strings.Contains(hidden, "Spending") {
		t.Errorf("Spending card should be suppressed")
	}
	if strings.Contains(hidden, "Forecast") {
		t.Errorf("Forecast card should be suppressed")
	}
}

// TestRenderDetailContent_HideCostsNoDollarSign is the comprehensive screenshot
// guard: when hide-costs is on, the detail render of a snapshot bristling with
// every $-rendering surface (compact rows, window cost, model/provider burn,
// trends) must NOT contain a single literal "$" character.
func TestRenderDetailContent_HideCostsNoDollarSign(t *testing.T) {
	snap := snapshotWithBroadCostSurfaces()

	// Sanity: in the SHOWN render at least one of the known $ values appears,
	// so we know the test snapshot is actually wired into the render path.
	shown := RenderDetailContent(snap, time.Now(), 140, 0.20, 0.05, 0, core.TimeWindow30d, false)
	sawAny := false
	for _, want := range []string{"$670.86", "$667.60", "$2750", "$3.26"} {
		if strings.Contains(shown, want) {
			sawAny = true
			break
		}
	}
	if !sawAny {
		t.Fatalf("test fixture does not surface any expected $ values when shown — fixture is wrong, not the gating")
	}

	hidden := RenderDetailContent(snap, time.Now(), 140, 0.20, 0.05, 0, core.TimeWindow30d, true)

	// No literal "$" character anywhere in the hide-costs render. This catches
	// any new render site that emits dollars without going through the gating.
	if idx := strings.Index(hidden, "$"); idx >= 0 {
		start := idx - 40
		if start < 0 {
			start = 0
		}
		end := idx + 40
		if end > len(hidden) {
			end = len(hidden)
		}
		t.Fatalf("hide-costs render still contains $ at index %d: …%q…", idx, hidden[start:end])
	}

	// Belt and braces: none of the specific values should appear.
	for _, want := range []string{"670.86", "667.60", "2750", "3.26"} {
		if strings.Contains(hidden, want) {
			t.Errorf("hide-costs render contains %q (should be suppressed)", want)
		}
	}
}

func TestResolveHideCosts_ModelIntegration(t *testing.T) {
	// Subscription claude_code account: auto policy hides costs by default.
	subSnap := core.UsageSnapshot{
		ProviderID: "claude_code",
		AccountID:  "claude-code",
		Raw:        map[string]string{"subscription": "active"},
	}
	m := Model{}
	if !m.resolveHideCosts(subSnap) {
		t.Errorf("default auto policy should hide costs for subscription claude_code")
	}

	// Per-account override beats auto.
	show := false
	m.hideCostsByAccount = map[string]*bool{"claude-code": &show}
	if m.resolveHideCosts(subSnap) {
		t.Errorf("per-account override false should show costs")
	}

	// Global override beats auto when per-account is nil.
	m2 := Model{}
	hide := true
	m2.hideCostsGlobal = &hide
	apiSnap := core.UsageSnapshot{ProviderID: "openai", AccountID: "openai-key"}
	if !m2.resolveHideCosts(apiSnap) {
		t.Errorf("global=true should hide costs for openai")
	}
}
