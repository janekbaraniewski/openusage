package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestMouseWheelDoesNotScrollTilesInMultiColumn(t *testing.T) {
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
	if got != 0 {
		t.Fatalf("tileOffset = %d, want 0", got)
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
