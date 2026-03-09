package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

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
	tempTable := "_deduped_tmp"

	matStart := time.Now()
	_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTable))
	materializeSQL := fmt.Sprintf("CREATE TEMP TABLE %s AS %s SELECT * FROM deduped_usage", tempTable, usageCTE)
	if _, err := db.ExecContext(ctx, materializeSQL, whereArgs...); err != nil {
		return usageFilter{}, nil, fmt.Errorf("materialize deduped usage: %w", err)
	}
	core.Tracef("[usage_view_perf] materialize temp table: %dms (providers=%v, windowHours=%d)",
		time.Since(matStart).Milliseconds(), filter.ProviderIDs, filter.TimeWindowHours)

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
