package copilot

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

type copilotTelemetrySessionState struct {
	path               string
	sessionID          string
	currentModel       string
	workspaceID        string
	repo               string
	cwd                string
	clientLabel        string
	turnIndex          int
	assistantUsageSeen bool
	toolContexts       map[string]copilotTelemetryToolContext
}

// parseCopilotTelemetrySessionFile parses a single session's events.jsonl and
// produces telemetry events from assistant.usage and assistant.message entries.
func parseCopilotTelemetrySessionFile(path, sessionID string) ([]shared.TelemetryEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	state := copilotTelemetrySessionState{
		path:         path,
		sessionID:    sessionID,
		clientLabel:  "cli",
		toolContexts: make(map[string]copilotTelemetryToolContext),
	}

	lines := strings.Split(string(data), "\n")
	out := make([]shared.TelemetryEvent, 0, len(lines))
	for lineNum, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt sessionEvent
		if json.Unmarshal([]byte(line), &evt) != nil {
			continue
		}
		occurredAt := time.Now().UTC()
		if ts := shared.FlexParseTime(evt.Timestamp); !ts.IsZero() {
			occurredAt = ts
		}
		appendSessionEvents(&out, &state, lineNum+1, evt, occurredAt)
	}

	return out, nil
}

func appendSessionEvents(out *[]shared.TelemetryEvent, state *copilotTelemetrySessionState, lineNum int, evt sessionEvent, occurredAt time.Time) {
	switch evt.Type {
	case "session.start":
		state.applyStart(evt.Data)
	case "session.context_changed":
		state.applyContextChanged(evt.Data)
	case "session.model_change":
		state.applyModelChange(evt.Data)
	case "session.info":
		state.applySessionInfo(evt.Data)
	case "assistant.message":
		appendAssistantMessageEvents(out, state, lineNum, evt, occurredAt)
	case "tool.execution_start":
		appendToolExecutionStartEvent(out, state, lineNum, evt.Data, occurredAt)
	case "tool.execution_complete":
		appendToolExecutionCompleteEvent(out, state, lineNum, evt.Data, occurredAt)
	case "session.workspace_file_changed":
		appendWorkspaceFileChangedEvent(out, state, lineNum, evt.Data, occurredAt)
	case "assistant.turn_start":
		return
	case "assistant.turn_end":
		appendSyntheticTurnEndEvent(out, state, lineNum, evt.ID, occurredAt)
	case "assistant.usage":
		appendAssistantUsageEvent(out, state, lineNum, evt.ID, evt.Data, occurredAt)
	case "session.shutdown":
		appendSessionShutdownEvents(out, state, lineNum, evt.ID, evt.Data, occurredAt)
	}
}

func (s *copilotTelemetrySessionState) applyStart(raw json.RawMessage) {
	var start sessionStartData
	if json.Unmarshal(raw, &start) != nil {
		return
	}
	s.applyContext(start.Context.Repository, start.Context.CWD)
	if s.currentModel == "" && start.SelectedModel != "" {
		s.currentModel = start.SelectedModel
	}
}

func (s *copilotTelemetrySessionState) applyContextChanged(raw json.RawMessage) {
	var changed copilotTelemetrySessionContextChangedData
	if json.Unmarshal(raw, &changed) != nil {
		return
	}
	s.applyContext(changed.Repository, changed.CWD)
}

func (s *copilotTelemetrySessionState) applyContext(repository, cwd string) {
	if repository != "" {
		s.repo = repository
	}
	if cwd != "" {
		s.cwd = cwd
		s.workspaceID = shared.SanitizeWorkspace(cwd)
	}
	s.clientLabel = normalizeCopilotClient(s.repo, s.cwd)
}

func (s *copilotTelemetrySessionState) applyModelChange(raw json.RawMessage) {
	var mc modelChangeData
	if json.Unmarshal(raw, &mc) == nil && mc.NewModel != "" {
		s.currentModel = mc.NewModel
	}
}

func (s *copilotTelemetrySessionState) applySessionInfo(raw json.RawMessage) {
	var info sessionInfoData
	if json.Unmarshal(raw, &info) == nil && info.InfoType == "model" {
		if model := extractModelFromInfoMsg(info.Message); model != "" {
			s.currentModel = model
		}
	}
}

func appendAssistantMessageEvents(out *[]shared.TelemetryEvent, state *copilotTelemetrySessionState, lineNum int, evt sessionEvent, occurredAt time.Time) {
	var msg copilotTelemetryAssistantMessageData
	if json.Unmarshal(evt.Data, &msg) != nil {
		return
	}

	var toolRequests []json.RawMessage
	if json.Unmarshal(msg.ToolRequests, &toolRequests) != nil || len(toolRequests) == 0 {
		return
	}

	messageID := copilotTelemetryMessageID(state.sessionID, lineNum, msg.MessageID, evt.ID)
	turnID := core.FirstNonEmpty(messageID, fmt.Sprintf("%s:line:%d", state.sessionID, lineNum))

	for reqIdx, rawReq := range toolRequests {
		req, ok := parseCopilotTelemetryToolRequest(rawReq)
		if !ok {
			continue
		}
		appendAssistantToolRequestEvent(out, state, lineNum, occurredAt, messageID, turnID, reqIdx, rawReq, req)
	}
}

