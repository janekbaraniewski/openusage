package active

import (
	"fmt"
	"sort"
	"strings"
)

// Explain renders a human-readable account of why Select would pick what it
// picks. It runs the same tiering as Select but reports the reasoning instead
// of the answer.
func Explain(in SelectInput) string {
	var b strings.Builder

	if len(in.Candidates) == 0 {
		return "no candidates: no providers are configured or detected\n"
	}
	fmt.Fprintf(&b, "%d candidate(s)\n", len(in.Candidates))

	if in.PinnedKey != "" {
		for _, c := range in.Candidates {
			if c.Key == in.PinnedKey {
				fmt.Fprintf(&b, "pinned: %s wins outright (pin is live)\n", in.PinnedKey)
				return b.String()
			}
		}
		fmt.Fprintf(&b, "pinned: %s is not among candidates; ignoring the pin\n", in.PinnedKey)
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
		fmt.Fprintf(&b, "tier 1 (events): %d of %d candidates have telemetry events\n",
			len(withEvents), len(in.Candidates))
		for _, c := range withEvents {
			fmt.Fprintf(&b, "  %-24s last event %s\n", c.Key, c.LastEventAt.Format("2006-01-02 15:04:05Z"))
		}
		fmt.Fprintf(&b, "winner: %s (most recent event)\n", withEvents[0].Key)
		return b.String()
	}

	order := in.PriorityOrder
	if len(order) == 0 {
		order = DefaultPriorityOrder
	}
	b.WriteString("tier 2: no provider has events, so event-less providers are eligible\n")
	fmt.Fprintf(&b, "priority order: %s\n", strings.Join(order, ", "))
	winner, _, found := Select(in)
	if !found {
		b.WriteString("winner: none\n")
		return b.String()
	}
	fmt.Fprintf(&b, "winner: %s (first match in priority order)\n", winner.Key)
	return b.String()
}
