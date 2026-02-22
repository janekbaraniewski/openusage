package opencode

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func TestParseTelemetryEventFile_ParsesMessageUpdatedAndToolEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	content := `{"type":"message.updated","properties":{"info":{"id":"msg-1","sessionID":"sess-1","role":"assistant","parentID":"turn-1","modelID":"gpt-5-codex","providerID":"zen","cost":0.012,"tokens":{"input":120,"output":40,"reasoning":5,"cache":{"read":10,"write":2}},"time":{"created":1771754400000,"completed":1771754405000},"path":{"cwd":"/tmp/work"}}}}
{"type":"tool.execute.after","payload":{"sessionID":"sess-1","messageID":"msg-1","toolCallID":"tool-1","toolName":"shell","timestamp":1771754406000}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write events file: %v", err)
	}

	events, err := ParseTelemetryEventFile(path)
	if err != nil {
		t.Fatalf("parse events file: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].EventType != shared.TelemetryEventTypeMessageUsage {
		t.Fatalf("first event_type = %s", events[0].EventType)
	}
	if events[0].TotalTokens == nil || *events[0].TotalTokens != 177 {
		t.Fatalf("total_tokens = %+v, want 177", events[0].TotalTokens)
	}
	if events[1].EventType != shared.TelemetryEventTypeToolUsage {
		t.Fatalf("second event_type = %s", events[1].EventType)
	}
	if events[1].ToolName != "shell" {
		t.Fatalf("tool_name = %q, want shell", events[1].ToolName)
	}
}

func TestCollectTelemetryFromSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT NOT NULL);`,
		`CREATE TABLE message (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL,
			data TEXT NOT NULL
		);`,
		`CREATE TABLE part (
			id TEXT PRIMARY KEY,
			message_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL,
			data TEXT NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec schema stmt: %v", err)
		}
	}

	if _, err := db.Exec(`INSERT INTO session (id, directory) VALUES (?, ?)`, "sess-1", "/tmp/work"); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	messageData := `{
		"role":"assistant",
		"time":{"created":1771754400000,"completed":1771754405000},
		"parentID":"turn-1",
		"modelID":"qwen/qwen3-coder-flash",
		"providerID":"openrouter",
		"agent":"zbysiu",
		"path":{"cwd":"/tmp/work"},
		"cost":0.012,
		"tokens":{"total":177,"input":120,"output":40,"reasoning":5,"cache":{"read":10,"write":2}}
	}`
	if _, err := db.Exec(`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)`,
		"msg-1", "sess-1", int64(1771754400000), int64(1771754405000), messageData,
	); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	toolCompleted := `{"type":"tool","callID":"tool-1","tool":"shell","state":{"status":"completed","time":{"start":1771754406000,"end":1771754407000}}}`
	if _, err := db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)`,
		"part-1", "msg-1", "sess-1", int64(1771754406000), int64(1771754407000), toolCompleted,
	); err != nil {
		t.Fatalf("insert completed tool part: %v", err)
	}

	toolRunning := `{"type":"tool","callID":"tool-running","tool":"read","state":{"status":"running","time":{"start":1771754408000}}}`
	if _, err := db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)`,
		"part-2", "msg-1", "sess-1", int64(1771754408000), int64(1771754409000), toolRunning,
	); err != nil {
		t.Fatalf("insert running tool part: %v", err)
	}

	events, err := CollectTelemetryFromSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("collect sqlite: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}

	var messageEvent shared.TelemetryEvent
	var toolEvent shared.TelemetryEvent
	for _, ev := range events {
		if ev.EventType == shared.TelemetryEventTypeMessageUsage {
			messageEvent = ev
		}
		if ev.EventType == shared.TelemetryEventTypeToolUsage {
			toolEvent = ev
		}
	}
	if messageEvent.EventType != shared.TelemetryEventTypeMessageUsage {
		t.Fatalf("message event missing: %+v", events)
	}
	if messageEvent.ProviderID != "openrouter" {
		t.Fatalf("message provider = %q, want openrouter", messageEvent.ProviderID)
	}
	if messageEvent.ModelRaw != "qwen/qwen3-coder-flash" {
		t.Fatalf("message model = %q, want qwen/qwen3-coder-flash", messageEvent.ModelRaw)
	}
	if messageEvent.TotalTokens == nil || *messageEvent.TotalTokens != 177 {
		t.Fatalf("message total tokens = %+v, want 177", messageEvent.TotalTokens)
	}
	if messageEvent.WorkspaceID != "work" {
		t.Fatalf("message workspace = %q, want work", messageEvent.WorkspaceID)
	}

	if toolEvent.EventType != shared.TelemetryEventTypeToolUsage {
		t.Fatalf("tool event missing: %+v", events)
	}
	if toolEvent.ToolCallID != "tool-1" {
		t.Fatalf("tool call id = %q, want tool-1", toolEvent.ToolCallID)
	}
	if toolEvent.ToolName != "shell" {
		t.Fatalf("tool name = %q, want shell", toolEvent.ToolName)
	}
	if toolEvent.Status != shared.TelemetryStatusOK {
		t.Fatalf("tool status = %q, want %q", toolEvent.Status, shared.TelemetryStatusOK)
	}
}
