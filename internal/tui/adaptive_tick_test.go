package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestNextTickInterval_Loading(t *testing.T) {
	m := Model{hasData: false, refreshing: false}
	if got := m.nextTickInterval(); got != tickFast {
		t.Fatalf("no data: got %v, want %v", got, tickFast)
	}

	m = Model{hasData: true, refreshing: true}
	if got := m.nextTickInterval(); got != tickFast {
		t.Fatalf("refreshing: got %v, want %v", got, tickFast)
	}
}

func TestNextTickInterval_RecentInteraction(t *testing.T) {
	m := Model{
		hasData:         true,
		lastInteraction: time.Now(),
	}
	if got := m.nextTickInterval(); got != tickNormal {
		t.Fatalf("recent interaction: got %v, want %v", got, tickNormal)
	}
}

func TestNextTickInterval_RecentData(t *testing.T) {
	m := Model{
		hasData:         true,
		lastInteraction: time.Now().Add(-idleAfterInteraction - time.Second),
		lastDataUpdate:  time.Now(),
	}
	if got := m.nextTickInterval(); got != tickSlow {
		t.Fatalf("recent data update: got %v, want %v", got, tickSlow)
	}
}

func TestNextTickInterval_FullyIdle(t *testing.T) {
	m := Model{
		hasData:         true,
		lastInteraction: time.Now().Add(-time.Minute),
		lastDataUpdate:  time.Now().Add(-time.Minute),
	}
	if got := m.nextTickInterval(); got != 0 {
		t.Fatalf("fully idle: got %v, want 0", got)
	}
}

func TestNextTickInterval_NoTimestampsIdle(t *testing.T) {
	// hasData true, but no interaction/data timestamps → fully idle.
	m := Model{hasData: true}
	if got := m.nextTickInterval(); got != 0 {
		t.Fatalf("no timestamps: got %v, want 0", got)
	}
}

func TestRestartTickIfNeeded_WhenPaused(t *testing.T) {
	m := Model{tickRunning: false}
	cmd := m.restartTickIfNeeded()
	if cmd == nil {
		t.Fatal("expected non-nil cmd when tick was paused")
	}
	if !m.tickRunning {
		t.Fatal("tickRunning should be true after restart")
	}
}

func TestRestartTickIfNeeded_WhenRunning(t *testing.T) {
	m := Model{tickRunning: true}
	cmd := m.restartTickIfNeeded()
	if cmd != nil {
		t.Fatal("expected nil cmd when tick already running")
	}
}

func TestUpdateTickMsg_TransitionsToIdle(t *testing.T) {
	m := Model{
		hasData:         true,
		tickRunning:     true,
		lastInteraction: time.Now().Add(-time.Minute),
		lastDataUpdate:  time.Now().Add(-time.Minute),
		snapshots:       make(map[string]core.UsageSnapshot),
	}

	updated, cmd := m.Update(tickMsg(time.Now()))
	model := updated.(Model)

	if model.tickRunning {
		t.Fatal("tickRunning should be false when fully idle")
	}
	if cmd != nil {
		t.Fatal("expected nil cmd when transitioning to idle")
	}
	if model.animFrame != 1 {
		t.Fatalf("animFrame = %d, want 1", model.animFrame)
	}
}

func TestUpdateTickMsg_ContinuesFastWhenLoading(t *testing.T) {
	m := Model{
		hasData:     false,
		tickRunning: true,
		snapshots:   make(map[string]core.UsageSnapshot),
	}

	_, cmd := m.Update(tickMsg(time.Now()))
	if cmd == nil {
		t.Fatal("expected non-nil cmd for loading state")
	}
}

func TestUpdateKeyMsg_RestartsTickWhenPaused(t *testing.T) {
	m := Model{
		hasData:         true,
		tickRunning:     false,
		snapshots:       make(map[string]core.UsageSnapshot),
		sortedIDs:       []string{"test"},
		providerEnabled: map[string]bool{"test": true},
	}

	msg := tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updated, cmd := m.Update(msg)
	model := updated.(Model)

	if !model.tickRunning {
		t.Fatal("tickRunning should be true after key press")
	}
	if model.lastInteraction.IsZero() {
		t.Fatal("lastInteraction should be set after key press")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd to restart tick")
	}
}

func TestUpdateSnapshotsMsg_RecordsDataUpdate(t *testing.T) {
	m := Model{
		hasData:          true,
		tickRunning:      false,
		snapshots:        make(map[string]core.UsageSnapshot),
		timeWindow:       core.TimeWindow30d,
		providerEnabled:  map[string]bool{},
		accountProviders: map[string]string{},
	}

	snaps := map[string]core.UsageSnapshot{
		"test": {
			ProviderID: "openai",
			Status:     core.StatusOK,
			Timestamp:  time.Now(),
		},
	}

	updated, cmd := m.Update(SnapshotsMsg{
		Snapshots:  snaps,
		TimeWindow: core.TimeWindow30d,
	})
	model := updated.(Model)

	if model.lastDataUpdate.IsZero() {
		t.Fatal("lastDataUpdate should be set after snapshot message")
	}
	if !model.tickRunning {
		t.Fatal("tickRunning should be true after snapshot restarts tick")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd to restart tick")
	}
}
