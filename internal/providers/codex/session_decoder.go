package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	f, err := os.Open(path)
	if err != nil {
		return byteOffset, startLine, err
	}
	defer f.Close()

	if _, err := f.Seek(byteOffset, io.SeekStart); err != nil {
		return byteOffset, startLine, err
	}

	reader := bufio.NewReaderSize(f, 512*1024)
	nextOffset := byteOffset
	lineNumber := startLine
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > maxScannerBufferSize {
			return nextOffset, lineNumber, fmt.Errorf("codex session line exceeds %d bytes", maxScannerBufferSize)
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
