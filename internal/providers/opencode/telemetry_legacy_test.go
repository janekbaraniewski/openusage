package opencode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// TestCollectTelemetryFromLegacyStorage_ParsesAssistantMessage verifies that
// a pre-v1.2 OpenCode JSON message under storage/message/<session>/<id>.json
// projects to a MessageUsage telemetry event with the expected token/cost
// fields.
func TestCollectTelemetryFromLegacyStorage_ParsesAssistantMessage(t *testing.T) {
	storageDir := t.TempDir()
	sessionDir := filepath.Join(storageDir, "sess-legacy-1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	// One valid assistant message with usage.
	assistant := `{
		"id": "msg-legacy-1",
		"sessionID": "sess-legacy-1",
		"role": "assistant",
		"modelID": "claude-3.5-sonnet",
		"providerID": "anthropic",
		"cost": 0.0123,
		"tokens": {"input": 150, "output": 75, "reasoning": 10, "cache": {"read": 20, "write": 5}},
		"time": {"created": 1771754400000, "completed": 1771754405000},
		"path": {"cwd": "/tmp/work"},
		"finish": "stop"
	}`
	if err := os.WriteFile(filepath.Join(sessionDir, "msg-legacy-1.json"), []byte(assistant), 0o600); err != nil {
		t.Fatalf("write assistant msg: %v", err)
	}

	// One user message — should be skipped (role != assistant).
	user := `{"id": "msg-user-1", "role": "user", "sessionID": "sess-legacy-1"}`
	if err := os.WriteFile(filepath.Join(sessionDir, "msg-user-1.json"), []byte(user), 0o600); err != nil {
		t.Fatalf("write user msg: %v", err)
	}

	// One unparseable file — should be tolerated.
	if err := os.WriteFile(filepath.Join(sessionDir, "garbage.json"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}

	events, err := CollectTelemetryFromLegacyStorage(context.Background(), storageDir)
	if err != nil {
		t.Fatalf("CollectTelemetryFromLegacyStorage err: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 (only the assistant message)", len(events))
	}
	ev := events[0]
	if ev.SchemaVersion != telemetryLegacySchema {
		t.Fatalf("schema = %q, want %q", ev.SchemaVersion, telemetryLegacySchema)
	}
	if ev.EventType != shared.TelemetryEventTypeMessageUsage {
		t.Fatalf("event_type = %s", ev.EventType)
	}
	if ev.MessageID != "msg-legacy-1" {
		t.Fatalf("message_id = %q", ev.MessageID)
	}
	if ev.SessionID != "sess-legacy-1" {
		t.Fatalf("session_id = %q", ev.SessionID)
	}
	if ev.ModelRaw != "claude-3.5-sonnet" {
		t.Fatalf("model = %q", ev.ModelRaw)
	}
	if ev.InputTokens == nil || *ev.InputTokens != 150 {
		t.Fatalf("input_tokens = %+v", ev.InputTokens)
	}
	if ev.OutputTokens == nil || *ev.OutputTokens != 75 {
		t.Fatalf("output_tokens = %+v", ev.OutputTokens)
	}
	if ev.CostUSD == nil || *ev.CostUSD < 0.012 || *ev.CostUSD > 0.013 {
		t.Fatalf("cost = %+v", ev.CostUSD)
	}
}

// TestCollectTelemetryFromLegacyStorage_MissingDir returns no error when the
// legacy storage path does not exist (legacy storage is optional).
func TestCollectTelemetryFromLegacyStorage_MissingDir(t *testing.T) {
	events, err := CollectTelemetryFromLegacyStorage(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %d, want 0", len(events))
	}
}

// TestDiscoverOpenCodeChannelDBs_FindsExistingDB verifies multi-channel
// discovery returns only paths that actually exist on disk.
func TestDiscoverOpenCodeChannelDBs_FindsExistingDB(t *testing.T) {
	home := t.TempDir()
	stableDir := filepath.Join(home, ".local", "share", "opencode")
	if err := os.MkdirAll(stableDir, 0o755); err != nil {
		t.Fatalf("mkdir stable: %v", err)
	}
	stablePath := filepath.Join(stableDir, "opencode.db")
	if err := os.WriteFile(stablePath, []byte("sqlite-stub"), 0o600); err != nil {
		t.Fatalf("write stable db: %v", err)
	}

	got := discoverOpenCodeChannelDBs(home)
	if len(got) != 1 || got[0] != stablePath {
		t.Fatalf("discoverOpenCodeChannelDBs = %v, want [%s]", got, stablePath)
	}
}
