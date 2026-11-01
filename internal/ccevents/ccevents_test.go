package ccevents

import (
	"testing"
	"time"
)

func TestBlockSince(t *testing.T) {
	now := time.Date(2026, 11, 1, 15, 30, 0, 0, time.UTC)
	if got, want := BlockSince(now), now.Add(-BlockLookback); !got.Equal(want) {
		t.Fatalf("BlockSince = %v, want %v", got, want)
	}
}

// TodaySince must reach local midnight (so "today" totals are exact) and at
// least the block lookback (so the active block keeps its history).
func TestTodaySince(t *testing.T) {
	loc := time.FixedZone("X", 2*3600)
	cases := []struct {
		now, want time.Time
	}{
		// Late in the day: midnight is further back than the block lookback.
		{time.Date(2026, 11, 1, 22, 0, 0, 0, loc), time.Date(2026, 11, 1, 0, 0, 0, 0, loc)},
		// Early morning: the block lookback reaches into yesterday.
		{time.Date(2026, 11, 1, 6, 0, 0, 0, loc), time.Date(2026, 10, 31, 20, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := TodaySince(c.now); !got.Equal(c.want) {
			t.Errorf("TodaySince(%v) = %v, want %v", c.now, got, c.want)
		}
	}
}
