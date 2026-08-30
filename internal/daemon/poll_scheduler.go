package daemon

import (
	"encoding/json"
	"hash/fnv"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PollScheduler manages per-provider adaptive backoff to reduce CPU usage when data
// sources are idle. Each account gets its own backoff state: when consecutive polls
// detect no changes, the effective interval increases until a provider-specific cap.
type PollScheduler struct {
	mu           sync.Mutex
	states       map[string]*pollBackoffState
	baseInterval time.Duration
}

type pollBackoffState struct {
	lastPollAt          time.Time
	consecutiveNoChange int
	lastSnapshotHash    string
	hasLocalDetector    bool // true if provider implements ChangeDetector
}

// backoff tier thresholds and multipliers
var backoffTiers = []struct {
	minNoChange int
	multiplier  int
}{
	{0, 1},   // 0-2:  1x (normal)
	{3, 2},   // 3-5:  2x
	{6, 6},   // 6-10: 6x
	{11, 8},  // 11-20: 8x
	{21, 16}, // 21+:  16x
}

const (
	// HTTP polls never run faster than this, even when refresh_interval_seconds is lower.
	minIntervalHTTP = 30 * time.Second
	// HTTP providers double their interval on each unchanged poll until this cap.
	maxIntervalHTTP = 8 * time.Minute
	// Local providers (with ChangeDetector) use tiered backoff up to 16x base.
	maxMultiplierLocal = 16
)

func newPollScheduler(baseInterval time.Duration) *PollScheduler {
	return &PollScheduler{
		states:       make(map[string]*pollBackoffState),
		baseInterval: baseInterval,
	}
}

// ShouldPoll returns true if enough time has elapsed for this account's current
// backoff tier. If the provider implements ChangeDetector, mark it accordingly
// for the correct cap.
func (ps *PollScheduler) ShouldPoll(accountID string, hasLocalDetector bool) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	state, ok := ps.states[accountID]
	if !ok {
		ps.states[accountID] = &pollBackoffState{
			hasLocalDetector: hasLocalDetector,
		}
		return true // first poll always runs
	}
	state.hasLocalDetector = hasLocalDetector

	interval := ps.effectiveIntervalLocked(state)
	return time.Since(state.lastPollAt) >= interval
}

// RecordPoll records that a poll was executed. changed indicates whether the data
// actually differed from the previous poll.
func (ps *PollScheduler) RecordPoll(accountID string, changed bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	state, ok := ps.states[accountID]
	if !ok {
		state = &pollBackoffState{}
		ps.states[accountID] = state
	}

	state.lastPollAt = time.Now()
	if changed {
		state.consecutiveNoChange = 0
	} else {
		state.consecutiveNoChange++
	}
}

// SnapshotChanged compares a snapshot's metrics to the previous hash for this account.
// Returns true if the snapshot is different (or first time seen).
func (ps *PollScheduler) SnapshotChanged(accountID string, snap core.UsageSnapshot) bool {
	hash := hashSnapshotMetrics(snap)

	ps.mu.Lock()
	defer ps.mu.Unlock()

	state, ok := ps.states[accountID]
	if !ok {
		state = &pollBackoffState{}
		ps.states[accountID] = state
	}

	if state.lastSnapshotHash == "" || state.lastSnapshotHash != hash {
		state.lastSnapshotHash = hash
		return true
	}
	return false
}

func (ps *PollScheduler) effectiveIntervalLocked(state *pollBackoffState) time.Duration {
	if !state.hasLocalDetector {
		return httpPollInterval(ps.baseInterval, state.consecutiveNoChange)
	}

	multiplier := 1
	for _, tier := range backoffTiers {
		if state.consecutiveNoChange >= tier.minNoChange {
			multiplier = tier.multiplier
		}
	}
	if multiplier > maxMultiplierLocal {
		multiplier = maxMultiplierLocal
	}
	return ps.baseInterval * time.Duration(multiplier)
}

// HTTPBasePollInterval returns the minimum poll interval for HTTP providers
// given the configured dashboard refresh interval.
func HTTPBasePollInterval(refreshInterval time.Duration) time.Duration {
	return httpPollInterval(refreshInterval, 0)
}

// httpPollInterval doubles the base interval for each consecutive unchanged poll,
// floored at minIntervalHTTP and capped at maxIntervalHTTP.
func httpPollInterval(base time.Duration, consecutiveNoChange int) time.Duration {
	if base < minIntervalHTTP {
		base = minIntervalHTTP
	}
	interval := base
	for i := 0; i < consecutiveNoChange; i++ {
		if interval >= maxIntervalHTTP {
			return maxIntervalHTTP
		}
		next := interval * 2
		if next > maxIntervalHTTP {
			return maxIntervalHTTP
		}
		interval = next
	}
	return interval
}

func hashSnapshotMetrics(snap core.UsageSnapshot) string {
	// Non-cryptographic hash for lightweight diff comparison (not security-sensitive).
	h := fnv.New128a()
	h.Write([]byte(string(snap.Status)))
	if data, err := json.Marshal(snap.Metrics); err == nil {
		h.Write(data)
	}
	return string(h.Sum(nil))
}
