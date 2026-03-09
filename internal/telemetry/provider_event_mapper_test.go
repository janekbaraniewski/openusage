package telemetry

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func TestMapProviderEvent_AccountFallsBackToSourceSystemBeforeProvider(t *testing.T) {
	ev := shared.TelemetryEvent{
		Channel:    shared.TelemetryChannelHook,
		OccurredAt: time.Date(2026, time.February, 22, 12, 0, 0, 0, time.UTC),
		ProviderID: "openrouter",
		EventType:  shared.TelemetryEventTypeMessageUsage,
		Status:     shared.TelemetryStatusOK,
	}

	req := mapProviderEvent("opencode", ev, "")
	if req.AccountID != "opencode" {
		t.Fatalf("account_id = %q, want opencode", req.AccountID)
	}
}

func TestMapProviderEvent_AccountOverrideWins(t *testing.T) {
	ev := shared.TelemetryEvent{
		Channel:    shared.TelemetryChannelHook,
		OccurredAt: time.Date(2026, time.February, 22, 12, 0, 0, 0, time.UTC),
		ProviderID: "openrouter",
		EventType:  shared.TelemetryEventTypeMessageUsage,
		Status:     shared.TelemetryStatusOK,
	}

	req := mapProviderEvent("opencode", ev, "workspace-a")
	if req.AccountID != "workspace-a" {
		t.Fatalf("account_id = %q, want workspace-a", req.AccountID)
	}
}

func TestMapProviderEvent_AccountFallsBackToSourceSystem(t *testing.T) {
	ev := shared.TelemetryEvent{
		Channel:    shared.TelemetryChannelHook,
		OccurredAt: time.Date(2026, time.February, 22, 12, 0, 0, 0, time.UTC),
		EventType:  shared.TelemetryEventTypeToolUsage,
		Status:     shared.TelemetryStatusOK,
	}

	req := mapProviderEvent("codex", ev, "")
	if req.AccountID != "codex" {
		t.Fatalf("account_id = %q, want codex", req.AccountID)
	}
}
