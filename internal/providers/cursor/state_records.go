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

	var records []cursorComposerSessionRecord
	for rows.Next() {
		if ctx.Err() != nil {
			return records, ctx.Err()
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

	var records []cursorBubbleRecord
	for rows.Next() {
		if ctx.Err() != nil {
			return records, ctx.Err()
		}

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
