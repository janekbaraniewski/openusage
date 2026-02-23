package claude_code

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func TestParseTelemetryConversationFile_DedupesByRequestIDAndExtractsToolEvents(t *testing.T) {
	projectsDir := filepath.Join(t.TempDir(), "projects", "repo-a")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("mkdir projects: %v", err)
	}

	path := filepath.Join(projectsDir, "session.jsonl")
	content := `{"type":"assistant","sessionId":"sess1","requestId":"req-1","timestamp":"2026-02-22T10:00:00Z","message":{"id":"msg-1","model":"claude-opus-4-6","role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"Read"}],"usage":{"input_tokens":100,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40,"reasoning_tokens":5}}}
{"type":"assistant","sessionId":"sess1","requestId":"req-1","timestamp":"2026-02-22T10:00:01Z","message":{"id":"msg-1b","model":"claude-opus-4-6","role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"Read"}],"usage":{"input_tokens":100,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40,"reasoning_tokens":5}}}
{"type":"assistant","sessionId":"sess1","requestId":"req-2","timestamp":"2026-02-22T10:00:02Z","message":{"id":"msg-2","model":"claude-opus-4-6","role":"assistant","content":[{"type":"tool_use","id":"tool-2","name":"Bash"}],"usage":{"input_tokens":10,"output_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"reasoning_tokens":0}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	events, err := ParseTelemetryConversationFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	messageCount := 0
	toolCount := 0
	for _, ev := range events {
		switch ev.EventType {
		case shared.TelemetryEventTypeMessageUsage:
			messageCount++
		case shared.TelemetryEventTypeToolUsage:
			toolCount++
		}
	}
	if messageCount != 2 {
		t.Fatalf("message events = %d, want 2", messageCount)
	}
	if toolCount != 2 {
		t.Fatalf("tool events = %d, want 2", toolCount)
	}
}

func TestParseTelemetryHookPayload_ParsesUsageHook(t *testing.T) {
	payload := []byte(`{
		"hook_event_name":"Stop",
		"session_id":"sess-1",
		"request_id":"req-1",
		"cwd":"/tmp/repo",
		"model":"claude-opus-4-6",
		"usage":{"input_tokens":100,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40,"reasoning_tokens":5}
	}`)

	events, err := ParseTelemetryHookPayload(payload, shared.TelemetryCollectOptions{})
	if err != nil {
		t.Fatalf("parse hook payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	ev := events[0]
	if ev.EventType != shared.TelemetryEventTypeMessageUsage {
		t.Fatalf("event_type = %q, want message_usage", ev.EventType)
	}
	if ev.ProviderID != "anthropic" {
		t.Fatalf("provider_id = %q, want anthropic", ev.ProviderID)
	}
	if ev.TotalTokens == nil || *ev.TotalTokens != 195 {
		t.Fatalf("total_tokens = %+v, want 195", ev.TotalTokens)
	}
	if ev.WorkspaceID != "repo" {
		t.Fatalf("workspace_id = %q, want repo", ev.WorkspaceID)
	}
}

func TestParseTelemetryHookPayload_ParsesToolEvent(t *testing.T) {
	payload := []byte(`{
		"hook_event_name":"PostToolUse",
		"session_id":"sess-1",
		"request_id":"req-1",
		"tool_name":"Bash",
		"tool_call_id":"tool-1"
	}`)

	events, err := ParseTelemetryHookPayload(payload, shared.TelemetryCollectOptions{})
	if err != nil {
		t.Fatalf("parse hook payload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	ev := events[0]
	if ev.EventType != shared.TelemetryEventTypeToolUsage {
		t.Fatalf("event_type = %q, want tool_usage", ev.EventType)
	}
	if ev.ToolName != "bash" {
		t.Fatalf("tool_name = %q, want bash", ev.ToolName)
	}
	if ev.ToolCallID != "tool-1" {
		t.Fatalf("tool_call_id = %q, want tool-1", ev.ToolCallID)
	}
}
