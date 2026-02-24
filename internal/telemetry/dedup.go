package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// BuildDedupKey computes a stable event fingerprint with priority for
// tool_call_id > message_id > turn_id > fallback fingerprint.
func BuildDedupKey(event IngestRequest) string {
	norm := normalizeRequest(event, time.Now().UTC())

	stableID := ""
	hasStableID := false
	switch {
	case norm.ToolCallID != "":
		stableID = "tool:" + norm.ToolCallID
		hasStableID = true
	case norm.MessageID != "":
		stableID = "message:" + norm.MessageID
		hasStableID = true
	case norm.TurnID != "":
		stableID = "turn:" + norm.TurnID
		hasStableID = true
	default:
		stableID = "fp:" + fallbackFingerprint(norm)
	}

	sourceSystem := stableKeyPart(string(norm.SourceSystem))
	eventType := stableKeyPart(string(norm.EventType))
	sessionID := stableKeyPart(norm.SessionID)
	workspaceID := stableKeyPart(norm.WorkspaceID)

	occurredBucket := ""
	if !hasStableID && !norm.OccurredAt.IsZero() {
		occurredBucket = norm.OccurredAt.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano)
	}

	tokenTuple := ""
	costTuple := ""
	modelRaw := ""
	modelCanonical := ""
	toolName := ""
	if hasStableID {
		parts := []string{
			sourceSystem,
			eventType,
			sessionID,
			workspaceID,
			stableKeyPart(stableID),
		}
		return hashStrings(parts...)
	}

	if !hasStableID {
		tokenTuple = strings.Join([]string{
			int64TupleValue(norm.InputTokens),
			int64TupleValue(norm.OutputTokens),
			int64TupleValue(norm.ReasoningTokens),
			int64TupleValue(norm.CacheReadTokens),
			int64TupleValue(norm.CacheWriteTokens),
			int64TupleValue(norm.TotalTokens),
		}, ",")

		costTuple = strings.Join([]string{
			float64TupleValue(norm.CostUSD),
			int64TupleValue(norm.Requests),
		}, ",")

		modelRaw = norm.ModelRaw
		modelCanonical = norm.ModelCanonical
		toolName = norm.ToolName
	}

	parts := []string{
		sourceSystem,
		eventType,
		stableKeyPart(norm.ProviderID),
		stableKeyPart(norm.AccountID),
		stableKeyPart(norm.AgentName),
		sessionID,
		workspaceID,
		stableKeyPart(stableID),
		occurredBucket,
		tokenTuple,
		costTuple,
		modelRaw,
		modelCanonical,
		toolName,
	}
	return hashStrings(parts...)
}

func fallbackFingerprint(event IngestRequest) string {
	parts := []string{
		stableKeyPart(string(event.SourceSystem)),
		stableKeyPart(string(event.EventType)),
		stableKeyPart(event.ProviderID),
		stableKeyPart(event.AccountID),
		stableKeyPart(event.SessionID),
		stableKeyPart(event.WorkspaceID),
		event.OccurredAt.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano),
		stableKeyPart(event.ModelRaw),
		stableKeyPart(event.ModelCanonical),
		stableKeyPart(event.ToolName),
		int64TupleValue(event.InputTokens),
		int64TupleValue(event.OutputTokens),
		int64TupleValue(event.ReasoningTokens),
		int64TupleValue(event.CacheReadTokens),
		int64TupleValue(event.CacheWriteTokens),
		int64TupleValue(event.TotalTokens),
		float64TupleValue(event.CostUSD),
		int64TupleValue(event.Requests),
	}
	return hashStrings(parts...)
}

func hashStrings(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func int64TupleValue(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func float64TupleValue(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.6f", *v)
}

func stableKeyPart(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}