func appendAssistantToolRequestEvent(
	out *[]shared.TelemetryEvent,
	state *copilotTelemetrySessionState,
	lineNum int,
	occurredAt time.Time,
	messageID, turnID string,
	reqIdx int,
	rawReq json.RawMessage,
	req copilotTelemetryToolRequest,
) {
	explicitCallID := strings.TrimSpace(req.ToolCallID) != ""
	toolCallID := strings.TrimSpace(req.ToolCallID)
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("%s:%d:tool:%d", state.sessionID, lineNum, reqIdx+1)
	}

	toolName, toolMeta := normalizeCopilotTelemetryToolName(req.RawName)
	if toolName == "" {
		toolName = "unknown"
	}
	payload := copilotTelemetryBasePayload(state.path, lineNum, state.clientLabel, state.repo, state.cwd, "assistant.message.tool_request")
	for key, value := range toolMeta {
		payload[key] = value
	}
	payload["tool_call_id"] = toolCallID

	applyTelemetryToolInputPayload(payload, req.Input)
	applyTelemetryFallbackPayload(payload, rawReq)

	model := currentOrUnknownModel(state.currentModel)
	if upstream := copilotUpstreamProviderForModel(model); upstream != "" {
		payload["upstream_provider"] = upstream
	}

	*out = append(*out, shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       shared.TelemetryChannelJSONL,
		OccurredAt:    occurredAt,
		AccountID:     "copilot",
		WorkspaceID:   state.workspaceID,
		SessionID:     state.sessionID,
		TurnID:        turnID,
		MessageID:     messageID,
		ToolCallID:    toolCallID,
		ProviderID:    "copilot",
		AgentName:     "copilot",
		EventType:     shared.TelemetryEventTypeToolUsage,
		ModelRaw:      model,
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(1),
		},
		ToolName: toolName,
		Status:   shared.TelemetryStatusUnknown,
		Payload:  payload,
	})

	if explicitCallID {
		state.toolContexts[toolCallID] = copilotTelemetryToolContext{
			MessageID: messageID,
			TurnID:    turnID,
			Model:     model,
			ToolName:  toolName,
			Payload:   copyCopilotTelemetryPayload(payload),
		}
	}
}

func applyTelemetryToolInputPayload(payload map[string]any, input any) {
	if input == nil {
		return
	}
	payload["tool_input"] = input
	if cmd := extractCopilotTelemetryCommand(input); cmd != "" {
		payload["command"] = cmd
	}
	if paths := shared.ExtractFilePathsFromPayload(input); len(paths) > 0 {
		payload["file"] = paths[0]
		if lang := inferCopilotLanguageFromPath(paths[0]); lang != "" {
			payload["language"] = lang
		}
	}
	if added, removed := estimateCopilotTelemetryLineDelta(input); added > 0 || removed > 0 {
		payload["lines_added"] = added
		payload["lines_removed"] = removed
	}
}

func applyTelemetryFallbackPayload(payload map[string]any, rawReq json.RawMessage) {
	if _, ok := payload["command"]; !ok {
		if cmd := extractCopilotToolCommand(rawReq); cmd != "" {
			payload["command"] = cmd
		}
	}
	if _, ok := payload["file"]; !ok {
		if paths := extractCopilotToolPaths(rawReq); len(paths) > 0 {
			payload["file"] = paths[0]
			if lang := inferCopilotLanguageFromPath(paths[0]); lang != "" {
				payload["language"] = lang
			}
		}
	}
	if _, ok := payload["lines_added"]; !ok {
		added, removed := estimateCopilotToolLineDelta(rawReq)
		if added > 0 || removed > 0 {
			payload["lines_added"] = added
			payload["lines_removed"] = removed
		}
	}
}

