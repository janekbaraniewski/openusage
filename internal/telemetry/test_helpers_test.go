package telemetry

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func openUsageViewTestStore(t *testing.T) (string, *Store) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return dbPath, store
}

func openUsageViewRawTestStore(t *testing.T) (string, *sql.DB, *Store) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}
	return dbPath, db, store
}

func mustIngestUsageEvent(t *testing.T, store *Store, req IngestRequest, contextLabel string) {
	t.Helper()

	if _, err := store.Ingest(context.Background(), req); err != nil {
		t.Fatalf("%s: %v", contextLabel, err)
	}
}
