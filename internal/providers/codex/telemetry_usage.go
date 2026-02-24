package codex

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

type telemetrySessionEvent struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type telemetrySessionMeta struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
}

type telemetryTurnContext struct {
	Model string `json:"model"`
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

func (p *Provider) System() string { return p.ID() }

func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	sessionsDir := shared.ExpandHome(opts.Path("sessions_dir", DefaultTelemetrySessionsDir()))
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
	var previous tokenUsage
	hasPrevious := false
	turnIndex := 0

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
			}
		case "turn_context":
			var tc telemetryTurnContext
			if json.Unmarshal(ev.Payload, &tc) == nil && strings.TrimSpace(tc.Model) != "" {
				model = strings.TrimSpace(tc.Model)
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
			if strings.TrimSpace(payload.RequestID) != "" {
				turnID = strings.TrimSpace(payload.RequestID)
			}
			messageID := strings.TrimSpace(payload.MessageID)
			if messageID == "" {
				messageID = fmt.Sprintf("%s:%d", sessionID, lineNumber)
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion:   "codex_session_v1",
				Channel:         shared.TelemetryChannelJSONL,
				OccurredAt:      occurredAt,
				AccountID:       "codex",
				SessionID:       sessionID,
				TurnID:          turnID,
				MessageID:       messageID,
				ProviderID:      "openai",
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
	if ts := codexFirstPathNumber(root,
		[]string{"timestamp"},
		[]string{"occurred_at"},
		[]string{"time"},
	); ts != nil {
		occurredAt = shared.UnixAuto(int64(*ts))
	} else if rawTs := codexFirstPathString(root, []string{"timestamp"}, []string{"occurred_at"}, []string{"time"}); rawTs != "" {
		if parsed, ok := shared.ParseFlexibleTimestamp(rawTs); ok {
			occurredAt = shared.UnixAuto(parsed)
		}
	}

	sessionID := codexFirstPathString(root,
		[]string{"session_id"},
		[]string{"sessionID"},
		[]string{"session", "id"},
	)
	turnID := codexFirstPathString(root,
		[]string{"turn_id"},
		[]string{"turnID"},
		[]string{"request_id"},
		[]string{"requestID"},
	)
	messageID := codexFirstPathString(root,
		[]string{"message_id"},
		[]string{"messageID"},
		[]string{"last_assistant_message", "id"},
	)
	providerID := shared.FirstNonEmpty(
		codexFirstPathString(root, []string{"provider_id"}, []string{"providerID"}, []string{"provider"}),
		"openai",
	)
	modelRaw := codexFirstPathString(root,
		[]string{"model"},
		[]string{"model_id"},
		[]string{"modelID"},
		[]string{"last_assistant_message", "model"},
	)
	workspaceID := shared.SanitizeWorkspace(codexFirstPathString(root,
		[]string{"cwd"},
		[]string{"workspace_id"},
		[]string{"workspaceID"},
	))
	accountID := shared.FirstNonEmpty(
		strings.TrimSpace(opts.Path("account_id", "")),
		codexFirstPathString(root, []string{"account_id"}, []string{"accountID"}),
		"codex",
	)

	usage := codexExtractHookUsage(root)
	if codexHasHookUsage(usage) {
		return []shared.TelemetryEvent{{
			SchemaVersion:    "codex_notify_v1",
			Channel:          shared.TelemetryChannelHook,
			OccurredAt:       occurredAt,
			AccountID:        accountID,
			WorkspaceID:      workspaceID,
			SessionID:        sessionID,
			TurnID:           turnID,
			MessageID:        messageID,
			ProviderID:       providerID,
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
		}}, nil
	}

	eventStatus := shared.TelemetryStatusOK
	switch strings.ToLower(strings.TrimSpace(codexFirstPathString(root, []string{"status"}, []string{"result"}, []string{"outcome"}))) {
	case "error", "failed", "failure":
		eventStatus = shared.TelemetryStatusError
	case "aborted", "canceled", "cancelled":
		eventStatus = shared.TelemetryStatusAborted
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
		ProviderID:    providerID,
		AgentName:     "codex",
		EventType:     shared.TelemetryEventTypeTurnCompleted,
		ModelRaw:      modelRaw,
		Requests:      shared.Int64Ptr(1),
		Status:        eventStatus,
		Payload:       root,
	}}, nil
}

