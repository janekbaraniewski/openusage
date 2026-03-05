package copilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func TestParseCopilotTelemetrySessionFile_ToolLifecycleAndMCP(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session-telemetry-1"
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	events := []map[string]any{
		{
			"type":      "session.start",
			"timestamp": "2026-03-01T10:00:00Z",
			"data": map[string]any{
				"sessionId":      sessionID,
				"copilotVersion": "0.0.500",
				"startTime":      "2026-03-01T10:00:00Z",
				"context": map[string]any{
					"cwd":        "/Users/test/openusage",
					"repository": "janekbaraniewski/openusage",
					"branch":     "main",
				},
			},
		},
		{
			"type":      "session.model_change",
			"timestamp": "2026-03-01T10:00:01Z",
			"data": map[string]any{
				"newModel": "claude-sonnet-4.5",
			},
		},
		{
			"type":      "assistant.message",
			"timestamp": "2026-03-01T10:00:02Z",
			"id":        "assistant-msg-1",
			"data": map[string]any{
				"messageId": "msg-1",
				"toolRequests": []map[string]any{
					{
						"toolCallId": "call-mcp-1",
						"name":       "mcp-kubernetes-user-kubernetes-pods_list",
						"arguments": map[string]any{
							"path": "internal/providers/copilot/copilot.go",
						},
					},
					{
						"toolCallId": "call-edit-1",
						"name":       "edit",
						"arguments": map[string]any{
							"filePath":   "internal/providers/copilot/telemetry.go",
							"old_string": "a\nb",
							"new_string": "a\nb\nc",
							"command":    "git commit -m \"copilot telemetry\"",
						},
					},
				},
			},
		},
		{
			"type":      "tool.execution_complete",
			"timestamp": "2026-03-01T10:00:03Z",
			"data": map[string]any{
				"toolCallId": "call-mcp-1",
				"success":    false,
				"error": map[string]any{
					"code":    "denied",
					"message": "user rejected this tool call",
				},
			},
		},
		{
			"type":      "tool.execution_complete",
			"timestamp": "2026-03-01T10:00:04Z",
			"data": map[string]any{
				"toolCallId": "call-edit-1",
				"success":    true,
				"result": map[string]any{
					"content": "ok",
				},
			},
		},
		{
			"type":      "session.workspace_file_changed",
			"timestamp": "2026-03-01T10:00:05Z",
			"data": map[string]any{
				"path":      "docs/NEW.md",
				"operation": "create",
			},
		},
	}
	writeCopilotTelemetryEvents(t, eventsPath, events)

	out, err := parseCopilotTelemetrySessionFile(eventsPath, sessionID)
	if err != nil {
		t.Fatalf("parseCopilotTelemetrySessionFile() error: %v", err)
	}

	mcpEvent, ok := findToolEventByCallIDAndStatus(out, "call-mcp-1", shared.TelemetryStatusAborted)
	if !ok {
		t.Fatal("missing MCP tool completion event with aborted status")
	}
	if mcpEvent.ToolName != "mcp__kubernetes__pods_list" {
		t.Fatalf("mcp tool name = %q, want canonical mcp__kubernetes__pods_list", mcpEvent.ToolName)
	}
	if got, _ := mcpEvent.Payload["mcp_server"].(string); got != "kubernetes" {
		t.Fatalf("payload.mcp_server = %q, want kubernetes", got)
	}
	if got, _ := mcpEvent.Payload["mcp_function"].(string); got != "pods_list" {
		t.Fatalf("payload.mcp_function = %q, want pods_list", got)
	}
	if got, _ := mcpEvent.Payload["client"].(string); got != "janekbaraniewski/openusage" {
		t.Fatalf("payload.client = %q, want janekbaraniewski/openusage", got)
	}
	if got, _ := mcpEvent.Payload["file"].(string); got != "internal/providers/copilot/copilot.go" {
		t.Fatalf("payload.file = %q, want internal/providers/copilot/copilot.go", got)
	}

	editEvent, ok := findToolEventByCallIDAndStatus(out, "call-edit-1", shared.TelemetryStatusOK)
	if !ok {
		t.Fatal("missing edit tool completion event with ok status")
	}
	if editEvent.ToolName != "edit" {
		t.Fatalf("edit tool name = %q, want edit", editEvent.ToolName)
	}
	if got, _ := editEvent.Payload["command"].(string); got == "" {
		t.Fatal("payload.command should be populated from tool args")
	}
	if got, ok := editEvent.Payload["lines_added"].(int); !ok || got != 3 {
		t.Fatalf("payload.lines_added = %#v, want 3", editEvent.Payload["lines_added"])
	}
	if got, ok := editEvent.Payload["lines_removed"].(int); !ok || got != 2 {
		t.Fatalf("payload.lines_removed = %#v, want 2", editEvent.Payload["lines_removed"])
	}

	workspaceEvent, ok := findToolEventByName(out, "workspace_file_create")
	if !ok {
		t.Fatal("missing session.workspace_file_changed tool_usage event")
	}
	if workspaceEvent.Requests == nil || *workspaceEvent.Requests != 0 {
		t.Fatalf("workspace file event requests = %v, want 0", workspaceEvent.Requests)
	}
	if got, _ := workspaceEvent.Payload["file"].(string); got != "docs/NEW.md" {
		t.Fatalf("workspace payload.file = %q, want docs/NEW.md", got)
	}
}

