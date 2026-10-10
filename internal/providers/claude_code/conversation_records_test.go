package claude_code

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// streamedLine builds one line of a streamed assistant message the way Claude
// Code writes it: one content block per line, and every line repeating the
// message's usage, with input and cache counts fixed and output_tokens growing
// to the final total on the last line.
func streamedLine(ts time.Time, req, msg string, output int, block string) string {
	return fmt.Sprintf(`{"type":"assistant","sessionId":"sess-1","timestamp":%q,"cwd":"/work/proj","requestId":%q,"message":{"id":%q,"model":"claude-opus-4-6","role":"assistant","usage":{"input_tokens":10,"output_tokens":%d,"cache_read_input_tokens":500,"cache_creation_input_tokens":20},"content":[%s]}}`,
		ts.UTC().Format(time.RFC3339Nano), req, msg, output, block)
}

const (
	textBlock     = `{"type":"text","text":"Let me look."}`
	thinkingBlock = `{"type":"thinking","thinking":"hmm"}`
)

func toolBlock(id, name string) string {
	return fmt.Sprintf(`{"type":"tool_use","id":%q,"name":%q,"input":{"file_path":"/work/proj/main.go"}}`, id, name)
}

func writeJSONL(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func toolNames(content []jsonlContent) []string {
	var names []string
	for _, c := range content {
		if c.Type == "tool_use" {
			names = append(names, strings.ToLower(c.Name))
		}
	}
	return names
}

var base = time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)

var streamedMessageCases = []struct {
	name       string
	lines      []string
	wantMsgs   int
	wantTools  []string // sorted
	wantOutput int      // summed over merged messages
	wantInput  int      // summed over merged messages
}{
	{
		name: "text then tool_use on a later line",
		lines: []string{
			streamedLine(base, "req_1", "msg_1", 1, textBlock),
			streamedLine(base.Add(time.Second), "req_1", "msg_1", 42, toolBlock("toolu_1", "Bash")),
		},
		wantMsgs: 1, wantTools: []string{"bash"}, wantOutput: 42, wantInput: 10,
	},
	{
		name: "thinking, text and two tool_use blocks",
		lines: []string{
			streamedLine(base, "req_1", "msg_1", 2, thinkingBlock),
			streamedLine(base.Add(time.Second), "req_1", "msg_1", 2, textBlock),
			streamedLine(base.Add(2*time.Second), "req_1", "msg_1", 2, toolBlock("toolu_1", "Read")),
			streamedLine(base.Add(3*time.Second), "req_1", "msg_1", 194, toolBlock("toolu_2", "Read")),
		},
		wantMsgs: 1, wantTools: []string{"read", "read"}, wantOutput: 194, wantInput: 10,
	},
	{
		name: "streamed line repeated verbatim in the same file",
		lines: []string{
			streamedLine(base, "req_1", "msg_1", 1, textBlock),
			streamedLine(base.Add(time.Second), "req_1", "msg_1", 42, toolBlock("toolu_1", "Bash")),
			streamedLine(base.Add(time.Second), "req_1", "msg_1", 42, toolBlock("toolu_1", "Bash")),
		},
		wantMsgs: 1, wantTools: []string{"bash"}, wantOutput: 42, wantInput: 10,
	},
	{
		name: "distinct messages stay separate",
		lines: []string{
			streamedLine(base, "req_1", "msg_1", 42, toolBlock("toolu_1", "Bash")),
			streamedLine(base.Add(time.Second), "req_2", "msg_2", 77, toolBlock("toolu_2", "Read")),
		},
		wantMsgs: 2, wantTools: []string{"bash", "read"}, wantOutput: 119, wantInput: 20,
	},
}

func TestParseConversationRecords_MergesStreamedMessageLines(t *testing.T) {
	for _, tc := range streamedMessageCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sess-1.jsonl")
			writeJSONL(t, path, tc.lines)

			records := parseConversationRecords(path)
			if len(records) != tc.wantMsgs {
				t.Fatalf("records = %d, want %d", len(records), tc.wantMsgs)
			}
			var tools []string
			output, input := 0, 0
			for _, r := range records {
				tools = append(tools, toolNames(r.content)...)
				output += r.usage.OutputTokens
				input += r.usage.InputTokens
			}
			sort.Strings(tools)
			if strings.Join(tools, ",") != strings.Join(tc.wantTools, ",") {
				t.Errorf("tools = %v, want %v", tools, tc.wantTools)
			}
			if output != tc.wantOutput {
				t.Errorf("output tokens = %d, want %d", output, tc.wantOutput)
			}
			if input != tc.wantInput {
				t.Errorf("input tokens = %d, want %d (lines repeat usage, summing would double-count)", input, tc.wantInput)
			}
		})
	}
}

