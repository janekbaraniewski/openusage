package telemetry

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestWALCheckpoint_TruncatesWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := configureSQLiteConnection(db); err != nil {
		t.Fatalf("configure: %v", err)
	}

	// Create a table and insert data to generate WAL entries.
	if _, err := db.Exec(`CREATE TABLE test_data (id INTEGER PRIMARY KEY, payload TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for i := 0; i < 500; i++ {
		if _, err := db.Exec(`INSERT INTO test_data (payload) VALUES (?)`, "some data for wal growth"); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	// WAL file should exist and have content.
	walPath := dbPath + "-wal"
	walBefore := WALSizeBytes(dbPath)
	if walBefore == 0 {
		// Check if the file exists at all.
		if _, err := os.Stat(walPath); err != nil {
			t.Skipf("WAL file not created (may be in-memory only): %v", err)
		}
	}

	// Checkpoint should succeed.
	if err := WALCheckpoint(context.Background(), db); err != nil {
		t.Fatalf("WALCheckpoint: %v", err)
	}

	// After TRUNCATE checkpoint, WAL should be 0 bytes.
	walAfter := WALSizeBytes(dbPath)
	if walAfter != 0 {
		t.Errorf("WAL size after checkpoint = %d, want 0", walAfter)
	}
}

func TestWALSizeBytes_NonExistentFile(t *testing.T) {
	size := WALSizeBytes("/nonexistent/path/test.db")
	if size != 0 {
		t.Errorf("WALSizeBytes for nonexistent = %d, want 0", size)
	}
}

func TestWALCheckpoint_NilDB(t *testing.T) {
	if err := WALCheckpoint(context.Background(), nil); err != nil {
		t.Errorf("WALCheckpoint(nil) should not error, got: %v", err)
	}
}

func TestQuickIntegrityCheck_HealthyDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	corrupt, detail := quickIntegrityCheck(db)
	if corrupt {
		t.Errorf("healthy DB reported as corrupt: %s", detail)
	}
}

func TestQuickIntegrityCheck_NilDB(t *testing.T) {
	corrupt, _ := quickIntegrityCheck(nil)
	if corrupt {
		t.Error("nil DB should not be reported as corrupt")
	}
}

func TestOpenStore_RecoverFromCorruptDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "telemetry.db")

	// Create a valid DB first.
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("initial OpenStore: %v", err)
	}
	store.Close()

	// Corrupt the DB by overwriting bytes in the middle of the file.
	f, err := os.OpenFile(dbPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open for corruption: %v", err)
	}
	// Write garbage at offset 4096 (second page) to corrupt a B-tree.
	garbage := make([]byte, 4096)
	for i := range garbage {
		garbage[i] = 0xFF
	}
	if _, err := f.WriteAt(garbage, 4096); err != nil {
		f.Close()
		t.Fatalf("write corruption: %v", err)
	}
	f.Close()

	// OpenStore should detect the corruption, back up, and create a fresh DB.
	store2, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore after corruption: %v", err)
	}
	defer store2.Close()

	// The corrupt backup should exist.
	entries, _ := filepath.Glob(filepath.Join(dir, "telemetry.db.corrupt.*"))
	if len(entries) == 0 {
		t.Error("expected a .corrupt backup file to be created")
	}

	// The new DB should be functional.
	if _, err := store2.db.Exec(`SELECT COUNT(*) FROM usage_events`); err != nil {
		t.Errorf("fresh DB should be queryable: %v", err)
	}
}
