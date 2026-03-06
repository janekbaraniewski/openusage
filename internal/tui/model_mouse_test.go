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
