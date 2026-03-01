package tui

import (
	"strings"
	"testing"
)

func TestRenderTilesTabs_ShowsInactiveTabs(t *testing.T) {
	m := Model{
		width:         90,
		height:        28,
		dashboardView: dashboardViewTabs,
		sortedIDs:     []string{"openrouter", "gemini-cli", "codex-cli"},
		snapshots:     testSnapshots("openrouter", "gemini-cli", "codex-cli"),
	}

	out := m.renderTilesTabs(90, 20)
	if !strings.Contains(out, "openrouter") || !strings.Contains(out, "gemini-cli") {
		t.Fatalf("expected active and inactive tabs to be visible, got:\n%s", out)
	}
}

func TestRenderTilesTabs_ShowsHorizontalScrollBarWhenOverflowing(t *testing.T) {
	m := Model{
		width:         48,
		height:        22,
		dashboardView: dashboardViewTabs,
		sortedIDs: []string{
			"openrouter", "gemini-cli", "codex-cli", "cursor-ide", "claude-code",
		},
		snapshots: testSnapshots(
			"openrouter", "gemini-cli", "codex-cli", "cursor-ide", "claude-code",
		),
	}

	out := m.renderTilesTabs(48, 18)
	if !strings.Contains(out, "↔") || !strings.Contains(out, "▶") {
		t.Fatalf("expected horizontal scrollbar indicators for overflow, got:\n%s", out)
	}
}

func TestRenderTilesTabs_ShowsHorizontalPaneIndicatorWithoutTabOverflow(t *testing.T) {
	m := Model{
		width:         120,
		height:        22,
		dashboardView: dashboardViewTabs,
		sortedIDs:     []string{"openrouter", "gemini-cli", "codex-cli"},
		snapshots:     testSnapshots("openrouter", "gemini-cli", "codex-cli"),
	}

	out := m.renderTilesTabs(120, 18)
	if !strings.Contains(out, "↔") || !strings.Contains(out, "▶") {
		t.Fatalf("expected horizontal pane indicator even without tab overflow, got:\n%s", out)
	}
}
