package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	codexTelemetryProviderID    = "codex"
	codexTelemetryUpstreamModel = "openai"
)

func (p *Provider) System() string { return p.ID() }

func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			"sessions_dir": DefaultTelemetrySessionsDir(),
		},
	}
}

func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	sessionsDir := shared.ExpandHome(opts.Path("sessions_dir", DefaultTelemetrySessionsDir()))
	accountID := strings.TrimSpace(opts.Path("account_id", "codex-cli"))
	baselineExisting := codexBaselineExistingEnabled(opts)
	baselineRecentWindow := codexBaselineRecentWindow(opts)
	baselineCutoff := time.Now().Add(-baselineRecentWindow)

	fileInfos, err := shared.CollectFilesWithStat([]string{sessionsDir}, map[string]bool{".jsonl": true})
	if err != nil {
		return nil, fmt.Errorf("collect codex telemetry files: %w", err)
	}
	if len(fileInfos) == 0 {
		return nil, nil
	}

	p.telemetryCacheMu.Lock()
	defer p.telemetryCacheMu.Unlock()
	if p.telemetryCache == nil {
		p.telemetryCache = make(map[string]*telemetryCacheEntry)
	}
	baselineInitialFiles := baselineExisting && !p.telemetryBaselineInitialized
	p.telemetryBaselineInitialized = true

	var out []shared.TelemetryEvent
	pendingCache := make(map[string]*telemetryCacheEntry)
	for path, info := range fileInfos {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if entry, ok := p.telemetryCache[path]; ok {
			// Codex session logs are append-only. Some filesystem activity can
			// update mtime without adding bytes, which must not trigger a full
			// historical reparse.
			if entry.size == info.Size() {
				entry.modTime = info.ModTime()
				continue
			}
			if info.Size() > entry.byteOffset && entry.byteOffset > 0 && entry.state != nil {
				nextState := entry.state.clone()
				events, nextOffset, nextLineNumber, err := parseTelemetrySessionFileFrom(path, entry.byteOffset, entry.lineNumber, nextState)
				if err == nil && nextOffset >= entry.byteOffset {
					if accountID != "" {
						for i := range events {
							events[i].AccountID = accountID
						}
					}
					pendingCache[path] = &telemetryCacheEntry{
						modTime:    info.ModTime(),
						size:       info.Size(),
						byteOffset: nextOffset,
						lineNumber: nextLineNumber,
						state:      nextState,
					}
					out = append(out, events...)
					continue
				}
			}
		} else if baselineInitialFiles && (baselineRecentWindow == 0 || info.ModTime().Before(baselineCutoff)) {
			state, nextOffset, nextLineNumber, err := primeTelemetryParserState(path, info.Size())
			if err != nil {
				continue
			}
			p.telemetryCache[path] = &telemetryCacheEntry{
				modTime:    info.ModTime(),
				size:       info.Size(),
				byteOffset: nextOffset,
				lineNumber: nextLineNumber,
				state:      state,
			}
			continue
		}

		state := newTelemetryParserState(path)
		events, nextOffset, nextLineNumber, err := parseTelemetrySessionFileFrom(path, 0, 0, state)
		if err != nil {
			continue
		}
		if accountID != "" {
			for i := range events {
				events[i].AccountID = accountID
			}
		}
		pendingCache[path] = &telemetryCacheEntry{
			modTime:    info.ModTime(),
			size:       info.Size(),
			byteOffset: nextOffset,
			lineNumber: nextLineNumber,
			state:      state,
		}
		out = append(out, events...)
	}
	for path, entry := range pendingCache {
		p.telemetryCache[path] = entry
	}
	return out, nil
}

