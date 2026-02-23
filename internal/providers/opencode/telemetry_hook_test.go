package opencode

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func TestParseTelemetryHookPayload_EventWrapperMessageUpdated(t *testing.T) {
	payload := []byte(`{"event":{"type":"message.updated","properties":{"info":{"id":"msg-1","sessionID":"sess-1","role":"assistant","parentID":"turn-1","modelID":"gpt-5-codex","providerID":"opencode","cost":0.012,"tokens":{"input":120,"output":40,"reasoning":5,"cache":{"read":10,"write":2}},"time":{"created":1771754400000,"completed":1771754405000},"path":{"cwd":"/tmp/work"}}}}}`)

	events, err := ParseTelemetryHookPayload(payload)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	ev := events[0]
	if ev.Channel != shared.TelemetryChannelHook {
		t.Fatalf("channel = %s, want %s", ev.Channel, shared.TelemetryChannelHook)
	}
	if ev.EventType != shared.TelemetryEventTypeMessageUsage {
		t.Fatalf("event_type = %s, want %s", ev.EventType, shared.TelemetryEventTypeMessageUsage)
	}
	if ev.MessageID != "msg-1" {
		t.Fatalf("message_id = %q, want msg-1", ev.MessageID)
	}
	if ev.TotalTokens == nil || *ev.TotalTokens != 177 {
		t.Fatalf("total_tokens = %+v, want 177", ev.TotalTokens)
	}
}

func TestParseTelemetryHookPayload_ToolExecuteAfterHook(t *testing.T) {
	payload := []byte(`{"hook":"tool.execute.after","timestamp":1771754406000,"input":{"tool":"shell","sessionID":"sess-1","callID":"tool-1","args":{"command":"echo hi"}},"output":{"title":"Shell"}}`)

	events, err := ParseTelemetryHookPayload(payload)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	ev := events[0]
	if ev.EventType != shared.TelemetryEventTypeToolUsage {
		t.Fatalf("event_type = %s, want %s", ev.EventType, shared.TelemetryEventTypeToolUsage)
	}
	if ev.ToolCallID != "tool-1" {
		t.Fatalf("tool_call_id = %q, want tool-1", ev.ToolCallID)
	}
	if ev.ToolName != "shell" {
		t.Fatalf("tool_name = %q, want shell", ev.ToolName)
	}
	if ev.Requests == nil || *ev.Requests != 1 {
		t.Fatalf("requests = %+v, want 1", ev.Requests)
	}
}

func TestParseTelemetryHookPayload_ChatMessageHook(t *testing.T) {
	payload := []byte(`{"hook":"chat.message","timestamp":"2026-02-22T10:00:00Z","input":{"sessionID":"sess-1","agent":"main","messageID":"turn-1","variant":"default","model":{"providerID":"openrouter","modelID":"anthropic/claude-sonnet-4.5"}},"output":{"message":{"id":"msg-2","sessionID":"sess-1","role":"assistant"},"usage":{"input_tokens":120,"output_tokens":40,"reasoning_tokens":5,"cache_read_tokens":10,"cache_write_tokens":2,"total_tokens":177,"cost_usd":0.012},"context":{"parts_total":3,"parts_by_type":{"text":2,"tool_result":1}},"parts_count":3}}`)

	events, err := ParseTelemetryHookPayload(payload)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	ev := events[0]
	if ev.EventType != shared.TelemetryEventTypeMessageUsage {
		t.Fatalf("event_type = %s, want %s", ev.EventType, shared.TelemetryEventTypeMessageUsage)
	}
	if ev.SessionID != "sess-1" {
		t.Fatalf("session_id = %q, want sess-1", ev.SessionID)
	}
	if ev.TurnID != "turn-1" {
		t.Fatalf("turn_id = %q, want turn-1", ev.TurnID)
	}
	if ev.MessageID != "msg-2" {
		t.Fatalf("message_id = %q, want msg-2", ev.MessageID)
	}
	if ev.ProviderID != "openrouter" {
		t.Fatalf("provider_id = %q, want openrouter", ev.ProviderID)
	}
	if ev.InputTokens == nil || *ev.InputTokens != 120 {
		t.Fatalf("input_tokens = %+v, want 120", ev.InputTokens)
	}
	if ev.OutputTokens == nil || *ev.OutputTokens != 40 {
		t.Fatalf("output_tokens = %+v, want 40", ev.OutputTokens)
	}
	if ev.TotalTokens == nil || *ev.TotalTokens != 177 {
		t.Fatalf("total_tokens = %+v, want 177", ev.TotalTokens)
	}
	if ev.CostUSD == nil || *ev.CostUSD != 0.012 {
		t.Fatalf("cost_usd = %+v, want 0.012", ev.CostUSD)
	}

	contextMap, ok := ev.Payload["context"].(map[string]any)
	if !ok {
		t.Fatalf("context payload missing")
	}
	if got, ok := contextMap["parts_total"]; !ok || got != int64(3) {
		t.Fatalf("context.parts_total = %#v, want 3", got)
	}
}

