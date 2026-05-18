package amp

// dedupAndMerge folds a slice of ampEvent values down to one entry per
// message id, taking the per-field max of token counts and the max credit
// cost across all observations of that id. Events without a message id
// (e.g. malformed thread rows where we couldn't recover an id) pass through
// verbatim — they cannot be deduplicated.
//
// The expected dedup keys are message IDs because the same message can
// appear in multiple thread files (e.g. when a thread is forked or
// re-saved) and we want the strongest single observation per message.
//
// Returns a new slice; the input is not mutated. Caller is responsible for
// sorting the result if order matters.
func dedupAndMerge(events []ampEvent) []ampEvent {
	if len(events) == 0 {
		return nil
	}

	byID := make(map[string]ampEvent, len(events))
	// keyOrder preserves first-seen ordering of dedup keys so the returned
	// slice has a deterministic shape independent of map iteration.
	keyOrder := make([]string, 0, len(events))
	var passthrough []ampEvent

	for _, evt := range events {
		if evt.MessageID == "" {
			passthrough = append(passthrough, evt)
			continue
		}
		existing, ok := byID[evt.MessageID]
		if !ok {
			byID[evt.MessageID] = evt
			keyOrder = append(keyOrder, evt.MessageID)
			continue
		}
		byID[evt.MessageID] = mergeEvents(existing, evt)
	}

	out := make([]ampEvent, 0, len(keyOrder)+len(passthrough))
	for _, id := range keyOrder {
		out = append(out, byID[id])
	}
	out = append(out, passthrough...)
	return out
}

// mergeEvents combines two observations of the same message id. The
// resulting event carries:
//   - per-field max of token counts
//   - max credit cost
//   - the earlier non-zero timestamp (so we attribute the message to its
//     first observation rather than a later refresh)
//   - the most informative model and thread id (prefer non-empty)
//   - source = "merged" when either side has been touched
func mergeEvents(a, b ampEvent) ampEvent {
	out := a

	out.Tokens = ampTokens{
		Input:      maxInt64(a.Tokens.Input, b.Tokens.Input),
		Output:     maxInt64(a.Tokens.Output, b.Tokens.Output),
		CacheRead:  maxInt64(a.Tokens.CacheRead, b.Tokens.CacheRead),
		CacheWrite: maxInt64(a.Tokens.CacheWrite, b.Tokens.CacheWrite),
	}

	if b.CreditCost > a.CreditCost {
		out.CreditCost = b.CreditCost
	}

	if a.Timestamp.IsZero() && !b.Timestamp.IsZero() {
		out.Timestamp = b.Timestamp
	} else if !a.Timestamp.IsZero() && !b.Timestamp.IsZero() && b.Timestamp.Before(a.Timestamp) {
		out.Timestamp = b.Timestamp
	}

	if out.Model == "" && b.Model != "" {
		out.Model = b.Model
	}
	if out.ThreadID == "" && b.ThreadID != "" {
		out.ThreadID = b.ThreadID
	}
	if out.SourcePath == "" && b.SourcePath != "" {
		out.SourcePath = b.SourcePath
	}

	out.Source = "merged"
	return out
}