func appendToolExecutionStartEvent(out *[]shared.TelemetryEvent, state *copilotTelemetrySessionState, lineNum int, raw json.RawMessage, occurredAt time.Time) {
	var start copilotTelemetryToolExecutionStartData
	if json.Unmarshal(raw, &start) != nil {
		return
	}

	explicitCallID := strings.TrimSpace(start.ToolCallID) != ""
	toolCallID := strings.TrimSpace(start.ToolCallID)
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("%s:%d:tool_start", state.sessionID, lineNum)
	}

	ctx := state.toolContexts[toolCallID]
	payload := copyCopilotTelemetryPayload(ctx.Payload)
	if len(payload) == 0 {
		payload = copilotTelemetryBasePayload(state.path, lineNum, state.clientLabel, state.repo, state.cwd, "tool.execution_start")
	} else {
		payload["event"] = "tool.execution_start"
		payload["line"] = lineNum
	}
	payload["tool_call_id"] = toolCallID

	toolName := strings.TrimSpace(ctx.ToolName)
	if start.ToolName != "" {
		normalized, meta := normalizeCopilotTelemetryToolName(start.ToolName)
		toolName = normalized
		for key, value := range meta {
			payload[key] = value
		}
	}
	if toolName == "" {
		toolName = "unknown"
	}

	if args := decodeCopilotTelemetryJSONAny(start.Arguments); args != nil {
		applyTelemetryToolInputPayload(payload, args)
	}

	model := currentOrUnknownModel(core.FirstNonEmpty(strings.TrimSpace(ctx.Model), strings.TrimSpace(state.currentModel)))
	if upstream := copilotUpstreamProviderForModel(model); upstream != "" {
		payload["upstream_provider"] = upstream
	}

	messageID := core.FirstNonEmpty(ctx.MessageID, fmt.Sprintf("%s:%d", state.sessionID, lineNum))
	turnID := core.FirstNonEmpty(ctx.TurnID, messageID)
	appendToolExecutionEvent(out, state, occurredAt, messageID, turnID, toolCallID, model, toolName, shared.TelemetryStatusUnknown, payload)

	if explicitCallID {
		state.toolContexts[toolCallID] = copilotTelemetryToolContext{
			MessageID: messageID,
			TurnID:    turnID,
			Model:     model,
			ToolName:  toolName,
			Payload:   copyCopilotTelemetryPayload(payload),
		}
	}
}

func appendToolExecutionCompleteEvent(out *[]shared.TelemetryEvent, state *copilotTelemetrySessionState, lineNum int, raw json.RawMessage, occurredAt time.Time) {
	var complete copilotTelemetryToolExecutionCompleteData
	if json.Unmarshal(raw, &complete) != nil {
		return
	}

	toolCallID := strings.TrimSpace(complete.ToolCallID)
	explicitCallID := toolCallID != ""
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("%s:%d:tool_complete", state.sessionID, lineNum)
	}

	ctx := state.toolContexts[toolCallID]
	payload := copyCopilotTelemetryPayload(ctx.Payload)
	if len(payload) == 0 {
		payload = copilotTelemetryBasePayload(state.path, lineNum, state.clientLabel, state.repo, state.cwd, "tool.execution_complete")
	} else {
		payload["event"] = "tool.execution_complete"
		payload["line"] = lineNum
	}
	payload["tool_call_id"] = toolCallID

	toolName := strings.TrimSpace(ctx.ToolName)
	if complete.ToolName != "" {
		normalized, meta := normalizeCopilotTelemetryToolName(complete.ToolName)
		toolName = normalized
		for key, value := range meta {
			payload[key] = value
		}
	}
	if toolName == "" {
		toolName = "unknown"
	}
	if complete.Success != nil {
		payload["success"] = *complete.Success
	}
	if strings.TrimSpace(complete.Status) != "" {
		payload["status_raw"] = strings.TrimSpace(complete.Status)
	}
	for key, value := range summarizeCopilotTelemetryResult(complete.Result) {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	errorCode, errorMessage := summarizeCopilotTelemetryError(complete.Error)
	if errorCode != "" {
		payload["error_code"] = errorCode
	}
	if errorMessage != "" {
		payload["error_message"] = truncate(errorMessage, 240)
	}

	model := currentOrUnknownModel(core.FirstNonEmpty(strings.TrimSpace(ctx.Model), strings.TrimSpace(state.currentModel)))
	if upstream := copilotUpstreamProviderForModel(model); upstream != "" {
		payload["upstream_provider"] = upstream
	}

	messageID := core.FirstNonEmpty(ctx.MessageID, fmt.Sprintf("%s:%d", state.sessionID, lineNum))
	turnID := core.FirstNonEmpty(ctx.TurnID, messageID)
	status := copilotTelemetryToolStatus(complete.Success, complete.Status, errorCode, errorMessage)
	appendToolExecutionEvent(out, state, occurredAt, messageID, turnID, toolCallID, model, toolName, status, payload)

	if explicitCallID {
		state.toolContexts[toolCallID] = copilotTelemetryToolContext{
			MessageID: messageID,
			TurnID:    turnID,
			Model:     model,
			ToolName:  toolName,
			Payload:   copyCopilotTelemetryPayload(payload),
		}
	}
}

