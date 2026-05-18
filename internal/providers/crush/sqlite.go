package crush

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mattn/go-sqlite3"
)

// openReadOnly opens a Crush DB at the given path using SQLite's
// read-only, immutable file URI. Immutable mode is critical here:
// Crush itself may be writing to this DB (it opens with WAL +
// `busy_timeout=30000` per `internal/db/connect.go` upstream), and
// taking a shared lock would race with that. Immutable mode tells
// SQLite to skip locking entirely; we trust that we can read a
// momentarily-stale snapshot.
//
// MaxOpenConns is pinned to 1 because the queries we run are short,
// serialized, and a single connection avoids surprise SQLITE_BUSY when
// multiple goroutines in our process race on the same handle.
func openReadOnly(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("crush: empty db path")
	}
	encoded := (&url.URL{Path: dbPath}).EscapedPath()
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", encoded)

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
