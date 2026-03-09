package cursor

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetryCursorSQLiteSchema = "cursor_sqlite_v1"
)

// System implements shared.TelemetrySource.
func (p *Provider) System() string { return p.ID() }

func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			"tracking_db": defaultTrackingDBPath(),
			"state_db":    defaultStateDBPath(),
		},
	}
}

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
		commitEvents, _ = queryScoredCommits(ctx, db, dbPath, core.SystemClock{})
	}

	if !cursorTableExists(ctx, db, "ai_code_hashes") {
		return nil, commitEvents, nil
	}

	records, err := loadTrackingRecords(ctx, db, core.SystemClock{})
	if err != nil {
		return nil, commitEvents, err
	}

	var out []shared.TelemetryEvent
	for _, record := range records {
		messageID := fmt.Sprintf("cursor-tracking:%d", record.RowID)
		clientBucket := cursorSourceToClientBucket(record.Source)
		payload := map[string]any{
			"source": map[string]any{
				"db_path": dbPath,
				"table":   "ai_code_hashes",
				"row_id":  record.RowID,
			},
			"client":        clientBucket,
			"cursor_source": record.Source,
		}
		if record.FileExt != "" {
			payload["file_extension"] = record.FileExt
		}
		if record.FileName != "" {
			payload["file"] = record.FileName
		} else if record.FileExt != "" {
			payload["file"] = "example" + normalizeFileExtension(record.FileExt)
		}
		if upstream := inferProviderFromModel(record.Model); upstream != "cursor" {
			payload["upstream_provider"] = upstream
		}
		if record.RequestID != "" {
			payload["request_id"] = record.RequestID
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetryCursorSQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    record.OccurredAt,
			AccountID:     "",
			SessionID:     strings.TrimSpace(record.SessionID),
			MessageID:     messageID,
			ProviderID:    "cursor",
			AgentName:     cursorAgentName(record.Source),
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      record.Model,
			TokenUsage: core.TokenUsage{
				Requests: core.Int64Ptr(1),
			},
			Status:  shared.TelemetryStatusOK,
			Payload: payload,
		})
	}

	return out, commitEvents, nil
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
	composerRecords, err := loadComposerSessionRecords(ctx, db)
	if err != nil {
		composerRecords = nil
	}
	bubbleRecords, err := loadBubbleRecords(ctx, db)
	if err != nil {
		bubbleRecords = nil
	}
	sessionTimestamps := composerSessionTimestampMap(composerRecords)

	composerEvents := composerEventsFromRecords(composerRecords, dbPath)
	if len(composerEvents) > 0 {
		out = append(out, composerEvents...)
	}

	toolEvents := toolEventsFromBubbleRecords(bubbleRecords, sessionTimestamps, dbPath)
	if len(toolEvents) > 0 {
		out = append(out, toolEvents...)
	}

	tokenEvents := bubbleTokenEventsFromRecords(bubbleRecords, sessionTimestamps, dbPath)
	if len(tokenEvents) > 0 {
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
func composerEventsFromRecords(records []cursorComposerSessionRecord, dbPath string) []shared.TelemetryEvent {
	var out []shared.TelemetryEvent
	for _, record := range records {
		for model, usage := range record.Usage {
			if usage.Amount <= 0 && usage.CostInCents <= 0 {
				continue
			}

			costUSD := usage.CostInCents / 100.0
			messageID := fmt.Sprintf("cursor-composer:%s:%s", record.SessionID, sanitizeCursorMetricName(model))
			payload := map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "cursorDiskKV",
					"key":     record.Key,
				},
				"client":        "IDE",
				"cursor_source": "composer",
			}
			if upstream := inferProviderFromModel(model); upstream != "cursor" {
				payload["upstream_provider"] = upstream
			}
			if record.Mode != "" {
				payload["mode"] = record.Mode
			}
			if record.ForceMode != "" {
				payload["force_mode"] = record.ForceMode
			}
			if record.IsAgentic != nil {
				payload["is_agentic"] = *record.IsAgentic
			}
			if record.LinesAdded > 0 {
				payload["lines_added"] = record.LinesAdded
			}
			if record.LinesRemoved > 0 {
				payload["lines_removed"] = record.LinesRemoved
			}
			if record.ModelConfigName != "" {
				payload["model_config"] = record.ModelConfigName
			}
			if record.NewlyCreatedFiles > 0 {
				payload["newly_created_files"] = record.NewlyCreatedFiles
			}
			if record.AddedFiles > 0 {
				payload["added_files"] = record.AddedFiles
			}
			if record.RemovedFiles > 0 {
				payload["removed_files"] = record.RemovedFiles
			}
			if record.ContextTokensUsed > 0 {
				payload["context_tokens_used"] = record.ContextTokensUsed
			}
			if record.ContextTokenLimit > 0 {
				payload["context_token_limit"] = record.ContextTokenLimit
			}
			if record.FilesChanged > 0 {
				payload["files_changed"] = record.FilesChanged
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetryCursorSQLiteSchema,
				Channel:       shared.TelemetryChannelSQLite,
				OccurredAt:    record.OccurredAt,
				AccountID:     "",
				SessionID:     record.SessionID,
				MessageID:     messageID,
				ProviderID:    "cursor",
				AgentName:     "cursor",
				EventType:     shared.TelemetryEventTypeMessageUsage,
				ModelRaw:      model,
				TokenUsage: core.TokenUsage{
					CostUSD:  core.Float64Ptr(costUSD),
					Requests: core.Int64Ptr(int64(usage.Amount)),
				},
				Status:  shared.TelemetryStatusOK,
				Payload: payload,
			})
		}
	}
	return out
}

