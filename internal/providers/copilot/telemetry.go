package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetrySchemaVersion   = "copilot_v2"
	defaultCopilotSessionDir = ".copilot/session-state"
	defaultCopilotStoreDB    = ".copilot/session-store.db"
)

type copilotTelemetryAssistantMessageData struct {
	MessageID    string          `json:"messageId"`
	ToolRequests json.RawMessage `json:"toolRequests"`
}

type copilotTelemetryToolRequest struct {
	ToolCallID string `json:"toolCallId"`
	RawName    string `json:"-"`
	Input      any    `json:"-"`
}

type copilotTelemetryToolExecutionStartData struct {
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Arguments  json.RawMessage `json:"arguments"`
}

type copilotTelemetryToolExecutionCompleteData struct {
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Success    *bool           `json:"success"`
	Status     string          `json:"status"`
	Result     json.RawMessage `json:"result"`
	Error      json.RawMessage `json:"error"`
}

type copilotTelemetrySessionContextChangedData struct {
	CWD        string `json:"cwd"`
	Repository string `json:"repository"`
}

type copilotTelemetryWorkspaceFileChangedData struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

type copilotTelemetryToolContext struct {
	MessageID string
	TurnID    string
	Model     string
	ToolName  string
	Payload   map[string]any
}

// System returns the telemetry system identifier for the copilot provider.
func (p *Provider) System() string { return p.ID() }

// Collect scans copilot session-state directories for events.jsonl files and
// extracts usage telemetry events from assistant.usage entries.
func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	sessionDir := shared.ExpandHome(opts.Path("sessions_dir", defaultCopilotSessionsDir()))
	storeDB := shared.ExpandHome(opts.Path("session_store_db", defaultCopilotSessionStoreDB()))
	if sessionDir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil, nil
	}

	var out []shared.TelemetryEvent
	seenSessions := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if !entry.IsDir() {
			continue
		}
		seenSessions[entry.Name()] = true
		eventsPath := filepath.Join(sessionDir, entry.Name(), "events.jsonl")
		events, err := parseCopilotTelemetrySessionFile(eventsPath, entry.Name())
		if err != nil {
			continue
		}
		out = append(out, events...)
	}

	// Fallback to durable session-store metadata for sessions that no longer have
	// events.jsonl state (Copilot rotates session-state aggressively).
	storeEvents, err := parseCopilotTelemetrySessionStore(ctx, storeDB, seenSessions)
	if err == nil {
		out = append(out, storeEvents...)
	}
	return out, nil
}

// ParseHookPayload is not supported for the copilot provider.
func (p *Provider) ParseHookPayload(_ []byte, _ shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return nil, shared.ErrHookUnsupported
}

// defaultCopilotSessionsDir returns the default copilot session-state directory.
func defaultCopilotSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, defaultCopilotSessionDir)
}

func defaultCopilotSessionStoreDB() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, defaultCopilotStoreDB)
}

