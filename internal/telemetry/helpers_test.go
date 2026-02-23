package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func int64Ptr(v int64) *int64 {
	vv := v
	return &vv
}

func applyCanonicalUsageViewForTest(ctx context.Context, dbPath string, snaps map[string]core.UsageSnapshot) (map[string]core.UsageSnapshot, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return snaps, nil
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return snaps, fmt.Errorf("open canonical usage db: %w", err)
	}
	defer db.Close()
	if err := configureSQLiteConnection(db); err != nil {
		return snaps, fmt.Errorf("configure canonical usage db: %w", err)
	}
	return applyCanonicalUsageViewWithDB(ctx, db, snaps, nil)
}

func applyCanonicalTelemetryViewForTest(ctx context.Context, dbPath string, snaps map[string]core.UsageSnapshot) (map[string]core.UsageSnapshot, error) {
	return ApplyCanonicalTelemetryViewWithOptions(ctx, dbPath, snaps, ReadModelOptions{})
}
