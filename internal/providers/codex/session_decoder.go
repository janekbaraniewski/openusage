package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	return m.BaseInstructions.Provenance.Model
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
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 512*1024), maxScannerBufferSize)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(`"type":"event_msg"`)) &&
			!bytes.Contains(line, []byte(`"type":"turn_context"`)) &&
			!bytes.Contains(line, []byte(`"type":"session_meta"`)) &&
			!bytes.Contains(line, []byte(`"type":"response_item"`)) {
			continue
		}

		var event sessionEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		record := sessionLine{
			Timestamp:  event.Timestamp,
			LineNumber: lineNumber,
		}

		switch event.Type {
		case "session_meta":
			var meta sessionMetaPayload
			if json.Unmarshal(event.Payload, &meta) != nil {
				continue
			}
			record.SessionMeta = &meta
		case "turn_context":
			var tc turnContextPayload
			if json.Unmarshal(event.Payload, &tc) != nil {
				continue
			}
			record.TurnContext = &tc
		case "event_msg":
			var payload eventPayload
			if json.Unmarshal(event.Payload, &payload) != nil {
				continue
			}
			record.EventPayload = &payload
		case "response_item":
			var item responseItemPayload
			if json.Unmarshal(event.Payload, &item) != nil {
				continue
			}
			record.ResponseItem = &item
		default:
			continue
		}

		if err := fn(record); err != nil {
			return err
		}
	}

	return scanner.Err()
}
