package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func testSnapshots(ids ...string) map[string]core.UsageSnapshot {
	snaps := make(map[string]core.UsageSnapshot, len(ids))
	for _, id := range ids {
		snaps[id] = core.UsageSnapshot{
			AccountID:  id,
			ProviderID: id,
		}
	}
	return snaps
}

func TestMouseWheelScrollsTilesInSingleColumn(t *testing.T) {
	m := Model{
		width:     90,
		height:    40,
		sortedIDs: []string{"a", "b", "c", "d"},
		snapshots: testSnapshots("a", "b", "c", "d"),
	}

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	got := updated.(Model).tileOffset
	if got <= 0 {
		t.Fatalf("tileOffset = %d, want > 0", got)
	}
}

func TestMouseWheelScrollsSelectedWidgetInMultiColumn(t *testing.T) {
	m := Model{
		width:     220,
		height:    40,
		sortedIDs: []string{"a", "b", "c", "d", "e", "f"},
		snapshots: testSnapshots("a", "b", "c", "d", "e", "f"),
	}

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	got := updated.(Model).tileOffset
	if got <= 0 {
		t.Fatalf("tileOffset = %d, want > 0", got)
	}
}

func TestMouseWheelUpClampsTileOffsetAtZero(t *testing.T) {
	m := Model{
		width:      90,
		height:     40,
		sortedIDs:  []string{"a", "b", "c"},
		snapshots:  testSnapshots("a", "b", "c"),
		tileOffset: 1,
	}

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	got := updated.(Model).tileOffset
	if got != 0 {
		t.Fatalf("tileOffset = %d, want 0", got)
	}
}

func TestMouseWheelScrollsWidgetInSplitView(t *testing.T) {
	m := Model{
		width:         220,
		height:        40,
		dashboardView: dashboardViewSplit,
		sortedIDs:     []string{"a", "b", "c"},
		snapshots:     testSnapshots("a", "b", "c"),
	}

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	got := updated.(Model).tileOffset
	if got <= 0 {
		t.Fatalf("tileOffset = %d, want > 0", got)
	}
}

func TestMouseWheelScrollsSettingsWidgetPreview(t *testing.T) {
	m := NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{},
		[]core.AccountConfig{{ID: "claude-preview", Provider: "claude_code"}},
		core.TimeWindow7d,
	)
	m.settings.show = true
	m.settings.tab = settingsTabWidgetSections

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	got := updated.(Model).settings.previewOffset
	if got <= 0 {
		t.Fatalf("settingsPreviewOffset = %d, want > 0", got)
	}
}

func TestMouseWheelUpClampsSettingsWidgetPreviewOffsetAtZero(t *testing.T) {
	m := NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{},
		[]core.AccountConfig{{ID: "claude-preview", Provider: "claude_code"}},
		core.TimeWindow7d,
	)
	m.settings.show = true
	m.settings.tab = settingsTabWidgetSections
	m.settings.previewOffset = 1

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	got := updated.(Model).settings.previewOffset
	if got != 0 {
		t.Fatalf("settingsPreviewOffset = %d, want 0", got)
	}
}

func TestMouseWheelDoesNotScrollSettingsPreviewOutsideWidgetSectionsTab(t *testing.T) {
	m := NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{},
		[]core.AccountConfig{{ID: "claude-preview", Provider: "claude_code"}},
		core.TimeWindow7d,
	)
	m.settings.show = true
	m.settings.tab = settingsTabTheme
	m.settings.previewOffset = 0

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	got := updated.(Model).settings.previewOffset
	if got != 0 {
		t.Fatalf("settingsPreviewOffset = %d, want 0", got)
	}
}

func TestMouseLeftClick_HeaderTabsSwitch(t *testing.T) {
	m := Model{
		width:                 120,
		height:                40,
		experimentalAnalytics: true,
		sortedIDs:             []string{"a"},
		snapshots:             testSnapshots("a"),
	}

	// Click on 2:Analytics tab in header (Y=0, X ~ 28)
	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      28,
		Y:      0,
	})
	got := updated.(Model)
	if got.screen != screenAnalytics {
		t.Fatalf("screen = %v, want screenAnalytics", got.screen)
	}

	// Click on 1:Dashboard tab in header (Y=0, X ~ 16)
	updated2, _ := got.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      16,
		Y:      0,
	})
	got2 := updated2.(Model)
	if got2.screen != screenDashboard {
		t.Fatalf("screen = %v, want screenDashboard", got2.screen)
	}
}

func TestMouseLeftClick_HelpOverlayDismissal(t *testing.T) {
	m := Model{
		width:     90,
		height:    40,
		showHelp:  true,
		sortedIDs: []string{"a"},
		snapshots: testSnapshots("a"),
	}

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      10,
		Y:      10,
	})
	got := updated.(Model)
	if got.showHelp {
		t.Fatalf("showHelp = true, want false after mouse click")
	}
}

func TestMouseLeftClick_SplitViewNavigator(t *testing.T) {
	m := Model{
		width:         120,
		height:        40,
		dashboardView: dashboardViewSplit,
		cursor:        0,
		sortedIDs:     []string{"a", "b", "c"},
		snapshots:     testSnapshots("a", "b", "c"),
	}

	// Left navigator item click (Y=6 is item 1)
	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      10,
		Y:      6,
	})
	got := updated.(Model)
	if got.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", got.cursor)
	}

	// Right focus pane click (X > leftW) -> stays in list mode without entering detail mode
	updated2, _ := got.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      80,
		Y:      10,
	})
	got2 := updated2.(Model)
	if got2.mode != modeList {
		t.Fatalf("mode = %v, want modeList", got2.mode)
	}
}

func TestMouseLeftClick_SettingsModalOutsideDismiss(t *testing.T) {
	m := Model{
		width:     120,
		height:    40,
		sortedIDs: []string{"a"},
		snapshots: testSnapshots("a"),
	}
	m.settings.show = true

	// Click far top-left outside modal
	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      2,
		Y:      2,
	})
	got := updated.(Model)
	if got.settings.show {
		t.Fatalf("settings.show = true, want false after clicking outside")
	}
}
