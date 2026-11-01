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

func newTestScheduler(base time.Duration) (*PollScheduler, *time.Time) {
	ps := newPollScheduler(base)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ps.now = func() time.Time { return now }
	return ps, &now
}

func TestPollScheduler_RespectsRetryAfter(t *testing.T) {
	ps, now := newTestScheduler(30 * time.Second)
	ps.ShouldPoll("acct1", false)
	ps.RecordPoll("acct1", true)

	// A 200 response that still carries Retry-After (e.g. a soft limit) must
	// hold off polling until the requested time even though Status is OK.
	snap := core.UsageSnapshot{Status: core.StatusOK, NextPollNotBefore: now.Add(10 * time.Minute)}
	ps.RecordOutcome("acct1", snap)

	*now = now.Add(9 * time.Minute)
	if ps.ShouldPoll("acct1", false) {
		t.Fatal("polled before Retry-After elapsed")
	}
	*now = now.Add(61 * time.Second)
	if !ps.ShouldPoll("acct1", false) {
		t.Fatal("did not poll after Retry-After elapsed")
	}
}

func TestPollScheduler_RetryAfterCapped(t *testing.T) {
	ps, now := newTestScheduler(30 * time.Second)
	ps.ShouldPoll("acct1", false)
	ps.RecordPoll("acct1", true)
	ps.RecordOutcome("acct1", core.UsageSnapshot{Status: core.StatusLimited, NextPollNotBefore: now.Add(48 * time.Hour)})

	*now = now.Add(maxNotBeforeHorizon + time.Second)
	if !ps.ShouldPoll("acct1", false) {
		t.Fatal("Retry-After beyond the horizon was not capped")
	}
}

func TestPollScheduler_ExponentialBackoffOnLimitedAndError(t *testing.T) {
	ps, now := newTestScheduler(30 * time.Second)
	ps.ShouldPoll("acct1", false)

	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for i, wait := range want {
		status := core.StatusLimited
		if i%2 == 1 {
			status = core.StatusError
		}
		ps.RecordPoll("acct1", false)
		ps.RecordOutcome("acct1", core.UsageSnapshot{Status: status, Message: "rate limited (HTTP 429)"})

		*now = now.Add(wait - time.Second)
		if ps.ShouldPoll("acct1", false) {
			t.Fatalf("failure %d: polled before %s backoff", i+1, wait)
		}
		*now = now.Add(time.Second)
		if !ps.ShouldPoll("acct1", false) {
			t.Fatalf("failure %d: not polled after %s backoff", i+1, wait)
		}
	}

	// Recovery clears the streak: next poll at the base interval.
	ps.RecordPoll("acct1", true)
	ps.RecordOutcome("acct1", core.UsageSnapshot{Status: core.StatusOK})
	*now = now.Add(30 * time.Second)
	if !ps.ShouldPoll("acct1", false) {
		t.Fatal("did not return to base interval after recovery")
	}
}

func TestPollScheduler_QuotaExhaustedDoesNotBackOff(t *testing.T) {
	ps, now := newTestScheduler(30 * time.Second)
	ps.ShouldPoll("acct1", false)

	// A provider at 100% of its quota reports StatusLimited without any
	// throttling signal. Polling must continue at the base interval so the
	// quota reset shows up promptly.
	for i := 0; i < 4; i++ {
		ps.RecordPoll("acct1", true)
		ps.RecordOutcome("acct1", core.UsageSnapshot{Status: core.StatusLimited, Message: "5h limit reached"})
		*now = now.Add(30 * time.Second)
		if !ps.ShouldPoll("acct1", false) {
			t.Fatalf("poll %d: quota-exhausted snapshot triggered failure backoff", i+1)
		}
	}
}

func TestIsFailedFetch(t *testing.T) {
	cases := []struct {
		name string
		snap core.UsageSnapshot
		want bool
	}{
		{"error", core.UsageSnapshot{Status: core.StatusError}, true},
		{"429 message", core.UsageSnapshot{Status: core.StatusLimited, Message: "Rate limited (HTTP 429)"}, true},
		{"retry-after raw", core.UsageSnapshot{Status: core.StatusLimited, Raw: map[string]string{"retry_after": "30"}}, true},
		{"retry-after parsed", core.UsageSnapshot{Status: core.StatusLimited, NextPollNotBefore: time.Now().Add(time.Minute)}, true},
		{"quota exhausted", core.UsageSnapshot{Status: core.StatusLimited}, false},
		{"near limit", core.UsageSnapshot{Status: core.StatusNearLimit}, false},
		{"ok", core.UsageSnapshot{Status: core.StatusOK}, false},
	}
	for _, tc := range cases {
		if got := isFailedFetch(tc.snap); got != tc.want {
			t.Errorf("%s: isFailedFetch = %v, want %v", tc.name, got, tc.want)
		}
	}
}
