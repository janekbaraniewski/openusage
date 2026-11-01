package daemon

import (
	"testing"
	"time"
)

// Collectors that re-emit their full history every cycle produce a large
// returned count with zero new ingests. The loop must still back off.
func TestCollectBackoff_BacksOffOnZeroIngested(t *testing.T) {
	b := newCollectBackoff(30 * time.Second)

	var got time.Duration
	for i := 0; i < 20; i++ {
		got, _ = b.observe(0, false)
	}
	if got != collectBackoffMax {
		t.Fatalf("interval after 20 idle cycles = %s, want %s", got, collectBackoffMax)
	}

	// The first two idle cycles keep the base interval.
	b = newCollectBackoff(30 * time.Second)
	for i := 0; i < 2; i++ {
		if got, _ = b.observe(0, false); got != 30*time.Second {
			t.Fatalf("cycle %d: interval = %s, want 30s", i, got)
		}
	}
	if got, _ = b.observe(0, false); got != time.Minute {
		t.Fatalf("third idle cycle: interval = %s, want 1m", got)
	}
}

func TestCollectBackoff_ResetsOnIngest(t *testing.T) {
	b := newCollectBackoff(30 * time.Second)
	for i := 0; i < 10; i++ {
		b.observe(0, false)
	}
	got, changed := b.observe(5, false)
	if got != 30*time.Second || !changed {
		t.Fatalf("after ingest: interval = %s changed=%t, want 30s true", got, changed)
	}
}

func TestCollectBackoff_ChangeTriggeredIdleCycleDoesNotAccelerate(t *testing.T) {
	b := newCollectBackoff(30 * time.Second)
	for i := 0; i < 10; i++ {
		if got, _ := b.observe(0, true); got != 30*time.Second {
			t.Fatalf("change-triggered idle cycle changed interval to %s", got)
		}
	}
}
