package claude_code

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

const telemetryScannerBufferSize = 8 * 1024 * 1024

func (p *Provider) System() string { return p.ID() }

func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	defaultProjectsDir, defaultAltProjectsDir := DefaultTelemetryProjectsDirs()
	projectsDir := shared.ExpandHome(opts.Path("projects_dir", defaultProjectsDir))
	altProjectsDir := shared.ExpandHome(opts.Path("alt_projects_dir", defaultAltProjectsDir))

	files := shared.CollectFilesByExt([]string{projectsDir, altProjectsDir}, map[string]bool{".jsonl": true})
	if len(files) == 0 {
		return nil, nil
	}

	var out []shared.TelemetryEvent
	for _, file := range files {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		events, err := ParseTelemetryConversationFile(file)
		if err != nil {
			continue
		}
		out = append(out, events...)
	}
	return out, nil
}

func (p *Provider) ParseHookPayload(raw []byte, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return ParseTelemetryHookPayload(raw, opts)
}

// DefaultTelemetryProjectsDirs returns the default Claude Code conversation roots.
func DefaultTelemetryProjectsDirs() (string, string) {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		return "", ""
	}
	return filepath.Join(home, ".claude", "projects"), filepath.Join(home, ".config", "claude", "projects")
}

// ParseTelemetryConversationFile parses a Claude Code conversation JSONL file
// and emits message/tool telemetry events.
func ParseTelemetryConversationFile(path string) ([]shared.TelemetryEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seenUsage := make(map[string]bool)
	seenTools := make(map[string]bool)
	var out []shared.TelemetryEvent

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 512*1024), telemetryScannerBufferSize)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		var entry jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" || entry.Message == nil || entry.Message.Usage == nil {
			continue
		}

		usageKey := claudeTelemetryUsageDedupKey(entry)
		if usageKey != "" && seenUsage[usageKey] {
			continue
		}
		if usageKey != "" {
			seenUsage[usageKey] = true
		}

		ts := time.Now().UTC()
		if parsed, err := shared.ParseTimestampString(entry.Timestamp); err == nil {
			ts = parsed
		}

		model := strings.TrimSpace(entry.Message.Model)
		if model == "" {
			model = "unknown"
		}

		usage := entry.Message.Usage
		totalTokens := int64(
			usage.InputTokens +
				usage.OutputTokens +
				usage.CacheReadInputTokens +
				usage.CacheCreationInputTokens +
				usage.ReasoningTokens,
		)
		cost := estimateCost(model, usage)

		turnID := shared.FirstNonEmpty(entry.RequestID, entry.Message.ID)
		if turnID == "" {
			turnID = fmt.Sprintf("%s:%d", strings.TrimSpace(entry.SessionID), lineNumber)
		}
		messageID := strings.TrimSpace(entry.Message.ID)
		if messageID == "" {
			messageID = turnID
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion:    "claude_jsonl_v1",
			Channel:          shared.TelemetryChannelJSONL,
			OccurredAt:       ts,
			AccountID:        "claude-code",
			WorkspaceID:      shared.SanitizeWorkspace(entry.CWD),
			SessionID:        strings.TrimSpace(entry.SessionID),
			TurnID:           turnID,
			MessageID:        messageID,
			ProviderID:       "anthropic",
			AgentName:        "claude_code",
			EventType:        shared.TelemetryEventTypeMessageUsage,
			ModelRaw:         model,
			InputTokens:      shared.Int64Ptr(int64(usage.InputTokens)),
			OutputTokens:     shared.Int64Ptr(int64(usage.OutputTokens)),
			ReasoningTokens:  shared.Int64Ptr(int64(usage.ReasoningTokens)),
			CacheReadTokens:  shared.Int64Ptr(int64(usage.CacheReadInputTokens)),
			CacheWriteTokens: shared.Int64Ptr(int64(usage.CacheCreationInputTokens)),
			TotalTokens:      shared.Int64Ptr(totalTokens),
			CostUSD:          shared.Float64Ptr(cost),
			Status:           shared.TelemetryStatusOK,
			Payload: map[string]any{
				"file": path,
				"line": lineNumber,
			},
		})

		for idx, part := range entry.Message.Content {
			if part.Type != "tool_use" {
				continue
			}
			toolKey := claudeTelemetryToolDedupKey(entry, idx, part)
			if toolKey != "" && seenTools[toolKey] {
				continue
			}
			if toolKey != "" {
				seenTools[toolKey] = true
			}

			toolName := strings.ToLower(strings.TrimSpace(part.Name))
			if toolName == "" {
				toolName = "unknown"
			}

			// Extract tool's target file path from input for language inference.
			toolFilePath := ""
			if paths := shared.ExtractFilePathsFromPayload(part.Input); len(paths) > 0 {
				toolFilePath = paths[0]
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: "claude_jsonl_v1",
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    ts,
				AccountID:     "claude-code",
				WorkspaceID:   shared.SanitizeWorkspace(entry.CWD),
				SessionID:     strings.TrimSpace(entry.SessionID),
				TurnID:        turnID,
				MessageID:     messageID,
				ToolCallID:    strings.TrimSpace(part.ID),
				ProviderID:    "anthropic",
				AgentName:     "claude_code",
				EventType:     shared.TelemetryEventTypeToolUsage,
				ModelRaw:      model,
				ToolName:      toolName,
				Requests:      shared.Int64Ptr(1),
				Status:        shared.TelemetryStatusOK,
				Payload: map[string]any{
					"source_file": path,
					"line":        lineNumber,
					"file":        toolFilePath,
				},
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func claudeTelemetryUsageDedupKey(entry jsonlEntry) string {
	if id := strings.TrimSpace(entry.RequestID); id != "" {
		return "req:" + id
	}
	if entry.Message != nil {
		if id := strings.TrimSpace(entry.Message.ID); id != "" {
			return "msg:" + id
		}
		if entry.Message.Usage != nil {
			u := entry.Message.Usage
			return fmt.Sprintf("fp:%s|%s|%s|%d|%d|%d|%d|%d",
				strings.TrimSpace(entry.SessionID),
				strings.TrimSpace(entry.Timestamp),
				strings.TrimSpace(entry.Message.Model),
				u.InputTokens,
				u.OutputTokens,
				u.CacheReadInputTokens,
				u.CacheCreationInputTokens,
				u.ReasoningTokens,
			)
		}
	}
	return ""
}

func claudeTelemetryToolDedupKey(entry jsonlEntry, idx int, part jsonlContent) string {
	base := strings.TrimSpace(entry.RequestID)
	if base == "" && entry.Message != nil {
		base = strings.TrimSpace(entry.Message.ID)
	}
	if base == "" {
		base = strings.TrimSpace(entry.SessionID) + "|" + strings.TrimSpace(entry.Timestamp)
	}
	if id := strings.TrimSpace(part.ID); id != "" {
		return base + "|tool:" + id
	}
	name := strings.ToLower(strings.TrimSpace(part.Name))
	if name == "" {
		name = "unknown"
	}
	return fmt.Sprintf("%s|tool:%s|%d", base, name, idx)
}

// ParseTelemetryHookPayload parses Claude Code hook stdin payloads.
func ParseTelemetryHookPayload(raw []byte, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode claude hook payload: %w", err)
	}

	occurredAt := time.Now().UTC()
	if rawTs := shared.FirstPathString(root,
		[]string{"timestamp"},
		[]string{"occurred_at"},
		[]string{"time"},
	); rawTs != "" {
		if ts, ok := shared.ParseFlexibleTimestamp(rawTs); ok {
			occurredAt = shared.UnixAuto(ts)
		}
	} else if ts := shared.FirstPathNumber(root, []string{"timestamp"}); ts != nil {
		occurredAt = shared.UnixAuto(int64(*ts))
	}

	eventName := strings.ToLower(shared.FirstNonEmpty(
		shared.FirstPathString(root, []string{"hook_event_name"}),
		shared.FirstPathString(root, []string{"hook_event"}),
		shared.FirstPathString(root, []string{"event"}),
		shared.FirstPathString(root, []string{"type"}),
		"hook",
	))
	sessionID := shared.FirstPathString(root,
		[]string{"session_id"},
		[]string{"sessionId"},
		[]string{"session", "id"},
	)
	turnID := shared.FirstPathString(root,
		[]string{"request_id"},
		[]string{"requestId"},
		[]string{"turn_id"},
		[]string{"turnId"},
	)
	messageID := shared.FirstPathString(root,
		[]string{"message", "id"},
		[]string{"message_id"},
		[]string{"messageId"},
	)
	modelRaw := shared.FirstPathString(root,
		[]string{"model"},
		[]string{"model_id"},
		[]string{"message", "model"},
	)
	accountID := shared.FirstNonEmpty(
		strings.TrimSpace(opts.Path("account_id", "")),
		shared.FirstPathString(root, []string{"account_id"}, []string{"accountId"}),
		"claude-code",
	)
	workspaceID := shared.SanitizeWorkspace(shared.FirstPathString(root,
		[]string{"cwd"},
		[]string{"workspace_id"},
		[]string{"workspaceId"},
	))

	usage := claudeExtractHookUsage(root)
	if shared.HasHookUsage(usage) {
		return []shared.TelemetryEvent{{
			SchemaVersion:    "claude_hook_v1",
			Channel:          shared.TelemetryChannelHook,
			OccurredAt:       occurredAt,
			AccountID:        accountID,
			WorkspaceID:      workspaceID,
			SessionID:        sessionID,
			TurnID:           turnID,
			MessageID:        messageID,
			ProviderID:       "anthropic",
			AgentName:        "claude_code",
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
		}}, nil
	}

	if strings.Contains(eventName, "tool") {
		toolName := strings.ToLower(shared.FirstNonEmpty(
			shared.FirstPathString(root, []string{"tool_name"}),
			shared.FirstPathString(root, []string{"tool", "name"}),
			shared.FirstPathString(root, []string{"tool_input", "name"}),
			shared.FirstPathString(root, []string{"tool"}),
			"unknown",
		))
		toolCallID := shared.FirstPathString(root,
			[]string{"tool_call_id"},
			[]string{"toolUseID"},
			[]string{"tool_use_id"},
		)
		return []shared.TelemetryEvent{{
			SchemaVersion: "claude_hook_v1",
			Channel:       shared.TelemetryChannelHook,
			OccurredAt:    occurredAt,
			AccountID:     accountID,
			WorkspaceID:   workspaceID,
			SessionID:     sessionID,
			TurnID:        turnID,
			MessageID:     messageID,
			ToolCallID:    toolCallID,
			ProviderID:    "anthropic",
			AgentName:     "claude_code",
			EventType:     shared.TelemetryEventTypeToolUsage,
			ModelRaw:      modelRaw,
			ToolName:      toolName,
			Requests:      shared.Int64Ptr(1),
			Status:        shared.TelemetryStatusOK,
			Payload:       root,
		}}, nil
	}

	status := shared.TelemetryStatusOK
	switch strings.ToLower(strings.TrimSpace(shared.FirstPathString(root, []string{"decision"}, []string{"status"}))) {
	case "block", "blocked", "error", "failed":
		status = shared.TelemetryStatusError
	}

	return []shared.TelemetryEvent{{
		SchemaVersion: "claude_hook_v1",
		Channel:       shared.TelemetryChannelHook,
		OccurredAt:    occurredAt,
		AccountID:     accountID,
		WorkspaceID:   workspaceID,
		SessionID:     sessionID,
		TurnID:        turnID,
		MessageID:     messageID,
		ProviderID:    "anthropic",
		AgentName:     "claude_code",
		EventType:     shared.TelemetryEventTypeTurnCompleted,
		ModelRaw:      modelRaw,
		Requests:      shared.Int64Ptr(1),
		Status:        status,
		Payload:       root,
	}}, nil
}

func claudeExtractHookUsage(root map[string]any) shared.HookUsage {
	input := shared.FirstPathNumber(root,
		[]string{"usage", "input_tokens"},
		[]string{"message", "usage", "input_tokens"},
	)
	output := shared.FirstPathNumber(root,
		[]string{"usage", "output_tokens"},
		[]string{"message", "usage", "output_tokens"},
	)
	reasoning := shared.FirstPathNumber(root,
		[]string{"usage", "reasoning_tokens"},
		[]string{"message", "usage", "reasoning_tokens"},
	)
	cacheRead := shared.FirstPathNumber(root,
		[]string{"usage", "cache_read_input_tokens"},
		[]string{"message", "usage", "cache_read_input_tokens"},
	)
	cacheWrite := shared.FirstPathNumber(root,
		[]string{"usage", "cache_creation_input_tokens"},
		[]string{"message", "usage", "cache_creation_input_tokens"},
	)
	total := shared.FirstPathNumber(root,
		[]string{"usage", "total_tokens"},
		[]string{"message", "usage", "total_tokens"},
	)
	cost := shared.FirstPathNumber(root,
		[]string{"usage", "cost_usd"},
		[]string{"cost_usd"},
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
