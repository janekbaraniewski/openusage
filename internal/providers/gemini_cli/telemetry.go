package gemini_cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetrySchemaVersion = "gemini_cli_v2"
)

// System implements shared.TelemetrySource.
func (p *Provider) System() string { return p.ID() }

func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			"sessions_dir": defaultGeminiSessionsDir(),
		},
	}
}

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
		messageOccurredAt := parseMessageTime(msg.Timestamp, chat.StartTime, chat.LastUpdated)
		messageID := geminiTelemetryMessageID(sessionID, msg, msgIdx)
		turnID := fmt.Sprintf("%s:msg%d", sessionID, msgIdx)
		if messageID != "" {
			turnID = messageID
		}

		// Emit tool usage events for each tool call.
		for tcIdx, tc := range msg.ToolCalls {
			toolName, payloadToolMeta := normalizeGeminiTelemetryToolName(tc)
			if strings.TrimSpace(toolName) == "" {
				continue
			}

			status := telemetryStatusFromToolCall(tc.Status)
			toolCallID := strings.TrimSpace(tc.ID)
			if toolCallID == "" {
				toolCallID = fmt.Sprintf("%s:msg%d:tc%d", sessionID, msgIdx, tcIdx)
			}
			occurredAt := parseToolCallTime(tc.Timestamp, msg.Timestamp, chat.StartTime, chat.LastUpdated)

			payload := map[string]any{
				"source_file":       path,
				"upstream_provider": "google",
				"client":            "CLI",
				"tool_type":         "native",
			}
			for key, value := range payloadToolMeta {
				payload[key] = value
			}
			if tc.Args != nil {
				for _, fp := range extractGeminiToolPaths(tc.Args) {
					payload["file"] = fp
					break
				}
			}
			if _, ok := payload["file"]; !ok {
				if resultFile := extractGeminiResultDisplayFile(tc.ResultDisplay); resultFile != "" {
					payload["file"] = resultFile
				}
			}
			if diff, ok := extractGeminiToolDiffStat(tc.ResultDisplay); ok {
				payload["model_added_lines"] = diff.ModelAddedLines
				payload["model_removed_lines"] = diff.ModelRemovedLines
				payload["user_added_lines"] = diff.UserAddedLines
				payload["user_removed_lines"] = diff.UserRemovedLines
				payload["model_added_chars"] = diff.ModelAddedChars
				payload["model_removed_chars"] = diff.ModelRemovedChars
				payload["user_added_chars"] = diff.UserAddedChars
				payload["user_removed_chars"] = diff.UserRemovedChars
				payload["lines_added"] = diff.ModelAddedLines + diff.UserAddedLines
				payload["lines_removed"] = diff.ModelRemovedLines + diff.UserRemovedLines
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetrySchemaVersion,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurredAt,
				AccountID:     "gemini_cli",
				SessionID:     sessionID,
				TurnID:        turnID,
				MessageID:     messageID,
				ToolCallID:    toolCallID,
				ProviderID:    "gemini_cli",
				AgentName:     "gemini_cli",
				EventType:     shared.TelemetryEventTypeToolUsage,
				ModelRaw:      normalizeModelName(msg.Model),
				TokenUsage: core.TokenUsage{
					Requests: core.Int64Ptr(1),
				},
				ToolName: toolName,
				Status:   status,
				Payload:  payload,
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
		tokenTurnID := fmt.Sprintf("%s:%d", sessionID, turnIndex)
		if strings.TrimSpace(messageID) != "" {
			tokenTurnID = messageID
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetrySchemaVersion,
			Channel:       shared.TelemetryChannelJSONL,
			OccurredAt:    messageOccurredAt,
			AccountID:     "gemini_cli",
			SessionID:     sessionID,
			TurnID:        tokenTurnID,
			MessageID:     messageID,
			ProviderID:    "gemini_cli",
			AgentName:     "gemini_cli",
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      normalizeModelName(msg.Model),
			TokenUsage: core.TokenUsage{
				InputTokens:     core.Int64Ptr(int64(delta.InputTokens)),
				OutputTokens:    core.Int64Ptr(int64(delta.OutputTokens)),
				ReasoningTokens: core.Int64Ptr(int64(delta.ReasoningTokens)),
				CacheReadTokens: core.Int64Ptr(int64(delta.CachedInputTokens)),
				TotalTokens:     core.Int64Ptr(int64(delta.TotalTokens)),
			},
			Status: shared.TelemetryStatusOK,
			Payload: map[string]any{
				"source_file":       path,
				"tool_tokens":       delta.ToolTokens,
				"client":            "CLI",
				"upstream_provider": "google",
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

func parseToolCallTime(toolTimestamp, msgTimestamp, sessionStart, sessionLastUpdated string) time.Time {
	if ts, err := shared.ParseTimestampString(toolTimestamp); err == nil {
		return ts
	}
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

func geminiTelemetryMessageID(sessionID string, msg geminiChatMessage, msgIdx int) string {
	msgID := strings.TrimSpace(msg.ID)
	if msgID == "" {
		return fmt.Sprintf("%s:msg%d", sessionID, msgIdx)
	}
	if strings.Contains(msgID, ":") {
		return msgID
	}
	return fmt.Sprintf("%s:%s", sessionID, msgID)
}

func normalizeGeminiTelemetryToolName(tc geminiToolCall) (string, map[string]any) {
	toolName := strings.TrimSpace(tc.Name)
	displayName := strings.TrimSpace(tc.DisplayName)
	description := strings.TrimSpace(tc.Description)

	payload := make(map[string]any)
	if toolName == "" && displayName == "" {
		return "", payload
	}
	if displayName != "" {
		payload["tool_display_name"] = displayName
	}
	if description != "" {
		payload["tool_description"] = truncate(description, 240)
	}
	if tc.RenderOutputAsMarkdown != nil {
		payload["render_output_markdown"] = *tc.RenderOutputAsMarkdown
	}
	if strings.TrimSpace(toolName) != "" {
		payload["tool_name_raw"] = strings.TrimSpace(toolName)
	}

	if strings.HasPrefix(strings.ToLower(toolName), "mcp__") {
		canonical := strings.ToLower(strings.TrimSpace(toolName))
		payload["tool_type"] = "mcp"
		if parts := strings.SplitN(strings.TrimPrefix(canonical, "mcp__"), "__", 2); len(parts) == 2 {
			payload["mcp_server"] = parts[0]
			payload["mcp_function"] = parts[1]
		}
		return canonical, payload
	}

	if mcpServer, mcpFunction, ok := extractGeminiMCPTool(displayName, toolName); ok {
		payload["tool_type"] = "mcp"
		payload["mcp_server"] = mcpServer
		payload["mcp_function"] = mcpFunction
		return "mcp__" + mcpServer + "__" + mcpFunction, payload
	}

	return sanitizeMetricName(toolName), payload
}

var geminiMCPDisplayPattern = regexp.MustCompile(`(?i)\(([^()]+?)\s+mcp server\)\s*$`)

func extractGeminiMCPTool(displayName, fallbackToolName string) (server, function string, ok bool) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return "", "", false
	}
	matches := geminiMCPDisplayPattern.FindStringSubmatch(displayName)
	if len(matches) != 2 {
		return "", "", false
	}

	server = normalizeGeminiMCPToken(matches[1])
	function = normalizeGeminiMCPToken(fallbackToolName)
	if function == "" {
		withoutSuffix := strings.TrimSpace(geminiMCPDisplayPattern.ReplaceAllString(displayName, ""))
		function = normalizeGeminiMCPToken(withoutSuffix)
	}
	if server == "" || function == "" {
		return "", "", false
	}
	return server, function, true
}

func normalizeGeminiMCPToken(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}

	var b strings.Builder
	underscore := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			underscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			underscore = false
		case r == '-' || r == '_':
			if !underscore {
				b.WriteRune(r)
				underscore = true
			}
		default:
			if !underscore {
				b.WriteByte('_')
				underscore = true
			}
		}
	}

	return strings.Trim(b.String(), "_-")
}

func extractGeminiResultDisplayFile(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || raw[0] != '{' {
		return ""
	}

	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return ""
	}
	for _, key := range []string{"filePath", "file_path", "path", "file"} {
		if value, ok := root[key].(string); ok {
			for _, token := range extractGeminiPathTokens(value) {
				if token != "" {
					return token
				}
			}
		}
	}
	return ""
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
