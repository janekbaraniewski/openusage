package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// materializedTableName is the fixed temp table name used for materialized
// deduped usage queries. Defined as a constant so it cannot be changed at
// runtime, eliminating SQL injection risk from table-name interpolation.
const materializedTableName = "_deduped_tmp"

// validTableNameRE matches only lowercase ASCII letters and underscores.
var validTableNameRE = regexp.MustCompile(`^[a-z_]+$`)

// allowedMaterializedTables is the set of table names that may be interpolated
// into SQL queries. Any name not in this set is rejected.
var allowedMaterializedTables = map[string]bool{
	materializedTableName: true,
}

// validateMaterializedTable ensures name is safe for SQL interpolation.
// It must match the allowed character pattern and appear in the allowlist.
func validateMaterializedTable(name string) error {
	if !validTableNameRE.MatchString(name) {
		return fmt.Errorf("invalid materialized table name %q: must match [a-z_]+", name)
	}
	if !allowedMaterializedTables[name] {
		return fmt.Errorf("disallowed materialized table name %q", name)
	}
	return nil
}

func newTelemetryUsageAgg() *telemetryUsageAgg {
	return &telemetryUsageAgg{
		ModelDaily:   make(map[string][]core.TimePoint),
		SourceDaily:  make(map[string][]core.TimePoint),
		ProjectDaily: make(map[string][]core.TimePoint),
		ClientDaily:  make(map[string][]core.TimePoint),
		ClientTokens: make(map[string][]core.TimePoint),
	}
}

func materializeUsageFilter(ctx context.Context, db *sql.DB, filter usageFilter) (usageFilter, func(), error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	tempTable := materializedTableName

	if err := validateMaterializedTable(tempTable); err != nil {
		return usageFilter{}, nil, fmt.Errorf("materialize: %w", err)
	}

	matStart := time.Now()
	_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTable))
	materializeSQL := fmt.Sprintf("CREATE TEMP TABLE %s AS %s SELECT * FROM deduped_usage", tempTable, usageCTE)
	if _, err := db.ExecContext(ctx, materializeSQL, whereArgs...); err != nil {
		return usageFilter{}, nil, fmt.Errorf("materialize deduped usage: %w", err)
	}
	core.Tracef("[usage_view_perf] materialize temp table: %dms (providers=%v, since=%s)",
		time.Since(matStart).Milliseconds(), filter.ProviderIDs, filter.Since.Format(time.RFC3339))

	_, _ = db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_deduped_event_status ON %s(event_type, status)", tempTable))
	_, _ = db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_deduped_occurred ON %s(occurred_at)", tempTable))

	filter.materializedTbl = tempTable
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_, _ = db.ExecContext(cleanupCtx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTable))
	}
	return filter, cleanup, nil
}
