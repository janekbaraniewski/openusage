package antigravity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const telemetrySchemaVersion = "antigravity_statusline_v1"

// System implements shared.TelemetrySource.
func (p *Provider) System() string { return p.ID() }

// DefaultCollectOptions points the daemon at the latest status-line state.
func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			"status_file": DefaultStatusFilePath(),
		},
	}
}

// Collect reads the latest status-line state. The existing telemetry store
// deduplicates the stable revision ID, so polling this file is cheap and safe.
func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := opts.Path("status_file", DefaultStatusFilePath())
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(shared.ExpandHome(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("antigravity: read telemetry state: %w", err)
	}
	payload, err := parseStatusLinePayload(data)
	if err != nil {
		return nil, err
	}
	event := statusLineTelemetryEvent(payload, opts.Path("account_id", defaultAccountID), shared.TelemetryChannelHook, path)
	return []shared.TelemetryEvent{event}, nil
}

// ParseHookPayload accepts a raw Antigravity status-line document for callers
// that explicitly route it through `openusage telemetry hook antigravity`.
func (p *Provider) ParseHookPayload(raw []byte, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	payload, err := parseStatusLinePayload(raw)
	if err != nil {
		return nil, err
	}
	return []shared.TelemetryEvent{
		statusLineTelemetryEvent(payload, opts.Path("account_id", defaultAccountID), shared.TelemetryChannelHook, ""),
	}, nil
}

func statusLineTelemetryEvent(
	payload statusLinePayload,
	accountID string,
	channel shared.TelemetryChannel,
	statusPath string,
) shared.TelemetryEvent {
	sessionID := strings.TrimSpace(payload.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(payload.ConversationID)
	}
	if sessionID == "" && strings.TrimSpace(payload.TranscriptPath) != "" {
		sessionID = strings.TrimSuffix(filepath.Base(payload.TranscriptPath), filepath.Ext(payload.TranscriptPath))
	}
	if sessionID == "" {
		sessionID = defaultAccountID
	}

	model := strings.TrimSpace(payload.Model.ID)
	if model == "" {
		model = strings.TrimSpace(payload.Model.DisplayName)
	}
	revision := statusLineRevision(payload)
	usage := currentTokenUsage(payload.ContextWindow.CurrentUsage)
	eventType := shared.TelemetryEventTypeRawEnvelope
	if usage.HasTokenData() {
		eventType = shared.TelemetryEventTypeMessageUsage
	}
	occurredAt := payloadReceivedAt(payload)

	event := shared.TelemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		Channel:       channel,
		OccurredAt:    occurredAt,
		AccountID:     strings.TrimSpace(accountID),
		WorkspaceID:   shared.SanitizeWorkspace(statusWorkspace(payload)),
		SessionID:     sessionID,
		TurnID:        sessionID + ":status:" + revision,
		MessageID:     revision,
		ProviderID:    providerID,
		AgentName:     providerID,
		EventType:     eventType,
		ModelRaw:      model,
		TokenUsage:    usage,
		Status:        shared.TelemetryStatusOK,
		Payload: map[string]any{
			"source":                       "antigravity_statusline",
			"status_file":                  statusPath,
			"agent_state":                  payload.AgentState,
			"product":                      payload.Product,
			"cumulative_input_tokens":      payload.ContextWindow.TotalInputTokens,
			"cumulative_output_tokens":     payload.ContextWindow.TotalOutputTokens,
			"context_window_size":          int64Value(payload.ContextWindow.ContextWindowSize),
			"context_used_percentage":      float64Value(payload.ContextWindow.UsedPercentage),
			"context_remaining_percentage": float64Value(payload.ContextWindow.RemainingPercentage),
		},
	}
	return event
}

func currentTokenUsage(usage statusLineCurrentUsage) core.TokenUsage {
	cacheWrite := usage.CacheWriteTokensValue()
	result := core.TokenUsage{
		InputTokens:      positiveInt64Ptr(usage.InputTokens),
		OutputTokens:     positiveInt64Ptr(usage.OutputTokens),
		CacheReadTokens:  positiveInt64Ptr(usage.CacheReadTokens),
		CacheWriteTokens: positiveInt64Ptr(cacheWrite),
	}
	result.SumTotalTokens()
	if result.HasTokenData() {
		result.Requests = core.Int64Ptr(1)
	}
	return result
}

func statusLineRevision(payload statusLinePayload) string {
	material := strings.Join([]string{
		strings.TrimSpace(payload.SessionID),
		strings.TrimSpace(payload.ConversationID),
		strings.TrimSpace(payload.Model.ID),
		strings.TrimSpace(payload.Model.DisplayName),
		strconv.FormatInt(payload.ContextWindow.TotalInputTokens, 10),
		strconv.FormatInt(payload.ContextWindow.TotalOutputTokens, 10),
		strconv.FormatInt(payload.ContextWindow.CurrentUsage.InputTokens, 10),
		strconv.FormatInt(payload.ContextWindow.CurrentUsage.OutputTokens, 10),
		strconv.FormatInt(payload.ContextWindow.CurrentUsage.CacheReadTokens, 10),
		strconv.FormatInt(payload.ContextWindow.CurrentUsage.CacheWriteTokensValue(), 10),
		strings.TrimSpace(payload.AgentState),
	}, "|")
	digest := sha256.Sum256([]byte(material))
	return hex.EncodeToString(digest[:8])
}

func positiveInt64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return core.Int64Ptr(value)
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func float64Value(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
