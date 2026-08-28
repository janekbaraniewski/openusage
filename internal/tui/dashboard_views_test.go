package tui

import (
	"testing"
)

func TestActiveDashboardView_AlwaysReturnsStacked(t *testing.T) {
	m := Model{
		dashboardView: dashboardViewSplit,
		width:         220,
		sortedIDs:     []string{"a", "b", "c"},
		snapshots:     testSnapshots("a", "b", "c"),
	}

	if got := m.activeDashboardView(); got != dashboardViewStacked {
		t.Fatalf("activeDashboardView = %q, want %q", got, dashboardViewStacked)
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
