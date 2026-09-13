package muse_code

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// museModelEntry is one assistant step's token usage, normalized from a
// model_completed event. ID/Stream/Sequence are kept for cross-file
// deduplication: the same record can appear in multiple session files
// (copied logs, feedback-session) and must be counted once.
type museModelEntry struct {
	Timestamp   time.Time
	SessionID   string
	Model       string
	Input       int64
	Output      int64
	Reasoning   int64
	CacheRead   int64
	CacheWrite  int64
	TotalTokens int64
	RecordID    string
	StreamID    string
	Sequence    int64
}

type museUsageBuckets struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CachedTokens     int64 `json:"cached_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`
}

type museRunEvent struct {
	Kind  string           `json:"kind"`
	Model string           `json:"model"`
	Usage museUsageBuckets `json:"usage"`
}

type musePayload struct {
	Kind  string       `json:"kind"`
	Event museRunEvent `json:"event"`
}

type museRecord struct {
	ID          string      `json:"id"`
	PayloadType string      `json:"payload_type"`
	Payload     musePayload `json:"payload"`
	RecordedAt  int64       `json:"recorded_at"`
	Sequence    int64       `json:"sequence"`
	Stream      struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"stream"`
}

type museRetainedFrame struct {
	Children []struct {
		RecordJSON string `json:"record_json"`
	} `json:"children"`
}

// readMuseSessionFile parses every model_completed event of one session
// file. Session files mix retained-frame wrappers
// ({"retained_frame":...,"children":[{"record_json":"..."}]}) and bare
// records, so each line is normalized to its record list first. Lines
// without usage events (resource samples, tool batches, permission frames)
// are dropped here.
func readMuseSessionFile(path string) ([]museModelEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// The session id is the parent directory name:
	// sessions/YYYY/MM/DD/<session-id>/session.jsonl.
	sessionID := filepath.Base(filepath.Dir(path))
	var entries []museModelEntry
	for _, line := range bytes.Split(data, []byte("\n")) {
		// Quoteless on purpose: retained-frame wrappers escape their
		// embedded records (...completed\"), so a quoted marker would never
		// match them. Shape validation still happens in recordEntry, so a
		// stray mention elsewhere parses to nothing.
		if !bytes.Contains(line, []byte("model_completed")) {
			continue
		}
		for _, entry := range parseMuseRecords(line) {
			entry.SessionID = sessionID
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func parseMuseRecords(line []byte) []museModelEntry {
	var frame museRetainedFrame
	if err := json.Unmarshal(line, &frame); err != nil || frame.Children == nil {
		if entry, ok := museRecordEntry(line); ok {
			return []museModelEntry{entry}
		}
		return nil
	}
	var entries []museModelEntry
	for _, child := range frame.Children {
		if entry, ok := museRecordEntry([]byte(child.RecordJSON)); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// museRecordEntry converts one record to an entry. recorded_at is
// microseconds since the epoch. Zero-usage completions and steps with no
// model carry nothing to count and are skipped.
func museRecordEntry(data []byte) (museModelEntry, bool) {
	var rec museRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return museModelEntry{}, false
	}
	if rec.PayloadType != "runtime.session" {
		return museModelEntry{}, false
	}
	event := rec.Payload.Event
	if event.Kind != "model_completed" {
		return museModelEntry{}, false
	}
	model := strings.TrimSpace(event.Model)
	if model == "" {
		return museModelEntry{}, false
	}
	usage := event.Usage
	if usage.InputTokens <= 0 && usage.OutputTokens <= 0 && usage.ReasoningTokens <= 0 {
		return museModelEntry{}, false
	}
	cacheRead := usage.CacheReadTokens
	if cacheRead <= 0 {
		cacheRead = usage.CachedTokens
	}
	input := usage.InputTokens - cacheRead
	if input < 0 {
		input = 0
	}
	return museModelEntry{
		Timestamp:  time.UnixMicro(rec.RecordedAt).UTC(),
		Model:      model,
		Input:      input,
		Output:     usage.OutputTokens,
		Reasoning:  usage.ReasoningTokens,
		CacheRead:  cacheRead,
		CacheWrite: usage.CacheWriteTokens,
		// Same total definition as populateSnapshot's per-model total
		// (input + output + reasoning + cacheRead + cacheWrite, where
		// input is already net of cacheRead). usage.InputTokens includes
		// the cached slice, so add only the cache-write remainder.
		TotalTokens: usage.InputTokens + usage.OutputTokens + usage.ReasoningTokens + usage.CacheWriteTokens,
		RecordID:    rec.ID,
		StreamID:    rec.Stream.ID,
		Sequence:    rec.Sequence,
	}, true
}

// museToolEntry is one tool invocation, normalized from tool_call and
// tool_result events. Muse's session logs store tool calls as
// `tool_call` (with tool_call_id + name) and results as
// `tool_result_batch_committed` (with tool_call_id). We count calls by name.
type museToolEntry struct {
	ToolCallID string
	Name       string
}

// readMuseToolCalls parses every tool_call of one session file.
// It handles both bare records and retained-frame wrappers, like
// readMuseSessionFile, but looks for tool_call/tool_result events.
func readMuseToolCalls(path string) ([]museToolEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []museToolEntry
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.Contains(line, []byte("tool_call")) && !bytes.Contains(line, []byte("tool_result")) {
			continue
		}
		for _, entry := range parseMuseToolRecords(line) {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func parseMuseToolRecords(line []byte) []museToolEntry {
	// First check for batched assistant_tool_calls_committed
	if batched := parseMuseToolRecordsBatched(line); len(batched) > 0 {
		return batched
	}
	var frame museRetainedFrame
	if err := json.Unmarshal(line, &frame); err != nil || frame.Children == nil {
		if entry, ok := museToolEntryFromRecord(line); ok {
			return []museToolEntry{entry}
		}
		// Also check batched in bare record
		if batched := parseMuseToolRecordsBatched(line); len(batched) > 0 {
			return batched
		}
		return nil
	}
	var entries []museToolEntry
	for _, child := range frame.Children {
		if entry, ok := museToolEntryFromRecord([]byte(child.RecordJSON)); ok {
			entries = append(entries, entry)
		} else if batched := parseMuseToolRecordsBatched([]byte(child.RecordJSON)); len(batched) > 0 {
			entries = append(entries, batched...)
		}
	}
	return entries
}

func museToolEntryFromRecord(data []byte) (museToolEntry, bool) {
	var rec struct {
		PayloadType string `json:"payload_type"`
		Payload     struct {
			Kind  string `json:"kind"`
			Event struct {
				Kind       string `json:"kind"`
				ToolCallID string `json:"tool_call_id"`
				Name       string `json:"name"`
				Tool       string `json:"tool"`
				ToolCalls  []struct {
					Name   string `json:"name"`
					CallID string `json:"call_id"`
					ID     string `json:"id"`
				} `json:"tool_calls"`
			} `json:"event"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return museToolEntry{}, false
	}
	if rec.PayloadType != "runtime.session" {
		return museToolEntry{}, false
	}
	kind := rec.Payload.Event.Kind
	// Muse logs tool calls as event.kind == "tool_call" (single) or
	// "assistant_tool_calls_committed" (batch with tool_calls array), and
	// results as "tool_result" / "tool_result_batch_committed".
	// We count the call, not the result, to avoid double-counting.
	if kind == "tool_call" {
		name := strings.TrimSpace(rec.Payload.Event.Name)
		if name == "" {
			name = strings.TrimSpace(rec.Payload.Event.Tool)
		}
		if name == "" {
			return museToolEntry{}, false
		}
		if name == "shell" || name == "bash" {
			name = "exec"
		}
		return museToolEntry{
			ToolCallID: rec.Payload.Event.ToolCallID,
			Name:       name,
		}, true
	}
	if kind == "assistant_tool_calls_committed" {
		// Batch of tool calls; count each by name
		// Use the first for now, the outer loop will handle each
		// This path is handled via parseMuseToolRecords returning multiple
		return museToolEntry{}, false
	}
	return museToolEntry{}, false
}

