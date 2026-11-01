package cursor

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// TestCollectSkipsUnchangedDBs locks in the power fix: the daemon calls
// Collect every interval, and re-reading an unchanged (multi-GB) Cursor DB
// re-emits the same events for nothing. An unchanged DB must yield no events;
// a write must make it collect again.
func TestCollectSkipsUnchangedDBs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE ai_code_hashes (hash TEXT PRIMARY KEY, source TEXT, createdAt INTEGER, model TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ai_code_hashes VALUES ('h1', 'composer', ?, 'claude')`, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	p := New()
	opts := shared.TelemetryCollectOptions{Paths: map[string]string{
		"tracking_db": dbPath,
		"state_db":    filepath.Join(t.TempDir(), "missing.vscdb"),
	}}
	ctx := context.Background()

	first, err := p.Collect(ctx, opts)
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first collect: got %d events, want 1", len(first))
	}

	second, err := p.Collect(ctx, opts)
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("unchanged DB: got %d events, want 0 (should be skipped)", len(second))
	}

	// Ensure the mtime moves even on coarse-grained filesystems.
	time.Sleep(10 * time.Millisecond)
	if _, err := db.Exec(`INSERT INTO ai_code_hashes VALUES ('h2', 'tab', ?, 'gpt-4o')`, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	third, err := p.Collect(ctx, opts)
	if err != nil {
		t.Fatalf("third collect: %v", err)
	}
	if len(third) != 2 {
		t.Fatalf("changed DB: got %d events, want 2 (full re-read; store dedups)", len(third))
	}
}
