package daemon

import (
	"testing"
	"time"
)

// An unchanged daemon state must not be re-sent: every send is a tea message
// and a re-render in the TUI.
func TestStateEmitterSkipsUnchangedState(t *testing.T) {
	rt := NewViewRuntime(nil, "", false)
	rt.setState(DaemonState{Status: DaemonStatusRunning})

	var got []DaemonState
	emit := newStateEmitter(rt, func(s DaemonState) { got = append(got, s) })

	emit()
	emit()
	emit()
	if len(got) != 1 {
		t.Fatalf("unchanged state emitted %d times, want 1", len(got))
	}

	rt.setState(DaemonState{Status: DaemonStatusRunning, Warning: "old helper"})
	emit()
	emit()
	if len(got) != 2 || got[1].Warning != "old helper" {
		t.Fatalf("changed state not emitted exactly once: %+v", got)
	}
}

func TestStateEmitterNilHandler(t *testing.T) {
	emit := newStateEmitter(NewViewRuntime(nil, "", false), nil)
	emit() // must not panic
}

func TestBroadcastIntervalFollowsRefreshInterval(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		0:                      30 * time.Second,
		-time.Second:           30 * time.Second,
		200 * time.Millisecond: time.Second,
		30 * time.Second:       30 * time.Second,
		2 * time.Minute:        2 * time.Minute,
	}
	for in, want := range cases {
		if got := broadcastInterval(in); got != want {
			t.Errorf("broadcastInterval(%v) = %v, want %v", in, got, want)
		}
	}
}
