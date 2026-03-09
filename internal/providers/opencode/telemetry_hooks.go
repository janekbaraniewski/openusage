package opencode

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// ParseTelemetryHookPayload parses OpenCode plugin hook payloads.
func ParseTelemetryHookPayload(raw []byte) ([]shared.TelemetryEvent, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return nil, fmt.Errorf("decode hook payload: %w", err)
	}
	rootPayload := decodeRawMessageMap(root)

	if eventRaw, ok := root["event"]; ok && len(eventRaw) > 0 {
		return parseEventJSON(eventRaw, decodeJSONMap(eventRaw), true)
	}
	if hookRaw, ok := root["hook"]; ok {
		var hook string
		if err := json.Unmarshal(hookRaw, &hook); err != nil {
			return nil, fmt.Errorf("decode hook name: %w", err)
		}
		switch strings.TrimSpace(hook) {
		case "tool.execute.after":
			return parseToolExecuteAfterHook(root, rootPayload)
		case "chat.message":
			return parseChatMessageHook(root, rootPayload)
		default:
			return []shared.TelemetryEvent{buildRawEnvelope(rootPayload, telemetryHookSchema, strings.TrimSpace(hook))}, nil
		}
	}
	if _, ok := root["type"]; ok {
		return parseEventJSON([]byte(trimmed), decodeJSONMap([]byte(trimmed)), true)
	}

	return []shared.TelemetryEvent{buildRawEnvelope(rootPayload, telemetryHookSchema, "")}, nil
}

func parseEventJSON(raw []byte, rawPayload map[string]any, includeUnknown bool) ([]shared.TelemetryEvent, error) {
	var ev eventEnvelope
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("decode opencode event: %w", err)
	}

	switch eventType := telemetryEventType(ev); eventType {
	case "message.updated":
		props, ok := decodeMessageUpdatedProps(ev.Properties)
		if !ok {
			return nil, fmt.Errorf("decode message.updated properties")
		}
		info := props.Info
		if strings.ToLower(strings.TrimSpace(info.Role)) != "assistant" {
			if includeUnknown {
				return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryEventSchema, eventType)}, nil
			}
			return nil, nil
		}
		if strings.TrimSpace(info.ID) == "" {
			if includeUnknown {
				return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryEventSchema, eventType)}, nil
			}
			return nil, nil
		}
		event := buildJSONLMessageUsageEvent("", 0, info)
		event.Channel = shared.TelemetryChannelHook
		event.Payload = mergePayload(rawPayload, map[string]any{"event_type": "message.updated"})
		return []shared.TelemetryEvent{event}, nil
	case "tool.execute.after":
		payload, _, ok := decodeToolPayload(ev.Payload)
		if !ok || strings.TrimSpace(payload.ToolCallID) == "" {
			if includeUnknown {
				return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryEventSchema, eventType)}, nil
			}
			return nil, nil
		}
		event := buildJSONLToolUsageEvent("", 0, payload, nil)
		event.Channel = shared.TelemetryChannelHook
		event.OccurredAt = hookTimestampOrNow(payload.Timestamp)
		event.Payload = mergePayload(rawPayload, map[string]any{"event_type": "tool.execute.after"})
		return []shared.TelemetryEvent{event}, nil
	}

	if includeUnknown {
		return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryEventSchema, telemetryEventType(ev))}, nil
	}
	return nil, nil
}

