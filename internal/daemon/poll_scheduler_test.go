package daemon

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestPollScheduler_ShouldPoll_FirstPollAlwaysRuns(t *testing.T) {
	ps := newPollScheduler(30 * time.Second)
	if !ps.ShouldPoll("acct1", false) {
		t.Error("first poll should always run")
	}
}

func TestPollScheduler_ShouldPoll_RespectsBaseInterval(t *testing.T) {
	ps := newPollScheduler(30 * time.Second)

	// First poll runs and records.
	ps.ShouldPoll("acct1", false)
	ps.RecordPoll("acct1", true) // changed

	// Immediately after: should not poll again.
	if ps.ShouldPoll("acct1", false) {
		t.Error("should not poll immediately after previous poll")
	}
}

func TestPollScheduler_BackoffTiers(t *testing.T) {
	ps := newPollScheduler(30 * time.Second)

	ps.ShouldPoll("acct1", false) // init

	tests := []struct {
		noChangeCount int
		wantInterval  time.Duration
	}{
		{0, 30 * time.Second},
		{2, 30 * time.Second},
		{3, 60 * time.Second},
		{5, 60 * time.Second},
		{6, 120 * time.Second}, // but capped at 4x for HTTP
		{11, 120 * time.Second},
		{21, 120 * time.Second},
	}

	for _, tt := range tests {
		ps.mu.Lock()
		ps.states["acct1"].consecutiveNoChange = tt.noChangeCount
		got := ps.effectiveIntervalLocked(ps.states["acct1"])
		ps.mu.Unlock()
		if got != tt.wantInterval {
			t.Errorf("noChange=%d: got %s, want %s", tt.noChangeCount, got, tt.wantInterval)
		}
	}
}

func TestPollScheduler_BackoffTiers_LocalProvider(t *testing.T) {
	ps := newPollScheduler(30 * time.Second)

	ps.ShouldPoll("acct1", true) // hasLocalDetector=true

	tests := []struct {
		noChangeCount int
		wantInterval  time.Duration
	}{
		{0, 30 * time.Second},
		{3, 60 * time.Second},
		{6, 120 * time.Second},
		{11, 240 * time.Second},
		{21, 480 * time.Second}, // 16x cap for local providers
	}

	for _, tt := range tests {
		ps.mu.Lock()
		ps.states["acct1"].consecutiveNoChange = tt.noChangeCount
		got := ps.effectiveIntervalLocked(ps.states["acct1"])
		ps.mu.Unlock()
		if got != tt.wantInterval {
			t.Errorf("noChange=%d: got %s, want %s", tt.noChangeCount, got, tt.wantInterval)
		}
	}
}

func TestPollScheduler_ResetOnChange(t *testing.T) {
	ps := newPollScheduler(30 * time.Second)

	ps.ShouldPoll("acct1", false)

	// Simulate 10 no-change polls.
	for i := 0; i < 10; i++ {
		ps.RecordPoll("acct1", false)
	}

	ps.mu.Lock()
	noChange := ps.states["acct1"].consecutiveNoChange
	ps.mu.Unlock()
	if noChange != 10 {
		t.Fatalf("expected 10 consecutive no-change, got %d", noChange)
	}

	// A changed poll resets to 0.
	ps.RecordPoll("acct1", true)

	ps.mu.Lock()
	noChange = ps.states["acct1"].consecutiveNoChange
	ps.mu.Unlock()
	if noChange != 0 {
		t.Errorf("expected 0 after change, got %d", noChange)
	}
}

func TestPollScheduler_SnapshotChanged(t *testing.T) {
	ps := newPollScheduler(30 * time.Second)

	snap1 := core.UsageSnapshot{
		Status: core.StatusOK,
		Metrics: map[string]core.Metric{
			"requests": {Used: ptr(100.0)},
		},
	}

	// First time is always "changed".
	if !ps.SnapshotChanged("acct1", snap1) {
		t.Error("first snapshot should be reported as changed")
	}

	// Same snapshot: not changed.
	if ps.SnapshotChanged("acct1", snap1) {
		t.Error("identical snapshot should not be reported as changed")
	}

	// Different snapshot: changed.
	snap2 := core.UsageSnapshot{
		Status: core.StatusOK,
		Metrics: map[string]core.Metric{
			"requests": {Used: ptr(200.0)},
		},
	}
	if !ps.SnapshotChanged("acct1", snap2) {
		t.Error("different snapshot should be reported as changed")
	}
}

func TestPollScheduler_UnknownAccount(t *testing.T) {
	ps := newPollScheduler(30 * time.Second)
	ps.mu.Lock()
	got := ps.baseInterval
	if state := ps.states["nonexistent"]; state != nil {
		got = ps.effectiveIntervalLocked(state)
	}
	ps.mu.Unlock()
	if got != 30*time.Second {
		t.Errorf("unknown account should return base interval, got %s", got)
	}
}

func ptr(f float64) *float64 { return &f }
