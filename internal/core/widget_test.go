package core

import "testing"

func TestDefaultDashboardWidget_StandardSectionOrder(t *testing.T) {
	w := DefaultDashboardWidget()
	got := w.EffectiveStandardSectionOrder()
	want := []DashboardStandardSection{
		DashboardSectionHeader,
		DashboardSectionTopUsageProgress,
		DashboardSectionModelBurn,
		DashboardSectionClientBurn,
		DashboardSectionToolUsage,
		DashboardSectionMCPUsage,
		DashboardSectionLanguageBurn,
		DashboardSectionCodeStats,
		DashboardSectionDailyUsage,
		DashboardSectionProviderBurn,
		DashboardSectionUpstreamProviders,
		DashboardSectionOtherData,
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

func TestDashboardWidget_EffectiveStandardSectionOrderNormalizesLegacyToolUsageAlias(t *testing.T) {
	w := DashboardWidget{
		StandardSectionOrder: []DashboardStandardSection{
			DashboardSectionActualToolUsage,
			DashboardSectionToolUsage,
			DashboardSectionOtherData,
		},
	}

	got := w.EffectiveStandardSectionOrder()
	want := []DashboardStandardSection{
		DashboardSectionToolUsage,
		DashboardSectionOtherData,
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

func TestDashboardWidget_EffectiveStandardSectionOrderFiltersUnknownAndDuplicates(t *testing.T) {
	w := DashboardWidget{
		StandardSectionOrder: []DashboardStandardSection{
			DashboardSectionTopUsageProgress,
			DashboardStandardSection("unknown_section"),
			DashboardSectionTopUsageProgress,
			DashboardSectionOtherData,
		},
	}

	got := w.EffectiveStandardSectionOrder()
	want := []DashboardStandardSection{
		DashboardSectionTopUsageProgress,
		DashboardSectionOtherData,
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

func TestDashboardStandardSections_ReturnsCanonicalOrderedCopy(t *testing.T) {
	got := DashboardStandardSections()
	want := DefaultDashboardWidget().EffectiveStandardSectionOrder()

	if len(got) != len(want) {
		t.Fatalf("section count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("section[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	got[0] = DashboardSectionOtherData
	again := DashboardStandardSections()
	if again[0] != DashboardSectionHeader {
		t.Fatalf("DashboardStandardSections should return a copy; first = %q", again[0])
	}
}

func TestIsKnownDashboardStandardSection(t *testing.T) {
	if !IsKnownDashboardStandardSection(DashboardSectionTopUsageProgress) {
		t.Fatal("expected top_usage_progress to be known")
	}
	if IsKnownDashboardStandardSection(DashboardStandardSection("nope")) {
		t.Fatal("unexpected unknown section marked as known")
	}
}
