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

func TestParseTelemetrySessionFile_UsesTurnIDAsMessageIDFallback(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions", "2026", "03", "05")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}

	path := filepath.Join(sessionsDir, "rollout-messageid.jsonl")
	content := `{"timestamp":"2026-03-05T10:00:00Z","type":"session_meta","payload":{"id":"sess-msgid"}}
{"timestamp":"2026-03-05T10:00:01Z","type":"turn_context","payload":{"model":"gpt-5-codex","turn_id":"turn-1"}}
{"timestamp":"2026-03-05T10:00:02Z","type":"event_msg","payload":{"type":"token_count","request_id":"req-1","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}}
{"timestamp":"2026-03-05T10:00:03Z","type":"event_msg","payload":{"type":"token_count","request_id":"req-1","info":{"total_token_usage":{"input_tokens":12,"output_tokens":7,"total_tokens":19}}}}
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
	if events[0].MessageID != "req-1" {
		t.Fatalf("first message_id = %q, want req-1", events[0].MessageID)
	}
	if events[1].MessageID != "req-1" {
		t.Fatalf("second message_id = %q, want req-1", events[1].MessageID)
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
	if ev.ProviderID != "codex" {
		t.Fatalf("provider_id = %q, want codex", ev.ProviderID)
	}
	if got, _ := ev.Payload["upstream_provider"].(string); got != "openai" {
		t.Fatalf("payload.upstream_provider = %q, want openai", got)
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

func TestParseTelemetrySessionFile_ParsesToolUsageAndPatchStats(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions", "2026", "03", "05")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}

	path := filepath.Join(sessionsDir, "rollout-tools.jsonl")
	content := `{"timestamp":"2026-03-05T19:00:00Z","type":"session_meta","payload":{"id":"sess-tools","cwd":"/Users/janekbaraniewski/Workspace/priv/openusage","source":"vscode","originator":"Codex Desktop","model_provider":"openai"}}
{"timestamp":"2026-03-05T19:00:01Z","type":"turn_context","payload":{"model":"gpt-5-codex","turn_id":"turn-abc"}}
{"timestamp":"2026-03-05T19:00:02Z","type":"response_item","payload":{"type":"function_call","name":"mcp__gopls__go_workspace","arguments":"{}","call_id":"call-mcp-1"}}
{"timestamp":"2026-03-05T19:00:03Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-mcp-1","output":"failed to list namespaces: exit code 255"}}
{"timestamp":"2026-03-05T19:00:04Z","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","call_id":"call-patch-1","input":"*** Begin Patch\n*** Update File: internal/providers/codex/telemetry_usage.go\n+added\n-removed\n*** End Patch\n"}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	events, err := ParseTelemetrySessionFile(path)
	if err != nil {
		t.Fatalf("parse telemetry file: %v", err)
	}

	var mcpEvent, patchEvent *shared.TelemetryEvent
	for i := range events {
		ev := &events[i]
		if ev.EventType != shared.TelemetryEventTypeToolUsage {
			continue
		}
		if ev.ToolName == "mcp__gopls__go_workspace" {
			mcpEvent = ev
		}
		if ev.ToolName == "apply_patch" {
			patchEvent = ev
		}
	}

	if mcpEvent == nil {
		t.Fatal("missing mcp tool usage event")
	}
	if mcpEvent.Status != shared.TelemetryStatusError {
		t.Fatalf("mcp status = %q, want error", mcpEvent.Status)
	}
	if mcpEvent.WorkspaceID != "openusage" {
		t.Fatalf("workspace_id = %q, want openusage", mcpEvent.WorkspaceID)
	}
	if mcpEvent.ProviderID != "codex" {
		t.Fatalf("provider_id = %q, want codex", mcpEvent.ProviderID)
	}
	if got, _ := mcpEvent.Payload["upstream_provider"].(string); got != "openai" {
		t.Fatalf("payload.upstream_provider = %q, want openai", got)
	}
	if got, _ := mcpEvent.Payload["client"].(string); got != "Desktop App" {
		t.Fatalf("payload.client = %q, want Desktop App", got)
	}
	if mcpEvent.TurnID != "turn-abc" {
		t.Fatalf("turn_id = %q, want turn-abc", mcpEvent.TurnID)
	}

	if patchEvent == nil {
		t.Fatal("missing apply_patch tool usage event")
	}
	if got, ok := patchEvent.Payload["lines_added"].(int); !ok || got != 1 {
		t.Fatalf("patch lines_added = %#v, want 1", patchEvent.Payload["lines_added"])
	}
	if got, ok := patchEvent.Payload["lines_removed"].(int); !ok || got != 1 {
		t.Fatalf("patch lines_removed = %#v, want 1", patchEvent.Payload["lines_removed"])
	}
	if got, ok := patchEvent.Payload["file"].(string); !ok || got == "" {
		t.Fatalf("patch payload file = %#v, want non-empty", patchEvent.Payload["file"])
	}
}

func TestParseTelemetryNotifyPayload_EmitsToolAndUsageEvents(t *testing.T) {
	payload := []byte(`{
		"type":"tool.execute.after",
		"timestamp":"2026-03-05T19:00:00Z",
		"session_id":"sess-hook-1",
		"turn_id":"turn-hook-1",
		"message_id":"msg-hook-1",
		"provider":"openai",
		"model":"gpt-5-codex",
		"tool_name":"mcp__gopls__go_workspace",
		"tool_call_id":"tool-hook-1",
		"tool_input":{"path":"internal/providers/codex/telemetry_usage.go"},
		"usage":{"input_tokens":12,"output_tokens":5,"total_tokens":17}
	}`)

	events, err := ParseTelemetryNotifyPayload(payload, shared.TelemetryCollectOptions{})
	if err != nil {
		t.Fatalf("parse notify payload: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}

	var toolEv, usageEv *shared.TelemetryEvent
	for i := range events {
		if events[i].EventType == shared.TelemetryEventTypeToolUsage {
			toolEv = &events[i]
		}
		if events[i].EventType == shared.TelemetryEventTypeMessageUsage {
			usageEv = &events[i]
		}
	}
	if toolEv == nil {
		t.Fatal("missing tool_usage event")
	}
	if usageEv == nil {
		t.Fatal("missing message_usage event")
	}
	if toolEv.ToolName != "mcp__gopls__go_workspace" {
		t.Fatalf("tool name = %q, want mcp__gopls__go_workspace", toolEv.ToolName)
	}
	if toolEv.ToolCallID != "tool-hook-1" {
		t.Fatalf("tool_call_id = %q, want tool-hook-1", toolEv.ToolCallID)
	}
	if toolEv.ProviderID != "codex" {
		t.Fatalf("provider_id = %q, want codex", toolEv.ProviderID)
	}
	if got, _ := toolEv.Payload["upstream_provider"].(string); got != "openai" {
		t.Fatalf("payload.upstream_provider = %q, want openai", got)
	}
	if got, _ := toolEv.Payload["file"].(string); got == "" {
		t.Fatalf("tool payload file = %#v, want non-empty", toolEv.Payload["file"])
	}
	if usageEv.TotalTokens == nil || *usageEv.TotalTokens != 17 {
		t.Fatalf("usage total_tokens = %#v, want 17", usageEv.TotalTokens)
	}
}
