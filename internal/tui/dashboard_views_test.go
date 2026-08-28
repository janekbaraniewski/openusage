package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestActiveDashboardView_UsesConfiguredView(t *testing.T) {
	m := Model{
		dashboardView: dashboardViewSplit,
		width:         120,
		sortedIDs:     []string{"a", "b", "c"},
		snapshots:     testSnapshots("a", "b", "c"),
	}

	if got := m.activeDashboardView(); got != dashboardViewSplit {
		t.Fatalf("activeDashboardView = %q, want %q", got, dashboardViewSplit)
	}
}

func TestActiveDashboardView_ForcedStackedWhenGridTooNarrow(t *testing.T) {
	m := Model{
		dashboardView: dashboardViewGrid,
		width:         minTwoColumnDashboardWidth() - 1,
		sortedIDs:     []string{"a", "b", "c"},
		snapshots:     testSnapshots("a", "b", "c"),
	}

	if got := m.activeDashboardView(); got != dashboardViewStacked {
		t.Fatalf("activeDashboardView = %q, want %q", got, dashboardViewStacked)
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
		settings:      settingsState{show: true, tab: settingsTabView, viewCursor: 1},
		dashboardView: dashboardViewGrid,
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