func parseToolExecuteAfterHook(root map[string]json.RawMessage, rawPayload map[string]any) ([]shared.TelemetryEvent, error) {
	var input hookToolExecuteAfterInput
	if rawInput, ok := root["input"]; ok {
		if err := json.Unmarshal(rawInput, &input); err != nil {
			return nil, fmt.Errorf("decode tool.execute.after hook input: %w", err)
		}
	}
	var output hookToolExecuteAfterOutput
	if rawOutput, ok := root["output"]; ok {
		_ = json.Unmarshal(rawOutput, &output)
	}

	toolCallID := strings.TrimSpace(input.CallID)
	if toolCallID == "" {
		return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryHookSchema, "tool.execute.after")}, nil
	}

	return []shared.TelemetryEvent{{
		SchemaVersion: telemetryHookSchema,
		Channel:       shared.TelemetryChannelHook,
		OccurredAt:    parseHookTimestamp(root),
		SessionID:     strings.TrimSpace(input.SessionID),
		ToolCallID:    toolCallID,
		ProviderID:    "opencode",
		AgentName:     "opencode",
		EventType:     shared.TelemetryEventTypeToolUsage,
		ToolName:      strings.ToLower(core.FirstNonEmpty(strings.TrimSpace(input.Tool), "unknown")),
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(1),
		},
		Status: shared.TelemetryStatusOK,
		Payload: mergePayload(rawPayload, map[string]any{
			"hook":  "tool.execute.after",
			"title": strings.TrimSpace(output.Title),
		}),
	}}, nil
}

func parseChatMessageHook(root map[string]json.RawMessage, rawPayload map[string]any) ([]shared.TelemetryEvent, error) {
	var input hookChatMessageInput
	if rawInput, ok := root["input"]; ok {
		if err := json.Unmarshal(rawInput, &input); err != nil {
			return nil, fmt.Errorf("decode chat.message hook input: %w", err)
		}
	}
	var output hookChatMessageOutput
	if rawOutput, ok := root["output"]; ok {
		_ = json.Unmarshal(rawOutput, &output)
	}
	var outputMap map[string]any
	if rawOutput, ok := root["output"]; ok {
		_ = json.Unmarshal(rawOutput, &outputMap)
	}

	sessionID := core.FirstNonEmpty(input.SessionID, output.Message.SessionID)
	turnID := core.FirstNonEmpty(input.MessageID, output.Message.ID)
	messageID := core.FirstNonEmpty(output.Message.ID, input.MessageID)
	outputProviderID := shared.FirstPathString(outputMap,
		[]string{"message", "model", "providerID"},
		[]string{"message", "model", "provider_id"},
		[]string{"message", "info", "providerID"},
		[]string{"message", "info", "provider_id"},
		[]string{"message", "info", "model", "providerID"},
		[]string{"message", "info", "model", "provider_id"},
		[]string{"model", "providerID"},
		[]string{"model", "provider_id"},
		[]string{"providerID"},
		[]string{"provider_id"},
		[]string{"message", "providerID"},
		[]string{"message", "provider_id"},
	)
	outputModelID := shared.FirstPathString(outputMap,
		[]string{"message", "model", "modelID"},
		[]string{"message", "model", "model_id"},
		[]string{"message", "info", "modelID"},
		[]string{"message", "info", "model_id"},
		[]string{"message", "info", "model", "modelID"},
		[]string{"message", "info", "model", "model_id"},
		[]string{"model", "modelID"},
		[]string{"model", "model_id"},
		[]string{"modelID"},
		[]string{"model_id"},
		[]string{"message", "modelID"},
		[]string{"message", "model_id"},
	)
	u := extractUsage(outputMap)
	providerID := core.FirstNonEmpty(outputProviderID, input.Model.ProviderID, "opencode")
	modelRaw := strings.TrimSpace(outputModelID)
	if !hasUsage(u) {
		modelRaw = core.FirstNonEmpty(outputModelID, strings.TrimSpace(input.Model.ModelID))
	}
	upstreamProvider := extractHookUpstreamProvider(outputMap, outputProviderID)
	contextSummary := extractContextSummary(outputMap)

	if turnID == "" && sessionID == "" {
		return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryHookSchema, "chat.message")}, nil
	}

	normalized := map[string]any{
		"hook":        "chat.message",
		"agent":       strings.TrimSpace(input.Agent),
		"variant":     strings.TrimSpace(input.Variant),
		"parts_count": output.PartsCount,
		"context":     contextSummary,
	}
	if upstreamProvider != "" {
		normalized["upstream_provider"] = upstreamProvider
	}

	return []shared.TelemetryEvent{{
		SchemaVersion: telemetryHookSchema,
		Channel:       shared.TelemetryChannelHook,
		OccurredAt:    parseHookTimestamp(root),
		SessionID:     sessionID,
		TurnID:        turnID,
		MessageID:     messageID,
		ProviderID:    providerID,
		AgentName:     "opencode",
		EventType:     shared.TelemetryEventTypeMessageUsage,
		ModelRaw:      modelRaw,
		TokenUsage: core.TokenUsage{
			InputTokens:      u.InputTokens,
			OutputTokens:     u.OutputTokens,
			ReasoningTokens:  u.ReasoningTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
			TotalTokens:      u.TotalTokens,
			CostUSD:          u.CostUSD,
			Requests:         core.Int64Ptr(1),
		},
		Status:  shared.TelemetryStatusOK,
		Payload: mergePayload(rawPayload, normalized),
	}}, nil
}

