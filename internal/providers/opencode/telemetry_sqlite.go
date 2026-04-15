package opencode

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// CollectTelemetryFromSQLite parses OpenCode SQLite data (message + part tables).
func CollectTelemetryFromSQLite(ctx context.Context, dbPath string) ([]shared.TelemetryEvent, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat opencode sqlite db: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	hasMessageTable, err := sqliteTableExists(ctx, db, "message")
	if err != nil {
		return nil, err
	}
	if !hasMessageTable {
		return nil, nil
	}

	partSummaryByMessage := make(map[string]partSummary)
	hasPartTable, err := sqliteTableExists(ctx, db, "part")
	if err != nil {
		return nil, err
	}
	if hasPartTable {
		partSummaryByMessage, err = collectPartSummary(ctx, db)
		if err != nil {
			return nil, err
		}
	}

	out, seenMessages, err := collectSQLiteMessageEvents(ctx, db, dbPath, partSummaryByMessage, hasPartTable)
	if err != nil {
		return out, err
	}
	if !hasPartTable {
		return out, nil
	}

	return collectSQLiteToolEvents(ctx, db, dbPath, partSummaryByMessage, seenMessages, out)
}

func collectSQLiteMessageEvents(
	ctx context.Context,
	db *sql.DB,
	dbPath string,
	partSummaryByMessage map[string]partSummary,
	hasPartTable bool,
) ([]shared.TelemetryEvent, map[string]bool, error) {
	var out []shared.TelemetryEvent
	seenMessages := map[string]bool{}

	if hasPartTable {
		if err := appendSQLiteStepFinishEvents(ctx, db, dbPath, partSummaryByMessage, &out, seenMessages); err != nil {
			return out, seenMessages, err
		}
	}
	if err := appendSQLiteMessageTableEvents(ctx, db, dbPath, partSummaryByMessage, &out, seenMessages); err != nil {
		return out, seenMessages, err
	}

	return out, seenMessages, nil
}

func appendSQLiteStepFinishEvents(
	ctx context.Context,
	db *sql.DB,
	dbPath string,
	partSummaryByMessage map[string]partSummary,
	out *[]shared.TelemetryEvent,
	seenMessages map[string]bool,
) error {
	rows, err := db.QueryContext(ctx, `
		SELECT p.id, p.message_id, p.session_id, p.time_created, p.time_updated, p.data, COALESCE(m.data, '{}'), COALESCE(s.directory, '')
		FROM part p
		LEFT JOIN message m ON m.id = p.message_id
		LEFT JOIN session s ON s.id = p.session_id
		WHERE COALESCE(json_extract(p.data, '$.type'), '') = 'step-finish'
		ORDER BY p.time_updated ASC
	`)
	if err != nil {
		return fmt.Errorf("query opencode sqlite step-finish rows: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		if ctx.Err() != nil {
			return ctx.Err()
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
		if err := rows.Scan(&partID, &messageIDDB, &sessionIDDB, &timeCreated, &timeUpdated, &partJSON, &messageJSON, &sessionDir); err != nil {
			return fmt.Errorf("scan opencode sqlite step-finish row: %w", err)
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

		*out = append(*out, buildSQLiteStepFinishEvent(
			dbPath,
			partID,
			messageIDDB,
			sessionIDDB,
			timeCreated,
			timeUpdated,
			sessionDir,
			partPayload,
			messagePayload,
			partSummaryByMessage[messageID],
			u,
		))
		seenMessages[messageID] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate opencode sqlite step-finish rows: %w", err)
	}
	return nil
}

func buildSQLiteStepFinishEvent(
	dbPath, partID, messageIDDB, sessionIDDB string,
	timeCreated, timeUpdated int64,
	sessionDir string,
	partPayload, messagePayload map[string]any,
	summary partSummary,
	u usage,
) shared.TelemetryEvent {
	messageID := core.FirstNonEmpty(strings.TrimSpace(messageIDDB), shared.FirstPathString(messagePayload, []string{"id"}), shared.FirstPathString(messagePayload, []string{"messageID"}))
	sessionID := core.FirstNonEmpty(strings.TrimSpace(sessionIDDB), shared.FirstPathString(messagePayload, []string{"sessionID"}))
	turnID := core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"parentID"}), shared.FirstPathString(messagePayload, []string{"turnID"}))
	providerID := core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"providerID"}), shared.FirstPathString(messagePayload, []string{"model", "providerID"}), "opencode")
	modelRaw := core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"modelID"}), shared.FirstPathString(messagePayload, []string{"model", "modelID"}))

	occurredAt := shared.UnixAuto(timeUpdated)
	if timeCreated > 0 {
		occurredAt = shared.UnixAuto(timeCreated)
	}

	return shared.TelemetryEvent{
		SchemaVersion: telemetrySQLiteSchema,
		Channel:       shared.TelemetryChannelSQLite,
		OccurredAt:    occurredAt,
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
		Status: mapMessageStatus(shared.FirstPathString(partPayload, []string{"reason"})),
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
			"upstream_provider": extractUpstreamProviderFromMaps(partPayload, messagePayload),
			"context":           contextSummaryFromPartSummary(summary),
		},
	}
}

