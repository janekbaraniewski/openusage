package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"

	_ "github.com/mattn/go-sqlite3"
)

type storedLimitSnapshot struct {
	ProviderID  string                       `json:"provider_id"`
	AccountID   string                       `json:"account_id"`
	Status      string                       `json:"status"`
	Message     string                       `json:"message"`
	Metrics     map[string]storedLimitMetric `json:"metrics"`
	Resets      map[string]string            `json:"resets"`
	Attributes  map[string]string            `json:"attributes"`
	Diagnostics map[string]string            `json:"diagnostics"`
}

type storedLimitMetric struct {
	Limit     *float64 `json:"limit"`
	Remaining *float64 `json:"remaining"`
	Used      *float64 `json:"used"`
	Unit      string   `json:"unit"`
	Window    string   `json:"window"`
}

type storedLimitEnvelope struct {
	Snapshot storedLimitSnapshot `json:"snapshot"`
}

// ApplyCanonicalTelemetryView hydrates snapshots from canonical telemetry streams.
// Root quota values come from limit_snapshot events, then usage overlays are applied.
func ApplyCanonicalTelemetryView(ctx context.Context, dbPath string, snaps map[string]core.UsageSnapshot) (map[string]core.UsageSnapshot, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		var err error
		dbPath, err = DefaultDBPath()
		if err != nil {
			return snaps, nil
		}
	}
	if _, err := os.Stat(dbPath); err != nil {
		return snaps, nil
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return snaps, fmt.Errorf("open telemetry read model db: %w", err)
	}
	defer db.Close()

	merged, err := hydrateRootsFromLimitSnapshots(ctx, db, snaps)
	if err != nil {
		return snaps, err
	}
	merged, err = appendTelemetryOnlyProviderSnapshots(ctx, db, merged)
	if err != nil {
		return snaps, err
	}
	return applyProviderTelemetryOverlayWithDB(ctx, db, merged)
}

func hydrateRootsFromLimitSnapshots(ctx context.Context, db *sql.DB, snaps map[string]core.UsageSnapshot) (map[string]core.UsageSnapshot, error) {
	out := make(map[string]core.UsageSnapshot, len(snaps))
	cache := make(map[string]*core.UsageSnapshot)

	for accountID, snap := range snaps {
		s := snap
		providerID := strings.TrimSpace(s.ProviderID)
		effectiveAccountID := strings.TrimSpace(s.AccountID)
		if effectiveAccountID == "" {
			effectiveAccountID = strings.TrimSpace(accountID)
		}
		if providerID == "" || effectiveAccountID == "" {
			out[accountID] = s
			continue
		}

		cacheKey := providerID + "|" + effectiveAccountID
		latest, ok := cache[cacheKey]
		if !ok {
			loaded, err := loadLatestLimitSnapshot(ctx, db, providerID, effectiveAccountID)
			if err != nil {
				return snaps, err
			}
			cache[cacheKey] = loaded
			latest = loaded
		}

		if latest != nil {
			s = mergeLimitSnapshotRoot(s, *latest)
		}
		out[accountID] = s
	}

	return out, nil
}

