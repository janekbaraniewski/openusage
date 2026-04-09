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

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func (p *Provider) System() string { return p.ID() }

func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	primary, alt := DefaultTelemetryProjectsDirs()
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			"projects_dir":     primary,
			"alt_projects_dir": alt,
		},
	}
}

func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	defaultProjectsDir, defaultAltProjectsDir := DefaultTelemetryProjectsDirs()
	projectsDir := shared.ExpandHome(opts.Path("projects_dir", defaultProjectsDir))
	altProjectsDir := shared.ExpandHome(opts.Path("alt_projects_dir", defaultAltProjectsDir))

	fileInfos := collectJSONLFilesWithStat(projectsDir)
	if altProjectsDir != "" {
		for k, v := range collectJSONLFilesWithStat(altProjectsDir) {
			fileInfos[k] = v
		}
	}
	if len(fileInfos) == 0 {
		return nil, nil
	}

	p.telemetryCacheMu.Lock()
	defer p.telemetryCacheMu.Unlock()
	if p.telemetryCache == nil {
		p.telemetryCache = make(map[string]*telemetryCacheEntry)
	}

	var out []shared.TelemetryEvent
	for path, info := range fileInfos {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		// Check cache: skip unchanged files entirely.
		if entry, ok := p.telemetryCache[path]; ok {
			if entry.modTime.Equal(info.ModTime()) && entry.size == info.Size() {
				out = append(out, entry.events...)
				continue
			}
			// File grew (append-only): parse only new lines.
			if info.Size() > entry.byteSize && entry.byteSize > 0 {
				newEvents, newSize, err := parseTelemetryConversationFileFrom(path, entry.byteSize)
				if err == nil && newSize > entry.byteSize {
					entry.events = append(entry.events, newEvents...)
					entry.modTime = info.ModTime()
					entry.size = info.Size()
					entry.byteSize = newSize
					out = append(out, entry.events...)
					continue
				}
			}
		}

		// Full parse (cache miss or file shrunk).
		events, err := ParseTelemetryConversationFile(path)
		if err != nil {
			continue
		}
		p.telemetryCache[path] = &telemetryCacheEntry{
			modTime:  info.ModTime(),
			size:     info.Size(),
			byteSize: info.Size(),
			events:   events,
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

// parseTelemetryConversationFileFrom parses only the NEW lines in a JSONL file
// starting from byteOffset. Returns the new events and the final file position.
// Used for incremental parsing of append-only conversation files.
func parseTelemetryConversationFileFrom(path string, byteOffset int64) ([]shared.TelemetryEvent, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, byteOffset, err
	}
	defer f.Close()

	if _, err := f.Seek(byteOffset, 0); err != nil {
		return nil, byteOffset, err
	}

	seenUsage := make(map[string]bool)
	seenTools := make(map[string]bool)
	var out []shared.TelemetryEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	lineNumber := 0 // approximate — we don't know exact line from offset

	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry jsonlEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" || entry.Message == nil {
			continue
		}
		ts, ok := parseJSONLTimestamp(entry.Timestamp)
		if !ok {
			continue
		}
		record := conversationRecord{
			lineNumber: lineNumber,
			timestamp:  ts,
			model:      entry.Message.Model,
			usage:      entry.Message.Usage,
			requestID:  entry.RequestID,
			messageID:  entry.Message.ID,
			sessionID:  entry.SessionID,
			cwd:        entry.CWD,
			sourcePath: path,
			content:    entry.Message.Content,
		}
		if record.model == "" {
			record.model = "unknown"
		}
		if record.usage == nil {
			continue
		}

		usageKey := conversationUsageDedupKey(record)
		if usageKey != "" && seenUsage[usageKey] {
			continue
		}
		if usageKey != "" {
			seenUsage[usageKey] = true
		}

		model := strings.TrimSpace(record.model)
		if model == "" {
			model = "unknown"
		}
		usage := record.usage
		totalTokens := conversationTotalTokens(usage)
		cost := estimateCost(model, usage)

		turnID := core.FirstNonEmpty(record.requestID, record.messageID)
		if turnID == "" {
			turnID = fmt.Sprintf("%s:%d", strings.TrimSpace(record.sessionID), record.lineNumber)
		}
		messageID := strings.TrimSpace(record.messageID)
		if messageID == "" {
			messageID = turnID
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: "claude_jsonl_v1",
			Channel:       shared.TelemetryChannelJSONL,
			OccurredAt:    ts,
			AccountID:     "claude-code",
			WorkspaceID:   shared.SanitizeWorkspace(record.cwd),
			SessionID:     strings.TrimSpace(record.sessionID),
			TurnID:        turnID,
			MessageID:     messageID,
			ProviderID:    "anthropic",
			AgentName:     "claude_code",
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      model,
			TokenUsage: core.TokenUsage{
				InputTokens:      core.Int64Ptr(int64(usage.InputTokens)),
				OutputTokens:     core.Int64Ptr(int64(usage.OutputTokens)),
				ReasoningTokens:  core.Int64Ptr(int64(usage.ReasoningTokens)),
				CacheReadTokens:  core.Int64Ptr(int64(usage.CacheReadInputTokens)),
				CacheWriteTokens: core.Int64Ptr(int64(usage.CacheCreationInputTokens)),
				TotalTokens:      core.Int64Ptr(totalTokens),
				CostUSD:          core.Float64Ptr(cost),
			},
			Status: shared.TelemetryStatusOK,
			Payload: map[string]any{
				"file": path,
			},
		})

		for idx, part := range record.content {
			if part.Type != "tool_use" {
				continue
			}
			toolKey := conversationToolDedupKey(record, idx, part)
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
			toolFilePath := ""
			if paths := shared.ExtractFilePathsFromPayload(part.Input); len(paths) > 0 {
				toolFilePath = paths[0]
			}
			out = append(out, shared.TelemetryEvent{
				SchemaVersion: "claude_jsonl_v1",
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    ts,
				AccountID:     "claude-code",
				WorkspaceID:   shared.SanitizeWorkspace(record.cwd),
				SessionID:     strings.TrimSpace(record.sessionID),
				TurnID:        turnID,
				MessageID:     messageID,
				ToolCallID:    strings.TrimSpace(part.ID),
				ProviderID:    "anthropic",
				AgentName:     "claude_code",
				EventType:     shared.TelemetryEventTypeToolUsage,
				ModelRaw:      model,
				TokenUsage: core.TokenUsage{
					Requests: core.Int64Ptr(1),
				},
				ToolName: toolName,
				Status:   shared.TelemetryStatusOK,
				Payload: map[string]any{
					"source_file": path,
					"file":        toolFilePath,
				},
			})
		}
	}

	// Calculate final position.
	finalPos, _ := f.Seek(0, 1) // current position after scanning
	if finalPos <= byteOffset {
		finalPos = byteOffset
	}
	return out, finalPos, nil
}