func appendToolExecutionEvent(
	out *[]shared.TelemetryEvent,
	state *copilotTelemetrySessionState,
	occurredAt time.Time,
	messageID, turnID, toolCallID, model, toolName string,
	status shared.TelemetryStatus,
	payload map[string]any,
) {
	*out = append(*out, shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       shared.TelemetryChannelJSONL,
		OccurredAt:    occurredAt,
		AccountID:     "copilot",
		WorkspaceID:   state.workspaceID,
		SessionID:     state.sessionID,
		TurnID:        turnID,
		MessageID:     messageID,
		ToolCallID:    toolCallID,
		ProviderID:    "copilot",
		AgentName:     "copilot",
		EventType:     shared.TelemetryEventTypeToolUsage,
		ModelRaw:      model,
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(1),
		},
		ToolName: toolName,
		Status:   status,
		Payload:  payload,
	})
}

func appendWorkspaceFileChangedEvent(out *[]shared.TelemetryEvent, state *copilotTelemetrySessionState, lineNum int, raw json.RawMessage, occurredAt time.Time) {
	var changed copilotTelemetryWorkspaceFileChangedData
	if json.Unmarshal(raw, &changed) != nil {
		return
	}
	filePath := strings.TrimSpace(changed.Path)
	if filePath == "" {
		return
	}

	op := sanitizeMetricName(changed.Operation)
	if op == "" || op == "unknown" {
		op = "change"
	}

	payload := copilotTelemetryBasePayload(state.path, lineNum, state.clientLabel, state.repo, state.cwd, "session.workspace_file_changed")
	payload["file"] = filePath
	payload["operation"] = strings.TrimSpace(changed.Operation)
	if lang := inferCopilotLanguageFromPath(filePath); lang != "" {
		payload["language"] = lang
	}

	model := currentOrUnknownModel(state.currentModel)
	if upstream := copilotUpstreamProviderForModel(model); upstream != "" {
		payload["upstream_provider"] = upstream
	}

	*out = append(*out, shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       shared.TelemetryChannelJSONL,
		OccurredAt:    occurredAt,
		AccountID:     "copilot",
		WorkspaceID:   state.workspaceID,
		SessionID:     state.sessionID,
		TurnID:        fmt.Sprintf("%s:file:%d", state.sessionID, lineNum),
		MessageID:     fmt.Sprintf("%s:%d", state.sessionID, lineNum),
		ProviderID:    "copilot",
		AgentName:     "copilot",
		EventType:     shared.TelemetryEventTypeToolUsage,
		ModelRaw:      model,
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(0),
		},
		ToolName: "workspace_file_" + op,
		Status:   shared.TelemetryStatusOK,
		Payload:  payload,
	})
}

func appendSyntheticTurnEndEvent(out *[]shared.TelemetryEvent, state *copilotTelemetrySessionState, lineNum int, evtID string, occurredAt time.Time) {
	state.turnIndex++
	if state.assistantUsageSeen || state.currentModel == "" {
		return
	}

	turnID := core.FirstNonEmpty(strings.TrimSpace(evtID), fmt.Sprintf("%s:synth:%d", state.sessionID, state.turnIndex))
	messageID := fmt.Sprintf("%s:%d", state.sessionID, lineNum)
	payload := copilotTelemetryBasePayload(state.path, lineNum, state.clientLabel, state.repo, state.cwd, "assistant.turn_end")
	payload["synthetic"] = true
	payload["upstream_provider"] = copilotUpstreamProviderForModel(state.currentModel)
	*out = append(*out, shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       shared.TelemetryChannelJSONL,
		OccurredAt:    occurredAt,
		AccountID:     "copilot",
		WorkspaceID:   state.workspaceID,
		SessionID:     state.sessionID,
		TurnID:        turnID,
		MessageID:     messageID,
		ProviderID:    "copilot",
		AgentName:     "copilot",
		EventType:     shared.TelemetryEventTypeMessageUsage,
		ModelRaw:      state.currentModel,
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(1),
		},
		Status:  shared.TelemetryStatusOK,
		Payload: payload,
	})
}