// parseCopilotTelemetrySessionFile parses a single session's events.jsonl and
// produces telemetry events from assistant.usage and assistant.message entries.
func parseCopilotTelemetrySessionFile(path, sessionID string) ([]shared.TelemetryEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	currentModel := ""
	workspaceID := ""
	repo := ""
	cwd := ""
	clientLabel := "cli"
	turnIndex := 0
	assistantUsageSeen := false
	toolContexts := make(map[string]copilotTelemetryToolContext)

	var out []shared.TelemetryEvent
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

		switch evt.Type {
		case "session.start":
			var start sessionStartData
			if json.Unmarshal(evt.Data, &start) == nil {
				if start.Context.Repository != "" {
					repo = start.Context.Repository
				}
				if start.Context.CWD != "" {
					cwd = start.Context.CWD
					workspaceID = shared.SanitizeWorkspace(start.Context.CWD)
				}
				clientLabel = normalizeCopilotClient(repo, cwd)
			}

		case "session.context_changed":
			var changed copilotTelemetrySessionContextChangedData
			if json.Unmarshal(evt.Data, &changed) == nil {
				if changed.Repository != "" {
					repo = changed.Repository
				}
				if changed.CWD != "" {
					cwd = changed.CWD
					workspaceID = shared.SanitizeWorkspace(changed.CWD)
				}
				clientLabel = normalizeCopilotClient(repo, cwd)
			}

		case "session.model_change":
			var mc modelChangeData
			if json.Unmarshal(evt.Data, &mc) == nil && mc.NewModel != "" {
				currentModel = mc.NewModel
			}

		case "session.info":
			var info sessionInfoData
			if json.Unmarshal(evt.Data, &info) == nil && info.InfoType == "model" {
				if m := extractModelFromInfoMsg(info.Message); m != "" {
					currentModel = m
				}
			}

		case "assistant.message":
			var msg copilotTelemetryAssistantMessageData
			if json.Unmarshal(evt.Data, &msg) != nil {
				continue
			}

			var toolRequests []json.RawMessage
			if json.Unmarshal(msg.ToolRequests, &toolRequests) != nil || len(toolRequests) == 0 {
				continue
			}

			messageID := copilotTelemetryMessageID(sessionID, lineNum+1, msg.MessageID, evt.ID)
			turnID := shared.FirstNonEmpty(messageID, fmt.Sprintf("%s:line:%d", sessionID, lineNum+1))

			for reqIdx, rawReq := range toolRequests {
				req, ok := parseCopilotTelemetryToolRequest(rawReq)
				if !ok {
					continue
				}

				explicitCallID := strings.TrimSpace(req.ToolCallID) != ""
				toolCallID := strings.TrimSpace(req.ToolCallID)
				if toolCallID == "" {
					toolCallID = fmt.Sprintf("%s:%d:tool:%d", sessionID, lineNum+1, reqIdx+1)
				}

				toolName, toolMeta := normalizeCopilotTelemetryToolName(req.RawName)
				if toolName == "" {
					toolName = "unknown"
				}

				payload := copilotTelemetryBasePayload(path, lineNum+1, clientLabel, repo, cwd, "assistant.message.tool_request")
				for key, value := range toolMeta {
					payload[key] = value
				}
				payload["tool_call_id"] = toolCallID

				if req.Input != nil {
					payload["tool_input"] = req.Input
					if cmd := extractCopilotTelemetryCommand(req.Input); cmd != "" {
						payload["command"] = cmd
					}
					if paths := shared.ExtractFilePathsFromPayload(req.Input); len(paths) > 0 {
						payload["file"] = paths[0]
						if lang := inferCopilotLanguageFromPath(paths[0]); lang != "" {
							payload["language"] = lang
						}
					}
					if added, removed := estimateCopilotTelemetryLineDelta(req.Input); added > 0 || removed > 0 {
						payload["lines_added"] = added
						payload["lines_removed"] = removed
					}
				}

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

				model := strings.TrimSpace(currentModel)
				if model == "" {
					model = "unknown"
				}
				if upstream := copilotUpstreamProviderForModel(model); upstream != "" {
					payload["upstream_provider"] = upstream
				}

				out = append(out, shared.TelemetryEvent{
					SchemaVersion: telemetrySchemaVersion,
					Channel:       shared.TelemetryChannelJSONL,
					OccurredAt:    occurredAt,
					AccountID:     "copilot",
					WorkspaceID:   workspaceID,
					SessionID:     sessionID,
					TurnID:        turnID,
					MessageID:     messageID,
					ToolCallID:    toolCallID,
					ProviderID:    "copilot",
					AgentName:     "copilot",
					EventType:     shared.TelemetryEventTypeToolUsage,
					ModelRaw:      model,
					ToolName:      toolName,
					Requests:      shared.Int64Ptr(1),
					Status:        shared.TelemetryStatusUnknown,
					Payload:       payload,
				})

				if explicitCallID {
					toolContexts[toolCallID] = copilotTelemetryToolContext{
						MessageID: messageID,
						TurnID:    turnID,
						Model:     model,
						ToolName:  toolName,
						Payload:   copyCopilotTelemetryPayload(payload),
					}
				}
			}

		case "tool.execution_start":
			var start copilotTelemetryToolExecutionStartData
			if json.Unmarshal(evt.Data, &start) != nil {
				continue
			}

			explicitCallID := strings.TrimSpace(start.ToolCallID) != ""
			toolCallID := strings.TrimSpace(start.ToolCallID)
			if toolCallID == "" {
				toolCallID = fmt.Sprintf("%s:%d:tool_start", sessionID, lineNum+1)
			}

			ctx := toolContexts[toolCallID]
			payload := copyCopilotTelemetryPayload(ctx.Payload)
			if len(payload) == 0 {
				payload = copilotTelemetryBasePayload(path, lineNum+1, clientLabel, repo, cwd, "tool.execution_start")
			} else {
				payload["event"] = "tool.execution_start"
				payload["line"] = lineNum + 1
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
				payload["tool_input"] = args
				if _, ok := payload["command"]; !ok {
					if cmd := extractCopilotTelemetryCommand(args); cmd != "" {
						payload["command"] = cmd
					}
				}
				if _, ok := payload["file"]; !ok {
					if paths := shared.ExtractFilePathsFromPayload(args); len(paths) > 0 {
						payload["file"] = paths[0]
						if lang := inferCopilotLanguageFromPath(paths[0]); lang != "" {
							payload["language"] = lang
						}
					}
				}
				if _, ok := payload["lines_added"]; !ok {
					added, removed := estimateCopilotTelemetryLineDelta(args)
					if added > 0 || removed > 0 {
						payload["lines_added"] = added
						payload["lines_removed"] = removed
					}
				}
			}

			model := strings.TrimSpace(ctx.Model)
			if model == "" {
				model = strings.TrimSpace(currentModel)
			}
			if model == "" {
				model = "unknown"
			}
			if upstream := copilotUpstreamProviderForModel(model); upstream != "" {
				payload["upstream_provider"] = upstream
			}

			messageID := shared.FirstNonEmpty(ctx.MessageID, fmt.Sprintf("%s:%d", sessionID, lineNum+1))
			turnID := shared.FirstNonEmpty(ctx.TurnID, messageID)

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetrySchemaVersion,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurredAt,
				AccountID:     "copilot",
				WorkspaceID:   workspaceID,
				SessionID:     sessionID,
				TurnID:        turnID,
				MessageID:     messageID,
				ToolCallID:    toolCallID,
				ProviderID:    "copilot",
				AgentName:     "copilot",
				EventType:     shared.TelemetryEventTypeToolUsage,
				ModelRaw:      model,
				ToolName:      toolName,
				Requests:      shared.Int64Ptr(1),
				Status:        shared.TelemetryStatusUnknown,
				Payload:       payload,
			})

			if explicitCallID {
				toolContexts[toolCallID] = copilotTelemetryToolContext{
					MessageID: messageID,
					TurnID:    turnID,
					Model:     model,
					ToolName:  toolName,
					Payload:   copyCopilotTelemetryPayload(payload),
				}
			}

		case "tool.execution_complete":
			var complete copilotTelemetryToolExecutionCompleteData
			if json.Unmarshal(evt.Data, &complete) != nil {
				continue
			}

			toolCallID := strings.TrimSpace(complete.ToolCallID)
			explicitCallID := toolCallID != ""
			if toolCallID == "" {
				toolCallID = fmt.Sprintf("%s:%d:tool_complete", sessionID, lineNum+1)
			}

			ctx := toolContexts[toolCallID]
			payload := copyCopilotTelemetryPayload(ctx.Payload)
			if len(payload) == 0 {
				payload = copilotTelemetryBasePayload(path, lineNum+1, clientLabel, repo, cwd, "tool.execution_complete")
			} else {
				payload["event"] = "tool.execution_complete"
				payload["line"] = lineNum + 1
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

			if resultMeta := summarizeCopilotTelemetryResult(complete.Result); len(resultMeta) > 0 {
				for key, value := range resultMeta {
					if _, exists := payload[key]; !exists {
						payload[key] = value
					}
				}
			}

			errorCode, errorMessage := summarizeCopilotTelemetryError(complete.Error)
			if errorCode != "" {
				payload["error_code"] = errorCode
			}
			if errorMessage != "" {
				payload["error_message"] = truncate(errorMessage, 240)
			}

			model := strings.TrimSpace(ctx.Model)
			if model == "" {
				model = strings.TrimSpace(currentModel)
			}
			if model == "" {
				model = "unknown"
			}
			if upstream := copilotUpstreamProviderForModel(model); upstream != "" {
				payload["upstream_provider"] = upstream
			}

			status := copilotTelemetryToolStatus(complete.Success, complete.Status, errorCode, errorMessage)
			messageID := shared.FirstNonEmpty(ctx.MessageID, fmt.Sprintf("%s:%d", sessionID, lineNum+1))
			turnID := shared.FirstNonEmpty(ctx.TurnID, messageID)

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetrySchemaVersion,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurredAt,
				AccountID:     "copilot",
				WorkspaceID:   workspaceID,
				SessionID:     sessionID,
				TurnID:        turnID,
				MessageID:     messageID,
				ToolCallID:    toolCallID,
				ProviderID:    "copilot",
				AgentName:     "copilot",
				EventType:     shared.TelemetryEventTypeToolUsage,
				ModelRaw:      model,
				ToolName:      toolName,
				Requests:      shared.Int64Ptr(1),
				Status:        status,
				Payload:       payload,
			})

			if explicitCallID {
				toolContexts[toolCallID] = copilotTelemetryToolContext{
					MessageID: messageID,
					TurnID:    turnID,
					Model:     model,
					ToolName:  toolName,
					Payload:   copyCopilotTelemetryPayload(payload),
				}
			}

		case "session.workspace_file_changed":
			var changed copilotTelemetryWorkspaceFileChangedData
			if json.Unmarshal(evt.Data, &changed) != nil {
				continue
			}
			filePath := strings.TrimSpace(changed.Path)
			if filePath == "" {
				continue
			}

			op := sanitizeMetricName(changed.Operation)
			if op == "" || op == "unknown" {
				op = "change"
			}

			payload := copilotTelemetryBasePayload(path, lineNum+1, clientLabel, repo, cwd, "session.workspace_file_changed")
			payload["file"] = filePath
			payload["operation"] = strings.TrimSpace(changed.Operation)
			if lang := inferCopilotLanguageFromPath(filePath); lang != "" {
				payload["language"] = lang
			}

			model := strings.TrimSpace(currentModel)
			if model == "" {
				model = "unknown"
			}
			if upstream := copilotUpstreamProviderForModel(model); upstream != "" {
				payload["upstream_provider"] = upstream
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetrySchemaVersion,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurredAt,
				AccountID:     "copilot",
				WorkspaceID:   workspaceID,
				SessionID:     sessionID,
				TurnID:        fmt.Sprintf("%s:file:%d", sessionID, lineNum+1),
				MessageID:     fmt.Sprintf("%s:%d", sessionID, lineNum+1),
				ProviderID:    "copilot",
				AgentName:     "copilot",
				EventType:     shared.TelemetryEventTypeToolUsage,
				ModelRaw:      model,
				ToolName:      "workspace_file_" + op,
				Requests:      shared.Int64Ptr(0),
				Status:        shared.TelemetryStatusOK,
				Payload:       payload,
			})

		case "assistant.usage":
			var usage assistantUsageData
			if json.Unmarshal(evt.Data, &usage) != nil {
				continue
			}
			assistantUsageSeen = true

			model := usage.Model
			if model == "" {
				model = currentModel
			}
			if model == "" {
				continue
			}

			turnIndex++

			turnID := shared.FirstNonEmpty(strings.TrimSpace(evt.ID), fmt.Sprintf("%s:usage:%d", sessionID, turnIndex))
			messageID := fmt.Sprintf("%s:%d", sessionID, lineNum+1)

			totalTokens := int64(usage.InputTokens + usage.OutputTokens)
			payload := copilotTelemetryBasePayload(path, lineNum+1, clientLabel, repo, cwd, "assistant.usage")
			payload["source_file"] = path
			payload["line"] = lineNum + 1
			payload["client"] = clientLabel
			payload["upstream_provider"] = copilotUpstreamProviderForModel(model)
			if usage.Duration > 0 {
				payload["duration_ms"] = usage.Duration
			}
			if len(usage.QuotaSnapshots) > 0 {
				payload["quota_snapshot_count"] = len(usage.QuotaSnapshots)
			}

			te := shared.TelemetryEvent{
				SchemaVersion: telemetrySchemaVersion,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurredAt,
				AccountID:     "copilot",
				WorkspaceID:   workspaceID,
				SessionID:     sessionID,
				TurnID:        turnID,
				MessageID:     messageID,
				ProviderID:    "copilot",
				AgentName:     "copilot",
				EventType:     shared.TelemetryEventTypeMessageUsage,
				ModelRaw:      model,
				InputTokens:   shared.Int64Ptr(int64(usage.InputTokens)),
				OutputTokens:  shared.Int64Ptr(int64(usage.OutputTokens)),
				TotalTokens:   shared.Int64Ptr(totalTokens),
				Requests:      shared.Int64Ptr(1),
				Status:        shared.TelemetryStatusOK,
				Payload:       payload,
			}

			if usage.CacheReadTokens > 0 {
				te.CacheReadTokens = shared.Int64Ptr(int64(usage.CacheReadTokens))
			}
			if usage.CacheWriteTokens > 0 {
				te.CacheWriteTokens = shared.Int64Ptr(int64(usage.CacheWriteTokens))
			}
			if usage.Cost > 0 {
				te.CostUSD = shared.Float64Ptr(usage.Cost)
			}

			out = append(out, te)

		case "session.shutdown":
			var shutdown sessionShutdownData
			if json.Unmarshal(evt.Data, &shutdown) != nil {
				continue
			}

			shutdownTurnID := shared.FirstNonEmpty(strings.TrimSpace(evt.ID), fmt.Sprintf("%s:shutdown", sessionID))
			shutdownMessageID := fmt.Sprintf("%s:shutdown:%d", sessionID, lineNum+1)

			shutdownPayload := copilotTelemetryBasePayload(path, lineNum+1, clientLabel, repo, cwd, "session.shutdown")
			shutdownPayload["shutdown_type"] = strings.TrimSpace(shutdown.ShutdownType)
			shutdownPayload["total_premium_requests"] = shutdown.TotalPremiumRequests
			shutdownPayload["total_api_duration_ms"] = shutdown.TotalAPIDurationMs
			shutdownPayload["session_start_time"] = strings.TrimSpace(shutdown.SessionStartTime)
			shutdownPayload["lines_added"] = shutdown.CodeChanges.LinesAdded
			shutdownPayload["lines_removed"] = shutdown.CodeChanges.LinesRemoved
			shutdownPayload["files_modified"] = shutdown.CodeChanges.FilesModified
			shutdownPayload["model_metrics_count"] = len(shutdown.ModelMetrics)
			if model := strings.TrimSpace(currentModel); model != "" {
				shutdownPayload["upstream_provider"] = copilotUpstreamProviderForModel(model)
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetrySchemaVersion,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurredAt,
				AccountID:     "copilot",
				WorkspaceID:   workspaceID,
				SessionID:     sessionID,
				TurnID:        shutdownTurnID,
				MessageID:     shutdownMessageID,
				ProviderID:    "copilot",
				AgentName:     "copilot",
				EventType:     shared.TelemetryEventTypeTurnCompleted,
				ModelRaw:      shared.FirstNonEmpty(strings.TrimSpace(currentModel), "unknown"),
				Status:        shared.TelemetryStatusOK,
				Payload:       shutdownPayload,
			})

			if assistantUsageSeen {
				continue
			}

			models := make([]string, 0, len(shutdown.ModelMetrics))
			for model := range shutdown.ModelMetrics {
				models = append(models, model)
			}
			sort.Strings(models)

			for idx, model := range models {
				modelMetric := shutdown.ModelMetrics[model]
				model = strings.TrimSpace(model)
				if model == "" {
					model = shared.FirstNonEmpty(strings.TrimSpace(currentModel), "unknown")
				}

				inputTokens := int64(modelMetric.Usage.InputTokens)
				outputTokens := int64(modelMetric.Usage.OutputTokens)
				cacheReadTokens := int64(modelMetric.Usage.CacheReadTokens)
				cacheWriteTokens := int64(modelMetric.Usage.CacheWriteTokens)
				totalTokens := inputTokens + outputTokens
				requests := int64(modelMetric.Requests.Count)
				cost := modelMetric.Requests.Cost

				if totalTokens <= 0 && requests <= 0 && cost <= 0 {
					continue
				}

				messageID := fmt.Sprintf("%s:shutdown:%s", sessionID, sanitizeMetricName(model))
				if idx > 0 {
					messageID = fmt.Sprintf("%s:%d", messageID, idx+1)
				}
				turnID := messageID

				payload := copilotTelemetryBasePayload(path, lineNum+1, clientLabel, repo, cwd, "session.shutdown.model_metric")
				payload["model_metrics_source"] = "session.shutdown"
				payload["upstream_provider"] = copilotUpstreamProviderForModel(model)
				if idx == 0 {
					payload["lines_added"] = shutdown.CodeChanges.LinesAdded
					payload["lines_removed"] = shutdown.CodeChanges.LinesRemoved
					payload["files_modified"] = shutdown.CodeChanges.FilesModified
				}

				usageEvent := shared.TelemetryEvent{
					SchemaVersion: telemetrySchemaVersion,
					Channel:       shared.TelemetryChannelJSONL,
					OccurredAt:    occurredAt,
					AccountID:     "copilot",
					WorkspaceID:   workspaceID,
					SessionID:     sessionID,
					TurnID:        turnID,
					MessageID:     messageID,
					ProviderID:    "copilot",
					AgentName:     "copilot",
					EventType:     shared.TelemetryEventTypeMessageUsage,
					ModelRaw:      model,
					InputTokens:   shared.Int64Ptr(inputTokens),
					OutputTokens:  shared.Int64Ptr(outputTokens),
					TotalTokens:   shared.Int64Ptr(totalTokens),
					Status:        shared.TelemetryStatusOK,
					Payload:       payload,
				}
				if requests > 0 {
					usageEvent.Requests = shared.Int64Ptr(requests)
				}
				if cacheReadTokens > 0 {
					usageEvent.CacheReadTokens = shared.Int64Ptr(cacheReadTokens)
				}
				if cacheWriteTokens > 0 {
					usageEvent.CacheWriteTokens = shared.Int64Ptr(cacheWriteTokens)
				}
				if cost > 0 {
					usageEvent.CostUSD = shared.Float64Ptr(cost)
				}

				out = append(out, usageEvent)
			}
		}
	}

	return out, nil
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
		RawName:    shared.FirstNonEmpty(anyToString(reqMap["name"]), anyToString(reqMap["toolName"]), anyToString(reqMap["tool"])),
	}
	if out.RawName == "" {
		out.RawName = extractCopilotToolName(raw)
	}

	if value, ok := reqMap["arguments"]; ok {
		out.Input = decodeCopilotTelemetryJSONAny(value)
	}
	if out.Input == nil {
		if value, ok := reqMap["args"]; ok {
			out.Input = decodeCopilotTelemetryJSONAny(value)
		}
	}
	if out.Input == nil {
		if value, ok := reqMap["input"]; ok {
			out.Input = decodeCopilotTelemetryJSONAny(value)
		}
	}

	return out, true
}

