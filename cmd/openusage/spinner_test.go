package main

import "testing"

func TestSpinner_StopIsSafeAndIdempotent(t *testing.T) {
	// In `go test` stderr is not a terminal, so this exercises the no-op path:
	// it must not panic and stop() must be safe to call more than once.
	s := startSpinner("working…")
	s.stop()
	s.stop()
}
