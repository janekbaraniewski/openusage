package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func tallStackedSnapshots(n, metricsPerProvider int) (map[string]core.UsageSnapshot, []string) {
	snaps := make(map[string]core.UsageSnapshot, n)
	ids := make([]string, 0, n)
	for p := 0; p < n; p++ {
		id := fmt.Sprintf("acct-%d", p)
		metrics := make(map[string]core.Metric, metricsPerProvider)
		for i := 0; i < metricsPerProvider; i++ {
			metrics[fmt.Sprintf("metric_%02d", i)] = core.Metric{
				Used: float64Ptr(float64(i + 1)),
				Unit: "count",
			}
		}
		snaps[id] = core.UsageSnapshot{
			AccountID:  id,
			ProviderID: "openai",
			Status:     core.StatusOK,
			Metrics:    metrics,
		}
		ids = append(ids, id)
	}
	return snaps, ids
}

func newStackedScrollModel(t *testing.T) Model {
	t.Helper()
	snaps, ids := tallStackedSnapshots(9, 30)
	m := NewModel(0.2, 0.1, config.DashboardConfig{View: config.DashboardViewStacked}, nil, core.TimeWindow30d)
	m.width, m.height = 120, 40
	m.hasData = true
	m.snapshots = snaps
	m.ensureSnapshotProvidersKnown()
	m.rebuildSortedIDs()
	if len(m.filteredIDs()) != len(ids) {
		t.Fatalf("filteredIDs = %v, want %d providers", m.filteredIDs(), len(ids))
	}
	if !m.shouldUsePanelScroll() {
		t.Fatal("expected stacked view to use panel scroll")
	}
	if maxOffset, ok := m.panelScrollMaxOffset(); !ok || maxOffset <= 0 {
		t.Fatalf("panelScrollMaxOffset = %d, %v; want scrollable content", maxOffset, ok)
	}
	return m
}

func sendKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "pgup":
		msg = tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		msg = tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		msg = tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		msg = tea.KeyMsg{Type: tea.KeyEnd}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func sendWheel(t *testing.T, m Model, button tea.MouseButton) Model {
	t.Helper()
	updated, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: button})
	return updated.(Model)
}

func stackedFrame(m Model) string {
	return m.renderTilesSingleColumn(m.width, dashboardContentHeight(m.height, m.renderHeader(m.width), m.renderFooter(m.width)))
}

func scrollToBottom(t *testing.T, m Model) Model {
	t.Helper()
	m = sendKey(t, m, "end")
	maxOffset, _ := m.panelScrollMaxOffset()
	if m.tileOffset != maxOffset {
		t.Fatalf("after End tileOffset = %d, want max %d", m.tileOffset, maxOffset)
	}
	return m
}

func TestStackedScroll_EndThenUpInputsScrollBackUp(t *testing.T) {
	for _, tc := range []struct {
		name string
		up   func(Model) Model
	}{
		{"pgup", func(m Model) Model { return sendKey(t, m, "pgup") }},
		{"ctrl+u", func(m Model) Model {
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
			return updated.(Model)
		}},
		{"wheel-up", func(m Model) Model { return sendWheel(t, m, tea.MouseButtonWheelUp) }},
		{"home", func(m Model) Model { return sendKey(t, m, "home") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := scrollToBottom(t, newStackedScrollModel(t))
			before := m.tileOffset
			frameBefore := stackedFrame(m)

			m = tc.up(m)
			if m.tileOffset >= before {
				t.Fatalf("tileOffset = %d after %s from bottom (%d), want decrease", m.tileOffset, tc.name, before)
			}
			if stackedFrame(m) == frameBefore {
				t.Fatalf("visible frame unchanged after %s from bottom", tc.name)
			}
		})
	}
}

func TestStackedScroll_OvershootingDownDoesNotTrapScrollUp(t *testing.T) {
	m := newStackedScrollModel(t)
	maxOffset, _ := m.panelScrollMaxOffset()

	// Keep pushing down well past the bottom: before the fix every extra
	// press grew tileOffset invisibly and had to be undone by scroll-up.
	extra := 0
	for i := 0; i < 500 && extra < 10; i++ {
		m = sendKey(t, m, "pgdown")
		m = sendWheel(t, m, tea.MouseButtonWheelDown)
		if m.tileOffset >= maxOffset {
			extra++
		}
	}
	if m.tileOffset != maxOffset {
		t.Fatalf("tileOffset = %d after overshooting down, want clamped to %d", m.tileOffset, maxOffset)
	}

	frameBottom := stackedFrame(m)
	m = sendWheel(t, m, tea.MouseButtonWheelUp)
	if m.tileOffset >= maxOffset {
		t.Fatalf("wheel-up after overshoot: tileOffset = %d, want < %d", m.tileOffset, maxOffset)
	}
	if stackedFrame(m) == frameBottom {
		t.Fatal("wheel-up after overshoot did not move the view")
	}
}

