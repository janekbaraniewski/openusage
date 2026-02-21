package main

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
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

func TestBuildDemoSnapshots_WidgetCoverage(t *testing.T) {
	snaps := buildDemoSnapshots()

	type expectation struct {
		hasModelBurnData   bool
		hasClientMixData   bool
		hasPricingShowcase bool
	}

	want := map[string]expectation{
		"claude_code": {hasModelBurnData: true, hasClientMixData: true},
		"codex":       {hasModelBurnData: true, hasClientMixData: true},
		"copilot":     {hasModelBurnData: true, hasClientMixData: true},
		"gemini_cli":  {hasModelBurnData: true, hasClientMixData: true},
		"openrouter":  {hasModelBurnData: true},
		"zen":         {hasModelBurnData: true, hasPricingShowcase: true},
	}

	for providerID, exp := range want {
		snap, ok := snapshotByProvider(snaps, providerID)
		if !ok {
			t.Fatalf("missing snapshot for provider %q", providerID)
		}
		if exp.hasModelBurnData && !hasModelBurnMetrics(snap) {
			t.Fatalf("provider %q missing model burn metrics", providerID)
		}
		if exp.hasClientMixData && !hasClientMixMetrics(snap) {
			t.Fatalf("provider %q missing client mix metrics", providerID)
		}
		if exp.hasPricingShowcase {
			if _, ok := snap.Metrics["pricing_input_min_paid_per_1m"]; !ok {
				t.Fatalf("provider %q missing pricing_input_min_paid_per_1m", providerID)
			}
		}
	}
}

func snapshotByProvider(snaps map[string]core.QuotaSnapshot, providerID string) (core.QuotaSnapshot, bool) {
	for _, snap := range snaps {
		if snap.ProviderID == providerID {
			return snap, true
		}
	}
	return core.QuotaSnapshot{}, false
}

func hasModelBurnMetrics(snap core.QuotaSnapshot) bool {
	for key, m := range snap.Metrics {
		if m.Used == nil {
			continue
		}
		if strings.HasPrefix(key, "model_") && (strings.HasSuffix(key, "_cost_usd") || strings.HasSuffix(key, "_cost")) {
			return true
		}
		if strings.HasPrefix(key, "model_") && (strings.HasSuffix(key, "_input_tokens") || strings.HasSuffix(key, "_output_tokens")) {
			return true
		}
	}
	return false
}

func hasClientMixMetrics(snap core.QuotaSnapshot) bool {
	for key, m := range snap.Metrics {
		if m.Used == nil {
			continue
		}
		if strings.HasPrefix(key, "client_") && strings.HasSuffix(key, "_total_tokens") {
			return true
		}
	}
	return false
}
