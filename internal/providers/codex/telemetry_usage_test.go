package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func TestParseTelemetrySessionFile_CollectsTokenDeltas(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions", "2026", "02", "22")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}

	path := filepath.Join(sessionsDir, "rollout-test.jsonl")
	content := `{"timestamp":"2026-02-22T10:00:00Z","type":"session_meta","payload":{"id":"sess-1"}}
{"timestamp":"2026-02-22T10:00:01Z","type":"turn_context","payload":{"model":"gpt-5-codex"}}
{"timestamp":"2026-02-22T10:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":60,"cached_input_tokens":20,"output_tokens":20,"reasoning_output_tokens":0,"total_tokens":100}}}}
{"timestamp":"2026-02-22T10:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":30,"output_tokens":50,"reasoning_output_tokens":0,"total_tokens":180}}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	events, err := ParseTelemetrySessionFile(path)
	if err != nil {
		t.Fatalf("parse telemetry file: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].EventType != shared.TelemetryEventTypeMessageUsage {
		t.Fatalf("event type = %q", events[0].EventType)
	}
	if events[0].TotalTokens == nil || *events[0].TotalTokens != 100 {
		t.Fatalf("first total tokens = %+v, want 100", events[0].TotalTokens)
	}
	if events[1].TotalTokens == nil || *events[1].TotalTokens != 80 {
		t.Fatalf("second total tokens = %+v, want 80", events[1].TotalTokens)
	}
	if events[1].ModelRaw != "gpt-5-codex" {
		t.Fatalf("model_raw = %q, want gpt-5-codex", events[1].ModelRaw)
	}
}

func TestParseTelemetryNotifyPayload_ParsesUsagePayload(t *testing.T) {
	payload := []byte(`{
		"type":"agent-turn-complete",
		"timestamp":"2026-02-22T10:00:00Z",
		"session_id":"sess-1",
		"turn_id":"turn-1",
		"message_id":"msg-1",
		"model":"gpt-5-codex",
		"provider":"openai",
		"usage":{"input_tokens":120,"output_tokens":40,"reasoning_output_tokens":5,"cached_input_tokens":10,"total_tokens":175}
	}`)

	events, err := ParseTelemetryNotifyPayload(payload, shared.TelemetryCollectOptions{})
	if err != nil {
		t.Fatalf("parse notify payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	ev := events[0]
	if ev.EventType != shared.TelemetryEventTypeMessageUsage {
		t.Fatalf("event_type = %q, want message_usage", ev.EventType)
	}
	if ev.ProviderID != "openai" {
		t.Fatalf("provider_id = %q, want openai", ev.ProviderID)
	}
	if ev.ModelRaw != "gpt-5-codex" {
		t.Fatalf("model_raw = %q, want gpt-5-codex", ev.ModelRaw)
	}
	if ev.TotalTokens == nil || *ev.TotalTokens != 175 {
		t.Fatalf("total_tokens = %+v, want 175", ev.TotalTokens)
	}
}

func TestParseTelemetryNotifyPayload_FallsBackToTurnCompleted(t *testing.T) {
	payload := []byte(`{
		"type":"agent-turn-complete",
		"timestamp":"2026-02-22T10:00:00Z",
		"session_id":"sess-1",
		"turn_id":"turn-1",
		"message_id":"msg-1",
		"model":"gpt-5-codex"
	}`)

	events, err := ParseTelemetryNotifyPayload(payload, shared.TelemetryCollectOptions{})
	if err != nil {
		t.Fatalf("parse notify payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].EventType != shared.TelemetryEventTypeTurnCompleted {
		t.Fatalf("event_type = %q, want turn_completed", events[0].EventType)
	}
}
