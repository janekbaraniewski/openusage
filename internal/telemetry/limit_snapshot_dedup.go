package telemetry

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// touchIdenticalLimitSnapshot handles a limit_snapshot whose payload is
// byte-identical to the newest stored one for the same provider/account. In
// that case the stored row's occurred_at is moved forward to the new poll time
// (so the read model still reports the snapshot as fresh) and no new raw or
// canonical row is written. Returns true when the request was absorbed this
// way and must not be ingested.
func (s *Store) touchIdenticalLimitSnapshot(ctx context.Context, req IngestRequest) (bool, error) {
	if s == nil || s.db == nil || req.EventType != EventTypeLimitSnapshot {
		return false, nil
	}
	norm := normalizeRequest(req, s.now().UTC())
	payloadBytes, err := marshalPayload(norm.Payload)
	if err != nil {
		return false, nil // let Ingest surface the marshal error
	}
	sum := sha256.Sum256(payloadBytes)
	hash := hex.EncodeToString(sum[:])

	var (
		eventID    string
		storedHash sql.NullString
		storedAt   string
		status     string
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT e.event_id, r.source_payload_hash, e.occurred_at, e.status
		FROM usage_events e
		JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
		WHERE e.event_type = 'limit_snapshot'
		  AND e.provider_id = ?
		  AND e.account_id = ?
		  AND r.source_system = ?
		ORDER BY e.occurred_at DESC
		LIMIT 1
	`, norm.ProviderID, norm.AccountID, string(norm.SourceSystem)).Scan(&eventID, &storedHash, &storedAt, &status)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("telemetry: load latest limit snapshot: %w", err)
	}
	if !storedHash.Valid || storedHash.String != hash || status != string(norm.Status) {
		return false, nil
	}
	newAt := norm.OccurredAt.UTC().Format(time.RFC3339Nano)
	if prev, perr := time.Parse(time.RFC3339Nano, storedAt); perr == nil && !norm.OccurredAt.After(prev) {
		return true, nil // same or older poll of identical content: nothing to do
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE usage_events SET occurred_at = ? WHERE event_id = ?`, newAt, eventID); err != nil {
		return false, fmt.Errorf("telemetry: touch limit snapshot: %w", err)
	}
	return true, nil
}
