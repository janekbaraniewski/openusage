package tui

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestDashboardWidget_AppliesSectionOverride(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	setDashboardWidgetSectionOverrides([]core.DashboardStandardSection{
		core.DashboardSectionOtherData,
		core.DashboardSectionTopUsageProgress,
	})

	got := dashboardWidget("openai").EffectiveStandardSectionOrder()
	want := []core.DashboardStandardSection{
		core.DashboardSectionOtherData,
		core.DashboardSectionTopUsageProgress,
	}
	if len(got) != len(want) {
		t.Fatalf("section count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("section[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDashboardWidget_AppliesGlobalOverrideToAllProviders(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	setDashboardWidgetSectionOverrides([]core.DashboardStandardSection{
		core.DashboardSectionOtherData,
	})

	for _, providerID := range []string{"openai", "claude_code", "openrouter"} {
		got := dashboardWidget(providerID).EffectiveStandardSectionOrder()
		if len(got) != 1 || got[0] != core.DashboardSectionOtherData {
			t.Fatalf("provider %q effective section order = %#v, want [other_data]", providerID, got)
		}
	}
}

func TestSetDashboardWidgetSectionOverrides_NormalizesInvalidValues(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	setDashboardWidgetSectionOverrides([]core.DashboardStandardSection{
		core.DashboardSectionTopUsageProgress,
		core.DashboardStandardSection("unknown"),
		core.DashboardSectionTopUsageProgress,
		core.DashboardSectionOtherData,
	})

	got := dashboardWidget("openai").EffectiveStandardSectionOrder()
	want := []core.DashboardStandardSection{
		core.DashboardSectionTopUsageProgress,
		core.DashboardSectionOtherData,
	}
	if len(got) != len(want) {
		t.Fatalf("section count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("section[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNewModel_AppliesWidgetSectionOverridesFromConfig(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	_ = NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{
			WidgetSections: []config.DashboardWidgetSection{
				{
					ID:      core.DashboardSectionOtherData,
					Enabled: true,
				},
				{
					ID:      core.DashboardSectionTopUsageProgress,
					Enabled: false,
				},
			},
		},
		[]core.AccountConfig{
			{ID: "openai", Provider: "openai"},
		},
		core.TimeWindow7d,
	)

	got := dashboardWidget("openai").EffectiveStandardSectionOrder()
	if len(got) != 1 || got[0] != core.DashboardSectionOtherData {
		t.Fatalf("effective section order = %#v, want [other_data]", got)
	}
}