func appendAssistantUsageEvent(out *[]shared.TelemetryEvent, state *copilotTelemetrySessionState, lineNum int, evtID string, raw json.RawMessage, occurredAt time.Time) {
	var usage assistantUsageData
	if json.Unmarshal(raw, &usage) != nil {
		return
	}
	state.assistantUsageSeen = true

	model := core.FirstNonEmpty(usage.Model, state.currentModel)
	if model == "" {
		return
	}
	state.turnIndex++

	turnID := core.FirstNonEmpty(strings.TrimSpace(evtID), fmt.Sprintf("%s:usage:%d", state.sessionID, state.turnIndex))
	messageID := fmt.Sprintf("%s:%d", state.sessionID, lineNum)
	totalTokens := int64(usage.InputTokens + usage.OutputTokens)
	payload := copilotTelemetryBasePayload(state.path, lineNum, state.clientLabel, state.repo, state.cwd, "assistant.usage")
	payload["source_file"] = state.path
	payload["line"] = lineNum
	payload["client"] = state.clientLabel
	payload["upstream_provider"] = copilotUpstreamProviderForModel(model)
	if usage.Duration > 0 {
		payload["duration_ms"] = usage.Duration
	}
	if len(usage.QuotaSnapshots) > 0 {
		payload["quota_snapshot_count"] = len(usage.QuotaSnapshots)
	}

	event := shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       shared.TelemetryChannelJSONL,
		OccurredAt:    occurredAt,
		AccountID:     "copilot",
		WorkspaceID:   state.workspaceID,
		SessionID:     state.sessionID,
		TurnID:        turnID,
		MessageID:     messageID,
		ProviderID:    "copilot",
		AgentName:     "copilot",
		EventType:     shared.TelemetryEventTypeMessageUsage,
		ModelRaw:      model,
		TokenUsage: core.TokenUsage{
			InputTokens:  core.Int64Ptr(int64(usage.InputTokens)),
			OutputTokens: core.Int64Ptr(int64(usage.OutputTokens)),
			TotalTokens:  core.Int64Ptr(totalTokens),
			Requests:     core.Int64Ptr(1),
		},
		Status:  shared.TelemetryStatusOK,
		Payload: payload,
	}
	if usage.CacheReadTokens > 0 {
		event.CacheReadTokens = core.Int64Ptr(int64(usage.CacheReadTokens))
	}
	if usage.CacheWriteTokens > 0 {
		event.CacheWriteTokens = core.Int64Ptr(int64(usage.CacheWriteTokens))
	}
	if usage.Cost > 0 {
		event.CostUSD = core.Float64Ptr(usage.Cost)
	}
	*out = append(*out, event)
}

func appendSessionShutdownEvents(out *[]shared.TelemetryEvent, state *copilotTelemetrySessionState, lineNum int, evtID string, raw json.RawMessage, occurredAt time.Time) {
	var shutdown sessionShutdownData
	if json.Unmarshal(raw, &shutdown) != nil {
		return
	}

	shutdownTurnID := core.FirstNonEmpty(strings.TrimSpace(evtID), fmt.Sprintf("%s:shutdown", state.sessionID))
	shutdownMessageID := fmt.Sprintf("%s:shutdown:%d", state.sessionID, lineNum)
	shutdownPayload := copilotTelemetryBasePayload(state.path, lineNum, state.clientLabel, state.repo, state.cwd, "session.shutdown")
	shutdownPayload["shutdown_type"] = strings.TrimSpace(shutdown.ShutdownType)
	shutdownPayload["total_premium_requests"] = shutdown.TotalPremiumRequests
	shutdownPayload["total_api_duration_ms"] = shutdown.TotalAPIDurationMs
	shutdownPayload["session_start_time"] = strings.TrimSpace(shutdown.SessionStartTime)
	shutdownPayload["lines_added"] = shutdown.CodeChanges.LinesAdded
	shutdownPayload["lines_removed"] = shutdown.CodeChanges.LinesRemoved
	shutdownPayload["files_modified"] = shutdown.CodeChanges.FilesModified
	shutdownPayload["model_metrics_count"] = len(shutdown.ModelMetrics)
	if model := strings.TrimSpace(state.currentModel); model != "" {
		shutdownPayload["upstream_provider"] = copilotUpstreamProviderForModel(model)
	}

	*out = append(*out, shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       shared.TelemetryChannelJSONL,
		OccurredAt:    occurredAt,
		AccountID:     "copilot",
		WorkspaceID:   state.workspaceID,
		SessionID:     state.sessionID,
		TurnID:        shutdownTurnID,
		MessageID:     shutdownMessageID,
		ProviderID:    "copilot",
		AgentName:     "copilot",
		EventType:     shared.TelemetryEventTypeTurnCompleted,
		ModelRaw:      core.FirstNonEmpty(strings.TrimSpace(state.currentModel), "unknown"),
		Status:        shared.TelemetryStatusOK,
		Payload:       shutdownPayload,
	})

	if state.assistantUsageSeen {
		return
	}

	models := core.SortedStringKeys(shutdown.ModelMetrics)

	for idx, model := range models {
		appendShutdownModelMetricEvent(out, state, lineNum, occurredAt, shutdown, model, idx)
	}
}