func TestStackedScroll_UpWorksAfterSnapshotRefresh(t *testing.T) {
	m := scrollToBottom(t, newStackedScrollModel(t))
	before := m.tileOffset

	snaps, _ := tallStackedSnapshots(9, 30)
	updated, _ := m.Update(SnapshotsMsg{Snapshots: snaps, TimeWindow: core.TimeWindow30d})
	m = updated.(Model)
	if m.tileOffset != before {
		t.Fatalf("refresh with identical content changed tileOffset %d -> %d", before, m.tileOffset)
	}

	m = sendKey(t, m, "pgup")
	if m.tileOffset >= before {
		t.Fatalf("pgup after refresh: tileOffset = %d, want < %d", m.tileOffset, before)
	}
	m = scrollToBottom(t, m)
	m = sendWheel(t, m, tea.MouseButtonWheelUp)
	if m.tileOffset >= before {
		t.Fatalf("wheel-up after refresh: tileOffset = %d, want < %d", m.tileOffset, before)
	}
}

func TestStackedScroll_RefreshShrinkingContentReclampsOffset(t *testing.T) {
	m := scrollToBottom(t, newStackedScrollModel(t))

	smaller, _ := tallStackedSnapshots(9, 5)
	updated, _ := m.Update(SnapshotsMsg{Snapshots: smaller, TimeWindow: core.TimeWindow30d})
	m = updated.(Model)

	maxOffset, _ := m.panelScrollMaxOffset()
	if m.tileOffset > maxOffset {
		t.Fatalf("tileOffset = %d exceeds new max %d after shrinking refresh", m.tileOffset, maxOffset)
	}
}

func TestStackedScroll_AllInputsWorkInBothDirections(t *testing.T) {
	m := newStackedScrollModel(t)

	// Selection keys move the cursor both ways.
	for _, pair := range [][2]string{{"j", "k"}, {"down", "up"}} {
		start := m.cursor
		m = sendKey(t, m, pair[0])
		if m.cursor != start+1 {
			t.Fatalf("%s: cursor = %d, want %d", pair[0], m.cursor, start+1)
		}
		m = sendKey(t, m, pair[1])
		if m.cursor != start {
			t.Fatalf("%s: cursor = %d, want %d", pair[1], m.cursor, start)
		}
	}

	// From the last provider at the very bottom, k still moves selection up.
	for i := 0; i < len(m.sortedIDs); i++ {
		m = sendKey(t, m, "j")
	}
	m = scrollToBottom(t, m)
	last := m.cursor
	m = sendKey(t, m, "k")
	if m.cursor != last-1 {
		t.Fatalf("k at bottom: cursor = %d, want %d", m.cursor, last-1)
	}
	m = sendKey(t, m, "home")

	// Offset scrolling inputs move both ways.
	for _, pair := range []struct {
		name     string
		down, up func(Model) Model
	}{
		{"pgdown/pgup", func(m Model) Model { return sendKey(t, m, "pgdown") }, func(m Model) Model { return sendKey(t, m, "pgup") }},
		{"wheel", func(m Model) Model { return sendWheel(t, m, tea.MouseButtonWheelDown) }, func(m Model) Model { return sendWheel(t, m, tea.MouseButtonWheelUp) }},
		{"end/home", func(m Model) Model { return sendKey(t, m, "end") }, func(m Model) Model { return sendKey(t, m, "home") }},
	} {
		start := m.tileOffset
		m = pair.down(m)
		if m.tileOffset <= start {
			t.Fatalf("%s down: tileOffset = %d, want > %d", pair.name, m.tileOffset, start)
		}
		m = pair.up(m)
		if m.tileOffset != start {
			t.Fatalf("%s up: tileOffset = %d, want %d", pair.name, m.tileOffset, start)
		}
	}
}
