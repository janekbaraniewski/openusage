package active

import (
	"sort"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// SelectInput is everything the ranking needs. Candidates carry their own
// LastEventAt so the selector stays free of storage concerns and is easy to
// test.
type SelectInput struct {
	Candidates []Candidate
	// PinnedKey is an already-validated live pin, or empty.
	PinnedKey string
	// PriorityOrder ranks event-less providers. Empty means the detector's
	// DefaultPriorityOrder.
	PriorityOrder []string
}

// Select ranks candidates and returns the winner, the source that produced it
// ("pinned", "events", or "local"), and whether anything matched.
func Select(in SelectInput) (Candidate, string, bool) {
	if len(in.Candidates) == 0 {
		core.Tracef("[active] winner=none source=none candidates=0 with_events=0")
		return Candidate{}, "", false
	}

	if in.PinnedKey != "" {
		for _, c := range in.Candidates {
			if c.Key == in.PinnedKey {
				core.Tracef("[active] winner=%s source=pinned candidates=%d with_events=0", c.Key, len(in.Candidates))
				return c, "pinned", true
			}
		}
	}

	withEvents := make([]Candidate, 0, len(in.Candidates))
	for _, c := range in.Candidates {
		if c.LastEventAt != nil && !c.LastEventAt.IsZero() {
			withEvents = append(withEvents, c)
		}
	}
	if len(withEvents) > 0 {
		sort.SliceStable(withEvents, func(i, j int) bool {
			return withEvents[i].LastEventAt.After(*withEvents[j].LastEventAt)
		})
		core.Tracef("[active] winner=%s source=events candidates=%d with_events=%d",
			withEvents[0].Key, len(in.Candidates), len(withEvents))
		return withEvents[0], "events", true
	}

	order := in.PriorityOrder
	if len(order) == 0 {
		order = DefaultPriorityOrder
	}
	rank := make(map[string]int, len(order))
	for i, id := range order {
		rank[id] = i
	}
	best := -1
	bestRank := len(order) + 1
	for i, c := range in.Candidates {
		r, ok := rank[c.ProviderID]
		if !ok {
			r = len(order)
		}
		if r < bestRank {
			bestRank, best = r, i
		}
	}
	if best < 0 {
		core.Tracef("[active] winner=none source=none candidates=%d with_events=0", len(in.Candidates))
		return Candidate{}, "", false
	}
	core.Tracef("[active] winner=%s source=local candidates=%d with_events=0",
		in.Candidates[best].Key, len(in.Candidates))
	return in.Candidates[best], "local", true
}