func appendShutdownModelMetricEvent(out *[]shared.TelemetryEvent, state *copilotTelemetrySessionState, lineNum int, occurredAt time.Time, shutdown sessionShutdownData, model string, idx int) {
	modelMetric := shutdown.ModelMetrics[model]
	model = strings.TrimSpace(model)
	if model == "" {
		model = core.FirstNonEmpty(strings.TrimSpace(state.currentModel), "unknown")
	}

	inputTokens := int64(modelMetric.Usage.InputTokens)
	outputTokens := int64(modelMetric.Usage.OutputTokens)
	cacheReadTokens := int64(modelMetric.Usage.CacheReadTokens)
	cacheWriteTokens := int64(modelMetric.Usage.CacheWriteTokens)
	totalTokens := inputTokens + outputTokens
	requests := int64(modelMetric.Requests.Count)
	cost := modelMetric.Requests.Cost
	if totalTokens <= 0 && requests <= 0 && cost <= 0 {
		return
	}

	messageID := fmt.Sprintf("%s:shutdown:%s", state.sessionID, sanitizeMetricName(model))
	if idx > 0 {
		messageID = fmt.Sprintf("%s:%d", messageID, idx+1)
	}
	payload := copilotTelemetryBasePayload(state.path, lineNum, state.clientLabel, state.repo, state.cwd, "session.shutdown.model_metric")
	payload["model_metrics_source"] = "session.shutdown"
	payload["upstream_provider"] = copilotUpstreamProviderForModel(model)
	if idx == 0 {
		payload["lines_added"] = shutdown.CodeChanges.LinesAdded
		payload["lines_removed"] = shutdown.CodeChanges.LinesRemoved
		payload["files_modified"] = shutdown.CodeChanges.FilesModified
	}

	event := shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       shared.TelemetryChannelJSONL,
		OccurredAt:    occurredAt,
		AccountID:     "copilot",
		WorkspaceID:   state.workspaceID,
		SessionID:     state.sessionID,
		TurnID:        messageID,
		MessageID:     messageID,
		ProviderID:    "copilot",
		AgentName:     "copilot",
		EventType:     shared.TelemetryEventTypeMessageUsage,
		ModelRaw:      model,
		TokenUsage: core.TokenUsage{
			InputTokens:  core.Int64Ptr(inputTokens),
			OutputTokens: core.Int64Ptr(outputTokens),
			TotalTokens:  core.Int64Ptr(totalTokens),
		},
		Status:  shared.TelemetryStatusOK,
		Payload: payload,
	}
	if requests > 0 {
		event.Requests = core.Int64Ptr(requests)
	}
	if cacheReadTokens > 0 {
		event.CacheReadTokens = core.Int64Ptr(cacheReadTokens)
	}
	if cacheWriteTokens > 0 {
		event.CacheWriteTokens = core.Int64Ptr(cacheWriteTokens)
	}
	if cost > 0 {
		event.CostUSD = core.Float64Ptr(cost)
	}
	*out = append(*out, event)
}

func currentOrUnknownModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "unknown"
	}
	return model
}

func copilotTelemetryMessageID(sessionID string, lineNum int, messageID, fallbackID string) string {
	messageID = strings.TrimSpace(messageID)
	if messageID != "" {
		if strings.Contains(messageID, ":") {
			return messageID
		}
		return fmt.Sprintf("%s:%s", sessionID, messageID)
	}

	fallbackID = strings.TrimSpace(fallbackID)
	if fallbackID != "" {
		return fmt.Sprintf("%s:%s", sessionID, fallbackID)
	}
	return fmt.Sprintf("%s:%d", sessionID, lineNum)
}

func parseCopilotTelemetryToolRequest(raw json.RawMessage) (copilotTelemetryToolRequest, bool) {
	var reqMap map[string]any
	if json.Unmarshal(raw, &reqMap) != nil {
		return copilotTelemetryToolRequest{}, false
	}

	out := copilotTelemetryToolRequest{
		ToolCallID: strings.TrimSpace(anyToString(reqMap["toolCallId"])),
		RawName:    core.FirstNonEmpty(anyToString(reqMap["name"]), anyToString(reqMap["toolName"]), anyToString(reqMap["tool"])),
	}
	if out.RawName == "" {
		out.RawName = extractCopilotToolName(raw)
	}
	for _, key := range []string{"arguments", "args", "input"} {
		if value, ok := reqMap[key]; ok && out.Input == nil {
			out.Input = decodeCopilotTelemetryJSONAny(value)
		}
	}
	return out, true
}

func normalizeCopilotTelemetryToolName(raw string) (string, map[string]any) {
	meta := map[string]any{}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "unknown", meta
	}
	meta["tool_name_raw"] = name
	if server, function, ok := parseCopilotTelemetryMCPTool(name); ok {
		meta["tool_type"] = "mcp"
		meta["mcp_server"] = server
		meta["mcp_function"] = function
		return "mcp__" + server + "__" + function, meta
	}
	return sanitizeMetricName(name), meta
}

