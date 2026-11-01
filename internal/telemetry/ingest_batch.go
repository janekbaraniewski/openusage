package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ingestBatchSize is how many events IngestBatch writes per transaction. The
// store runs on a single connection, so a transaction holds every other
// reader and writer off for its duration; 500 events keeps that window in the
// tens of milliseconds while amortising the commit (fsync) that dominated the
// per-event path.
const ingestBatchSize = 500

// ingestExec runs the ingest statements of one transaction, preparing each
// distinct query once and reusing it for every event written in that
// transaction. Prepared statements are bound to the transaction and released
// by close (or implicitly when the transaction ends).
type ingestExec struct {
	tx    *sql.Tx
	stmts map[string]*sql.Stmt
}

func newIngestExec(tx *sql.Tx) *ingestExec {
	return &ingestExec{tx: tx, stmts: make(map[string]*sql.Stmt, 6)}
}

func (e *ingestExec) stmt(ctx context.Context, query string) (*sql.Stmt, error) {
	if st, ok := e.stmts[query]; ok {
		return st, nil
	}
	st, err := e.tx.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	e.stmts[query] = st
	return st, nil
}

func (e *ingestExec) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	st, err := e.stmt(ctx, query)
	if err != nil {
		return nil, err
	}
	return st.ExecContext(ctx, args...)
}

func (e *ingestExec) queryRow(ctx context.Context, query string, args ...any) (*sql.Row, error) {
	st, err := e.stmt(ctx, query)
	if err != nil {
		return nil, err
	}
	return st.QueryRowContext(ctx, args...), nil
}

func (e *ingestExec) close() {
	for _, st := range e.stmts {
		_ = st.Close()
	}
	e.stmts = nil
}

// IngestOutcome is the per-event result of IngestBatch. Err is set when that
// event was not stored; Result is meaningful only when Err is nil.
type IngestOutcome struct {
	Result IngestResult
	Err    error
}

// IngestBatch writes reqs with the same semantics as calling Ingest once per
// request, in order, but commits ingestBatchSize events per transaction and
// reuses prepared statements across them. Each event runs inside its own
// SAVEPOINT, so an event that fails is rolled back on its own and the rest of
// its batch still commits. The returned slice is index-aligned with reqs.
//
// If a whole transaction cannot be committed (or SQLite aborts it), every
// event of that chunk is reported as failed, so callers can retry or spool
// them exactly as they would a failed Ingest.
func (s *Store) IngestBatch(ctx context.Context, reqs []IngestRequest) []IngestOutcome {
	out := make([]IngestOutcome, len(reqs))
	for start := 0; start < len(reqs); start += ingestBatchSize {
		end := start + ingestBatchSize
		if end > len(reqs) {
			end = len(reqs)
		}
		s.ingestChunk(ctx, reqs[start:end], out[start:end])
	}
	return out
}

func (s *Store) ingestChunk(ctx context.Context, reqs []IngestRequest, out []IngestOutcome) {
	failAll := func(err error) {
		for i := range out {
			out[i] = IngestOutcome{Err: err}
		}
	}
	if s == nil || s.db == nil {
		failAll(fmt.Errorf("telemetry: store unavailable"))
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		failAll(fmt.Errorf("telemetry: begin batch tx: %w", err))
		return
	}
	defer func() { _ = tx.Rollback() }()
	ex := newIngestExec(tx)
	defer ex.close()

	for i, req := range reqs {
		if _, err := ex.exec(ctx, `SAVEPOINT ingest_event`); err != nil {
			failAll(fmt.Errorf("telemetry: savepoint: %w", err))
			return
		}
		res, ingestErr := s.ingestInTx(ctx, ex, req)
		if ingestErr != nil {
			if _, err := ex.exec(ctx, `ROLLBACK TO ingest_event`); err != nil {
				// SQLite aborted the whole transaction (I/O error, cancelled
				// context, ...). Nothing in this chunk will commit.
				failAll(fmt.Errorf("telemetry: rollback event: %w (event error: %v)", err, ingestErr))
				return
			}
			out[i] = IngestOutcome{Err: ingestErr}
		} else {
			out[i] = IngestOutcome{Result: res}
		}
		if _, err := ex.exec(ctx, `RELEASE ingest_event`); err != nil {
			failAll(fmt.Errorf("telemetry: release savepoint: %w", err))
			return
		}
	}
	if err := tx.Commit(); err != nil {
		failAll(fmt.Errorf("telemetry: commit batch tx: %w", err))
	}
}

// IsLocalFileChannel reports whether events on this channel are re-read from
// files on disk (session logs, local SQLite databases) on every collection, as
// opposed to pushed once by a hook or poller.
func IsLocalFileChannel(ch SourceChannel) bool {
	return ch == SourceChannelJSONL || ch == SourceChannelSQLite
}

// LocalIngestHorizon returns the occurred_at before which a re-read
// local-file event can be dropped at ingest instead of being written and then
// deleted by the next retention pass. An event qualifies only when both hold:
//
//   - it is older than the retention window, so PruneOldEvents would delete it;
//   - its day is strictly before the rollup watermark, so usage_rollup_daily
//     already holds its aggregate and RollupDaily (which recomputes from the
//     watermark day forward) will never look at that day again.
//
// The horizon is the earlier of the two bounds. A zero time means nothing may
// be dropped: no rollup has run yet (first import must reach the rollup) or
// retention is disabled.
func (s *Store) LocalIngestHorizon(ctx context.Context, retentionDays int) (time.Time, error) {
	if s == nil || s.db == nil || retentionDays <= 0 {
		return time.Time{}, nil
	}
	wm, err := s.RollupWatermark(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if wm == "" {
		return time.Time{}, nil
	}
	wmDay, err := time.Parse("2006-01-02", wm)
	if err != nil {
		return time.Time{}, nil
	}
	cutoff := s.now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	if wmDay.Before(cutoff) {
		return wmDay, nil
	}
	return cutoff, nil
}

// DropExpiredLocalEvents removes local-file events that occurred before
// horizon (see LocalIngestHorizon). Hook, API and poller events are always
// kept. The input slice is not modified.
func DropExpiredLocalEvents(reqs []IngestRequest, horizon time.Time) (kept []IngestRequest, dropped int) {
	if horizon.IsZero() || len(reqs) == 0 {
		return reqs, 0
	}
	kept = make([]IngestRequest, 0, len(reqs))
	for _, req := range reqs {
		if IsLocalFileChannel(req.SourceChannel) && !req.OccurredAt.IsZero() && req.OccurredAt.Before(horizon) {
			dropped++
			continue
		}
		kept = append(kept, req)
	}
	return kept, dropped
}