func normalizeCopilotTelemetryToolName(raw string) (string, map[string]any) {
	meta := make(map[string]any)
	name := strings.TrimSpace(raw)
	if name == "" {
		return "unknown", meta
	}

	meta["tool_name_raw"] = name

	if server, function, ok := parseCopilotTelemetryMCPTool(name); ok {
		canonical := "mcp__" + server + "__" + function
		meta["tool_type"] = "mcp"
		meta["mcp_server"] = server
		meta["mcp_function"] = function
		return canonical, meta
	}

	return sanitizeMetricName(name), meta
}

func parseCopilotTelemetryMCPTool(raw string) (string, string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return "", "", false
	}

	// Copilot-native MCP wrappers: github_mcp_server_list_issues.
	if parts := strings.SplitN(normalized, "_mcp_server_", 2); len(parts) == 2 {
		server := sanitizeCopilotMCPSegment(parts[0])
		function := sanitizeCopilotMCPSegment(parts[1])
		if server != "" && function != "" {
			return server, function, true
		}
	}
	if parts := strings.SplitN(normalized, "-mcp-server-", 2); len(parts) == 2 {
		server := sanitizeCopilotMCPSegment(parts[0])
		function := sanitizeCopilotMCPSegment(parts[1])
		if server != "" && function != "" {
			return server, function, true
		}
	}

	if strings.HasPrefix(normalized, "mcp__") {
		rest := strings.TrimPrefix(normalized, "mcp__")
		parts := strings.SplitN(rest, "__", 2)
		if len(parts) != 2 {
			return sanitizeCopilotMCPSegment(rest), "", false
		}
		server := sanitizeCopilotMCPSegment(parts[0])
		function := sanitizeCopilotMCPSegment(parts[1])
		if server == "" || function == "" {
			return "", "", false
		}
		return server, function, true
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

	// Legacy suffix format from earlier tool adapters: "server-function (mcp)".
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
		if s[i] != '-' {
			continue
		}
		rest := s[i+1:]
		if strings.Contains(rest, "_") {
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
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '_' || r == '-':
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
		if strings.Contains(lower, "denied") ||
			strings.Contains(lower, "cancel") ||
			strings.Contains(lower, "abort") ||
			strings.Contains(lower, "rejected") ||
			strings.Contains(lower, "user initiated") {
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

	payload := make(map[string]any)

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
			if _, hasLines := payload["lines_added"]; !hasLines {
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
	if raw == "" {
		return 0, 0
	}
	if !strings.Contains(raw, "diff --git") && !strings.Contains(raw, "\n@@") {
		return 0, 0
	}

	added := 0
	removed := 0
	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"), strings.HasPrefix(line, "@@"):
			continue
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
	case map[string]any:
		return value
	case []any:
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
	var walk func(value any)
	walk = func(value any) {
		if command != "" || value == nil {
			return
		}
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				k := strings.ToLower(strings.TrimSpace(key))
				if k == "command" || k == "cmd" || k == "script" || k == "shell_command" {
					if s, ok := child.(string); ok {
						command = strings.TrimSpace(s)
						return
					}
				}
			}
			for _, child := range v {
				walk(child)
				if command != "" {
					return
				}
			}
		case []any:
			for _, child := range v {
				walk(child)
				if command != "" {
					return
				}
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

func parseCopilotTelemetrySessionStore(ctx context.Context, dbPath string, skipSessions map[string]bool) ([]shared.TelemetryEvent, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if !copilotTelemetryTableExists(ctx, db, "sessions") || !copilotTelemetryTableExists(ctx, db, "turns") {
		return nil, nil
	}

	query := `
		SELECT
			s.id,
			COALESCE(s.cwd, ''),
			COALESCE(s.repository, ''),
			COALESCE(t.turn_index, 0),
			COALESCE(t.user_message, ''),
			COALESCE(t.assistant_response, ''),
			COALESCE(t.timestamp, '')
		FROM sessions s
		JOIN turns t ON t.session_id = s.id
		ORDER BY s.id ASC, t.turn_index ASC
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []shared.TelemetryEvent
	for rows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		var (
			sessionID string
			cwd       string
			repo      string
			turnIndex int
			userMsg   string
			reply     string
			tsRaw     string
		)
		if err := rows.Scan(&sessionID, &cwd, &repo, &turnIndex, &userMsg, &reply, &tsRaw); err != nil {
			continue
		}
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" || skipSessions[sessionID] {
			continue
		}

		workspaceID := shared.SanitizeWorkspace(cwd)
		clientLabel := normalizeCopilotClient(repo, cwd)
		occurredAt := time.Now().UTC()
		if parsed := shared.FlexParseTime(tsRaw); !parsed.IsZero() {
			occurredAt = parsed
		}

		messageID := fmt.Sprintf("%s:turn:%d", sessionID, turnIndex)
		model := "unknown"

		payload := map[string]any{
			"source_file":            dbPath,
			"event":                  "session_store.turn",
			"client":                 clientLabel,
			"upstream_provider":      "github",
			"session_store_fallback": true,
			"user_chars":             len(strings.TrimSpace(userMsg)),
			"assistant_chars":        len(strings.TrimSpace(reply)),
			"turn_index":             turnIndex,
		}
		if strings.TrimSpace(repo) != "" {
			payload["repository"] = strings.TrimSpace(repo)
		}
		if strings.TrimSpace(cwd) != "" {
			payload["cwd"] = strings.TrimSpace(cwd)
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetrySchemaVersion,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    occurredAt,
			AccountID:     "copilot",
			WorkspaceID:   workspaceID,
			SessionID:     sessionID,
			TurnID:        messageID,
			MessageID:     messageID,
			ProviderID:    "copilot",
			AgentName:     "copilot",
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      model,
			Requests:      shared.Int64Ptr(1),
			Status:        shared.TelemetryStatusOK,
			Payload:       payload,
		})
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	// Add session_files fallback for language/code-stats even when JSONL tool
	// execution events are unavailable.
	if copilotTelemetryTableExists(ctx, db, "session_files") {
		fileRows, err := db.QueryContext(ctx, `
			SELECT
				COALESCE(sf.session_id, ''),
				COALESCE(sf.file_path, ''),
				COALESCE(sf.tool_name, ''),
				COALESCE(sf.turn_index, 0),
				COALESCE(sf.first_seen_at, ''),
				COALESCE(s.cwd, ''),
				COALESCE(s.repository, '')
			FROM session_files sf
			LEFT JOIN sessions s ON s.id = sf.session_id
			ORDER BY sf.session_id ASC, sf.turn_index ASC, sf.id ASC
		`)
		if err == nil {
			defer fileRows.Close()
			for fileRows.Next() {
				if ctx.Err() != nil {
					return out, ctx.Err()
				}
				var (
					sessionID string
					filePath  string
					toolRaw   string
					turnIndex int
					tsRaw     string
					cwd       string
					repo      string
				)
				if err := fileRows.Scan(&sessionID, &filePath, &toolRaw, &turnIndex, &tsRaw, &cwd, &repo); err != nil {
					continue
				}
				sessionID = strings.TrimSpace(sessionID)
				filePath = strings.TrimSpace(filePath)
				if sessionID == "" || filePath == "" || skipSessions[sessionID] {
					continue
				}

				workspaceID := shared.SanitizeWorkspace(cwd)
				clientLabel := normalizeCopilotClient(repo, cwd)
				occurredAt := time.Now().UTC()
				if parsed := shared.FlexParseTime(tsRaw); !parsed.IsZero() {
					occurredAt = parsed
				}

				toolName, meta := normalizeCopilotTelemetryToolName(toolRaw)
				if toolName == "" || toolName == "unknown" {
					toolName = "workspace_file_changed"
				}

				toolCallID := fmt.Sprintf("store:%s:%d:%s", sessionID, turnIndex, sanitizeMetricName(filePath))
				messageID := fmt.Sprintf("%s:turn:%d", sessionID, turnIndex)
				payload := map[string]any{
					"source_file":            dbPath,
					"event":                  "session_store.file",
					"client":                 clientLabel,
					"upstream_provider":      "github",
					"session_store_fallback": true,
					"file":                   filePath,
					"turn_index":             turnIndex,
					"tool_name_raw":          strings.TrimSpace(toolRaw),
				}
				for key, value := range meta {
					payload[key] = value
				}
				if lang := inferCopilotLanguageFromPath(filePath); lang != "" {
					payload["language"] = lang
				}
				if strings.TrimSpace(repo) != "" {
					payload["repository"] = strings.TrimSpace(repo)
				}
				if strings.TrimSpace(cwd) != "" {
					payload["cwd"] = strings.TrimSpace(cwd)
				}

				out = append(out, shared.TelemetryEvent{
					SchemaVersion: telemetrySchemaVersion,
					Channel:       shared.TelemetryChannelSQLite,
					OccurredAt:    occurredAt,
					AccountID:     "copilot",
					WorkspaceID:   workspaceID,
					SessionID:     sessionID,
					TurnID:        messageID,
					MessageID:     messageID,
					ToolCallID:    toolCallID,
					ProviderID:    "copilot",
					AgentName:     "copilot",
					EventType:     shared.TelemetryEventTypeToolUsage,
					ModelRaw:      "unknown",
					ToolName:      toolName,
					Requests:      shared.Int64Ptr(1),
					Status:        shared.TelemetryStatusOK,
					Payload:       payload,
				})
			}
		}
	}

	return out, nil
}

func copilotTelemetryTableExists(ctx context.Context, db *sql.DB, table string) bool {
	var exists int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM sqlite_master WHERE type='table' AND name=? LIMIT 1`,
		strings.TrimSpace(table),
	).Scan(&exists)
	return err == nil && exists == 1
}
