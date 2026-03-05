package providers

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

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

func TestAllProviders_HaveUniqueAndConsistentIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, p := range AllProviders() {
		id := p.ID()
		if id == "" {
			t.Fatalf("provider %T has empty ID", p)
		}
		if seen[id] {
			t.Fatalf("duplicate provider ID %q", id)
		}
		seen[id] = true

		spec := p.Spec()
		if spec.ID != "" && spec.ID != id {
			t.Fatalf("provider %q spec.ID = %q, want %q", id, spec.ID, id)
		}
	}
}

func TestAllProviders_DashboardSectionsAreKnownAndUnique(t *testing.T) {
	for _, p := range AllProviders() {
		id := p.ID()
		sections := p.DashboardWidget().EffectiveStandardSectionOrder()
		seen := make(map[core.DashboardStandardSection]bool, len(sections))
		for _, section := range sections {
			if !core.IsKnownDashboardStandardSection(section) {
				t.Fatalf("provider %q has unknown dashboard section %q", id, section)
			}
			if seen[section] {
				t.Fatalf("provider %q has duplicate dashboard section %q", id, section)
			}
			seen[section] = true
		}
	}
}
