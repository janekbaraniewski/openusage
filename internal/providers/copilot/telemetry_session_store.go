package copilot

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

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

	out, err := appendSessionStoreTurnEvents(ctx, db, dbPath, skipSessions)
	if err != nil {
		return out, err
	}
	if !copilotTelemetryTableExists(ctx, db, "session_files") {
		return out, nil
	}
	return appendSessionStoreFileEvents(ctx, db, dbPath, skipSessions, out)
}

func appendSessionStoreTurnEvents(ctx context.Context, db *sql.DB, dbPath string, skipSessions map[string]bool) ([]shared.TelemetryEvent, error) {
	rows, err := db.QueryContext(ctx, `
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
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []shared.TelemetryEvent
	for rows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		var sessionID, cwd, repo, userMsg, reply, tsRaw string
		var turnIndex int
		if err := rows.Scan(&sessionID, &cwd, &repo, &turnIndex, &userMsg, &reply, &tsRaw); err != nil {
			continue
		}
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" || skipSessions[sessionID] {
			continue
		}
		out = append(out, buildSessionStoreTurnEvent(dbPath, sessionID, cwd, repo, userMsg, reply, tsRaw, turnIndex))
	}
	return out, rows.Err()
}

func buildSessionStoreTurnEvent(dbPath, sessionID, cwd, repo, userMsg, reply, tsRaw string, turnIndex int) shared.TelemetryEvent {
	occurredAt := time.Now().UTC()
	if parsed := shared.FlexParseTime(tsRaw); !parsed.IsZero() {
		occurredAt = parsed
	}
	messageID := fmt.Sprintf("%s:turn:%d", sessionID, turnIndex)
	payload := map[string]any{
		"source_file":            dbPath,
		"event":                  "session_store.turn",
		"client":                 normalizeCopilotClient(repo, cwd),
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
	return shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       shared.TelemetryChannelSQLite,
		OccurredAt:    occurredAt,
		AccountID:     "copilot",
		WorkspaceID:   shared.SanitizeWorkspace(cwd),
		SessionID:     sessionID,
		TurnID:        messageID,
		MessageID:     messageID,
		ProviderID:    "copilot",
		AgentName:     "copilot",
		EventType:     shared.TelemetryEventTypeMessageUsage,
		ModelRaw:      "unknown",
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(1),
		},
		Status:  shared.TelemetryStatusOK,
		Payload: payload,
	}
}

func appendSessionStoreFileEvents(ctx context.Context, db *sql.DB, dbPath string, skipSessions map[string]bool, out []shared.TelemetryEvent) ([]shared.TelemetryEvent, error) {
	rows, err := db.QueryContext(ctx, `
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
	if err != nil {
		return out, nil
	}
	defer rows.Close()

	for rows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		var sessionID, filePath, toolRaw, tsRaw, cwd, repo string
		var turnIndex int
		if err := rows.Scan(&sessionID, &filePath, &toolRaw, &turnIndex, &tsRaw, &cwd, &repo); err != nil {
			continue
		}
		sessionID = strings.TrimSpace(sessionID)
		filePath = strings.TrimSpace(filePath)
		if sessionID == "" || filePath == "" || skipSessions[sessionID] {
			continue
		}
		out = append(out, buildSessionStoreFileEvent(dbPath, sessionID, filePath, toolRaw, tsRaw, cwd, repo, turnIndex))
	}
	return out, nil
}

func buildSessionStoreFileEvent(dbPath, sessionID, filePath, toolRaw, tsRaw, cwd, repo string, turnIndex int) shared.TelemetryEvent {
	occurredAt := time.Now().UTC()
	if parsed := shared.FlexParseTime(tsRaw); !parsed.IsZero() {
		occurredAt = parsed
	}
	toolName, meta := normalizeCopilotTelemetryToolName(toolRaw)
	if toolName == "" || toolName == "unknown" {
		toolName = "workspace_file_changed"
	}
	messageID := fmt.Sprintf("%s:turn:%d", sessionID, turnIndex)
	payload := map[string]any{
		"source_file":            dbPath,
		"event":                  "session_store.file",
		"client":                 normalizeCopilotClient(repo, cwd),
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
	return shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       shared.TelemetryChannelSQLite,
		OccurredAt:    occurredAt,
		AccountID:     "copilot",
		WorkspaceID:   shared.SanitizeWorkspace(cwd),
		SessionID:     sessionID,
		TurnID:        messageID,
		MessageID:     messageID,
		ToolCallID:    fmt.Sprintf("store:%s:%d:%s", sessionID, turnIndex, sanitizeMetricName(filePath)),
		ProviderID:    "copilot",
		AgentName:     "copilot",
		EventType:     shared.TelemetryEventTypeToolUsage,
		ModelRaw:      "unknown",
		TokenUsage: core.TokenUsage{
			Requests: core.Int64Ptr(1),
		},
		ToolName: toolName,
		Status:   shared.TelemetryStatusOK,
		Payload:  payload,
	}
}

func copilotTelemetryTableExists(ctx context.Context, db *sql.DB, table string) bool {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name=? LIMIT 1`, strings.TrimSpace(table)).Scan(&exists)
	return err == nil && exists == 1
}