func extractHookUpstreamProvider(outputMap map[string]any, outputProviderID string) string {
	upstreamProvider := sanitizeUpstreamProviderCandidate(core.FirstNonEmpty(
		shared.FirstPathString(outputMap,
			[]string{"upstream_provider"},
			[]string{"upstreamProvider"},
			[]string{"route", "provider_name"},
			[]string{"route", "providerName"},
			[]string{"route", "provider"},
			[]string{"router", "provider_name"},
			[]string{"router", "providerName"},
			[]string{"router", "provider"},
			[]string{"routing", "provider_name"},
			[]string{"routing", "providerName"},
			[]string{"routing", "provider"},
			[]string{"endpoint", "provider_name"},
			[]string{"endpoint", "providerName"},
			[]string{"endpoint", "provider"},
			[]string{"provider_name"},
			[]string{"providerName"},
			[]string{"provider"},
			[]string{"message", "provider_name"},
			[]string{"message", "providerName"},
			[]string{"message", "provider"},
			[]string{"message", "info", "provider_name"},
			[]string{"message", "info", "providerName"},
			[]string{"message", "info", "provider"},
		),
	))
	if upstreamProvider != "" {
		return upstreamProvider
	}
	return sanitizeUpstreamProviderCandidate(core.FirstNonEmpty(
		shared.FirstPathString(outputMap,
			[]string{"message", "model", "provider"},
			[]string{"message", "model", "provider_name"},
			[]string{"message", "model", "providerName"},
			[]string{"model", "provider"},
			[]string{"model", "provider_name"},
			[]string{"model", "providerName"},
		),
		outputProviderID,
	))
}

func sanitizeUpstreamProviderCandidate(value string) string {
	name := strings.TrimSpace(value)
	if name == "" {
		return ""
	}
	clean := strings.ToLower(name)
	switch clean {
	case "openrouter", "openusage", "opencode", "unknown":
		return ""
	default:
		return clean
	}
}

