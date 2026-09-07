package muse_code

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// museModelEntry is one assistant step's token usage, normalized from a
// model_completed event.
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
	PayloadType string      `json:"payload_type"`
	Payload     musePayload `json:"payload"`
	RecordedAt  int64       `json:"recorded_at"`
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
		Timestamp:   time.UnixMicro(rec.RecordedAt).UTC(),
		Model:       model,
		Input:       input,
		Output:      usage.OutputTokens,
		Reasoning:   usage.ReasoningTokens,
		CacheRead:   cacheRead,
		CacheWrite:  usage.CacheWriteTokens,
		TotalTokens: usage.InputTokens + usage.OutputTokens + usage.ReasoningTokens,
	}, true
}
