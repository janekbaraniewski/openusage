package main

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestStabilizeReadModelSnapshots_PreservesPreviousOnDegradedFrame(t *testing.T) {
	used := 42.0
	prev := map[string]core.UsageSnapshot{
		"codex-cli": {
			ProviderID: "codex",
			AccountID:  "codex-cli",
			Status:     core.StatusOK,
			Metrics: map[string]core.Metric{
				"messages_today": {Used: &used, Unit: "messages"},
			},
		},
	}
	current := map[string]core.UsageSnapshot{
		"codex-cli": {
			ProviderID: "codex",
			AccountID:  "codex-cli",
			Status:     core.StatusUnknown,
			Metrics:    map[string]core.Metric{},
		},
	}

	out := stabilizeReadModelSnapshots(current, prev)
	got := out["codex-cli"]
	if got.Status != core.StatusOK {
		t.Fatalf("status = %q, want %q", got.Status, core.StatusOK)
	}
	if gotMetric := got.Metrics["messages_today"]; gotMetric.Used == nil || *gotMetric.Used != 42 {
		t.Fatalf("messages_today = %+v, want 42", gotMetric)
	}
}

func TestStabilizeReadModelSnapshots_UsesCurrentWhenNotDegraded(t *testing.T) {
	prev := map[string]core.UsageSnapshot{
		"copilot": {
			ProviderID: "copilot",
			AccountID:  "copilot",
			Status:     core.StatusUnknown,
		},
	}
	used := 10.0
	current := map[string]core.UsageSnapshot{
		"copilot": {
			ProviderID: "copilot",
			AccountID:  "copilot",
			Status:     core.StatusUnknown,
			Metrics: map[string]core.Metric{
				"messages_today": {Used: &used, Unit: "messages"},
			},
		},
	}

	out := stabilizeReadModelSnapshots(current, prev)
	got := out["copilot"]
	if gotMetric := got.Metrics["messages_today"]; gotMetric.Used == nil || *gotMetric.Used != 10 {
		t.Fatalf("messages_today = %+v, want 10", gotMetric)
	}
}
