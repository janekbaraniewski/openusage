package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestRequestRefreshInvokesCallback(t *testing.T) {
	m := Model{}
	m.timeWindow = core.TimeWindow7d

	refreshCalls := 0
	var got RefreshRequest
	m.SetOnRefresh(func(req RefreshRequest) uint64 {
		refreshCalls++
		got = req
		return 42
	})

	updated := m.requestRefresh(RefreshRequest{
		TimeWindow: core.TimeWindow7d,
		AccountID:  "cursor-main",
	})
	if !updated.refreshing {
		t.Fatal("refreshing = false, want true")
	}
	if updated.pendingRefreshRequestID != 42 {
		t.Fatalf("pendingRefreshRequestID = %d, want 42", updated.pendingRefreshRequestID)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh callback calls = %d, want 1", refreshCalls)
	}
	if got.TimeWindow != core.TimeWindow7d {
		t.Fatalf("refresh callback window = %q, want %q", got.TimeWindow, core.TimeWindow7d)
	}
	if got.AccountID != "cursor-main" {
		t.Fatalf("refresh callback account = %q, want cursor-main", got.AccountID)
	}
}

func TestTriggerRefreshAllShowsFetchingAllStatus(t *testing.T) {
	m := Model{
		hasData:     true,
		animFrame:   1,
		tickRunning: false,
	}
	m.SetOnRefresh(func(_ RefreshRequest) uint64 { return 1 })

	updated, cmd := m.triggerRefreshAll()
	if !updated.refreshing {
		t.Fatal("refreshing = false, want true")
	}
	if !updated.refreshAll {
		t.Fatal("refreshAll = false, want true")
	}
	if cmd == nil {
		t.Fatal("expected tick cmd for fetching animation")
	}
	footer := updated.renderFooterStatusLine(120)
	if !strings.Contains(footer, "Fetching all") {
		t.Fatalf("footer = %q, want Fetching all status", footer)
	}
}

func TestTriggerRefreshFocusedScopesAccount(t *testing.T) {
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.hasData = true
	m.sortedIDs = []string{"cursor-main", "opencode"}
	m.snapshots = map[string]core.UsageSnapshot{
		"cursor-main": {ProviderID: "cursor", AccountID: "cursor-main", Status: core.StatusOK},
		"opencode":    {ProviderID: "opencode", AccountID: "opencode", Status: core.StatusOK},
	}
	m.cursor = 1

	var got RefreshRequest
	m.SetOnRefresh(func(req RefreshRequest) uint64 {
		got = req
		return 9
	})

	updated, _ := m.triggerRefreshFocused()
	if updated.refreshAll {
		t.Fatal("refreshAll = true, want focused refresh")
	}
	if got.AccountID != "opencode" {
		t.Fatalf("accountID = %q, want opencode", got.AccountID)
	}
}

func TestRefreshFooterHintDocumentsBothKeys(t *testing.T) {
	hint := refreshFooterHint()
	if !strings.Contains(hint, "r refresh") {
		t.Fatalf("hint = %q, want r refresh", hint)
	}
	if !strings.Contains(hint, "R refresh all") {
		t.Fatalf("hint = %q, want R refresh all", hint)
	}

	m := Model{hasData: true, width: 160, height: 40}
	footer := m.renderFooterStatusLine(160)
	if !strings.Contains(footer, "R refresh all") {
		t.Fatalf("footer = %q, want R refresh all hint", footer)
	}
}

func TestRefreshingSurvivesBroadcastSnapshot(t *testing.T) {
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.hasData = true
	m.refreshing = true
	m.pendingRefreshRequestID = 5
	m.lastSnapshotRequestID = 3
	m.snapshots = map[string]core.UsageSnapshot{
		"a": {
			ProviderID: "cursor",
			AccountID:  "a",
			Status:     core.StatusOK,
			Metrics:    map[string]core.Metric{"plan_percent_used": {Used: core.Float64Ptr(1)}},
		},
	}

	broadcast, _ := m.handleSnapshotsMsg(SnapshotsMsg{
		RequestID:  4,
		TimeWindow: core.TimeWindow30d,
		Snapshots: map[string]core.UsageSnapshot{
			"a": {
				ProviderID: "cursor",
				AccountID:  "a",
				Status:     core.StatusOK,
				Metrics:    map[string]core.Metric{"plan_percent_used": {Used: core.Float64Ptr(2)}},
			},
		},
	})
	b := broadcast.(Model)
	if !b.refreshing {
		t.Fatal("refreshing cleared by unrelated broadcast snapshot")
	}
	if b.pendingRefreshRequestID != 5 {
		t.Fatalf("pendingRefreshRequestID = %d, want 5", b.pendingRefreshRequestID)
	}

	completed, _ := b.handleSnapshotsMsg(SnapshotsMsg{
		RequestID:  5,
		TimeWindow: core.TimeWindow30d,
		Snapshots: map[string]core.UsageSnapshot{
			"a": {
				ProviderID: "cursor",
				AccountID:  "a",
				Status:     core.StatusOK,
				Metrics:    map[string]core.Metric{"plan_percent_used": {Used: core.Float64Ptr(9)}},
			},
		},
	})
	c := completed.(Model)
	if c.refreshing {
		t.Fatal("refreshing still true after pending refresh completed")
	}
	if c.pendingRefreshRequestID != 0 {
		t.Fatalf("pendingRefreshRequestID = %d, want 0", c.pendingRefreshRequestID)
	}
	if metric := c.snapshots["a"].Metrics["plan_percent_used"]; metric.Used == nil || *metric.Used != 9 {
		t.Fatalf("refresh snapshot not applied: %+v", metric)
	}
}

