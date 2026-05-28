package openclaw

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestReadOpenClawIndex_ValidAndMissingEntries(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "ses_aaa.jsonl")
	writeFile(t, good, "")
	index := `{
		"k1": {"sessionId": "ses_aaa", "sessionFile": "ses_aaa.jsonl"},
		"k2": {"sessionId": "ses_bbb", "sessionFile": "missing.jsonl"},
		"k3": {"sessionId": "ses_ccc", "sessionFile": ""}
	}`
	indexPath := filepath.Join(dir, "sessions.json")
	writeFile(t, indexPath, index)

	entries, err := readOpenClawIndex(indexPath)
	if err != nil {
		t.Fatalf("readOpenClawIndex: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].SessionID != "ses_aaa" {
		t.Errorf("session id = %q", entries[0].SessionID)
	}
	if entries[0].SessionFile != good {
		t.Errorf("session file = %q, want %q", entries[0].SessionFile, good)
	}
}

func TestReadOpenClawTranscript_AssistantOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ses_x.jsonl")
	lines := `{"type":"message","message":{"role":"user","timestamp":1735689600000,"usage":{"input":10,"output":5}}}
{"type":"message","message":{"role":"assistant","timestamp":1735689601000,"provider":"anthropic","model":"claude-opus-4-5","usage":{"input":100,"output":50,"cacheRead":10,"cacheWrite":5,"totalTokens":165}}}
{"type":"message","message":{"role":"assistant","timestamp":1735689602000,"provider":"anthropic","model":"claude-opus-4-5"}}
`
	writeFile(t, path, lines)

	entries, err := readOpenClawTranscript(path, "ses_x")
	if err != nil {
		t.Fatalf("readOpenClawTranscript: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d, want 1 entry", len(entries))
	}
	e := entries[0]
	if e.Provider != "anthropic" || e.Model != "claude-opus-4-5" {
		t.Errorf("provider/model = %q/%q", e.Provider, e.Model)
	}
	if e.Input != 100 || e.Output != 50 || e.CacheRead != 10 || e.CacheWrite != 5 {
		t.Errorf("token fields wrong: %+v", e)
	}
	if e.SessionID != "ses_x" {
		t.Errorf("sessionID = %q", e.SessionID)
	}
}

func TestReadOpenClawTranscript_VendorCostPassthrough(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ses_cost.jsonl")
	line := `{"type":"message","message":{"role":"assistant","timestamp":1735689600000,"provider":"anthropic","model":"claude-opus-4-5","usage":{"input":100,"output":50,"cost":{"total":0.0042}}}}` + "\n"
	writeFile(t, path, line)

	entries, err := readOpenClawTranscript(path, "ses_cost")
	if err != nil {
		t.Fatalf("readOpenClawTranscript: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if !entries[0].HasCost || entries[0].CostUSD != 0.0042 {
		t.Errorf("cost passthrough failed: %+v", entries[0])
	}
}

func TestReadOpenClawTranscript_TimestampFormats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ses_ts.jsonl")
	lines := `{"type":"message","message":{"role":"assistant","timestamp":1735689600000,"provider":"a","model":"m","usage":{"input":1,"output":1}}}
{"type":"message","message":{"role":"assistant","timestamp":"2025-01-01T00:00:00Z","provider":"a","model":"m","usage":{"input":1,"output":1}}}
{"type":"message","message":{"role":"assistant","timestamp":"not-a-time","provider":"a","model":"m","usage":{"input":1,"output":1}}}
`
	writeFile(t, path, lines)

	entries, err := readOpenClawTranscript(path, "ses_ts")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Timestamp.IsZero() {
		t.Errorf("ms timestamp not parsed")
	}
	if entries[1].Timestamp.IsZero() {
		t.Errorf("RFC3339 timestamp not parsed")
	}
	if !entries[2].Timestamp.IsZero() {
		t.Errorf("garbage timestamp should be zero, got %v", entries[2].Timestamp)
	}
}

func TestReadOpenClawTranscript_ModelTypeInjection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ses_modelinj.jsonl")
	// First line: model declaration. Second: assistant message without
	// embedded provider/model — should adopt them from the declaration.
	lines := `{"type":"model","data":{"provider":"openai","modelId":"gpt-4o-mini"}}
{"type":"message","message":{"role":"assistant","timestamp":1735689600000,"usage":{"input":50,"output":25}}}
`
	writeFile(t, path, lines)

	entries, err := readOpenClawTranscript(path, "ses_modelinj")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Provider != "openai" || entries[0].Model != "gpt-4o-mini" {
		t.Errorf("provider/model = %q/%q, want openai/gpt-4o-mini", entries[0].Provider, entries[0].Model)
	}
}

func TestReadOpenClawTranscript_FlatBasename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ses_basename.jsonl")
	line := `{"type":"message","message":{"role":"assistant","timestamp":1735689600000,"provider":"a","model":"m","usage":{"input":1,"output":1}}}` + "\n"
	writeFile(t, path, line)

	entries, err := readOpenClawTranscript(path, "")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d, want 1", len(entries))
	}
	if entries[0].SessionID != "ses_basename" {
		t.Errorf("sessionID = %q, want ses_basename", entries[0].SessionID)
	}
}

func TestReadOpenClawTranscript_SkipsMessagesWithoutUsage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ses_no_usage.jsonl")
	lines := `{"type":"message","message":{"role":"assistant","timestamp":1735689600000,"provider":"a","model":"m"}}
{"type":"message","message":{"role":"assistant","timestamp":1735689600000,"provider":"a","model":"m","usage":{"input":0,"output":0}}}
`
	writeFile(t, path, lines)

	entries, err := readOpenClawTranscript(path, "ses_no_usage")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}
