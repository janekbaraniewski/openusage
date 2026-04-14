package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestRequestRefreshInvokesCallback(t *testing.T) {
	m := Model{}
	m.timeWindow = core.TimeWindow7d

	refreshCalls := 0
	var gotWindow core.TimeWindow
	m.SetOnRefresh(func(window core.TimeWindow) {
		refreshCalls++
		gotWindow = window
	})

	updated := m.requestRefresh()
	if !updated.refreshing {
		t.Fatal("refreshing = false, want true")
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh callback calls = %d, want 1", refreshCalls)
	}
	if gotWindow != core.TimeWindow7d {
		t.Fatalf("refresh callback window = %q, want %q", gotWindow, core.TimeWindow7d)
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
	var gotWindow core.TimeWindow
	m.SetOnRefresh(func(window core.TimeWindow) {
		refreshCalls++
		gotWindow = window
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
	if gotWindow != core.TimeWindowAll {
		t.Fatalf("refresh callback window = %q, want %q", gotWindow, core.TimeWindowAll)
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