func extractUpstreamProviderFromMaps(payloads ...map[string]any) string {
	for _, payload := range payloads {
		if len(payload) == 0 {
			continue
		}
		if candidate := extractHookUpstreamProvider(payload, shared.FirstPathString(payload, []string{"model", "providerID"})); candidate != "" {
			return candidate
		}

		rawResponseBody := core.FirstNonEmpty(
			shared.FirstPathString(payload, []string{"error", "data", "responseBody"}),
			shared.FirstPathString(payload, []string{"error", "responseBody"}),
		)
		if rawResponseBody == "" {
			continue
		}
		responseBodyPayload := decodeJSONMap([]byte(rawResponseBody))
		candidate := sanitizeUpstreamProviderCandidate(core.FirstNonEmpty(
			shared.FirstPathString(responseBodyPayload,
				[]string{"error", "metadata", "provider_name"},
				[]string{"error", "metadata", "providerName"},
				[]string{"metadata", "provider_name"},
				[]string{"metadata", "providerName"},
				[]string{"metadata", "provider"},
				[]string{"provider_name"},
				[]string{"providerName"},
				[]string{"provider"},
			),
		))
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func buildRawEnvelope(rawPayload map[string]any, schemaVersion, detectedType string) shared.TelemetryEvent {
	return shared.TelemetryEvent{
		SchemaVersion: schemaVersion,
		Channel:       shared.TelemetryChannelHook,
		OccurredAt:    parseHookTimestampAny(rawPayload),
		WorkspaceID: shared.SanitizeWorkspace(shared.FirstPathString(rawPayload,
			[]string{"workspace_id"},
			[]string{"workspaceID"},
			[]string{"event", "properties", "info", "path", "cwd"},
		)),
		SessionID: shared.FirstPathString(rawPayload,
			[]string{"session_id"},
			[]string{"sessionID"},
			[]string{"input", "sessionID"},
			[]string{"output", "message", "sessionID"},
			[]string{"event", "properties", "info", "sessionID"},
		),
		TurnID: shared.FirstPathString(rawPayload,
			[]string{"turn_id"},
			[]string{"turnID"},
			[]string{"input", "messageID"},
			[]string{"output", "message", "id"},
			[]string{"event", "properties", "info", "parentID"},
		),
		MessageID: shared.FirstPathString(rawPayload,
			[]string{"message_id"},
			[]string{"messageID"},
			[]string{"input", "messageID"},
			[]string{"output", "message", "id"},
			[]string{"event", "properties", "info", "id"},
		),
		ToolCallID: shared.FirstPathString(rawPayload,
			[]string{"tool_call_id"},
			[]string{"toolCallID"},
			[]string{"input", "callID"},
			[]string{"event", "payload", "toolCallID"},
		),
		ProviderID: core.FirstNonEmpty(
			shared.FirstPathString(rawPayload,
				[]string{"provider_id"},
				[]string{"providerID"},
				[]string{"input", "model", "providerID"},
				[]string{"output", "message", "model", "providerID"},
				[]string{"output", "model", "providerID"},
				[]string{"model", "providerID"},
				[]string{"event", "properties", "info", "providerID"},
			),
			"opencode",
		),
		AgentName: "opencode",
		EventType: shared.TelemetryEventTypeRawEnvelope,
		ModelRaw: shared.FirstPathString(rawPayload,
			[]string{"model_id"},
			[]string{"modelID"},
			[]string{"input", "model", "modelID"},
			[]string{"output", "message", "model", "modelID"},
			[]string{"output", "model", "modelID"},
			[]string{"model", "modelID"},
			[]string{"event", "properties", "info", "modelID"},
		),
		Status: shared.TelemetryStatusUnknown,
		Payload: mergePayload(rawPayload, map[string]any{
			"captured_as": "raw_envelope",
			"detected_event": core.FirstNonEmpty(
				detectedType,
				shared.FirstPathString(rawPayload, []string{"hook"}),
				shared.FirstPathString(rawPayload, []string{"type"}),
				shared.FirstPathString(rawPayload, []string{"event"}),
			),
		}),
	}
}

func mapToolStatus(status string) (shared.TelemetryStatus, bool) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "", "completed", "complete", "success", "succeeded":
		return shared.TelemetryStatusOK, true
	case "error", "failed", "failure":
		return shared.TelemetryStatusError, true
	case "aborted", "cancelled", "canceled", "terminated":
		return shared.TelemetryStatusAborted, true
	case "running", "pending", "queued", "in_progress", "in-progress":
		return shared.TelemetryStatusUnknown, false
	default:
		return shared.TelemetryStatusUnknown, true
	}
}

func mapMessageStatus(reason string) shared.TelemetryStatus {
	reason = strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(reason, "error"), strings.Contains(reason, "fail"):
		return shared.TelemetryStatusError
	case strings.Contains(reason, "abort"), strings.Contains(reason, "cancel"):
		return shared.TelemetryStatusAborted
	default:
		return shared.TelemetryStatusOK
	}
}

