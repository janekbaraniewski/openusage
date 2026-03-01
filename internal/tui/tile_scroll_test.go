package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestRenderTile_ShowsInternalScrollIndicatorsWhenContentHidden(t *testing.T) {
	metrics := make(map[string]core.Metric)
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("metric_%02d", i)
		metrics[key] = core.Metric{
			Used: float64Ptr(float64(i + 1)),
			Unit: "count",
		}
	}

	m := Model{timeWindow: core.TimeWindow30d}
	snap := core.UsageSnapshot{
		AccountID:  "acct",
		ProviderID: "openai",
		Status:     core.StatusOK,
		Metrics:    metrics,
	}

	top := m.renderTile(snap, true, false, 76, 12, 0)
	if !strings.Contains(top, "↕") || !strings.Contains(top, "▼") {
		t.Fatalf("expected vertical scrollbar indicator, got:\n%s", top)
	}

	scrolled := m.renderTile(snap, true, false, 76, 12, 4)
	if !strings.Contains(scrolled, "↕") || !strings.Contains(scrolled, "▲") {
		t.Fatalf("expected vertical scrollbar/top indicator after offset, got:\n%s", scrolled)
	}
}
