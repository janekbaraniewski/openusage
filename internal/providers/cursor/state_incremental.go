package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// Incremental telemetry reads of Cursor's state.vscdb.
//
// cursorDiskKV is a plain rowid table ("key TEXT UNIQUE ON CONFLICT REPLACE,
// value BLOB", no INTEGER PRIMARY KEY, no AUTOINCREMENT). Cursor writes every
// value with INSERT, so an update deletes the old row and inserts a new one at
// max(rowid)+1. Rowid therefore orders rows by last write, and "rowid > mark"
// is exactly the set of rows written since the mark. The values carry no
// usable timestamp (createdAt is missing or in mixed formats, lastUpdatedAt is
// often absent), so rowid is the high-water mark.
//
// SQLite may reuse a rowid only after the rows at the top of the table are
// deleted. To detect that, the mark keeps the (rowid, key) of the top rows at
// the last read ("anchors"). An anchor is valid when the row at its rowid is
// gone or still holds its key, and the key is still at that rowid or above.
// A valid anchor proves max(rowid) never fell below it, so no new row can sit
// at or below it. The read resumes after the highest valid anchor.
//
// A full read runs instead when there is no mark, the file was replaced
// (different device/inode), the file shrank (VACUUM can renumber rowids), no
// anchor is valid, or stateDBFullReadInterval has passed since the last full
// read (a safety net for writes that bypass the rowid order).

const (
	stateDBAnchorCount      = 32
	stateDBFullReadInterval = 24 * time.Hour
	stateDBMarkMetaPrefix   = "cursor.state_db.mark:"
	stateDBComposerKeyChunk = 500
)

type stateDBAnchor struct {
	RowID int64  `json:"rowid"`
	Key   string `json:"key"`
}

// stateDBMark is the high-water mark of one state.vscdb file.
type stateDBMark struct {
	Dev        uint64          `json:"dev,omitempty"`
	Ino        uint64          `json:"ino,omitempty"`
	Size       int64           `json:"size"`
	Anchors    []stateDBAnchor `json:"anchors"` // highest rowid first
	FullReadAt time.Time       `json:"full_read_at"`
}

// SetTelemetryStateStore implements shared.TelemetryStatefulSource.
func (p *Provider) SetTelemetryStateStore(store shared.TelemetryStateStore) {
	p.stateMarkMu.Lock()
	defer p.stateMarkMu.Unlock()
	p.stateStore = store
}

// collectStateDBEventsIncremental returns events for the state DB rows
// written since the last call for this path, or for all rows when a full
// read is needed.
func (p *Provider) collectStateDBEventsIncremental(ctx context.Context, dbPath string) ([]shared.TelemetryEvent, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, nil
	}
	fi, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat cursor state db: %w", err)
	}
	dev, ino, _ := fileIdentity(dbPath, fi)
	now := p.now().UTC()

	db, err := openCursorDBReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// One read transaction: the anchors, the delta and the next mark all
	// come from the same snapshot, so a concurrent Cursor write either lands
	// fully before the mark or fully after it.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin cursor state db read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if !cursorTableExists(ctx, tx, "cursorDiskKV") {
		return nil, nil
	}

	prev := p.stateMark(ctx, dbPath)
	afterRowID := stateDBResumeRowID(ctx, tx, prev, dev, ino, fi.Size(), now)

	events, err := readStateDBEvents(ctx, tx, dbPath, fi.ModTime().UTC(), afterRowID)
	if err != nil {
		return nil, err
	}
	anchors, err := topStateDBAnchors(ctx, tx)
	if err != nil {
		return nil, err
	}

	next := &stateDBMark{Dev: dev, Ino: ino, Size: fi.Size(), Anchors: anchors, FullReadAt: now}
	if afterRowID > 0 {
		next.FullReadAt = prev.FullReadAt
	}
	p.setStateMark(dbPath, next)
	return events, nil
}

// stateDBResumeRowID returns the rowid to resume reading after, or 0 when a
// full read is needed.
func stateDBResumeRowID(ctx context.Context, q sqlQueryer, prev *stateDBMark, dev, ino uint64, size int64, now time.Time) int64 {
	if prev == nil || len(prev.Anchors) == 0 {
		return 0
	}
	if prev.Ino != 0 && ino != 0 && (prev.Dev != dev || prev.Ino != ino) {
		return 0 // file replaced
	}
	if size < prev.Size {
		return 0 // truncated or vacuumed
	}
	if prev.FullReadAt.IsZero() || now.Sub(prev.FullReadAt) >= stateDBFullReadInterval {
		return 0
	}
	for _, anchor := range prev.Anchors {
		if stateDBAnchorValid(ctx, q, anchor) {
			return anchor.RowID
		}
	}
	return 0
}