func TestParseCopilotTelemetrySessionFile_AssistantUsageFallbackModel(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session-telemetry-usage"
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	events := []map[string]any{
		{
			"type":      "session.start",
			"timestamp": "2026-03-01T11:00:00Z",
			"data": map[string]any{
				"sessionId": sessionID,
				"context": map[string]any{
					"cwd":        "/Users/test/openusage",
					"repository": "janekbaraniewski/openusage",
				},
			},
		},
		{
			"type":      "session.model_change",
			"timestamp": "2026-03-01T11:00:01Z",
			"data": map[string]any{
				"newModel": "claude-sonnet-4.5",
			},
		},
		{
			"type":      "assistant.usage",
			"id":        "usage-evt-1",
			"timestamp": "2026-03-01T11:00:02Z",
			"data": map[string]any{
				"model":            "",
				"inputTokens":      120,
				"outputTokens":     30,
				"cacheReadTokens":  10,
				"cacheWriteTokens": 2,
				"cost":             1.25,
				"duration":         3250,
			},
		},
	}
	writeCopilotTelemetryEvents(t, eventsPath, events)

	out, err := parseCopilotTelemetrySessionFile(eventsPath, sessionID)
	if err != nil {
		t.Fatalf("parseCopilotTelemetrySessionFile() error: %v", err)
	}

	var usageEvent *shared.TelemetryEvent
	for i := range out {
		if out[i].EventType == shared.TelemetryEventTypeMessageUsage {
			usageEvent = &out[i]
			break
		}
	}
	if usageEvent == nil {
		t.Fatal("missing message_usage event")
	}
	if usageEvent.ModelRaw != "claude-sonnet-4.5" {
		t.Fatalf("model_raw = %q, want claude-sonnet-4.5", usageEvent.ModelRaw)
	}
	if usageEvent.TotalTokens == nil || *usageEvent.TotalTokens != 150 {
		t.Fatalf("total_tokens = %v, want 150", usageEvent.TotalTokens)
	}
	if usageEvent.CacheReadTokens == nil || *usageEvent.CacheReadTokens != 10 {
		t.Fatalf("cache_read_tokens = %v, want 10", usageEvent.CacheReadTokens)
	}
	if usageEvent.CacheWriteTokens == nil || *usageEvent.CacheWriteTokens != 2 {
		t.Fatalf("cache_write_tokens = %v, want 2", usageEvent.CacheWriteTokens)
	}
	if usageEvent.CostUSD == nil || *usageEvent.CostUSD != 1.25 {
		t.Fatalf("cost_usd = %v, want 1.25", usageEvent.CostUSD)
	}
	if got, _ := usageEvent.Payload["client"].(string); got != "janekbaraniewski/openusage" {
		t.Fatalf("payload.client = %q, want janekbaraniewski/openusage", got)
	}
}

