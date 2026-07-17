package daemon

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestPollSnapshotsForIngestExcludesUnchangedCachedResults(t *testing.T) {
	results := []pollProviderResult{
		{
			accountID:    "cached",
			snapshot:     core.UsageSnapshot{ProviderID: "codex", AccountID: "cached"},
			shouldIngest: false,
		},
		{
			accountID:    "changed",
			snapshot:     core.UsageSnapshot{ProviderID: "openai", AccountID: "changed"},
			shouldIngest: true,
		},
	}

	got := pollSnapshotsForIngest(results)
	if _, ok := got["cached"]; ok {
		t.Fatal("unchanged cached snapshot should not be re-ingested")
	}
	if _, ok := got["changed"]; !ok {
		t.Fatal("changed snapshot should be ingested")
	}
}
