package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestComputeDisplayInfo_MapsActivityFallbackToUsage(t *testing.T) {
	msgs := 12.0
	snap := core.UsageSnapshot{
		ProviderID: "ollama",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"messages_today": {Used: &msgs, Unit: "messages", Window: "1d"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Usage" {
		t.Fatalf("tagLabel = %q, want Usage", got.tagLabel)
	}
	if got.tagEmoji != "⚡" {
		t.Fatalf("tagEmoji = %q, want ⚡", got.tagEmoji)
	}
	if !strings.Contains(got.summary, "msgs today") {
		t.Fatalf("summary = %q, want messages summary", got.summary)
	}
}

func TestComputeDisplayInfo_MapsGenericMetricsFallbackToUsage(t *testing.T) {
	custom := 7.0
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"custom_counter": {Used: &custom, Unit: "count"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Usage" {
		t.Fatalf("tagLabel = %q, want Usage", got.tagLabel)
	}
	if got.tagEmoji != "⚡" {
		t.Fatalf("tagEmoji = %q, want ⚡", got.tagEmoji)
	}
}

func TestComputeDisplayInfo_PreservesCreditsTag(t *testing.T) {
	total := 42.0
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"total_cost_usd": {Used: &total, Unit: "USD", Window: "all_time"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Credits" {
		t.Fatalf("tagLabel = %q, want Credits", got.tagLabel)
	}
	if got.tagEmoji != "💰" {
		t.Fatalf("tagEmoji = %q, want 💰", got.tagEmoji)
	}
}

func TestComputeDisplayInfo_PreservesErrorStatusTag(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Status:     core.StatusError,
		Message:    "boom",
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Error" {
		t.Fatalf("tagLabel = %q, want Error", got.tagLabel)
	}
	if got.tagEmoji != "⚠" {
		t.Fatalf("tagEmoji = %q, want ⚠", got.tagEmoji)
	}
}

