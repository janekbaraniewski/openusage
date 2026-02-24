package providers

import "testing"

func TestAllProviders_ContainsOpenCode(t *testing.T) {
	all := AllProviders()
	found := false
	for _, p := range all {
		if p.ID() == "opencode" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected opencode provider in registry")
	}
}

func TestAllTelemetrySources_DerivedFromProviderRegistry(t *testing.T) {
	found := map[string]bool{}
	for _, provider := range AllProviders() {
		source, ok := provider.(interface{ System() string })
		if !ok {
			continue
		}
		found[source.System()] = true
	}

	for _, want := range []string{"codex", "claude_code", "opencode"} {
		if !found[want] {
			t.Fatalf("missing telemetry source %q", want)
		}
	}
}

func TestTelemetrySourceBySystem_CaseInsensitive(t *testing.T) {
	source, ok := TelemetrySourceBySystem("CoDeX")
	if !ok {
		t.Fatalf("expected codex source")
	}
	if source.System() != "codex" {
		t.Fatalf("source.system = %q, want codex", source.System())
	}
}
