package opencode

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetryEventSchema  = "opencode_event_v1"
	telemetryHookSchema   = "opencode_hook_v1"
	telemetrySQLiteSchema = "opencode_sqlite_v1"
)

type eventEnvelope struct {
	Type       string          `json:"type"`
	Event      string          `json:"event"`
	Properties json.RawMessage `json:"properties"`
	Payload    json.RawMessage `json:"payload"`
}

type messageUpdatedProps struct {
	Info assistantInfo `json:"info"`
}

type assistantInfo struct {
	ID         string  `json:"id"`
	SessionID  string  `json:"sessionID"`
	Role       string  `json:"role"`
	ParentID   string  `json:"parentID"`
	ModelID    string  `json:"modelID"`
	ProviderID string  `json:"providerID"`
	Cost       float64 `json:"cost"`
	Tokens     struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
	Time struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed"`
	} `json:"time"`
	Path struct {
		CWD string `json:"cwd"`
	} `json:"path"`
}

type toolPayload struct {
	SessionID  string `json:"sessionID"`
	MessageID  string `json:"messageID"`
	ToolCallID string `json:"toolCallID"`
	ToolName   string `json:"toolName"`
	Name       string `json:"name"`
	Timestamp  int64  `json:"timestamp"`
}

type hookToolExecuteAfterInput struct {
	Tool      string `json:"tool"`
	SessionID string `json:"sessionID"`
	CallID    string `json:"callID"`
}

type hookToolExecuteAfterOutput struct {
	Title string `json:"title"`
}

type hookChatMessageInput struct {
	SessionID string `json:"sessionID"`
	Agent     string `json:"agent"`
	MessageID string `json:"messageID"`
	Variant   string `json:"variant"`
	Model     struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model"`
}

type hookChatMessageOutput struct {
	Message struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionID"`
		Role      string `json:"role"`
	} `json:"message"`
	PartsCount int64 `json:"parts_count"`
}

type usage struct {
	InputTokens      *int64
	OutputTokens     *int64
	ReasoningTokens  *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
	TotalTokens      *int64
	CostUSD          *float64
}

func (u usage) toMap() map[string]any {
	return map[string]any{
		"input_tokens":       ptrInt64Value(u.InputTokens),
		"output_tokens":      ptrInt64Value(u.OutputTokens),
		"reasoning_tokens":   ptrInt64Value(u.ReasoningTokens),
		"cache_read_tokens":  ptrInt64Value(u.CacheReadTokens),
		"cache_write_tokens": ptrInt64Value(u.CacheWriteTokens),
		"total_tokens":       ptrInt64Value(u.TotalTokens),
		"cost_usd":           ptrFloat64Value(u.CostUSD),
	}
}

type partSummary struct {
	PartsTotal  int64
	PartsByType map[string]int64
}

type telemetrySource struct{}

func NewTelemetrySource() shared.TelemetrySource { return telemetrySource{} }

func (telemetrySource) System() string { return "opencode" }

func (telemetrySource) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	home, _ := os.UserHomeDir()
	defaultDirs := []string{
		filepath.Join(home, ".opencode", "events"),
		filepath.Join(home, ".opencode", "logs"),
		filepath.Join(home, ".local", "state", "opencode", "events"),
		filepath.Join(home, ".local", "state", "opencode", "logs"),
	}

	dbPath := shared.ExpandHome(opts.Path("db_path", ""))
	eventsDirs := opts.PathsFor("events_dirs", defaultDirs)
	eventsFile := opts.Path("events_file", "")
	accountID := shared.FirstNonEmpty(opts.Path("account_id", ""), "zen")

	seenMessage := map[string]bool{}
	seenTools := map[string]bool{}
	var out []shared.TelemetryEvent

	if strings.TrimSpace(dbPath) != "" {
		events, err := CollectTelemetryFromSQLite(ctx, dbPath)
		if err == nil {
			appendDedupTelemetryEvents(&out, events, seenMessage, seenTools, accountID)
		}
	}

	roots := append([]string{}, eventsDirs...)
	if strings.TrimSpace(eventsFile) != "" {
		roots = append(roots, eventsFile)
	}
	files := shared.CollectFilesByExt(roots, map[string]bool{".jsonl": true, ".ndjson": true})
	for _, file := range files {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		events, err := ParseTelemetryEventFile(file)
		if err != nil {
			continue
		}
		appendDedupTelemetryEvents(&out, events, seenMessage, seenTools, accountID)
	}

	return out, nil
}

func (telemetrySource) ParseHookPayload(raw []byte, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	events, err := ParseTelemetryHookPayload(raw)
	if err != nil {
		return nil, err
	}
	accountID := shared.FirstNonEmpty(opts.Path("account_id", ""), "zen")
	for i := range events {
		events[i].AccountID = shared.FirstNonEmpty(events[i].AccountID, accountID)
	}
	return events, nil
}

