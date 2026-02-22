package telemetry

import (
	"context"
	"fmt"
)

type StoreStats struct {
	RawEvents             int64
	CanonicalEvents       int64
	ReconciliationWindows int64
}

func (s *Store) Stats(ctx context.Context) (StoreStats, error) {
	if s == nil || s.db == nil {
		return StoreStats{}, fmt.Errorf("telemetry: store not initialized")
	}
	stats := StoreStats{}

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_raw_events`).Scan(&stats.RawEvents); err != nil {
		return StoreStats{}, fmt.Errorf("telemetry: count usage_raw_events: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events`).Scan(&stats.CanonicalEvents); err != nil {
		return StoreStats{}, fmt.Errorf("telemetry: count usage_events: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_reconciliation_windows`).Scan(&stats.ReconciliationWindows); err != nil {
		return StoreStats{}, fmt.Errorf("telemetry: count usage_reconciliation_windows: %w", err)
	}
	return stats, nil
}