func appendDedupTelemetryEvents(
	out *[]shared.TelemetryEvent,
	events []shared.TelemetryEvent,
	seenMessage map[string]bool,
	seenTools map[string]bool,
	accountID string,
) {
	for _, ev := range events {
		ev.AccountID = core.FirstNonEmpty(accountID, ev.AccountID)
		switch ev.EventType {
		case shared.TelemetryEventTypeToolUsage:
			key := core.FirstNonEmpty(strings.TrimSpace(ev.ToolCallID))
			if key == "" {
				key = core.FirstNonEmpty(strings.TrimSpace(ev.SessionID), strings.TrimSpace(ev.MessageID)) + "|" + strings.ToLower(strings.TrimSpace(ev.ToolName))
			}
			if key != "" {
				if seenTools[key] {
					continue
				}
				seenTools[key] = true
			}
		case shared.TelemetryEventTypeMessageUsage:
			key := core.FirstNonEmpty(strings.TrimSpace(ev.MessageID))
			if key == "" {
				key = core.FirstNonEmpty(strings.TrimSpace(ev.SessionID), strings.TrimSpace(ev.TurnID))
			}
			if key != "" {
				if seenMessage[key] {
					continue
				}
				seenMessage[key] = true
			}
		}
		*out = append(*out, ev)
	}
}

func hasUsage(u usage) bool {
	for _, value := range []*int64{
		u.InputTokens, u.OutputTokens, u.ReasoningTokens, u.CacheReadTokens, u.CacheWriteTokens, u.TotalTokens,
	} {
		if value != nil && *value > 0 {
			return true
		}
	}
	return u.CostUSD != nil && *u.CostUSD > 0
}

func extractUsage(output map[string]any) usage {
	if len(output) == 0 {
		return usage{}
	}
	input := shared.FirstPathNumber(output,
		[]string{"usage", "input_tokens"}, []string{"usage", "inputTokens"}, []string{"usage", "input"},
		[]string{"message", "usage", "input_tokens"}, []string{"message", "usage", "inputTokens"}, []string{"message", "usage", "input"},
		[]string{"tokens", "input"}, []string{"input_tokens"}, []string{"inputTokens"},
	)
	outputTokens := shared.FirstPathNumber(output,
		[]string{"usage", "output_tokens"}, []string{"usage", "outputTokens"}, []string{"usage", "output"},
		[]string{"message", "usage", "output_tokens"}, []string{"message", "usage", "outputTokens"}, []string{"message", "usage", "output"},
		[]string{"tokens", "output"}, []string{"output_tokens"}, []string{"outputTokens"},
	)
	reasoning := shared.FirstPathNumber(output,
		[]string{"usage", "reasoning_tokens"}, []string{"usage", "reasoningTokens"}, []string{"usage", "reasoning"},
		[]string{"message", "usage", "reasoning_tokens"}, []string{"message", "usage", "reasoningTokens"}, []string{"message", "usage", "reasoning"},
		[]string{"tokens", "reasoning"}, []string{"reasoning_tokens"}, []string{"reasoningTokens"},
	)
	cacheRead := shared.FirstPathNumber(output,
		[]string{"usage", "cache_read_input_tokens"}, []string{"usage", "cacheReadInputTokens"}, []string{"usage", "cache_read_tokens"},
		[]string{"usage", "cacheReadTokens"}, []string{"usage", "cache", "read"},
		[]string{"message", "usage", "cache_read_input_tokens"}, []string{"message", "usage", "cacheReadInputTokens"}, []string{"message", "usage", "cache", "read"},
		[]string{"tokens", "cache", "read"},
	)
	cacheWrite := shared.FirstPathNumber(output,
		[]string{"usage", "cache_creation_input_tokens"}, []string{"usage", "cacheCreationInputTokens"}, []string{"usage", "cache_write_tokens"},
		[]string{"usage", "cacheWriteTokens"}, []string{"usage", "cache", "write"},
		[]string{"message", "usage", "cache_creation_input_tokens"}, []string{"message", "usage", "cacheCreationInputTokens"}, []string{"message", "usage", "cache", "write"},
		[]string{"tokens", "cache", "write"},
	)
	total := shared.FirstPathNumber(output,
		[]string{"usage", "total_tokens"}, []string{"usage", "totalTokens"}, []string{"usage", "total"},
		[]string{"message", "usage", "total_tokens"}, []string{"message", "usage", "totalTokens"}, []string{"message", "usage", "total"},
		[]string{"tokens", "total"}, []string{"total_tokens"}, []string{"totalTokens"},
	)
	cost := shared.FirstPathNumber(output,
		[]string{"usage", "cost_usd"}, []string{"usage", "costUSD"}, []string{"usage", "cost"},
		[]string{"message", "usage", "cost_usd"}, []string{"message", "usage", "costUSD"}, []string{"message", "usage", "cost"},
		[]string{"cost_usd"}, []string{"costUSD"}, []string{"cost"},
	)

	result := usage{
		InputTokens:      shared.NumberToInt64Ptr(input),
		OutputTokens:     shared.NumberToInt64Ptr(outputTokens),
		ReasoningTokens:  shared.NumberToInt64Ptr(reasoning),
		CacheReadTokens:  shared.NumberToInt64Ptr(cacheRead),
		CacheWriteTokens: shared.NumberToInt64Ptr(cacheWrite),
		TotalTokens:      shared.NumberToInt64Ptr(total),
		CostUSD:          shared.NumberToFloat64Ptr(cost),
	}
	if result.TotalTokens == nil {
		combined := int64(0)
		hasAny := false
		for _, ptr := range []*int64{result.InputTokens, result.OutputTokens, result.ReasoningTokens, result.CacheReadTokens, result.CacheWriteTokens} {
			if ptr != nil {
				combined += *ptr
				hasAny = true
			}
		}
		if hasAny {
			result.TotalTokens = core.Int64Ptr(combined)
		}
	}
	return result
}

