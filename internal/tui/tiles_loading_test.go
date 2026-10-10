package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestTileShouldRenderLoading_MetadataOnlySnapshot(t *testing.T) {
	m := Model{}
	snap := core.UsageSnapshot{
		Status: core.StatusUnknown,
		Attributes: map[string]string{
			"account": "test@example.com",
		},
	}

	if !m.tileShouldRenderLoading(snap) {
		t.Fatal("tileShouldRenderLoading(metadata-only) = false, want true")
	}
}

func TestTileShouldRenderLoading_WithUsageData(t *testing.T) {
	m := Model{}
	snap := core.UsageSnapshot{
		Status: core.StatusUnknown,
		Metrics: map[string]core.Metric{
			"requests_today": {Used: float64Ptr(1), Unit: "requests"},
		},
	}

	if m.tileShouldRenderLoading(snap) {
		t.Fatal("tileShouldRenderLoading(with metrics) = true, want false")
	}
}

func TestTileShouldRenderLoading_ErrorStatus(t *testing.T) {
	m := Model{}
	snap := core.UsageSnapshot{
		Status:  core.StatusError,
		Message: "failed",
	}

	if m.tileShouldRenderLoading(snap) {
		t.Fatal("tileShouldRenderLoading(error) = true, want false")
	}
}

// Regression: an opencode account authenticated with only a Zen API key
// (no browser session, no telemetry events in the current window) reports
// StatusOK with attributes but zero metrics. It must render its normal body
// with empty-state sections, not the branded "syncing" loader.
func TestTileShouldRenderLoading_ResolvedSnapshotWithoutMetrics(t *testing.T) {
	m := Model{}
	for _, status := range []core.Status{core.StatusOK, core.StatusNearLimit, core.StatusUnsupported} {
		snap := core.UsageSnapshot{
			ProviderID: "opencode",
			AccountID:  "opencode",
			Status:     status,
			Message:    "Auth OK · 82 Zen models",
			Metrics:    map[string]core.Metric{},
			Attributes: map[string]string{
				"auth_scope":             "zen",
				"available_models_count": "82",
			},
		}
		if m.tileShouldRenderLoading(snap) {
			t.Fatalf("tileShouldRenderLoading(status=%s, no metrics) = true, want false", status)
		}
	}
}

func TestRenderTile_OpencodeAPIKeyOnlySnapshotSkipsLoader(t *testing.T) {
	m := Model{}
	snap := core.UsageSnapshot{
		ProviderID: "opencode",
		AccountID:  "opencode",
		Timestamp:  time.Now(),
		Status:     core.StatusOK,
		Message:    "Auth OK · 82 Zen models",
		Metrics:    map[string]core.Metric{},
		Attributes: map[string]string{
			"auth_scope":             "zen",
			"available_models_count": "82",
		},
	}

	out := m.renderTile(snap, false, false, 80, 0, 0)
	loader := strings.Split(ASCIIBanner(m.animFrame), "\n")[0]
	if strings.Contains(out, loader) {
		t.Fatalf("resolved opencode tile rendered the loading banner:\n%s", out)
	}
	if !strings.Contains(out, "No model data for this time range") {
		t.Fatalf("resolved opencode tile missing empty-state sections:\n%s", out)
	}
}