func TestComputeDisplayInfo_FallbackSkipsDerivedMetrics(t *testing.T) {
	derived := 999.0
	coreRPM := 42.0
	snap := core.UsageSnapshot{
		ProviderID: "copilot",
		Status:     core.StatusUnknown,
		Metrics: map[string]core.Metric{
			"model_gpt_5_tokens":  {Used: &derived, Unit: "tokens"},
			"client_cli_requests": {Used: &derived, Unit: "requests"},
			"gh_core_rpm":         {Used: &coreRPM, Unit: "rpm"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if !strings.Contains(strings.ToLower(got.summary), "core rpm") {
		t.Fatalf("summary = %q, want core rpm fallback metric", got.summary)
	}
}

func TestSnapshotsReady(t *testing.T) {
	if snapshotsReady(nil) {
		t.Fatal("snapshotsReady(nil) = true, want false")
	}

	notReady := map[string]core.UsageSnapshot{
		"a": {
			Status:      core.StatusUnknown,
			Metrics:     map[string]core.Metric{},
			Resets:      map[string]time.Time{},
			DailySeries: map[string][]core.TimePoint{},
		},
	}
	if snapshotsReady(notReady) {
		t.Fatal("snapshotsReady(notReady) = true, want false")
	}

	messageOnly := map[string]core.UsageSnapshot{
		"a": {
			Status:  core.StatusUnknown,
			Message: "connecting to telemetry daemon...",
		},
	}
	if snapshotsReady(messageOnly) {
		t.Fatal("snapshotsReady(messageOnly) = true, want false")
	}

	ready := map[string]core.UsageSnapshot{
		"a": {
			Status: core.StatusUnknown,
			Metrics: map[string]core.Metric{
				"messages_today": {Used: float64Ptr(1), Unit: "messages"},
			},
		},
	}
	if !snapshotsReady(ready) {
		t.Fatal("snapshotsReady(ready) = false, want true")
	}
}

func TestComputeDisplayInfo_SpendLimitWithoutIndividualSpend(t *testing.T) {
	used := 488.0
	limit := 3600.0
	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"spend_limit": {Used: &used, Limit: &limit, Unit: "USD"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Credits" {
		t.Fatalf("tagLabel = %q, want Credits", got.tagLabel)
	}
	if !strings.Contains(got.summary, "$488 / $3600 spent") {
		t.Fatalf("summary = %q, want '$488 / $3600 spent'", got.summary)
	}
	if !strings.Contains(got.detail, "$3112 remaining") {
		t.Fatalf("detail = %q, want '$3112 remaining'", got.detail)
	}
}

func TestComputeDisplayInfo_SpendLimitWithIndividualSpend(t *testing.T) {
	used := 488.0
	limit := 3600.0
	indivUsed := 200.0
	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"spend_limit":      {Used: &used, Limit: &limit, Unit: "USD"},
			"individual_spend": {Used: &indivUsed, Unit: "USD"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Credits" {
		t.Fatalf("tagLabel = %q, want Credits", got.tagLabel)
	}
	if !strings.Contains(got.summary, "$488 / $3600 spent") {
		t.Fatalf("summary = %q, want '$488 / $3600 spent'", got.summary)
	}
	// Should show self vs team breakdown
	if !strings.Contains(got.detail, "you $200") {
		t.Fatalf("detail = %q, want 'you $200' in breakdown", got.detail)
	}
	if !strings.Contains(got.detail, "team $288") {
		t.Fatalf("detail = %q, want 'team $288' in breakdown", got.detail)
	}
	if !strings.Contains(got.detail, "$3112 remaining") {
		t.Fatalf("detail = %q, want '$3112 remaining' in breakdown", got.detail)
	}
}

func TestComputeDisplayInfo_IndividualSpendClampedToZero(t *testing.T) {
	used := 100.0
	limit := 3600.0
	// individual_spend > total used (edge case / data inconsistency)
	indivUsed := 150.0
	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"spend_limit":      {Used: &used, Limit: &limit, Unit: "USD"},
			"individual_spend": {Used: &indivUsed, Unit: "USD"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	// team portion should be clamped to 0, not negative
	if !strings.Contains(got.detail, "team $0") {
		t.Fatalf("detail = %q, want 'team $0' (clamped)", got.detail)
	}
}

func TestUpdate_SnapshotsMsgMarksModelReadyOnFirstFrame(t *testing.T) {
	m := NewModel(0.2, 0.1, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	if m.hasData {
		t.Fatal("expected hasData=false on fresh model")
	}

	snaps := SnapshotsMsg{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Status:     core.StatusUnknown,
			Message:    "daemon warming up",
			Metrics:    map[string]core.Metric{},
		},
	}

	updated, _ := m.Update(snaps)
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updated)
	}
	if !got.hasData {
		t.Fatal("expected hasData=true after first snapshots frame")
	}
}

func TestUpdate_AppUpdateMsgStoresNotice(t *testing.T) {
	m := NewModel(0.2, 0.1, false, config.DashboardConfig{}, nil, core.TimeWindow30d)

	updated, _ := m.Update(AppUpdateMsg{
		CurrentVersion: "v0.4.0",
		LatestVersion:  "v0.5.0",
		UpgradeHint:    "brew upgrade janekbaraniewski/tap/openusage",
	})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updated)
	}
	if got.appUpdateCurrent != "v0.4.0" {
		t.Fatalf("appUpdateCurrent = %q, want v0.4.0", got.appUpdateCurrent)
	}
	if got.appUpdateLatest != "v0.5.0" {
		t.Fatalf("appUpdateLatest = %q, want v0.5.0", got.appUpdateLatest)
	}
	if got.appUpdateHint != "brew upgrade janekbaraniewski/tap/openusage" {
		t.Fatalf("appUpdateHint = %q", got.appUpdateHint)
	}
}

