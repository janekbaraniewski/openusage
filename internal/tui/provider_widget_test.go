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
	want := []core.DashboardStandardSection{
		core.DashboardSectionOtherData,
		core.DashboardSectionModelBurn,
		core.DashboardSectionClientBurn,
		core.DashboardSectionProjectBreakdown,
		core.DashboardSectionToolUsage,
		core.DashboardSectionMCPUsage,
		core.DashboardSectionLanguageBurn,
		core.DashboardSectionCodeStats,
		core.DashboardSectionDailyUsage,
		core.DashboardSectionProviderBurn,
		core.DashboardSectionUpstreamProviders,
	}
	if len(got) != len(want) {
		t.Fatalf("effective section count = %d, want %d (sections=%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("effective section[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAPIKeyEnvLabelForProvider_IncludesAliases(t *testing.T) {
	tests := []struct {
		providerID string
		want       string
	}{
		{providerID: "opencode", want: "OPENCODE_API_KEY / ZEN_API_KEY"},
		{providerID: "gemini_api", want: "GEMINI_API_KEY / GOOGLE_API_KEY"},
		{providerID: "zai", want: "ZAI_API_KEY / ZHIPUAI_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.providerID, func(t *testing.T) {
			if got := apiKeyEnvLabelForProvider(tt.providerID); got != tt.want {
				t.Fatalf("apiKeyEnvLabelForProvider(%q) = %q, want %q", tt.providerID, got, tt.want)
			}
		})
	}
}

func TestResolvedAPIKeyEnvForProvider_PrefersConfiguredAlias(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("ZEN_API_KEY", "zen-test-key")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "google-test-key")
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("ZHIPUAI_API_KEY", "zhipu-test-key")

	tests := []struct {
		providerID string
		want       string
	}{
		{providerID: "opencode", want: "ZEN_API_KEY"},
		{providerID: "gemini_api", want: "GOOGLE_API_KEY"},
		{providerID: "zai", want: "ZHIPUAI_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.providerID, func(t *testing.T) {
			if got := resolvedAPIKeyEnvForProvider(tt.providerID); got != tt.want {
				t.Fatalf("resolvedAPIKeyEnvForProvider(%q) = %q, want %q", tt.providerID, got, tt.want)
			}
			if !hasConfiguredAPIKeyEnv(tt.providerID) {
				t.Fatalf("hasConfiguredAPIKeyEnv(%q) = false, want true", tt.providerID)
			}
		})
	}
}

func TestResolvedAPIKeyEnvForProvider_FallsBackToPrimary(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("ZEN_API_KEY", "")

	if got := resolvedAPIKeyEnvForProvider("opencode"); got != "OPENCODE_API_KEY" {
		t.Fatalf("resolvedAPIKeyEnvForProvider(opencode) = %q, want OPENCODE_API_KEY", got)
	}
	if hasConfiguredAPIKeyEnv("opencode") {
		t.Fatal("hasConfiguredAPIKeyEnv(opencode) = true, want false")
	}
}
