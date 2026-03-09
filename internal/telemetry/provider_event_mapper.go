package telemetry

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func mapProviderEvent(sourceSystem string, ev shared.TelemetryEvent, accountOverride string) IngestRequest {
	req := IngestRequest{
		SourceSystem:        SourceSystem(sourceSystem),
		SourceChannel:       mapProviderChannel(ev.Channel),
		SourceSchemaVersion: core.FirstNonEmpty(ev.SchemaVersion, "v1"),
		OccurredAt:          ev.OccurredAt,
		WorkspaceID:         ev.WorkspaceID,
		SessionID:           ev.SessionID,
		TurnID:              ev.TurnID,
		MessageID:           ev.MessageID,
		ToolCallID:          ev.ToolCallID,
		ProviderID:          ev.ProviderID,
		AccountID:           core.FirstNonEmpty(accountOverride, ev.AccountID, sourceSystem, ev.ProviderID),
		AgentName:           core.FirstNonEmpty(ev.AgentName, sourceSystem),
		EventType:           mapProviderEventType(ev.EventType),
		ModelRaw:            ev.ModelRaw,
		TokenUsage:          ev.TokenUsage,
		ToolName:            ev.ToolName,
		Status:              mapProviderStatus(ev.Status),
		Payload:             ev.Payload,
	}
	if req.AccountID == "" {
		req.AccountID = "default"
	}
	return req
}

func mapProviderChannel(channel shared.TelemetryChannel) SourceChannel {
	switch channel {
	case shared.TelemetryChannelHook:
		return SourceChannelHook
	case shared.TelemetryChannelSSE:
		return SourceChannelSSE
	case shared.TelemetryChannelAPI:
		return SourceChannelAPI
	case shared.TelemetryChannelSQLite:
		return SourceChannelSQLite
	default:
		return SourceChannelJSONL
	}
}

func mapProviderEventType(t shared.TelemetryEventType) EventType {
	switch t {
	case shared.TelemetryEventTypeTurnCompleted:
		return EventTypeTurnCompleted
	case shared.TelemetryEventTypeToolUsage:
		return EventTypeToolUsage
	case shared.TelemetryEventTypeRawEnvelope:
		return EventTypeRawEnvelope
	default:
		return EventTypeMessageUsage
	}
}

func mapProviderStatus(s shared.TelemetryStatus) EventStatus {
	switch s {
	case shared.TelemetryStatusError:
		return EventStatusError
	case shared.TelemetryStatusAborted:
		return EventStatusAborted
	case shared.TelemetryStatusUnknown:
		return EventStatusUnknown
	default:
		return EventStatusOK
	}
}