func appendSQLiteMessageTableEvents(
	ctx context.Context,
	db *sql.DB,
	dbPath string,
	partSummaryByMessage map[string]partSummary,
	out *[]shared.TelemetryEvent,
	seenMessages map[string]bool,
) error {
	rows, err := db.QueryContext(ctx, `
		SELECT m.id, m.session_id, m.time_created, m.time_updated, m.data, COALESCE(s.directory, '')
		FROM message m
		LEFT JOIN session s ON s.id = m.session_id
		ORDER BY m.time_updated ASC
	`)
	if err != nil {
		return fmt.Errorf("query opencode sqlite message rows: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		var (
			messageIDRaw string
			sessionIDRaw string
			timeCreated  int64
			timeUpdated  int64
			messageJSON  string
			sessionDir   string
		)
		if err := rows.Scan(&messageIDRaw, &sessionIDRaw, &timeCreated, &timeUpdated, &messageJSON, &sessionDir); err != nil {
			return fmt.Errorf("scan opencode sqlite message row: %w", err)
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
		if messageID == "" || seenMessages[messageID] || !hasUsage(u) {
			continue
		}

		*out = append(*out, buildSQLiteMessageTableEvent(
			dbPath,
			messageIDRaw,
			sessionIDRaw,
			timeCreated,
			timeUpdated,
			completedAt,
			createdAt,
			sessionDir,
			payload,
			partSummaryByMessage[messageID],
			u,
		))
		seenMessages[messageID] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate opencode sqlite message rows: %w", err)
	}
	return nil
}

func buildSQLiteMessageTableEvent(
	dbPath, messageIDRaw, sessionIDRaw string,
	timeCreated, timeUpdated, completedAt, createdAt int64,
	sessionDir string,
	payload map[string]any,
	summary partSummary,
	u usage,
) shared.TelemetryEvent {
	messageID := core.FirstNonEmpty(strings.TrimSpace(messageIDRaw), shared.FirstPathString(payload, []string{"id"}), shared.FirstPathString(payload, []string{"messageID"}))
	providerID := core.FirstNonEmpty(shared.FirstPathString(payload, []string{"providerID"}), shared.FirstPathString(payload, []string{"model", "providerID"}), "opencode")
	modelRaw := core.FirstNonEmpty(shared.FirstPathString(payload, []string{"modelID"}), shared.FirstPathString(payload, []string{"model", "modelID"}))
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

	return shared.TelemetryEvent{
		SchemaVersion: telemetrySQLiteSchema,
		Channel:       shared.TelemetryChannelSQLite,
		OccurredAt:    occurredAt,
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
		Status:  finishStatus(shared.FirstPathString(payload, []string{"finish"})),
		Payload: sqliteMessagePayload(dbPath, messageIDRaw, sessionIDRaw, timeCreated, timeUpdated, payload, providerID, modelRaw, summary),
	}
}

func finishStatus(finish string) shared.TelemetryStatus {
	status := shared.TelemetryStatusOK
	finish = strings.ToLower(finish)
	if strings.Contains(finish, "error") || strings.Contains(finish, "fail") {
		status = shared.TelemetryStatusError
	}
	if strings.Contains(finish, "abort") || strings.Contains(finish, "cancel") {
		status = shared.TelemetryStatusAborted
	}
	return status
}

func sqliteMessagePayload(
	dbPath, messageIDRaw, sessionIDRaw string,
	timeCreated, timeUpdated int64,
	payload map[string]any,
	providerID, modelRaw string,
	summary partSummary,
) map[string]any {
	return map[string]any{
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
		"upstream_provider": extractUpstreamProviderFromMaps(payload),
		"context":           contextSummaryFromPartSummary(summary),
	}
}

func collectSQLiteToolEvents(
	ctx context.Context,
	db *sql.DB,
	dbPath string,
	partSummaryByMessage map[string]partSummary,
	seenMessages map[string]bool,
	out []shared.TelemetryEvent,
) ([]shared.TelemetryEvent, error) {
	_ = partSummaryByMessage
	_ = seenMessages

	rows, err := db.QueryContext(ctx, `
		SELECT p.id, p.message_id, p.session_id, p.time_created, p.time_updated, p.data, COALESCE(m.data, '{}'), COALESCE(s.directory, '')
		FROM part p
		LEFT JOIN message m ON m.id = p.message_id
		LEFT JOIN session s ON s.id = p.session_id
		WHERE COALESCE(json_extract(p.data, '$.type'), '') = 'tool'
		ORDER BY p.time_updated ASC
	`)
	if err != nil {
		return out, fmt.Errorf("query opencode sqlite tool rows: %w", err)
	}
	defer rows.Close()

	seenTools := map[string]bool{}
	for rows.Next() {
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
		if err := rows.Scan(&partID, &messageIDDB, &sessionIDDB, &timeCreated, &timeUpdated, &partJSON, &messageJSON, &sessionDir); err != nil {
			return out, fmt.Errorf("scan opencode sqlite tool row: %w", err)
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

		out = append(out, buildSQLiteToolEvent(
			dbPath,
			partID,
			messageIDDB,
			sessionIDDB,
			timeCreated,
			timeUpdated,
			sessionDir,
			partPayload,
			messagePayload,
			status,
			statusRaw,
		))
	}

	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate opencode sqlite tool rows: %w", err)
	}
	return out, nil
}

func buildSQLiteToolEvent(
	dbPath, partID, messageIDDB, sessionIDDB string,
	timeCreated, timeUpdated int64,
	sessionDir string,
	partPayload, messagePayload map[string]any,
	status shared.TelemetryStatus,
	statusRaw string,
) shared.TelemetryEvent {
	toolCallID := core.FirstNonEmpty(shared.FirstPathString(partPayload, []string{"callID"}), shared.FirstPathString(partPayload, []string{"call_id"}), strings.TrimSpace(partID))
	toolName := strings.ToLower(core.FirstNonEmpty(shared.FirstPathString(partPayload, []string{"tool"}), shared.FirstPathString(partPayload, []string{"name"}), "unknown"))
	sessionID := core.FirstNonEmpty(strings.TrimSpace(sessionIDDB), shared.FirstPathString(partPayload, []string{"sessionID"}), shared.FirstPathString(messagePayload, []string{"sessionID"}))
	messageID := core.FirstNonEmpty(strings.TrimSpace(messageIDDB), shared.FirstPathString(partPayload, []string{"messageID"}), shared.FirstPathString(messagePayload, []string{"id"}))
	providerID := core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"providerID"}), shared.FirstPathString(messagePayload, []string{"model", "providerID"}), "opencode")
	modelRaw := core.FirstNonEmpty(shared.FirstPathString(messagePayload, []string{"modelID"}), shared.FirstPathString(messagePayload, []string{"model", "modelID"}))

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

	return shared.TelemetryEvent{
		SchemaVersion: telemetrySQLiteSchema,
		Channel:       shared.TelemetryChannelSQLite,
		OccurredAt:    occurredAt,
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
			"upstream_provider": extractUpstreamProviderFromMaps(partPayload, messagePayload),
			"status":            statusRaw,
			"file":              extractToolFilePath(partPayload),
		},
	}
}

func extractToolFilePath(partPayload map[string]any) string {
	if stateInput, ok := partPayload["state"].(map[string]any); ok {
		if paths := shared.ExtractFilePathsFromPayload(stateInput); len(paths) > 0 {
			return paths[0]
		}
	}
	if paths := shared.ExtractFilePathsFromPayload(partPayload); len(paths) > 0 {
		return paths[0]
	}
	return ""
}

func contextSummaryFromPartSummary(summary partSummary) map[string]any {
	if summary.PartsTotal == 0 && len(summary.PartsByType) == 0 {
		return map[string]any{}
	}
	partsByType := make(map[string]any, len(summary.PartsByType))
	for partType, count := range summary.PartsByType {
		partsByType[partType] = count
	}
	return map[string]any{
		"parts_total":   summary.PartsTotal,
		"parts_by_type": partsByType,
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
			return out, fmt.Errorf("scan opencode sqlite part summary row: %w", err)
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
		return out, fmt.Errorf("iterate opencode sqlite part summary rows: %w", err)
	}
	return out, nil
}

func sqliteTableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name=? LIMIT 1`, strings.TrimSpace(table)).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query opencode sqlite table %q: %w", strings.TrimSpace(table), err)
	}
	return exists == 1, nil
}
