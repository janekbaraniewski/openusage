package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

func configureSQLiteConnection(db *sql.DB) error {
	if db == nil {
		return nil
	}

	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		return fmt.Errorf("set journal_mode WAL: %w", err)
	}
	if _, err := db.Exec(`PRAGMA synchronous = NORMAL;`); err != nil {
		return fmt.Errorf("set synchronous NORMAL: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		return fmt.Errorf("set busy_timeout: %w", err)
	}
	// Cap WAL file size at 64 MB. SQLite will attempt to keep the WAL
	// below this limit by checkpointing more aggressively.
	if _, err := db.Exec(`PRAGMA journal_size_limit = 67108864;`); err != nil {
		return fmt.Errorf("set journal_size_limit: %w", err)
	}
	// Explicit auto-checkpoint threshold (pages). SQLite default is 1000
	// but some drivers reset it; be explicit.
	if _, err := db.Exec(`PRAGMA wal_autocheckpoint = 1000;`); err != nil {
		return fmt.Errorf("set wal_autocheckpoint: %w", err)
	}

	// Keep multiple connections so daemon reads do not stall behind ingest writes.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	return nil
}

// WALCheckpoint runs a TRUNCATE checkpoint, folding the WAL back into the
// quickIntegrityCheck runs PRAGMA quick_check(1) which examines the first
// page of each B-tree. It catches the most common corruption patterns
// (duplicate page refs, free-list errors) in O(tables) time rather than the
// O(rows) full integrity_check. Returns (true, detail) if corruption is found.
func quickIntegrityCheck(db *sql.DB) (corrupt bool, detail string) {
	if db == nil {
		return false, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check(1);`).Scan(&result); err != nil {
		return true, fmt.Sprintf("quick_check failed: %v", err)
	}
	if strings.TrimSpace(strings.ToLower(result)) == "ok" {
		return false, ""
	}
	return true, strings.TrimSpace(result)
}

// main database file and truncating the WAL to zero bytes. It is safe to
// call concurrently — SQLite serialises checkpoint operations internally.
func WALCheckpoint(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE);`)
	return err
}

// WALSizeBytes returns the current size of the WAL file for the given DB path.
// Returns 0 if the file does not exist.
func WALSizeBytes(dbPath string) int64 {
	info, err := os.Stat(dbPath + "-wal")
	if err != nil {
		return 0
	}
	return info.Size()
}

const (
	// walCheckpointInterval is how often the daemon attempts a WAL checkpoint.
	walCheckpointInterval = 60 * time.Second

	// walSizeWarningThreshold is the WAL size at which a warning is logged.
	walSizeWarningThreshold = 128 * 1024 * 1024 // 128 MB

	// walSizeEmergencyThreshold is the WAL size at which an immediate
	// TRUNCATE checkpoint is forced on startup before any queries run.
	walSizeEmergencyThreshold = 512 * 1024 * 1024 // 512 MB
)

// RunWALCheckpointLoop periodically checkpoints the WAL file to prevent
// unbounded growth. This is critical because with multiple open connections
// and continuous reads, SQLite's auto-checkpoint may never find a window to
// run.
func RunWALCheckpointLoop(ctx context.Context, db *sql.DB, dbPath string, logFn func(string, string, string)) {
	if db == nil {
		return
	}
	if logFn == nil {
		logFn = func(_, _, msg string) { log.Printf("[wal] %s", msg) }
	}

	// Emergency checkpoint on startup if WAL is oversized.
	if walSize := WALSizeBytes(dbPath); walSize > walSizeEmergencyThreshold {
		logFn("wal_emergency_checkpoint", "start",
			fmt.Sprintf("WAL is %d MB, forcing emergency checkpoint", walSize/(1024*1024)))
		emergencyCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		if err := WALCheckpoint(emergencyCtx, db); err != nil {
			logFn("wal_emergency_checkpoint", "error", fmt.Sprintf("error=%v", err))
		} else {
			newSize := WALSizeBytes(dbPath)
			logFn("wal_emergency_checkpoint", "done",
				fmt.Sprintf("WAL reduced from %d MB to %d MB", walSize/(1024*1024), newSize/(1024*1024)))
		}
		cancel()
	}

	ticker := time.NewTicker(walCheckpointInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final checkpoint on shutdown.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = WALCheckpoint(shutdownCtx, db)
			cancel()
			return
		case <-ticker.C:
			walSize := WALSizeBytes(dbPath)
			if walSize > walSizeWarningThreshold {
				logFn("wal_checkpoint", "warn",
					fmt.Sprintf("WAL is %d MB, checkpointing", walSize/(1024*1024)))
			}
			ckCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if err := WALCheckpoint(ckCtx, db); err != nil {
				logFn("wal_checkpoint", "error", fmt.Sprintf("error=%v", err))
			}
			cancel()
		}
	}
}
