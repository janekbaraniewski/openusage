package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

	occurredBucket := ""
	if !hasStableID && !norm.OccurredAt.IsZero() {
		occurredBucket = norm.OccurredAt.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano)
	}

	tokenTuple := ""
	costTuple := ""
	modelRaw := ""
	modelCanonical := ""
	toolName := ""
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
		norm.AgentName,
		norm.ProviderID,
		norm.AccountID,
		norm.SessionID,
		stableID,
		string(norm.EventType),
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
		event.OccurredAt.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano),
		event.ModelRaw,
		event.ModelCanonical,
		event.ToolName,
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
	return fmt.Sprintf("%d", *v)
}

func float64TupleValue(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.6f", *v)
}
