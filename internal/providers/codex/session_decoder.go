package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type sessionEvent struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type eventPayload struct {
	Type       string      `json:"type"`
	Info       *tokenInfo  `json:"info,omitempty"`
	RateLimits *rateLimits `json:"rate_limits,omitempty"`
	RequestID  string      `json:"request_id,omitempty"`
	MessageID  string      `json:"message_id,omitempty"`
	// Model and ModelID let per-event messages override the model_id resolved
	// from the session header. Codex emits either tag depending on version.
	Model   string `json:"model,omitempty"`
	ModelID string `json:"model_id,omitempty"`
	// ThreadSettings is present on payload.type == "thread_settings_applied"
	// since Codex CLI 0.153, which stopped writing model to session_meta and
	// turn_context. Example: payload.thread_settings.model == "gpt-6-astra".
	ThreadSettings *threadSettings `json:"thread_settings,omitempty"`
}

type threadSettings struct {
	Model   string `json:"model,omitempty"`
	ModelID string `json:"model_id,omitempty"`
}

type tokenInfo struct {
	TotalTokenUsage    tokenUsage `json:"total_token_usage"`
	LastTokenUsage     tokenUsage `json:"last_token_usage"`
	ModelContextWindow int        `json:"model_context_window"`
}

type tokenUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

