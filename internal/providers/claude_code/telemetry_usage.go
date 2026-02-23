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
	if rawTs := claudeFirstPathString(root,
		[]string{"timestamp"},
		[]string{"occurred_at"},
		[]string{"time"},
	); rawTs != "" {
		if ts, ok := shared.ParseFlexibleTimestamp(rawTs); ok {
			occurredAt = shared.UnixAuto(ts)
		}
	} else if ts := claudeFirstPathNumber(root, []string{"timestamp"}); ts != nil {
		occurredAt = shared.UnixAuto(int64(*ts))
	}

	eventName := strings.ToLower(shared.FirstNonEmpty(
		claudeFirstPathString(root, []string{"hook_event_name"}),
		claudeFirstPathString(root, []string{"hook_event"}),
		claudeFirstPathString(root, []string{"event"}),
		claudeFirstPathString(root, []string{"type"}),
		"hook",
	))
	sessionID := claudeFirstPathString(root,
		[]string{"session_id"},
		[]string{"sessionId"},
		[]string{"session", "id"},
	)
	turnID := claudeFirstPathString(root,
		[]string{"request_id"},
		[]string{"requestId"},
		[]string{"turn_id"},
		[]string{"turnId"},
	)
	messageID := claudeFirstPathString(root,
		[]string{"message", "id"},
		[]string{"message_id"},
		[]string{"messageId"},
	)
	modelRaw := claudeFirstPathString(root,
		[]string{"model"},
		[]string{"model_id"},
		[]string{"message", "model"},
	)
	accountID := shared.FirstNonEmpty(
		strings.TrimSpace(opts.Path("account_id", "")),
		claudeFirstPathString(root, []string{"account_id"}, []string{"accountId"}),
		"claude-code",
	)
	workspaceID := shared.SanitizeWorkspace(claudeFirstPathString(root,
		[]string{"cwd"},
		[]string{"workspace_id"},
		[]string{"workspaceId"},
	))

	usage := claudeExtractHookUsage(root)
	if claudeHasHookUsage(usage) {
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
			claudeFirstPathString(root, []string{"tool_name"}),
			claudeFirstPathString(root, []string{"tool", "name"}),
			claudeFirstPathString(root, []string{"tool_input", "name"}),
			claudeFirstPathString(root, []string{"tool"}),
			"unknown",
		))
		toolCallID := claudeFirstPathString(root,
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
	switch strings.ToLower(strings.TrimSpace(claudeFirstPathString(root, []string{"decision"}, []string{"status"}))) {
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

type claudeHookUsage struct {
	InputTokens      *int64
	OutputTokens     *int64
	ReasoningTokens  *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
	TotalTokens      *int64
	CostUSD          *float64
}

func claudeExtractHookUsage(root map[string]any) claudeHookUsage {
	input := claudeFirstPathNumber(root,
		[]string{"usage", "input_tokens"},
		[]string{"message", "usage", "input_tokens"},
	)
	output := claudeFirstPathNumber(root,
		[]string{"usage", "output_tokens"},
		[]string{"message", "usage", "output_tokens"},
	)
	reasoning := claudeFirstPathNumber(root,
		[]string{"usage", "reasoning_tokens"},
		[]string{"message", "usage", "reasoning_tokens"},
	)
	cacheRead := claudeFirstPathNumber(root,
		[]string{"usage", "cache_read_input_tokens"},
		[]string{"message", "usage", "cache_read_input_tokens"},
	)
	cacheWrite := claudeFirstPathNumber(root,
		[]string{"usage", "cache_creation_input_tokens"},
		[]string{"message", "usage", "cache_creation_input_tokens"},
	)
	total := claudeFirstPathNumber(root,
		[]string{"usage", "total_tokens"},
		[]string{"message", "usage", "total_tokens"},
	)
	cost := claudeFirstPathNumber(root,
		[]string{"usage", "cost_usd"},
		[]string{"cost_usd"},
	)

	out := claudeHookUsage{
		InputTokens:      claudeNumberToInt64Ptr(input),
		OutputTokens:     claudeNumberToInt64Ptr(output),
		ReasoningTokens:  claudeNumberToInt64Ptr(reasoning),
		CacheReadTokens:  claudeNumberToInt64Ptr(cacheRead),
		CacheWriteTokens: claudeNumberToInt64Ptr(cacheWrite),
		TotalTokens:      claudeNumberToInt64Ptr(total),
		CostUSD:          claudeNumberToFloat64Ptr(cost),
	}
	if out.TotalTokens == nil {
		var total int64
		hasAny := false
		for _, tokenPart := range []*int64{out.InputTokens, out.OutputTokens, out.ReasoningTokens, out.CacheReadTokens, out.CacheWriteTokens} {
			if tokenPart != nil {
				total += *tokenPart
				hasAny = true
			}
		}
		if hasAny {
			out.TotalTokens = shared.Int64Ptr(total)
		}
	}
	return out
}

func claudeHasHookUsage(u claudeHookUsage) bool {
	for _, tokenPart := range []*int64{u.InputTokens, u.OutputTokens, u.ReasoningTokens, u.CacheReadTokens, u.CacheWriteTokens, u.TotalTokens} {
		if tokenPart != nil && *tokenPart > 0 {
			return true
		}
	}
	return u.CostUSD != nil && *u.CostUSD > 0
}

func claudeFirstPathString(root map[string]any, paths ...[]string) string {
	for _, path := range paths {
		if value, ok := claudePathValue(root, path...); ok {
			switch v := value.(type) {
			case string:
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					return trimmed
				}
			case json.Number:
				if trimmed := strings.TrimSpace(v.String()); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

func claudeFirstPathNumber(root map[string]any, paths ...[]string) *float64 {
	for _, path := range paths {
		if value, ok := claudePathValue(root, path...); ok {
			if parsed, ok := claudeNumberFromAny(value); ok {
				return &parsed
			}
		}
	}
	return nil
}

func claudePathValue(root map[string]any, path ...string) (any, bool) {
	var current any = root
	for _, segment := range path {
		node, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := node[segment]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func claudeNumberFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	case string:
		parsed, err := json.Number(strings.TrimSpace(v)).Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func claudeNumberToInt64Ptr(v *float64) *int64 {
	if v == nil {
		return nil
	}
	return shared.Int64Ptr(int64(*v))
}

func claudeNumberToFloat64Ptr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return shared.Float64Ptr(*v)
}
