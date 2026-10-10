package hermes

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mattn/go-sqlite3"
)

// openReadOnly opens the Hermes state.db read-only with a busy timeout. It
// deliberately does not use immutable=1: Hermes writes to this file while we
// read it, and immutable tells SQLite the file never changes, so it skips
// locking and ignores the WAL. A read that lands on a page mid-write then
// fails with "database disk image is malformed", and rows still in the WAL
// are invisible. A plain read-only connection reads a consistent snapshot
// and the busy timeout absorbs the writer's short exclusive window.
//
// MaxOpenConns is capped at 1 because the queries we run are short and
// serialized.
func openReadOnly(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("hermes: empty db path")
	}
	encoded := (&url.URL{Path: dbPath}).EscapedPath()
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", encoded)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("hermes: opening state db: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func pingContext(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("hermes: pinging state db: %w", err)
	}
	return nil
}