type codexHookUsage struct {
	InputTokens      *int64
	OutputTokens     *int64
	ReasoningTokens  *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
	TotalTokens      *int64
	CostUSD          *float64
}

func codexExtractHookUsage(root map[string]any) codexHookUsage {
	input := codexFirstPathNumber(root,
		[]string{"usage", "input_tokens"},
		[]string{"usage", "inputTokens"},
		[]string{"info", "total_token_usage", "input_tokens"},
		[]string{"last_assistant_message", "usage", "input_tokens"},
	)
	output := codexFirstPathNumber(root,
		[]string{"usage", "output_tokens"},
		[]string{"usage", "outputTokens"},
		[]string{"info", "total_token_usage", "output_tokens"},
		[]string{"last_assistant_message", "usage", "output_tokens"},
	)
	reasoning := codexFirstPathNumber(root,
		[]string{"usage", "reasoning_tokens"},
		[]string{"usage", "reasoning_output_tokens"},
		[]string{"info", "total_token_usage", "reasoning_output_tokens"},
		[]string{"last_assistant_message", "usage", "reasoning_tokens"},
	)
	cacheRead := codexFirstPathNumber(root,
		[]string{"usage", "cache_read_tokens"},
		[]string{"usage", "cached_input_tokens"},
		[]string{"info", "total_token_usage", "cached_input_tokens"},
		[]string{"last_assistant_message", "usage", "cached_input_tokens"},
	)
	cacheWrite := codexFirstPathNumber(root,
		[]string{"usage", "cache_write_tokens"},
		[]string{"last_assistant_message", "usage", "cache_write_tokens"},
	)
	total := codexFirstPathNumber(root,
		[]string{"usage", "total_tokens"},
		[]string{"usage", "totalTokens"},
		[]string{"info", "total_token_usage", "total_tokens"},
		[]string{"last_assistant_message", "usage", "total_tokens"},
	)
	cost := codexFirstPathNumber(root,
		[]string{"usage", "cost_usd"},
		[]string{"usage", "costUSD"},
		[]string{"cost_usd"},
		[]string{"costUSD"},
	)

	out := codexHookUsage{
		InputTokens:      codexNumberToInt64Ptr(input),
		OutputTokens:     codexNumberToInt64Ptr(output),
		ReasoningTokens:  codexNumberToInt64Ptr(reasoning),
		CacheReadTokens:  codexNumberToInt64Ptr(cacheRead),
		CacheWriteTokens: codexNumberToInt64Ptr(cacheWrite),
		TotalTokens:      codexNumberToInt64Ptr(total),
		CostUSD:          codexNumberToFloat64Ptr(cost),
	}
	if out.TotalTokens == nil {
		var combined int64
		hasAny := false
		for _, part := range []*int64{out.InputTokens, out.OutputTokens, out.ReasoningTokens, out.CacheReadTokens, out.CacheWriteTokens} {
			if part != nil {
				combined += *part
				hasAny = true
			}
		}
		if hasAny {
			out.TotalTokens = shared.Int64Ptr(combined)
		}
	}
	return out
}

func codexHasHookUsage(u codexHookUsage) bool {
	for _, field := range []*int64{u.InputTokens, u.OutputTokens, u.ReasoningTokens, u.CacheReadTokens, u.CacheWriteTokens, u.TotalTokens} {
		if field != nil && *field > 0 {
			return true
		}
	}
	return u.CostUSD != nil && *u.CostUSD > 0
}

func codexFirstPathString(root map[string]any, paths ...[]string) string {
	for _, path := range paths {
		if value, ok := codexPathValue(root, path...); ok {
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

func codexFirstPathNumber(root map[string]any, paths ...[]string) *float64 {
	for _, path := range paths {
		if value, ok := codexPathValue(root, path...); ok {
			if parsed, ok := codexNumberFromAny(value); ok {
				return &parsed
			}
		}
	}
	return nil
}

func codexPathValue(root map[string]any, path ...string) (any, bool) {
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

func codexNumberFromAny(value any) (float64, bool) {
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

func codexNumberToInt64Ptr(v *float64) *int64 {
	if v == nil {
		return nil
	}
	return shared.Int64Ptr(int64(*v))
}

func codexNumberToFloat64Ptr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return shared.Float64Ptr(*v)
}
