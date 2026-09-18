package kimi_cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeWire(t *testing.T, lines ...string) (path, sessionID string) {
	t.Helper()
	root := t.TempDir()
	group := "group-a"
	uuid := "sess-uuid-123"
	sessionID = group + "/" + uuid
	dir := filepath.Join(root, group, uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path = filepath.Join(dir, "wire.jsonl")
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path, sessionID
}

func TestReadKimiWireFile_StatusUpdateFullTokens(t *testing.T) {
	line := `{"timestamp":1735689600.5,"message":{"type":"StatusUpdate","payload":{"token_usage":{"input_other":100,"output":50,"input_cache_read":10,"input_cache_creation":5},"message_id":"msg_001"}}}`
	path, sessionID := writeWire(t, line)

	entries, err := readKimiWireFileWithModel(path, "kimi-for-coding")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", e.SessionID, sessionID)
	}
	if e.Provider != "moonshot" {
		t.Errorf("Provider = %q, want moonshot", e.Provider)
	}
	if e.Model != "kimi-for-coding" {
		t.Errorf("Model = %q, want kimi-for-coding", e.Model)
	}
	if e.Input != 100 || e.Output != 50 || e.CacheRead != 10 || e.CacheWrite != 5 {
		t.Errorf("tokens = (%d,%d,%d,%d), want (100,50,10,5)", e.Input, e.Output, e.CacheRead, e.CacheWrite)
	}
	wantTS := time.Unix(1735689600, 500_000_000).UTC()
	if !e.Timestamp.Equal(wantTS) {
		t.Errorf("Timestamp = %v, want %v", e.Timestamp, wantTS)
	}
}

func TestReadKimiWireFile_SkipsNonStatusUpdate(t *testing.T) {
	path, _ := writeWire(t,
		`{"timestamp":1735689600.0,"message":{"type":"UserMessage","payload":{"text":"hi"}}}`,
		`{"timestamp":1735689601.0,"message":{"type":"ToolCall","payload":{}}}`,
	)
	entries, err := readKimiWireFileWithModel(path, defaultModel)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(entries))
	}
}

func TestReadKimiWireFile_SkipsMissingTokenUsage(t *testing.T) {
	path, _ := writeWire(t,
		`{"timestamp":1735689600.0,"message":{"type":"StatusUpdate","payload":{"message_id":"only-id"}}}`,
	)
	entries, err := readKimiWireFileWithModel(path, defaultModel)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(entries))
	}
}

func TestReadKimiWireFile_SkipsZeroTokens(t *testing.T) {
	path, _ := writeWire(t,
		`{"timestamp":1735689600.0,"message":{"type":"StatusUpdate","payload":{"token_usage":{"input_other":0,"output":0,"input_cache_read":0,"input_cache_creation":0}}}}`,
	)
	entries, err := readKimiWireFileWithModel(path, defaultModel)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(entries))
	}
}

func TestReadKimiWireFile_FloatSecondTimestamp(t *testing.T) {
	// Sub-second precision must survive the round-trip.
	line := `{"timestamp":1735689600.123,"message":{"type":"StatusUpdate","payload":{"token_usage":{"output":1}}}}`
	path, _ := writeWire(t, line)
	entries, err := readKimiWireFileWithModel(path, defaultModel)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	want := time.Unix(1735689600, 123_000_000).UTC()
	got := entries[0].Timestamp
	// Allow a 1us tolerance to absorb float rounding noise from json parsing.
	diff := got.Sub(want)
	if diff < -time.Microsecond || diff > time.Microsecond {
		t.Errorf("Timestamp = %v, want ~%v (diff %v)", got, want, diff)
	}
}

func TestReadKimiWireFile_SkipsMalformedLines(t *testing.T) {
	path, _ := writeWire(t,
		`{not json`,
		``,
		`{"timestamp":1735689600.0,"message":{"type":"StatusUpdate","payload":{"token_usage":{"input_other":7,"output":3}}}}`,
		`also-garbage`,
	)
	entries, err := readKimiWireFileWithModel(path, defaultModel)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 (malformed lines should be skipped)", len(entries))
	}
	if entries[0].Input != 7 || entries[0].Output != 3 {
		t.Errorf("tokens = (%d,%d), want (7,3)", entries[0].Input, entries[0].Output)
	}
}

func TestReadKimiConfigModel_FromFile(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"model":"kimi-k2","other":"ignored"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readKimiConfigModel(cfg); got != "kimi-k2" {
		t.Errorf("readKimiConfigModel = %q, want kimi-k2", got)
	}
}

