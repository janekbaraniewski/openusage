package main

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestApplyEnrich_ScopesToAccount(t *testing.T) {
	d := &snapshotDispatcher{
		enrich: func(snaps map[string]core.UsageSnapshot) {
			for id := range snaps {
				snap := snaps[id]
				snap.Message = "live"
				snaps[id] = snap
			}
		},
	}

	snaps := map[string]core.UsageSnapshot{
		"cursor-main": {ProviderID: "cursor", AccountID: "cursor-main", Message: "old"},
		"opencode":    {ProviderID: "opencode", AccountID: "opencode", Message: "old"},
	}
	d.applyEnrich(snaps, "opencode")

	if snaps["opencode"].Message != "live" {
		t.Fatalf("opencode message = %q, want live", snaps["opencode"].Message)
	}
	if snaps["cursor-main"].Message != "old" {
		t.Fatalf("cursor-main message = %q, want unchanged old", snaps["cursor-main"].Message)
	}
}
