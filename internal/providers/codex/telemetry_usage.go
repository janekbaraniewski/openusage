package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	ID            string `json:"id"`
	SessionID     string `json:"session_id"`
	Model         string `json:"model"`
	CWD           string `json:"cwd"`
	Source        string `json:"source"`
	Originator    string `json:"originator"`
	ModelProvider string `json:"model_provider"`
}

type telemetryTurnContext struct {
	Model  string `json:"model"`
	TurnID string `json:"turn_id"`
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

const (
	codexTelemetryProviderID    = "codex"
	codexTelemetryUpstreamModel = "openai"
)

func (p *Provider) System() string { return p.ID() }

func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	sessionsDir := shared.ExpandHome(opts.Path("sessions_dir", DefaultTelemetrySessionsDir()))
	accountID := strings.TrimSpace(opts.Path("account_id", "codex-cli"))
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
		if accountID != "" {
			for i := range events {
				events[i].AccountID = accountID
			}
		}
		out = append(out, events...)
	}
	return out, nil
}

func (p *Provider) ParseHookPayload(raw []byte, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return ParseTelemetryNotifyPayload(raw, opts)
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
	upstreamProviderID := codexTelemetryUpstreamModel
	workspaceID := ""
	currentTurnID := ""
	clientName := "Other"
	clientSource := ""
	clientOriginator := ""
	var previous tokenUsage
	hasPrevious := false
	turnIndex := 0
	toolByCallID := make(map[string]int)

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
				if strings.TrimSpace(meta.ModelProvider) != "" {
					upstreamProviderID = strings.TrimSpace(meta.ModelProvider)
				}
				if ws := shared.SanitizeWorkspace(meta.CWD); ws != "" {
					workspaceID = ws
				}
				clientSource = strings.TrimSpace(meta.Source)
				clientOriginator = strings.TrimSpace(meta.Originator)
				clientName = classifyClient(clientSource, clientOriginator)
			}
		case "turn_context":
			var tc telemetryTurnContext
			if json.Unmarshal(ev.Payload, &tc) == nil {
				if strings.TrimSpace(tc.Model) != "" {
					model = strings.TrimSpace(tc.Model)
				}
				if strings.TrimSpace(tc.TurnID) != "" {
					currentTurnID = strings.TrimSpace(tc.TurnID)
				}
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
			if strings.TrimSpace(currentTurnID) != "" {
				turnID = strings.TrimSpace(currentTurnID)
			}
			if strings.TrimSpace(payload.RequestID) != "" {
				turnID = strings.TrimSpace(payload.RequestID)
			}
			messageID := strings.TrimSpace(payload.MessageID)
			if messageID == "" {
				messageID = turnID
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion:   "codex_session_v1",
				Channel:         shared.TelemetryChannelJSONL,
				OccurredAt:      occurredAt,
				AccountID:       "codex",
				WorkspaceID:     workspaceID,
				SessionID:       sessionID,
				TurnID:          turnID,
				MessageID:       messageID,
				ProviderID:      codexTelemetryProviderID,
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
					"source_file":       path,
					"line":              lineNumber,
					"upstream_provider": upstreamProviderID,
					"client":            clientName,
					"client_source":     clientSource,
					"client_originator": clientOriginator,
				},
			})
		case "response_item":
			var item responseItemPayload
			if json.Unmarshal(ev.Payload, &item) != nil {
				continue
			}

			occurredAt := time.Now().UTC()
			if ts, err := shared.ParseTimestampString(ev.Timestamp); err == nil {
				occurredAt = ts
			}

			switch item.Type {
			case "function_call", "custom_tool_call", "web_search_call":
				toolName := normalizeToolName(item.Name)
				if item.Type == "web_search_call" {
					toolName = "web_search"
				}
				if strings.TrimSpace(toolName) == "" {
					toolName = "unknown"
				}

				turnID := fmt.Sprintf("%s:tool:%d", sessionID, lineNumber)
				if strings.TrimSpace(currentTurnID) != "" {
					turnID = strings.TrimSpace(currentTurnID)
				}
				callID := strings.TrimSpace(item.CallID)
				messageID := shared.FirstNonEmpty(callID, turnID, fmt.Sprintf("%s:%d", sessionID, lineNumber))
				eventPayload := codexBuildToolPayload(path, lineNumber, item)
				if strings.TrimSpace(upstreamProviderID) != "" {
					eventPayload["upstream_provider"] = strings.TrimSpace(upstreamProviderID)
				}
				eventPayload["client"] = clientName
				if clientSource != "" {
					eventPayload["client_source"] = clientSource
				}
				if clientOriginator != "" {
					eventPayload["client_originator"] = clientOriginator
				}

				out = append(out, shared.TelemetryEvent{
					SchemaVersion: "codex_session_v1",
					Channel:       shared.TelemetryChannelJSONL,
					OccurredAt:    occurredAt,
					AccountID:     "codex",
					WorkspaceID:   workspaceID,
					SessionID:     sessionID,
					TurnID:        turnID,
					MessageID:     messageID,
					ToolCallID:    callID,
					ProviderID:    codexTelemetryProviderID,
					AgentName:     "codex",
					EventType:     shared.TelemetryEventTypeToolUsage,
					ModelRaw:      model,
					ToolName:      toolName,
					Requests:      shared.Int64Ptr(1),
					Status:        shared.TelemetryStatusOK,
					Payload:       eventPayload,
				})
				if callID != "" {
					toolByCallID[callID] = len(out) - 1
				}
			case "function_call_output", "custom_tool_call_output":
				callID := strings.TrimSpace(item.CallID)
				idx, ok := toolByCallID[callID]
				if !ok || idx < 0 || idx >= len(out) {
					continue
				}
				switch inferToolCallOutcome(item.Output) {
				case 2:
					out[idx].Status = shared.TelemetryStatusError
				case 3:
					out[idx].Status = shared.TelemetryStatusAborted
				default:
					out[idx].Status = shared.TelemetryStatusOK
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// ParseTelemetryNotifyPayload parses Codex notify hook payloads.
func ParseTelemetryNotifyPayload(raw []byte, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode codex notify payload: %w", err)
	}

	occurredAt := time.Now().UTC()
	if ts := shared.FirstPathNumber(root,
		[]string{"timestamp"},
		[]string{"occurred_at"},
		[]string{"time"},
	); ts != nil {
		occurredAt = shared.UnixAuto(int64(*ts))
	} else if rawTs := shared.FirstPathString(root, []string{"timestamp"}, []string{"occurred_at"}, []string{"time"}); rawTs != "" {
		if parsed, ok := shared.ParseFlexibleTimestamp(rawTs); ok {
			occurredAt = shared.UnixAuto(parsed)
		}
	}

	sessionID := shared.FirstPathString(root,
		[]string{"session_id"},
		[]string{"sessionID"},
		[]string{"session", "id"},
	)
	turnID := shared.FirstPathString(root,
		[]string{"turn_id"},
		[]string{"turnID"},
		[]string{"request_id"},
		[]string{"requestID"},
	)
	messageID := shared.FirstPathString(root,
		[]string{"message_id"},
		[]string{"messageID"},
		[]string{"last_assistant_message", "id"},
	)
	upstreamProviderID := shared.FirstNonEmpty(
		shared.FirstPathString(root, []string{"provider_id"}, []string{"providerID"}, []string{"provider"}),
		codexTelemetryUpstreamModel,
	)
	modelRaw := shared.FirstPathString(root,
		[]string{"model"},
		[]string{"model_id"},
		[]string{"modelID"},
		[]string{"last_assistant_message", "model"},
	)
	workspaceID := shared.SanitizeWorkspace(shared.FirstPathString(root,
		[]string{"cwd"},
		[]string{"workspace_id"},
		[]string{"workspaceID"},
	))
	accountID := shared.FirstNonEmpty(
		strings.TrimSpace(opts.Path("account_id", "")),
		shared.FirstPathString(root, []string{"account_id"}, []string{"accountID"}),
		"codex-cli",
	)
	eventStatus := codexHookEventStatus(root)
	hookSource := strings.TrimSpace(shared.FirstPathString(root, []string{"source"}))
	hookOriginator := strings.TrimSpace(shared.FirstPathString(root, []string{"originator"}))
	if hookSource != "" || hookOriginator != "" {
		root["client"] = classifyClient(hookSource, hookOriginator)
		if hookSource != "" {
			root["client_source"] = hookSource
		}
		if hookOriginator != "" {
			root["client_originator"] = hookOriginator
		}
	}
	if strings.TrimSpace(upstreamProviderID) != "" {
		root["upstream_provider"] = strings.TrimSpace(upstreamProviderID)
	}

	out := make([]shared.TelemetryEvent, 0, 2)

	if toolName, toolCallID, hasTool := codexExtractHookTool(root); hasTool {
		if paths := shared.ExtractFilePathsFromPayload(root); len(paths) > 0 {
			root["file"] = paths[0]
		}
		out = append(out, shared.TelemetryEvent{
			SchemaVersion: "codex_notify_v1",
			Channel:       shared.TelemetryChannelHook,
			OccurredAt:    occurredAt,
			AccountID:     accountID,
			WorkspaceID:   workspaceID,
			SessionID:     sessionID,
			TurnID:        turnID,
			MessageID:     messageID,
			ToolCallID:    toolCallID,
			ProviderID:    codexTelemetryProviderID,
			AgentName:     "codex",
			EventType:     shared.TelemetryEventTypeToolUsage,
			ModelRaw:      modelRaw,
			ToolName:      toolName,
			Requests:      shared.Int64Ptr(1),
			Status:        eventStatus,
			Payload:       root,
		})
	}

	usage := codexExtractHookUsage(root)
	if shared.HasHookUsage(usage) {
		out = append(out, shared.TelemetryEvent{
			SchemaVersion:    "codex_notify_v1",
			Channel:          shared.TelemetryChannelHook,
			OccurredAt:       occurredAt,
			AccountID:        accountID,
			WorkspaceID:      workspaceID,
			SessionID:        sessionID,
			TurnID:           turnID,
			MessageID:        messageID,
			ProviderID:       codexTelemetryProviderID,
			AgentName:        "codex",
			EventType:        shared.TelemetryEventTypeMessageUsage,
			ModelRaw:         modelRaw,
			InputTokens:      usage.InputTokens,
			OutputTokens:     usage.OutputTokens,
			ReasoningTokens:  usage.ReasoningTokens,
			CacheReadTokens:  usage.CacheReadTokens,
			CacheWriteTokens: usage.CacheWriteTokens,
			TotalTokens:      usage.TotalTokens,
			CostUSD:          usage.CostUSD,
			Requests:         shared.Int64Ptr(1),
			Status:           shared.TelemetryStatusOK,
			Payload:          root,
		})
	}

	if len(out) > 0 {
		return out, nil
	}

	return []shared.TelemetryEvent{{
		SchemaVersion: "codex_notify_v1",
		Channel:       shared.TelemetryChannelHook,
		OccurredAt:    occurredAt,
		AccountID:     accountID,
		WorkspaceID:   workspaceID,
		SessionID:     sessionID,
		TurnID:        turnID,
		MessageID:     messageID,
		ProviderID:    codexTelemetryProviderID,
		AgentName:     "codex",
		EventType:     shared.TelemetryEventTypeTurnCompleted,
		ModelRaw:      modelRaw,
		Requests:      shared.Int64Ptr(1),
		Status:        eventStatus,
		Payload:       root,
	}}, nil
}

func codexExtractHookUsage(root map[string]any) shared.HookUsage {
	input := shared.FirstPathNumber(root,
		[]string{"usage", "input_tokens"},
		[]string{"usage", "inputTokens"},
		[]string{"info", "total_token_usage", "input_tokens"},
		[]string{"last_assistant_message", "usage", "input_tokens"},
	)
	output := shared.FirstPathNumber(root,
		[]string{"usage", "output_tokens"},
		[]string{"usage", "outputTokens"},
		[]string{"info", "total_token_usage", "output_tokens"},
		[]string{"last_assistant_message", "usage", "output_tokens"},
	)
	reasoning := shared.FirstPathNumber(root,
		[]string{"usage", "reasoning_tokens"},
		[]string{"usage", "reasoning_output_tokens"},
		[]string{"info", "total_token_usage", "reasoning_output_tokens"},
		[]string{"last_assistant_message", "usage", "reasoning_tokens"},
	)
	cacheRead := shared.FirstPathNumber(root,
		[]string{"usage", "cache_read_tokens"},
		[]string{"usage", "cached_input_tokens"},
		[]string{"info", "total_token_usage", "cached_input_tokens"},
		[]string{"last_assistant_message", "usage", "cached_input_tokens"},
	)
	cacheWrite := shared.FirstPathNumber(root,
		[]string{"usage", "cache_write_tokens"},
		[]string{"last_assistant_message", "usage", "cache_write_tokens"},
	)
	total := shared.FirstPathNumber(root,
		[]string{"usage", "total_tokens"},
		[]string{"usage", "totalTokens"},
		[]string{"info", "total_token_usage", "total_tokens"},
		[]string{"last_assistant_message", "usage", "total_tokens"},
	)
	cost := shared.FirstPathNumber(root,
		[]string{"usage", "cost_usd"},
		[]string{"usage", "costUSD"},
		[]string{"cost_usd"},
		[]string{"costUSD"},
	)

	out := shared.HookUsage{
		InputTokens:      shared.NumberToInt64Ptr(input),
		OutputTokens:     shared.NumberToInt64Ptr(output),
		ReasoningTokens:  shared.NumberToInt64Ptr(reasoning),
		CacheReadTokens:  shared.NumberToInt64Ptr(cacheRead),
		CacheWriteTokens: shared.NumberToInt64Ptr(cacheWrite),
		TotalTokens:      shared.NumberToInt64Ptr(total),
		CostUSD:          shared.NumberToFloat64Ptr(cost),
	}
	out.SumTotalTokens()
	return out
}

func codexBuildToolPayload(sourcePath string, lineNumber int, item responseItemPayload) map[string]any {
	payload := map[string]any{
		"source_file": sourcePath,
		"line":        lineNumber,
	}

	setFirstToolPath := func(value any) {
		if _, exists := payload["file"]; exists {
			return
		}
		paths := shared.ExtractFilePathsFromPayload(value)
		if len(paths) > 0 {
			payload["file"] = paths[0]
		}
	}

	if parsed, ok := codexDecodeJSONValue(item.Arguments); ok {
		setFirstToolPath(parsed)
		if argsMap, ok := parsed.(map[string]any); ok {
			if cmd, ok := argsMap["cmd"].(string); ok && strings.TrimSpace(cmd) != "" {
				payload["command"] = cmd
				setFirstToolPath(map[string]any{"path": cmd})
			}
		}
	}
	if parsed, ok := codexDecodeJSONValue(item.Input); ok {
		setFirstToolPath(parsed)
	} else if strings.TrimSpace(item.Input) != "" {
		setFirstToolPath(map[string]any{"path": item.Input})
	}

	if strings.EqualFold(strings.TrimSpace(item.Name), "apply_patch") && strings.TrimSpace(item.Input) != "" {
		stats := patchStats{
			Files:   make(map[string]struct{}),
			Deleted: make(map[string]struct{}),
		}
		accumulatePatchStats(item.Input, &stats, make(map[string]int))
		if stats.Added > 0 {
			payload["lines_added"] = stats.Added
		}
		if stats.Removed > 0 {
			payload["lines_removed"] = stats.Removed
		}
		if _, exists := payload["file"]; !exists {
			if first := codexFirstFileFromPatchStats(stats); first != "" {
				payload["file"] = first
			}
		}
	}

	return payload
}

func codexDecodeJSONValue(raw any) (any, bool) {
	var body string
	switch v := raw.(type) {
	case string:
		body = strings.TrimSpace(v)
	case json.RawMessage:
		body = strings.TrimSpace(string(v))
	case []byte:
		body = strings.TrimSpace(string(v))
	default:
		return nil, false
	}
	if body == "" {
		return nil, false
	}

	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, false
	}
	return out, true
}

