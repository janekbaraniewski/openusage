package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetryCursorSQLiteSchema = "cursor_sqlite_v1"
)

// System implements shared.TelemetrySource.
func (p *Provider) System() string { return p.ID() }

// Collect implements shared.TelemetrySource. It reads from both the Cursor
// tracking DB (ai_code_hashes) and state DB (composerData, bubbleId) to
// produce telemetry events for time-windowed analytics.
func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	trackingDBPath := shared.ExpandHome(opts.Path("tracking_db", defaultTrackingDBPath()))
	stateDBPath := shared.ExpandHome(opts.Path("state_db", defaultStateDBPath()))

	seenMessages := make(map[string]bool)
	seenTools := make(map[string]bool)
	var out []shared.TelemetryEvent

	// Collect from the tracking DB (ai_code_hashes + scored_commits).
	if trackingDBPath != "" {
		events, commitEvents, err := collectTrackingDBEvents(ctx, trackingDBPath)
		if err == nil {
			appendCursorDedupEvents(&out, events, seenMessages, seenTools)
			appendCursorDedupEvents(&out, commitEvents, seenMessages, seenTools)
		}
	}

	// Collect from the state DB (composerData + bubbleId entries).
	if stateDBPath != "" {
		events, err := collectStateDBEvents(ctx, stateDBPath)
		if err == nil {
			appendCursorDedupEvents(&out, events, seenMessages, seenTools)
		}
	}

	return out, nil
}

// ParseHookPayload implements shared.TelemetrySource. Cursor does not have a
// hook system, so this always returns ErrHookUnsupported.
func (p *Provider) ParseHookPayload(_ []byte, _ shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return nil, shared.ErrHookUnsupported
}

// defaultTrackingDBPath returns the platform-specific default path for the
// Cursor AI code tracking database.
func defaultTrackingDBPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cursor", "ai-tracking", "ai-code-tracking.db")
}

// defaultStateDBPath returns the platform-specific default path for the
// Cursor state database.
func defaultStateDBPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb")
	case "linux":
		return filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "Cursor", "User", "globalStorage", "state.vscdb")
		}
		return filepath.Join(home, "AppData", "Roaming", "Cursor", "User", "globalStorage", "state.vscdb")
	default:
		return filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb")
	}
}

