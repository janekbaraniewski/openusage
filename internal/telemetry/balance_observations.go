package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// BalanceSemantics classifies how a money metric's numeric value behaves over
// time, which determines how windowed spend is derived from a series of
// observations. It mirrors core.BalanceSemantics but is duplicated as a plain
// string at the storage layer so the telemetry package has no dependency on a
// particular provider-spec shape.
const (
	balanceSemanticsCumulative = "cumulative" // monotonic used-counter (spend = delta of used)
	balanceSemanticsBalance    = "balance"    // remaining balance (spend = sum of drops)
	balanceSemanticsLimit      = "limit"      // hard cap, no spend signal
)

// BalanceObservation is one numeric reading of a credit/balance metric at a
// point in time. Used/Remaining are pointers because a given provider exposes
// only one of them (a cumulative counter vs. a point-in-time balance).
type BalanceObservation struct {
	MetricKey  string
	ObservedAt time.Time
	Used       *float64
	Limit      *float64
	Remaining  *float64
	Unit       string
	Semantics  string
}

// RecordBalanceObservations upserts a batch of observations for one
// (provider, account). Observations are keyed by minute: a re-poll within the
// same minute overwrites the existing row rather than accumulating, which keeps
// the series compact without losing meaningful resolution for windowed deltas.
func (s *Store) RecordBalanceObservations(ctx context.Context, providerID, accountID string, obs []BalanceObservation) error {
	if s == nil || s.db == nil || len(obs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("telemetry: begin balance tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO balance_observations
			(provider_id, account_id, metric_key, observed_at, used, limit_val, remaining, unit, semantics)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider_id, account_id, metric_key, observed_at)
		DO UPDATE SET used=excluded.used, limit_val=excluded.limit_val,
			remaining=excluded.remaining, unit=excluded.unit, semantics=excluded.semantics
	`)
	if err != nil {
		return fmt.Errorf("telemetry: prepare balance insert: %w", err)
	}
	defer stmt.Close()

	for _, o := range obs {
		if o.MetricKey == "" || o.Semantics == "" {
			continue
		}
		// Minute-truncated timestamp is the dedup unit.
		ts := o.ObservedAt.UTC().Truncate(time.Minute).Format(time.RFC3339Nano)
		if _, err := stmt.ExecContext(ctx, providerID, accountID, o.MetricKey, ts,
			o.Used, o.Limit, o.Remaining, o.Unit, o.Semantics); err != nil {
			return fmt.Errorf("telemetry: insert balance observation: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("telemetry: commit balance tx: %w", err)
	}
	return nil
}

// latestBalanceObservation returns the most recent observation for the metric,
// or ok=false when none exists.
func (s *Store) latestBalanceObservation(ctx context.Context, providerID, accountID, metricKey string) (BalanceObservation, bool, error) {
	if s == nil || s.db == nil {
		return BalanceObservation{}, false, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT observed_at, used, limit_val, remaining, unit, semantics
		FROM balance_observations
		WHERE provider_id = ? AND account_id = ? AND metric_key = ?
		ORDER BY observed_at DESC
		LIMIT 1
	`, providerID, accountID, metricKey)
	return scanBalanceObservation(metricKey, row)
}

// balanceObservationAtOrBefore returns the latest observation at or before the
// given time. Used to anchor a cumulative-spend delta at the window's start.
func (s *Store) balanceObservationAtOrBefore(ctx context.Context, providerID, accountID, metricKey string, at time.Time) (BalanceObservation, bool, error) {
	if s == nil || s.db == nil {
		return BalanceObservation{}, false, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT observed_at, used, limit_val, remaining, unit, semantics
		FROM balance_observations
		WHERE provider_id = ? AND account_id = ? AND metric_key = ? AND observed_at <= ?
		ORDER BY observed_at DESC
		LIMIT 1
	`, providerID, accountID, metricKey, at.UTC().Format(time.RFC3339Nano))
	return scanBalanceObservation(metricKey, row)
}

// earliestBalanceObservation returns the oldest observation for the metric.
func (s *Store) earliestBalanceObservation(ctx context.Context, providerID, accountID, metricKey string) (BalanceObservation, bool, error) {
	if s == nil || s.db == nil {
		return BalanceObservation{}, false, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT observed_at, used, limit_val, remaining, unit, semantics
		FROM balance_observations
		WHERE provider_id = ? AND account_id = ? AND metric_key = ?
		ORDER BY observed_at ASC
		LIMIT 1
	`, providerID, accountID, metricKey)
	return scanBalanceObservation(metricKey, row)
}

