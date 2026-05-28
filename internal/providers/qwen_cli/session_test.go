package qwen_cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestReadQwenChatFile_ValidMix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abc.jsonl")
	writeJSONL(t, path,
		`{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","sessionId":"s-1","content":"hi"}`,
		`{"type":"assistant","model":"qwen3-coder","timestamp":"2026-01-01T00:00:01.000Z","sessionId":"s-1","usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":50,"thoughtsTokenCount":20,"cachedContentTokenCount":10}}`,
		`{"type":"assistant","model":"qwen3-coder","timestamp":"2026-01-01T00:00:02.000Z","sessionId":"s-1"}`,
		`{"type":"assistant","model":"qwen3-coder","timestamp":"2026-01-01T00:00:03.000Z","sessionId":"s-1","usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"thoughtsTokenCount":0,"cachedContentTokenCount":0}}`,
		`not valid json`,
		`{"type":"assistant","model":"qwen3-coder","timestamp":"2026-01-01T00:00:04.000Z","sessionId":"s-1","usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3}}`,
	)

	entries, err := readQwenChatFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	e0 := entries[0]
	if e0.Model != "qwen3-coder" {
		t.Errorf("model = %q", e0.Model)
	}
	if e0.Provider != "qwen" {
		t.Errorf("provider = %q", e0.Provider)
	}
	if e0.SessionID != "s-1" {
		t.Errorf("sessionID = %q", e0.SessionID)
	}
	if e0.Input != 100 || e0.Output != 50 || e0.Reasoning != 20 || e0.Cached != 10 {
		t.Errorf("tokens = (in=%d,out=%d,reason=%d,cached=%d)", e0.Input, e0.Output, e0.Reasoning, e0.Cached)
	}
	if e0.Timestamp.IsZero() {
		t.Error("timestamp not parsed")
	}

	if entries[1].Input != 7 || entries[1].Output != 3 {
		t.Errorf("entry1 tokens unexpected: in=%d out=%d", entries[1].Input, entries[1].Output)
	}
}

func TestReadQwenChatFile_MissingModelDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	writeJSONL(t, path,
		`{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","sessionId":"s-2","usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`,
	)
	entries, err := readQwenChatFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len=%d", len(entries))
	}
	if entries[0].Model != "unknown" {
		t.Errorf("model = %q, want unknown", entries[0].Model)
	}
}

func TestReadQwenChatFile_MissingSessionIDDerived(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "myproject")
	chatsDir := filepath.Join(projectDir, chatsSubdir)
	path := filepath.Join(chatsDir, "topic-42.jsonl")
	writeJSONL(t, path,
		`{"type":"assistant","model":"qwen3-coder","timestamp":"2026-01-01T00:00:01.000Z","usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":5}}`,
	)
	entries, err := readQwenChatFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len=%d", len(entries))
	}
	want := "myproject-topic-42"
	if entries[0].SessionID != want {
		t.Errorf("sessionID = %q, want %q", entries[0].SessionID, want)
	}
}

func TestReadQwenChatFile_NonAssistantOrMissingMetadataIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	writeJSONL(t, path,
		`{"type":"system","content":"start"}`,
		`{"type":"user","content":"hi"}`,
		`{"type":"assistant","content":"no metadata"}`,
	)
	entries, err := readQwenChatFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestDeriveSessionID_DistinctAcrossProjectsAndFiles(t *testing.T) {
	root := t.TempDir()
	p1 := filepath.Join(root, "proj-a", chatsSubdir, "foo.jsonl")
	p2 := filepath.Join(root, "proj-b", chatsSubdir, "foo.jsonl")
	p3 := filepath.Join(root, "proj-a", chatsSubdir, "bar.jsonl")

	got1 := deriveSessionID(p1)
	got2 := deriveSessionID(p2)
	got3 := deriveSessionID(p3)

	if got1 == got2 {
		t.Errorf("expected distinct ids across projects, got %q", got1)
	}
	if got1 == got3 {
		t.Errorf("expected distinct ids across files, got %q", got1)
	}
	if got1 != "proj-a-foo" {
		t.Errorf("derived id = %q, want proj-a-foo", got1)
	}
}
