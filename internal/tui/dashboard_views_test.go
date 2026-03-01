package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestActiveDashboardView_ForcedStackedWhenNarrow(t *testing.T) {
	m := Model{
		dashboardView: dashboardViewSplit,
		width:         minTwoColumnDashboardWidth() - 1,
		sortedIDs:     []string{"a", "b", "c"},
		snapshots:     testSnapshots("a", "b", "c"),
	}

	if got := m.activeDashboardView(); got != dashboardViewStacked {
		t.Fatalf("activeDashboardView = %q, want %q", got, dashboardViewStacked)
	}
}

func TestActiveDashboardView_ForcedStackedWhenNarrowEvenForTabs(t *testing.T) {
	m := Model{
		dashboardView: dashboardViewTabs,
		width:         minTwoColumnDashboardWidth() - 1,
		sortedIDs:     []string{"a", "b", "c"},
		snapshots:     testSnapshots("a", "b", "c"),
	}

	if got := m.activeDashboardView(); got != dashboardViewStacked {
		t.Fatalf("activeDashboardView = %q, want %q", got, dashboardViewStacked)
	}
}

func TestActiveDashboardView_UsesConfiguredWhenWide(t *testing.T) {
	m := Model{
		dashboardView: dashboardViewSplit,
		width:         minTwoColumnDashboardWidth() + 10,
		sortedIDs:     []string{"a", "b", "c"},
		snapshots:     testSnapshots("a", "b", "c"),
	}

	if got := m.activeDashboardView(); got != dashboardViewSplit {
		t.Fatalf("activeDashboardView = %q, want %q", got, dashboardViewSplit)
	}
}

func TestHandleDashboardTilesKey_SplitViewUsesListNavigation(t *testing.T) {
	m := Model{
		dashboardView: dashboardViewSplit,
		width:         220,
		sortedIDs:     []string{"a", "b", "c", "d"},
		snapshots:     testSnapshots("a", "b", "c", "d"),
	}

	updated, _ := m.handleDashboardTilesKey(tea.KeyMsg{Type: tea.KeyDown})
	got := updated.(Model)

	if got.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", got.cursor)
	}
}

func TestNormalizeDashboardViewMode_LegacyListMapsToSplit(t *testing.T) {
	if got := normalizeDashboardViewMode("list"); got != dashboardViewSplit {
		t.Fatalf("normalizeDashboardViewMode(list) = %q, want %q", got, dashboardViewSplit)
	}
}

func TestDashboardViewOptions_DoNotExposeLegacyList(t *testing.T) {
	for _, option := range dashboardViewOptions {
		if option.ID == dashboardViewMode("list") {
			t.Fatalf("legacy list view should not be exposed in options: %#v", option)
		}
	}
}

func TestHandleKey_CyclesDashboardView(t *testing.T) {
	m := Model{
		dashboardView: dashboardViewGrid,
		screen:        screenDashboard,
	}

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	got := updated.(Model)

	if got.dashboardView != dashboardViewStacked {
		t.Fatalf("dashboardView = %q, want %q", got.dashboardView, dashboardViewStacked)
	}
	if cmd == nil {
		t.Fatal("expected persist command when cycling dashboard view")
	}
}

func TestSettingsModalKey_ViewTabAppliesSelection(t *testing.T) {
	m := Model{
		showSettingsModal:  true,
		settingsModalTab:   settingsTabView,
		dashboardView:      dashboardViewGrid,
		settingsViewCursor: 1,
	}

	updated, cmd := m.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)

	if got.dashboardView != dashboardViewStacked {
		t.Fatalf("dashboardView = %q, want %q", got.dashboardView, dashboardViewStacked)
	}
	if cmd == nil {
		t.Fatal("expected persist command when applying view in settings")
	}
}
