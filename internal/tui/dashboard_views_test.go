package tui

import (
	"testing"
)

func TestActiveDashboardView_AlwaysReturnsSplit(t *testing.T) {
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

func TestNormalizeDashboardViewMode_AlwaysMapsToSplit(t *testing.T) {
	cases := []string{"grid", "stacked", "tabs", "compare", "list", "split", "unknown"}
	for _, c := range cases {
		if got := normalizeDashboardViewMode(c); got != dashboardViewSplit {
			t.Fatalf("normalizeDashboardViewMode(%q) = %q, want %q", c, got, dashboardViewSplit)
		}
	}
}

func TestDashboardViewOptions_ContainsOnlySplit(t *testing.T) {
	if len(dashboardViewOptions) != 1 {
		t.Fatalf("len(dashboardViewOptions) = %d, want 1", len(dashboardViewOptions))
	}
	if dashboardViewOptions[0].ID != dashboardViewSplit {
		t.Fatalf("dashboardViewOptions[0].ID = %q, want %q", dashboardViewOptions[0].ID, dashboardViewSplit)
	}
}
