package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

type telemetrySessionEvent struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type telemetrySessionMeta struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
}

type telemetryTurnContext struct {
	Model string `json:"model"`
}

type telemetryTokenInfo struct {
	TotalTokenUsage tokenUsage `json:"total_token_usage"`
}

type telemetryEventPayload struct {
	Type      string              `json:"type"`
	Info      *telemetryTokenInfo `json:"info"`
	RequestID string              `json:"request_id,omitempty"`
	MessageID string              `json:"message_id,omitempty"`
}

type telemetrySource struct{}

func NewTelemetrySource() shared.TelemetrySource { return telemetrySource{} }

func (telemetrySource) System() string { return "codex" }

func (telemetrySource) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	sessionsDir := shared.ExpandHome(opts.Path("sessions_dir", DefaultTelemetrySessionsDir()))
	files := shared.CollectFilesByExt([]string{sessionsDir}, map[string]bool{".jsonl": true})
	if len(files) == 0 {
		return nil, nil
	}

	var out []shared.TelemetryEvent
	for _, path := range files {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		events, err := ParseTelemetrySessionFile(path)
		if err != nil {
			continue
		}
		out = append(out, events...)
	}
	return out, nil
}

func (telemetrySource) ParseHookPayload(_ []byte, _ shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return nil, shared.ErrHookUnsupported
}

// DefaultTelemetrySessionsDir returns the default Codex sessions directory.
func DefaultTelemetrySessionsDir() string {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, defaultCodexConfigDir, "sessions")
}

// ParseTelemetrySessionFile parses a Codex session JSONL file into normalized telemetry events.
func ParseTelemetrySessionFile(path string) ([]shared.TelemetryEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sessionID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	model := ""
	var previous tokenUsage
	hasPrevious := false
	turnIndex := 0

	var out []shared.TelemetryEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 512*1024), maxScannerBufferSize)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		var ev telemetrySessionEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "session_meta":
			var meta telemetrySessionMeta
			if json.Unmarshal(ev.Payload, &meta) == nil {
				sid := shared.FirstNonEmpty(meta.SessionID, meta.ID)
				if sid != "" {
					sessionID = sid
				}
				if strings.TrimSpace(meta.Model) != "" {
					model = strings.TrimSpace(meta.Model)
				}
			}
		case "turn_context":
			var tc telemetryTurnContext
			if json.Unmarshal(ev.Payload, &tc) == nil && strings.TrimSpace(tc.Model) != "" {
				model = strings.TrimSpace(tc.Model)
			}
		case "event_msg":
			var payload telemetryEventPayload
			if json.Unmarshal(ev.Payload, &payload) != nil || payload.Type != "token_count" || payload.Info == nil {
				continue
			}

			total := payload.Info.TotalTokenUsage
			delta := total
			if hasPrevious {
				delta = usageDelta(total, previous)
				if !validUsageDelta(delta) {
					delta = total
				}
			}
			previous = total
			hasPrevious = true

			if delta.TotalTokens <= 0 {
				continue
			}
			turnIndex++

			occurredAt := time.Now().UTC()
			if ts, err := shared.ParseTimestampString(ev.Timestamp); err == nil {
				occurredAt = ts
			}

			turnID := fmt.Sprintf("%s:%d", sessionID, turnIndex)
			if strings.TrimSpace(payload.RequestID) != "" {
				turnID = strings.TrimSpace(payload.RequestID)
			}
			messageID := strings.TrimSpace(payload.MessageID)
			if messageID == "" {
				messageID = fmt.Sprintf("%s:%d", sessionID, lineNumber)
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion:   "codex_session_v1",
				Channel:         shared.TelemetryChannelJSONL,
				OccurredAt:      occurredAt,
				AccountID:       "codex",
				SessionID:       sessionID,
				TurnID:          turnID,
				MessageID:       messageID,
				ProviderID:      "openai",
				AgentName:       "codex",
				EventType:       shared.TelemetryEventTypeMessageUsage,
				ModelRaw:        model,
				InputTokens:     shared.Int64Ptr(int64(delta.InputTokens)),
				OutputTokens:    shared.Int64Ptr(int64(delta.OutputTokens)),
				ReasoningTokens: shared.Int64Ptr(int64(delta.ReasoningOutputTokens)),
				CacheReadTokens: shared.Int64Ptr(int64(delta.CachedInputTokens)),
				TotalTokens:     shared.Int64Ptr(int64(delta.TotalTokens)),
				Status:          shared.TelemetryStatusOK,
				Payload: map[string]any{
					"file": path,
					"line": lineNumber,
				},
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}