func codexBaselineExistingEnabled(opts shared.TelemetryCollectOptions) bool {
	value := strings.ToLower(strings.TrimSpace(opts.Path("baseline_existing", os.Getenv("OPENUSAGE_CODEX_BASELINE_EXISTING"))))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func codexBaselineRecentWindow(opts shared.TelemetryCollectOptions) time.Duration {
	value := strings.TrimSpace(opts.Path("baseline_recent_window", os.Getenv("OPENUSAGE_CODEX_BASELINE_RECENT_WINDOW")))
	if value == "" {
		return 10 * time.Minute
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 10 * time.Minute
	}
	return d
}

const (
	codexTelemetryPrimeInitialTailBytes int64 = 512 * 1024
	codexTelemetryPrimeMaxTailBytes     int64 = 8 * 1024 * 1024
)

// primeTelemetryParserState establishes the resume offset and cumulative token
// state without materializing telemetry events for history that is already in
// the local store. Only the first record and a bounded tail are inspected.
func primeTelemetryParserState(path string, size int64) (*telemetryParserState, int64, int, error) {
	state := newTelemetryParserState(path)

	firstFile, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	firstReader := bufio.NewReaderSize(firstFile, 512*1024)
	firstLine, readErr := firstReader.ReadBytes('\n')
	_ = firstFile.Close()
	if readErr != nil && readErr != io.EOF {
		return nil, 0, 0, readErr
	}
	if record, ok := decodeSessionLine(firstLine, 1); ok && record.SessionMeta != nil {
		applyTelemetrySessionMeta(state, record.SessionMeta)
	}

	baseState := state
	for tailBytes := codexTelemetryPrimeInitialTailBytes; ; tailBytes *= 2 {
		if tailBytes > codexTelemetryPrimeMaxTailBytes {
			tailBytes = codexTelemetryPrimeMaxTailBytes
		}
		state = baseState.clone()
		tailOffset := size - tailBytes
		if tailOffset < 0 {
			tailOffset = 0
		}
		if tailOffset > 0 {
			tailFile, err := os.Open(path)
			if err != nil {
				return nil, 0, 0, err
			}
			if _, err := tailFile.Seek(tailOffset, io.SeekStart); err != nil {
				_ = tailFile.Close()
				return nil, 0, 0, err
			}
			tailReader := bufio.NewReaderSize(tailFile, 512*1024)
			discarded, err := tailReader.ReadBytes('\n')
			_ = tailFile.Close()
			if err != nil && err != io.EOF {
				return nil, 0, 0, err
			}
			tailOffset += int64(len(discarded))
		}

		// The exact historical line number is intentionally not counted because
		// that would require rereading the whole file. Using the byte offset keeps
		// fallback IDs monotonic and collision-free for newly appended records.
		startLine := 0
		if tailOffset > 0 {
			startLine = int(tailOffset)
		}
		nextOffset, nextLineNumber, err := walkSessionFileFrom(path, tailOffset, startLine, func(record sessionLine) error {
			switch {
			case record.SessionMeta != nil:
				applyTelemetrySessionMeta(state, record.SessionMeta)
			case record.TurnContext != nil:
				applyTelemetryTurnContext(state, record.TurnContext)
			case record.EventPayload != nil && record.EventPayload.Type == "token_count" && record.EventPayload.Info != nil:
				state.previous = record.EventPayload.Info.TotalTokenUsage
				state.hasPrevious = true
				if state.previous.TotalTokens > 0 {
					state.turnIndex++
				}
			}
			return nil
		})
		if err != nil {
			return nil, nextOffset, nextLineNumber, err
		}
		if state.hasPrevious || tailOffset == 0 || tailBytes == codexTelemetryPrimeMaxTailBytes {
			return state, nextOffset, nextLineNumber, nil
		}
	}
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
	state := newTelemetryParserState(path)
	events, _, _, err := parseTelemetrySessionFileFrom(path, 0, 0, state)
	return events, err
}

type telemetryParserState struct {
	sessionID          string
	model              string
	upstreamProviderID string
	workspaceID        string
	currentTurnID      string
	clientName         string
	clientSource       string
	clientOriginator   string
	previous           tokenUsage
	hasPrevious        bool
	turnIndex          int
}

func newTelemetryParserState(path string) *telemetryParserState {
	return &telemetryParserState{
		sessionID:          strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		upstreamProviderID: codexTelemetryUpstreamModel,
		clientName:         "Other",
	}
}

func (s *telemetryParserState) clone() *telemetryParserState {
	if s == nil {
		return nil
	}
	cloned := *s
	return &cloned
}

func applyTelemetrySessionMeta(state *telemetryParserState, meta *sessionMetaPayload) {
	if state == nil || meta == nil {
		return
	}
	if sid := core.FirstNonEmpty(meta.SessionID, meta.ID); sid != "" {
		state.sessionID = sid
	}
	if strings.TrimSpace(meta.Model) != "" {
		state.model = strings.TrimSpace(meta.Model)
	}
	if strings.TrimSpace(meta.ModelProvider) != "" {
		state.upstreamProviderID = strings.TrimSpace(meta.ModelProvider)
	}
	if ws := shared.SanitizeWorkspace(meta.CWD); ws != "" {
		state.workspaceID = ws
	}
	state.clientSource = strings.TrimSpace(meta.Source)
	state.clientOriginator = strings.TrimSpace(meta.Originator)
	state.clientName = classifyClient(state.clientSource, state.clientOriginator)
}

func applyTelemetryTurnContext(state *telemetryParserState, turn *turnContextPayload) {
	if state == nil || turn == nil {
		return
	}
	if strings.TrimSpace(turn.Model) != "" {
		state.model = strings.TrimSpace(turn.Model)
	}
	if strings.TrimSpace(turn.TurnID) != "" {
		state.currentTurnID = strings.TrimSpace(turn.TurnID)
	}
}

func parseTelemetrySessionFileFrom(path string, byteOffset int64, lineNumber int, state *telemetryParserState) ([]shared.TelemetryEvent, int64, int, error) {
	if state == nil {
		state = newTelemetryParserState(path)
	}
	toolByCallID := make(map[string]int)

	var out []shared.TelemetryEvent
	nextOffset, nextLineNumber, err := walkSessionFileFrom(path, byteOffset, lineNumber, func(record sessionLine) error {
		switch {
		case record.SessionMeta != nil:
			applyTelemetrySessionMeta(state, record.SessionMeta)
		case record.TurnContext != nil:
			applyTelemetryTurnContext(state, record.TurnContext)
		case record.EventPayload != nil:
			payload := record.EventPayload
			if payload.Type != "token_count" || payload.Info == nil {
				return nil
			}

			total := payload.Info.TotalTokenUsage
			delta := total
			if state.hasPrevious {
				delta = usageDelta(total, state.previous)
				if !validUsageDelta(delta) {
					delta = total
				}
			}
			state.previous = total
			state.hasPrevious = true

			if delta.TotalTokens <= 0 {
				return nil
			}
			state.turnIndex++

			occurredAt := time.Now().UTC()
			if ts, err := shared.ParseTimestampString(record.Timestamp); err == nil {
				occurredAt = ts
			}

			turnID := fmt.Sprintf("%s:%d", state.sessionID, state.turnIndex)
			if strings.TrimSpace(state.currentTurnID) != "" {
				turnID = strings.TrimSpace(state.currentTurnID)
			}
			if strings.TrimSpace(payload.RequestID) != "" {
				turnID = strings.TrimSpace(payload.RequestID)
			}
			messageID := strings.TrimSpace(payload.MessageID)
			if messageID == "" {
				messageID = turnID
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: "codex_session_v1",
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurredAt,
				AccountID:     "codex",
				WorkspaceID:   state.workspaceID,
				SessionID:     state.sessionID,
				TurnID:        turnID,
				MessageID:     messageID,
				ProviderID:    codexTelemetryProviderID,
				AgentName:     "codex",
				EventType:     shared.TelemetryEventTypeMessageUsage,
				ModelRaw:      state.model,
				TokenUsage: core.TokenUsage{
					InputTokens:     core.Int64Ptr(int64(delta.InputTokens)),
					OutputTokens:    core.Int64Ptr(int64(delta.OutputTokens)),
					ReasoningTokens: core.Int64Ptr(int64(delta.ReasoningOutputTokens)),
					CacheReadTokens: core.Int64Ptr(int64(delta.CachedInputTokens)),
					TotalTokens:     core.Int64Ptr(int64(delta.TotalTokens)),
				},
				Status: shared.TelemetryStatusOK,
				Payload: map[string]any{
					"source_file":       path,
					"line":              record.LineNumber,
					"upstream_provider": state.upstreamProviderID,
					"client":            state.clientName,
					"client_source":     state.clientSource,
					"client_originator": state.clientOriginator,
				},
			})
		case record.ResponseItem != nil:
			item := record.ResponseItem
			occurredAt := time.Now().UTC()
			if ts, err := shared.ParseTimestampString(record.Timestamp); err == nil {
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

				turnID := fmt.Sprintf("%s:tool:%d", state.sessionID, record.LineNumber)
				if strings.TrimSpace(state.currentTurnID) != "" {
					turnID = strings.TrimSpace(state.currentTurnID)
				}
				callID := strings.TrimSpace(item.CallID)
				messageID := core.FirstNonEmpty(callID, turnID, fmt.Sprintf("%s:%d", state.sessionID, record.LineNumber))
				eventPayload := codexBuildToolPayload(path, record.LineNumber, *item)
				if strings.TrimSpace(state.upstreamProviderID) != "" {
					eventPayload["upstream_provider"] = strings.TrimSpace(state.upstreamProviderID)
				}
				eventPayload["client"] = state.clientName
				if state.clientSource != "" {
					eventPayload["client_source"] = state.clientSource
				}
				if state.clientOriginator != "" {
					eventPayload["client_originator"] = state.clientOriginator
				}

				out = append(out, shared.TelemetryEvent{
					SchemaVersion: "codex_session_v1",
					Channel:       shared.TelemetryChannelJSONL,
					OccurredAt:    occurredAt,
					AccountID:     "codex",
					WorkspaceID:   state.workspaceID,
					SessionID:     state.sessionID,
					TurnID:        turnID,
					MessageID:     messageID,
					ToolCallID:    callID,
					ProviderID:    codexTelemetryProviderID,
					AgentName:     "codex",
					EventType:     shared.TelemetryEventTypeToolUsage,
					ModelRaw:      state.model,
					TokenUsage: core.TokenUsage{
						Requests: core.Int64Ptr(1),
					},
					ToolName: toolName,
					Status:   shared.TelemetryStatusOK,
					Payload:  eventPayload,
				})
				if callID != "" {
					toolByCallID[callID] = len(out) - 1
				}
			case "function_call_output", "custom_tool_call_output":
				callID := strings.TrimSpace(item.CallID)
				idx, ok := toolByCallID[callID]
				if !ok || idx < 0 || idx >= len(out) {
					return nil
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
		return nil
	})
	if err != nil {
		return out, nextOffset, nextLineNumber, err
	}
	return out, nextOffset, nextLineNumber, nil
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
	upstreamProviderID := core.FirstNonEmpty(
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
	accountID := core.FirstNonEmpty(
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
			TokenUsage: core.TokenUsage{
				Requests: core.Int64Ptr(1),
			},
			ToolName: toolName,
			Status:   eventStatus,
			Payload:  root,
		})
	}

	usage := codexExtractHookUsage(root)
	if usage.HasTokenData() {
		out = append(out, shared.TelemetryEvent{
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
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(1),
		},
		Status:  eventStatus,
		Payload: root,
	}}, nil
}

func codexExtractHookUsage(root map[string]any) core.TokenUsage {
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
	eventName := strings.ToLower(core.FirstNonEmpty(
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
