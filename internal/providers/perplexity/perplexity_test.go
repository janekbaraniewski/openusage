package perplexity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func configSaveSession(accountID, value string) error {
	return config.SaveSession(accountID, config.BrowserSession{
		Domain:        ".perplexity.ai",
		CookieName:    "__Secure-next-auth.session-token",
		Value:         value,
		SourceBrowser: "chrome",
	})
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// startFakeConsole serves the captured Perplexity console fixtures from
// testdata/. Each request maps to one of the four endpoints we hit in
// production: groups list, group detail, usage analytics, invoices.
func startFakeConsole(t *testing.T, orgID string) *httptest.Server {
	t.Helper()
	groupsList := loadFixture(t, "rest_pplx-api_v2_groups.json")
	groupDetail := loadFixture(t, "rest_pplx-api_v2_groups_25fb0cf4-fb6f-41dc-964f-ec8a3857bdcd.json")
	analytics := loadFixture(t, "rest_pplx-api_v2_groups_25fb0cf4-fb6f-41dc-964f-ec8a3857bdcd_usage-analytics.json")

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("__Secure-next-auth.session-token"); err != nil || c.Value == "" {
			http.Error(w, "no cookie", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/pplx-api/v2/groups":
			_, _ = w.Write(groupsList)
		case r.URL.Path == "/rest/pplx-api/v2/groups/"+orgID:
			_, _ = w.Write(groupDetail)
		case strings.HasSuffix(r.URL.Path, "/usage-analytics"):
			_, _ = w.Write(analytics)
		default:
			http.NotFound(w, r)
		}
	}))
}

// E2E: cookie configured → Fetch returns OK with balance, tier, account
// metadata populated from the captured fixtures.
func TestFetch_CookieConfigured_PopulatesAllFields(t *testing.T) {
	const orgID = "25fb0cf4-fb6f-41dc-964f-ec8a3857bdcd"
	server := startFakeConsole(t, orgID)
	defer server.Close()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))

	// Persist a session for this account using the real config helpers,
	// then point the provider at our fake console via base URL override.
	pinSessionForTest(t, "perplexity", "test-cookie-value")

	p := New()
	// Override the console base URL via a back-door so we can hit the
	// fake httptest server. The constant is unexported; we use a small
	// test seam via os.Setenv-driven override.
	t.Setenv("OPENUSAGE_PERPLEXITY_CONSOLE_BASE_URL", server.URL)

	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "perplexity",
		Provider: "perplexity",
	})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status == core.StatusAuth {
		t.Fatalf("Status = AUTH unexpectedly (msg=%q)", snap.Message)
	}
	if got := snap.Attributes["org_id"]; got != orgID {
		t.Errorf("org_id = %q, want %s", got, orgID)
	}
	if got := snap.Attributes["org_display_name"]; got != "openusage" {
		t.Errorf("org_display_name = %q", got)
	}
	if got := snap.Attributes["usage_tier"]; got != "0" {
		t.Errorf("usage_tier = %q, want 0", got)
	}
	if got := snap.Attributes["account_email"]; got != "jan@baraniewski.com" {
		t.Errorf("account_email = %q", got)
	}

	if bal, ok := snap.Metrics["available_balance"]; !ok || bal.Remaining == nil {
		t.Errorf("missing available_balance metric")
	}
}

// No cookie → AUTH state with helpful message pointing at the connect flow.
func TestFetch_NoCookie_AuthMessage(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))

	snap, err := New().Fetch(context.Background(), core.AccountConfig{
		ID:       "perplexity",
		Provider: "perplexity",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %s, want AUTH", snap.Status)
	}
	if !strings.Contains(snap.Message, "Settings") {
		t.Errorf("Message should hint at the connect flow: %q", snap.Message)
	}
}

// Auth-rejected (server returns 401) → AUTH state with re-login hint.
func TestFetch_CookieRejected_SurfacesAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"session expired"}`))
	}))
	defer server.Close()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	pinSessionForTest(t, "perplexity", "expired-cookie")
	t.Setenv("OPENUSAGE_PERPLEXITY_CONSOLE_BASE_URL", server.URL)

	snap, err := New().Fetch(context.Background(), core.AccountConfig{
		ID:       "perplexity",
		Provider: "perplexity",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %s, want AUTH on 401", snap.Status)
	}
}

// pinSessionForTest writes a session entry into the test config dir using
// the real config helpers — same persistence layer the connect flow uses.
// The test caller must have already done t.Setenv("HOME", tmp) so HOME-
// based credential-path resolution lands in the temp dir.
func pinSessionForTest(t *testing.T, accountID, value string) {
	t.Helper()
	if err := configSaveSession(accountID, value); err != nil {
		t.Fatalf("save session: %v", err)
	}
}
