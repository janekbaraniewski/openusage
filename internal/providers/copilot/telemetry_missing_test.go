package copilot

import (
	"path/filepath"
	"testing"
)

// TestParseSessionFile_MissingEventsIsNotAnError guards the fix for session
// directories that outlive their rotated events.jsonl: a missing file must be
// skipped (nil, nil) rather than failing the whole telemetry collection.
func TestParseSessionFile_MissingEventsIsNotAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nonexistent", "events.jsonl")
	events, err := parseCopilotTelemetrySessionFile(missing, "sess-1")
	if err != nil {
		t.Fatalf("missing events.jsonl should not error, got: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events for missing file, got %d", len(events))
	}
}
