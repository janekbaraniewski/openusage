package tui

import (
	"testing"

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

