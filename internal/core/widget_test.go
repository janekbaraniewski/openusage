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
		DashboardSectionDailyUsage,
		DashboardSectionProviderBurn,
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
