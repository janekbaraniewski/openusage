package antigravity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const telemetrySchemaVersion = "antigravity_quota_api_v1"

// System implements shared.TelemetrySource.
func (p *Provider) System() string { return p.ID() }

// DefaultCollectOptions carries the account id for multi-box collects.
func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			"config_dir": "",
		},
	}
}

// Collect fetches live quota via the same path as Fetch and emits a stable
// revision event the telemetry store can dedupe.
func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	acct := core.AccountConfig{
		ID:            opts.Path("account_id", defaultAccountID),
		Provider:      providerID,
		ProviderPaths: map[string]string{},
		RuntimeHints:  map[string]string{},
	}
	if dir := strings.TrimSpace(opts.Path("config_dir", "")); dir != "" {
		acct.ProviderPaths["config_dir"] = dir
	}
	if box := strings.TrimSpace(opts.Path("box_name", "")); box != "" {
		acct.RuntimeHints["box_name"] = box
	}
	if token := strings.TrimSpace(opts.Path("oauth_token_file", "")); token != "" {
		acct.ProviderPaths["oauth_token_file"] = token
	}
	if endpoint := strings.TrimSpace(opts.Path("quota_endpoint", "")); endpoint != "" {
		acct.RuntimeHints["quota_endpoint"] = endpoint
	}

	snap, err := p.Fetch(ctx, acct)
	if err != nil {
		return nil, err
	}
	if snap.Status == core.StatusAuth || snap.Status == core.StatusError {
		return nil, nil
	}
	return []shared.TelemetryEvent{quotaTelemetryEvent(snap, opts.Path("account_id", defaultAccountID))}, nil
}

// ParseHookPayload is retained for daemon hook routing but no longer accepts
// Antigravity status-line documents.
func (p *Provider) ParseHookPayload(raw []byte, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return nil, fmt.Errorf("antigravity: status-line hook payloads are no longer supported; quota is polled via API")
}

func quotaTelemetryEvent(snap core.UsageSnapshot, accountID string) shared.TelemetryEvent {
	occurredAt := snap.Timestamp
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	revision := quotaRevision(snap)
	sessionID := strings.TrimSpace(accountID)
	if sessionID == "" {
		sessionID = defaultAccountID
	}
	return shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       shared.TelemetryChannelAPI,
		OccurredAt:    occurredAt,
		AccountID:     sessionID,
		SessionID:     sessionID,
		TurnID:        sessionID + ":quota:" + revision,
		MessageID:     revision,
		ProviderID:    providerID,
		AgentName:     providerID,
		EventType:     shared.TelemetryEventTypeRawEnvelope,
		Status:        shared.TelemetryStatusOK,
		Payload: map[string]any{
			"source":      "antigravity_quota_api",
			"quota_api":   snap.Raw["quota_api"],
			"box":         snap.Attributes["box"],
			"metric_keys": sortedMetricKeys(snap),
		},
	}
}

func quotaRevision(snap core.UsageSnapshot) string {
	parts := []string{string(snap.Status)}
	for _, key := range sortedMetricKeys(snap) {
		metric := snap.Metrics[key]
		rem := ""
		if metric.Remaining != nil {
			rem = strconv.FormatFloat(*metric.Remaining, 'f', 4, 64)
		}
		parts = append(parts, key+"="+rem)
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(digest[:8])
}

func sortedMetricKeys(snap core.UsageSnapshot) []string {
	return core.SortedStringKeys(snap.Metrics)
}