// collectTrackingDBEvents reads the ai_code_hashes and scored_commits tables
// from the Cursor tracking database. Returns (usage events, commit events, error).
func collectTrackingDBEvents(ctx context.Context, dbPath string) ([]shared.TelemetryEvent, []shared.TelemetryEvent, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, nil, nil
	}

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()

	// Collect scored commits from the same DB connection.
	var commitEvents []shared.TelemetryEvent
	if cursorTableExists(ctx, db, "scored_commits") {
		commitEvents, _ = queryScoredCommits(ctx, db, dbPath)
	}

	if !cursorTableExists(ctx, db, "ai_code_hashes") {
		return nil, commitEvents, nil
	}

	timeExpr := chooseTrackingTimeExpr(ctx, db)

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(source, ''),
		       COALESCE(model, ''),
		       COALESCE(fileExtension, ''),
		       COALESCE(fileName, ''),
		       COALESCE(requestId, ''),
		       COALESCE(conversationId, ''),
		       COALESCE(%s, 0),
		       rowid
		FROM ai_code_hashes
		ORDER BY %s ASC`, timeExpr, timeExpr))
	if err != nil {
		return nil, commitEvents, fmt.Errorf("cursor: querying ai_code_hashes: %w", err)
	}
	defer rows.Close()

	var out []shared.TelemetryEvent
	for rows.Next() {
		if ctx.Err() != nil {
			return out, commitEvents, ctx.Err()
		}

		var (
			source         string
			model          string
			fileExt        string
			fileName       string
			requestID      string
			conversationID string
			timestamp      int64
			rowID          int64
		)
		if err := rows.Scan(&source, &model, &fileExt, &fileName, &requestID, &conversationID, &timestamp, &rowID); err != nil {
			continue
		}

		occurredAt := time.Now().UTC()
		if timestamp > 0 {
			occurredAt = shared.UnixAuto(timestamp)
		}

		messageID := fmt.Sprintf("cursor-tracking:%d", rowID)

		clientBucket := cursorSourceToClientBucket(source)

		// Use conversationId as session ID to link tracking events to composer sessions.
		sessionID := strings.TrimSpace(conversationID)

		payload := map[string]any{
			"source": map[string]any{
				"db_path": dbPath,
				"table":   "ai_code_hashes",
				"row_id":  rowID,
			},
			"client":        clientBucket,
			"cursor_source": source,
		}
		if fileExt != "" {
			payload["file_extension"] = fileExt
		}
		if fileName != "" {
			payload["file"] = fileName
		} else if fileExt != "" {
			payload["file"] = "example" + normalizeFileExtension(fileExt)
		}
		if upstream := inferProviderFromModel(model); upstream != "cursor" {
			payload["upstream_provider"] = upstream
		}
		if requestID != "" {
			payload["request_id"] = requestID
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetryCursorSQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    occurredAt,
			AccountID:     "",
			SessionID:     sessionID,
			MessageID:     messageID,
			ProviderID:    "cursor",
			AgentName:     cursorAgentName(source),
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      model,
			Requests:      shared.Int64Ptr(1),
			Status:        shared.TelemetryStatusOK,
			Payload:       payload,
		})
	}

	return out, commitEvents, rows.Err()
}

// collectStateDBEvents reads composerData and bubbleId entries from the
// Cursor state database (cursorDiskKV table).
func collectStateDBEvents(ctx context.Context, dbPath string) ([]shared.TelemetryEvent, error) {
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

	if !cursorTableExists(ctx, db, "cursorDiskKV") {
		return nil, nil
	}

	var out []shared.TelemetryEvent

	// Collect composer session usage events.
	composerEvents, err := collectComposerEvents(ctx, db, dbPath)
	if err == nil {
		out = append(out, composerEvents...)
	}

	// Collect tool usage events from bubble data.
	toolEvents, err := collectToolEvents(ctx, db, dbPath)
	if err == nil {
		out = append(out, toolEvents...)
	}

	// Collect token counts from bubble entries and attach to composer sessions.
	tokenEvents, err := collectBubbleTokenEvents(ctx, db, dbPath)
	if err == nil {
		out = append(out, tokenEvents...)
	}

	// Collect daily stats (tab/composer suggested/accepted lines).
	if cursorTableExists(ctx, db, "ItemTable") {
		dailyEvents, err := collectDailyStatsEvents(ctx, db, dbPath)
		if err == nil {
			out = append(out, dailyEvents...)
		}
	}

	return out, nil
}

// collectComposerEvents extracts usage data from composerData entries.
// Each composer session has a usageData map with per-model cost and request counts,
// plus session metadata like mode, model config, and file changes.
func collectComposerEvents(ctx context.Context, db *sql.DB, dbPath string) ([]shared.TelemetryEvent, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT key,
		       json_extract(value, '$.usageData'),
		       json_extract(value, '$.createdAt'),
		       json_extract(value, '$.unifiedMode'),
		       json_extract(value, '$.forceMode'),
		       json_extract(value, '$.isAgentic'),
		       json_extract(value, '$.totalLinesAdded'),
		       json_extract(value, '$.totalLinesRemoved'),
		       json_extract(value, '$.modelConfig.modelName'),
		       json_extract(value, '$.newlyCreatedFiles'),
		       json_extract(value, '$.addedFiles'),
		       json_extract(value, '$.removedFiles'),
		       json_extract(value, '$.contextTokensUsed'),
		       json_extract(value, '$.contextTokenLimit'),
		       json_extract(value, '$.filesChangedCount')
		FROM cursorDiskKV
		WHERE key LIKE 'composerData:%'
		  AND json_extract(value, '$.usageData') IS NOT NULL
		  AND json_extract(value, '$.usageData') != '{}'`)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying composerData: %w", err)
	}
	defer rows.Close()

	var out []shared.TelemetryEvent
	for rows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		var (
			key             string
			usageJSON       sql.NullString
			createdAt       sql.NullInt64
			mode            sql.NullString
			forceMode       sql.NullString
			isAgentic       sql.NullBool
			linesAdded      sql.NullInt64
			linesRemoved    sql.NullInt64
			modelConfigName sql.NullString
			newlyCreated    sql.NullString
			addedFiles      sql.NullString
			removedFiles    sql.NullString
			ctxTokensUsed   sql.NullInt64
			ctxTokenLimit   sql.NullInt64
			filesChangedCnt sql.NullInt64
		)
		if err := rows.Scan(&key, &usageJSON, &createdAt, &mode, &forceMode, &isAgentic,
			&linesAdded, &linesRemoved, &modelConfigName, &newlyCreated, &addedFiles, &removedFiles,
			&ctxTokensUsed, &ctxTokenLimit, &filesChangedCnt); err != nil {
			continue
		}
		if !usageJSON.Valid || usageJSON.String == "" || usageJSON.String == "{}" {
			continue
		}

		sessionID := strings.TrimPrefix(key, "composerData:")

		var usage map[string]composerModelUsage
		if json.Unmarshal([]byte(usageJSON.String), &usage) != nil {
			continue
		}

		occurredAt := time.Now().UTC()
		if createdAt.Valid && createdAt.Int64 > 0 {
			occurredAt = shared.UnixAuto(createdAt.Int64)
		}

		for model, mu := range usage {
			if mu.Amount <= 0 && mu.CostInCents <= 0 {
				continue
			}

			costUSD := mu.CostInCents / 100.0
			messageID := fmt.Sprintf("cursor-composer:%s:%s", sessionID, sanitizeCursorMetricName(model))

			payload := map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "cursorDiskKV",
					"key":     key,
				},
				"client":        "IDE",
				"cursor_source": "composer",
			}
			if upstream := inferProviderFromModel(model); upstream != "cursor" {
				payload["upstream_provider"] = upstream
			}
			if mode.Valid && mode.String != "" {
				payload["mode"] = mode.String
			}
			if forceMode.Valid && forceMode.String != "" {
				payload["force_mode"] = forceMode.String
			}
			if isAgentic.Valid {
				payload["is_agentic"] = isAgentic.Bool
			}
			if linesAdded.Valid && linesAdded.Int64 > 0 {
				payload["lines_added"] = linesAdded.Int64
			}
			if linesRemoved.Valid && linesRemoved.Int64 > 0 {
				payload["lines_removed"] = linesRemoved.Int64
			}
			if modelConfigName.Valid && modelConfigName.String != "" {
				payload["model_config"] = modelConfigName.String
			}
			newFileCount := countJSONArrayItems(newlyCreated)
			addedCount := countNullableInt(addedFiles)
			removedCount := countNullableInt(removedFiles)
			if newFileCount > 0 {
				payload["newly_created_files"] = newFileCount
			}
			if addedCount > 0 {
				payload["added_files"] = addedCount
			}
			if removedCount > 0 {
				payload["removed_files"] = removedCount
			}
			if ctxTokensUsed.Valid && ctxTokensUsed.Int64 > 0 {
				payload["context_tokens_used"] = ctxTokensUsed.Int64
			}
			if ctxTokenLimit.Valid && ctxTokenLimit.Int64 > 0 {
				payload["context_token_limit"] = ctxTokenLimit.Int64
			}
			if filesChangedCnt.Valid && filesChangedCnt.Int64 > 0 {
				payload["files_changed"] = filesChangedCnt.Int64
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetryCursorSQLiteSchema,
				Channel:       shared.TelemetryChannelSQLite,
				OccurredAt:    occurredAt,
				AccountID:     "",
				SessionID:     sessionID,
				MessageID:     messageID,
				ProviderID:    "cursor",
				AgentName:     "cursor",
				EventType:     shared.TelemetryEventTypeMessageUsage,
				ModelRaw:      model,
				CostUSD:       shared.Float64Ptr(costUSD),
				Requests:      shared.Int64Ptr(int64(mu.Amount)),
				Status:        shared.TelemetryStatusOK,
				Payload:       payload,
			})
		}
	}

	return out, rows.Err()
}

