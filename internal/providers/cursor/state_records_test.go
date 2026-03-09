package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadStateRecords(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatalf("create cursorDiskKV: %v", err)
	}

	usageJSON, err := json.Marshal(map[string]composerModelUsage{
		"claude-4.5-sonnet": {CostInCents: 123.0, Amount: 2},
	})
	if err != nil {
		t.Fatalf("marshal usage json: %v", err)
	}

	createdAt := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC).UnixMilli()
	composerValue := fmt.Sprintf(`{
		"usageData": %s,
		"createdAt": %d,
		"unifiedMode": "agent",
		"forceMode": "manual",
		"isAgentic": true,
		"totalLinesAdded": 10,
		"totalLinesRemoved": 2,
		"modelConfig": {"modelName": "claude-4.5-sonnet"},
		"newlyCreatedFiles": ["a.go"],
		"addedFiles": 3,
		"removedFiles": 1,
		"contextTokensUsed": 120,
		"contextTokenLimit": 1000,
		"filesChangedCount": 4,
		"subagentInfo": {"subagentTypeName": "research"},
		"status": "completed"
	}`, string(usageJSON), createdAt)
	if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:session-1", composerValue); err != nil {
		t.Fatalf("insert composerData: %v", err)
	}

	bubbleValue := `{
		"type": 2,
		"toolFormerData": {"name": "read_file_v2", "status": "completed"},
		"conversationId": "session-1",
		"tokenCount": {"inputTokens": 9, "outputTokens": 3},
		"model": "claude-4.5-sonnet"
	}`
	if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:bubble-1", bubbleValue); err != nil {
		t.Fatalf("insert bubbleId: %v", err)
	}

	composerRecords, err := loadComposerSessionRecords(context.Background(), db)
	if err != nil {
		t.Fatalf("loadComposerSessionRecords: %v", err)
	}
	if len(composerRecords) != 1 {
		t.Fatalf("composer records = %d, want 1", len(composerRecords))
	}
	record := composerRecords[0]
	if record.SessionID != "session-1" {
		t.Fatalf("session id = %q, want session-1", record.SessionID)
	}
	if record.OccurredAt.UnixMilli() != createdAt {
		t.Fatalf("occurredAt = %d, want %d", record.OccurredAt.UnixMilli(), createdAt)
	}
	if record.Mode != "agent" || record.ForceMode != "manual" {
		t.Fatalf("modes = %q/%q", record.Mode, record.ForceMode)
	}
	if record.IsAgentic == nil || !*record.IsAgentic {
		t.Fatalf("isAgentic = %#v, want true", record.IsAgentic)
	}
	if record.NewlyCreatedFiles != 1 || record.AddedFiles != 3 || record.RemovedFiles != 1 {
		t.Fatalf("file counts = %+v", record)
	}
	if record.ContextTokensUsed != 120 || record.ContextTokenLimit != 1000 {
		t.Fatalf("context usage = %.0f/%.0f", record.ContextTokensUsed, record.ContextTokenLimit)
	}

	bubbleRecords, err := loadBubbleRecords(context.Background(), db)
	if err != nil {
		t.Fatalf("loadBubbleRecords: %v", err)
	}
	if len(bubbleRecords) != 1 {
		t.Fatalf("bubble records = %d, want 1", len(bubbleRecords))
	}
	bubble := bubbleRecords[0]
	if bubble.BubbleID != "bubble-1" {
		t.Fatalf("bubble id = %q, want bubble-1", bubble.BubbleID)
	}
	if bubble.ToolName != "read_file_v2" || bubble.ToolStatus != "completed" {
		t.Fatalf("tool payload = %+v", bubble)
	}
	if bubble.SessionID != "session-1" || bubble.InputTokens != 9 || bubble.OutputTokens != 3 {
		t.Fatalf("bubble tokens/session = %+v", bubble)
	}

	timestamps := composerSessionTimestampMap(composerRecords)
	if ts, ok := timestamps["session-1"]; !ok || ts.UnixMilli() != createdAt {
		t.Fatalf("timestamp map = %+v, want session-1 => %d", timestamps, createdAt)
	}
}
