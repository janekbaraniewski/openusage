package opencode

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// opencodeSQLiteWatermarkSlackMS is subtracted from the stored watermark
// before an incremental read. OpenCode stamps time_updated (Unix ms) when it
// builds a row, not when the row commits, so a row from a slower writer can
// land after we read with a stamp just below our watermark. Re-reading a short
// window is cheap and the telemetry dedup absorbs the re-emitted events.
const opencodeSQLiteWatermarkSlackMS = int64(2 * time.Minute / time.Millisecond)

// opencodeSQLiteWatermark is the per-database incremental read state. It is
// kept next to the fingerprint cache on the Provider.
type opencodeSQLiteWatermark struct {
	// file identifies the database file the watermark was taken from. A
	// different file at the same path (OpenCode reinstalled, DB restored or
	// swapped) forces a full read.
	file os.FileInfo
	// maxUpdated is the highest time_updated across message and part seen in
	// the read that produced this watermark.
	maxUpdated int64
}

// opencodeSQLiteReadResult is the outcome of one collection pass.
type opencodeSQLiteReadResult struct {
	Events []shared.TelemetryEvent
	// Watermark is the highest time_updated seen across message and part.
	// Only meaningful when HasWatermark is true.
	Watermark int64
	// HasWatermark is false when the schema cannot support incremental
	// reads (missing table or time_updated column) or the DB does not exist.
	HasWatermark bool
	// Incremental reports whether the pass read only changed messages.
	Incremental bool
}

// collectTelemetryFromSQLiteSince collects OpenCode telemetry from dbPath.
//
// With since == nil it reads the whole database. Otherwise it reads only the
// messages that have a message or part row with time_updated at or after
// *since minus the slack window, and for each of those messages it re-runs the
// full-read logic over all of that message's rows. Because every event depends
// only on its own message's rows, the result for those messages is exactly
// what a full read would produce.
//
// It falls back to a full read when the schema lacks time_updated on message
// or part, or when the newest time_updated is below *since (rows were removed
// or the file was rolled back, so the watermark no longer describes it).
//
// The database is opened read-only and all queries run in one read
// transaction, so the watermark and the events come from the same snapshot.
func collectTelemetryFromSQLiteSince(ctx context.Context, dbPath string, since *int64) (opencodeSQLiteReadResult, error) {
	var res opencodeSQLiteReadResult
	if strings.TrimSpace(dbPath) == "" {
		return res, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, fmt.Errorf("stat opencode sqlite db: %w", err)
	}

	db, err := openOpenCodeSQLiteReadOnly(dbPath)
	if err != nil {
		return res, err
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("begin opencode sqlite read: %w", err)
	}
	// Read-only: nothing to commit.
	defer func() { _ = tx.Rollback() }()

	hasMessageTable, err := sqliteTableExists(ctx, tx, "message")
	if err != nil {
		return res, err
	}
	if !hasMessageTable {
		return res, nil
	}
	hasPartTable, err := sqliteTableExists(ctx, tx, "part")
	if err != nil {
		return res, err
	}

	watermarkable, err := opencodeSQLiteHasWatermarkColumns(ctx, tx, hasPartTable)
	if err != nil {
		return res, err
	}
	if watermarkable {
		res.Watermark, err = opencodeSQLiteMaxUpdated(ctx, tx, hasPartTable)
		if err != nil {
			return res, err
		}
		res.HasWatermark = true
	}

	var scope *messageScope
	if since != nil && watermarkable && res.Watermark >= *since {
		ids, err := opencodeSQLiteChangedMessageIDs(ctx, tx, hasPartTable, *since-opencodeSQLiteWatermarkSlackMS)
		if err != nil {
			return res, err
		}
		res.Incremental = true
		if len(ids) == 0 {
			return res, nil
		}
		if scope, err = newMessageScope(ids); err != nil {
			return res, err
		}
	}

	res.Events, err = collectSQLiteEvents(ctx, tx, dbPath, hasPartTable, scope)
	return res, err
}

// opencodeSQLiteHasWatermarkColumns reports whether message (and part, when
// present) carry the time_updated column the watermark is built on.
func opencodeSQLiteHasWatermarkColumns(ctx context.Context, q sqliteQuerier, hasPartTable bool) (bool, error) {
	tables := []string{"message"}
	if hasPartTable {
		tables = append(tables, "part")
	}
	for _, table := range tables {
		var n int
		err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'time_updated'`, table).Scan(&n)
		if err != nil {
			return false, fmt.Errorf("inspect opencode sqlite %s columns: %w", table, err)
		}
		if n == 0 {
			return false, nil
		}
	}
	return true, nil
}

// opencodeSQLiteMaxUpdated returns the newest time_updated across message and
// part. The scan reads only the row headers, never the large data column.
func opencodeSQLiteMaxUpdated(ctx context.Context, q sqliteQuerier, hasPartTable bool) (int64, error) {
	query := `SELECT COALESCE(MAX(time_updated), 0) FROM message`
	if hasPartTable {
		query = `SELECT MAX(
			(SELECT COALESCE(MAX(time_updated), 0) FROM message),
			(SELECT COALESCE(MAX(time_updated), 0) FROM part)
		)`
	}
	var maxUpdated sql.NullInt64
	if err := q.QueryRowContext(ctx, query).Scan(&maxUpdated); err != nil {
		return 0, fmt.Errorf("query opencode sqlite watermark: %w", err)
	}
	return maxUpdated.Int64, nil
}

// opencodeSQLiteChangedMessageIDs returns the IDs of messages with a message
// or part row updated at or after from.
func opencodeSQLiteChangedMessageIDs(ctx context.Context, q sqliteQuerier, hasPartTable bool, from int64) ([]string, error) {
	query := `SELECT id FROM message WHERE time_updated >= ?`
	args := []any{from}
	if hasPartTable {
		query += ` UNION SELECT message_id FROM part WHERE time_updated >= ?`
		args = append(args, from)
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query opencode sqlite changed messages: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id sql.NullString
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan opencode sqlite changed message: %w", err)
		}
		if id.Valid && id.String != "" {
			ids = append(ids, id.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate opencode sqlite changed messages: %w", err)
	}
	return ids, nil
}

// sqliteWatermarkSince returns the stored watermark for cacheKey if it still
// describes the file at dbPath, or nil to request a full read.
func (p *Provider) sqliteWatermarkSince(cacheKey string, current os.FileInfo) *int64 {
	p.telemetryCacheMu.Lock()
	defer p.telemetryCacheMu.Unlock()
	wm, ok := p.telemetryDBWatermarks[cacheKey]
	if !ok || current == nil || wm.file == nil || !os.SameFile(wm.file, current) {
		return nil
	}
	since := wm.maxUpdated
	return &since
}

// storeSQLiteWatermark records the outcome of a read. A result without a
// watermark clears the entry so the next read is a full one.
func (p *Provider) storeSQLiteWatermark(cacheKey string, file os.FileInfo, res opencodeSQLiteReadResult) {
	p.telemetryCacheMu.Lock()
	defer p.telemetryCacheMu.Unlock()
	if !res.HasWatermark || file == nil {
		delete(p.telemetryDBWatermarks, cacheKey)
		return
	}
	if p.telemetryDBWatermarks == nil {
		p.telemetryDBWatermarks = make(map[string]opencodeSQLiteWatermark)
	}
	p.telemetryDBWatermarks[cacheKey] = opencodeSQLiteWatermark{file: file, maxUpdated: res.Watermark}
}
