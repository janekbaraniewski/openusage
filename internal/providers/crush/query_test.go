package crush

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestQuerySessions_TimestampsAreUnixSeconds pins that sessions.created_at
// and sessions.updated_at are Unix SECONDS, not milliseconds.
//
// Crush's initial migration comment claims milliseconds, but the values
// written are seconds: the update_sessions_updated_at trigger stores
// strftime('%s','now') and the Crush CLI renders time.Unix(CreatedAt, 0)
// (see charmbracelet/crush internal/db/migrations and internal/session).
// 1784269138 is a real captured value: 2026-07-17 as seconds, but
// 1970-01-21 if misread as milliseconds — which is exactly how this bug
// shipped (sessions silently bucketed into January 1970).
func TestQuerySessions_TimestampsAreUnixSeconds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crush.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	defer db.Close()

	schema := `
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			parent_session_id TEXT,
			message_count INTEGER DEFAULT 0,
			prompt_tokens INTEGER DEFAULT 0,
			completion_tokens INTEGER DEFAULT 0,
			cost REAL DEFAULT 0,
			created_at INTEGER,
			updated_at INTEGER
		);
		CREATE TABLE messages (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			role TEXT,
			model TEXT,
			created_at INTEGER
		);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO sessions (id, message_count, prompt_tokens, completion_tokens, cost, created_at, updated_at)
		 VALUES ('sess-1', 3, 100, 200, 0.5, ?, ?)`,
		int64(1784269138), int64(1784269500),
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	sessions, err := querySessions(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("querySessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}

	want := time.Unix(1784269138, 0).UTC()
	got := sessions[0].CreatedAt
	if !got.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", got, want)
	}
	if got.Year() < 2020 {
		t.Errorf("CreatedAt year = %d, dates landed before 2020 (millisecond misread?); got %v", got.Year(), got)
	}
	if got.UTC().Format(time.DateOnly) != "2026-07-17" {
		t.Errorf("CreatedAt date = %s, want 2026-07-17", got.UTC().Format(time.DateOnly))
	}

	wantUpdated := time.Unix(1784269500, 0).UTC()
	if !sessions[0].UpdatedAt.Equal(wantUpdated) {
		t.Errorf("UpdatedAt = %v, want %v", sessions[0].UpdatedAt, wantUpdated)
	}
}