// ParseTelemetryConversationFile parses a Claude Code conversation JSONL file
// and emits message/tool telemetry events.
func ParseTelemetryConversationFile(path string) ([]shared.TelemetryEvent, error) {
	seenUsage := make(map[string]bool)
	seenTools := make(map[string]bool)
	var out []shared.TelemetryEvent
	records := parseConversationRecords(path)
	for _, record := range records {
		if record.usage == nil {
			continue
		}

		usageKey := conversationUsageDedupKey(record)
		if usageKey != "" && seenUsage[usageKey] {
			continue
		}
		if usageKey != "" {
			seenUsage[usageKey] = true
		}

		ts := record.timestamp
		model := strings.TrimSpace(record.model)
		if model == "" {
			model = "unknown"
		}

		usage := record.usage
		totalTokens := conversationTotalTokens(usage)
		cost := estimateCost(model, usage)

		turnID := core.FirstNonEmpty(record.requestID, record.messageID)
		if turnID == "" {
			turnID = fmt.Sprintf("%s:%d", strings.TrimSpace(record.sessionID), record.lineNumber)
		}
		messageID := strings.TrimSpace(record.messageID)
		if messageID == "" {
			messageID = turnID
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: "claude_jsonl_v1",
			Channel:       shared.TelemetryChannelJSONL,
			OccurredAt:    ts,
			AccountID:     "claude-code",
			WorkspaceID:   shared.SanitizeWorkspace(record.cwd),
			SessionID:     strings.TrimSpace(record.sessionID),
			TurnID:        turnID,
			MessageID:     messageID,
			ProviderID:    "anthropic",
			AgentName:     "claude_code",
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      model,
			TokenUsage: core.TokenUsage{
				InputTokens:      core.Int64Ptr(int64(usage.InputTokens)),
				OutputTokens:     core.Int64Ptr(int64(usage.OutputTokens)),
				ReasoningTokens:  core.Int64Ptr(int64(usage.ReasoningTokens)),
				CacheReadTokens:  core.Int64Ptr(int64(usage.CacheReadInputTokens)),
				CacheWriteTokens: core.Int64Ptr(int64(usage.CacheCreationInputTokens)),
				TotalTokens:      core.Int64Ptr(totalTokens),
				CostUSD:          core.Float64Ptr(cost),
			},
			Status: shared.TelemetryStatusOK,
			Payload: map[string]any{
				"file": path,
				"line": record.lineNumber,
			},
		})

		for idx, part := range record.content {
			if part.Type != "tool_use" {
				continue
			}
			toolKey := conversationToolDedupKey(record, idx, part)
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
				WorkspaceID:   shared.SanitizeWorkspace(record.cwd),
				SessionID:     strings.TrimSpace(record.sessionID),
				TurnID:        turnID,
				MessageID:     messageID,
				ToolCallID:    strings.TrimSpace(part.ID),
				ProviderID:    "anthropic",
				AgentName:     "claude_code",
				EventType:     shared.TelemetryEventTypeToolUsage,
				ModelRaw:      model,
				TokenUsage: core.TokenUsage{
					Requests: core.Int64Ptr(1),
				},
				ToolName: toolName,
				Status:   shared.TelemetryStatusOK,
				Payload: map[string]any{
					"source_file": path,
					"line":        record.lineNumber,
					"file":        toolFilePath,
				},
			})
		}
	}
	return out, nil
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

	eventName := strings.ToLower(core.FirstNonEmpty(
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
	accountID := core.FirstNonEmpty(
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
	if usage.HasTokenData() {
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
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      modelRaw,
			TokenUsage: core.TokenUsage{
				InputTokens:      usage.InputTokens,
				OutputTokens:     usage.OutputTokens,
				ReasoningTokens:  usage.ReasoningTokens,
				CacheReadTokens:  usage.CacheReadTokens,
				CacheWriteTokens: usage.CacheWriteTokens,
				TotalTokens:      usage.TotalTokens,
				CostUSD:          usage.CostUSD,
				Requests:         core.Int64Ptr(1),
			},
			Status:  shared.TelemetryStatusOK,
			Payload: root,
		}}, nil
	}

	if strings.Contains(eventName, "tool") {
		toolName := strings.ToLower(core.FirstNonEmpty(
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
			TokenUsage: core.TokenUsage{
				Requests: core.Int64Ptr(1),
			},
			ToolName: toolName,
			Status:   shared.TelemetryStatusOK,
			Payload:  root,
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
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(1),
		},
		Status:  status,
		Payload: root,
	}}, nil
}

func claudeExtractHookUsage(root map[string]any) core.TokenUsage {
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

	out := core.TokenUsage{
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