func extractContextSummary(output map[string]any) map[string]any {
	if len(output) == 0 {
		return map[string]any{}
	}
	partsTotal := shared.FirstPathNumber(output, []string{"context", "parts_total"}, []string{"context", "partsTotal"}, []string{"parts_count"})
	partsByType := map[string]any{}
	if m, ok := shared.PathMap(output, "context", "parts_by_type"); ok {
		for key, value := range m {
			if count, ok := shared.NumberFromAny(value); ok {
				partsByType[strings.TrimSpace(key)] = int64(count)
			}
		}
	}
	if len(partsByType) == 0 {
		if arr, ok := shared.PathSlice(output, "parts"); ok {
			typeCounts := make(map[string]int64)
			for _, part := range arr {
				partMap, ok := part.(map[string]any)
				if !ok {
					typeCounts["unknown"]++
					continue
				}
				partType := "unknown"
				if rawType, ok := partMap["type"].(string); ok && strings.TrimSpace(rawType) != "" {
					partType = strings.TrimSpace(rawType)
				}
				typeCounts[partType]++
			}
			for key, value := range typeCounts {
				partsByType[key] = value
			}
			if partsTotal == nil {
				v := float64(len(arr))
				partsTotal = &v
			}
		}
	}
	return map[string]any{
		"parts_total":   ptrInt64Value(shared.NumberToInt64Ptr(partsTotal)),
		"parts_by_type": partsByType,
	}
}

func decodeRawMessageMap(root map[string]json.RawMessage) map[string]any {
	out := make(map[string]any, len(root))
	for key, raw := range root {
		if len(raw) == 0 {
			out[key] = nil
			continue
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			out[key] = string(raw)
			continue
		}
		out[key] = decoded
	}
	return out
}

func decodeJSONMap(raw []byte) map[string]any {
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err == nil && len(out) > 0 {
		return out
	}
	return map[string]any{"_raw_json": string(raw)}
}

func mergePayload(rawPayload map[string]any, normalized map[string]any) map[string]any {
	if len(rawPayload) == 0 && len(normalized) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, 8)
	if len(normalized) > 0 {
		out["_normalized"] = normalized
		for key, value := range normalized {
			if _, exists := out[key]; !exists {
				out[key] = value
			}
		}
	}
	rawSummary := summarizeRawPayload(rawPayload)
	if len(rawSummary) > 0 {
		out["_raw"] = rawSummary
		for key, value := range rawSummary {
			if _, exists := out[key]; !exists {
				out[key] = value
			}
		}
	}
	return out
}