func TestParseTelemetryHookPayload_UnknownHookCreatesRawEnvelope(t *testing.T) {
	payload := []byte(`{"hook":"session.started","input":{},"output":{}}`)

	events, err := ParseTelemetryHookPayload(payload)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].EventType != shared.TelemetryEventTypeRawEnvelope {
		t.Fatalf("event_type = %s, want %s", events[0].EventType, shared.TelemetryEventTypeRawEnvelope)
	}
	if events[0].Payload["hook"] != "session.started" {
		t.Fatalf("payload.hook = %#v, want session.started", events[0].Payload["hook"])
	}
}

func TestParseTelemetryHookPayload_UnknownEventCreatesRawEnvelope(t *testing.T) {
	payload := []byte(`{"type":"session.updated","timestamp":1771754406000,"properties":{"sessionID":"sess-unknown-1"}}`)

	events, err := ParseTelemetryHookPayload(payload)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].EventType != shared.TelemetryEventTypeRawEnvelope {
		t.Fatalf("event_type = %s, want %s", events[0].EventType, shared.TelemetryEventTypeRawEnvelope)
	}
}

func TestParseTelemetryHookPayload_ChatMessageHook_PrefersOutputModel(t *testing.T) {
	payload := []byte(`{"hook":"chat.message","timestamp":"2026-02-22T10:00:00Z","input":{"sessionID":"sess-1","agent":"main","messageID":"turn-1","variant":"default","model":{"providerID":"openrouter","modelID":"anthropic/claude-sonnet-4.5"}},"output":{"message":{"id":"msg-2","sessionID":"sess-1","role":"assistant"},"model":{"providerID":"openrouter","modelID":"qwen/qwen3-coder-flash"},"usage":{"input_tokens":10,"output_tokens":5}}}`)

	events, err := ParseTelemetryHookPayload(payload)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	ev := events[0]
	if ev.ModelRaw != "qwen/qwen3-coder-flash" {
		t.Fatalf("model_raw = %q, want qwen/qwen3-coder-flash", ev.ModelRaw)
	}
	if ev.ProviderID != "openrouter" {
		t.Fatalf("provider_id = %q, want openrouter", ev.ProviderID)
	}
}

func TestParseTelemetryHookPayload_ChatMessageHook_DoesNotForceInputModelWhenOutputMissing(t *testing.T) {
	payload := []byte(`{"hook":"chat.message","timestamp":"2026-02-22T10:00:00Z","input":{"sessionID":"sess-1","agent":"main","messageID":"turn-1","variant":"default","model":{"providerID":"openrouter","modelID":"anthropic/claude-sonnet-4.5"}},"output":{"message":{"id":"msg-2","sessionID":"sess-1","role":"assistant"},"usage":{"input_tokens":10,"output_tokens":5}}}`)

	events, err := ParseTelemetryHookPayload(payload)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	ev := events[0]
	if ev.EventType != shared.TelemetryEventTypeMessageUsage {
		t.Fatalf("event_type = %s, want %s", ev.EventType, shared.TelemetryEventTypeMessageUsage)
	}
	if ev.ModelRaw != "" {
		t.Fatalf("model_raw = %q, want empty when output model is missing", ev.ModelRaw)
	}
	if ev.ProviderID != "openrouter" {
		t.Fatalf("provider_id = %q, want openrouter", ev.ProviderID)
	}
}
