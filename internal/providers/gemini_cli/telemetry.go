package gemini_cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetrySchemaVersion = "gemini_cli_v1"
)

// System implements shared.TelemetrySource.
func (p *Provider) System() string { return p.ID() }

// Collect implements shared.TelemetrySource. It reads Gemini CLI local session
// files and produces normalized telemetry events for token usage and tool calls.
func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	sessionsDir := shared.ExpandHome(opts.Path("sessions_dir", defaultGeminiSessionsDir()))
	if sessionsDir == "" {
		return nil, nil
	}

	files, err := findGeminiSessionFiles(sessionsDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	var out []shared.TelemetryEvent
	for _, path := range files {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		events, err := parseGeminiTelemetrySessionFile(path)
		if err != nil {
			continue
		}
		for _, ev := range events {
			key := deduplicationKey(ev)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ev)
		}
	}
	return out, nil
}

// ParseHookPayload implements shared.TelemetrySource.
// Gemini CLI does not support hook-based telemetry.
func (p *Provider) ParseHookPayload(_ []byte, _ shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return nil, shared.ErrHookUnsupported
}

// defaultGeminiSessionsDir returns the default directory where Gemini CLI
// stores session files (~/.gemini/tmp).
func defaultGeminiSessionsDir() string {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".gemini", "tmp")
}

// parseGeminiTelemetrySessionFile reads a single Gemini CLI session JSON file
// and produces telemetry events from its messages.
func parseGeminiTelemetrySessionFile(path string) ([]shared.TelemetryEvent, error) {
	chat, err := readGeminiChatFile(path)
	if err != nil {
		return nil, err
	}

	sessionID := strings.TrimSpace(chat.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	var previous tokenUsage
	var hasPrevious bool
	turnIndex := 0

	var out []shared.TelemetryEvent

	for msgIdx, msg := range chat.Messages {
		occurredAt := parseMessageTime(msg.Timestamp, chat.StartTime, chat.LastUpdated)

		// Emit tool usage events for each tool call.
		for tcIdx, tc := range msg.ToolCalls {
			toolName := strings.TrimSpace(tc.Name)
			if toolName == "" {
				continue
			}

			status := telemetryStatusFromToolCall(tc.Status)
			toolCallID := fmt.Sprintf("%s:msg%d:tc%d", sessionID, msgIdx, tcIdx)

			payload := map[string]any{
				"file": path,
			}
			if tc.Args != nil {
				for _, fp := range extractGeminiToolPaths(tc.Args) {
					payload["file"] = fp
					break
				}
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetrySchemaVersion,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurredAt,
				AccountID:     "gemini_cli",
				SessionID:     sessionID,
				TurnID:        fmt.Sprintf("%s:msg%d", sessionID, msgIdx),
				ToolCallID:    toolCallID,
				ProviderID:    "google",
				AgentName:     "gemini_cli",
				EventType:     shared.TelemetryEventTypeToolUsage,
				ModelRaw:      normalizeModelName(msg.Model),
				ToolName:      toolName,
				Requests:      shared.Int64Ptr(1),
				Status:        status,
				Payload:       payload,
			})
		}

		// Emit message usage events for messages with token data.
		if msg.Tokens == nil {
			continue
		}

		total := msg.Tokens.toUsage()
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
		turnID := fmt.Sprintf("%s:%d", sessionID, turnIndex)
		messageID := fmt.Sprintf("%s:msg%d", sessionID, msgIdx)

		out = append(out, shared.TelemetryEvent{
			SchemaVersion:   telemetrySchemaVersion,
			Channel:         shared.TelemetryChannelJSONL,
			OccurredAt:      occurredAt,
			AccountID:       "gemini_cli",
			SessionID:       sessionID,
			TurnID:          turnID,
			MessageID:       messageID,
			ProviderID:      "google",
			AgentName:       "gemini_cli",
			EventType:       shared.TelemetryEventTypeMessageUsage,
			ModelRaw:        normalizeModelName(msg.Model),
			InputTokens:     shared.Int64Ptr(int64(delta.InputTokens)),
			OutputTokens:    shared.Int64Ptr(int64(delta.OutputTokens)),
			ReasoningTokens: shared.Int64Ptr(int64(delta.ReasoningTokens)),
			CacheReadTokens: shared.Int64Ptr(int64(delta.CachedInputTokens)),
			TotalTokens:     shared.Int64Ptr(int64(delta.TotalTokens)),
			Status:          shared.TelemetryStatusOK,
			Payload: map[string]any{
				"file":       path,
				"tool_tokens": delta.ToolTokens,
			},
		})
	}

	return out, nil
}

// parseMessageTime attempts to parse a message timestamp, falling back to
// session-level timestamps, and finally to the current time.
func parseMessageTime(msgTimestamp, sessionStart, sessionLastUpdated string) time.Time {
	if ts, err := shared.ParseTimestampString(msgTimestamp); err == nil {
		return ts
	}
	if ts, err := shared.ParseTimestampString(sessionLastUpdated); err == nil {
		return ts
	}
	if ts, err := shared.ParseTimestampString(sessionStart); err == nil {
		return ts
	}
	return time.Now().UTC()
}

// telemetryStatusFromToolCall maps a Gemini CLI tool call status string to a
// TelemetryStatus value.
func telemetryStatusFromToolCall(status string) shared.TelemetryStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "success", "succeeded", "ok", "completed":
		return shared.TelemetryStatusOK
	case "cancelled", "canceled":
		return shared.TelemetryStatusAborted
	case "error", "failed", "failure":
		return shared.TelemetryStatusError
	default:
		return shared.TelemetryStatusUnknown
	}
}

// deduplicationKey returns a unique key for a telemetry event used to prevent
// duplicate events when session files overlap.
func deduplicationKey(ev shared.TelemetryEvent) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s",
		ev.SessionID, ev.TurnID, ev.MessageID,
		ev.ToolCallID, ev.EventType, ev.OccurredAt.Format(time.RFC3339Nano))
}
