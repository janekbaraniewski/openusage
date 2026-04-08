package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

type cursorComposerSessionRecord struct {
	Key               string
	SessionID         string
	OccurredAt        time.Time
	Usage             map[string]composerModelUsage
	Mode              string
	ForceMode         string
	IsAgentic         *bool
	LinesAdded        int
	LinesRemoved      int
	ModelConfigName   string
	NewlyCreatedFiles int
	AddedFiles        int
	RemovedFiles      int
	ContextTokensUsed float64
	ContextTokenLimit float64
	FilesChanged      int
	SubagentType      string
	Status            string
}

type cursorBubbleRecord struct {
	Key          string
	BubbleID     string
	SessionID    string
	ToolName     string
	ToolStatus   string
	Model        string
	InputTokens  int64
	OutputTokens int64
}

// loadComposerSessionKeys returns just the keys for composerData entries that
// have non-empty usageData. This is a cheap query (no json_extract on value
// payload beyond the filter) used to detect new sessions before doing the
// expensive full extraction.
func loadComposerSessionKeys(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT key FROM cursorDiskKV
		WHERE key LIKE 'composerData:%'
		  AND json_extract(value, '$.usageData') IS NOT NULL
		  AND json_extract(value, '$.usageData') != '{}'`)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying composerData keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if rows.Scan(&key) == nil {
			keys = append(keys, key)
		}
	}
	return keys, rows.Err()
}

// loadComposerSessionRecordsByKeys loads composer session records for the given keys only.
// This performs the expensive json_extract query but scoped to a specific key set.
func loadComposerSessionRecordsByKeys(ctx context.Context, db *sql.DB, keys []string) ([]cursorComposerSessionRecord, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(keys))
	args := make([]interface{}, len(keys))
	for i, key := range keys {
		placeholders[i] = "?"
		args[i] = key
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
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
		       json_extract(value, '$.filesChangedCount'),
		       json_extract(value, '$.subagentInfo.subagentTypeName'),
		       json_extract(value, '$.status')
		FROM cursorDiskKV
		WHERE key IN (%s)`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying composerData by keys: %w", err)
	}
	defer rows.Close()

	return scanComposerSessionRows(rows)
}

func loadComposerSessionRecords(ctx context.Context, db *sql.DB) ([]cursorComposerSessionRecord, error) {
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
		       json_extract(value, '$.filesChangedCount'),
		       json_extract(value, '$.subagentInfo.subagentTypeName'),
		       json_extract(value, '$.status')
		FROM cursorDiskKV
		WHERE key LIKE 'composerData:%'
		  AND json_extract(value, '$.usageData') IS NOT NULL
		  AND json_extract(value, '$.usageData') != '{}'`)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying composerData: %w", err)
	}
	defer rows.Close()

	return scanComposerSessionRows(rows)
}

// scanComposerSessionRows scans composer session rows from any query that
// returns the same 17-column shape used by loadComposerSessionRecords and
// loadComposerSessionRecordsByKeys.
func scanComposerSessionRows(rows *sql.Rows) ([]cursorComposerSessionRecord, error) {
	var records []cursorComposerSessionRecord
	for rows.Next() {
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
			ctxTokensUsed   sql.NullFloat64
			ctxTokenLimit   sql.NullFloat64
			filesChangedCnt sql.NullInt64
			subagentType    sql.NullString
			status          sql.NullString
		)
		if err := rows.Scan(&key, &usageJSON, &createdAt, &mode, &forceMode, &isAgentic,
			&linesAdded, &linesRemoved, &modelConfigName, &newlyCreated, &addedFiles, &removedFiles,
			&ctxTokensUsed, &ctxTokenLimit, &filesChangedCnt, &subagentType, &status); err != nil {
			continue
		}
		if !usageJSON.Valid || usageJSON.String == "" || usageJSON.String == "{}" {
			continue
		}

		var usage map[string]composerModelUsage
		if json.Unmarshal([]byte(usageJSON.String), &usage) != nil {
			continue
		}

		record := cursorComposerSessionRecord{
			Key:               key,
			SessionID:         strings.TrimPrefix(key, "composerData:"),
			Usage:             usage,
			Mode:              nullableString(mode),
			ForceMode:         nullableString(forceMode),
			LinesAdded:        nullableInt(linesAdded),
			LinesRemoved:      nullableInt(linesRemoved),
			ModelConfigName:   nullableString(modelConfigName),
			NewlyCreatedFiles: countJSONArrayItems(newlyCreated),
			AddedFiles:        countNullableInt(addedFiles),
			RemovedFiles:      countNullableInt(removedFiles),
			ContextTokensUsed: nullableFloat(ctxTokensUsed),
			ContextTokenLimit: nullableFloat(ctxTokenLimit),
			FilesChanged:      nullableInt(filesChangedCnt),
			SubagentType:      nullableString(subagentType),
			Status:            nullableString(status),
		}
		if createdAt.Valid && createdAt.Int64 > 0 {
			record.OccurredAt = shared.UnixAuto(createdAt.Int64)
		}
		if isAgentic.Valid {
			value := isAgentic.Bool
			record.IsAgentic = &value
		}

		records = append(records, record)
	}

	return records, rows.Err()
}

// loadBubbleKeys returns just the keys for bubbleId entries with type=2.
// This is a cheaper query than the full json_extract used by loadBubbleRecords,
// used to detect new bubble records before doing the expensive full extraction.
func loadBubbleKeys(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT key FROM cursorDiskKV
		WHERE key LIKE 'bubbleId:%'
		  AND json_extract(value, '$.type') = 2`)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying bubbleId keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if rows.Scan(&key) == nil {
			keys = append(keys, key)
		}
	}
	return keys, rows.Err()
}