// collectToolEvents extracts tool call data from bubbleId entries in the
// state database. Each AI response bubble (type=2) may contain toolFormerData.
func collectToolEvents(ctx context.Context, db *sql.DB, dbPath string) ([]shared.TelemetryEvent, error) {
	// Pre-query composerData to build a map of conversationId → createdAt
	// so tool events can be assigned meaningful timestamps.
	sessionTimestamps := buildSessionTimestampMap(ctx, db)

	rows, err := db.QueryContext(ctx, `
		SELECT key,
		       json_extract(value, '$.toolFormerData.name'),
		       json_extract(value, '$.toolFormerData.status'),
		       json_extract(value, '$.conversationId')
		FROM cursorDiskKV
		WHERE key LIKE 'bubbleId:%'
		  AND json_extract(value, '$.type') = 2
		  AND json_extract(value, '$.toolFormerData.name') IS NOT NULL
		  AND json_extract(value, '$.toolFormerData.name') != ''`)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying bubbleId tool data: %w", err)
	}
	defer rows.Close()

	var out []shared.TelemetryEvent
	for rows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		var (
			key            string
			toolNameRaw    sql.NullString
			toolStatusRaw  sql.NullString
			conversationID sql.NullString
		)
		if err := rows.Scan(&key, &toolNameRaw, &toolStatusRaw, &conversationID); err != nil {
			continue
		}
		if !toolNameRaw.Valid || toolNameRaw.String == "" {
			continue
		}

		toolName := normalizeToolName(toolNameRaw.String)
		toolCallID := strings.TrimPrefix(key, "bubbleId:")

		status := shared.TelemetryStatusOK
		if toolStatusRaw.Valid {
			status = mapCursorToolStatus(toolStatusRaw.String)
		}

		sessionID := ""
		if conversationID.Valid && conversationID.String != "" {
			sessionID = conversationID.String
		}

		// Derive timestamp from the parent composer session's createdAt.
		// If no matching session is found, use zero time so the telemetry
		// store can handle it appropriately.
		var occurredAt time.Time
		if sessionID != "" {
			if ts, ok := sessionTimestamps[sessionID]; ok {
				occurredAt = ts
			}
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetryCursorSQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    occurredAt,
			AccountID:     "",
			SessionID:     sessionID,
			ToolCallID:    toolCallID,
			ProviderID:    "cursor",
			AgentName:     "cursor",
			EventType:     shared.TelemetryEventTypeToolUsage,
			ToolName:      strings.ToLower(toolName),
			Requests:      shared.Int64Ptr(1),
			Status:        status,
			Payload: map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "cursorDiskKV",
					"key":     key,
				},
				"client":          "IDE",
				"raw_tool_name":   toolNameRaw.String,
				"raw_tool_status": toolStatusRaw.String,
			},
		})
	}

	return out, rows.Err()
}