func toolEventsFromBubbleRecords(records []cursorBubbleRecord, sessionTimestamps map[string]time.Time, dbPath string) []shared.TelemetryEvent {
	var out []shared.TelemetryEvent
	for _, record := range records {
		if strings.TrimSpace(record.ToolName) == "" {
			continue
		}
		status := mapCursorToolStatus(record.ToolStatus)
		occurredAt := sessionTimestamps[record.SessionID]
		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetryCursorSQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    occurredAt,
			AccountID:     "",
			SessionID:     record.SessionID,
			ToolCallID:    record.BubbleID,
			ProviderID:    "cursor",
			AgentName:     "cursor",
			EventType:     shared.TelemetryEventTypeToolUsage,
			TokenUsage: core.TokenUsage{
				Requests: core.Int64Ptr(1),
			},
			ToolName: strings.ToLower(normalizeToolName(record.ToolName)),
			Status:   status,
			Payload: map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "cursorDiskKV",
					"key":     record.Key,
				},
				"client":          "IDE",
				"raw_tool_name":   record.ToolName,
				"raw_tool_status": record.ToolStatus,
			},
		})
	}
	return out
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
func bubbleTokenEventsFromRecords(records []cursorBubbleRecord, sessionTimestamps map[string]time.Time, dbPath string) []shared.TelemetryEvent {
	var out []shared.TelemetryEvent
	for _, record := range records {
		if record.InputTokens <= 0 {
			continue
		}
		messageID := fmt.Sprintf("cursor-bubble-tokens:%s", record.BubbleID)
		occurredAt := sessionTimestamps[record.SessionID]
		var inTok, outTok *int64
		inTok = core.Int64Ptr(record.InputTokens)
		if record.OutputTokens > 0 {
			outTok = core.Int64Ptr(record.OutputTokens)
		}
		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetryCursorSQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    occurredAt,
			SessionID:     record.SessionID,
			MessageID:     messageID,
			ProviderID:    "cursor",
			AgentName:     "cursor",
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      record.Model,
			TokenUsage: core.TokenUsage{
				InputTokens:  inTok,
				OutputTokens: outTok,
				Requests:     core.Int64Ptr(1),
			},
			Status: shared.TelemetryStatusOK,
			Payload: map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "cursorDiskKV",
					"key":     record.Key,
				},
				"client":        "IDE",
				"cursor_source": "composer",
			},
		})
	}
	return out
}

// collectDailyStatsEvents extracts daily code tracking stats from ItemTable.
// Keys like "aiCodeTracking.dailyStats.v1.5.2025-11-23" contain tab/composer
// suggested/accepted line counts per day.
func collectDailyStatsEvents(ctx context.Context, db *sql.DB, dbPath string) ([]shared.TelemetryEvent, error) {
	records, err := loadDailyStatsRecords(ctx, db)
	if err != nil {
		return nil, err
	}

	var out []shared.TelemetryEvent
	for _, record := range records {
		dayTime, err := time.Parse("2006-01-02", record.Date)
		if err != nil {
			continue
		}

		messageID := fmt.Sprintf("cursor-daily-stats:%s", record.Date)

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetryCursorSQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    dayTime,
			MessageID:     messageID,
			ProviderID:    "cursor",
			AgentName:     "cursor",
			EventType:     shared.TelemetryEventTypeRawEnvelope,
			TokenUsage: core.TokenUsage{
				Requests: core.Int64Ptr(1),
			},
			Status: shared.TelemetryStatusOK,
			Payload: map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "ItemTable",
					"key":     record.Key,
				},
				"daily_stats": map[string]any{
					"date":                     record.Date,
					"tab_suggested_lines":      record.Stats.TabSuggestedLines,
					"tab_accepted_lines":       record.Stats.TabAcceptedLines,
					"composer_suggested_lines": record.Stats.ComposerSuggestedLines,
					"composer_accepted_lines":  record.Stats.ComposerAcceptedLines,
				},
			},
		})
	}

	return out, nil
}

// queryScoredCommits reads scored_commits from an already-open tracking DB
// and produces telemetry events with AI contribution percentages per commit.
func queryScoredCommits(ctx context.Context, db *sql.DB, dbPath string, clock core.Clock) ([]shared.TelemetryEvent, error) {
	if clock == nil {
		clock = core.SystemClock{}
	}
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

		occurredAt := clock.Now().UTC()
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
			TokenUsage: core.TokenUsage{
				Requests: core.Int64Ptr(1),
			},
			Status: shared.TelemetryStatusOK,
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

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
