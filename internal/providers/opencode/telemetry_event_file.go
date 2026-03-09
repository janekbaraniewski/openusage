package opencode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// ParseTelemetryEventFile parses OpenCode event jsonl/ndjson files.
func ParseTelemetryEventFile(path string) ([]shared.TelemetryEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []shared.TelemetryEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 512*1024), 8*1024*1024)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		var ev eventEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}

		switch eventType := telemetryEventType(ev); eventType {
		case "message.updated":
			props, ok := decodeMessageUpdatedProps(ev.Properties)
			if !ok || strings.ToLower(strings.TrimSpace(props.Info.Role)) != "assistant" {
				continue
			}
			out = append(out, buildJSONLMessageUsageEvent(path, lineNumber, props.Info))
		case "tool.execute.after":
			tool, rawPayloadMap, ok := decodeToolPayload(ev.Payload)
			if !ok {
				continue
			}
			out = append(out, buildJSONLToolUsageEvent(path, lineNumber, tool, rawPayloadMap))
		}
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func telemetryEventType(ev eventEnvelope) string {
	eventType := strings.TrimSpace(ev.Type)
	if eventType == "" {
		eventType = strings.TrimSpace(ev.Event)
	}
	return eventType
}

func decodeMessageUpdatedProps(raw json.RawMessage) (messageUpdatedProps, bool) {
	var props messageUpdatedProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return messageUpdatedProps{}, false
	}
	return props, true
}

func buildJSONLMessageUsageEvent(path string, lineNumber int, info assistantInfo) shared.TelemetryEvent {
	messageID := strings.TrimSpace(info.ID)
	if messageID == "" {
		messageID = fmt.Sprintf("%s:%d", path, lineNumber)
	}

	total := info.Tokens.Input + info.Tokens.Output + info.Tokens.Reasoning + info.Tokens.Cache.Read + info.Tokens.Cache.Write
	occurredAt := shared.UnixAuto(info.Time.Created)
	if info.Time.Completed > 0 {
		occurredAt = shared.UnixAuto(info.Time.Completed)
	}

	return shared.TelemetryEvent{
		SchemaVersion: telemetryEventSchema,
		Channel:       shared.TelemetryChannelJSONL,
		OccurredAt:    occurredAt,
		WorkspaceID:   shared.SanitizeWorkspace(info.Path.CWD),
		SessionID:     strings.TrimSpace(info.SessionID),
		TurnID:        strings.TrimSpace(info.ParentID),
		MessageID:     messageID,
		ProviderID:    core.FirstNonEmpty(strings.TrimSpace(info.ProviderID), "opencode"),
		AgentName:     "opencode",
		EventType:     shared.TelemetryEventTypeMessageUsage,
		ModelRaw:      strings.TrimSpace(info.ModelID),
		TokenUsage: core.TokenUsage{
			InputTokens:      core.Int64Ptr(info.Tokens.Input),
			OutputTokens:     core.Int64Ptr(info.Tokens.Output),
			ReasoningTokens:  core.Int64Ptr(info.Tokens.Reasoning),
			CacheReadTokens:  core.Int64Ptr(info.Tokens.Cache.Read),
			CacheWriteTokens: core.Int64Ptr(info.Tokens.Cache.Write),
			TotalTokens:      core.Int64Ptr(total),
			CostUSD:          core.Float64Ptr(info.Cost),
		},
		Status: shared.TelemetryStatusOK,
		Payload: map[string]any{
			"file": path,
			"line": lineNumber,
		},
	}
}

func decodeToolPayload(raw json.RawMessage) (toolPayload, map[string]any, bool) {
	if len(raw) == 0 {
		return toolPayload{}, nil, false
	}
	var tool toolPayload
	if err := json.Unmarshal(raw, &tool); err != nil {
		return toolPayload{}, nil, false
	}
	var rawPayloadMap map[string]any
	if err := json.Unmarshal(raw, &rawPayloadMap); err != nil {
		rawPayloadMap = nil
	}
	return tool, rawPayloadMap, true
}

func buildJSONLToolUsageEvent(path string, lineNumber int, tool toolPayload, rawPayloadMap map[string]any) shared.TelemetryEvent {
	toolCallID := strings.TrimSpace(tool.ToolCallID)
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("%s:%d", path, lineNumber)
	}

	toolName := strings.TrimSpace(tool.ToolName)
	if toolName == "" {
		toolName = strings.TrimSpace(tool.Name)
	}
	if toolName == "" {
		toolName = "unknown"
	}

	occurredAt := time.Now().UTC()
	if tool.Timestamp > 0 {
		occurredAt = shared.UnixAuto(tool.Timestamp)
	}

	toolFilePath := ""
	if paths := shared.ExtractFilePathsFromPayload(rawPayloadMap); len(paths) > 0 {
		toolFilePath = paths[0]
	}

	return shared.TelemetryEvent{
		SchemaVersion: telemetryEventSchema,
		Channel:       shared.TelemetryChannelJSONL,
		OccurredAt:    occurredAt,
		SessionID:     strings.TrimSpace(tool.SessionID),
		MessageID:     strings.TrimSpace(tool.MessageID),
		ToolCallID:    toolCallID,
		ProviderID:    "opencode",
		AgentName:     "opencode",
		EventType:     shared.TelemetryEventTypeToolUsage,
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(1),
		},
		ToolName: strings.ToLower(toolName),
		Status:   shared.TelemetryStatusOK,
		Payload: map[string]any{
			"source_file": path,
			"line":        lineNumber,
			"file":        toolFilePath,
		},
	}
}
