package core

import (
	"testing"
	"time"
)

func TestLogThrottleAllow(t *testing.T) {
	throttle := NewLogThrottle(4, time.Minute)
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)

	if !throttle.Allow("read_model", 2*time.Second, now) {
		t.Fatal("first call should be allowed")
	}
	if throttle.Allow("read_model", 2*time.Second, now.Add(time.Second)) {
		t.Fatal("second call inside interval should be blocked")
	}
	if !throttle.Allow("read_model", 2*time.Second, now.Add(3*time.Second)) {
		t.Fatal("call after interval should be allowed")
	}
}

func TestLogThrottlePrunesOldestEntries(t *testing.T) {
	throttle := NewLogThrottle(2, time.Minute)
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)

	if !throttle.Allow("a", 0, base) {
		t.Fatal("expected a")
	}
	if !throttle.Allow("b", 0, base.Add(time.Second)) {
		t.Fatal("expected b")
	}
	if !throttle.Allow("c", 0, base.Add(2*time.Second)) {
		t.Fatal("expected c")
	}
	if len(throttle.lastAt) != 2 {
		t.Fatalf("len(lastAt) = %d, want 2", len(throttle.lastAt))
	}
	if _, ok := throttle.lastAt["a"]; ok {
		t.Fatal("oldest entry should have been pruned")
	}
}