func TestRenderFooterStatusLine_ShowsAppUpdateWhenIdle(t *testing.T) {
	m := NewModel(0.2, 0.1, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.appUpdateCurrent = "v0.4.0"
	m.appUpdateLatest = "v0.5.0"
	m.appUpdateHint = "go install github.com/janekbaraniewski/openusage/cmd/openusage@latest"

	line := m.renderFooterStatusLine(180)

	if !strings.Contains(line, "Update available: v0.4.0 -> v0.5.0") {
		t.Fatalf("footer line missing update versions, got: %q", line)
	}
	if !strings.Contains(line, "Run: go install github.com/janekbaraniewski/openusage/cmd/openusage@latest") {
		t.Fatalf("footer line missing update command, got: %q", line)
	}
}

func TestComputeDisplayInfo_UsageFiveHourBranch(t *testing.T) {
	fiveHour := 57.0
	todayCost := 55.57
	snap := core.UsageSnapshot{
		ProviderID: "claude_code",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"usage_five_hour": {Used: &fiveHour, Unit: "%", Window: "5h"},
			"today_api_cost":  {Used: &todayCost, Unit: "USD", Window: "1d"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Usage" {
		t.Fatalf("tagLabel = %q, want Usage", got.tagLabel)
	}
	if got.tagEmoji != "⚡" {
		t.Fatalf("tagEmoji = %q, want ⚡", got.tagEmoji)
	}
	if got.gaugePercent != 57.0 {
		t.Fatalf("gaugePercent = %v, want 57.0", got.gaugePercent)
	}
	if !strings.Contains(got.summary, "5h 57%") {
		t.Fatalf("summary = %q, want '5h 57%%'", got.summary)
	}
	if got.reason != "usage_five_hour" {
		t.Fatalf("reason = %q, want usage_five_hour", got.reason)
	}
}

func TestComputeDisplayInfo_TodayApiCostBranchWithoutFiveHour(t *testing.T) {
	todayCost := 55.57
	snap := core.UsageSnapshot{
		ProviderID: "claude_code",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"today_api_cost": {Used: &todayCost, Unit: "USD", Window: "1d"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Credits" {
		t.Fatalf("tagLabel = %q, want Credits", got.tagLabel)
	}
	if got.tagEmoji != "💰" {
		t.Fatalf("tagEmoji = %q, want 💰", got.tagEmoji)
	}
	if got.gaugePercent != -1 {
		t.Fatalf("gaugePercent = %v, want -1 (no gauge)", got.gaugePercent)
	}
	if !strings.Contains(got.summary, "$55.57 today") {
		t.Fatalf("summary = %q, want '$55.57 today'", got.summary)
	}
	if got.reason != "today_api_cost" {
		t.Fatalf("reason = %q, want today_api_cost", got.reason)
	}
}

func TestComputeDisplayInfo_BillingBlockFallbackClassifiesAsUsage(t *testing.T) {
	todayCost := 161.85
	burnRate := 31.42
	blockCost := 94.93
	snap := core.UsageSnapshot{
		ProviderID: "claude_code",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"today_api_cost": {Used: &todayCost, Unit: "USD", Window: "1d"},
			"burn_rate":      {Used: &burnRate, Unit: "USD/h"},
			"5h_block_cost":  {Used: &blockCost, Unit: "USD"},
		},
		Resets: map[string]time.Time{
			"billing_block": time.Now().Add(2 * time.Hour),
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Usage" {
		t.Fatalf("tagLabel = %q, want Usage", got.tagLabel)
	}
	if got.tagEmoji != "⚡" {
		t.Fatalf("tagEmoji = %q, want ⚡", got.tagEmoji)
	}
	if got.reason != "billing_block_fallback" {
		t.Fatalf("reason = %q, want billing_block_fallback", got.reason)
	}
	if !strings.Contains(got.summary, "$161.85 today") {
		t.Fatalf("summary = %q, want '$161.85 today'", got.summary)
	}
	if !strings.Contains(got.detail, "$94.93 5h block") {
		t.Fatalf("detail = %q, want '$94.93 5h block'", got.detail)
	}
}
