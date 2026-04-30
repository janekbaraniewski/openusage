package tui

import "testing"

// Regression: when validating an API key for a provider that has NOT been
// auto-detected (no env var set, no account in config), the API Keys tab
// must still resolve providerID for the row via the static provider-spec
// list. Previously the validate path read providerID directly from
// m.accountProviders, which is empty for such rows, and the resulting
// empty providerID caused the daemon's ValidateAPIKey to return
// "unknown provider".
func TestProviderForAccountID_FallsBackToSpecsForUnconfiguredAccount(t *testing.T) {
	// Empty map mirrors the runtime case for a provider whose env var isn't
	// set. The function under test must still resolve the provider id.
	empty := map[string]string{}

	cases := map[string]string{
		"moonshot-ai": "moonshot",
		"openai":      "openai",
		"deepseek":    "deepseek",
	}
	for accountID, wantProviderID := range cases {
		got := providerForAccountID(accountID, empty)
		if got != wantProviderID {
			t.Errorf("providerForAccountID(%q, empty) = %q, want %q", accountID, got, wantProviderID)
		}
	}
}

// Sanity: when a provider HAS been auto-detected, the configured map's
// entry should win over the static spec lookup.
func TestProviderForAccountID_PrefersAccountProvidersMap(t *testing.T) {
	configured := map[string]string{
		"moonshot-ai": "moonshot-custom-override",
	}
	if got := providerForAccountID("moonshot-ai", configured); got != "moonshot-custom-override" {
		t.Errorf("providerForAccountID with override = %q, want moonshot-custom-override", got)
	}
}
