package core

import "testing"

func TestExtractAnalyticsModelSeries_PrefersCanonicalModelSeries(t *testing.T) {
	series := map[string][]TimePoint{
		"tokens_total":               {{Date: "2026-04-10", Value: 500}},
		"tokens_client_cli":          {{Date: "2026-04-10", Value: 200}},
		"tokens_model_gpt_5_codex":   {{Date: "2026-04-10", Value: 300}},
		"usage_model_gpt_5_fallback": {{Date: "2026-04-10", Value: 100}},
	}

	got := ExtractAnalyticsModelSeries(series)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %#v", len(got), got)
	}
	if got[0].Name != "gpt_5_codex" {
		t.Fatalf("name = %q, want gpt_5_codex", got[0].Name)
	}
}

func TestExtractAnalyticsModelSeries_ExcludesClientTokenSeriesFromLegacyFallback(t *testing.T) {
	series := map[string][]TimePoint{
		"tokens_total":         {{Date: "2026-04-10", Value: 500}},
		"tokens_client_cli":    {{Date: "2026-04-10", Value: 200}},
		"tokens_gpt_5_4":       {{Date: "2026-04-10", Value: 300}},
		"tokens_claude_opus_4": {{Date: "2026-04-11", Value: 100}},
	}

	got := ExtractAnalyticsModelSeries(series)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	for _, named := range got {
		if named.Name == "client_cli" || named.Name == "total" {
			t.Fatalf("unexpected non-model series: %#v", got)
		}
	}
}
