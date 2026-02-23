package telemetry

import (
	"database/sql"
	"fmt"
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

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return nil
}