type sessionMetaPayload struct {
	ID         string `json:"id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Source     string `json:"source,omitempty"`
	Originator string `json:"originator,omitempty"`
	Model      string `json:"model,omitempty"`
	// ModelID is the alternative spelling some Codex versions emit in the
	// session header. We pick whichever is non-empty when resolving the
	// session-wide default model.
	ModelID       string `json:"model_id,omitempty"`
	CWD           string `json:"cwd,omitempty"`
	ModelProvider string `json:"model_provider,omitempty"`
	// BaseInstructions is the last-resort source for the session model.
	// Codex CLI 0.147.0 dropped both `model` and `model_id` from the session
	// header and stopped emitting turn_context lines, leaving
	// base_instructions.provenance.model as the only record of which model
	// the session ran on.
	BaseInstructions *baseInstructions `json:"base_instructions,omitempty"`
}

// ProvenanceModel returns the model recorded in base_instructions provenance,
// or "" when the header predates that field or the instructions did not come
// from a model.
func (m *sessionMetaPayload) ProvenanceModel() string {
	if m == nil || m.BaseInstructions == nil || m.BaseInstructions.Provenance == nil {
		return ""
	}
	provenance := m.BaseInstructions.Provenance
	if provenance.Type != "model" {
		return ""
	}
	return provenance.Model
}

type baseInstructions struct {
	Provenance *instructionsProvenance `json:"provenance,omitempty"`
}

type instructionsProvenance struct {
	Type  string `json:"type,omitempty"`
	Model string `json:"model,omitempty"`
}

type turnContextPayload struct {
	Model   string `json:"model,omitempty"`
	ModelID string `json:"model_id,omitempty"`
	TurnID  string `json:"turn_id,omitempty"`
}

type sessionLine struct {
	Timestamp    string
	LineNumber   int
	SessionMeta  *sessionMetaPayload
	TurnContext  *turnContextPayload
	EventPayload *eventPayload
	ResponseItem *responseItemPayload
}

func walkSessionFile(path string, fn func(sessionLine) error) error {
	_, _, err := walkSessionFileFrom(path, 0, 0, fn)
	return err
}

// walkSessionFileFrom walks complete JSONL records starting at byteOffset.
// It returns the next safe byte offset and absolute line number so callers can
// resume after append-only growth without reading the file from the beginning.
func walkSessionFileFrom(path string, byteOffset int64, startLine int, fn func(sessionLine) error) (int64, int, error) {
	return walkSessionFileRange(path, byteOffset, -1, startLine, fn)
}

// walkSessionFileRange is the bounded form used when a caller must parse a
// filesystem snapshot without consuming bytes appended after it was taken.
// A negative endOffset reads through the current EOF.
func walkSessionFileRange(path string, byteOffset int64, endOffset int64, startLine int, fn func(sessionLine) error) (int64, int, error) {
	return walkSessionLines(path, byteOffset, endOffset, startLine, nil, fn)
}

// sessionLineFilter reports whether a line with the given top-level record
// type and payload type (nil when not visible in the prefix) must be decoded.
// It lets callers skip json.Unmarshal for record kinds they never read, which
// is most of the bytes in a rollout (compacted history, messages, reasoning).
type sessionLineFilter func(recordType, payloadType []byte) bool

// walkSessionLines is walkSessionFileRange with an optional prefix filter.
// Lines rejected by keep are still counted and consumed, only not decoded.
func walkSessionLines(path string, byteOffset int64, endOffset int64, startLine int, keep sessionLineFilter, fn func(sessionLine) error) (int64, int, error) {
	f, err := openSessionFile(path, byteOffset)
	if err != nil {
		return byteOffset, startLine, err
	}
	defer f.Close()

	var source io.Reader = f
	if endOffset >= 0 {
		if endOffset < byteOffset {
			return byteOffset, startLine, fmt.Errorf("codex session end offset %d precedes start offset %d", endOffset, byteOffset)
		}
		source = io.LimitReader(f, endOffset-byteOffset)
	}
	reader := bufio.NewReaderSize(source, 512*1024)
	nextOffset := byteOffset
	lineNumber := startLine
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > maxScannerBufferSize {
			// An oversized record still being written is left for the next
			// cycle, so the resume offset never lands mid-line.
			if readErr == io.EOF && line[len(line)-1] != '\n' {
				return nextOffset, lineNumber, nil
			}
			// Skip oversized lines (e.g., compacted history with huge prior conversation
			// like 13.6M in 2026/08/13 rollout) instead of failing the whole file,
			// so token counts for other lines still contribute to Model Burn.
			nextOffset += int64(len(line))
			lineNumber++
			if readErr == io.EOF {
				return nextOffset, lineNumber, nil
			}
			if readErr != nil {
				return nextOffset, lineNumber, readErr
			}
			continue
		}
		if len(line) == 0 && readErr == io.EOF {
			return nextOffset, lineNumber, nil
		}

		// Do not consume a partially-written final record. It will be retried
		// after the writer appends the remaining bytes on the next cycle.
		if readErr == io.EOF && !json.Valid(bytes.TrimSpace(line)) {
			return nextOffset, lineNumber, nil
		}

		lineNumber++
		nextOffset += int64(len(line))
		if keep != nil && !keepSessionLine(line, keep) {
			if readErr == io.EOF {
				return nextOffset, lineNumber, nil
			}
			if readErr != nil {
				return nextOffset, lineNumber, readErr
			}
			continue
		}
		if record, ok := decodeSessionLine(line, lineNumber); ok {
			if err := fn(record); err != nil {
				return nextOffset, lineNumber, err
			}
		}

		if readErr == io.EOF {
			return nextOffset, lineNumber, nil
		}
		if readErr != nil {
			return nextOffset, lineNumber, readErr
		}
	}
}

func decodeSessionLine(line []byte, lineNumber int) (sessionLine, bool) {
	if !bytes.Contains(line, []byte(`"type":"event_msg"`)) &&
		!bytes.Contains(line, []byte(`"type":"turn_context"`)) &&
		!bytes.Contains(line, []byte(`"type":"session_meta"`)) &&
		!bytes.Contains(line, []byte(`"type":"response_item"`)) {
		return sessionLine{}, false
	}

	var event sessionEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return sessionLine{}, false
	}

	record := sessionLine{
		Timestamp:  event.Timestamp,
		LineNumber: lineNumber,
	}
	switch event.Type {
	case "session_meta":
		var meta sessionMetaPayload
		if json.Unmarshal(event.Payload, &meta) != nil {
			return sessionLine{}, false
		}
		record.SessionMeta = &meta
	case "turn_context":
		var tc turnContextPayload
		if json.Unmarshal(event.Payload, &tc) != nil {
			return sessionLine{}, false
		}
		record.TurnContext = &tc
	case "event_msg":
		var payload eventPayload
		if json.Unmarshal(event.Payload, &payload) != nil {
			return sessionLine{}, false
		}
		record.EventPayload = &payload
	case "response_item":
		var item responseItemPayload
		if json.Unmarshal(event.Payload, &item) != nil {
			return sessionLine{}, false
		}
		record.ResponseItem = &item
	default:
		return sessionLine{}, false
	}
	return record, true
}

// keepSessionLine applies keep to the record kind read from the line prefix.
// Lines whose prefix does not have the expected shape are always kept, so an
// unexpected writer format falls back to a full decode instead of being lost.
func keepSessionLine(line []byte, keep sessionLineFilter) bool {
	recordType, payloadType, ok := sessionLineKind(line)
	if !ok {
		return true
	}
	return keep(recordType, payloadType)
}

// sessionLineKind reads the record type and payload type from the fixed
// prefix Codex writes on every rollout line:
//
//	{"timestamp":"...","type":"event_msg","payload":{"type":"token_count",...
//
// with an optional "ordinal":N between timestamp and type. payloadType is nil
// when the payload does not open with a type key. ok is false when the line
// does not match this shape at all.
func sessionLineKind(line []byte) (recordType, payloadType []byte, ok bool) {
	rest, found := bytes.CutPrefix(line, []byte(`{"timestamp":"`))
	if !found {
		return nil, nil, false
	}
	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return nil, nil, false
	}
	rest = rest[end+1:]
	if after, hasOrdinal := bytes.CutPrefix(rest, []byte(`,"ordinal":`)); hasOrdinal {
		digits := 0
		for digits < len(after) && after[digits] >= '0' && after[digits] <= '9' {
			digits++
		}
		if digits == 0 {
			return nil, nil, false
		}
		rest = after[digits:]
	}
	rest, found = bytes.CutPrefix(rest, []byte(`,"type":"`))
	if !found {
		return nil, nil, false
	}
	end = bytes.IndexByte(rest, '"')
	if end < 0 {
		return nil, nil, false
	}
	recordType = rest[:end]
	if after, hasPayloadType := bytes.CutPrefix(rest[end+1:], []byte(`,"payload":{"type":"`)); hasPayloadType {
		if end = bytes.IndexByte(after, '"'); end >= 0 {
			payloadType = after[:end]
		}
	}
	return recordType, payloadType, true
}
