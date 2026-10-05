package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

var kimiQuotaKeys = []string{"usage_five_hour", "usage_monthly", "usage_monthly_code"}

// recoverKimiQuota keeps the latest activity and diagnostics, borrowing only
// subscription gauges from a successful poll of the same provider/account.
func recoverKimiQuota(ctx context.Context, db *sql.DB, latest *core.UsageSnapshot) error {
	latest.EnsureMaps()
	for _, key := range kimiQuotaKeys {
		if metric, ok := latest.Metrics[key]; ok && metric.Used != nil {
			latest.SetAttribute("quota_state", "fresh")
			if latest.Attributes["quota_fetched_at"] == "" {
				latest.SetAttribute("quota_fetched_at", latest.Timestamp.UTC().Format(time.RFC3339Nano))
			}
			return nil
		}
	}
	latest.SetAttribute("quota_state", "unavailable")
	var payload, occurredAt string
	err := db.QueryRowContext(ctx, `
		SELECT r.source_payload, e.occurred_at
		FROM usage_events e
		JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
		WHERE e.event_type = 'limit_snapshot'
		  AND e.provider_id = ? AND e.account_id = ?
		  AND r.source_system = ? AND e.occurred_at <= ?
		  AND CASE WHEN json_valid(r.source_payload) THEN
		    (json_extract(r.source_payload, '$.snapshot.metrics.usage_five_hour.used') IS NOT NULL
		     OR json_extract(r.source_payload, '$.snapshot.metrics.usage_monthly.used') IS NOT NULL
		     OR json_extract(r.source_payload, '$.snapshot.metrics.usage_monthly_code.used') IS NOT NULL)
		    AND coalesce(json_extract(r.source_payload, '$.snapshot.diagnostics.quota_error'), '') = ''
		    AND coalesce(json_extract(r.source_payload, '$.snapshot.diagnostics.quota'), '') = ''
		    AND coalesce(json_extract(r.source_payload, '$.snapshot.attributes.quota_state'), 'fresh') = 'fresh'
		    ELSE 0 END
		ORDER BY e.occurred_at DESC LIMIT 1
	`, latest.ProviderID, latest.AccountID, string(SourceSystemPoller), latest.Timestamp.UTC().Format(time.RFC3339Nano)).Scan(&payload, &occurredAt)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load last successful Kimi quota (%s): %w", latest.AccountID, err)
	}
	previous, ok := decodeStoredLimitSnapshot(latest.ProviderID, latest.AccountID, payload, occurredAt)
	if !ok {
		return nil
	}
	for _, key := range kimiQuotaKeys {
		if metric, ok := previous.Metrics[key]; ok && metric.Used != nil {
			latest.Metrics[key] = metric
			if resetAt, ok := previous.Resets[key]; ok {
				latest.Resets[key] = resetAt
			}
		}
	}
	latest.SetAttribute("quota_state", "stale")
	fetchedAt := previous.Attributes["quota_fetched_at"]
	if fetchedAt == "" {
		fetchedAt = previous.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	latest.SetAttribute("quota_fetched_at", fetchedAt)
	return nil
}
