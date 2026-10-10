package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// claudeReingestFixture is a small Claude Code history shaped like the real
// thing: a streamed assistant message written as several lines that share one
// message/request id, a tool_use block, and a subagent transcript that repeats
// a parent message with different usage numbers (so two events in one
// collection cycle share a dedup key but disagree on their fields).
var claudeReingestFixture = map[string][]string{
	"proj-a/sess-1.jsonl": {
		`{"type":"user","sessionId":"sess-1","timestamp":"2026-08-10T10:00:00Z","cwd":"/work/proj-a","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","sessionId":"sess-1","timestamp":"2026-08-10T10:00:01Z","cwd":"/work/proj-a","requestId":"req_1","message":{"id":"msg_1","model":"claude-opus-4-6","usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":500,"cache_creation_input_tokens":20},"content":[{"type":"text","text":"Let me"}]}}`,
		`{"type":"assistant","sessionId":"sess-1","timestamp":"2026-08-10T10:00:02Z","cwd":"/work/proj-a","requestId":"req_1","message":{"id":"msg_1","model":"claude-opus-4-6","usage":{"input_tokens":10,"output_tokens":42,"cache_read_input_tokens":500,"cache_creation_input_tokens":20},"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"assistant","sessionId":"sess-1","timestamp":"2026-08-10T10:00:05Z","cwd":"/work/proj-a","requestId":"req_2","message":{"id":"msg_2","model":"claude-opus-4-6","usage":{"input_tokens":3,"output_tokens":77,"cache_read_input_tokens":900,"cache_creation_input_tokens":0},"content":[{"type":"tool_use","id":"toolu_2","name":"Read","input":{"file_path":"/work/proj-a/main.go"}}]}}`,
	},
	"proj-a/sess-1/subagents/agent-x.jsonl": {
		`{"type":"assistant","sessionId":"sess-1","timestamp":"2026-08-10T10:00:03Z","cwd":"/work/proj-a","requestId":"req_2","message":{"id":"msg_2","model":"claude-opus-4-6","usage":{"input_tokens":4,"output_tokens":80,"cache_read_input_tokens":900,"cache_creation_input_tokens":0},"content":[{"type":"text","text":"sub"}]}}`,
	},
	"proj-b/sess-2.jsonl": {
		`{"type":"assistant","sessionId":"sess-2","timestamp":"2026-07-01T08:00:00Z","cwd":"/work/proj-b","requestId":"req_3","message":{"id":"msg_3","model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":200,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"content":[{"type":"tool_use","id":"toolu_3","name":"mcp__github__search","input":{}}]}}`,
	},
}

// Issue #266 reports ~60k rows logged as updates every collection cycle,
// including weeks-old sessions, and attributes it to a merged field that
// differs on every re-ingest so the #339 no-op skip never fires. This pins the
// opposite: replaying an unchanged Claude Code history through the real parser,
// collector mapping and store must write nothing, however many times it runs.
// A fresh provider per cycle forces a full reparse, which is the worst case (a
// source whose per-file cache was lost re-emits every event).
func TestIngest_ReplayedClaudeCodeHistoryWritesNothing(t *testing.T) {
	projects := t.TempDir()
	for rel, lines := range claudeReingestFixture {
		path := filepath.Join(projects, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	cycle := func() int {
		t.Helper()
		collector := NewSourceCollector(claude_code.New(), shared.TelemetryCollectOptions{
			Paths: map[string]string{
				"projects_dir":     projects,
				"alt_projects_dir": filepath.Join(projects, "absent"),
			},
		}, "")
		reqs, err := collector.Collect(ctx)
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		for _, req := range reqs {
			if _, err := store.Ingest(ctx, req); err != nil {
				t.Fatalf("Ingest: %v", err)
			}
		}
		return len(reqs)
	}

	if n := cycle(); n == 0 {
		t.Fatal("fixture produced no telemetry events")
	}
	var rows int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&rows); err != nil {
		t.Fatalf("count events: %v", err)
	}

	before := totalChanges(t, store.db)
	for i := 0; i < 3; i++ {
		cycle()
	}
	if after := totalChanges(t, store.db); after != before {
		t.Errorf("replaying unchanged history across 3 cycles wrote %d row change(s) over %d stored events, want 0", after-before, rows)
	}
}