func parseCopilotTelemetryMCPTool(raw string) (string, string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return "", "", false
	}
	for _, marker := range []string{"_mcp_server_", "-mcp-server-"} {
		if parts := strings.SplitN(normalized, marker, 2); len(parts) == 2 {
			server := sanitizeCopilotMCPSegment(parts[0])
			function := sanitizeCopilotMCPSegment(parts[1])
			if server != "" && function != "" {
				return server, function, true
			}
		}
	}
	if strings.HasPrefix(normalized, "mcp__") {
		parts := strings.SplitN(strings.TrimPrefix(normalized, "mcp__"), "__", 2)
		if len(parts) == 2 {
			server := sanitizeCopilotMCPSegment(parts[0])
			function := sanitizeCopilotMCPSegment(parts[1])
			if server != "" && function != "" {
				return server, function, true
			}
		}
	}
	if strings.HasPrefix(normalized, "mcp-") || strings.HasPrefix(normalized, "mcp_") {
		canonical := normalizeCopilotCursorStyleMCPName(normalized)
		if strings.HasPrefix(canonical, "mcp__") {
			parts := strings.SplitN(strings.TrimPrefix(canonical, "mcp__"), "__", 2)
			if len(parts) == 2 {
				server := sanitizeCopilotMCPSegment(parts[0])
				function := sanitizeCopilotMCPSegment(parts[1])
				if server != "" && function != "" {
					return server, function, true
				}
			}
		}
	}
	if strings.HasSuffix(normalized, " (mcp)") {
		body := strings.TrimSpace(strings.TrimSuffix(normalized, " (mcp)"))
		body = strings.TrimPrefix(body, "user-")
		if body == "" {
			return "", "", false
		}
		if idx := findCopilotTelemetryServerFunctionSplit(body); idx > 0 {
			server := sanitizeCopilotMCPSegment(body[:idx])
			function := sanitizeCopilotMCPSegment(body[idx+1:])
			if server != "" && function != "" {
				return server, function, true
			}
		}
		return "other", sanitizeCopilotMCPSegment(body), true
	}
	return "", "", false
}

func normalizeCopilotCursorStyleMCPName(name string) string {
	if strings.HasPrefix(name, "mcp-") {
		rest := name[4:]
		parts := strings.SplitN(rest, "-user-", 2)
		if len(parts) == 2 {
			server := parts[0]
			afterUser := parts[1]
			serverDash := server + "-"
			if strings.HasPrefix(afterUser, serverDash) {
				return "mcp__" + server + "__" + afterUser[len(serverDash):]
			}
			if idx := strings.LastIndex(afterUser, "-"); idx > 0 {
				return "mcp__" + server + "__" + afterUser[idx+1:]
			}
			return "mcp__" + server + "__" + afterUser
		}
		if idx := strings.Index(rest, "-"); idx > 0 {
			return "mcp__" + rest[:idx] + "__" + rest[idx+1:]
		}
		return "mcp__" + rest + "__"
	}
	if strings.HasPrefix(name, "mcp_") {
		rest := name[4:]
		if idx := strings.Index(rest, "_"); idx > 0 {
			return "mcp__" + rest[:idx] + "__" + rest[idx+1:]
		}
		return "mcp__" + rest + "__"
	}
	return name
}

func findCopilotTelemetryServerFunctionSplit(s string) int {
	best := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '-' && strings.Contains(s[i+1:], "_") {
			best = i
		}
	}
	return best
}