func loadLatestLimitSnapshot(ctx context.Context, db *sql.DB, providerID, accountID string) (*core.UsageSnapshot, error) {
	var (
		payload    string
		occurredAt string
	)
	err := db.QueryRowContext(ctx, `
		SELECT r.source_payload, e.occurred_at
		FROM usage_events e
		JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
		WHERE e.event_type = 'limit_snapshot'
		  AND e.provider_id = ?
		  AND COALESCE(e.account_id, '') = ?
		  AND r.source_system = ?
		ORDER BY e.occurred_at DESC
		LIMIT 1
	`, providerID, accountID, string(SourceSystemPoller)).Scan(&payload, &occurredAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load latest limit snapshot (%s/%s): %w", providerID, accountID, err)
	}

	var envelope storedLimitEnvelope
	if unmarshalErr := json.Unmarshal([]byte(payload), &envelope); unmarshalErr != nil {
		return nil, nil
	}

	s := core.UsageSnapshot{
		ProviderID:  firstNonEmptyNonBlank(envelope.Snapshot.ProviderID, providerID),
		AccountID:   firstNonEmptyNonBlank(envelope.Snapshot.AccountID, accountID),
		Status:      mapCoreStatus(envelope.Snapshot.Status),
		Message:     strings.TrimSpace(envelope.Snapshot.Message),
		Metrics:     make(map[string]core.Metric, len(envelope.Snapshot.Metrics)),
		Resets:      make(map[string]time.Time, len(envelope.Snapshot.Resets)),
		Attributes:  mapClone(envelope.Snapshot.Attributes),
		Diagnostics: mapClone(envelope.Snapshot.Diagnostics),
	}

	for key, metric := range envelope.Snapshot.Metrics {
		s.Metrics[key] = core.Metric{
			Limit:     metric.Limit,
			Remaining: metric.Remaining,
			Used:      metric.Used,
			Unit:      strings.TrimSpace(metric.Unit),
			Window:    strings.TrimSpace(metric.Window),
		}
	}
	for key, raw := range envelope.Snapshot.Resets {
		ts, err := parseFlexibleTime(raw)
		if err != nil {
			continue
		}
		s.Resets[key] = ts
	}

	if ts, err := parseFlexibleTime(occurredAt); err == nil {
		s.Timestamp = ts
	} else {
		s.Timestamp = time.Now().UTC()
	}
	s.SetAttribute("telemetry_root", "limit_snapshot")
	return &s, nil
}

func loadLatestLimitSnapshotAnyAccount(ctx context.Context, db *sql.DB, providerID string) (*core.UsageSnapshot, error) {
	var (
		payload    string
		occurredAt string
	)
	err := db.QueryRowContext(ctx, `
		SELECT r.source_payload, e.occurred_at
		FROM usage_events e
		JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
		WHERE e.event_type = 'limit_snapshot'
		  AND e.provider_id = ?
		  AND r.source_system = ?
		ORDER BY e.occurred_at DESC
		LIMIT 1
	`, providerID, string(SourceSystemPoller)).Scan(&payload, &occurredAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load latest limit snapshot (%s): %w", providerID, err)
	}

	var envelope storedLimitEnvelope
	if unmarshalErr := json.Unmarshal([]byte(payload), &envelope); unmarshalErr != nil {
		return nil, nil
	}

	s := core.UsageSnapshot{
		ProviderID:  firstNonEmptyNonBlank(envelope.Snapshot.ProviderID, providerID),
		AccountID:   strings.TrimSpace(envelope.Snapshot.AccountID),
		Status:      mapCoreStatus(envelope.Snapshot.Status),
		Message:     strings.TrimSpace(envelope.Snapshot.Message),
		Metrics:     make(map[string]core.Metric, len(envelope.Snapshot.Metrics)),
		Resets:      make(map[string]time.Time, len(envelope.Snapshot.Resets)),
		Attributes:  mapClone(envelope.Snapshot.Attributes),
		Diagnostics: mapClone(envelope.Snapshot.Diagnostics),
	}

	for key, metric := range envelope.Snapshot.Metrics {
		s.Metrics[key] = core.Metric{
			Limit:     metric.Limit,
			Remaining: metric.Remaining,
			Used:      metric.Used,
			Unit:      strings.TrimSpace(metric.Unit),
			Window:    strings.TrimSpace(metric.Window),
		}
	}
	for key, raw := range envelope.Snapshot.Resets {
		ts, err := parseFlexibleTime(raw)
		if err != nil {
			continue
		}
		s.Resets[key] = ts
	}

	if ts, err := parseFlexibleTime(occurredAt); err == nil {
		s.Timestamp = ts
	} else {
		s.Timestamp = time.Now().UTC()
	}
	s.SetAttribute("telemetry_root", "limit_snapshot")
	return &s, nil
}