// buildSessionTimestampMap queries composerData entries from cursorDiskKV and
// returns a map of sessionID (composerData key suffix) → createdAt time.
// This is used to assign meaningful timestamps to tool events (bubbleId entries)
// that reference a conversationId matching a composer session.
func buildSessionTimestampMap(ctx context.Context, db *sql.DB) map[string]time.Time {
	m := make(map[string]time.Time)

	rows, err := db.QueryContext(ctx, `
		SELECT key, json_extract(value, '$.createdAt')
		FROM cursorDiskKV
		WHERE key LIKE 'composerData:%'
		  AND json_extract(value, '$.createdAt') IS NOT NULL`)
	if err != nil {
		return m
	}
	defer rows.Close()

	for rows.Next() {
		if ctx.Err() != nil {
			return m
		}
		var (
			key       string
			createdAt sql.NullInt64
		)
		if err := rows.Scan(&key, &createdAt); err != nil {
			continue
		}
		if !createdAt.Valid || createdAt.Int64 <= 0 {
			continue
		}
		sessionID := strings.TrimPrefix(key, "composerData:")
		m[sessionID] = shared.UnixAuto(createdAt.Int64)
	}

	return m
}

// appendCursorDedupEvents appends events to the output slice, deduplicating
// by message ID (for message usage events) or tool call ID (for tool events).
func appendCursorDedupEvents(
	out *[]shared.TelemetryEvent,
	events []shared.TelemetryEvent,
	seenMessages map[string]bool,
	seenTools map[string]bool,
) {
	for _, ev := range events {
		switch ev.EventType {
		case shared.TelemetryEventTypeToolUsage:
			key := strings.TrimSpace(ev.ToolCallID)
			if key == "" {
				key = strings.TrimSpace(ev.SessionID) + "|" + strings.ToLower(strings.TrimSpace(ev.ToolName))
			}
			if key != "" && seenTools[key] {
				continue
			}
			if key != "" {
				seenTools[key] = true
			}
		case shared.TelemetryEventTypeMessageUsage:
			key := strings.TrimSpace(ev.MessageID)
			if key == "" {
				key = strings.TrimSpace(ev.SessionID) + "|" + strings.TrimSpace(ev.ModelRaw)
			}
			if key != "" && seenMessages[key] {
				continue
			}
			if key != "" {
				seenMessages[key] = true
			}
		}
		*out = append(*out, ev)
	}
}

