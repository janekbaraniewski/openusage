package providers

import (
	"sort"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// TestItemizedCoverage documents and locks in which providers can feed the
// headless session/blocks reports — either through the telemetry source
// interface (per-turn Collect) or the itemized usage interface (file/DB
// parsing). It fails if a provider silently loses that capability.
func TestItemizedCoverage(t *testing.T) {
	wantTelemetry := map[string]bool{
		"claude_code": true, "codex": true, "gemini_cli": true, "antigravity": true,
		"copilot": true, "cursor": true, "ollama": true, "opencode": true,
	}
	wantItemized := map[string]bool{
		"amp": true, "codebuff": true, "openclaw": true, "roocode": true,
		"kilo_code": true, "crush": true, "goose": true, "hermes": true,
		"zed": true, "droid": true, "kiro_cli": true,
	}

	gotTelemetry := map[string]bool{}
	gotItemized := map[string]bool{}
	for _, p := range AllProviders() {
		if _, ok := p.(shared.TelemetrySource); ok {
			gotTelemetry[p.ID()] = true
		}
		if _, ok := p.(core.ItemizedUsageProvider); ok {
			gotItemized[p.ID()] = true
		}
	}

	check := func(name string, want, got map[string]bool) {
		for id := range want {
			if !got[id] {
				t.Errorf("%s: provider %q no longer exposes %s usage", name, id, name)
			}
		}
		// surface any newly-added capability so the matrix stays honest
		var extra []string
		for id := range got {
			if !want[id] {
				extra = append(extra, id)
			}
		}
		sort.Strings(extra)
		for _, id := range extra {
			t.Logf("%s: provider %q now also exposes %s usage (update the matrix/docs)", name, id, name)
		}
	}
	check("telemetry", wantTelemetry, gotTelemetry)
	check("itemized", wantItemized, gotItemized)
}