func stateDBAnchorValid(ctx context.Context, q sqlQueryer, anchor stateDBAnchor) bool {
	if anchor.RowID <= 0 {
		return false
	}
	var keyAt sql.NullString
	err := q.QueryRowContext(ctx, `SELECT key FROM cursorDiskKV WHERE rowid = ?`, anchor.RowID).Scan(&keyAt)
	switch {
	case err == nil:
		// Same key: the row was never touched. Another key: the rowid was reused.
		return keyAt.Valid && keyAt.String == anchor.Key
	case errors.Is(err, sql.ErrNoRows):
		// Rewritten (moved above the anchor) or deleted.
		var at sql.NullInt64
		if err := q.QueryRowContext(ctx, `SELECT MAX(rowid) FROM cursorDiskKV WHERE key = ?`, anchor.Key).Scan(&at); err != nil {
			return false
		}
		return at.Valid && at.Int64 > anchor.RowID
	default:
		return false
	}
}

func topStateDBAnchors(ctx context.Context, q sqlQueryer) ([]stateDBAnchor, error) {
	rows, err := q.QueryContext(ctx, `SELECT rowid, key FROM cursorDiskKV ORDER BY rowid DESC LIMIT ?`, stateDBAnchorCount)
	if err != nil {
		return nil, fmt.Errorf("cursor: querying state db mark: %w", err)
	}
	defer rows.Close()
	var out []stateDBAnchor
	for rows.Next() {
		var (
			rowID int64
			key   sql.NullString
		)
		if err := rows.Scan(&rowID, &key); err != nil {
			return nil, fmt.Errorf("cursor: scan state db mark: %w", err)
		}
		if !key.Valid {
			continue
		}
		out = append(out, stateDBAnchor{RowID: rowID, Key: key.String})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cursor: iterate state db mark: %w", err)
	}
	return out, nil
}

// readStateDBEvents builds telemetry events from the composerData and bubbleId
// rows past afterRowID (all rows when afterRowID <= 0) plus the daily stats in
// ItemTable, which is small and always read in full.
func readStateDBEvents(ctx context.Context, q sqlQueryer, dbPath string, dbMtime time.Time, afterRowID int64) ([]shared.TelemetryEvent, error) {
	composerRecords, err := loadComposerSessionRecordsAfter(ctx, q, afterRowID)
	if err != nil {
		return nil, fmt.Errorf("load cursor composer session records: %w", err)
	}
	bubbleRecords, err := loadBubbleRecordsAfter(ctx, q, afterRowID)
	if err != nil {
		return nil, fmt.Errorf("load cursor bubble records: %w", err)
	}
	sessionTimestamps := composerSessionTimestampMap(composerRecords)
	if afterRowID > 0 {
		// New bubbles can belong to a session whose composerData row was
		// written before the mark. Look those up by key so the bubbles get
		// the same timestamp a full read would give them.
		if err := addComposerTimestampsForBubbles(ctx, q, bubbleRecords, sessionTimestamps); err != nil {
			return nil, fmt.Errorf("load cursor composer timestamps: %w", err)
		}
	}

	var out []shared.TelemetryEvent
	out = append(out, composerEventsFromRecords(composerRecords, dbPath)...)
	out = append(out, toolEventsFromBubbleRecords(bubbleRecords, sessionTimestamps, dbMtime, dbPath)...)
	out = append(out, bubbleTokenEventsFromRecords(bubbleRecords, sessionTimestamps, dbMtime, dbPath)...)

	if cursorTableExists(ctx, q, "ItemTable") {
		dailyEvents, err := collectDailyStatsEvents(ctx, q, dbPath)
		if err != nil {
			return nil, fmt.Errorf("collect cursor daily stats telemetry: %w", err)
		}
		out = append(out, dailyEvents...)
	}
	return out, nil
}