// cursorTableExists checks whether a table exists in a SQLite database.
func cursorTableExists(ctx context.Context, db *sql.DB, table string) bool {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name=? LIMIT 1`, strings.TrimSpace(table)).Scan(&exists)
	return err == nil && exists == 1
}

// inferProviderFromModel maps a Cursor model intent string to an upstream
// provider ID where possible, falling back to "cursor".
func inferProviderFromModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return "cursor"
	}
	switch {
	case strings.Contains(m, "gpt") || strings.Contains(m, "o1") || strings.Contains(m, "o3") || strings.Contains(m, "o4"):
		return "openai"
	case strings.Contains(m, "claude") || strings.Contains(m, "anthropic"):
		return "anthropic"
	case strings.Contains(m, "gemini") || strings.Contains(m, "google"):
		return "google"
	case strings.Contains(m, "deepseek"):
		return "deepseek"
	case strings.Contains(m, "mistral"):
		return "mistral"
	case strings.Contains(m, "llama") || strings.Contains(m, "meta"):
		return "meta"
	default:
		return "cursor"
	}
}

// cursorSourceToClientBucket maps a Cursor source column value to a client
// bucket name suitable for the clientDimensionExpr "$.client" field.
func cursorSourceToClientBucket(source string) string {
	if strings.ToLower(strings.TrimSpace(source)) == "cli" {
		return "CLI"
	}
	return "IDE"
}

// cursorAgentName maps a Cursor source identifier to an agent name for
// telemetry classification.
func cursorAgentName(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	switch {
	case s == "":
		return "cursor"
	case s == "composer":
		return "cursor-composer"
	case s == "tab":
		return "cursor-tab"
	case strings.Contains(s, "agent"), strings.Contains(s, "cli"):
		return "cursor-agent"
	default:
		return "cursor"
	}
}

// mapCursorToolStatus translates a Cursor tool status string into a
// TelemetryStatus value.
func mapCursorToolStatus(status string) shared.TelemetryStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "completed", "complete", "success":
		return shared.TelemetryStatusOK
	case "error", "failed", "failure":
		return shared.TelemetryStatusError
	case "aborted", "cancelled", "canceled":
		return shared.TelemetryStatusAborted
	default:
		return shared.TelemetryStatusUnknown
	}
}

// normalizeFileExtension ensures the extension starts with a dot.
func normalizeFileExtension(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		return "." + ext
	}
	return ext
}

// collectBubbleTokenEvents extracts token counts from bubbleId entries in the
// state DB. Each AI response bubble (type=2) may have a tokenCount with
// inputTokens/outputTokens. These are emitted as message_usage events linked
// to their parent composer session via conversationId.
func collectBubbleTokenEvents(ctx context.Context, db *sql.DB, dbPath string) ([]shared.TelemetryEvent, error) {
	sessionTimestamps := buildSessionTimestampMap(ctx, db)

	rows, err := db.QueryContext(ctx, `
		SELECT key,
		       json_extract(value, '$.tokenCount.inputTokens'),
		       json_extract(value, '$.tokenCount.outputTokens'),
		       json_extract(value, '$.conversationId'),
		       json_extract(value, '$.model')
		FROM cursorDiskKV
		WHERE key LIKE 'bubbleId:%'
		  AND json_extract(value, '$.type') = 2
		  AND json_extract(value, '$.tokenCount') IS NOT NULL
		  AND json_extract(value, '$.tokenCount.inputTokens') > 0`)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying bubbleId tokens: %w", err)
	}
	defer rows.Close()

	var out []shared.TelemetryEvent
	for rows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		var (
			key            string
			inputTokens    sql.NullInt64
			outputTokens   sql.NullInt64
			conversationID sql.NullString
			model          sql.NullString
		)
		if err := rows.Scan(&key, &inputTokens, &outputTokens, &conversationID, &model); err != nil {
			continue
		}
		if !inputTokens.Valid || inputTokens.Int64 <= 0 {
			continue
		}

		bubbleID := strings.TrimPrefix(key, "bubbleId:")
		messageID := fmt.Sprintf("cursor-bubble-tokens:%s", bubbleID)

		sessionID := ""
		if conversationID.Valid && conversationID.String != "" {
			sessionID = conversationID.String
		}

		var occurredAt time.Time
		if sessionID != "" {
			if ts, ok := sessionTimestamps[sessionID]; ok {
				occurredAt = ts
			}
		}

		modelRaw := ""
		if model.Valid {
			modelRaw = model.String
		}

		var inTok, outTok *int64
		if inputTokens.Valid && inputTokens.Int64 > 0 {
			inTok = shared.Int64Ptr(inputTokens.Int64)
		}
		if outputTokens.Valid && outputTokens.Int64 > 0 {
			outTok = shared.Int64Ptr(outputTokens.Int64)
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetryCursorSQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    occurredAt,
			SessionID:     sessionID,
			MessageID:     messageID,
			ProviderID:    "cursor",
			AgentName:     "cursor",
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      modelRaw,
			InputTokens:   inTok,
			OutputTokens:  outTok,
			Requests:      shared.Int64Ptr(1),
			Status:        shared.TelemetryStatusOK,
			Payload: map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "cursorDiskKV",
					"key":     key,
				},
				"client":        "IDE",
				"cursor_source": "composer",
			},
		})
	}

	return out, rows.Err()
}

// collectDailyStatsEvents extracts daily code tracking stats from ItemTable.
// Keys like "aiCodeTracking.dailyStats.v1.5.2025-11-23" contain tab/composer
// suggested/accepted line counts per day.
func collectDailyStatsEvents(ctx context.Context, db *sql.DB, dbPath string) ([]shared.TelemetryEvent, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT key, value FROM ItemTable
		WHERE key LIKE 'aiCodeTracking.dailyStats.%'`)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying dailyStats: %w", err)
	}
	defer rows.Close()

	var out []shared.TelemetryEvent
	for rows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		var key, rawJSON string
		if err := rows.Scan(&key, &rawJSON); err != nil {
			continue
		}

		var stats cursorDailyStats
		if json.Unmarshal([]byte(rawJSON), &stats) != nil {
			continue
		}
		if stats.Date == "" {
			continue
		}

		dayTime, err := time.Parse("2006-01-02", stats.Date)
		if err != nil {
			continue
		}

		messageID := fmt.Sprintf("cursor-daily-stats:%s", stats.Date)

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetryCursorSQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    dayTime,
			MessageID:     messageID,
			ProviderID:    "cursor",
			AgentName:     "cursor",
			EventType:     shared.TelemetryEventTypeRawEnvelope,
			Requests:      shared.Int64Ptr(1),
			Status:        shared.TelemetryStatusOK,
			Payload: map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "ItemTable",
					"key":     key,
				},
				"daily_stats": map[string]any{
					"date":                     stats.Date,
					"tab_suggested_lines":      stats.TabSuggestedLines,
					"tab_accepted_lines":       stats.TabAcceptedLines,
					"composer_suggested_lines": stats.ComposerSuggestedLines,
					"composer_accepted_lines":  stats.ComposerAcceptedLines,
				},
			},
		})
	}

	return out, rows.Err()
}

