package tui

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestSnapshotMetaEntries_IncludesDiagnostics(t *testing.T) {
	snap := core.UsageSnapshot{
		Attributes: map[string]string{
			"tier": "paid",
		},
		Diagnostics: map[string]string{
			"telemetry_unmapped_providers": "anthropic,openai",
		},
		Raw: map[string]string{
			"raw_only": "x",
		},
	}

	meta := snapshotMetaEntries(snap)
	if got := meta["tier"]; got != "paid" {
		t.Fatalf("tier = %q, want paid", got)
	}
	if got := meta["telemetry_unmapped_providers"]; got != "anthropic,openai" {
		t.Fatalf("telemetry_unmapped_providers = %q, want anthropic,openai", got)
	}
	if got := meta["raw_only"]; got != "x" {
		t.Fatalf("raw_only = %q, want x", got)
	}
}
