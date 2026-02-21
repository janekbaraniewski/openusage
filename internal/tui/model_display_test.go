package tui

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestComputeDisplayInfo_MapsActivityFallbackToUsage(t *testing.T) {
	msgs := 12.0
	snap := core.UsageSnapshot{
		ProviderID: "ollama",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"messages_today": {Used: &msgs, Unit: "messages", Window: "1d"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Usage" {
		t.Fatalf("tagLabel = %q, want Usage", got.tagLabel)
	}
	if got.tagEmoji != "⚡" {
		t.Fatalf("tagEmoji = %q, want ⚡", got.tagEmoji)
	}
	if !strings.Contains(got.summary, "msgs today") {
		t.Fatalf("summary = %q, want messages summary", got.summary)
	}
}

func TestComputeDisplayInfo_MapsGenericMetricsFallbackToUsage(t *testing.T) {
	custom := 7.0
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"custom_counter": {Used: &custom, Unit: "count"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Usage" {
		t.Fatalf("tagLabel = %q, want Usage", got.tagLabel)
	}
	if got.tagEmoji != "⚡" {
		t.Fatalf("tagEmoji = %q, want ⚡", got.tagEmoji)
	}
}

func TestComputeDisplayInfo_PreservesCreditsTag(t *testing.T) {
	total := 42.0
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"total_cost_usd": {Used: &total, Unit: "USD", Window: "all_time"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Credits" {
		t.Fatalf("tagLabel = %q, want Credits", got.tagLabel)
	}
	if got.tagEmoji != "💰" {
		t.Fatalf("tagEmoji = %q, want 💰", got.tagEmoji)
	}
}

func TestComputeDisplayInfo_PreservesErrorStatusTag(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "test",
		Status:     core.StatusError,
		Message:    "boom",
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if got.tagLabel != "Error" {
		t.Fatalf("tagLabel = %q, want Error", got.tagLabel)
	}
	if got.tagEmoji != "⚠" {
		t.Fatalf("tagEmoji = %q, want ⚠", got.tagEmoji)
	}
}
