package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"

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

type ReadModelOptions struct {
	ProviderLinks   map[string]string
	TimeWindowHours int    // 0 = no filter (all data)
	TimeWindow      string // raw value like "7d", "1h" for metric labels
}

// ApplyCanonicalTelemetryView hydrates snapshots from canonical telemetry streams.
// Root quota values come from limit_snapshot events, then canonical usage aggregates are applied.
func ApplyCanonicalTelemetryViewWithOptions(
	ctx context.Context,
	dbPath string,
	snaps map[string]core.UsageSnapshot,
	options ReadModelOptions,
) (map[string]core.UsageSnapshot, error) {
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
	if err := configureSQLiteConnection(db); err != nil {
		return snaps, fmt.Errorf("configure telemetry read model db: %w", err)
	}

	merged, err := hydrateRootsFromLimitSnapshots(ctx, db, snaps)
	if err != nil {
		return snaps, err
	}
	links := normalizeProviderLinks(options.ProviderLinks)
	merged, err = annotateUnmappedTelemetryProviders(ctx, db, merged, links)
	if err != nil {
		return snaps, err
	}
	return applyCanonicalUsageViewWithDB(ctx, db, merged, links, options.TimeWindowHours, options.TimeWindow)
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
	payload, occurredAt, found, err := queryLatestLimitSnapshotPayload(ctx, db, providerID, accountID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	latestDecoded, ok := decodeStoredLimitSnapshot(providerID, accountID, payload, occurredAt)
	if !ok {
		return nil, nil
	}
	latest := &latestDecoded
	latest.SetAttribute("telemetry_root", "limit_snapshot")
	return latest, nil
}

func queryLatestLimitSnapshotPayload(
	ctx context.Context,
	db *sql.DB,
	providerID, accountID string,
) (string, string, bool, error) {
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
		  AND e.account_id = ?
		  AND r.source_system = ?
		ORDER BY e.occurred_at DESC
		LIMIT 1
	`, providerID, accountID, string(SourceSystemPoller)).Scan(&payload, &occurredAt)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("load latest limit snapshot (%s/%s): %w", providerID, accountID, err)
	}
	return payload, occurredAt, true, nil
}

func decodeStoredLimitSnapshot(providerID, accountID, payload, occurredAt string) (core.UsageSnapshot, bool) {
	var envelope storedLimitEnvelope
	if unmarshalErr := json.Unmarshal([]byte(payload), &envelope); unmarshalErr != nil {
		return core.UsageSnapshot{}, false
	}

	s := core.UsageSnapshot{
		ProviderID:  firstNonEmptyNonBlank(envelope.Snapshot.ProviderID, providerID),
		AccountID:   firstNonEmptyNonBlank(envelope.Snapshot.AccountID, accountID),
		Status:      mapCoreStatus(envelope.Snapshot.Status),
		Message:     strings.TrimSpace(envelope.Snapshot.Message),
		Metrics:     make(map[string]core.Metric, len(envelope.Snapshot.Metrics)),
		Resets:      make(map[string]time.Time, len(envelope.Snapshot.Resets)),
		Attributes:  maps.Clone(envelope.Snapshot.Attributes),
		Diagnostics: maps.Clone(envelope.Snapshot.Diagnostics),
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
	return s, true
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

	merged.Metrics = maps.Clone(root.Metrics)
	merged.Resets = maps.Clone(root.Resets)
	merged.Attributes = maps.Clone(root.Attributes)
	merged.Diagnostics = maps.Clone(root.Diagnostics)
	if merged.Raw == nil {
		merged.Raw = map[string]string{}
	}
	return merged
}

func annotateUnmappedTelemetryProviders(
	ctx context.Context,
	db *sql.DB,
	snaps map[string]core.UsageSnapshot,
	providerLinks map[string]string,
) (map[string]core.UsageSnapshot, error) {
	if db == nil {
		return snaps, nil
	}

	out := make(map[string]core.UsageSnapshot, len(snaps))
	configuredProviders := make(map[string]bool, len(snaps))
	for accountID, snap := range snaps {
		s := snap
		s.EnsureMaps()
		delete(s.Diagnostics, "telemetry_unmapped_providers")
		delete(s.Diagnostics, "telemetry_provider_link_hint")
		out[accountID] = s

		provider := strings.ToLower(strings.TrimSpace(s.ProviderID))
		if provider != "" {
			configuredProviders[provider] = true
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT provider_id
		FROM usage_events
		WHERE provider_id IS NOT NULL
		  AND provider_id != ''
		  AND event_type IN ('message_usage', 'tool_usage')
		GROUP BY provider_id
		ORDER BY provider_id ASC
	`)
	if err != nil {
		return snaps, fmt.Errorf("list telemetry providers for mapping: %w", err)
	}
	defer rows.Close()

	unmapped := make([]string, 0)
	for rows.Next() {
		var providerID string
		if err := rows.Scan(&providerID); err != nil {
			continue
		}
		providerID = strings.ToLower(strings.TrimSpace(providerID))
		if providerID == "" {
			continue
		}
		if configuredProviders[providerID] {
			continue
		}
		if mappedTarget := providerLinks[providerID]; mappedTarget != "" {
			if configuredProviders[mappedTarget] {
				continue
			}
			unmapped = append(unmapped, providerID+"->"+mappedTarget)
			continue
		}
		unmapped = append(unmapped, providerID)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return snaps, fmt.Errorf("scan telemetry provider mappings: %w", rowsErr)
	}
	if len(unmapped) == 0 {
		return out, nil
	}

	unmappedCSV := strings.Join(unmapped, ",")
	for accountID, snap := range out {
		s := snap
		s.EnsureMaps()
		s.SetDiagnostic("telemetry_unmapped_providers", unmappedCSV)
		s.SetDiagnostic("telemetry_provider_link_hint", "Configure telemetry.provider_links.<source_provider>=<configured_provider_id> in settings.json")
		out[accountID] = s
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
	return shared.ParseTimestampString(raw)
}
