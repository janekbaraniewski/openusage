package tui

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// A repeated, unchanged daemon status must not wake a paused tick chain.
func TestDaemonStatusUnchangedDoesNotRestartTick(t *testing.T) {
	m := Model{
		hasData:   true,
		snapshots: make(map[string]core.UsageSnapshot),
		daemon:    daemonState{status: DaemonRunning},
	}
	updated, cmd := m.Update(DaemonStatusMsg{Status: DaemonRunning})
	if cmd != nil {
		t.Fatal("unchanged daemon status restarted the tick")
	}
	if updated.(Model).tickRunning {
		t.Fatal("tickRunning set for an unchanged daemon status")
	}

	_, cmd = m.Update(DaemonStatusMsg{Status: DaemonRunning, Warning: "old helper"})
	if cmd == nil {
		t.Fatal("changed daemon status did not restart the tick")
	}
}

func TestLoadingTickSlowsAfterFastWindow(t *testing.T) {
	m := Model{hasData: false, loadingSince: time.Now()}
	if got := m.nextTickInterval(); got != tickFast {
		t.Fatalf("fresh splash: got %v, want %v", got, tickFast)
	}
	m.loadingSince = time.Now().Add(-loadingFastFor - time.Second)
	if got := m.nextTickInterval(); got != tickNormal {
		t.Fatalf("long splash: got %v, want %v", got, tickNormal)
	}
}

func TestStuckRefreshClearsAfterTimeout(t *testing.T) {
	now := time.Now()
	m := Model{hasData: true, refreshing: true}

	m.trackLoading(now)
	if m.loadingSince != now || !m.refreshing {
		t.Fatalf("refresh start not recorded: since=%v refreshing=%v", m.loadingSince, m.refreshing)
	}
	m.trackLoading(now.Add(refreshTimeout - time.Second))
	if !m.refreshing {
		t.Fatal("refresh cleared before the timeout")
	}
	m.trackLoading(now.Add(refreshTimeout))
	if m.refreshing || !m.loadingSince.IsZero() {
		t.Fatal("stuck refresh not cleared after the timeout")
	}

	// The splash (no data yet) has nothing to clear; it only slows down.
	s := Model{hasData: false}
	s.trackLoading(now)
	s.trackLoading(now.Add(2 * refreshTimeout))
	if s.loadingSince != now {
		t.Fatal("splash loading start should persist until data arrives")
	}
}
