package tui

import (
	"strings"
	"testing"
)

func TestRenderSplitPanes_FocusPaneMatchesDetailView(t *testing.T) {
	m := Model{
		width:         120,
		height:        28,
		dashboardView: dashboardViewSplit,
		sortedIDs:     []string{"openrouter"},
		snapshots:     testSnapshots("openrouter"),
	}

	split := m.renderSplitPanes(120, 20)
	detail := m.renderDetailPanel(81, 20)

	if !strings.Contains(split, "openrouter") {
		t.Fatalf("expected navigator and focus pane items to be visible, got:\n%s", split)
	}
	if detail == "" {
		t.Fatal("expected detail panel content")
	}
	if !strings.Contains(split, strings.TrimSpace(strings.Split(detail, "\n")[0])) {
		t.Fatalf("expected focus pane to render detail view content, got split:\n%s\n\ndetail:\n%s", split, detail)
	}
}

func TestRenderSplitPanes_ListScrollCentering(t *testing.T) {
	ids := []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10"}
	snaps := testSnapshots(ids...)
	m := Model{
		width:         120,
		height:        20,
		dashboardView: dashboardViewSplit,
		sortedIDs:     ids,
		snapshots:     snaps,
		cursor:        5,
	}

	start, end := listVisibleWindow(snaps, ids, m.cursor, 15)
	if m.cursor < start || m.cursor >= end {
		t.Fatalf("cursor %d should be within visible window [%d, %d)", m.cursor, start, end)
	}

	out := m.renderList(30, 15)
	if !strings.Contains(out, "p6") {
		t.Fatalf("expected cursor item p6 to be rendered in list, got:\n%s", out)
	}
}
