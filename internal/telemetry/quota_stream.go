package telemetry

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const providerSnapshotSchemaVersion = "provider_snapshot_v1"

type QuotaSnapshotIngestor struct {
	store *Store
}

func NewQuotaSnapshotIngestor(store *Store) *QuotaSnapshotIngestor {
	return &QuotaSnapshotIngestor{store: store}
}

func (i *QuotaSnapshotIngestor) Ingest(ctx context.Context, snaps map[string]core.UsageSnapshot) error {
	if i == nil || i.store == nil || len(snaps) == 0 {
		return nil
	}
	reqs := BuildLimitSnapshotRequests(snaps)
	for _, req := range reqs {
		if _, err := i.store.Ingest(ctx, req); err != nil {
			return fmt.Errorf("ingest limit snapshot (%s/%s): %w", req.ProviderID, req.AccountID, err)
		}
	}
	return nil
}

// BuildLimitSnapshotRequests turns provider fetch snapshots into normalized
// telemetry events. This makes provider quota usage part of the same canonical stream.
func BuildLimitSnapshotRequests(snaps map[string]core.UsageSnapshot) []IngestRequest {
	if len(snaps) == 0 {
		return nil
	}

	accountIDs := make([]string, 0, len(snaps))
	for accountID := range snaps {
		accountIDs = append(accountIDs, accountID)
	}
	sort.Strings(accountIDs)

	out := make([]IngestRequest, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		snap := snaps[accountID]

		providerID := firstNonEmptyNonBlank(snap.ProviderID, "unknown")
		effectiveAccountID := firstNonEmptyNonBlank(snap.AccountID, accountID, "default")
		occurredAt := snap.Timestamp.UTC()
		if occurredAt.IsZero() {
			occurredAt = time.Now().UTC()
		}

		// Stable per provider/account/timestamp-second snapshot ID.
		turnID := fmt.Sprintf(
			"snapshot:%s:%s:%s",
			providerID,
			effectiveAccountID,
			occurredAt.Truncate(time.Second).Format(time.RFC3339),
		)

		out = append(out, IngestRequest{
			SourceSystem:        SourceSystemPoller,
			SourceChannel:       SourceChannelAPI,
			SourceSchemaVersion: providerSnapshotSchemaVersion,
			OccurredAt:          occurredAt,
			TurnID:              turnID,
			ProviderID:          providerID,
			AccountID:           effectiveAccountID,
			AgentName:           "provider_poller",
			EventType:           EventTypeLimitSnapshot,
			Status:              statusFromSnapshot(snap.Status),
			Payload: map[string]any{
				"snapshot": map[string]any{
					"provider_id": providerID,
					"account_id":  effectiveAccountID,
					"status":      string(snap.Status),
					"message":     snap.Message,
					"metrics":     serializeMetrics(snap.Metrics),
					"resets":      serializeResets(snap.Resets),
					"attributes":  cloneStringMap(snap.Attributes),
					"diagnostics": cloneStringMap(snap.Diagnostics),
				},
			},
		})
	}
	return out
}

func serializeMetrics(metrics map[string]core.Metric) map[string]any {
	if len(metrics) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(metrics))
	for key, metric := range metrics {
		out[key] = map[string]any{
			"limit":     ptrFloat(metric.Limit),
			"remaining": ptrFloat(metric.Remaining),
			"used":      ptrFloat(metric.Used),
			"unit":      metric.Unit,
			"window":    metric.Window,
		}
	}
	return out
}

func serializeResets(resets map[string]time.Time) map[string]any {
	if len(resets) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(resets))
	for key, value := range resets {
		if value.IsZero() {
			continue
		}
		out[key] = value.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func statusFromSnapshot(status core.Status) EventStatus {
	switch status {
	case core.StatusError:
		return EventStatusError
	case core.StatusLimited:
		return EventStatusAborted
	default:
		return EventStatusOK
	}
}

func cloneStringMap(in map[string]string) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func ptrFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