func parseMuseToolRecordsBatched(line []byte) []museToolEntry {
	// Special handling for assistant_tool_calls_committed which contains
	// multiple tool_calls in one record
	var rec struct {
		PayloadType string `json:"payload_type"`
		Payload     struct {
			Kind  string `json:"kind"`
			Event struct {
				Kind      string `json:"kind"`
				ToolCalls []struct {
					Name   string `json:"name"`
					CallID string `json:"call_id"`
					ID     string `json:"id"`
				} `json:"tool_calls"`
			} `json:"event"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &rec); err != nil {
		return nil
	}
	if rec.PayloadType != "runtime.session" || rec.Payload.Event.Kind != "assistant_tool_calls_committed" {
		return nil
	}
	var entries []museToolEntry
	for _, tc := range rec.Payload.Event.ToolCalls {
		name := strings.TrimSpace(tc.Name)
		if name == "" {
			continue
		}
		if name == "shell" || name == "bash" {
			name = "exec"
		}
		entries = append(entries, museToolEntry{
			ToolCallID: tc.CallID,
			Name:       name,
		})
	}
	return entries
}

func readAllToolCalls(ctx context.Context, dirs []string) ([]museToolEntry, error) {
	var all []museToolEntry
	seen := make(map[string]struct{})
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".jsonl" {
				return nil
			}
			if isSubagentTranscript(path) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			canonical := canonicalPath(path)
			if _, dup := seen[canonical]; dup {
				return nil
			}
			seen[canonical] = struct{}{}
			entries, err := readMuseToolCalls(path)
			if err != nil {
				return nil
			}
			all = append(all, entries...)
			return nil
		})
	}
	// Dedup by tool_call_id across files (same call can appear in feedback-session)
	seenCalls := make(map[string]struct{}, len(all))
	deduped := make([]museToolEntry, 0, len(all))
	for _, e := range all {
		key := e.ToolCallID
		if key == "" {
			key = e.Name + "|" + strconv.Itoa(len(deduped))
		}
		if _, dup := seenCalls[key]; dup {
			continue
		}
		seenCalls[key] = struct{}{}
		deduped = append(deduped, e)
	}
	return deduped, nil
}
