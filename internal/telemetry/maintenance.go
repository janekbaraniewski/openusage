package telemetry

import (
	"context"
	"database/sql"
	"fmt"
)

type CompactionResult struct {
	DuplicateEventsRemoved int64
	OrphanRawEventsRemoved int64
}

// CompactUsage removes legacy duplicate canonical usage rows and unreferenced raw rows.
// It keeps the highest-quality row per logical event, preferring richer payloads and hook channels.
func (s *Store) CompactUsage(ctx context.Context) (CompactionResult, error) {
	if s == nil || s.db == nil {
		return CompactionResult{}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CompactionResult{}, fmt.Errorf("telemetry: compact begin tx: %w", err)
	}
	defer tx.Rollback()

	dupCount, err := countDuplicateUsageRows(ctx, tx)
	if err != nil {
		return CompactionResult{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM usage_events
		WHERE event_id IN (
			WITH ranked AS (
				SELECT
					e.event_id,
					ROW_NUMBER() OVER (
						PARTITION BY
							LOWER(TRIM(COALESCE(r.source_system, ''))),
							LOWER(TRIM(COALESCE(e.event_type, ''))),
							LOWER(TRIM(COALESCE(e.session_id, ''))),
							CASE
								WHEN COALESCE(NULLIF(TRIM(e.tool_call_id), ''), '') != '' THEN 'tool:' || LOWER(TRIM(e.tool_call_id))
								WHEN COALESCE(NULLIF(TRIM(e.message_id), ''), '') != '' THEN 'message:' || LOWER(TRIM(e.message_id))
								WHEN COALESCE(NULLIF(TRIM(e.turn_id), ''), '') != '' THEN 'turn:' || LOWER(TRIM(e.turn_id))
								ELSE 'fallback:' || e.dedup_key
							END
						ORDER BY
							CASE COALESCE(NULLIF(TRIM(r.source_channel), ''), '')
								WHEN 'hook' THEN 4
								WHEN 'sse' THEN 3
								WHEN 'sqlite' THEN 2
								WHEN 'jsonl' THEN 2
								WHEN 'api' THEN 1
								ELSE 0
							END DESC,
							(
								CASE WHEN COALESCE(e.total_tokens, 0) > 0 THEN 4 ELSE 0 END +
								CASE WHEN COALESCE(e.cost_usd, 0) > 0 THEN 2 ELSE 0 END +
								CASE WHEN COALESCE(NULLIF(TRIM(COALESCE(e.model_canonical, e.model_raw)), ''), '') != '' THEN 1 ELSE 0 END +
								CASE
									WHEN COALESCE(NULLIF(TRIM(e.provider_id), ''), '') != ''
										AND LOWER(TRIM(e.provider_id)) NOT IN ('unknown', 'opencode')
									THEN 1
									ELSE 0
								END
							) DESC,
							e.occurred_at DESC,
							e.event_id DESC
					) AS rn
				FROM usage_events e
				JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
				WHERE e.event_type IN ('message_usage', 'tool_usage')
			)
			SELECT event_id FROM ranked WHERE rn > 1
		)
	`); err != nil {
		return CompactionResult{}, fmt.Errorf("telemetry: compact delete duplicate usage rows: %w", err)
	}

	orphanResult, err := tx.ExecContext(ctx, `
		DELETE FROM usage_raw_events
		WHERE raw_event_id NOT IN (SELECT DISTINCT raw_event_id FROM usage_events)
	`)
	if err != nil {
		return CompactionResult{}, fmt.Errorf("telemetry: compact delete orphan raw rows: %w", err)
	}
	orphanCount, _ := orphanResult.RowsAffected()

	if err := tx.Commit(); err != nil {
		return CompactionResult{}, fmt.Errorf("telemetry: compact commit tx: %w", err)
	}

	return CompactionResult{
		DuplicateEventsRemoved: dupCount,
		OrphanRawEventsRemoved: orphanCount,
	}, nil
}

func countDuplicateUsageRows(ctx context.Context, tx *sql.Tx) (int64, error) {
	var count int64
	err := tx.QueryRowContext(ctx, `
		WITH ranked AS (
			SELECT
				e.event_id,
				ROW_NUMBER() OVER (
					PARTITION BY
						LOWER(TRIM(COALESCE(r.source_system, ''))),
						LOWER(TRIM(COALESCE(e.event_type, ''))),
						LOWER(TRIM(COALESCE(e.session_id, ''))),
						CASE
							WHEN COALESCE(NULLIF(TRIM(e.tool_call_id), ''), '') != '' THEN 'tool:' || LOWER(TRIM(e.tool_call_id))
							WHEN COALESCE(NULLIF(TRIM(e.message_id), ''), '') != '' THEN 'message:' || LOWER(TRIM(e.message_id))
							WHEN COALESCE(NULLIF(TRIM(e.turn_id), ''), '') != '' THEN 'turn:' || LOWER(TRIM(e.turn_id))
							ELSE 'fallback:' || e.dedup_key
						END
					ORDER BY
						CASE COALESCE(NULLIF(TRIM(r.source_channel), ''), '')
							WHEN 'hook' THEN 4
							WHEN 'sse' THEN 3
							WHEN 'sqlite' THEN 2
							WHEN 'jsonl' THEN 2
							WHEN 'api' THEN 1
							ELSE 0
						END DESC,
						(
							CASE WHEN COALESCE(e.total_tokens, 0) > 0 THEN 4 ELSE 0 END +
							CASE WHEN COALESCE(e.cost_usd, 0) > 0 THEN 2 ELSE 0 END +
							CASE WHEN COALESCE(NULLIF(TRIM(COALESCE(e.model_canonical, e.model_raw)), ''), '') != '' THEN 1 ELSE 0 END +
							CASE
								WHEN COALESCE(NULLIF(TRIM(e.provider_id), ''), '') != ''
									AND LOWER(TRIM(e.provider_id)) NOT IN ('unknown', 'opencode')
								THEN 1
								ELSE 0
							END
						) DESC,
						e.occurred_at DESC,
						e.event_id DESC
				) AS rn
			FROM usage_events e
			JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
			WHERE e.event_type IN ('message_usage', 'tool_usage')
		)
		SELECT COUNT(*) FROM ranked WHERE rn > 1
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("telemetry: compact count duplicates: %w", err)
	}
	return count, nil
}
