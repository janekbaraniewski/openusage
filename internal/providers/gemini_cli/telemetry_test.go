package gemini_cli

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func TestParseGeminiTelemetrySessionFile_NormalizesMCPToolsAndDiffStats(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session-telemetry.json")

	writeJSON(t, sessionPath, map[string]any{
		"sessionId":   "sess-1",
		"startTime":   "2026-02-17T09:43:00Z",
		"lastUpdated": "2026-02-17T09:44:00Z",
		"messages": []map[string]any{
			{
				"id":        "msg-1",
				"type":      "gemini",
				"timestamp": "2026-02-17T09:43:20Z",
				"model":     "gemini-2.5-pro-preview",
				"tokens": map[string]any{
					"input":    90,
					"output":   20,
					"cached":   0,
					"thoughts": 5,
					"tool":     5,
					"total":    120,
				},
				"toolCalls": []map[string]any{
					{
						"id":                     "run_gcloud_command-1771321394695-e36bfb60d36d08",
						"name":                   "run_gcloud_command",
						"displayName":            "run_gcloud_command (gcp-gcloud MCP Server)",
						"description":            "Execute gcloud command via MCP",
						"status":                 "success",
						"timestamp":              "2026-02-17T09:43:24.124Z",
						"renderOutputAsMarkdown": true,
						"args": map[string]any{
							"command":   "gcloud compute instances list",
							"file_path": "infra/main.tf",
						},
						"resultDisplay": map[string]any{
							"filePath": "infra/main.tf",
							"diffStat": map[string]any{
								"model_added_lines":   4,
								"model_removed_lines": 1,
								"model_added_chars":   120,
								"model_removed_chars": 30,
								"user_added_lines":    2,
								"user_removed_lines":  0,
								"user_added_chars":    40,
								"user_removed_chars":  0,
							},
						},
					},
				},
			},
		},
	})

	events, err := parseGeminiTelemetrySessionFile(sessionPath)
	if err != nil {
		t.Fatalf("parseGeminiTelemetrySessionFile() error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}

	var toolEvent *shared.TelemetryEvent
	var usageEvent *shared.TelemetryEvent
	for i := range events {
		ev := &events[i]
		switch ev.EventType {
		case shared.TelemetryEventTypeToolUsage:
			toolEvent = ev
		case shared.TelemetryEventTypeMessageUsage:
			usageEvent = ev
		}
	}
	if toolEvent == nil {
		t.Fatal("missing tool usage event")
	}
	if usageEvent == nil {
		t.Fatal("missing message usage event")
	}

	if toolEvent.ToolName != "mcp__gcp-gcloud__run_gcloud_command" {
		t.Fatalf("tool_name = %q, want canonical MCP name", toolEvent.ToolName)
	}
	if toolEvent.ToolCallID != "run_gcloud_command-1771321394695-e36bfb60d36d08" {
		t.Fatalf("tool_call_id = %q, want upstream id", toolEvent.ToolCallID)
	}
	if toolEvent.MessageID != "sess-1:msg-1" {
		t.Fatalf("message_id = %q, want sess-1:msg-1", toolEvent.MessageID)
	}
	if toolEvent.Status != shared.TelemetryStatusOK {
		t.Fatalf("status = %q, want ok", toolEvent.Status)
	}

	wantToolTime, err := shared.ParseTimestampString("2026-02-17T09:43:24.124Z")
	if err != nil {
		t.Fatalf("parse want timestamp: %v", err)
	}
	if !toolEvent.OccurredAt.Equal(wantToolTime) {
		t.Fatalf("tool occurred_at = %s, want %s", toolEvent.OccurredAt.Format(time.RFC3339Nano), wantToolTime.Format(time.RFC3339Nano))
	}

	if got, _ := toolEvent.Payload["tool_type"].(string); got != "mcp" {
		t.Fatalf("payload.tool_type = %q, want mcp", got)
	}
	if got, _ := toolEvent.Payload["mcp_server"].(string); got != "gcp-gcloud" {
		t.Fatalf("payload.mcp_server = %q, want gcp-gcloud", got)
	}
	if got, _ := toolEvent.Payload["mcp_function"].(string); got != "run_gcloud_command" {
		t.Fatalf("payload.mcp_function = %q, want run_gcloud_command", got)
	}
	if got, _ := toolEvent.Payload["file"].(string); got != "infra/main.tf" {
		t.Fatalf("payload.file = %q, want infra/main.tf", got)
	}
	if got, ok := toolEvent.Payload["lines_added"].(int); !ok || got != 6 {
		t.Fatalf("payload.lines_added = %#v, want 6", toolEvent.Payload["lines_added"])
	}
	if got, ok := toolEvent.Payload["lines_removed"].(int); !ok || got != 1 {
		t.Fatalf("payload.lines_removed = %#v, want 1", toolEvent.Payload["lines_removed"])
	}

	if usageEvent.TotalTokens == nil || *usageEvent.TotalTokens != 120 {
		t.Fatalf("total_tokens = %+v, want 120", usageEvent.TotalTokens)
	}
	if got, _ := usageEvent.Payload["client"].(string); got != "CLI" {
		t.Fatalf("payload.client = %q, want CLI", got)
	}
}

func TestExtractGeminiMCPTool(t *testing.T) {
	server, function, ok := extractGeminiMCPTool("go_workspace (gopls MCP Server)", "go_workspace")
	if !ok {
		t.Fatal("expected MCP extraction to succeed")
	}
	if server != "gopls" || function != "go_workspace" {
		t.Fatalf("server/function = %q/%q, want gopls/go_workspace", server, function)
	}

	if _, _, ok := extractGeminiMCPTool("ReadFile", "read_file"); ok {
		t.Fatal("expected non-MCP display name to return ok=false")
	}
}
