package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
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

func TestComputeDisplayInfo_FallbackSkipsDerivedMetrics(t *testing.T) {
	derived := 999.0
	coreRPM := 42.0
	snap := core.UsageSnapshot{
		ProviderID: "copilot",
		Status:     core.StatusUnknown,
		Metrics: map[string]core.Metric{
			"model_gpt_5_tokens":  {Used: &derived, Unit: "tokens"},
			"client_cli_requests": {Used: &derived, Unit: "requests"},
			"gh_core_rpm":         {Used: &coreRPM, Unit: "rpm"},
		},
	}

	got := computeDisplayInfo(snap, core.DefaultDashboardWidget())
	if !strings.Contains(strings.ToLower(got.summary), "core rpm") {
		t.Fatalf("summary = %q, want core rpm fallback metric", got.summary)
	}
}

func TestSnapshotsReady(t *testing.T) {
	if snapshotsReady(nil) {
		t.Fatal("snapshotsReady(nil) = true, want false")
	}

	notReady := map[string]core.UsageSnapshot{
		"a": {
			Status:      core.StatusUnknown,
			Metrics:     map[string]core.Metric{},
			Resets:      map[string]time.Time{},
			DailySeries: map[string][]core.TimePoint{},
		},
	}
	if snapshotsReady(notReady) {
		t.Fatal("snapshotsReady(notReady) = true, want false")
	}

	messageOnly := map[string]core.UsageSnapshot{
		"a": {
			Status:  core.StatusUnknown,
			Message: "connecting to telemetry daemon...",
		},
	}
	if snapshotsReady(messageOnly) {
		t.Fatal("snapshotsReady(messageOnly) = true, want false")
	}

	ready := map[string]core.UsageSnapshot{
		"a": {
			Status: core.StatusUnknown,
			Metrics: map[string]core.Metric{
				"messages_today": {Used: float64Ptr(1), Unit: "messages"},
			},
		},
	}
	if !snapshotsReady(ready) {
		t.Fatal("snapshotsReady(ready) = false, want true")
	}
}

func TestUpdate_SnapshotsMsgMarksModelReadyOnFirstFrame(t *testing.T) {
	m := NewModel(0.2, 0.1, false, config.DashboardConfig{}, nil)
	if m.hasData {
		t.Fatal("expected hasData=false on fresh model")
	}

	snaps := SnapshotsMsg{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Status:     core.StatusUnknown,
			Message:    "daemon warming up",
			Metrics:    map[string]core.Metric{},
		},
	}

	updated, _ := m.Update(snaps)
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updated)
	}
	if !got.hasData {
		t.Fatal("expected hasData=true after first snapshots frame")
	}
}
