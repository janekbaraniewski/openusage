package core

import "testing"

func TestExtractModelBreakdownUnsplitTotal(t *testing.T) {
	snap := UsageSnapshot{Metrics: map[string]Metric{
		// Reported only as a total: the total is the model's volume.
		"model_glm-5_3_total_tokens": {Used: Float64Ptr(900)},
		// Reported with a split: the total must not be added on top.
		"model_other_total_tokens":  {Used: Float64Ptr(500)},
		"model_other_input_tokens":  {Used: Float64Ptr(300)},
		"model_other_output_tokens": {Used: Float64Ptr(200)},
	}}

	entries, used := ExtractModelBreakdown(snap)

	byName := map[string]ModelBreakdownEntry{}
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	if got := byName["glm-5_3"]; got.Unsplit != 900 || got.TotalTokens() != 900 {
		t.Errorf("total-only model = %+v, want 900 unsplit tokens", got)
	}
	if got := byName["other"]; got.Unsplit != 0 || got.TotalTokens() != 500 {
		t.Errorf("split model = %+v, want 500 tokens and no unsplit total", got)
	}
	if !used["model_glm-5_3_total_tokens"] || used["model_other_total_tokens"] {
		t.Errorf("used keys = %v", used)
	}
}
