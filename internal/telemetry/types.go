package telemetry

import (
	"encoding/json"
	"time"
)

type SourceSystem string

const (
	SourceSystemPoller SourceSystem = "provider_poller"
)

type SourceChannel string

const (
	SourceChannelHook   SourceChannel = "hook"
	SourceChannelSSE    SourceChannel = "sse"
	SourceChannelJSONL  SourceChannel = "jsonl"
	SourceChannelAPI    SourceChannel = "api"
	SourceChannelSQLite SourceChannel = "sqlite"
)

type EventType string

const (
	EventTypeTurnCompleted   EventType = "turn_completed"
	EventTypeMessageUsage    EventType = "message_usage"
	EventTypeToolUsage       EventType = "tool_usage"
	EventTypeRawEnvelope     EventType = "raw_envelope"
	EventTypeLimitSnapshot   EventType = "limit_snapshot"
	EventTypeReconcileAdjust EventType = "reconcile_adjustment"
)

type EventStatus string

const (
	EventStatusOK      EventStatus = "ok"
	EventStatusError   EventStatus = "error"
	EventStatusAborted EventStatus = "aborted"
	EventStatusUnknown EventStatus = "unknown"
)

const DefaultNormalizationVersion = "v1"

// IngestRequest is the normalized contract used by local adapters and workers
// before writing to the telemetry store.
type IngestRequest struct {
	SourceSystem        SourceSystem  `json:"source_system"`
	SourceChannel       SourceChannel `json:"source_channel"`
	SourceSchemaVersion string        `json:"source_schema_version"`
	OccurredAt          time.Time     `json:"occurred_at"`
	WorkspaceID         string        `json:"workspace_id,omitempty"`
	SessionID           string        `json:"session_id,omitempty"`
	TurnID              string        `json:"turn_id,omitempty"`
	MessageID           string        `json:"message_id,omitempty"`
	ToolCallID          string        `json:"tool_call_id,omitempty"`
	ProviderID          string        `json:"provider_id,omitempty"`
	AccountID           string        `json:"account_id,omitempty"`

	AgentName            string      `json:"agent_name,omitempty"`
	EventType            EventType   `json:"event_type,omitempty"`
	ModelRaw             string      `json:"model_raw,omitempty"`
	ModelCanonical       string      `json:"model_canonical,omitempty"`
	ModelLineageID       string      `json:"model_lineage_id,omitempty"`
	InputTokens          *int64      `json:"input_tokens,omitempty"`
	OutputTokens         *int64      `json:"output_tokens,omitempty"`
	ReasoningTokens      *int64      `json:"reasoning_tokens,omitempty"`
	CacheReadTokens      *int64      `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens     *int64      `json:"cache_write_tokens,omitempty"`
	TotalTokens          *int64      `json:"total_tokens,omitempty"`
	CostUSD              *float64    `json:"cost_usd,omitempty"`
	Requests             *int64      `json:"requests,omitempty"`
	ToolName             string      `json:"tool_name,omitempty"`
	Status               EventStatus `json:"status,omitempty"`
	NormalizationVersion string      `json:"normalization_version,omitempty"`
	Payload              any         `json:"payload,omitempty"`
}

type CanonicalEvent struct {
	EventID string `json:"event_id"`

	OccurredAt           time.Time   `json:"occurred_at"`
	ProviderID           string      `json:"provider_id,omitempty"`
	AgentName            string      `json:"agent_name"`
	AccountID            string      `json:"account_id,omitempty"`
	WorkspaceID          string      `json:"workspace_id,omitempty"`
	SessionID            string      `json:"session_id,omitempty"`
	TurnID               string      `json:"turn_id,omitempty"`
	MessageID            string      `json:"message_id,omitempty"`
	ToolCallID           string      `json:"tool_call_id,omitempty"`
	EventType            EventType   `json:"event_type"`
	ModelRaw             string      `json:"model_raw,omitempty"`
	ModelCanonical       string      `json:"model_canonical,omitempty"`
	ModelLineageID       string      `json:"model_lineage_id,omitempty"`
	InputTokens          *int64      `json:"input_tokens,omitempty"`
	OutputTokens         *int64      `json:"output_tokens,omitempty"`
	ReasoningTokens      *int64      `json:"reasoning_tokens,omitempty"`
	CacheReadTokens      *int64      `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens     *int64      `json:"cache_write_tokens,omitempty"`
	TotalTokens          *int64      `json:"total_tokens,omitempty"`
	CostUSD              *float64    `json:"cost_usd,omitempty"`
	Requests             *int64      `json:"requests,omitempty"`
	ToolName             string      `json:"tool_name,omitempty"`
	Status               EventStatus `json:"status"`
	DedupKey             string      `json:"dedup_key"`
	RawEventID           string      `json:"raw_event_id"`
	NormalizationVersion string      `json:"normalization_version"`
}

type IngestResult struct {
	Status     string `json:"status"`
	Deduped    bool   `json:"deduped"`
	EventID    string `json:"event_id"`
	RawEventID string `json:"raw_event_id"`
}

func normalizeRequest(req IngestRequest, now time.Time) IngestRequest {
	norm := req
	if norm.OccurredAt.IsZero() {
		norm.OccurredAt = now
	}
	norm.OccurredAt = norm.OccurredAt.UTC()

	if norm.AgentName == "" {
		norm.AgentName = string(norm.SourceSystem)
	}
	if norm.EventType == "" {
		norm.EventType = EventTypeMessageUsage
	}
	if norm.Status == "" {
		norm.Status = EventStatusOK
	}
	if norm.NormalizationVersion == "" {
		norm.NormalizationVersion = DefaultNormalizationVersion
	}
	if norm.SourceSchemaVersion == "" {
		norm.SourceSchemaVersion = "v1"
	}
	if norm.TotalTokens == nil {
		total := int64(0)
		hasAny := false
		for _, v := range []*int64{norm.InputTokens, norm.OutputTokens, norm.ReasoningTokens, norm.CacheReadTokens, norm.CacheWriteTokens} {
			if v != nil {
				total += *v
				hasAny = true
			}
		}
		if hasAny {
			norm.TotalTokens = &total
		}
	}
	return norm
}

func marshalPayload(payload any) ([]byte, error) {
	if payload == nil {
		return []byte("{}"), nil
	}
	switch v := payload.(type) {
	case json.RawMessage:
		if len(v) == 0 {
			return []byte("{}"), nil
		}
		return v, nil
	case []byte:
		if len(v) == 0 {
			return []byte("{}"), nil
		}
		return v, nil
	default:
		return json.Marshal(payload)
	}
}