// queryScoredCommits reads scored_commits from an already-open tracking DB
// and produces telemetry events with AI contribution percentages per commit.
func queryScoredCommits(ctx context.Context, db *sql.DB, dbPath string) ([]shared.TelemetryEvent, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT commitHash, branchName, scoredAt,
		       COALESCE(linesAdded, 0), COALESCE(linesDeleted, 0),
		       COALESCE(tabLinesAdded, 0), COALESCE(tabLinesDeleted, 0),
		       COALESCE(composerLinesAdded, 0), COALESCE(composerLinesDeleted, 0),
		       COALESCE(humanLinesAdded, 0), COALESCE(humanLinesDeleted, 0),
		       COALESCE(commitMessage, ''),
		       COALESCE(v1AiPercentage, ''), COALESCE(v2AiPercentage, '')
		FROM scored_commits
		ORDER BY scoredAt ASC`)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying scored_commits: %w", err)
	}
	defer rows.Close()

	var out []shared.TelemetryEvent
	for rows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		var (
			commitHash       string
			branchName       string
			scoredAt         int64
			linesAdded       int64
			linesDeleted     int64
			tabAdded         int64
			tabDeleted       int64
			composerAdded    int64
			composerDeleted  int64
			humanAdded       int64
			humanDeleted     int64
			commitMessage    string
			v1AiPct, v2AiPct string
		)
		if err := rows.Scan(&commitHash, &branchName, &scoredAt,
			&linesAdded, &linesDeleted, &tabAdded, &tabDeleted,
			&composerAdded, &composerDeleted, &humanAdded, &humanDeleted,
			&commitMessage, &v1AiPct, &v2AiPct); err != nil {
			continue
		}

		occurredAt := time.Now().UTC()
		if scoredAt > 0 {
			occurredAt = shared.UnixAuto(scoredAt)
		}

		messageID := fmt.Sprintf("cursor-scored-commit:%s", commitHash)

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetryCursorSQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    occurredAt,
			MessageID:     messageID,
			ProviderID:    "cursor",
			AgentName:     "cursor",
			EventType:     shared.TelemetryEventTypeRawEnvelope,
			Requests:      shared.Int64Ptr(1),
			Status:        shared.TelemetryStatusOK,
			Payload: map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "scored_commits",
				},
				"scored_commit": map[string]any{
					"commit_hash":            commitHash,
					"branch":                 branchName,
					"message":                truncateString(commitMessage, 200),
					"lines_added":            linesAdded,
					"lines_deleted":          linesDeleted,
					"tab_lines_added":        tabAdded,
					"tab_lines_deleted":      tabDeleted,
					"composer_lines_added":   composerAdded,
					"composer_lines_deleted": composerDeleted,
					"human_lines_added":      humanAdded,
					"human_lines_deleted":    humanDeleted,
					"v1_ai_percentage":       v1AiPct,
					"v2_ai_percentage":       v2AiPct,
				},
			},
		})
	}

	return out, rows.Err()
}

type cursorDailyStats struct {
	Date                   string `json:"date"`
	TabSuggestedLines      int    `json:"tabSuggestedLines"`
	TabAcceptedLines       int    `json:"tabAcceptedLines"`
	ComposerSuggestedLines int    `json:"composerSuggestedLines"`
	ComposerAcceptedLines  int    `json:"composerAcceptedLines"`
}

// countJSONArrayItems parses a nullable string as a JSON array and returns its length.
func countJSONArrayItems(s sql.NullString) int {
	if !s.Valid || s.String == "" || s.String == "[]" {
		return 0
	}
	var arr []any
	if json.Unmarshal([]byte(s.String), &arr) != nil {
		return 0
	}
	return len(arr)
}

// countNullableInt parses a nullable string as an integer count.
// Handles both integer values and JSON array strings.
func countNullableInt(s sql.NullString) int {
	if !s.Valid || s.String == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(s.String, "%d", &n); err == nil {
		return n
	}
	return countJSONArrayItems(s)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