func TestEnterDetailModePreservesTimeWindow(t *testing.T) {
	m := Model{
		timeWindow:      core.TimeWindow7d,
		detailOffset:    12,
		lastDataUpdate:  time.Now(),
		lastInteraction: time.Now(),
	}

	updated := m.enterDetailMode()

	if updated.mode != modeDetail {
		t.Fatalf("mode = %v, want %v", updated.mode, modeDetail)
	}
	if updated.timeWindow != core.TimeWindow7d {
		t.Fatalf("timeWindow = %q, want %q", updated.timeWindow, core.TimeWindow7d)
	}
	if updated.detailOffset != 0 {
		t.Fatalf("detailOffset = %d, want 0", updated.detailOffset)
	}
}

func TestBeginTimeWindowRefreshRequestsSelectedWindow(t *testing.T) {
	m := Model{
		timeWindow: core.TimeWindow30d,
		mode:       modeDetail,
	}

	refreshCalls := 0
	var got RefreshRequest
	m.SetOnRefresh(func(req RefreshRequest) uint64 {
		refreshCalls++
		got = req
		return 7
	})

	updated := m.beginTimeWindowRefresh(core.TimeWindowAll)

	if updated.timeWindow != core.TimeWindowAll {
		t.Fatalf("timeWindow = %q, want %q", updated.timeWindow, core.TimeWindowAll)
	}
	if !updated.refreshing {
		t.Fatal("refreshing = false, want true")
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh callback calls = %d, want 1", refreshCalls)
	}
	if got.TimeWindow != core.TimeWindowAll {
		t.Fatalf("refresh callback window = %q, want %q", got.TimeWindow, core.TimeWindowAll)
	}
	if updated.pendingRefreshRequestID != 7 {
		t.Fatalf("pendingRefreshRequestID = %d, want 7", updated.pendingRefreshRequestID)
	}
}

func TestHandleKey_DetailTabNavigatesSectionsInsteadOfSwitchingScreen(t *testing.T) {
	m := Model{
		screen:                screenDashboard,
		mode:                  modeDetail,
		experimentalAnalytics: true,
		width:                 120,
		height:                40,
		sortedIDs:             []string{"codex-cli"},
		snapshots: map[string]core.UsageSnapshot{
			"codex-cli": {
				ProviderID: "codex",
				AccountID:  "codex-cli",
				Timestamp:  time.Now(),
				Metrics: map[string]core.Metric{
					"usage_five_hour": {Used: core.Float64Ptr(10), Unit: "percent", Window: "5h"},
					"credit_balance":  {Used: core.Float64Ptr(12), Unit: "USD", Window: "month"},
				},
			},
		},
	}

	updatedModel, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	updated := updatedModel.(Model)

	if updated.screen != screenDashboard {
		t.Fatalf("screen = %v, want %v", updated.screen, screenDashboard)
	}
	if updated.mode != modeDetail {
		t.Fatalf("mode = %v, want %v", updated.mode, modeDetail)
	}
	if updated.detailOffset <= 0 {
		t.Fatalf("detailOffset = %d, want section jump > 0", updated.detailOffset)
	}
}

func TestHandleKey_DetailArrowsNavigateSectionsInsteadOfExiting(t *testing.T) {
	m := Model{
		screen:    screenDashboard,
		mode:      modeDetail,
		width:     120,
		height:    40,
		sortedIDs: []string{"codex-cli"},
		snapshots: map[string]core.UsageSnapshot{
			"codex-cli": {
				ProviderID: "codex",
				AccountID:  "codex-cli",
				Timestamp:  time.Now(),
				Metrics: map[string]core.Metric{
					"usage_five_hour": {Used: core.Float64Ptr(10), Unit: "percent", Window: "5h"},
					"credit_balance":  {Used: core.Float64Ptr(12), Unit: "USD", Window: "month"},
				},
			},
		},
	}

	nextModel, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	next := nextModel.(Model)
	if next.mode != modeDetail {
		t.Fatalf("mode after right = %v, want %v", next.mode, modeDetail)
	}
	if next.detailOffset <= 0 {
		t.Fatalf("detailOffset after right = %d, want > 0", next.detailOffset)
	}

	prevModel, _ := next.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	prev := prevModel.(Model)
	if prev.mode != modeDetail {
		t.Fatalf("mode after left = %v, want %v", prev.mode, modeDetail)
	}
	if prev.detailOffset != 0 {
		t.Fatalf("detailOffset after left = %d, want 0", prev.detailOffset)
	}
}