func addComposerTimestampsForBubbles(ctx context.Context, q sqlQueryer, bubbles []cursorBubbleRecord, timestamps map[string]time.Time) error {
	seen := make(map[string]bool)
	var keys []string
	for _, b := range bubbles {
		if b.SessionID == "" || seen[b.SessionID] {
			continue
		}
		seen[b.SessionID] = true
		if _, ok := timestamps[b.SessionID]; ok {
			continue
		}
		keys = append(keys, "composerData:"+b.SessionID)
	}
	for start := 0; start < len(keys); start += stateDBComposerKeyChunk {
		end := min(start+stateDBComposerKeyChunk, len(keys))
		records, err := loadComposerSessionRecordsByKeys(ctx, q, keys[start:end])
		if err != nil {
			return err
		}
		for sid, ts := range composerSessionTimestampMap(records) {
			timestamps[sid] = ts
		}
	}
	return nil
}

// stateMark returns the mark for dbPath from memory, falling back to the
// persisted copy (daemon restart).
func (p *Provider) stateMark(ctx context.Context, dbPath string) *stateDBMark {
	p.stateMarkMu.Lock()
	mark, ok := p.stateMarks[dbPath]
	store := p.stateStore
	p.stateMarkMu.Unlock()
	if ok || store == nil {
		return mark
	}
	raw, found, err := store.MetaGet(ctx, stateDBMarkMetaPrefix+dbPath)
	if err != nil || !found {
		return nil
	}
	var loaded stateDBMark
	if json.Unmarshal([]byte(raw), &loaded) != nil {
		return nil
	}
	return &loaded
}

func (p *Provider) setStateMark(dbPath string, mark *stateDBMark) {
	p.stateMarkMu.Lock()
	defer p.stateMarkMu.Unlock()
	if p.stateMarks == nil {
		p.stateMarks = make(map[string]*stateDBMark)
		p.stateMarkDirty = make(map[string]bool)
	}
	p.stateMarks[dbPath] = mark
	p.stateMarkDirty[dbPath] = true
}

// persistStateMarks writes marks advanced by the previous Collect. It runs at
// the start of the next Collect, after the daemon has ingested (or spooled)
// the previous batch, so a crash in between re-reads that batch instead of
// losing it. Failures leave the mark dirty for the next attempt.
func (p *Provider) persistStateMarks(ctx context.Context) {
	p.stateMarkMu.Lock()
	store := p.stateStore
	pending := make(map[string]*stateDBMark, len(p.stateMarkDirty))
	for path := range p.stateMarkDirty {
		pending[path] = p.stateMarks[path]
	}
	p.stateMarkMu.Unlock()
	if store == nil || len(pending) == 0 {
		return
	}
	for path, mark := range pending {
		raw, err := json.Marshal(mark)
		if err != nil {
			continue
		}
		if store.MetaSet(ctx, stateDBMarkMetaPrefix+path, string(raw)) != nil {
			continue
		}
		p.stateMarkMu.Lock()
		if p.stateMarks[path] == mark {
			delete(p.stateMarkDirty, path)
		}
		p.stateMarkMu.Unlock()
	}
}

// openCursorDBReadOnly opens a Cursor SQLite DB so that nothing can write to
// it. mode=ro opens the file read-only, and _query_only rejects any write
// statement. WAL frames from a running Cursor are still read, at a consistent
// snapshot per transaction. immutable=1 would skip the WAL and locking, which
// is unsafe while Cursor writes, so it is used only when the read-only open
// fails (WAL DB without a -shm file in a directory we cannot write) and
// there is no WAL content to miss.
func openCursorDBReadOnly(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", cursorSQLiteURI(dbPath, "mode=ro&_query_only=1"))
	if err != nil {
		return nil, err
	}
	var version int
	probeErr := db.QueryRow(`PRAGMA schema_version`).Scan(&version)
	if probeErr == nil {
		return db, nil
	}
	_ = db.Close()
	if info, err := os.Stat(dbPath + "-wal"); err == nil && info.Size() > 0 {
		return nil, fmt.Errorf("open cursor db read-only: %w", probeErr)
	}
	db, err = sql.Open("sqlite3", cursorSQLiteURI(dbPath, "mode=ro&immutable=1&_query_only=1"))
	if err != nil {
		return nil, err
	}
	if err := db.QueryRow(`PRAGMA schema_version`).Scan(&version); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open cursor db read-only: %w", err)
	}
	return db, nil
}

// cursorURIPathEscaper escapes only the characters SQLite's URI parser treats
// as syntax, so plain paths (including Windows ones) pass through unchanged.
var cursorURIPathEscaper = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")

// cursorSQLiteURI builds a file: URI for dbPath with the given query string.
func cursorSQLiteURI(dbPath, query string) string {
	return "file:" + cursorURIPathEscaper.Replace(dbPath) + "?" + query
}