// DefaultTelemetryDBPath returns the default OpenCode SQLite location.
func DefaultTelemetryDBPath() string {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

// ParseTelemetryEventFile parses OpenCode event jsonl/ndjson files.
func ParseTelemetryEventFile(path string) ([]shared.TelemetryEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []shared.TelemetryEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 512*1024), 8*1024*1024)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		var ev eventEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}

		typ := strings.TrimSpace(ev.Type)
		if typ == "" {
			typ = strings.TrimSpace(ev.Event)
		}
		switch typ {
		case "message.updated":
			var props messageUpdatedProps
			if err := json.Unmarshal(ev.Properties, &props); err != nil {
				continue
			}
			info := props.Info
			if strings.ToLower(strings.TrimSpace(info.Role)) != "assistant" {
				continue
			}

			messageID := strings.TrimSpace(info.ID)
			if messageID == "" {
				messageID = fmt.Sprintf("%s:%d", path, lineNumber)
			}
			total := info.Tokens.Input + info.Tokens.Output + info.Tokens.Reasoning + info.Tokens.Cache.Read + info.Tokens.Cache.Write
			occurred := shared.UnixAuto(info.Time.Created)
			if info.Time.Completed > 0 {
				occurred = shared.UnixAuto(info.Time.Completed)
			}

			providerID := strings.TrimSpace(info.ProviderID)
			if providerID == "" {
				providerID = "zen"
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion:    telemetryEventSchema,
				Channel:          shared.TelemetryChannelJSONL,
				OccurredAt:       occurred,
				AccountID:        "zen",
				WorkspaceID:      shared.SanitizeWorkspace(info.Path.CWD),
				SessionID:        strings.TrimSpace(info.SessionID),
				TurnID:           strings.TrimSpace(info.ParentID),
				MessageID:        messageID,
				ProviderID:       providerID,
				AgentName:        "opencode",
				EventType:        shared.TelemetryEventTypeMessageUsage,
				ModelRaw:         strings.TrimSpace(info.ModelID),
				InputTokens:      shared.Int64Ptr(info.Tokens.Input),
				OutputTokens:     shared.Int64Ptr(info.Tokens.Output),
				ReasoningTokens:  shared.Int64Ptr(info.Tokens.Reasoning),
				CacheReadTokens:  shared.Int64Ptr(info.Tokens.Cache.Read),
				CacheWriteTokens: shared.Int64Ptr(info.Tokens.Cache.Write),
				TotalTokens:      shared.Int64Ptr(total),
				CostUSD:          shared.Float64Ptr(info.Cost),
				Status:           shared.TelemetryStatusOK,
				Payload: map[string]any{
					"file": path,
					"line": lineNumber,
				},
			})

		case "tool.execute.after":
			if len(ev.Payload) == 0 {
				continue
			}
			var tool toolPayload
			if err := json.Unmarshal(ev.Payload, &tool); err != nil {
				continue
			}
			toolID := strings.TrimSpace(tool.ToolCallID)
			if toolID == "" {
				toolID = fmt.Sprintf("%s:%d", path, lineNumber)
			}

			name := strings.TrimSpace(tool.ToolName)
			if name == "" {
				name = strings.TrimSpace(tool.Name)
			}
			if name == "" {
				name = "unknown"
			}
			occurred := time.Now().UTC()
			if tool.Timestamp > 0 {
				occurred = shared.UnixAuto(tool.Timestamp)
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetryEventSchema,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurred,
				AccountID:     "zen",
				SessionID:     strings.TrimSpace(tool.SessionID),
				MessageID:     strings.TrimSpace(tool.MessageID),
				ToolCallID:    toolID,
				ProviderID:    "zen",
				AgentName:     "opencode",
				EventType:     shared.TelemetryEventTypeToolUsage,
				ToolName:      strings.ToLower(name),
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

// CollectTelemetryFromSQLite parses OpenCode SQLite data (message + part tables).
func CollectTelemetryFromSQLite(ctx context.Context, dbPath string) ([]shared.TelemetryEvent, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if !sqliteTableExists(ctx, db, "message") {
		return nil, nil
	}

	partSummaryByMessage := make(map[string]partSummary)
	if sqliteTableExists(ctx, db, "part") {
		partSummaryByMessage, _ = collectPartSummary(ctx, db)
	}

	var out []shared.TelemetryEvent

	messageRows, err := db.QueryContext(ctx, `
		SELECT m.id, m.session_id, m.time_created, m.time_updated, m.data, COALESCE(s.directory, '')
		FROM message m
		LEFT JOIN session s ON s.id = m.session_id
		ORDER BY m.time_updated ASC
	`)
	if err == nil {
		for messageRows.Next() {
			if ctx.Err() != nil {
				_ = messageRows.Close()
				return out, ctx.Err()
			}

			var (
				messageIDRaw string
				sessionIDRaw string
				timeCreated  int64
				timeUpdated  int64
				messageJSON  string
				sessionDir   string
			)
			if err := messageRows.Scan(&messageIDRaw, &sessionIDRaw, &timeCreated, &timeUpdated, &messageJSON, &sessionDir); err != nil {
				continue
			}
			payload := decodeJSONMap([]byte(messageJSON))
			if strings.ToLower(firstPathString(payload, []string{"role"})) != "assistant" {
				continue
			}

			u := extractUsage(payload)
			completedAt := ptrInt64FromFloat(firstPathNumber(payload, []string{"time", "completed"}))
			createdAt := ptrInt64FromFloat(firstPathNumber(payload, []string{"time", "created"}))
			if completedAt <= 0 && !hasUsage(u) {
				continue
			}

			messageID := shared.FirstNonEmpty(strings.TrimSpace(messageIDRaw), firstPathString(payload, []string{"id"}), firstPathString(payload, []string{"messageID"}))
			if messageID == "" {
				continue
			}
			providerID := shared.FirstNonEmpty(firstPathString(payload, []string{"providerID"}), firstPathString(payload, []string{"model", "providerID"}), "zen")
			modelRaw := shared.FirstNonEmpty(firstPathString(payload, []string{"modelID"}), firstPathString(payload, []string{"model", "modelID"}))
			sessionID := shared.FirstNonEmpty(strings.TrimSpace(sessionIDRaw), firstPathString(payload, []string{"sessionID"}))
			turnID := shared.FirstNonEmpty(firstPathString(payload, []string{"parentID"}), firstPathString(payload, []string{"turnID"}))

			occurredAt := shared.UnixAuto(timeUpdated)
			switch {
			case completedAt > 0:
				occurredAt = shared.UnixAuto(completedAt)
			case createdAt > 0:
				occurredAt = shared.UnixAuto(createdAt)
			case timeCreated > 0:
				occurredAt = shared.UnixAuto(timeCreated)
			}

			eventStatus := shared.TelemetryStatusOK
			finish := strings.ToLower(firstPathString(payload, []string{"finish"}))
			if strings.Contains(finish, "error") || strings.Contains(finish, "fail") {
				eventStatus = shared.TelemetryStatusError
			}
			if strings.Contains(finish, "abort") || strings.Contains(finish, "cancel") {
				eventStatus = shared.TelemetryStatusAborted
			}

			contextSummary := map[string]any{}
			if summary, ok := partSummaryByMessage[messageID]; ok {
				partsByType := make(map[string]any, len(summary.PartsByType))
				for partType, count := range summary.PartsByType {
					partsByType[partType] = count
				}
				contextSummary = map[string]any{
					"parts_total":   summary.PartsTotal,
					"parts_by_type": partsByType,
				}
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion:    telemetrySQLiteSchema,
				Channel:          shared.TelemetryChannelSQLite,
				OccurredAt:       occurredAt,
				AccountID:        "zen",
				WorkspaceID:      shared.SanitizeWorkspace(shared.FirstNonEmpty(firstPathString(payload, []string{"path", "cwd"}), firstPathString(payload, []string{"path", "root"}), strings.TrimSpace(sessionDir))),
				SessionID:        sessionID,
				TurnID:           turnID,
				MessageID:        messageID,
				ProviderID:       providerID,
				AgentName:        shared.FirstNonEmpty(firstPathString(payload, []string{"agent"}), "opencode"),
				EventType:        shared.TelemetryEventTypeMessageUsage,
				ModelRaw:         modelRaw,
				InputTokens:      u.InputTokens,
				OutputTokens:     u.OutputTokens,
				ReasoningTokens:  u.ReasoningTokens,
				CacheReadTokens:  u.CacheReadTokens,
				CacheWriteTokens: u.CacheWriteTokens,
				TotalTokens:      u.TotalTokens,
				CostUSD:          u.CostUSD,
				Status:           eventStatus,
				Payload: map[string]any{
					"source": map[string]any{
						"db_path": dbPath,
						"table":   "message",
					},
					"db": map[string]any{
						"message_id":   strings.TrimSpace(messageIDRaw),
						"session_id":   strings.TrimSpace(sessionIDRaw),
						"time_created": timeCreated,
						"time_updated": timeUpdated,
					},
					"message": payload,
					"context": contextSummary,
				},
			})
		}
		_ = messageRows.Close()
	}

	if !sqliteTableExists(ctx, db, "part") {
		return out, nil
	}

	seenTools := map[string]bool{}
	toolRows, err := db.QueryContext(ctx, `
		SELECT p.id, p.message_id, p.session_id, p.time_created, p.time_updated, p.data, COALESCE(m.data, '{}'), COALESCE(s.directory, '')
		FROM part p
		LEFT JOIN message m ON m.id = p.message_id
		LEFT JOIN session s ON s.id = p.session_id
		WHERE COALESCE(json_extract(p.data, '$.type'), '') = 'tool'
		ORDER BY p.time_updated ASC
	`)
	if err != nil {
		return out, nil
	}
	defer toolRows.Close()

	for toolRows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		var (
			partID      string
			messageIDDB string
			sessionIDDB string
			timeCreated int64
			timeUpdated int64
			partJSON    string
			messageJSON string
			sessionDir  string
		)
		if err := toolRows.Scan(&partID, &messageIDDB, &sessionIDDB, &timeCreated, &timeUpdated, &partJSON, &messageJSON, &sessionDir); err != nil {
			continue
		}

		partPayload := decodeJSONMap([]byte(partJSON))
		messagePayload := decodeJSONMap([]byte(messageJSON))

		toolCallID := shared.FirstNonEmpty(firstPathString(partPayload, []string{"callID"}), firstPathString(partPayload, []string{"call_id"}), strings.TrimSpace(partID))
		if toolCallID == "" || seenTools[toolCallID] {
			continue
		}

		statusRaw := strings.ToLower(firstPathString(partPayload, []string{"state", "status"}))
		status, include := mapToolStatus(statusRaw)
		if !include {
			continue
		}
		seenTools[toolCallID] = true

		toolName := strings.ToLower(shared.FirstNonEmpty(firstPathString(partPayload, []string{"tool"}), firstPathString(partPayload, []string{"name"}), "unknown"))
		sessionID := shared.FirstNonEmpty(strings.TrimSpace(sessionIDDB), firstPathString(partPayload, []string{"sessionID"}), firstPathString(messagePayload, []string{"sessionID"}))
		messageID := shared.FirstNonEmpty(strings.TrimSpace(messageIDDB), firstPathString(partPayload, []string{"messageID"}), firstPathString(messagePayload, []string{"id"}))
		providerID := shared.FirstNonEmpty(firstPathString(messagePayload, []string{"providerID"}), firstPathString(messagePayload, []string{"model", "providerID"}), "zen")
		modelRaw := shared.FirstNonEmpty(firstPathString(messagePayload, []string{"modelID"}), firstPathString(messagePayload, []string{"model", "modelID"}))

		occurredAt := shared.UnixAuto(timeUpdated)
		if ts := ptrInt64FromFloat(firstPathNumber(partPayload,
			[]string{"state", "time", "end"},
			[]string{"state", "time", "start"},
			[]string{"time", "end"},
			[]string{"time", "start"},
		)); ts > 0 {
			occurredAt = shared.UnixAuto(ts)
		} else if timeCreated > 0 {
			occurredAt = shared.UnixAuto(timeCreated)
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetrySQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    occurredAt,
			AccountID:     "zen",
			WorkspaceID: shared.SanitizeWorkspace(shared.FirstNonEmpty(
				firstPathString(messagePayload, []string{"path", "cwd"}),
				firstPathString(messagePayload, []string{"path", "root"}),
				strings.TrimSpace(sessionDir),
			)),
			SessionID:  sessionID,
			MessageID:  messageID,
			ToolCallID: toolCallID,
			ProviderID: providerID,
			AgentName:  shared.FirstNonEmpty(firstPathString(messagePayload, []string{"agent"}), "opencode"),
			EventType:  shared.TelemetryEventTypeToolUsage,
			ModelRaw:   modelRaw,
			ToolName:   toolName,
			Requests:   shared.Int64Ptr(1),
			Status:     status,
			Payload: map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "part",
				},
				"db": map[string]any{
					"part_id":      strings.TrimSpace(partID),
					"message_id":   strings.TrimSpace(messageIDDB),
					"session_id":   strings.TrimSpace(sessionIDDB),
					"time_created": timeCreated,
					"time_updated": timeUpdated,
				},
				"part":    partPayload,
				"message": messagePayload,
			},
		})
	}

	return out, nil
}

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

	typ := strings.TrimSpace(ev.Type)
	if typ == "" {
		typ = strings.TrimSpace(ev.Event)
	}
	switch typ {
	case "message.updated":
		var props messageUpdatedProps
		if err := json.Unmarshal(ev.Properties, &props); err != nil {
			return nil, fmt.Errorf("decode message.updated properties: %w", err)
		}
		info := props.Info
		if strings.ToLower(strings.TrimSpace(info.Role)) != "assistant" {
			if includeUnknown {
				return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryEventSchema, typ)}, nil
			}
			return nil, nil
		}
		messageID := strings.TrimSpace(info.ID)
		if messageID == "" {
			if includeUnknown {
				return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryEventSchema, typ)}, nil
			}
			return nil, nil
		}
		providerID := shared.FirstNonEmpty(strings.TrimSpace(info.ProviderID), "zen")
		occurredAt := shared.UnixAuto(info.Time.Created)
		if info.Time.Completed > 0 {
			occurredAt = shared.UnixAuto(info.Time.Completed)
		}
		totalTokens := info.Tokens.Input + info.Tokens.Output + info.Tokens.Reasoning + info.Tokens.Cache.Read + info.Tokens.Cache.Write

		return []shared.TelemetryEvent{{
			SchemaVersion:    telemetryEventSchema,
			Channel:          shared.TelemetryChannelHook,
			OccurredAt:       occurredAt,
			AccountID:        "zen",
			WorkspaceID:      shared.SanitizeWorkspace(info.Path.CWD),
			SessionID:        strings.TrimSpace(info.SessionID),
			TurnID:           strings.TrimSpace(info.ParentID),
			MessageID:        messageID,
			ProviderID:       providerID,
			AgentName:        "opencode",
			EventType:        shared.TelemetryEventTypeMessageUsage,
			ModelRaw:         strings.TrimSpace(info.ModelID),
			InputTokens:      shared.Int64Ptr(info.Tokens.Input),
			OutputTokens:     shared.Int64Ptr(info.Tokens.Output),
			ReasoningTokens:  shared.Int64Ptr(info.Tokens.Reasoning),
			CacheReadTokens:  shared.Int64Ptr(info.Tokens.Cache.Read),
			CacheWriteTokens: shared.Int64Ptr(info.Tokens.Cache.Write),
			TotalTokens:      shared.Int64Ptr(totalTokens),
			CostUSD:          shared.Float64Ptr(info.Cost),
			Status:           shared.TelemetryStatusOK,
			Payload: mergePayload(rawPayload, map[string]any{
				"event_type": "message.updated",
			}),
		}}, nil

	case "tool.execute.after":
		if len(ev.Payload) == 0 {
			if includeUnknown {
				return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryEventSchema, typ)}, nil
			}
			return nil, nil
		}
		var payload toolPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode tool.execute.after payload: %w", err)
		}
		toolCallID := strings.TrimSpace(payload.ToolCallID)
		if toolCallID == "" {
			if includeUnknown {
				return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryEventSchema, typ)}, nil
			}
			return nil, nil
		}
		toolName := strings.ToLower(shared.FirstNonEmpty(strings.TrimSpace(payload.ToolName), strings.TrimSpace(payload.Name), "unknown"))

		return []shared.TelemetryEvent{{
			SchemaVersion: telemetryEventSchema,
			Channel:       shared.TelemetryChannelHook,
			OccurredAt:    hookTimestampOrNow(payload.Timestamp),
			AccountID:     "zen",
			SessionID:     strings.TrimSpace(payload.SessionID),
			MessageID:     strings.TrimSpace(payload.MessageID),
			ToolCallID:    toolCallID,
			ProviderID:    "zen",
			AgentName:     "opencode",
			EventType:     shared.TelemetryEventTypeToolUsage,
			ToolName:      toolName,
			Requests:      shared.Int64Ptr(1),
			Status:        shared.TelemetryStatusOK,
			Payload: mergePayload(rawPayload, map[string]any{
				"event_type": "tool.execute.after",
			}),
		}}, nil
	}

	if includeUnknown {
		return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryEventSchema, typ)}, nil
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
	toolName := strings.ToLower(shared.FirstNonEmpty(strings.TrimSpace(input.Tool), "unknown"))

	return []shared.TelemetryEvent{{
		SchemaVersion: telemetryHookSchema,
		Channel:       shared.TelemetryChannelHook,
		OccurredAt:    parseHookTimestamp(root),
		AccountID:     "zen",
		SessionID:     strings.TrimSpace(input.SessionID),
		ToolCallID:    toolCallID,
		ProviderID:    "zen",
		AgentName:     "opencode",
		EventType:     shared.TelemetryEventTypeToolUsage,
		ToolName:      toolName,
		Requests:      shared.Int64Ptr(1),
		Status:        shared.TelemetryStatusOK,
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

	sessionID := shared.FirstNonEmpty(input.SessionID, output.Message.SessionID)
	turnID := shared.FirstNonEmpty(input.MessageID, output.Message.ID)
	messageID := shared.FirstNonEmpty(output.Message.ID, input.MessageID)
	outputProviderID := firstPathString(outputMap,
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
	outputModelID := firstPathString(outputMap,
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
	providerID := shared.FirstNonEmpty(outputProviderID, input.Model.ProviderID, "zen")
	modelRaw := strings.TrimSpace(outputModelID)
	if !hasUsage(u) {
		providerID = shared.FirstNonEmpty(outputProviderID, input.Model.ProviderID, "zen")
		modelRaw = shared.FirstNonEmpty(outputModelID, strings.TrimSpace(input.Model.ModelID))
	}
	contextSummary := extractContextSummary(outputMap)

	if turnID == "" && sessionID == "" {
		return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryHookSchema, "chat.message")}, nil
	}

	return []shared.TelemetryEvent{{
		SchemaVersion:    telemetryHookSchema,
		Channel:          shared.TelemetryChannelHook,
		OccurredAt:       parseHookTimestamp(root),
		AccountID:        "zen",
		SessionID:        sessionID,
		TurnID:           turnID,
		MessageID:        messageID,
		ProviderID:       providerID,
		AgentName:        "opencode",
		EventType:        shared.TelemetryEventTypeMessageUsage,
		ModelRaw:         modelRaw,
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		ReasoningTokens:  u.ReasoningTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		TotalTokens:      u.TotalTokens,
		CostUSD:          u.CostUSD,
		Requests:         shared.Int64Ptr(1),
		Status:           shared.TelemetryStatusOK,
		Payload: mergePayload(rawPayload, map[string]any{
			"hook":        "chat.message",
			"agent":       strings.TrimSpace(input.Agent),
			"variant":     strings.TrimSpace(input.Variant),
			"parts_count": output.PartsCount,
			"context":     contextSummary,
		}),
	}}, nil
}

func buildRawEnvelope(rawPayload map[string]any, schemaVersion, detectedType string) shared.TelemetryEvent {
	occurredAt := parseHookTimestampAny(rawPayload)
	providerID := shared.FirstNonEmpty(
		firstPathString(rawPayload,
			[]string{"provider_id"},
			[]string{"providerID"},
			[]string{"input", "model", "providerID"},
			[]string{"output", "message", "model", "providerID"},
			[]string{"output", "model", "providerID"},
			[]string{"model", "providerID"},
			[]string{"event", "properties", "info", "providerID"},
		),
		"zen",
	)
	sessionID := firstPathString(rawPayload,
		[]string{"session_id"},
		[]string{"sessionID"},
		[]string{"input", "sessionID"},
		[]string{"output", "message", "sessionID"},
		[]string{"event", "properties", "info", "sessionID"},
	)
	turnID := firstPathString(rawPayload,
		[]string{"turn_id"},
		[]string{"turnID"},
		[]string{"input", "messageID"},
		[]string{"output", "message", "id"},
		[]string{"event", "properties", "info", "parentID"},
	)
	messageID := firstPathString(rawPayload,
		[]string{"message_id"},
		[]string{"messageID"},
		[]string{"input", "messageID"},
		[]string{"output", "message", "id"},
		[]string{"event", "properties", "info", "id"},
	)
	toolCallID := firstPathString(rawPayload,
		[]string{"tool_call_id"},
		[]string{"toolCallID"},
		[]string{"input", "callID"},
		[]string{"event", "payload", "toolCallID"},
	)
	modelRaw := firstPathString(rawPayload,
		[]string{"model_id"},
		[]string{"modelID"},
		[]string{"input", "model", "modelID"},
		[]string{"output", "message", "model", "modelID"},
		[]string{"output", "model", "modelID"},
		[]string{"model", "modelID"},
		[]string{"event", "properties", "info", "modelID"},
	)
	workspace := shared.SanitizeWorkspace(firstPathString(rawPayload,
		[]string{"workspace_id"},
		[]string{"workspaceID"},
		[]string{"event", "properties", "info", "path", "cwd"},
	))
	eventName := shared.FirstNonEmpty(
		detectedType,
		firstPathString(rawPayload, []string{"hook"}),
		firstPathString(rawPayload, []string{"type"}),
		firstPathString(rawPayload, []string{"event"}),
	)

	return shared.TelemetryEvent{
		SchemaVersion: schemaVersion,
		Channel:       shared.TelemetryChannelHook,
		OccurredAt:    occurredAt,
		AccountID:     "zen",
		WorkspaceID:   workspace,
		SessionID:     sessionID,
		TurnID:        turnID,
		MessageID:     messageID,
		ToolCallID:    toolCallID,
		ProviderID:    providerID,
		AgentName:     "opencode",
		EventType:     shared.TelemetryEventTypeRawEnvelope,
		ModelRaw:      modelRaw,
		Status:        shared.TelemetryStatusUnknown,
		Payload: mergePayload(rawPayload, map[string]any{
			"captured_as":    "raw_envelope",
			"detected_event": eventName,
		}),
	}
}

func collectPartSummary(ctx context.Context, db *sql.DB) (map[string]partSummary, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT message_id, COALESCE(NULLIF(TRIM(json_extract(data, '$.type')), ''), 'unknown') AS part_type, COUNT(*)
		FROM part
		GROUP BY message_id, part_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]partSummary)
	for rows.Next() {
		var (
			messageID string
			partType  string
			count     int64
		)
		if err := rows.Scan(&messageID, &partType, &count); err != nil {
			continue
		}
		messageID = strings.TrimSpace(messageID)
		if messageID == "" {
			continue
		}
		partType = strings.TrimSpace(partType)
		if partType == "" {
			partType = "unknown"
		}
		s := out[messageID]
		if s.PartsByType == nil {
			s.PartsByType = map[string]int64{}
		}
		s.PartsTotal += count
		s.PartsByType[partType] += count
		out[messageID] = s
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func sqliteTableExists(ctx context.Context, db *sql.DB, table string) bool {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name=? LIMIT 1`, strings.TrimSpace(table)).Scan(&exists)
	return err == nil && exists == 1
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

func appendDedupTelemetryEvents(
	out *[]shared.TelemetryEvent,
	events []shared.TelemetryEvent,
	seenMessage map[string]bool,
	seenTools map[string]bool,
	accountID string,
) {
	for _, ev := range events {
		ev.AccountID = shared.FirstNonEmpty(ev.AccountID, accountID)
		switch ev.EventType {
		case shared.TelemetryEventTypeToolUsage:
			key := shared.FirstNonEmpty(strings.TrimSpace(ev.ToolCallID))
			if key == "" {
				key = shared.FirstNonEmpty(strings.TrimSpace(ev.SessionID), strings.TrimSpace(ev.MessageID)) + "|" + strings.ToLower(strings.TrimSpace(ev.ToolName))
			}
			if key != "" {
				if seenTools[key] {
					continue
				}
				seenTools[key] = true
			}
		case shared.TelemetryEventTypeMessageUsage:
			key := shared.FirstNonEmpty(strings.TrimSpace(ev.MessageID))
			if key == "" {
				key = shared.FirstNonEmpty(strings.TrimSpace(ev.SessionID), strings.TrimSpace(ev.TurnID))
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
	input := firstPathNumber(output,
		[]string{"usage", "input_tokens"}, []string{"usage", "inputTokens"}, []string{"usage", "input"},
		[]string{"message", "usage", "input_tokens"}, []string{"message", "usage", "inputTokens"}, []string{"message", "usage", "input"},
		[]string{"tokens", "input"}, []string{"input_tokens"}, []string{"inputTokens"},
	)
	outputTokens := firstPathNumber(output,
		[]string{"usage", "output_tokens"}, []string{"usage", "outputTokens"}, []string{"usage", "output"},
		[]string{"message", "usage", "output_tokens"}, []string{"message", "usage", "outputTokens"}, []string{"message", "usage", "output"},
		[]string{"tokens", "output"}, []string{"output_tokens"}, []string{"outputTokens"},
	)
	reasoning := firstPathNumber(output,
		[]string{"usage", "reasoning_tokens"}, []string{"usage", "reasoningTokens"}, []string{"usage", "reasoning"},
		[]string{"message", "usage", "reasoning_tokens"}, []string{"message", "usage", "reasoningTokens"}, []string{"message", "usage", "reasoning"},
		[]string{"tokens", "reasoning"}, []string{"reasoning_tokens"}, []string{"reasoningTokens"},
	)
	cacheRead := firstPathNumber(output,
		[]string{"usage", "cache_read_input_tokens"}, []string{"usage", "cacheReadInputTokens"}, []string{"usage", "cache_read_tokens"},
		[]string{"usage", "cacheReadTokens"}, []string{"usage", "cache", "read"},
		[]string{"message", "usage", "cache_read_input_tokens"}, []string{"message", "usage", "cacheReadInputTokens"}, []string{"message", "usage", "cache", "read"},
		[]string{"tokens", "cache", "read"},
	)
	cacheWrite := firstPathNumber(output,
		[]string{"usage", "cache_creation_input_tokens"}, []string{"usage", "cacheCreationInputTokens"}, []string{"usage", "cache_write_tokens"},
		[]string{"usage", "cacheWriteTokens"}, []string{"usage", "cache", "write"},
		[]string{"message", "usage", "cache_creation_input_tokens"}, []string{"message", "usage", "cacheCreationInputTokens"}, []string{"message", "usage", "cache", "write"},
		[]string{"tokens", "cache", "write"},
	)
	total := firstPathNumber(output,
		[]string{"usage", "total_tokens"}, []string{"usage", "totalTokens"}, []string{"usage", "total"},
		[]string{"message", "usage", "total_tokens"}, []string{"message", "usage", "totalTokens"}, []string{"message", "usage", "total"},
		[]string{"tokens", "total"}, []string{"total_tokens"}, []string{"totalTokens"},
	)
	cost := firstPathNumber(output,
		[]string{"usage", "cost_usd"}, []string{"usage", "costUSD"}, []string{"usage", "cost"},
		[]string{"message", "usage", "cost_usd"}, []string{"message", "usage", "costUSD"}, []string{"message", "usage", "cost"},
		[]string{"cost_usd"}, []string{"costUSD"}, []string{"cost"},
	)

	result := usage{
		InputTokens:      floatNumberToInt64Ptr(input),
		OutputTokens:     floatNumberToInt64Ptr(outputTokens),
		ReasoningTokens:  floatNumberToInt64Ptr(reasoning),
		CacheReadTokens:  floatNumberToInt64Ptr(cacheRead),
		CacheWriteTokens: floatNumberToInt64Ptr(cacheWrite),
		TotalTokens:      floatNumberToInt64Ptr(total),
		CostUSD:          floatNumberToFloat64Ptr(cost),
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
			result.TotalTokens = shared.Int64Ptr(combined)
		}
	}
	return result
}

func extractContextSummary(output map[string]any) map[string]any {
	if len(output) == 0 {
		return map[string]any{}
	}
	partsTotal := firstPathNumber(output, []string{"context", "parts_total"}, []string{"context", "partsTotal"}, []string{"parts_count"})
	partsByType := map[string]any{}
	if m, ok := pathMap(output, "context", "parts_by_type"); ok {
		for key, value := range m {
			if count, ok := numberFromAny(value); ok {
				partsByType[strings.TrimSpace(key)] = int64(count)
			}
		}
	}
	if len(partsByType) == 0 {
		if arr, ok := pathSlice(output, "parts"); ok {
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
		"parts_total":   ptrInt64Value(floatNumberToInt64Ptr(partsTotal)),
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
	out := make(map[string]any, len(rawPayload)+1)
	for key, value := range rawPayload {
		out[key] = value
	}
	if len(normalized) > 0 {
		out["_normalized"] = normalized
		for key, value := range normalized {
			if _, exists := out[key]; !exists {
				out[key] = value
			}
		}
	}
	return out
}

func firstPathNumber(root map[string]any, paths ...[]string) *float64 {
	for _, path := range paths {
		if value, ok := pathValue(root, path...); ok {
			if parsed, ok := numberFromAny(value); ok {
				return &parsed
			}
		}
	}
	return nil
}

func firstPathString(root map[string]any, paths ...[]string) string {
	for _, path := range paths {
		if value, ok := pathValue(root, path...); ok {
			switch v := value.(type) {
			case string:
				if s := strings.TrimSpace(v); s != "" {
					return s
				}
			case json.Number:
				if s := strings.TrimSpace(v.String()); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func pathValue(root map[string]any, path ...string) (any, bool) {
	var current any = root
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := m[key]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func pathMap(root map[string]any, path ...string) (map[string]any, bool) {
	value, ok := pathValue(root, path...)
	if !ok {
		return nil, false
	}
	m, ok := value.(map[string]any)
	return m, ok
}

func pathSlice(root map[string]any, path ...string) ([]any, bool) {
	value, ok := pathValue(root, path...)
	if !ok {
		return nil, false
	}
	arr, ok := value.([]any)
	return arr, ok
}

func numberFromAny(value any) (float64, bool) {
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
		f, err := v.Float64()
		if err == nil {
			return f, true
		}
	case string:
		parsed, err := json.Number(strings.TrimSpace(v)).Float64()
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func floatNumberToInt64Ptr(v *float64) *int64 {
	if v == nil {
		return nil
	}
	return shared.Int64Ptr(int64(*v))
}

func floatNumberToFloat64Ptr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return shared.Float64Ptr(*v)
}

func ptrInt64Value(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func ptrFloat64Value(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func parseHookTimestampAny(root map[string]any) time.Time {
	if root == nil {
		return time.Now().UTC()
	}
	if ts := firstPathNumber(root,
		[]string{"timestamp"},
		[]string{"time"},
		[]string{"event", "timestamp"},
		[]string{"event", "properties", "info", "time", "completed"},
		[]string{"event", "properties", "info", "time", "created"},
	); ts != nil && *ts > 0 {
		return hookTimestampOrNow(int64(*ts))
	}
	if raw := firstPathString(root, []string{"timestamp"}, []string{"time"}, []string{"event", "timestamp"}); raw != "" {
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
