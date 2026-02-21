package core

import "testing"

func TestDefaultDetailWidget(t *testing.T) {
	w := DefaultDetailWidget()
	if got := w.SectionStyle("Usage"); got != DetailSectionStyleUsage {
		t.Fatalf("Usage style = %q, want %q", got, DetailSectionStyleUsage)
	}
	if got := w.SectionOrder("Usage"); got != 1 {
		t.Fatalf("Usage order = %d, want 1", got)
	}
	if got := w.SectionStyle("Unknown"); got != DetailSectionStyleList {
		t.Fatalf("Unknown style = %q, want %q", got, DetailSectionStyleList)
	}
	if got := w.SectionOrder("Unknown"); got != 0 {
		t.Fatalf("Unknown order = %d, want 0", got)
	}
}
