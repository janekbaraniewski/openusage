package core

import "testing"

func TestBuildModelUsageFromSnapshotMetrics(t *testing.T) {
	inp := 1200.0
	out := 300.0
	cost := 4.5

	snap := UsageSnapshot{
		ProviderID: "cursor",
		AccountID:  "cursor-work",
		Metrics: map[string]Metric{
			"model_claude-4.6-opus-high-thinking_input_tokens":  {Used: &inp, Unit: "tokens", Window: "billing-cycle"},
			"model_claude-4.6-opus-high-thinking_output_tokens": {Used: &out, Unit: "tokens", Window: "billing-cycle"},
			"model_claude-4.6-opus-high-thinking_cost":          {Used: &cost, Unit: "USD", Window: "billing-cycle"},
		},
	}

	records := BuildModelUsageFromSnapshotMetrics(snap)
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}

	rec := records[0]
	if rec.RawModelID != "claude-4.6-opus-high-thinking" {
		t.Fatalf("raw model = %q", rec.RawModelID)
	}
	if rec.Window != "billing-cycle" {
		t.Fatalf("window = %q", rec.Window)
	}
	if rec.InputTokens == nil || *rec.InputTokens != 1200 {
		t.Fatalf("input tokens = %v", rec.InputTokens)
	}
	if rec.OutputTokens == nil || *rec.OutputTokens != 300 {
		t.Fatalf("output tokens = %v", rec.OutputTokens)
	}
	if rec.TotalTokens == nil || *rec.TotalTokens != 1500 {
		t.Fatalf("total tokens = %v", rec.TotalTokens)
	}
	if rec.CostUSD == nil || *rec.CostUSD != 4.5 {
		t.Fatalf("cost = %v", rec.CostUSD)
	}
}
