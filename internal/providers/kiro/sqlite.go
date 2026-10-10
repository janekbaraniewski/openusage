package kiro

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mattn/go-sqlite3"
)

// openReadOnly opens data.sqlite3 at the given path read-only with a busy
// timeout. It deliberately does not use immutable=1: Kiro CLI writes to this
// file while we read it, and immutable tells SQLite the file never changes,
// so it skips locking and ignores the WAL. A read that lands on a page
// mid-write then fails with "database disk image is malformed", and rows
// still in the WAL are invisible. A plain read-only connection reads a
// consistent snapshot and the busy timeout absorbs the writer's short
// exclusive window.
//
// MaxOpenConns is pinned to 1: the queries we run are short and serialised,
// and a single connection avoids surprise SQLITE_BUSY when concurrent
// goroutines inside our process race on the same handle.
func openReadOnly(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("kiro: empty db path")
	}

	// The SQLite URI parser needs the path component pre-escaped on its
	// own; url.PathEscape leaves the leading "/" intact which is exactly
	// what `file:` URIs want.
	encoded := (&url.URL{Path: dbPath}).EscapedPath()
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", encoded)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("kiro: opening data.sqlite3: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// pingContext wraps DB.PingContext so callers always get a
// provider-prefixed error.
func pingContext(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("kiro: pinging data.sqlite3: %w", err)
	}
	return nil
}
