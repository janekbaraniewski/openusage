package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

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

func TestNormalizeCopilotTelemetryToolName_CopilotMCPPattern(t *testing.T) {
	tool, meta := normalizeCopilotTelemetryToolName("github_mcp_server_list_issues")
	if tool != "mcp__github__list_issues" {
		t.Fatalf("tool = %q, want mcp__github__list_issues", tool)
	}
	if got, _ := meta["mcp_server"].(string); got != "github" {
		t.Fatalf("meta.mcp_server = %q, want github", got)
	}
	if got, _ := meta["mcp_function"].(string); got != "list_issues" {
		t.Fatalf("meta.mcp_function = %q, want list_issues", got)
	}
}

func TestParseCopilotTelemetrySessionStore_Fallback(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "session-store.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	for _, stmt := range []string{
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			cwd TEXT,
			repository TEXT,
			branch TEXT,
			summary TEXT,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE turns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			turn_index INTEGER NOT NULL,
			user_message TEXT,
			assistant_response TEXT,
			timestamp TEXT
		)`,
		`CREATE TABLE session_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			tool_name TEXT,
			turn_index INTEGER,
			first_seen_at TEXT
		)`,
		`INSERT INTO sessions (id, cwd, repository) VALUES
			('sess-missing-jsonl', '/Users/test/openusage', 'janek/openusage'),
			('sess-has-jsonl', '/Users/test/other', 'janek/other')`,
		`INSERT INTO turns (session_id, turn_index, user_message, assistant_response, timestamp) VALUES
			('sess-missing-jsonl', 0, 'hello', 'world', '2026-03-01T10:00:00Z'),
			('sess-has-jsonl', 1, 'skip', 'skip', '2026-03-01T10:01:00Z')`,
		`INSERT INTO session_files (session_id, file_path, tool_name, turn_index, first_seen_at) VALUES
			('sess-missing-jsonl', 'internal/providers/copilot/telemetry.go', 'edit', 0, '2026-03-01T10:00:10Z'),
			('sess-has-jsonl', 'internal/providers/copilot/copilot.go', 'view', 1, '2026-03-01T10:01:10Z')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}

	events, err := parseCopilotTelemetrySessionStore(context.Background(), dbPath, map[string]bool{
		"sess-has-jsonl": true,
	})
	if err != nil {
		t.Fatalf("parseCopilotTelemetrySessionStore() error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2 (turn + session_file for fallback session)", len(events))
	}

	var hasMessageUsage bool
	var hasFileTool bool
	for _, ev := range events {
		if ev.SessionID != "sess-missing-jsonl" {
			t.Fatalf("unexpected session id %q, expected fallback-only session", ev.SessionID)
		}
		switch ev.EventType {
		case shared.TelemetryEventTypeMessageUsage:
			hasMessageUsage = true
			if ev.Requests == nil || *ev.Requests != 1 {
				t.Fatalf("message usage requests = %v, want 1", ev.Requests)
			}
			if got, _ := ev.Payload["session_store_fallback"].(bool); !got {
				t.Fatalf("missing payload.session_store_fallback=true")
			}
		case shared.TelemetryEventTypeToolUsage:
			hasFileTool = true
			if ev.ToolName != "edit" {
				t.Fatalf("tool fallback name = %q, want edit", ev.ToolName)
			}
			if got, _ := ev.Payload["file"].(string); got != "internal/providers/copilot/telemetry.go" {
				t.Fatalf("tool fallback payload.file = %q, want internal/providers/copilot/telemetry.go", got)
			}
		}
	}
	if !hasMessageUsage {
		t.Fatal("missing message usage fallback event")
	}
	if !hasFileTool {
		t.Fatal("missing session file fallback tool event")
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

func TestSyntheticMessageUsageFromTurnEnd(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session-synth-1"
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	events := []map[string]any{
		{
			"type":      "session.start",
			"timestamp": "2026-03-01T10:00:00Z",
			"data": map[string]any{
				"sessionId":      sessionID,
				"copilotVersion": "0.0.500",
				"startTime":      "2026-03-01T10:00:00Z",
				"selectedModel":  "claude-sonnet-4.5",
			},
		},
		{
			"type":      "assistant.turn_start",
			"timestamp": "2026-03-01T10:00:10Z",
			"data":      map[string]any{},
		},
		{
			"type":      "assistant.turn_end",
			"timestamp": "2026-03-01T10:00:20Z",
			"id":        "turn-end-1",
			"data":      map[string]any{},
		},
		{
			"type":      "assistant.turn_start",
			"timestamp": "2026-03-01T10:01:00Z",
			"data":      map[string]any{},
		},
		{
			"type":      "assistant.turn_end",
			"timestamp": "2026-03-01T10:01:30Z",
			"id":        "turn-end-2",
			"data":      map[string]any{},
		},
	}
	writeCopilotTelemetryEvents(t, eventsPath, events)

	result, err := parseCopilotTelemetrySessionFile(eventsPath, sessionID)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	var syntheticEvents []shared.TelemetryEvent
	for _, ev := range result {
		if ev.EventType == shared.TelemetryEventTypeMessageUsage {
			syntheticEvents = append(syntheticEvents, ev)
		}
	}

	if len(syntheticEvents) != 2 {
		t.Fatalf("expected 2 synthetic message_usage events, got %d", len(syntheticEvents))
	}

	for i, ev := range syntheticEvents {
		if ev.ModelRaw != "claude-sonnet-4.5" {
			t.Errorf("event %d: expected model claude-sonnet-4.5, got %s", i, ev.ModelRaw)
		}
		if ev.Requests == nil || *ev.Requests != 1 {
			t.Errorf("event %d: expected Requests=1", i)
		}
		if ev.InputTokens != nil {
			t.Errorf("event %d: expected InputTokens=nil for unenriched event", i)
		}
		syn, _ := ev.Payload["synthetic"].(bool)
		if !syn {
			t.Errorf("event %d: expected synthetic=true in payload", i)
		}
	}
}

func TestSyntheticMessageUsage_SuppressedByRealUsage(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session-synth-suppress"
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	events := []map[string]any{
		{
			"type":      "session.start",
			"timestamp": "2026-03-01T10:00:00Z",
			"data": map[string]any{
				"sessionId":      sessionID,
				"copilotVersion": "0.0.500",
				"startTime":      "2026-03-01T10:00:00Z",
				"selectedModel":  "gpt-4o",
			},
		},
		{
			"type":      "assistant.usage",
			"timestamp": "2026-03-01T10:00:15Z",
			"id":        "usage-1",
			"data": map[string]any{
				"model":        "gpt-4o",
				"inputTokens":  500,
				"outputTokens": 100,
			},
		},
		{
			"type":      "assistant.turn_end",
			"timestamp": "2026-03-01T10:00:20Z",
			"id":        "turn-end-1",
			"data":      map[string]any{},
		},
	}
	writeCopilotTelemetryEvents(t, eventsPath, events)

	result, err := parseCopilotTelemetrySessionFile(eventsPath, sessionID)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	var messageUsageEvents []shared.TelemetryEvent
	for _, ev := range result {
		if ev.EventType == shared.TelemetryEventTypeMessageUsage {
			messageUsageEvents = append(messageUsageEvents, ev)
		}
	}

	// Should have exactly 1 event from assistant.usage, not a synthetic one from turn_end
	if len(messageUsageEvents) != 1 {
		t.Fatalf("expected 1 message_usage event (from real assistant.usage), got %d", len(messageUsageEvents))
	}
	if messageUsageEvents[0].InputTokens == nil || *messageUsageEvents[0].InputTokens != 500 {
		t.Error("expected real assistant.usage event with InputTokens=500")
	}
}

func TestSelectedModelFromSessionStart(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session-model-seed"
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	events := []map[string]any{
		{
			"type":      "session.start",
			"timestamp": "2026-03-01T10:00:00Z",
			"data": map[string]any{
				"sessionId":      sessionID,
				"copilotVersion": "0.0.500",
				"startTime":      "2026-03-01T10:00:00Z",
				"selectedModel":  "o3-mini",
			},
		},
		{
			"type":      "assistant.turn_end",
			"timestamp": "2026-03-01T10:00:20Z",
			"id":        "turn-end-1",
			"data":      map[string]any{},
		},
	}
	writeCopilotTelemetryEvents(t, eventsPath, events)

	result, err := parseCopilotTelemetrySessionFile(eventsPath, sessionID)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	var found bool
	for _, ev := range result {
		if ev.EventType == shared.TelemetryEventTypeMessageUsage && ev.ModelRaw == "o3-mini" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected synthetic message_usage event with model=o3-mini from selectedModel")
	}
}

func TestParseCopilotLogTokenDeltas(t *testing.T) {
	tmpDir := t.TempDir()

	logContent := `2026-02-21T19:45:41.056Z [INFO] CompactionProcessor: Utilization 16.0% (20465/128000 tokens) below threshold 80%
2026-02-21T19:45:44.145Z [INFO] CompactionProcessor: Utilization 16.5% (21063/128000 tokens) below threshold 80%
2026-02-21T19:45:46.896Z [INFO] CompactionProcessor: Utilization 21.5% (27463/128000 tokens) below threshold 80%
2026-02-21T19:46:05.556Z [INFO] Some other log line
2026-02-21T19:46:20.897Z [INFO] CompactionProcessor: Utilization 21.6% (27708/128000 tokens) below threshold 80%
`
	if err := os.WriteFile(filepath.Join(tmpDir, "process-1.log"), []byte(logContent), 0o600); err != nil {
		t.Fatal(err)
	}

	deltas := parseCopilotLogTokenDeltas(tmpDir)
	if len(deltas) == 0 {
		t.Fatal("expected non-empty deltas")
	}

	// 4 observations → up to 3 deltas (only positive ones)
	// Delta 1: 21063 - 20465 = 598
	// Delta 2: 27463 - 21063 = 6400
	// Delta 3: 27708 - 27463 = 245
	expectedDeltas := []int64{598, 6400, 245}
	if len(deltas) != len(expectedDeltas) {
		t.Fatalf("expected %d deltas, got %d", len(expectedDeltas), len(deltas))
	}
	for i, d := range deltas {
		if d.Used != expectedDeltas[i] {
			t.Errorf("delta %d: expected %d, got %d", i, expectedDeltas[i], d.Used)
		}
	}
}

func TestEnrichSyntheticTokenEstimates(t *testing.T) {
	ts := shared.FlexParseTime("2026-02-21T19:45:44Z")

	events := []shared.TelemetryEvent{
		{
			EventType:  shared.TelemetryEventTypeMessageUsage,
			OccurredAt: ts,
			Payload:    map[string]any{"synthetic": true},
		},
		{
			// Real event — should not be modified
			EventType:   shared.TelemetryEventTypeMessageUsage,
			OccurredAt:  ts,
			InputTokens: shared.Int64Ptr(500),
			Payload:     map[string]any{},
		},
	}

	deltas := []logTokenDelta{
		{Timestamp: shared.FlexParseTime("2026-02-21T19:45:44.145Z"), Used: 598, Limit: 128000},
		{Timestamp: shared.FlexParseTime("2026-02-21T19:45:46.896Z"), Used: 6400, Limit: 128000},
	}

	enrichSyntheticTokenEstimates(events, deltas)

	if events[0].InputTokens == nil {
		t.Fatal("expected synthetic event to be enriched with InputTokens")
	}
	if *events[0].InputTokens != 598 {
		t.Errorf("expected InputTokens=598 (closest delta), got %d", *events[0].InputTokens)
	}
	est, _ := events[0].Payload["estimated_tokens"].(bool)
	if !est {
		t.Error("expected estimated_tokens=true in payload")
	}

	// Real event should be untouched
	if *events[1].InputTokens != 500 {
		t.Errorf("real event should still have InputTokens=500, got %d", *events[1].InputTokens)
	}
}
