package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestTelemetryUnmappedProviders_DeduplicatesAndSorts(t *testing.T) {
	m := Model{
		snapshots: map[string]core.UsageSnapshot{
			"a": {
				Diagnostics: map[string]string{
					"telemetry_unmapped_providers": "anthropic, openai, zen",
				},
			},
			"b": {
				Diagnostics: map[string]string{
					"telemetry_unmapped_providers": "openai, google",
				},
			},
		},
	}

	got := m.telemetryUnmappedProviders()
	want := []string{"anthropic", "google", "openai", "zen"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("telemetryUnmappedProviders() = %#v, want %#v", got, want)
	}
}

func TestBuildTileMetaLines_OmitsTelemetryMappingDiagnostics(t *testing.T) {
	snap := core.UsageSnapshot{
		Attributes: map[string]string{
			"account_email": "jan@example.com",
		},
		Diagnostics: map[string]string{
			"telemetry_unmapped_providers": "anthropic,openai",
			"telemetry_provider_link_hint": "Configure telemetry.provider_links.<source_provider>=<configured_provider_id>",
		},
	}

	lines := buildTileMetaLines(snap, 120)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "Unmapped") {
		t.Fatalf("tile metadata unexpectedly contains unmapped diagnostics: %q", joined)
	}
	if strings.Contains(joined, "Link") {
		t.Fatalf("tile metadata unexpectedly contains link hint diagnostics: %q", joined)
	}
	if !strings.Contains(joined, "Account") {
		t.Fatalf("tile metadata missing expected account metadata: %q", joined)
	}
}

func TestRenderSettingsTelemetryBody_ShowsUnmappedProviders(t *testing.T) {
	m := Model{
		snapshots: map[string]core.UsageSnapshot{
			"openrouter": {
				ProviderID: "openrouter",
				Diagnostics: map[string]string{
					"telemetry_unmapped_providers": "anthropic,openai",
					"telemetry_provider_link_hint": "Configure telemetry.provider_links.<source_provider>=<configured_provider_id>",
				},
			},
		},
		accountProviders: map[string]string{
			"openrouter": "openrouter",
			"cursor":     "cursor",
		},
	}

	body := m.renderSettingsTelemetryBody(120, 12)
	if !strings.Contains(body, "Detected additional telemetry providers") {
		t.Fatalf("telemetry settings body missing detection banner: %q", body)
	}
	if !strings.Contains(body, "anthropic") || !strings.Contains(body, "openai") {
		t.Fatalf("telemetry settings body missing unmapped providers: %q", body)
	}
	if !strings.Contains(body, "telemetry.provider_links") {
		t.Fatalf("telemetry settings body missing mapping guidance: %q", body)
	}
}

func TestRenderHeader_ShowsGlobalUnmappedWarning(t *testing.T) {
	m := Model{
		snapshots: map[string]core.UsageSnapshot{
			"openrouter": {
				Status: core.StatusOK,
				Diagnostics: map[string]string{
					"telemetry_unmapped_providers": "anthropic,openai",
				},
			},
		},
		sortedIDs: []string{"openrouter"},
	}

	header := m.renderHeader(160)
	if !strings.Contains(header, "detected additional providers, check settings") {
		t.Fatalf("header missing unmapped provider guidance: %q", header)
	}
	if !strings.Contains(header, "unmapped") {
		t.Fatalf("header missing unmapped status badge: %q", header)
	}
}
