package daemon

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func TestBuildCollectors_ScopesConfiguredAccount(t *testing.T) {
	collectors, warnings := buildCollectors([]core.AccountConfig{
		{
			ID:       "codex-main",
			Provider: "codex",
			RuntimeHints: map[string]string{
				"sessions_dir": "/tmp/codex-main",
			},
		},
	})

	collector := findSourceCollector(t, collectors, "codex")
	if collector.AccountOverride != "codex-main" {
		t.Fatalf("account override = %q, want codex-main", collector.AccountOverride)
	}
	if got := collector.Options.Paths["account_id"]; got != "codex-main" {
		t.Fatalf("account_id option = %q, want codex-main", got)
	}
	if got := collector.Options.Paths["sessions_dir"]; got != "/tmp/codex-main" {
		t.Fatalf("sessions_dir = %q, want /tmp/codex-main", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestBuildCollectors_AmbiguousAccountsFallBackToSourceScope(t *testing.T) {
	collectors, warnings := buildCollectors([]core.AccountConfig{
		{ID: "codex-a", Provider: "codex"},
		{ID: "codex-b", Provider: "codex"},
	})

	collector := findSourceCollector(t, collectors, "codex")
	if collector.AccountOverride != "" {
		t.Fatalf("account override = %q, want empty", collector.AccountOverride)
	}
	if got := collector.Options.Paths["account_id"]; got != "" {
		t.Fatalf("account_id option = %q, want empty", got)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings len = %d, want 1", len(warnings))
	}
}

func TestResolveTelemetrySourceOptionsFromAccounts_UsesExplicitAccount(t *testing.T) {
	source, ok := providers.TelemetrySourceBySystem("codex")
	if !ok {
		t.Fatal("codex telemetry source not found")
	}

	options, accountID, warnings := resolveTelemetrySourceOptionsFromAccounts(source, []core.AccountConfig{
		{
			ID:       "codex-a",
			Provider: "codex",
			RuntimeHints: map[string]string{
				"sessions_dir": "/tmp/codex-a",
			},
		},
		{
			ID:       "codex-b",
			Provider: "codex",
			RuntimeHints: map[string]string{
				"sessions_dir": "/tmp/codex-b",
			},
		},
	}, "codex-b")

	if accountID != "codex-b" {
		t.Fatalf("account id = %q, want codex-b", accountID)
	}
	if got := options.Paths["sessions_dir"]; got != "/tmp/codex-b" {
		t.Fatalf("sessions_dir = %q, want /tmp/codex-b", got)
	}
	if got := options.Paths["account_id"]; got != "codex-b" {
		t.Fatalf("account_id option = %q, want codex-b", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func findSourceCollector(t *testing.T, collectors []telemetry.Collector, name string) *telemetry.SourceCollector {
	t.Helper()
	for _, collector := range collectors {
		if collector.Name() != name {
			continue
		}
		sourceCollector, ok := collector.(*telemetry.SourceCollector)
		if !ok {
			t.Fatalf("collector %q has type %T, want *telemetry.SourceCollector", name, collector)
		}
		return sourceCollector
	}
	t.Fatalf("collector %q not found", name)
	return nil
}
