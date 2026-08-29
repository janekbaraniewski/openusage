package tui

import (
	"strings"
	"testing"
)

func TestRenderSplitPanes_ShowsNavigatorAndFocusPane(t *testing.T) {
	m := Model{
		width:         120,
		height:        28,
		dashboardView: dashboardViewSplit,
		sortedIDs:     []string{"openrouter", "gemini-cli", "codex-cli"},
		snapshots:     testSnapshots("openrouter", "gemini-cli", "codex-cli"),
	}

	out := m.renderSplitPanes(120, 20)
	if !strings.Contains(out, "openrouter") || !strings.Contains(out, "gemini-cli") {
		t.Fatalf("expected navigator and focus pane items to be visible, got:\n%s", out)
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
