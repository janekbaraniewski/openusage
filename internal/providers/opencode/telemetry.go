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

	"github.com/janekbaraniewski/openusage/internal/core"
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

type partSummary struct {
	PartsTotal  int64
	PartsByType map[string]int64
}

func (p *Provider) System() string { return p.ID() }

func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	home, _ := os.UserHomeDir()
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			"db_path": filepath.Join(home, ".local", "share", "opencode", "opencode.db"),
		},
		PathLists: map[string][]string{
			"events_dirs": {
				filepath.Join(home, ".opencode", "events"),
				filepath.Join(home, ".opencode", "logs"),
				filepath.Join(home, ".local", "state", "opencode", "events"),
				filepath.Join(home, ".local", "state", "opencode", "logs"),
			},
		},
	}
}

func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	defaults := p.DefaultCollectOptions()
	dbPath := shared.ExpandHome(opts.Path("db_path", defaults.Paths["db_path"]))
	eventsDirs := opts.PathsFor("events_dirs", defaults.PathLists["events_dirs"])
	eventsFile := opts.Path("events_file", "")
	accountID := strings.TrimSpace(opts.Path("account_id", ""))

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

func (p *Provider) ParseHookPayload(raw []byte, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	events, err := ParseTelemetryHookPayload(raw)
	if err != nil {
		return nil, err
	}
	accountID := strings.TrimSpace(opts.Path("account_id", ""))
	for i := range events {
		events[i].AccountID = core.FirstNonEmpty(accountID, events[i].AccountID)
	}
	return events, nil
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
				providerID = "opencode"
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetryEventSchema,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurred,
				AccountID:     "",
				WorkspaceID:   shared.SanitizeWorkspace(info.Path.CWD),
				SessionID:     strings.TrimSpace(info.SessionID),
				TurnID:        strings.TrimSpace(info.ParentID),
				MessageID:     messageID,
				ProviderID:    providerID,
				AgentName:     "opencode",
				EventType:     shared.TelemetryEventTypeMessageUsage,
				ModelRaw:      strings.TrimSpace(info.ModelID),
				TokenUsage: core.TokenUsage{
					InputTokens:      core.Int64Ptr(info.Tokens.Input),
					OutputTokens:     core.Int64Ptr(info.Tokens.Output),
					ReasoningTokens:  core.Int64Ptr(info.Tokens.Reasoning),
					CacheReadTokens:  core.Int64Ptr(info.Tokens.Cache.Read),
					CacheWriteTokens: core.Int64Ptr(info.Tokens.Cache.Write),
					TotalTokens:      core.Int64Ptr(total),
					CostUSD:          core.Float64Ptr(info.Cost),
				},
				Status: shared.TelemetryStatusOK,
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

			// Extract tool's target file path from raw payload for language inference.
			toolFilePath := ""
			var rawPayloadMap map[string]any
			if json.Unmarshal(ev.Payload, &rawPayloadMap) == nil {
				if paths := shared.ExtractFilePathsFromPayload(rawPayloadMap); len(paths) > 0 {
					toolFilePath = paths[0]
				}
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetryEventSchema,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurred,
				AccountID:     "",
				SessionID:     strings.TrimSpace(tool.SessionID),
				MessageID:     strings.TrimSpace(tool.MessageID),
				ToolCallID:    toolID,
				ProviderID:    "opencode",
				AgentName:     "opencode",
				EventType:     shared.TelemetryEventTypeToolUsage,
				TokenUsage: core.TokenUsage{
					Requests: core.Int64Ptr(1),
				},
				ToolName: strings.ToLower(name),
				Status:   shared.TelemetryStatusOK,
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
	hasPartTable := sqliteTableExists(ctx, db, "part")
	if hasPartTable {
		partSummaryByMessage, _ = collectPartSummary(ctx, db)
	}

	var out []shared.TelemetryEvent
	seenMessages := map[string]bool{}

	if hasPartTable {
		stepRows, err := db.QueryContext(ctx, `
			SELECT p.id, p.message_id, p.session_id, p.time_created, p.time_updated, p.data, COALESCE(m.data, '{}'), COALESCE(s.directory, '')
			FROM part p
			LEFT JOIN message m ON m.id = p.message_id
			LEFT JOIN session s ON s.id = p.session_id
			WHERE COALESCE(json_extract(p.data, '$.type'), '') = 'step-finish'
			ORDER BY p.time_updated ASC
		`)
		if err == nil {
			for stepRows.Next() {
				if ctx.Err() != nil {
					_ = stepRows.Close()
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
				if err := stepRows.Scan(&partID, &messageIDDB, &sessionIDDB, &timeCreated, &timeUpdated, &partJSON, &messageJSON, &sessionDir); err != nil {
					continue
				}

				partPayload := decodeJSONMap([]byte(partJSON))
				messagePayload := decodeJSONMap([]byte(messageJSON))

				u := extractUsage(partPayload)
				if !hasUsage(u) {
					continue
				}

				messageID := core.FirstNonEmpty(strings.TrimSpace(messageIDDB), shared.FirstPathString(messagePayload, []string{"id"}), shared.FirstPathString(messagePayload, []string{"messageID"}))
				if messageID == "" || seenMessages[messageID] {
					continue
				}

				sessionID := core.FirstNonEmpty(strings.TrimSpace(sessionIDDB), shared.FirstPathString(messagePayload, []string{"sessionID"}))
				turnID := core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"parentID"}), shared.FirstPathString(messagePayload, []string{"turnID"}))
				providerID := core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"providerID"}), shared.FirstPathString(messagePayload, []string{"model", "providerID"}), "opencode")
				modelRaw := core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"modelID"}), shared.FirstPathString(messagePayload, []string{"model", "modelID"}))
				upstreamProvider := extractUpstreamProviderFromMaps(partPayload, messagePayload)

				occurredAt := shared.UnixAuto(timeUpdated)
				if timeCreated > 0 {
					occurredAt = shared.UnixAuto(timeCreated)
				}

				eventStatus := mapMessageStatus(shared.FirstPathString(partPayload, []string{"reason"}))

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
					SchemaVersion: telemetrySQLiteSchema,
					Channel:       shared.TelemetryChannelSQLite,
					OccurredAt:    occurredAt,
					AccountID:     "",
					WorkspaceID:   shared.SanitizeWorkspace(core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"path", "cwd"}), shared.FirstPathString(messagePayload, []string{"path", "root"}), strings.TrimSpace(sessionDir))),
					SessionID:     sessionID,
					TurnID:        turnID,
					MessageID:     messageID,
					ProviderID:    providerID,
					AgentName:     core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"agent"}), "opencode"),
					EventType:     shared.TelemetryEventTypeMessageUsage,
					ModelRaw:      modelRaw,
					TokenUsage: core.TokenUsage{
						InputTokens:      u.InputTokens,
						OutputTokens:     u.OutputTokens,
						ReasoningTokens:  u.ReasoningTokens,
						CacheReadTokens:  u.CacheReadTokens,
						CacheWriteTokens: u.CacheWriteTokens,
						TotalTokens:      u.TotalTokens,
						CostUSD:          u.CostUSD,
						Requests:         core.Int64Ptr(1),
					},
					Status: eventStatus,
					Payload: map[string]any{
						"source": map[string]any{
							"db_path": dbPath,
							"table":   "part",
							"type":    "step-finish",
						},
						"db": map[string]any{
							"part_id":      strings.TrimSpace(partID),
							"message_id":   strings.TrimSpace(messageIDDB),
							"session_id":   strings.TrimSpace(sessionIDDB),
							"time_created": timeCreated,
							"time_updated": timeUpdated,
						},
						"message": map[string]any{
							"provider_id": providerID,
							"model_id":    modelRaw,
							"mode":        shared.FirstPathString(messagePayload, []string{"mode"}),
							"finish":      shared.FirstPathString(messagePayload, []string{"finish"}),
						},
						"step": map[string]any{
							"type":   shared.FirstPathString(partPayload, []string{"type"}),
							"reason": shared.FirstPathString(partPayload, []string{"reason"}),
						},
						"upstream_provider": upstreamProvider,
						"context":           contextSummary,
					},
				})
				seenMessages[messageID] = true
			}
			_ = stepRows.Close()
		}
	}

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
			if strings.ToLower(shared.FirstPathString(payload, []string{"role"})) != "assistant" {
				continue
			}

			u := extractUsage(payload)
			completedAt := ptrInt64FromFloat(shared.FirstPathNumber(payload, []string{"time", "completed"}))
			createdAt := ptrInt64FromFloat(shared.FirstPathNumber(payload, []string{"time", "created"}))
			if !hasUsage(u) && completedAt <= 0 {
				continue
			}

			messageID := core.FirstNonEmpty(strings.TrimSpace(messageIDRaw), shared.FirstPathString(payload, []string{"id"}), shared.FirstPathString(payload, []string{"messageID"}))
			if messageID == "" || seenMessages[messageID] {
				continue
			}

			if !hasUsage(u) {
				continue
			}

			providerID := core.FirstNonEmpty(shared.FirstPathString(payload, []string{"providerID"}), shared.FirstPathString(payload, []string{"model", "providerID"}), "opencode")
			modelRaw := core.FirstNonEmpty(shared.FirstPathString(payload, []string{"modelID"}), shared.FirstPathString(payload, []string{"model", "modelID"}))
			upstreamProvider := extractUpstreamProviderFromMaps(payload)
			sessionID := core.FirstNonEmpty(strings.TrimSpace(sessionIDRaw), shared.FirstPathString(payload, []string{"sessionID"}))
			turnID := core.FirstNonEmpty(shared.FirstPathString(payload, []string{"parentID"}), shared.FirstPathString(payload, []string{"turnID"}))

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
			finish := strings.ToLower(shared.FirstPathString(payload, []string{"finish"}))
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
				SchemaVersion: telemetrySQLiteSchema,
				Channel:       shared.TelemetryChannelSQLite,
				OccurredAt:    occurredAt,
				AccountID:     "",
				WorkspaceID:   shared.SanitizeWorkspace(core.FirstNonEmpty(shared.FirstPathString(payload, []string{"path", "cwd"}), shared.FirstPathString(payload, []string{"path", "root"}), strings.TrimSpace(sessionDir))),
				SessionID:     sessionID,
				TurnID:        turnID,
				MessageID:     messageID,
				ProviderID:    providerID,
				AgentName:     core.FirstNonEmpty(shared.FirstPathString(payload, []string{"agent"}), "opencode"),
				EventType:     shared.TelemetryEventTypeMessageUsage,
				ModelRaw:      modelRaw,
				TokenUsage: core.TokenUsage{
					InputTokens:      u.InputTokens,
					OutputTokens:     u.OutputTokens,
					ReasoningTokens:  u.ReasoningTokens,
					CacheReadTokens:  u.CacheReadTokens,
					CacheWriteTokens: u.CacheWriteTokens,
					TotalTokens:      u.TotalTokens,
					CostUSD:          u.CostUSD,
					Requests:         core.Int64Ptr(1),
				},
				Status: eventStatus,
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
					"message": map[string]any{
						"provider_id": providerID,
						"model_id":    modelRaw,
						"role":        shared.FirstPathString(payload, []string{"role"}),
						"mode":        shared.FirstPathString(payload, []string{"mode"}),
						"finish":      shared.FirstPathString(payload, []string{"finish"}),
						"error_name":  shared.FirstPathString(payload, []string{"error", "name"}),
					},
					"upstream_provider": upstreamProvider,
					"context":           contextSummary,
				},
			})
			seenMessages[messageID] = true
		}
		_ = messageRows.Close()
	}

	if !hasPartTable {
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

		toolCallID := core.FirstNonEmpty(shared.FirstPathString(partPayload, []string{"callID"}), shared.FirstPathString(partPayload, []string{"call_id"}), strings.TrimSpace(partID))
		if toolCallID == "" || seenTools[toolCallID] {
			continue
		}

		statusRaw := strings.ToLower(shared.FirstPathString(partPayload, []string{"state", "status"}))
		status, include := mapToolStatus(statusRaw)
		if !include {
			continue
		}
		seenTools[toolCallID] = true

		toolName := strings.ToLower(core.FirstNonEmpty(shared.FirstPathString(partPayload, []string{"tool"}), shared.FirstPathString(partPayload, []string{"name"}), "unknown"))
		sessionID := core.FirstNonEmpty(strings.TrimSpace(sessionIDDB), shared.FirstPathString(partPayload, []string{"sessionID"}), shared.FirstPathString(messagePayload, []string{"sessionID"}))
		messageID := core.FirstNonEmpty(strings.TrimSpace(messageIDDB), shared.FirstPathString(partPayload, []string{"messageID"}), shared.FirstPathString(messagePayload, []string{"id"}))
		providerID := core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"providerID"}), shared.FirstPathString(messagePayload, []string{"model", "providerID"}), "opencode")
		modelRaw := core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"modelID"}), shared.FirstPathString(messagePayload, []string{"model", "modelID"}))
		upstreamProvider := extractUpstreamProviderFromMaps(partPayload, messagePayload)

		occurredAt := shared.UnixAuto(timeUpdated)
		if ts := ptrInt64FromFloat(shared.FirstPathNumber(partPayload,
			[]string{"state", "time", "end"},
			[]string{"state", "time", "start"},
			[]string{"time", "end"},
			[]string{"time", "start"},
		)); ts > 0 {
			occurredAt = shared.UnixAuto(ts)
		} else if timeCreated > 0 {
			occurredAt = shared.UnixAuto(timeCreated)
		}

		// Extract tool's target file path from part payload for language inference.
		toolFilePath := ""
		if stateInput, ok := partPayload["state"].(map[string]any); ok {
			if paths := shared.ExtractFilePathsFromPayload(stateInput); len(paths) > 0 {
				toolFilePath = paths[0]
			}
		}
		if toolFilePath == "" {
			if paths := shared.ExtractFilePathsFromPayload(partPayload); len(paths) > 0 {
				toolFilePath = paths[0]
			}
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetrySQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    occurredAt,
			AccountID:     "",
			WorkspaceID: shared.SanitizeWorkspace(core.FirstNonEmpty(
				shared.FirstPathString(messagePayload, []string{"path", "cwd"}),
				shared.FirstPathString(messagePayload, []string{"path", "root"}),
				strings.TrimSpace(sessionDir),
			)),
			SessionID:  sessionID,
			MessageID:  messageID,
			ToolCallID: toolCallID,
			ProviderID: providerID,
			AgentName:  core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"agent"}), "opencode"),
			EventType:  shared.TelemetryEventTypeToolUsage,
			ModelRaw:   modelRaw,
			ToolName:   toolName,
			TokenUsage: core.TokenUsage{
				Requests: core.Int64Ptr(1),
			},
			Status: status,
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
				"message": map[string]any{
					"provider_id": providerID,
					"model_id":    modelRaw,
					"mode":        shared.FirstPathString(messagePayload, []string{"mode"}),
				},
				"upstream_provider": upstreamProvider,
				"status":            statusRaw,
				"file":              toolFilePath,
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
		providerID := core.FirstNonEmpty(strings.TrimSpace(info.ProviderID), "opencode")
		occurredAt := shared.UnixAuto(info.Time.Created)
		if info.Time.Completed > 0 {
			occurredAt = shared.UnixAuto(info.Time.Completed)
		}
		totalTokens := info.Tokens.Input + info.Tokens.Output + info.Tokens.Reasoning + info.Tokens.Cache.Read + info.Tokens.Cache.Write

		return []shared.TelemetryEvent{{
			SchemaVersion: telemetryEventSchema,
			Channel:       shared.TelemetryChannelHook,
			OccurredAt:    occurredAt,
			AccountID:     "",
			WorkspaceID:   shared.SanitizeWorkspace(info.Path.CWD),
			SessionID:     strings.TrimSpace(info.SessionID),
			TurnID:        strings.TrimSpace(info.ParentID),
			MessageID:     messageID,
			ProviderID:    providerID,
			AgentName:     "opencode",
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      strings.TrimSpace(info.ModelID),
			TokenUsage: core.TokenUsage{
				InputTokens:      core.Int64Ptr(info.Tokens.Input),
				OutputTokens:     core.Int64Ptr(info.Tokens.Output),
				ReasoningTokens:  core.Int64Ptr(info.Tokens.Reasoning),
				CacheReadTokens:  core.Int64Ptr(info.Tokens.Cache.Read),
				CacheWriteTokens: core.Int64Ptr(info.Tokens.Cache.Write),
				TotalTokens:      core.Int64Ptr(totalTokens),
				CostUSD:          core.Float64Ptr(info.Cost),
			},
			Status: shared.TelemetryStatusOK,
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
		toolName := strings.ToLower(core.FirstNonEmpty(strings.TrimSpace(payload.ToolName), strings.TrimSpace(payload.Name), "unknown"))

		return []shared.TelemetryEvent{{
			SchemaVersion: telemetryEventSchema,
			Channel:       shared.TelemetryChannelHook,
			OccurredAt:    hookTimestampOrNow(payload.Timestamp),
			AccountID:     "",
			SessionID:     strings.TrimSpace(payload.SessionID),
			MessageID:     strings.TrimSpace(payload.MessageID),
			ToolCallID:    toolCallID,
			ProviderID:    "opencode",
			AgentName:     "opencode",
			EventType:     shared.TelemetryEventTypeToolUsage,
			ToolName:      toolName,
			TokenUsage: core.TokenUsage{
				Requests: core.Int64Ptr(1),
			},
			Status: shared.TelemetryStatusOK,
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
	toolName := strings.ToLower(core.FirstNonEmpty(strings.TrimSpace(input.Tool), "unknown"))

	return []shared.TelemetryEvent{{
		SchemaVersion: telemetryHookSchema,
		Channel:       shared.TelemetryChannelHook,
		OccurredAt:    parseHookTimestamp(root),
		AccountID:     "",
		SessionID:     strings.TrimSpace(input.SessionID),
		ToolCallID:    toolCallID,
		ProviderID:    "opencode",
		AgentName:     "opencode",
		EventType:     shared.TelemetryEventTypeToolUsage,
		ToolName:      toolName,
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(1),
		},
		Status: shared.TelemetryStatusOK,
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

	sessionID := core.FirstNonEmpty(input.SessionID, output.Message.SessionID)
	turnID := core.FirstNonEmpty(input.MessageID, output.Message.ID)
	messageID := core.FirstNonEmpty(output.Message.ID, input.MessageID)
	outputProviderID := shared.FirstPathString(outputMap,
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
	outputModelID := shared.FirstPathString(outputMap,
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
	providerID := core.FirstNonEmpty(outputProviderID, input.Model.ProviderID, "opencode")
	modelRaw := strings.TrimSpace(outputModelID)
	if !hasUsage(u) {
		providerID = core.FirstNonEmpty(outputProviderID, input.Model.ProviderID, "opencode")
		modelRaw = core.FirstNonEmpty(outputModelID, strings.TrimSpace(input.Model.ModelID))
	}
	upstreamProvider := sanitizeUpstreamProviderCandidate(core.FirstNonEmpty(
		shared.FirstPathString(outputMap,
			[]string{"upstream_provider"},
			[]string{"upstreamProvider"},
			[]string{"route", "provider_name"},
			[]string{"route", "providerName"},
			[]string{"route", "provider"},
			[]string{"router", "provider_name"},
			[]string{"router", "providerName"},
			[]string{"router", "provider"},
			[]string{"routing", "provider_name"},
			[]string{"routing", "providerName"},
			[]string{"routing", "provider"},
			[]string{"endpoint", "provider_name"},
			[]string{"endpoint", "providerName"},
			[]string{"endpoint", "provider"},
			[]string{"provider_name"},
			[]string{"providerName"},
			[]string{"provider"},
			[]string{"message", "provider_name"},
			[]string{"message", "providerName"},
			[]string{"message", "provider"},
			[]string{"message", "info", "provider_name"},
			[]string{"message", "info", "providerName"},
			[]string{"message", "info", "provider"},
		),
	))
	if upstreamProvider == "" {
		modelProviderHint := sanitizeUpstreamProviderCandidate(core.FirstNonEmpty(
			shared.FirstPathString(outputMap,
				[]string{"message", "model", "provider"},
				[]string{"message", "model", "provider_name"},
				[]string{"message", "model", "providerName"},
				[]string{"model", "provider"},
				[]string{"model", "provider_name"},
				[]string{"model", "providerName"},
			),
			outputProviderID,
		))
		if modelProviderHint != "" {
			upstreamProvider = modelProviderHint
		}
	}
	contextSummary := extractContextSummary(outputMap)

	if turnID == "" && sessionID == "" {
		return []shared.TelemetryEvent{buildRawEnvelope(rawPayload, telemetryHookSchema, "chat.message")}, nil
	}

	normalized := map[string]any{
		"hook":        "chat.message",
		"agent":       strings.TrimSpace(input.Agent),
		"variant":     strings.TrimSpace(input.Variant),
		"parts_count": output.PartsCount,
		"context":     contextSummary,
	}
	if upstreamProvider != "" {
		normalized["upstream_provider"] = upstreamProvider
	}

	return []shared.TelemetryEvent{{
		SchemaVersion: telemetryHookSchema,
		Channel:       shared.TelemetryChannelHook,
		OccurredAt:    parseHookTimestamp(root),
		AccountID:     "",
		SessionID:     sessionID,
		TurnID:        turnID,
		MessageID:     messageID,
		ProviderID:    providerID,
		AgentName:     "opencode",
		EventType:     shared.TelemetryEventTypeMessageUsage,
		ModelRaw:      modelRaw,
		TokenUsage: core.TokenUsage{
			InputTokens:      u.InputTokens,
			OutputTokens:     u.OutputTokens,
			ReasoningTokens:  u.ReasoningTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
			TotalTokens:      u.TotalTokens,
			CostUSD:          u.CostUSD,
			Requests:         core.Int64Ptr(1),
		},
		Status:  shared.TelemetryStatusOK,
		Payload: mergePayload(rawPayload, normalized),
	}}, nil
}

func sanitizeUpstreamProviderCandidate(value string) string {
	name := strings.TrimSpace(value)
	if name == "" {
		return ""
	}
	clean := strings.ToLower(name)
	switch clean {
	case "openrouter", "openusage", "opencode", "unknown":
		return ""
	}
	return clean
}

func extractUpstreamProviderFromMaps(payloads ...map[string]any) string {
	for _, payload := range payloads {
		if len(payload) == 0 {
			continue
		}
		candidate := sanitizeUpstreamProviderCandidate(core.FirstNonEmpty(
			shared.FirstPathString(payload,
				[]string{"upstream_provider"},
				[]string{"upstreamProvider"},
				[]string{"route", "provider_name"},
				[]string{"route", "providerName"},
				[]string{"route", "provider"},
				[]string{"router", "provider_name"},
				[]string{"router", "providerName"},
				[]string{"router", "provider"},
				[]string{"routing", "provider_name"},
				[]string{"routing", "providerName"},
				[]string{"routing", "provider"},
				[]string{"endpoint", "provider_name"},
				[]string{"endpoint", "providerName"},
				[]string{"endpoint", "provider"},
				[]string{"provider_name"},
				[]string{"providerName"},
				[]string{"provider"},
				[]string{"message", "provider_name"},
				[]string{"message", "providerName"},
				[]string{"message", "provider"},
				[]string{"message", "info", "provider_name"},
				[]string{"message", "info", "providerName"},
				[]string{"message", "info", "provider"},
			),
			shared.FirstPathString(payload,
				[]string{"message", "model", "provider"},
				[]string{"message", "model", "provider_name"},
				[]string{"message", "model", "providerName"},
				[]string{"model", "provider"},
				[]string{"model", "provider_name"},
				[]string{"model", "providerName"},
				[]string{"model", "providerID"},
			),
		))
		if candidate != "" {
			return candidate
		}

		rawResponseBody := core.FirstNonEmpty(
			shared.FirstPathString(payload, []string{"error", "data", "responseBody"}),
			shared.FirstPathString(payload, []string{"error", "responseBody"}),
		)
		if rawResponseBody == "" {
			continue
		}
		responseBodyPayload := decodeJSONMap([]byte(rawResponseBody))
		candidate = sanitizeUpstreamProviderCandidate(core.FirstNonEmpty(
			shared.FirstPathString(responseBodyPayload,
				[]string{"error", "metadata", "provider_name"},
				[]string{"error", "metadata", "providerName"},
				[]string{"metadata", "provider_name"},
				[]string{"metadata", "providerName"},
				[]string{"metadata", "provider"},
				[]string{"provider_name"},
				[]string{"providerName"},
				[]string{"provider"},
			),
		))
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func buildRawEnvelope(rawPayload map[string]any, schemaVersion, detectedType string) shared.TelemetryEvent {
	occurredAt := parseHookTimestampAny(rawPayload)
	providerID := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload,
			[]string{"provider_id"},
			[]string{"providerID"},
			[]string{"input", "model", "providerID"},
			[]string{"output", "message", "model", "providerID"},
			[]string{"output", "model", "providerID"},
			[]string{"model", "providerID"},
			[]string{"event", "properties", "info", "providerID"},
		),
		"opencode",
	)
	sessionID := shared.FirstPathString(rawPayload,
		[]string{"session_id"},
		[]string{"sessionID"},
		[]string{"input", "sessionID"},
		[]string{"output", "message", "sessionID"},
		[]string{"event", "properties", "info", "sessionID"},
	)
	turnID := shared.FirstPathString(rawPayload,
		[]string{"turn_id"},
		[]string{"turnID"},
		[]string{"input", "messageID"},
		[]string{"output", "message", "id"},
		[]string{"event", "properties", "info", "parentID"},
	)
	messageID := shared.FirstPathString(rawPayload,
		[]string{"message_id"},
		[]string{"messageID"},
		[]string{"input", "messageID"},
		[]string{"output", "message", "id"},
		[]string{"event", "properties", "info", "id"},
	)
	toolCallID := shared.FirstPathString(rawPayload,
		[]string{"tool_call_id"},
		[]string{"toolCallID"},
		[]string{"input", "callID"},
		[]string{"event", "payload", "toolCallID"},
	)
	modelRaw := shared.FirstPathString(rawPayload,
		[]string{"model_id"},
		[]string{"modelID"},
		[]string{"input", "model", "modelID"},
		[]string{"output", "message", "model", "modelID"},
		[]string{"output", "model", "modelID"},
		[]string{"model", "modelID"},
		[]string{"event", "properties", "info", "modelID"},
	)
	workspace := shared.SanitizeWorkspace(shared.FirstPathString(rawPayload,
		[]string{"workspace_id"},
		[]string{"workspaceID"},
		[]string{"event", "properties", "info", "path", "cwd"},
	))
	eventName := core.FirstNonEmpty(
		detectedType,
		shared.FirstPathString(rawPayload, []string{"hook"}),
		shared.FirstPathString(rawPayload, []string{"type"}),
		shared.FirstPathString(rawPayload, []string{"event"}),
	)

	return shared.TelemetryEvent{
		SchemaVersion: schemaVersion,
		Channel:       shared.TelemetryChannelHook,
		OccurredAt:    occurredAt,
		AccountID:     "",
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

func mapMessageStatus(reason string) shared.TelemetryStatus {
	reason = strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(reason, "error"), strings.Contains(reason, "fail"):
		return shared.TelemetryStatusError
	case strings.Contains(reason, "abort"), strings.Contains(reason, "cancel"):
		return shared.TelemetryStatusAborted
	default:
		return shared.TelemetryStatusOK
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
		ev.AccountID = core.FirstNonEmpty(accountID, ev.AccountID)
		switch ev.EventType {
		case shared.TelemetryEventTypeToolUsage:
			key := core.FirstNonEmpty(strings.TrimSpace(ev.ToolCallID))
			if key == "" {
				key = core.FirstNonEmpty(strings.TrimSpace(ev.SessionID), strings.TrimSpace(ev.MessageID)) + "|" + strings.ToLower(strings.TrimSpace(ev.ToolName))
			}
			if key != "" {
				if seenTools[key] {
					continue
				}
				seenTools[key] = true
			}
		case shared.TelemetryEventTypeMessageUsage:
			key := core.FirstNonEmpty(strings.TrimSpace(ev.MessageID))
			if key == "" {
				key = core.FirstNonEmpty(strings.TrimSpace(ev.SessionID), strings.TrimSpace(ev.TurnID))
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
	input := shared.FirstPathNumber(output,
		[]string{"usage", "input_tokens"}, []string{"usage", "inputTokens"}, []string{"usage", "input"},
		[]string{"message", "usage", "input_tokens"}, []string{"message", "usage", "inputTokens"}, []string{"message", "usage", "input"},
		[]string{"tokens", "input"}, []string{"input_tokens"}, []string{"inputTokens"},
	)
	outputTokens := shared.FirstPathNumber(output,
		[]string{"usage", "output_tokens"}, []string{"usage", "outputTokens"}, []string{"usage", "output"},
		[]string{"message", "usage", "output_tokens"}, []string{"message", "usage", "outputTokens"}, []string{"message", "usage", "output"},
		[]string{"tokens", "output"}, []string{"output_tokens"}, []string{"outputTokens"},
	)
	reasoning := shared.FirstPathNumber(output,
		[]string{"usage", "reasoning_tokens"}, []string{"usage", "reasoningTokens"}, []string{"usage", "reasoning"},
		[]string{"message", "usage", "reasoning_tokens"}, []string{"message", "usage", "reasoningTokens"}, []string{"message", "usage", "reasoning"},
		[]string{"tokens", "reasoning"}, []string{"reasoning_tokens"}, []string{"reasoningTokens"},
	)
	cacheRead := shared.FirstPathNumber(output,
		[]string{"usage", "cache_read_input_tokens"}, []string{"usage", "cacheReadInputTokens"}, []string{"usage", "cache_read_tokens"},
		[]string{"usage", "cacheReadTokens"}, []string{"usage", "cache", "read"},
		[]string{"message", "usage", "cache_read_input_tokens"}, []string{"message", "usage", "cacheReadInputTokens"}, []string{"message", "usage", "cache", "read"},
		[]string{"tokens", "cache", "read"},
	)
	cacheWrite := shared.FirstPathNumber(output,
		[]string{"usage", "cache_creation_input_tokens"}, []string{"usage", "cacheCreationInputTokens"}, []string{"usage", "cache_write_tokens"},
		[]string{"usage", "cacheWriteTokens"}, []string{"usage", "cache", "write"},
		[]string{"message", "usage", "cache_creation_input_tokens"}, []string{"message", "usage", "cacheCreationInputTokens"}, []string{"message", "usage", "cache", "write"},
		[]string{"tokens", "cache", "write"},
	)
	total := shared.FirstPathNumber(output,
		[]string{"usage", "total_tokens"}, []string{"usage", "totalTokens"}, []string{"usage", "total"},
		[]string{"message", "usage", "total_tokens"}, []string{"message", "usage", "totalTokens"}, []string{"message", "usage", "total"},
		[]string{"tokens", "total"}, []string{"total_tokens"}, []string{"totalTokens"},
	)
	cost := shared.FirstPathNumber(output,
		[]string{"usage", "cost_usd"}, []string{"usage", "costUSD"}, []string{"usage", "cost"},
		[]string{"message", "usage", "cost_usd"}, []string{"message", "usage", "costUSD"}, []string{"message", "usage", "cost"},
		[]string{"cost_usd"}, []string{"costUSD"}, []string{"cost"},
	)

	result := usage{
		InputTokens:      shared.NumberToInt64Ptr(input),
		OutputTokens:     shared.NumberToInt64Ptr(outputTokens),
		ReasoningTokens:  shared.NumberToInt64Ptr(reasoning),
		CacheReadTokens:  shared.NumberToInt64Ptr(cacheRead),
		CacheWriteTokens: shared.NumberToInt64Ptr(cacheWrite),
		TotalTokens:      shared.NumberToInt64Ptr(total),
		CostUSD:          shared.NumberToFloat64Ptr(cost),
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
			result.TotalTokens = core.Int64Ptr(combined)
		}
	}
	return result
}

func extractContextSummary(output map[string]any) map[string]any {
	if len(output) == 0 {
		return map[string]any{}
	}
	partsTotal := shared.FirstPathNumber(output, []string{"context", "parts_total"}, []string{"context", "partsTotal"}, []string{"parts_count"})
	partsByType := map[string]any{}
	if m, ok := shared.PathMap(output, "context", "parts_by_type"); ok {
		for key, value := range m {
			if count, ok := shared.NumberFromAny(value); ok {
				partsByType[strings.TrimSpace(key)] = int64(count)
			}
		}
	}
	if len(partsByType) == 0 {
		if arr, ok := shared.PathSlice(output, "parts"); ok {
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
		"parts_total":   ptrInt64Value(shared.NumberToInt64Ptr(partsTotal)),
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
	out := make(map[string]any, 8)
	if len(normalized) > 0 {
		out["_normalized"] = normalized
		for key, value := range normalized {
			if _, exists := out[key]; !exists {
				out[key] = value
			}
		}
	}
	rawSummary := summarizeRawPayload(rawPayload)
	if len(rawSummary) > 0 {
		out["_raw"] = rawSummary
		for key, value := range rawSummary {
			if _, exists := out[key]; !exists {
				out[key] = value
			}
		}
	}
	return out
}

func summarizeRawPayload(rawPayload map[string]any) map[string]any {
	if len(rawPayload) == 0 {
		return map[string]any{}
	}
	out := map[string]any{
		"raw_keys": len(rawPayload),
	}

	if hook := shared.FirstPathString(rawPayload, []string{"hook"}); hook != "" {
		out["hook"] = hook
	}
	if typ := shared.FirstPathString(rawPayload, []string{"type"}); typ != "" {
		out["type"] = typ
	}

	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"hook"}),
		shared.FirstPathString(rawPayload, []string{"event"}),
		shared.FirstPathString(rawPayload, []string{"type"}),
	); value != "" {
		out["event"] = value
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"sessionID"}),
		shared.FirstPathString(rawPayload, []string{"session_id"}),
		shared.FirstPathString(rawPayload, []string{"input", "sessionID"}),
		shared.FirstPathString(rawPayload, []string{"output", "message", "sessionID"}),
	); value != "" {
		out["session_id"] = value
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"messageID"}),
		shared.FirstPathString(rawPayload, []string{"message_id"}),
		shared.FirstPathString(rawPayload, []string{"input", "messageID"}),
		shared.FirstPathString(rawPayload, []string{"output", "message", "id"}),
	); value != "" {
		out["message_id"] = value
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"toolCallID"}),
		shared.FirstPathString(rawPayload, []string{"tool_call_id"}),
		shared.FirstPathString(rawPayload, []string{"input", "callID"}),
	); value != "" {
		out["tool_call_id"] = value
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"providerID"}),
		shared.FirstPathString(rawPayload, []string{"provider_id"}),
		shared.FirstPathString(rawPayload, []string{"input", "model", "providerID"}),
		shared.FirstPathString(rawPayload, []string{"output", "message", "model", "providerID"}),
	); value != "" {
		out["provider_id"] = value
	}
	if value := core.FirstNonEmpty(
		shared.FirstPathString(rawPayload, []string{"modelID"}),
		shared.FirstPathString(rawPayload, []string{"model_id"}),
		shared.FirstPathString(rawPayload, []string{"input", "model", "modelID"}),
		shared.FirstPathString(rawPayload, []string{"output", "message", "model", "modelID"}),
	); value != "" {
		out["model_id"] = value
	}
	if ts := shared.FirstPathString(rawPayload, []string{"timestamp"}, []string{"time"}); ts != "" {
		out["timestamp"] = ts
	}
	return out
}

func ptrInt64Value(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func parseHookTimestampAny(root map[string]any) time.Time {
	if root == nil {
		return time.Now().UTC()
	}
	if ts := shared.FirstPathNumber(root,
		[]string{"timestamp"},
		[]string{"time"},
		[]string{"event", "timestamp"},
		[]string{"event", "properties", "info", "time", "completed"},
		[]string{"event", "properties", "info", "time", "created"},
	); ts != nil && *ts > 0 {
		return hookTimestampOrNow(int64(*ts))
	}
	if raw := shared.FirstPathString(root, []string{"timestamp"}, []string{"time"}, []string{"event", "timestamp"}); raw != "" {
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
