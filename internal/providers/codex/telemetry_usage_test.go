package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func TestCollectSkipsUnchangedSessionFiles(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions", "2026", "07", "17")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}

	path := filepath.Join(sessionsDir, "rollout-unchanged.jsonl")
	content := `{"timestamp":"2026-07-17T10:00:00Z","type":"session_meta","payload":{"id":"sess-unchanged"}}
{"timestamp":"2026-07-17T10:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	provider := New()
	opts := shared.TelemetryCollectOptions{Paths: map[string]string{"sessions_dir": sessionsDir}}
	first, err := provider.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Collect() error: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first Collect() events = %d, want 1", len(first))
	}

	second, err := provider.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Collect() error: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second Collect() events = %d, want 0 for unchanged files", len(second))
	}

	touchedTime := time.Now().Add(time.Second)
	if err := os.Chtimes(path, touchedTime, touchedTime); err != nil {
		t.Fatalf("touch unchanged session: %v", err)
	}
	touched, err := provider.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("Collect() after mtime-only change error: %v", err)
	}
	if len(touched) != 0 {
		t.Fatalf("Collect() after mtime-only change events = %d, want 0", len(touched))
	}

	appended := `{"timestamp":"2026-07-17T10:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":15,"output_tokens":10,"total_tokens":25}}}}
`
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open session for append: %v", err)
	}
	if _, err := f.WriteString(appended); err != nil {
		_ = f.Close()
		t.Fatalf("append session: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}

	third, err := provider.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("third Collect() error: %v", err)
	}
	if len(third) != 1 {
		t.Fatalf("third Collect() events = %d, want only the appended event", len(third))
	}
	if got := *third[0].TokenUsage.TotalTokens; got != 10 {
		t.Fatalf("third Collect() total_tokens = %d, want appended delta 10", got)
	}
}

func TestCollectBaselinesOldSessionFilesWhenEnabled(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions", "2026", "07", "17")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}

	oldPath := filepath.Join(sessionsDir, "rollout-old.jsonl")
	recentPath := filepath.Join(sessionsDir, "rollout-recent.jsonl")
	content := `{"timestamp":"2026-07-17T10:00:00Z","type":"session_meta","payload":{"id":"sess-baseline"}}
{"timestamp":"2026-07-17T10:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}}
`
	if err := os.WriteFile(oldPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write old session file: %v", err)
	}
	if err := os.WriteFile(recentPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write recent session file: %v", err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old session file: %v", err)
	}

	provider := New()
	opts := shared.TelemetryCollectOptions{Paths: map[string]string{
		"sessions_dir":           sessionsDir,
		"baseline_existing":      "true",
		"baseline_recent_window": "30m",
	}}
	first, err := provider.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Collect() error: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first Collect() events = %d, want only recent file event", len(first))
	}
	if got := first[0].Payload["source_file"]; got != recentPath {
		t.Fatalf("source_file = %#v, want %q", got, recentPath)
	}

	second, err := provider.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Collect() error: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second Collect() events = %d, want 0 after baseline", len(second))
	}

	appended := `{"timestamp":"2026-07-17T10:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":15,"output_tokens":10,"total_tokens":25}}}}
`
	f, err := os.OpenFile(oldPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open old session for append: %v", err)
	}
	if _, err := f.WriteString(appended); err != nil {
		_ = f.Close()
		t.Fatalf("append old session: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close old session: %v", err)
	}

	third, err := provider.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("third Collect() error: %v", err)
	}
	if len(third) != 1 {
		t.Fatalf("third Collect() events = %d, want only the appended baseline event", len(third))
	}
	if got := *third[0].TokenUsage.TotalTokens; got != 10 {
		t.Fatalf("third Collect() total_tokens = %d, want appended delta 10", got)
	}
}

func TestCollectBaselinesOnlyFilesPresentOnInitialScan(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions", "2026", "07", "17")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	content := `{"timestamp":"2026-07-17T10:00:00Z","type":"session_meta","payload":{"id":"sess-baseline-once"}}
{"timestamp":"2026-07-17T10:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}}
`
	existingPath := filepath.Join(sessionsDir, "rollout-existing.jsonl")
	if err := os.WriteFile(existingPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write existing session: %v", err)
	}
	// A live writer can update mtime after Collect captures its cutoff but
	// before the directory walk reaches this file. A zero window means all
	// files present on the initial scan must still be baselined.
	futureTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(existingPath, futureTime, futureTime); err != nil {
		t.Fatalf("set future mtime: %v", err)
	}

	provider := New()
	opts := shared.TelemetryCollectOptions{Paths: map[string]string{
		"sessions_dir":           sessionsDir,
		"baseline_existing":      "true",
		"baseline_recent_window": "0s",
	}}
	first, err := provider.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Collect() error: %v", err)
	}
	if len(first) != 0 {
		t.Fatalf("first Collect() events = %d, want existing history baselined", len(first))
	}

	newPath := filepath.Join(sessionsDir, "rollout-new.jsonl")
	if err := os.WriteFile(newPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write new session: %v", err)
	}
	second, err := provider.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Collect() error: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("second Collect() events = %d, want new session collected", len(second))
	}
}

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
