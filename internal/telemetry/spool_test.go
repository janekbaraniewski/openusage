package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSpoolAppendReadAck(t *testing.T) {
	dir := t.TempDir()
	s := NewSpool(dir)

	_, err := s.Append(SpoolRecord{
		SpoolID:       "one",
		CreatedAt:     time.Date(2026, time.February, 22, 10, 0, 0, 0, time.UTC),
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelHook,
		Payload:       json.RawMessage(`{"id":1}`),
	})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	_, err = s.Append(SpoolRecord{
		SpoolID:       "two",
		CreatedAt:     time.Date(2026, time.February, 22, 10, 0, 1, 0, time.UTC),
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelSSE,
		Payload:       json.RawMessage(`{"id":2}`),
	})
	if err != nil {
		t.Fatalf("append second: %v", err)
	}

	items, err := s.ReadOldest(10)
	if err != nil {
		t.Fatalf("ReadOldest: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Record.SpoolID != "one" {
		t.Fatalf("first spool id = %q, want one", items[0].Record.SpoolID)
	}
	if items[1].Record.SpoolID != "two" {
		t.Fatalf("second spool id = %q, want two", items[1].Record.SpoolID)
	}

	if err := s.Ack(items[0].Path); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	next, err := s.ReadOldest(10)
	if err != nil {
		t.Fatalf("ReadOldest after ack: %v", err)
	}
	if len(next) != 1 || next[0].Record.SpoolID != "two" {
		t.Fatalf("remaining record mismatch: %+v", next)
	}
}

func TestSpoolReadOldest_SkipsMalformedFile(t *testing.T) {
	dir := t.TempDir()
	s := NewSpool(dir)

	if _, err := s.Append(SpoolRecord{
		SpoolID:       "good",
		CreatedAt:     time.Date(2026, time.February, 22, 10, 0, 0, 0, time.UTC),
		SourceSystem:  SourceSystem("claude_code"),
		SourceChannel: SourceChannelJSONL,
		Payload:       json.RawMessage(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("append good: %v", err)
	}

	badPath := filepath.Join(dir, "9999999999999999999_bad.jsonl")
	if err := os.WriteFile(badPath, []byte("{not-json\n"), 0o644); err != nil {
		t.Fatalf("write malformed file: %v", err)
	}

	items, err := s.ReadOldest(10)
	if err == nil {
		t.Fatal("expected malformed-file warning error")
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Record.SpoolID != "good" {
		t.Fatalf("spool id = %q, want good", items[0].Record.SpoolID)
	}
}

func TestSpoolMarkFailed_IncrementsAttempt(t *testing.T) {
	dir := t.TempDir()
	s := NewSpool(dir)

	path, err := s.Append(SpoolRecord{
		SpoolID:       "retry-me",
		CreatedAt:     time.Date(2026, time.February, 22, 10, 0, 0, 0, time.UTC),
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelHook,
		Payload:       json.RawMessage(`{"x":1}`),
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	if err := s.MarkFailed(path, "timeout"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	items, err := s.ReadOldest(1)
	if err != nil {
		t.Fatalf("ReadOldest: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Record.Attempt != 1 {
		t.Fatalf("attempt = %d, want 1", items[0].Record.Attempt)
	}
	if items[0].Record.LastError != "timeout" {
		t.Fatalf("last_error = %q, want timeout", items[0].Record.LastError)
	}
}