// balanceObservationsSince returns every observation at or after `since`,
// oldest first. Used to walk a point-in-time balance and sum the drops.
func (s *Store) balanceObservationsSince(ctx context.Context, providerID, accountID, metricKey string, since time.Time) ([]BalanceObservation, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT observed_at, used, limit_val, remaining, unit, semantics
		FROM balance_observations
		WHERE provider_id = ? AND account_id = ? AND metric_key = ? AND observed_at >= ?
		ORDER BY observed_at ASC
	`, providerID, accountID, metricKey, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("telemetry: query balance series: %w", err)
	}
	defer rows.Close()

	var out []BalanceObservation
	for rows.Next() {
		o, _, err := scanBalanceObservationRow(metricKey, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanBalanceObservation(metricKey string, row *sql.Row) (BalanceObservation, bool, error) {
	o, err := scanBalanceCols(metricKey, row)
	if err == sql.ErrNoRows {
		return BalanceObservation{}, false, nil
	}
	if err != nil {
		return BalanceObservation{}, false, fmt.Errorf("telemetry: scan balance observation: %w", err)
	}
	return o, true, nil
}

func scanBalanceObservationRow(metricKey string, rows *sql.Rows) (BalanceObservation, bool, error) {
	o, err := scanBalanceCols(metricKey, rows)
	if err != nil {
		return BalanceObservation{}, false, fmt.Errorf("telemetry: scan balance row: %w", err)
	}
	return o, true, nil
}

func scanBalanceCols(metricKey string, sc rowScanner) (BalanceObservation, error) {
	var (
		tsStr     string
		used      sql.NullFloat64
		limitVal  sql.NullFloat64
		remaining sql.NullFloat64
		unit      sql.NullString
		semantics string
	)
	if err := sc.Scan(&tsStr, &used, &limitVal, &remaining, &unit, &semantics); err != nil {
		return BalanceObservation{}, err
	}
	ts, _ := time.Parse(time.RFC3339Nano, tsStr)
	o := BalanceObservation{MetricKey: metricKey, ObservedAt: ts, Unit: unit.String, Semantics: semantics}
	if used.Valid {
		o.Used = &used.Float64
	}
	if limitVal.Valid {
		o.Limit = &limitVal.Float64
	}
	if remaining.Valid {
		o.Remaining = &remaining.Float64
	}
	return o, nil
}

// creditMetricPriority is the order in which we pick the single "primary"
// money metric to surface as windowed spend when an account has observations
// for more than one. Mirrors the display layer's gauge priority so the windowed
// figure aligns with the headline the user sees.
var creditMetricPriority = []string{
	"credit_balance",
	"available_balance",
	"credits",
	"monthly_spend",
	"daily_spend",
	"total_cost",
	"total_spend",
	"total_balance",
}

// PrimaryCreditMetric returns the highest-priority money metric we have
// observations for on this account, with its stored semantics. ok=false when
// the account has no balance observations.
func (s *Store) PrimaryCreditMetric(ctx context.Context, providerID, accountID string) (metricKey, semantics string, ok bool, err error) {
	if s == nil || s.db == nil {
		return "", "", false, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT metric_key, semantics
		FROM balance_observations
		WHERE provider_id = ? AND account_id = ?
	`, providerID, accountID)
	if err != nil {
		return "", "", false, fmt.Errorf("telemetry: query credit metrics: %w", err)
	}
	defer rows.Close()

	present := map[string]string{}
	for rows.Next() {
		var k, sem string
		if err := rows.Scan(&k, &sem); err != nil {
			return "", "", false, fmt.Errorf("telemetry: scan credit metric: %w", err)
		}
		present[k] = sem
	}
	if err := rows.Err(); err != nil {
		return "", "", false, err
	}
	for _, k := range creditMetricPriority {
		if sem, found := present[k]; found {
			return k, sem, true, nil
		}
	}
	return "", "", false, nil
}

// PruneBalanceObservations thins and trims the series:
//   - rows older than max(retentionDays, minRetentionDays) are deleted;
//   - between 7 days and that horizon, at most one row per metric per day;
//   - between 48h and 7 days, at most one row per metric per hour.
//
// Recent (<48h) rows keep full poll resolution. Thinning keeps the oldest row
// in each bucket so window left-anchors stay stable.
func (s *Store) PruneBalanceObservations(ctx context.Context, retentionDays int) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	const minRetentionDays = 35
	if retentionDays < minRetentionDays {
		retentionDays = minRetentionDays
	}

	// observed_at is RFC3339Nano, so the horizons are rendered in Go rather
	// than with datetime('now', ...) — see Store.retentionCutoff for why the
	// two forms do not compare correctly within the same day.
	horizon := s.retentionCutoff(time.Duration(retentionDays) * 24 * time.Hour)
	sevenDays := s.retentionCutoff(7 * 24 * time.Hour)
	fortyEightHours := s.retentionCutoff(48 * time.Hour)

	var total int64
	// 1. Hard delete beyond the retention horizon.
	if res, err := s.db.ExecContext(ctx, `
		DELETE FROM balance_observations
		WHERE observed_at < ?
	`, horizon); err == nil {
		n, _ := res.RowsAffected()
		total += n
	} else {
		return total, fmt.Errorf("telemetry: prune balance horizon: %w", err)
	}

	// 2. Thin 7d..horizon to one row per metric per day (keep the earliest).
	if res, err := s.db.ExecContext(ctx, `
		DELETE FROM balance_observations
		WHERE observed_at < ?
		  AND rowid NOT IN (
			SELECT MIN(rowid) FROM balance_observations
			WHERE observed_at < ?
			GROUP BY provider_id, account_id, metric_key, date(observed_at)
		  )
	`, sevenDays, sevenDays); err == nil {
		n, _ := res.RowsAffected()
		total += n
	} else {
		return total, fmt.Errorf("telemetry: thin balance daily: %w", err)
	}

	// 3. Thin 48h..7d to one row per metric per hour (keep the earliest).
	if res, err := s.db.ExecContext(ctx, `
		DELETE FROM balance_observations
		WHERE observed_at < ?
		  AND observed_at >= ?
		  AND rowid NOT IN (
			SELECT MIN(rowid) FROM balance_observations
			WHERE observed_at < ?
			  AND observed_at >= ?
			GROUP BY provider_id, account_id, metric_key, strftime('%Y-%m-%dT%H', observed_at)
		  )
	`, fortyEightHours, sevenDays, fortyEightHours, sevenDays); err == nil {
		n, _ := res.RowsAffected()
		total += n
	} else {
		return total, fmt.Errorf("telemetry: thin balance hourly: %w", err)
	}
	return total, nil
}
