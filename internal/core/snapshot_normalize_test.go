package core

import "testing"

func TestNormalizeUsageSnapshot_SplitsAttributesAndDiagnostics(t *testing.T) {
	snap := UsageSnapshot{
		Raw: map[string]string{
			"account_email": "dev@example.com",
			"quota_api":     "live",
			"api_error":     "HTTP 500",
		},
	}

	got := NormalizeUsageSnapshotWithConfig(snap, DefaultModelNormalizationConfig())

	if got.Attributes["account_email"] != "dev@example.com" {
		t.Fatalf("attribute account_email = %q", got.Attributes["account_email"])
	}
	if got.Attributes["quota_api"] != "live" {
		t.Fatalf("attribute quota_api = %q", got.Attributes["quota_api"])
	}
	if got.Diagnostics["api_error"] != "HTTP 500" {
		t.Fatalf("diagnostic api_error = %q", got.Diagnostics["api_error"])
	}
}

func TestUsageSnapshotMetaValue_PrefersAttributes(t *testing.T) {
	snap := UsageSnapshot{
		Attributes:  map[string]string{"status": "from-attributes"},
		Diagnostics: map[string]string{"status": "from-diagnostics"},
		Raw:         map[string]string{"status": "from-raw"},
	}

	got, ok := snap.MetaValue("status")
	if !ok {
		t.Fatal("expected key status")
	}
	if got != "from-attributes" {
		t.Fatalf("MetaValue(status) = %q, want from-attributes", got)
	}
}