// Both telemetry parsers (full file and append-only from an offset) must see
// the merged message: one message_usage event carrying the final output
// tokens, plus a tool_usage event for every tool_use in the message.
func TestTelemetryParsers_MergeStreamedMessageLines(t *testing.T) {
	parsers := map[string]func(path string) ([]shared.TelemetryEvent, error){
		"full": ParseTelemetryConversationFile,
		"from_offset": func(path string) ([]shared.TelemetryEvent, error) {
			events, _, err := parseTelemetryConversationFileFrom(path, 0)
			return events, err
		},
	}
	for _, tc := range streamedMessageCases {
		for parserName, parse := range parsers {
			t.Run(tc.name+"/"+parserName, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "sess-1.jsonl")
				writeJSONL(t, path, tc.lines)

				events, err := parse(path)
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				msgs, output, input := 0, int64(0), int64(0)
				var tools []string
				for _, ev := range events {
					switch ev.EventType {
					case shared.TelemetryEventTypeMessageUsage:
						msgs++
						output += *ev.OutputTokens
						input += *ev.InputTokens
					case shared.TelemetryEventTypeToolUsage:
						tools = append(tools, ev.ToolName)
					}
				}
				sort.Strings(tools)
				if msgs != tc.wantMsgs {
					t.Errorf("message events = %d, want %d", msgs, tc.wantMsgs)
				}
				if strings.Join(tools, ",") != strings.Join(tc.wantTools, ",") {
					t.Errorf("tool events = %v, want %v", tools, tc.wantTools)
				}
				if output != int64(tc.wantOutput) {
					t.Errorf("output tokens = %d, want %d", output, tc.wantOutput)
				}
				if input != int64(tc.wantInput) {
					t.Errorf("input tokens = %d, want %d", input, tc.wantInput)
				}
			})
		}
	}
}

// A resumed session and a subagent transcript both copy parent messages into
// another file. After merging, each copy holds the full tool list, so the
// cross-file dedup (request id for usage, tool_use id for tools) must still
// count every message and tool call exactly once.
func TestReadConversationJSONL_StreamedMessageCopiedAcrossFilesCountsOnce(t *testing.T) {
	now := time.Now().UTC().Add(-10 * time.Minute)
	streamed := []string{
		streamedLine(now, "req_1", "msg_1", 1, textBlock),
		streamedLine(now.Add(time.Second), "req_1", "msg_1", 42, toolBlock("toolu_1", "Bash")),
	}
	projects := filepath.Join(t.TempDir(), "projects")
	writeJSONL(t, filepath.Join(projects, "proj", "sess-1.jsonl"), streamed)
	// The resumed transcript replays the whole streamed message, then
	// continues with a new turn.
	resumed := append(append([]string{}, streamed...),
		streamedLine(now.Add(time.Minute), "req_2", "msg_2", 7, toolBlock("toolu_2", "Read")),
	)
	writeJSONL(t, filepath.Join(projects, "proj", "sess-1-resumed.jsonl"), resumed)

	p := New()
	snap := core.UsageSnapshot{
		ProviderID:  p.ID(),
		AccountID:   "multiline-test",
		Timestamp:   time.Now(),
		Status:      core.StatusOK,
		Metrics:     make(map[string]core.Metric),
		Raw:         make(map[string]string),
		Resets:      make(map[string]time.Time),
		DailySeries: make(map[string][]core.TimePoint),
	}
	if err := p.readConversationJSONL(projects, "", &snap); err != nil {
		t.Fatalf("readConversationJSONL: %v", err)
	}

	metric := func(key string) float64 {
		t.Helper()
		m, ok := snap.Metrics[key]
		if !ok || m.Used == nil {
			t.Fatalf("missing metric %s", key)
		}
		return *m.Used
	}
	if got := snap.Raw["jsonl_unique_requests"]; got != "2" {
		t.Errorf("unique requests = %q, want 2", got)
	}
	if got := metric("all_time_tool_calls"); got != 2 {
		t.Errorf("all_time_tool_calls = %.0f, want 2", got)
	}
	if got := metric("tool_bash"); got != 1 {
		t.Errorf("tool_bash = %.0f, want 1", got)
	}
	if got := metric("all_time_output_tokens"); got != 49 {
		t.Errorf("all_time_output_tokens = %.0f, want 49 (42 final + 7)", got)
	}
	if got := metric("all_time_input_tokens"); got != 20 {
		t.Errorf("all_time_input_tokens = %.0f, want 20", got)
	}
}
