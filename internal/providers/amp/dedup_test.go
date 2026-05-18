package amp

import (
	"testing"
	"time"
)

func TestDedupAndMergeSameMessageMultipleFiles(t *testing.T) {
	tsA := time.Date(2026, 5, 10, 9, 0, 5, 0, time.UTC)
	tsB := time.Date(2026, 5, 10, 9, 0, 5, 0, time.UTC)
	events := []ampEvent{
		{
			MessageID:  "msg-asst-1",
			Model:      "sonnet-4.5",
			Timestamp:  tsA,
			Tokens:     ampTokens{Input: 1000, Output: 200, CacheRead: 500, CacheWrite: 50},
			CreditCost: 0.020,
			Source:     "thread",
			ThreadID:   "thread-A",
			SourcePath: "/data/threads/A.json",
		},
		{
			MessageID:  "msg-asst-1",
			Model:      "sonnet-4.5",
			Timestamp:  tsB,
			Tokens:     ampTokens{Input: 1200, Output: 150, CacheRead: 800, CacheWrite: 100},
			CreditCost: 0.025,
			Source:     "thread",
			ThreadID:   "thread-B",
			SourcePath: "/data/threads/B.json",
		},
	}

	out := dedupAndMerge(events)
	if len(out) != 1 {
		t.Fatalf("expected single deduped event, got %d", len(out))
	}
	got := out[0]
	// Per-field max merge:
	if got.Tokens.Input != 1200 {
		t.Errorf("Input: expected max 1200, got %d", got.Tokens.Input)
	}
	if got.Tokens.Output != 200 {
		t.Errorf("Output: expected max 200, got %d", got.Tokens.Output)
	}
	if got.Tokens.CacheRead != 800 {
		t.Errorf("CacheRead: expected max 800, got %d", got.Tokens.CacheRead)
	}
	if got.Tokens.CacheWrite != 100 {
		t.Errorf("CacheWrite: expected max 100, got %d", got.Tokens.CacheWrite)
	}
	if got.CreditCost != 0.025 {
		t.Errorf("CreditCost: expected max 0.025, got %v", got.CreditCost)
	}
	if got.Source != "merged" {
		t.Errorf("expected Source 'merged', got %q", got.Source)
	}
}

func TestDedupAndMergeKeepsUnkeyedEvents(t *testing.T) {
	events := []ampEvent{
		{MessageID: "", Tokens: ampTokens{Input: 10}, Source: "thread"},
		{MessageID: "", Tokens: ampTokens{Input: 20}, Source: "thread"},
		{MessageID: "id-1", Tokens: ampTokens{Input: 5}, Source: "thread"},
	}
	out := dedupAndMerge(events)
	if len(out) != 3 {
		t.Fatalf("expected 3 entries (2 unkeyed + 1 keyed), got %d", len(out))
	}
}

func TestDedupAndMergePicksEarlierNonZeroTimestamp(t *testing.T) {
	tsEarlier := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	tsLater := time.Date(2026, 5, 10, 9, 1, 0, 0, time.UTC)

	events := []ampEvent{
		{MessageID: "id", Timestamp: tsLater, Tokens: ampTokens{Input: 1}, Source: "thread"},
		{MessageID: "id", Timestamp: tsEarlier, Tokens: ampTokens{Input: 2}, Source: "thread"},
	}
	out := dedupAndMerge(events)
	if len(out) != 1 {
		t.Fatalf("expected one merged event, got %d", len(out))
	}
	if !out[0].Timestamp.Equal(tsEarlier) {
		t.Errorf("expected earlier ts %v, got %v", tsEarlier, out[0].Timestamp)
	}
}

func TestDedupAndMergeFillsZeroTimestampFromOther(t *testing.T) {
	tsOnly := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	events := []ampEvent{
		{MessageID: "id", Timestamp: time.Time{}, Tokens: ampTokens{Input: 1}},
		{MessageID: "id", Timestamp: tsOnly, Tokens: ampTokens{Input: 2}},
	}
	out := dedupAndMerge(events)
	if len(out) != 1 {
		t.Fatalf("expected 1 merged event, got %d", len(out))
	}
	if !out[0].Timestamp.Equal(tsOnly) {
		t.Errorf("expected non-zero ts %v, got %v", tsOnly, out[0].Timestamp)
	}
}

func TestDedupAndMergeEmpty(t *testing.T) {
	if got := dedupAndMerge(nil); got != nil {
		t.Errorf("expected nil for empty input, got %+v", got)
	}
}
