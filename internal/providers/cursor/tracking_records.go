package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

type cursorTrackingRecord struct {
	Source      string
	Model       string
	FileExt     string
	FileName    string
	RequestID   string
	SessionID   string
	OccurredAt  time.Time
	OccurredDay string
	RowID       int64
}

type cursorDailyStatsRecord struct {
	Date  string
	Stats dailyStats
	Key   string
}

func loadTrackingRecords(ctx context.Context, db *sql.DB, clock core.Clock) ([]cursorTrackingRecord, error) {
	return loadTrackingRecordsIncremental(ctx, db, clock, 0)
}

// trackingMaxRowID returns the maximum rowid in ai_code_hashes, or 0 if the table is empty.
func trackingMaxRowID(ctx context.Context, db *sql.DB) (int64, error) {
	var maxID int64
	err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(rowid), 0) FROM ai_code_hashes").Scan(&maxID)
	return maxID, err
}

// loadTrackingRecordsIncremental loads tracking records with rowid > afterRowID.
// Pass afterRowID=0 to load all records.
func loadTrackingRecordsIncremental(ctx context.Context, db *sql.DB, clock core.Clock, afterRowID int64) ([]cursorTrackingRecord, error) {
	if clock == nil {
		clock = core.SystemClock{}
	}
	columns := cursorTableColumns(ctx, db, "ai_code_hashes")
	timeExpr := chooseTrackingTimeExpr(ctx, db)

	var whereClause string
	var args []interface{}
	if afterRowID > 0 {
		whereClause = "WHERE rowid > ?"
		args = append(args, afterRowID)
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       COALESCE(%s, 0),
		       rowid
		FROM ai_code_hashes
		%s
		ORDER BY %s ASC`,
		cursorTrackingTextColumnExpr(columns, "source"),
		cursorTrackingTextColumnExpr(columns, "model"),
		cursorTrackingTextColumnExpr(columns, "fileExtension"),
		cursorTrackingTextColumnExpr(columns, "fileName"),
		cursorTrackingTextColumnExpr(columns, "requestId"),
		cursorTrackingTextColumnExpr(columns, "conversationId"),
		timeExpr,
		whereClause,
		timeExpr), args...)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying ai_code_hashes: %w", err)
	}
	defer rows.Close()

	var records []cursorTrackingRecord
	for rows.Next() {
		if ctx.Err() != nil {
			return records, ctx.Err()
		}

		var (
			record    cursorTrackingRecord
			timestamp int64
		)
		if err := rows.Scan(
			&record.Source,
			&record.Model,
			&record.FileExt,
			&record.FileName,
			&record.RequestID,
			&record.SessionID,
			&timestamp,
			&record.RowID,
		); err != nil {
			continue
		}

		record.OccurredAt = clock.Now().UTC()
		if timestamp > 0 {
			record.OccurredAt = shared.UnixAuto(timestamp)
		}
		record.OccurredDay = record.OccurredAt.Local().Format("2006-01-02")
		records = append(records, record)
	}

	return records, rows.Err()
}

func cursorTrackingTextColumnExpr(columns map[string]bool, name string) string {
	if columns[strings.ToLower(strings.TrimSpace(name))] {
		return fmt.Sprintf("COALESCE(%s, '')", name)
	}
	return "''"
}

func cursorTableColumns(ctx context.Context, db *sql.DB, table string) map[string]bool {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, strings.TrimSpace(table)))
	if err != nil {
		return nil
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var (
			cid       int
			name      string
			dataType  string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if rows.Scan(&cid, &name, &dataType, &notNull, &dfltValue, &pk) != nil {
			continue
		}
		columns[strings.ToLower(strings.TrimSpace(name))] = true
	}
	return columns
}

func loadDailyStatsRecords(ctx context.Context, db *sql.DB) ([]cursorDailyStatsRecord, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT key, value FROM ItemTable
		WHERE key LIKE 'aiCodeTracking.dailyStats.%'
		ORDER BY key ASC`)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying dailyStats: %w", err)
	}
	defer rows.Close()

	const prefix = "aiCodeTracking.dailyStats.v1.5."
	var records []cursorDailyStatsRecord
	for rows.Next() {
		if ctx.Err() != nil {
			return records, ctx.Err()
		}

		var key string
		var rawJSON string
		if err := rows.Scan(&key, &rawJSON); err != nil {
			continue
		}

		dateStr := strings.TrimPrefix(key, prefix)
		if len(dateStr) != 10 {
			continue
		}

		var stats dailyStats
		if json.Unmarshal([]byte(rawJSON), &stats) != nil {
			continue
		}
		if stats.Date == "" {
			stats.Date = dateStr
		}

		records = append(records, cursorDailyStatsRecord{
			Date:  dateStr,
			Stats: stats,
			Key:   key,
		})
	}

	return records, rows.Err()
}