func sanitizeCopilotMCPSegment(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func copilotTelemetryToolStatus(success *bool, statusRaw, errorCode, errorMessage string) shared.TelemetryStatus {
	if success != nil {
		if *success {
			return shared.TelemetryStatusOK
		}
		if copilotTelemetryLooksAborted(errorCode, errorMessage, statusRaw) {
			return shared.TelemetryStatusAborted
		}
		return shared.TelemetryStatusError
	}
	switch strings.ToLower(strings.TrimSpace(statusRaw)) {
	case "ok", "success", "succeeded", "completed", "complete":
		return shared.TelemetryStatusOK
	case "aborted", "cancelled", "canceled", "denied":
		return shared.TelemetryStatusAborted
	case "error", "failed", "failure":
		return shared.TelemetryStatusError
	}
	if errorCode != "" || errorMessage != "" {
		if copilotTelemetryLooksAborted(errorCode, errorMessage, statusRaw) {
			return shared.TelemetryStatusAborted
		}
		return shared.TelemetryStatusError
	}
	return shared.TelemetryStatusUnknown
}

func copilotTelemetryLooksAborted(parts ...string) bool {
	for _, part := range parts {
		lower := strings.ToLower(strings.TrimSpace(part))
		if lower == "" {
			continue
		}
		if strings.Contains(lower, "denied") || strings.Contains(lower, "cancel") || strings.Contains(lower, "abort") || strings.Contains(lower, "rejected") || strings.Contains(lower, "user initiated") {
			return true
		}
	}
	return false
}

func summarizeCopilotTelemetryResult(raw json.RawMessage) map[string]any {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	decoded := decodeCopilotTelemetryJSONAny(raw)
	if decoded == nil {
		return nil
	}
	payload := map[string]any{}
	if paths := shared.ExtractFilePathsFromPayload(decoded); len(paths) > 0 {
		payload["result_file"] = paths[0]
	}
	switch value := decoded.(type) {
	case map[string]any:
		if content := anyToString(value["content"]); content != "" {
			payload["result_chars"] = len(content)
			if added, removed := countCopilotTelemetryUnifiedDiff(content); added > 0 || removed > 0 {
				payload["lines_added"] = added
				payload["lines_removed"] = removed
			}
		}
		if detailed := anyToString(value["detailedContent"]); detailed != "" {
			payload["result_detailed_chars"] = len(detailed)
			if _, ok := payload["lines_added"]; !ok {
				if added, removed := countCopilotTelemetryUnifiedDiff(detailed); added > 0 || removed > 0 {
					payload["lines_added"] = added
					payload["lines_removed"] = removed
				}
			}
		}
		if msg := anyToString(value["message"]); msg != "" {
			payload["result_message"] = truncate(msg, 240)
		}
	case string:
		if value != "" {
			payload["result_chars"] = len(value)
			if added, removed := countCopilotTelemetryUnifiedDiff(value); added > 0 || removed > 0 {
				payload["lines_added"] = added
				payload["lines_removed"] = removed
			}
		}
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func countCopilotTelemetryUnifiedDiff(raw string) (int, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" || (!strings.Contains(raw, "diff --git") && !strings.Contains(raw, "\n@@")) {
		return 0, 0
	}
	added, removed := 0, 0
	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"), strings.HasPrefix(line, "@@"):
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

func summarizeCopilotTelemetryError(raw json.RawMessage) (string, string) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return "", ""
	}
	decoded := decodeCopilotTelemetryJSONAny(raw)
	if decoded == nil {
		return "", ""
	}
	switch value := decoded.(type) {
	case map[string]any:
		return strings.TrimSpace(anyToString(value["code"])), strings.TrimSpace(anyToString(value["message"]))
	case string:
		return "", strings.TrimSpace(value)
	default:
		return "", strings.TrimSpace(anyToString(decoded))
	}
}

func copilotTelemetryBasePayload(path string, line int, client, repo, cwd, event string) map[string]any {
	payload := map[string]any{
		"source_file":       path,
		"line":              line,
		"event":             event,
		"client":            client,
		"upstream_provider": "github",
	}
	if strings.TrimSpace(repo) != "" {
		payload["repository"] = strings.TrimSpace(repo)
	}
	if strings.TrimSpace(cwd) != "" {
		payload["cwd"] = strings.TrimSpace(cwd)
	}
	return payload
}

func copyCopilotTelemetryPayload(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func decodeCopilotTelemetryJSONAny(raw any) any {
	switch value := raw.(type) {
	case nil:
		return nil
	case map[string]any, []any:
		return value
	case json.RawMessage:
		var out any
		if json.Unmarshal(value, &out) == nil {
			return out
		}
		return strings.TrimSpace(string(value))
	case []byte:
		var out any
		if json.Unmarshal(value, &out) == nil {
			return out
		}
		return strings.TrimSpace(string(value))
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		var out any
		if json.Unmarshal([]byte(trimmed), &out) == nil {
			return out
		}
		return trimmed
	default:
		return value
	}
}

func extractCopilotTelemetryCommand(input any) string {
	var command string
	var walk func(any)
	walk = func(value any) {
		if command != "" || value == nil {
			return
		}
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				k := strings.ToLower(strings.TrimSpace(key))
				if (k == "command" || k == "cmd" || k == "script" || k == "shell_command") && child != nil {
					if s, ok := child.(string); ok {
						command = strings.TrimSpace(s)
						return
					}
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(input)
	return command
}

func estimateCopilotTelemetryLineDelta(input any) (int, int) {
	if input == nil {
		return 0, 0
	}
	encoded, err := json.Marshal(map[string]any{"arguments": input})
	if err != nil {
		return 0, 0
	}
	return estimateCopilotToolLineDelta(encoded)
}

func copilotUpstreamProviderForModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || model == "unknown" {
		return "github"
	}
	switch {
	case strings.Contains(model, "claude"):
		return "anthropic"
	case strings.Contains(model, "gpt"), strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"), strings.HasPrefix(model, "o4"):
		return "openai"
	case strings.Contains(model, "gemini"):
		return "google"
	case strings.Contains(model, "qwen"):
		return "alibaba_cloud"
	case strings.Contains(model, "deepseek"):
		return "deepseek"
	case strings.Contains(model, "llama"):
		return "meta"
	case strings.Contains(model, "mistral"):
		return "mistral"
	default:
		return "github"
	}
}

func anyToString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("%v", value)
	}
}

func truncate(input string, max int) string {
	input = strings.TrimSpace(input)
	if max <= 0 || len(input) <= max {
		return input
	}
	return input[:max]
}
