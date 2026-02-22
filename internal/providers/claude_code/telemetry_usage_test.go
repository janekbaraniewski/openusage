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
