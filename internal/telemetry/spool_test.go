package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestSpoolCleanup_RemovesOldByAge(t *testing.T) {
	dir := t.TempDir()
	s := NewSpool(dir)

	oldPath, err := s.Append(SpoolRecord{
		SpoolID:       "old",
		CreatedAt:     time.Now().Add(-10 * time.Hour).UTC(),
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelSQLite,
		Payload:       json.RawMessage(`{"x":1}`),
	})
	if err != nil {
		t.Fatalf("append old: %v", err)
	}
	newPath, err := s.Append(SpoolRecord{
		SpoolID:       "new",
		CreatedAt:     time.Now().UTC(),
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelHook,
		Payload:       json.RawMessage(`{"x":2}`),
	})
	if err != nil {
		t.Fatalf("append new: %v", err)
	}

	oldTS := time.Now().Add(-10 * time.Hour)
	if err := os.Chtimes(oldPath, oldTS, oldTS); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	newTS := time.Now()
	if err := os.Chtimes(newPath, newTS, newTS); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}

	result, err := s.Cleanup(SpoolCleanupPolicy{MaxAge: 2 * time.Hour})
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if result.RemovedFiles != 1 {
		t.Fatalf("removed_files = %d, want 1", result.RemovedFiles)
	}

	items, err := s.ReadOldest(10)
	if err != nil {
		t.Fatalf("ReadOldest: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Record.SpoolID != "new" {
		t.Fatalf("remaining spool id = %q, want new", items[0].Record.SpoolID)
	}
}

func TestSpoolCleanup_EnforcesFileAndByteCaps(t *testing.T) {
	dir := t.TempDir()
	s := NewSpool(dir)

	makePayload := func(size int) json.RawMessage {
		return json.RawMessage(`{"blob":"` + strings.Repeat("x", size) + `"}`)
	}

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_, err := s.Append(SpoolRecord{
			SpoolID:       fmt.Sprintf("id-%d", i),
			CreatedAt:     now.Add(time.Duration(i) * time.Second),
			SourceSystem:  SourceSystem("opencode"),
			SourceChannel: SourceChannelSQLite,
			Payload:       makePayload(500),
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Keep at most 3 files and ~2KB total payload envelope.
	result, err := s.Cleanup(SpoolCleanupPolicy{
		MaxFiles: 3,
		MaxBytes: 2200,
	})
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if result.RemainingFiles > 3 {
		t.Fatalf("remaining_files = %d, want <= 3", result.RemainingFiles)
	}
	if result.RemainingBytes > 2200 {
		t.Fatalf("remaining_bytes = %d, want <= 2200", result.RemainingBytes)
	}
	if result.RemovedFiles < 2 {
		t.Fatalf("removed_files = %d, want >= 2", result.RemovedFiles)
	}
}
