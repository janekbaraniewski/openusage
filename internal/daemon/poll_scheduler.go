package daemon

import (
	"encoding/json"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PollScheduler manages per-provider adaptive backoff to reduce CPU usage when data
// sources are idle. Each account gets its own backoff state: when consecutive polls
// detect no changes, the effective interval increases in tiers up to a configurable cap.
//
// Independently of the no-change tiers, a fetch that ends in StatusLimited or
// StatusError backs off exponentially (base * 2^failures, capped at
// maxFailureBackoff), and a provider-supplied NextPollNotBefore (Retry-After)
// is honoured up to maxNotBeforeHorizon.
type PollScheduler struct {
	mu           sync.Mutex
	states       map[string]*pollBackoffState
	baseInterval time.Duration
	now          func() time.Time
}

type pollBackoffState struct {
	lastPollAt          time.Time
	consecutiveNoChange int
	lastSnapshotHash    string
	hasLocalDetector    bool // true if provider implements ChangeDetector

	consecutiveFailures int       // consecutive Limited/Error outcomes
	notBefore           time.Time // no fetch before this instant
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

	// maxFailureBackoff caps the exponential backoff after Limited/Error
	// outcomes.
	maxFailureBackoff = 30 * time.Minute
	// maxNotBeforeHorizon caps how far a provider-supplied Retry-After can
	// push the next poll, so a bogus header cannot silence an account.
	maxNotBeforeHorizon = 2 * time.Hour
)

func newPollScheduler(baseInterval time.Duration) *PollScheduler {
	return &PollScheduler{
		states:       make(map[string]*pollBackoffState),
		baseInterval: baseInterval,
		now:          time.Now,
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

	now := ps.now()
	if now.Before(state.notBefore) {
		return false
	}
	interval := ps.effectiveIntervalLocked(state)
	return now.Sub(state.lastPollAt) >= interval
}

// RecordOutcome applies failure backoff and provider-requested delays from a
// freshly fetched snapshot. Error and throttled (429) outcomes back off
// exponentially (see isFailedFetch);
// any other status clears the failure streak. snap.NextPollNotBefore, when
// set, extends the hold-off (capped at maxNotBeforeHorizon).
func (ps *PollScheduler) RecordOutcome(accountID string, snap core.UsageSnapshot) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	state, ok := ps.states[accountID]
	if !ok {
		state = &pollBackoffState{}
		ps.states[accountID] = state
	}

	now := ps.now()
	state.notBefore = time.Time{}
	if isFailedFetch(snap) {
		state.consecutiveFailures++
		state.notBefore = now.Add(ps.failureBackoffLocked(state.consecutiveFailures))
	} else {
		state.consecutiveFailures = 0
	}

	if !snap.NextPollNotBefore.IsZero() {
		requested := snap.NextPollNotBefore
		if limit := now.Add(maxNotBeforeHorizon); requested.After(limit) {
			requested = limit
		}
		if requested.After(state.notBefore) {
			state.notBefore = requested
		}
	}
}

// isFailedFetch reports whether a snapshot is a failed or throttled request
// that warrants exponential backoff. StatusLimited alone is not enough:
// several providers (codex, cursor, copilot) also report it when a quota is
// exhausted, and backing off then would delay showing the quota reset by up
// to maxFailureBackoff. A limited snapshot counts only when it came from a
// throttled request: a Retry-After header or a "rate limited" message, which
// every 429 path sets.
func isFailedFetch(snap core.UsageSnapshot) bool {
	switch snap.Status {
	case core.StatusError:
		return true
	case core.StatusLimited:
		return !snap.NextPollNotBefore.IsZero() ||
			snap.Raw["retry_after"] != "" ||
			strings.Contains(strings.ToLower(snap.Message), "rate limited")
	default:
		return false
	}
}

func (ps *PollScheduler) failureBackoffLocked(failures int) time.Duration {
	backoff := ps.baseInterval
	for i := 0; i < failures; i++ {
		backoff *= 2
		if backoff >= maxFailureBackoff {
			return maxFailureBackoff
		}
	}
	return backoff
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

	state.lastPollAt = ps.now()
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
	// Non-cryptographic hash for lightweight diff comparison (not security-sensitive).
	h := fnv.New128a()
	h.Write([]byte(string(snap.Status)))
	if data, err := json.Marshal(snap.Metrics); err == nil {
		h.Write(data)
	}
	return string(h.Sum(nil))
}
