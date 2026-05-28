package codebuff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeChat(t *testing.T, root, channel, project, chatID, body string) {
	t.Helper()
	dir := filepath.Join(root, channel, "projects", project, "chats", chatID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat-messages.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestReadCodebuffChatFile_MetadataUsage(t *testing.T) {
	dir := t.TempDir()
	chat := filepath.Join(dir, "chat-messages.json")
	body := `[
		{"role":"user","id":"u1"},
		{"role":"assistant","id":"a1","metadata":{"usage":{
			"input_tokens": 100, "output_tokens": 50,
			"cache_read_input_tokens": 10, "cache_creation_input_tokens": 5,
			"credits": 0.25, "model": "claude-opus-4-7"
		},"timestamp":"2025-12-14T10:00:00.000Z"}}
	]`
	if err := os.WriteFile(chat, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := readCodebuffChatFile(chat)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Input != 100 || e.Output != 50 || e.CacheRead != 10 || e.CacheWrite != 5 {
		t.Errorf("tokens wrong: %+v", e)
	}
	if !e.HasCredits || e.Credits != 0.25 {
		t.Errorf("credits wrong: %+v", e)
	}
	if e.Model != "claude-opus-4-7" || e.Provider != "anthropic" {
		t.Errorf("model/provider wrong: %+v", e)
	}
	if e.DedupKey != "id:a1" {
		t.Errorf("dedup = %q, want id:a1", e.DedupKey)
	}
}

func TestReadCodebuffChatFile_CodebuffNestedUsage(t *testing.T) {
	dir := t.TempDir()
	chat := filepath.Join(dir, "chat-messages.json")
	body := `[
		{"role":"assistant","id":"a1","metadata":{
			"codebuff":{"usage":{"input_tokens":200,"output_tokens":75,"model":"gpt-5"}}
		}}
	]`
	if err := os.WriteFile(chat, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := readCodebuffChatFile(chat)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d, want 1", len(entries))
	}
	if entries[0].Input != 200 || entries[0].Output != 75 {
		t.Errorf("tokens wrong: %+v", entries[0])
	}
	if entries[0].Provider != "openai" {
		t.Errorf("provider = %q, want openai", entries[0].Provider)
	}
}

func TestReadCodebuffChatFile_RunStateFallback(t *testing.T) {
	dir := t.TempDir()
	chat := filepath.Join(dir, "chat-messages.json")
	body := `[
		{"role":"assistant","metadata":{
			"runState":{"sessionState":{"mainAgentState":{"messageHistory":[
				{"providerOptions":{"usage":{"input_tokens":42,"output_tokens":17,"model":"gemini-2.5-pro"}}}
			]}}}
		}}
	]`
	if err := os.WriteFile(chat, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := readCodebuffChatFile(chat)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 || entries[0].Input != 42 || entries[0].Output != 17 {
		t.Fatalf("wrong: %+v", entries)
	}
	if entries[0].Provider != "google" {
		t.Errorf("provider = %q", entries[0].Provider)
	}
}

func TestReadCodebuffChatFile_ZeroTokensSkipped(t *testing.T) {
	dir := t.TempDir()
	chat := filepath.Join(dir, "chat-messages.json")
	body := `[
		{"role":"assistant","metadata":{"usage":{"input_tokens":0,"output_tokens":0}}}
	]`
	if err := os.WriteFile(chat, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := readCodebuffChatFile(chat)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestReadCodebuffChatFile_DefaultModel(t *testing.T) {
	dir := t.TempDir()
	chat := filepath.Join(dir, "chat-messages.json")
	body := `[
		{"role":"assistant","metadata":{"usage":{"input_tokens":10,"output_tokens":5}}}
	]`
	if err := os.WriteFile(chat, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := readCodebuffChatFile(chat)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 || entries[0].Model != "codebuff-unknown" {
		t.Errorf("default model wrong: %+v", entries)
	}
}

func TestReadCodebuffChatFile_MalformedJSONIgnored(t *testing.T) {
	dir := t.TempDir()
	chat := filepath.Join(dir, "chat-messages.json")
	if err := os.WriteFile(chat, []byte(`{not-an-array`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := readCodebuffChatFile(chat)
	if err != nil {
		t.Fatalf("read should not error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestReadAllChats_DedupViaID(t *testing.T) {
	root := t.TempDir()
	body := `[
		{"role":"assistant","id":"shared","metadata":{"usage":{"input_tokens":10,"output_tokens":5,"model":"gpt-5"}}}
	]`
	writeChat(t, root, "manicode", "proj", "2025-12-14T10-00-00.000Z", body)
	writeChat(t, root, "manicode-dev", "proj", "2025-12-15T11-00-00.000Z", body)

	dirs := []string{
		filepath.Join(root, "manicode"),
		filepath.Join(root, "manicode-dev"),
	}
	entries, err := readAllChats(context.Background(), dirs)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("dedup by id failed: got %d entries", len(entries))
	}
}

func TestReadAllChats_DedupViaDerivedKey(t *testing.T) {
	root := t.TempDir()
	// Two distinct messages, no stable id, structurally identical tokens —
	// different ordinals must keep them distinct.
	body := `[
		{"role":"assistant","metadata":{"usage":{"input_tokens":10,"output_tokens":5,"model":"gpt-5"}}},
		{"role":"assistant","metadata":{"usage":{"input_tokens":10,"output_tokens":5,"model":"gpt-5"}}}
	]`
	writeChat(t, root, "manicode", "proj", "2025-12-14T10-00-00.000Z", body)
	dirs := []string{filepath.Join(root, "manicode")}
	entries, err := readAllChats(context.Background(), dirs)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 distinct messages, got %d", len(entries))
	}

	// Now copy the same chat into a second channel — same ordinals, same
	// tokens, same chatId → must dedup.
	writeChat(t, root, "manicode", "proj-dup", "2025-12-14T10-00-00.000Z", body)
	// Different project: distinct dedup key, NOT deduped.
	dirs2 := []string{filepath.Join(root, "manicode")}
	entries2, err := readAllChats(context.Background(), dirs2)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries2) != 4 {
		t.Errorf("expected 4 across two projects, got %d", len(entries2))
	}
}

func TestReadAllChats_MultiChannelWalk(t *testing.T) {
	root := t.TempDir()
	bodyA := `[
		{"role":"assistant","id":"a","metadata":{"usage":{"input_tokens":10,"output_tokens":5,"model":"claude-sonnet-4-5"}}}
	]`
	bodyB := `[
		{"role":"assistant","id":"b","metadata":{"usage":{"input_tokens":20,"output_tokens":7,"model":"gpt-5"}}}
	]`
	writeChat(t, root, "manicode", "proj1", "2025-12-14T10-00-00.000Z", bodyA)
	writeChat(t, root, "manicode-staging", "proj2", "2025-12-15T11-00-00.000Z", bodyB)

	dirs := []string{
		filepath.Join(root, "manicode"),
		filepath.Join(root, "manicode-staging"),
	}
	entries, err := readAllChats(context.Background(), dirs)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	channels := map[string]bool{}
	for _, e := range entries {
		channels[e.Channel] = true
	}
	if !channels["manicode"] || !channels["manicode-staging"] {
		t.Errorf("missing channel: %v", channels)
	}
}

func TestInferProvider(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-7": "anthropic",
		"gpt-5":           "openai",
		"o1-preview":      "openai",
		"gemini-2.5-pro":  "google",
		"qwen-coder":      "unknown",
		"":                "unknown",
	}
	for in, want := range cases {
		if got := inferProvider(in); got != want {
			t.Errorf("inferProvider(%q) = %q, want %q", in, got, want)
		}
	}
}
