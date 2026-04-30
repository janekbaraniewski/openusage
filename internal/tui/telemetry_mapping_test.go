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

	body := m.renderSettingsTelemetryBody(120, 30)
	if !strings.Contains(body, "Detected additional telemetry providers") {
		t.Fatalf("telemetry settings body missing detection banner: %q", body)
	}
	if !strings.Contains(body, "anthropic") || !strings.Contains(body, "openai") {
		t.Fatalf("telemetry settings body missing unmapped providers: %q", body)
	}
	if !strings.Contains(body, "telemetry.provider_links") {
		t.Fatalf("telemetry settings body missing mapping guidance: %q", body)
	}
	// Sources without meta default to the unconfigured category and render the
	// "no account configured" badge.
	if !strings.Contains(body, "no account configured") {
		t.Fatalf("telemetry settings body missing unconfigured category badge: %q", body)
	}
}

func TestRenderSettingsTelemetryBody_RendersCategorizedRows(t *testing.T) {
	m := Model{
		snapshots: map[string]core.UsageSnapshot{
			"copilot": {
				ProviderID: "copilot",
				Diagnostics: map[string]string{
					"telemetry_unmapped_providers": "github-copilot,google,openai",
					"telemetry_unmapped_meta":      "github-copilot=unconfigured:copilot,google=mapped_target_missing:gemini_api,openai=unconfigured",
				},
			},
		},
		accountProviders: map[string]string{
			"copilot": "copilot",
		},
	}

	body := m.renderSettingsTelemetryBody(140, 40)
	if !strings.Contains(body, "github-copilot") || !strings.Contains(body, "suggested: copilot") {
		t.Errorf("body missing suggestion row: %q", body)
	}
	if !strings.Contains(body, "google") || !strings.Contains(body, "mapped → gemini_api, target not configured") {
		t.Errorf("body missing mapped-target-missing row: %q", body)
	}
	if !strings.Contains(body, "openai") || !strings.Contains(body, "no account configured") {
		t.Errorf("body missing unconfigured row without suggestion: %q", body)
	}
	if !strings.Contains(body, "m: map to account") {
		t.Errorf("body missing keybinding hint: %q", body)
	}
}

func TestRenderHeader_ShowsGlobalUnmappedWarning_Passive(t *testing.T) {
	m := Model{
		snapshots: map[string]core.UsageSnapshot{
			"openrouter": {
				Status: core.StatusOK,
				Diagnostics: map[string]string{
					"telemetry_unmapped_providers": "anthropic,openai",
					"telemetry_unmapped_meta":      "anthropic=unconfigured,openai=unconfigured",
				},
			},
		},
		sortedIDs: []string{"openrouter"},
	}

	header := m.renderHeader(160)
	if !strings.Contains(header, "telemetry sources without an account") {
		t.Fatalf("header missing passive phrasing: %q", header)
	}
	if !strings.Contains(header, "unmapped") {
		t.Fatalf("header missing unmapped status badge: %q", header)
	}
}

func TestRenderHeader_ShowsGlobalUnmappedWarning_Actionable(t *testing.T) {
	m := Model{
		snapshots: map[string]core.UsageSnapshot{
			"copilot": {
				Status: core.StatusOK,
				Diagnostics: map[string]string{
					"telemetry_unmapped_providers": "github-copilot,openai",
					"telemetry_unmapped_meta":      "github-copilot=unconfigured:copilot,openai=unconfigured",
				},
			},
		},
		sortedIDs: []string{"copilot"},
	}

	header := m.renderHeader(160)
	if !strings.Contains(header, "telemetry sources need mapping") {
		t.Fatalf("header missing actionable phrasing: %q", header)
	}
}