// loadBubbleRecordsByKeys loads bubble records for the given keys only.
func loadBubbleRecordsByKeys(ctx context.Context, db *sql.DB, keys []string) ([]cursorBubbleRecord, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(keys))
	args := make([]interface{}, len(keys))
	for i, key := range keys {
		placeholders[i] = "?"
		args[i] = key
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT key,
		       json_extract(value, '$.toolFormerData.name'),
		       json_extract(value, '$.toolFormerData.status'),
		       json_extract(value, '$.conversationId'),
		       json_extract(value, '$.tokenCount.inputTokens'),
		       json_extract(value, '$.tokenCount.outputTokens'),
		       json_extract(value, '$.model')
		FROM cursorDiskKV
		WHERE key IN (%s)`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying bubbleId by keys: %w", err)
	}
	defer rows.Close()

	return scanBubbleRows(rows)
}

func loadBubbleRecords(ctx context.Context, db *sql.DB) ([]cursorBubbleRecord, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT key,
		       json_extract(value, '$.toolFormerData.name'),
		       json_extract(value, '$.toolFormerData.status'),
		       json_extract(value, '$.conversationId'),
		       json_extract(value, '$.tokenCount.inputTokens'),
		       json_extract(value, '$.tokenCount.outputTokens'),
		       json_extract(value, '$.model')
		FROM cursorDiskKV
		WHERE key LIKE 'bubbleId:%'
		  AND json_extract(value, '$.type') = 2`)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying bubbleId records: %w", err)
	}
	defer rows.Close()

	return scanBubbleRows(rows)
}

// scanBubbleRows scans bubble record rows from any query that returns the same
// 7-column shape used by loadBubbleRecords and loadBubbleRecordsByKeys.
func scanBubbleRows(rows *sql.Rows) ([]cursorBubbleRecord, error) {
	var records []cursorBubbleRecord
	for rows.Next() {
		var (
			key            string
			toolName       sql.NullString
			toolStatus     sql.NullString
			conversationID sql.NullString
			inputTokens    sql.NullInt64
			outputTokens   sql.NullInt64
			model          sql.NullString
		)
		if err := rows.Scan(&key, &toolName, &toolStatus, &conversationID, &inputTokens, &outputTokens, &model); err != nil {
			continue
		}

		records = append(records, cursorBubbleRecord{
			Key:          key,
			BubbleID:     strings.TrimPrefix(key, "bubbleId:"),
			SessionID:    nullableString(conversationID),
			ToolName:     nullableString(toolName),
			ToolStatus:   nullableString(toolStatus),
			Model:        nullableString(model),
			InputTokens:  nullableInt64(inputTokens),
			OutputTokens: nullableInt64(outputTokens),
		})
	}

	return records, rows.Err()
}

func composerSessionTimestampMap(records []cursorComposerSessionRecord) map[string]time.Time {
	out := make(map[string]time.Time, len(records))
	for _, record := range records {
		if record.SessionID == "" || record.OccurredAt.IsZero() {
			continue
		}
		out[record.SessionID] = record.OccurredAt
	}
	return out
}

func nullableString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nullableInt(value sql.NullInt64) int {
	if !value.Valid {
		return 0
	}
	return int(value.Int64)
}

func nullableInt64(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func nullableFloat(value sql.NullFloat64) float64 {
	if !value.Valid {
		return 0
	}
	return value.Float64
}

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
