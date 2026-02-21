package main

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/providers"
)

func TestBuildDemoSnapshots_IncludesAllProviders(t *testing.T) {
	snaps := buildDemoSnapshots()
	if len(snaps) == 0 {
		t.Fatal("buildDemoSnapshots returned no snapshots")
	}

	byProvider := make(map[string]string)
	for accountID, snap := range snaps {
		if snap.AccountID == "" {
			t.Fatalf("snapshot for key %q has empty account id", accountID)
		}
		if accountID != snap.AccountID {
			t.Fatalf("snapshot key/account mismatch: key=%q account=%q", accountID, snap.AccountID)
		}
		if snap.ProviderID == "" {
			t.Fatalf("snapshot %q has empty provider id", accountID)
		}
		if snap.Status == "" {
			t.Fatalf("snapshot %q has empty status", accountID)
		}
		if snap.Metrics == nil {
			t.Fatalf("snapshot %q has nil metrics map", accountID)
		}
		if existing, ok := byProvider[snap.ProviderID]; ok {
			t.Fatalf("provider %q appears multiple times (%q, %q)", snap.ProviderID, existing, accountID)
		}
		byProvider[snap.ProviderID] = accountID
	}

	for _, provider := range providers.AllProviders() {
		if _, ok := byProvider[provider.ID()]; !ok {
			t.Fatalf("missing demo snapshot for provider %q", provider.ID())
		}
	}
}
