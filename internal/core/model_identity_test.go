package core

import "testing"

func TestNormalizeCanonicalModel_ClaudeLineage(t *testing.T) {
	cfg := DefaultModelNormalizationConfig()
	got := normalizeCanonicalModel("cursor", "claude-4.6-opus-high-thinking", cfg)

	if got.LineageID != "anthropic/claude-opus-4.6" {
		t.Fatalf("lineage = %q, want anthropic/claude-opus-4.6", got.LineageID)
	}
	if got.Confidence < 0.9 {
		t.Fatalf("confidence = %.2f, want >= 0.9", got.Confidence)
	}
}

func TestNormalizeCanonicalModel_OverrideWins(t *testing.T) {
	cfg := DefaultModelNormalizationConfig()
	cfg.Overrides = []ModelNormalizationOverride{
		{
			Provider:         "cursor",
			RawModelID:       "claude-4.6-opus-high-thinking",
			CanonicalLineage: "anthropic/claude-opus-4.6",
			CanonicalRelease: "anthropic/claude-opus-4.6@20260219",
		},
	}

	got := normalizeCanonicalModel("cursor", "claude-4.6-opus-high-thinking", cfg)
	if got.LineageID != "anthropic/claude-opus-4.6" {
		t.Fatalf("lineage = %q", got.LineageID)
	}
	if got.ReleaseID != "anthropic/claude-opus-4.6@20260219" {
		t.Fatalf("release = %q", got.ReleaseID)
	}
	if got.Confidence != 1 {
		t.Fatalf("confidence = %.2f, want 1", got.Confidence)
	}
	if got.Reason != "override" {
		t.Fatalf("reason = %q, want override", got.Reason)
	}
}

func TestNormalizeUsageSnapshotWithConfig_BuildsModelUsage(t *testing.T) {
	cfg := DefaultModelNormalizationConfig()
	inp := 1000.0
	out := 250.0
	s := UsageSnapshot{
		ProviderID: "claude_code",
		AccountID:  "local",
		Metrics: map[string]Metric{
			"model_claude_opus_4_6_input_tokens":  {Used: &inp, Unit: "tokens", Window: "7d"},
			"model_claude_opus_4_6_output_tokens": {Used: &out, Unit: "tokens", Window: "7d"},
		},
	}

	got := NormalizeUsageSnapshotWithConfig(s, cfg)
	if len(got.ModelUsage) != 1 {
		t.Fatalf("model_usage len = %d, want 1", len(got.ModelUsage))
	}
	rec := got.ModelUsage[0]
	if rec.CanonicalLineageID != "anthropic/claude-opus-4.6" {
		t.Fatalf("canonical lineage = %q", rec.CanonicalLineageID)
	}
	if rec.TotalTokens == nil || *rec.TotalTokens != 1250 {
		t.Fatalf("total tokens = %v", rec.TotalTokens)
	}
	if rec.Dimensions["provider_id"] != "claude_code" {
		t.Fatalf("provider dimension = %q", rec.Dimensions["provider_id"])
	}
}
