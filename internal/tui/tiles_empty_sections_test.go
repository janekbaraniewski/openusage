package tui

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestEmptyTileSectionContent_AllSectionsHaveStandardNoData(t *testing.T) {
	widget := core.DefaultDashboardWidget()
	for _, section := range core.DashboardStandardSections() {
		if section == core.DashboardSectionHeader {
			continue
		}
		heading, message := emptyTileSectionContent(section, widget)
		if strings.TrimSpace(heading) == "" {
			t.Fatalf("section %q has empty no-data heading", section)
		}
		if strings.TrimSpace(message) == "" {
			t.Fatalf("section %q has empty no-data message", section)
		}
	}
}

func TestRenderTile_NoDataSectionsShownOrHiddenBySetting(t *testing.T) {
	snap := core.UsageSnapshot{
		AccountID:  "gemini-cli",
		ProviderID: "gemini_cli",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"requests_today": {Used: float64Ptr(3), Unit: "requests", Window: "1d"},
		},
	}

	shown := Model{
		timeWindow:             core.TimeWindow7d,
		hideSectionsWithNoData: false,
	}
	shownTile := stripANSI(shown.renderTile(snap, false, false, 100, 0, 0))
	if !strings.Contains(shownTile, "No model data for this time range") {
		t.Fatalf("expected no-data model section to be visible, got: %q", shownTile)
	}
	if !strings.Contains(shownTile, "No code stats for this time range") {
		t.Fatalf("expected no-data code stats section to be visible, got: %q", shownTile)
	}

	hidden := Model{
		timeWindow:             core.TimeWindow7d,
		hideSectionsWithNoData: true,
	}
	hiddenTile := stripANSI(hidden.renderTile(snap, false, false, 100, 0, 0))
	if strings.Contains(hiddenTile, "No model data for this time range") {
		t.Fatalf("expected no-data model section to be hidden, got: %q", hiddenTile)
	}
	if strings.Contains(hiddenTile, "No code stats for this time range") {
		t.Fatalf("expected no-data code stats section to be hidden, got: %q", hiddenTile)
	}
}