func TestReadKimiConfigModel_FallbackOnMissing(t *testing.T) {
	if got := readKimiConfigModel(filepath.Join(t.TempDir(), "nope.json")); got != defaultModel {
		t.Errorf("missing file: got %q, want %q", got, defaultModel)
	}
	if got := readKimiConfigModel(""); got != defaultModel {
		t.Errorf("empty path: got %q, want %q", got, defaultModel)
	}
}

func TestReadKimiConfigModel_FallbackOnMalformedOrEmpty(t *testing.T) {
	dir := t.TempDir()

	garbage := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(garbage, []byte(`{not-json`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readKimiConfigModel(garbage); got != defaultModel {
		t.Errorf("malformed: got %q, want %q", got, defaultModel)
	}

	noModel := filepath.Join(dir, "nomodel.json")
	if err := os.WriteFile(noModel, []byte(`{"other":"x"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readKimiConfigModel(noModel); got != defaultModel {
		t.Errorf("no model field: got %q, want %q", got, defaultModel)
	}
}

func TestReadKimiWireFile_SessionIDIncludesGroup(t *testing.T) {
	root := t.TempDir()
	uuid := "shared-uuid"
	for _, group := range []string{"alpha", "beta"} {
		dir := filepath.Join(root, group, uuid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		line := `{"timestamp":1735689600.0,"message":{"type":"StatusUpdate","payload":{"token_usage":{"output":1}}}}`
		if err := os.WriteFile(filepath.Join(dir, "wire.jsonl"), []byte(line+"\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	got := make(map[string]struct{})
	for _, group := range []string{"alpha", "beta"} {
		entries, err := readKimiWireFileWithModel(filepath.Join(root, group, uuid, "wire.jsonl"), defaultModel)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(entries))
		}
		got[entries[0].SessionID] = struct{}{}
	}
	if len(got) != 2 {
		t.Errorf("session ids = %v, want 2 distinct entries (group prefix must disambiguate)", got)
	}
	for _, want := range []string{"alpha/shared-uuid", "beta/shared-uuid"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing session id %q in %v", want, got)
		}
	}
}

func TestReadKimiWireFile_MissingFile(t *testing.T) {
	entries, err := readKimiWireFileWithModel(filepath.Join(t.TempDir(), "nope.jsonl"), defaultModel)
	if err != nil {
		t.Fatalf("missing file should be nil error, got %v", err)
	}
	if entries != nil {
		t.Errorf("missing file entries = %v, want nil", entries)
	}
}

func TestReadKimiWireFile_KimiCodeUsageRecord(t *testing.T) {
	// Kimi Code CLI nests wire logs at
	// <root>/<group>/<uuid>/agents/<agent>/wire.jsonl and emits camelCase
	// usage.record frames with an epoch-milliseconds timestamp.
	root := t.TempDir()
	dir := filepath.Join(root, "wd_project_abc", "session-uuid-456", "agents", "main")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "wire.jsonl")
	lines := "" +
		`{"type":"metadata","version":1}` + "\n" +
		`{"type":"usage.record","agentId":"main","model":"kimi-code/k3","usage":{"inputOther":52290,"output":376,"inputCacheRead":11264,"inputCacheCreation":0},"usageScope":"turn","time":1789752352149}` + "\n" +
		// Zero-usage record — must be ignored.
		`{"type":"usage.record","agentId":"main","model":"kimi-code/k3","usage":{"inputOther":0,"output":0,"inputCacheRead":0,"inputCacheCreation":0},"usageScope":"turn","time":1789752353000}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	entries, err := readKimiWireFileWithModel(path, defaultModel)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.SessionID != "wd_project_abc/session-uuid-456" {
		t.Errorf("SessionID = %q, want wd_project_abc/session-uuid-456 (agents dir must be climbed)", e.SessionID)
	}
	if e.Provider != "moonshot" {
		t.Errorf("Provider = %q, want moonshot", e.Provider)
	}
	if e.Model != "kimi-code/k3" {
		t.Errorf("Model = %q, want kimi-code/k3", e.Model)
	}
	if e.Input != 52290 || e.Output != 376 || e.CacheRead != 11264 || e.CacheWrite != 0 {
		t.Errorf("tokens = (%d,%d,%d,%d), want (52290,376,11264,0)", e.Input, e.Output, e.CacheRead, e.CacheWrite)
	}
	wantTS := time.UnixMilli(1789752352149).UTC()
	if !e.Timestamp.Equal(wantTS) {
		t.Errorf("Timestamp = %v, want %v", e.Timestamp, wantTS)
	}
}