func summarizeRawPayload(rawPayload map[string]any) map[string]any {
	if len(rawPayload) == 0 {
		return map[string]any{}
	}
	out := map[string]any{"raw_keys": len(rawPayload)}
	if hook := shared.FirstPathString(rawPayload, []string{"hook"}); hook != "" {
		out["hook"] = hook
	}
	if typ := shared.FirstPathString(rawPayload, []string{"type"}); typ != "" {
		out["type"] = typ
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"hook"}),
		shared.FirstPathString(rawPayload, []string{"event"}),
		shared.FirstPathString(rawPayload, []string{"type"}),
	); value != "" {
		out["event"] = value
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"sessionID"}),
		shared.FirstPathString(rawPayload, []string{"session_id"}),
		shared.FirstPathString(rawPayload, []string{"input", "sessionID"}),
		shared.FirstPathString(rawPayload, []string{"output", "message", "sessionID"}),
	); value != "" {
		out["session_id"] = value
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"messageID"}),
		shared.FirstPathString(rawPayload, []string{"message_id"}),
		shared.FirstPathString(rawPayload, []string{"input", "messageID"}),
		shared.FirstPathString(rawPayload, []string{"output", "message", "id"}),
	); value != "" {
		out["message_id"] = value
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"toolCallID"}),
		shared.FirstPathString(rawPayload, []string{"tool_call_id"}),
		shared.FirstPathString(rawPayload, []string{"input", "callID"}),
	); value != "" {
		out["tool_call_id"] = value
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"providerID"}),
		shared.FirstPathString(rawPayload, []string{"provider_id"}),
		shared.FirstPathString(rawPayload, []string{"input", "model", "providerID"}),
		shared.FirstPathString(rawPayload, []string{"output", "message", "model", "providerID"}),
	); value != "" {
		out["provider_id"] = value
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"modelID"}),
		shared.FirstPathString(rawPayload, []string{"model_id"}),
		shared.FirstPathString(rawPayload, []string{"input", "model", "modelID"}),
		shared.FirstPathString(rawPayload, []string{"output", "message", "model", "modelID"}),
	); value != "" {
		out["model_id"] = value
	}
	if ts := shared.FirstPathString(rawPayload, []string{"timestamp"}, []string{"time"}); ts != "" {
		out["timestamp"] = ts
	}
	return out
}

func ptrInt64Value(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func parseHookTimestampAny(root map[string]any) time.Time {
	if root == nil {
		return time.Now().UTC()
	}
	if ts := shared.FirstPathNumber(root,
		[]string{"timestamp"},
		[]string{"time"},
		[]string{"event", "timestamp"},
		[]string{"event", "properties", "info", "time", "completed"},
		[]string{"event", "properties", "info", "time", "created"},
	); ts != nil && *ts > 0 {
		return hookTimestampOrNow(int64(*ts))
	}
	if raw := shared.FirstPathString(root, []string{"timestamp"}, []string{"time"}, []string{"event", "timestamp"}); raw != "" {
		if ts, ok := shared.ParseFlexibleTimestamp(raw); ok {
			return shared.UnixAuto(ts)
		}
	}
	return time.Now().UTC()
}

func parseHookTimestamp(root map[string]json.RawMessage) time.Time {
	if raw, ok := root["timestamp"]; ok {
		var intVal int64
		if err := json.Unmarshal(raw, &intVal); err == nil && intVal > 0 {
			return hookTimestampOrNow(intVal)
		}
		var strVal string
		if err := json.Unmarshal(raw, &strVal); err == nil {
			if ts, ok := shared.ParseFlexibleTimestamp(strVal); ok {
				return shared.UnixAuto(ts)
			}
		}
	}
	return time.Now().UTC()
}

func hookTimestampOrNow(ts int64) time.Time {
	if ts <= 0 {
		return time.Now().UTC()
	}
	return shared.UnixAuto(ts)
}

func ptrInt64FromFloat(v *float64) int64 {
	if v == nil {
		return 0
	}
	return int64(*v)
}
