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

func TestDetailSectionStyleConstants(t *testing.T) {
	tests := []struct {
		name  string
		style DetailSectionStyle
		want  string
	}{
		{"models", DetailSectionStyleModels, "models"},
		{"trends", DetailSectionStyleTrends, "trends"},
		{"usage", DetailSectionStyleUsage, "usage"},
		{"spending", DetailSectionStyleSpending, "spending"},
		{"tokens", DetailSectionStyleTokens, "tokens"},
		{"activity", DetailSectionStyleActivity, "activity"},
		{"list", DetailSectionStyleList, "list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.style); got != tt.want {
				t.Fatalf("style = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetailWidgetWithModelsAndTrends(t *testing.T) {
	w := DetailWidget{
		Sections: []DetailSection{
			{Name: "Usage", Order: 1, Style: DetailSectionStyleUsage},
			{Name: "Models", Order: 2, Style: DetailSectionStyleModels},
			{Name: "Trends", Order: 3, Style: DetailSectionStyleTrends},
		},
	}
	if got := w.SectionStyle("Models"); got != DetailSectionStyleModels {
		t.Fatalf("Models style = %q, want %q", got, DetailSectionStyleModels)
	}
	if got := w.SectionOrder("Models"); got != 2 {
		t.Fatalf("Models order = %d, want 2", got)
	}
	if got := w.SectionStyle("Trends"); got != DetailSectionStyleTrends {
		t.Fatalf("Trends style = %q, want %q", got, DetailSectionStyleTrends)
	}
	if got := w.SectionOrder("Trends"); got != 3 {
		t.Fatalf("Trends order = %d, want 3", got)
	}
}
