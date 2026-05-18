package gemini_cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindGeminiSessionFiles_LegacyAndModernLayouts(t *testing.T) {
	tmp := t.TempDir()

	// Legacy layout: <tmp>/<uuid>/session-XYZ.json
	legacyDir := filepath.Join(tmp, "uuid-legacy")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, "session-abc.json")
	if err := os.WriteFile(legacyPath, []byte(`{"sessionId":"legacy","messages":[]}`), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	// Modern layout: <tmp>/<uuid>/chats/foo.json + chats/foo.jsonl
	chatsDir := filepath.Join(tmp, "uuid-modern", "chats")
	if err := os.MkdirAll(chatsDir, 0o755); err != nil {
		t.Fatalf("mkdir chats: %v", err)
	}
	modernJSON := filepath.Join(chatsDir, "foo.json")
	if err := os.WriteFile(modernJSON, []byte(`{"sessionId":"modern-json","messages":[]}`), 0o644); err != nil {
		t.Fatalf("write modern json: %v", err)
	}
	modernJSONL := filepath.Join(chatsDir, "foo.jsonl")
	if err := os.WriteFile(modernJSONL, []byte(`{"sessionId":"modern-jsonl"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write modern jsonl: %v", err)
	}

	// Backup subtree (must be ignored).
	backupDir := filepath.Join(tmp, "uuid-modern", "chats", "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup: %v", err)
	}
	backupPath := filepath.Join(backupDir, "bar.json")
	if err := os.WriteFile(backupPath, []byte(`{"sessionId":"backup","messages":[]}`), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	got, err := findGeminiSessionFiles(tmp)
	if err != nil {
		t.Fatalf("findGeminiSessionFiles: %v", err)
	}

	want := map[string]bool{
		legacyPath:  true,
		modernJSON:  true,
		modernJSONL: true,
	}
	for _, p := range got {
		if p == backupPath {
			t.Errorf("backup file should have been skipped, but appeared in results: %s", p)
		}
		delete(want, p)
	}
	if len(want) > 0 {
		t.Errorf("missing expected files: %v (got %v)", want, got)
	}
}

func TestReadGeminiChatJSONL_HeaderAndMessageDedup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "foo.jsonl")
	lines := `{"sessionId":"sess-jsonl","startTime":"2026-05-19T10:00:00Z","lastUpdated":"2026-05-19T10:05:00Z"}
{"id":"m1","type":"user","timestamp":"2026-05-19T10:00:01Z"}
{"id":"m2","type":"model","timestamp":"2026-05-19T10:00:02Z","model":"gemini-2.5-pro","tokens":{"input":100,"output":50,"total":150}}
{"id":"m2","type":"model","timestamp":"2026-05-19T10:00:03Z","model":"gemini-2.5-pro","tokens":{"input":200,"output":80,"total":280}}
`
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	chat, err := readGeminiChatJSONL(path)
	if err != nil {
		t.Fatalf("readGeminiChatJSONL: %v", err)
	}
	if chat.SessionID != "sess-jsonl" {
		t.Errorf("SessionID = %q, want sess-jsonl", chat.SessionID)
	}
	if len(chat.Messages) != 2 {
		t.Fatalf("Messages = %d, want 2 (duplicate m2 should collapse)", len(chat.Messages))
	}
	// The replaced m2 should carry the LATER tokens.
	m2 := chat.Messages[1]
	if m2.Tokens == nil || m2.Tokens.Total != 280 {
		t.Errorf("expected m2 tokens to be the later record (total=280); got %+v", m2.Tokens)
	}
}
