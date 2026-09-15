package tui

import (
	"strings"
	"testing"
)

func TestLocalAuthEnvHintForProvider_MuseCode(t *testing.T) {
	if got := localAuthEnvHintForProvider("muse_code"); got != "META_API_KEY" {
		t.Fatalf("hint for muse_code = %q, want META_API_KEY", got)
	}
}

func TestLocalAuthEnvHintForProvider_UnknownIsEmpty(t *testing.T) {
	if got := localAuthEnvHintForProvider("openai"); got != "" {
		t.Fatalf("hint for openai = %q, want empty (API-key providers use the spec env)", got)
	}
}

// The Credential Management tab should show Muse Code's optional env
// credential instead of a bare "-" like every other row shows its var.
func TestRenderSettingsAPIKeysBody_MuseCodeShowsMetaAPIKey(t *testing.T) {
	m := Model{}
	m.providerOrder = []string{"muse-code"}
	m.accountProviders = map[string]string{"muse-code": "muse_code"}

	body := m.renderSettingsAPIKeysBody(100, 30)
	if !strings.Contains(body, "muse-code") {
		t.Fatalf("expected a muse-code row in:\n%s", body)
	}
	if !strings.Contains(body, "META_API_KEY") {
		t.Fatalf("expected META_API_KEY auth source in:\n%s", body)
	}
}
