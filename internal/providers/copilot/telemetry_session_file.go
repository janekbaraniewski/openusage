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
