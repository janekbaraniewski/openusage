package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// rollupWatermarkKey stores the last fully-rolled day (YYYY-MM-DD, UTC) in
// daemon_meta. Days at or before it are guaranteed represented in
// usage_rollup_daily, which is the safety gate for pruning raw events.
const rollupWatermarkKey = "rollup_daily_watermark"

// MetaGet returns a daemon_meta value and whether it was present.
func (s *Store) MetaGet(ctx context.Context, key string) (string, bool, error) {
	if s == nil || s.db == nil {
		return "", false, nil
	}
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM daemon_meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("telemetry: meta get %q: %w", key, err)
	}
	return v, true, nil
}

// MetaSet upserts a daemon_meta value.
func (s *Store) MetaSet(ctx context.Context, key, value string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO daemon_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("telemetry: meta set %q: %w", key, err)
	}
	return nil
}

// RollupWatermark returns the last fully-rolled day (YYYY-MM-DD), or "" if the
// rollup has never run.
func (s *Store) RollupWatermark(ctx context.Context) (string, error) {
	v, _, err := s.MetaGet(ctx, rollupWatermarkKey)
	return v, err
}

// RollupDaily (re)computes the usage_rollup_daily aggregates and advances the
// watermark. It is incremental and idempotent: it recomputes every day from the
// current watermark through `now` (re-capturing late-arriving events for the
// boundary day) by deleting those days' rollup rows and re-inserting them from
// the deduped raw events, then sets the watermark to the last fully-settled day
// (yesterday, UTC). On first run (no watermark) it backfills all history.
//
// Recompute-and-replace per day is what makes it safe to re-run after a crash
// or alongside re-ingested (deduped) events: the result for a day is a pure
// function of the raw rows for that day.
func (s *Store) RollupDaily(ctx context.Context, now time.Time) (rolledDays int, err error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	now = now.UTC()
	wm, err := s.RollupWatermark(ctx)
	if err != nil {
		return 0, err
	}

	// Scope: recompute from the watermark day forward (inclusive), or everything
	// on first run.
	var sinceTS, deleteFrom string
	if wm == "" {
		sinceTS = "1970-01-01T00:00:00Z"
		deleteFrom = "" // delete all
	} else {
		sinceTS = wm + "T00:00:00Z"
		deleteFrom = wm
	}

	cte, args := dedupedUsageCTEWhere("e.occurred_at >= ?", []any{sinceTS})
	insert := cte + `
		INSERT INTO usage_rollup_daily (
			day, provider_id, account_id, model_canonical, tool_name, project, status,
			input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_write_tokens,
			total_tokens, cost_usd, requests, event_count
		)
		SELECT
			date(occurred_at) AS day,
			provider_id,
			COALESCE(account_id, ''),
			COALESCE(NULLIF(TRIM(model_canonical), ''), NULLIF(TRIM(model_raw), ''), ''),
			COALESCE(tool_name, ''),
			COALESCE(workspace_id, ''),
			COALESCE(status, ''),
			CAST(SUM(COALESCE(input_tokens, 0)) AS INTEGER),
			CAST(SUM(COALESCE(output_tokens, 0)) AS INTEGER),
			CAST(SUM(COALESCE(reasoning_tokens, 0)) AS INTEGER),
			CAST(SUM(COALESCE(cache_read_tokens, 0)) AS INTEGER),
			CAST(SUM(COALESCE(cache_write_tokens, 0)) AS INTEGER),
			CAST(SUM(COALESCE(total_tokens, 0)) AS INTEGER),
			SUM(COALESCE(cost_usd, 0)),
			CAST(SUM(COALESCE(requests, 0)) AS INTEGER),
			COUNT(*)
		FROM deduped_usage
		GROUP BY 1, 2, 3, 4, 5, 6, 7`

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("telemetry: rollup begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if deleteFrom == "" {
		if _, err = tx.ExecContext(ctx, `DELETE FROM usage_rollup_daily`); err != nil {
			return 0, fmt.Errorf("telemetry: rollup clear: %w", err)
		}
	} else {
		if _, err = tx.ExecContext(ctx, `DELETE FROM usage_rollup_daily WHERE day >= ?`, deleteFrom); err != nil {
			return 0, fmt.Errorf("telemetry: rollup clear range: %w", err)
		}
	}

	res, err := tx.ExecContext(ctx, insert, args...)
	if err != nil {
		return 0, fmt.Errorf("telemetry: rollup insert: %w", err)
	}
	n, _ := res.RowsAffected()

	// The last fully-settled day is yesterday (UTC); today may still receive
	// events. Never advance past it.
	newWM := now.AddDate(0, 0, -1).Format("2006-01-02")
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO daemon_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, rollupWatermarkKey, newWM); err != nil {
		return 0, fmt.Errorf("telemetry: rollup watermark: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("telemetry: rollup commit: %w", err)
	}
	return int(n), nil
}
