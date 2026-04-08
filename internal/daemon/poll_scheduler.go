package daemon

import (
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PollScheduler manages per-provider adaptive backoff to reduce CPU usage when data
// sources are idle. Each account gets its own backoff state: when consecutive polls
// detect no changes, the effective interval increases in tiers up to a configurable cap.
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
	{6, 4},   // 6-10: 4x
	{11, 8},  // 11-20: 8x
	{21, 16}, // 21+:  16x
}

const (
	// HTTP-only providers cap at 4x (they can't do cheap local change detection).
	maxMultiplierHTTP = 4
	// Local providers (with ChangeDetector) can back off further since stat() is cheap.
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

// EffectiveInterval returns the current backoff interval for an account.
func (ps *PollScheduler) EffectiveInterval(accountID string) time.Duration {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	state, ok := ps.states[accountID]
	if !ok {
		return ps.baseInterval
	}
	return ps.effectiveIntervalLocked(state)
}

func (ps *PollScheduler) effectiveIntervalLocked(state *pollBackoffState) time.Duration {
	multiplier := 1
	for _, tier := range backoffTiers {
		if state.consecutiveNoChange >= tier.minNoChange {
			multiplier = tier.multiplier
		}
	}

	maxMult := maxMultiplierHTTP
	if state.hasLocalDetector {
		maxMult = maxMultiplierLocal
	}
	if multiplier > maxMult {
		multiplier = maxMult
	}

	return ps.baseInterval * time.Duration(multiplier)
}

func hashSnapshotMetrics(snap core.UsageSnapshot) string {
	// Hash status + metrics keys/values for a lightweight diff.
	h := sha256.New()
	h.Write([]byte(string(snap.Status)))
	if data, err := json.Marshal(snap.Metrics); err == nil {
		h.Write(data)
	}
	return string(h.Sum(nil))
}