func mergeLimitSnapshotRoot(base core.UsageSnapshot, root core.UsageSnapshot) core.UsageSnapshot {
	merged := base
	merged.ProviderID = firstNonEmptyNonBlank(root.ProviderID, merged.ProviderID)
	merged.AccountID = firstNonEmptyNonBlank(root.AccountID, merged.AccountID)
	if !root.Timestamp.IsZero() {
		merged.Timestamp = root.Timestamp
	}
	if root.Status != "" {
		merged.Status = root.Status
	}
	if strings.TrimSpace(root.Message) != "" {
		merged.Message = strings.TrimSpace(root.Message)
	}

	merged.Metrics = cloneMetricMap(root.Metrics)
	merged.Resets = cloneTimeMap(root.Resets)
	merged.Attributes = mapClone(root.Attributes)
	merged.Diagnostics = mapClone(root.Diagnostics)
	if merged.Raw == nil {
		merged.Raw = map[string]string{}
	}
	return merged
}

func appendTelemetryOnlyProviderSnapshots(
	ctx context.Context,
	db *sql.DB,
	snaps map[string]core.UsageSnapshot,
) (map[string]core.UsageSnapshot, error) {
	if db == nil {
		return snaps, nil
	}

	out := make(map[string]core.UsageSnapshot, len(snaps))
	providerSeen := make(map[string]bool, len(snaps))
	for accountID, snap := range snaps {
		out[accountID] = snap
		provider := strings.TrimSpace(snap.ProviderID)
		if provider != "" {
			providerSeen[provider] = true
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT provider_id, COALESCE(MAX(occurred_at), '')
		FROM usage_events
		WHERE COALESCE(TRIM(provider_id), '') != ''
		  AND event_type IN ('message_usage', 'tool_usage', 'limit_snapshot')
		GROUP BY provider_id
		ORDER BY provider_id ASC
	`)
	if err != nil {
		return snaps, fmt.Errorf("list telemetry providers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			providerID string
			lastAtRaw  string
		)
		if err := rows.Scan(&providerID, &lastAtRaw); err != nil {
			continue
		}
		providerID = strings.TrimSpace(providerID)
		if providerID == "" || providerSeen[providerID] {
			continue
		}

		snapKey := "telemetry:" + providerID
		synthetic := core.UsageSnapshot{
			ProviderID: providerID,
			AccountID:  snapKey,
			Timestamp:  time.Now().UTC(),
			Status:     core.StatusOK,
			Metrics:    map[string]core.Metric{},
			Resets:     map[string]time.Time{},
			Attributes: map[string]string{
				"telemetry_only": "true",
			},
			Diagnostics: map[string]string{},
			Raw:         map[string]string{},
			Message:     "Telemetry-only provider (no local provider configured)",
		}
		if ts, err := parseFlexibleTime(lastAtRaw); err == nil {
			synthetic.Timestamp = ts
			synthetic.SetAttribute("telemetry_last_event_at", ts.Format(time.RFC3339Nano))
		}

		limitSnap, limitErr := loadLatestLimitSnapshotAnyAccount(ctx, db, providerID)
		if limitErr != nil {
			return snaps, limitErr
		}
		if limitSnap != nil {
			synthetic = mergeLimitSnapshotRoot(synthetic, *limitSnap)
			synthetic.AccountID = snapKey
			synthetic.SetAttribute("telemetry_only", "true")
		}

		out[snapKey] = synthetic
		providerSeen[providerID] = true
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return snaps, fmt.Errorf("scan telemetry providers: %w", rowsErr)
	}
	return out, nil
}

func mapCoreStatus(raw string) core.Status {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(core.StatusOK):
		return core.StatusOK
	case string(core.StatusNearLimit):
		return core.StatusNearLimit
	case string(core.StatusLimited):
		return core.StatusLimited
	case string(core.StatusAuth):
		return core.StatusAuth
	case string(core.StatusUnsupported):
		return core.StatusUnsupported
	case string(core.StatusError):
		return core.StatusError
	default:
		return core.StatusUnknown
	}
}

func parseFlexibleTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts.UTC(), nil
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported time format")
}

func mapClone(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneMetricMap(in map[string]core.Metric) map[string]core.Metric {
	if len(in) == 0 {
		return map[string]core.Metric{}
	}
	out := make(map[string]core.Metric, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneTimeMap(in map[string]time.Time) map[string]time.Time {
	if len(in) == 0 {
		return map[string]time.Time{}
	}
	out := make(map[string]time.Time, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
