package crush

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mattn/go-sqlite3"
)

// openReadOnly opens a Crush DB at the given path read-only with a busy
// timeout. It deliberately does not use immutable=1: Crush (WAL mode) writes
// to this file while we read it, and immutable tells SQLite the file never
// changes, so it skips locking and ignores the WAL. A read that lands on a
// page mid-write then fails with "database disk image is malformed", and
// rows still in the WAL are invisible. A plain read-only connection reads a
// consistent snapshot and the busy timeout absorbs the writer's short
// exclusive window.
//
// MaxOpenConns is pinned to 1 because the queries we run are short,
// serialized, and a single connection avoids surprise SQLITE_BUSY when
// multiple goroutines in our process race on the same handle.
func openReadOnly(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("crush: empty db path")
	}
	encoded := (&url.URL{Path: dbPath}).EscapedPath()
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", encoded)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("crush: opening db: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func pingContext(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("crush: pinging db: %w", err)
	}
	return nil
}