func TestParseCopilotTelemetrySessionFile_ShutdownFallbackUsage(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session-telemetry-shutdown"
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	events := []map[string]any{
		{
			"type":      "session.start",
			"timestamp": "2026-03-01T12:00:00Z",
			"data": map[string]any{
				"sessionId": sessionID,
				"context": map[string]any{
					"cwd":        "/Users/test/openusage",
					"repository": "janekbaraniewski/openusage",
				},
			},
		},
		{
			"type":      "session.shutdown",
			"id":        "shutdown-evt-1",
			"timestamp": "2026-03-01T12:05:00Z",
			"data": map[string]any{
				"shutdownType":         "normal",
				"totalPremiumRequests": 4,
				"totalApiDurationMs":   9000,
				"sessionStartTime":     "2026-03-01T12:00:00Z",
				"codeChanges": map[string]any{
					"linesAdded":    12,
					"linesRemoved":  3,
					"filesModified": 2,
				},
				"modelMetrics": map[string]any{
					"gpt-5": map[string]any{
						"requests": map[string]any{
							"count": 4,
							"cost":  0.88,
						},
						"usage": map[string]any{
							"inputTokens":      100,
							"outputTokens":     40,
							"cacheReadTokens":  10,
							"cacheWriteTokens": 2,
						},
					},
				},
			},
		},
	}
	writeCopilotTelemetryEvents(t, eventsPath, events)

	out, err := parseCopilotTelemetrySessionFile(eventsPath, sessionID)
	if err != nil {
		t.Fatalf("parseCopilotTelemetrySessionFile() error: %v", err)
	}

	var turnCompleted *shared.TelemetryEvent
	var usageEvent *shared.TelemetryEvent
	for i := range out {
		switch out[i].EventType {
		case shared.TelemetryEventTypeTurnCompleted:
			turnCompleted = &out[i]
		case shared.TelemetryEventTypeMessageUsage:
			if strings.Contains(out[i].MessageID, "shutdown:gpt_5") {
				usageEvent = &out[i]
			}
		}
	}

	if turnCompleted == nil {
		t.Fatal("missing turn_completed event from session.shutdown")
	}
	if got, ok := turnCompleted.Payload["lines_added"].(int); !ok || got != 12 {
		t.Fatalf("turn_completed lines_added = %#v, want 12", turnCompleted.Payload["lines_added"])
	}
	if got, ok := turnCompleted.Payload["lines_removed"].(int); !ok || got != 3 {
		t.Fatalf("turn_completed lines_removed = %#v, want 3", turnCompleted.Payload["lines_removed"])
	}

	if usageEvent == nil {
		t.Fatal("missing shutdown fallback message_usage event")
	}
	if usageEvent.ModelRaw != "gpt-5" {
		t.Fatalf("fallback model_raw = %q, want gpt-5", usageEvent.ModelRaw)
	}
	if usageEvent.TotalTokens == nil || *usageEvent.TotalTokens != 140 {
		t.Fatalf("fallback total_tokens = %v, want 140", usageEvent.TotalTokens)
	}
	if usageEvent.Requests == nil || *usageEvent.Requests != 4 {
		t.Fatalf("fallback requests = %v, want 4", usageEvent.Requests)
	}
	if usageEvent.CostUSD == nil || *usageEvent.CostUSD != 0.88 {
		t.Fatalf("fallback cost = %v, want 0.88", usageEvent.CostUSD)
	}
	if got, ok := usageEvent.Payload["lines_added"].(int); !ok || got != 12 {
		t.Fatalf("fallback payload.lines_added = %#v, want 12", usageEvent.Payload["lines_added"])
	}
}

func TestParseCopilotTelemetrySessionFile_ShutdownDoesNotDuplicateWhenUsageExists(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session-telemetry-no-duplicate"
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	events := []map[string]any{
		{
			"type":      "session.start",
			"timestamp": "2026-03-01T13:00:00Z",
			"data": map[string]any{
				"sessionId": sessionID,
			},
		},
		{
			"type":      "assistant.usage",
			"timestamp": "2026-03-01T13:00:01Z",
			"data": map[string]any{
				"model":        "gpt-5",
				"inputTokens":  50,
				"outputTokens": 10,
			},
		},
		{
			"type":      "session.shutdown",
			"timestamp": "2026-03-01T13:00:02Z",
			"data": map[string]any{
				"codeChanges": map[string]any{
					"linesAdded":    2,
					"linesRemoved":  1,
					"filesModified": 1,
				},
				"modelMetrics": map[string]any{
					"gpt-5": map[string]any{
						"requests": map[string]any{"count": 2, "cost": 0.2},
						"usage":    map[string]any{"inputTokens": 20, "outputTokens": 5},
					},
				},
			},
		},
	}
	writeCopilotTelemetryEvents(t, eventsPath, events)

	out, err := parseCopilotTelemetrySessionFile(eventsPath, sessionID)
	if err != nil {
		t.Fatalf("parseCopilotTelemetrySessionFile() error: %v", err)
	}

	messageUsageCount := 0
	for _, ev := range out {
		if ev.EventType == shared.TelemetryEventTypeMessageUsage {
			messageUsageCount++
		}
	}
	if messageUsageCount != 1 {
		t.Fatalf("message usage count = %d, want 1 (assistant.usage only)", messageUsageCount)
	}
}

func writeCopilotTelemetryEvents(t *testing.T, path string, events []map[string]any) {
	t.Helper()

	lines := make([]string, 0, len(events))
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		lines = append(lines, string(raw))
	}

	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func findToolEventByCallIDAndStatus(events []shared.TelemetryEvent, callID string, status shared.TelemetryStatus) (shared.TelemetryEvent, bool) {
	for _, ev := range events {
		if ev.EventType == shared.TelemetryEventTypeToolUsage &&
			ev.ToolCallID == callID &&
			ev.Status == status {
			return ev, true
		}
	}
	return shared.TelemetryEvent{}, false
}

func findToolEventByName(events []shared.TelemetryEvent, toolName string) (shared.TelemetryEvent, bool) {
	for _, ev := range events {
		if ev.EventType == shared.TelemetryEventTypeToolUsage &&
			ev.ToolName == toolName {
			return ev, true
		}
	}
	return shared.TelemetryEvent{}, false
}