func codexFirstFileFromPatchStats(stats patchStats) string {
	files := make([]string, 0, len(stats.Files)+len(stats.Deleted))
	for file := range stats.Files {
		files = append(files, file)
	}
	for file := range stats.Deleted {
		files = append(files, file)
	}
	if len(files) == 0 {
		return ""
	}
	sort.Strings(files)
	return files[0]
}

func codexHookEventStatus(root map[string]any) shared.TelemetryStatus {
	switch strings.ToLower(strings.TrimSpace(shared.FirstPathString(root,
		[]string{"status"},
		[]string{"result"},
		[]string{"outcome"},
		[]string{"tool", "status"},
		[]string{"tool_result", "status"},
	))) {
	case "error", "failed", "failure":
		return shared.TelemetryStatusError
	case "aborted", "canceled", "cancelled":
		return shared.TelemetryStatusAborted
	default:
		return shared.TelemetryStatusOK
	}
}

func codexExtractHookTool(root map[string]any) (toolName, toolCallID string, ok bool) {
	eventName := strings.ToLower(shared.FirstNonEmpty(
		shared.FirstPathString(root, []string{"hook_event_name"}),
		shared.FirstPathString(root, []string{"hook_event"}),
		shared.FirstPathString(root, []string{"event"}),
		shared.FirstPathString(root, []string{"type"}),
	))
	toolName = strings.TrimSpace(shared.FirstPathString(root,
		[]string{"tool_name"},
		[]string{"toolName"},
		[]string{"tool", "name"},
		[]string{"tool"},
	))
	if toolName == "" && strings.Contains(eventName, "tool") {
		toolName = strings.TrimSpace(shared.FirstPathString(root, []string{"name"}))
	}
	if toolName == "" {
		return "", "", false
	}
	toolCallID = strings.TrimSpace(shared.FirstPathString(root,
		[]string{"tool_call_id"},
		[]string{"toolCallID"},
		[]string{"tool_call", "id"},
		[]string{"call_id"},
		[]string{"callID"},
	))
	if strings.Contains(eventName, "tool") || strings.HasPrefix(strings.ToLower(toolName), "mcp__") || toolCallID != "" {
		return normalizeToolName(toolName), toolCallID, true
	}
	return "", "", false
}
