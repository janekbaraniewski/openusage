package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func zenModelsBody() string {
	return `{
		"object": "list",
		"data": [
			{"id": "minimax-m2.7", "object": "model", "created": 1, "owned_by": "opencode"},
			{"id": "kimi-k2.6",   "object": "model", "created": 1, "owned_by": "opencode"},
			{"id": "glm-5",       "object": "model", "created": 1, "owned_by": "opencode"}
		]
	}`
}

func startFakeZen(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	if status == 0 {
		status = http.StatusOK
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modelsPath {
			http.NotFound(w, r)
			return
		}
		// Verify the request carries Bearer auth — the provider would lose its
		// reason for existing if it forgot to attach it.
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "missing bearer", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func newAcct(t *testing.T, baseURL string) core.AccountConfig {
	t.Helper()
	t.Setenv("TEST_OPENCODE_KEY", "sk-zen-test-1234567890")
	return core.AccountConfig{
		ID:        "opencode",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_KEY",
		BaseURL:   baseURL,
	}
}

func TestFetch_Success_AuthOKExposesModels(t *testing.T) {
	server := startFakeZen(t, http.StatusOK, zenModelsBody())
	defer server.Close()

	snap, err := New().Fetch(context.Background(), newAcct(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %s (msg=%q), want OK", snap.Status, snap.Message)
	}
	if got := snap.Attributes["available_models_count"]; got != "3" {
		t.Errorf("available_models_count = %q, want 3", got)
	}
	if got := snap.Attributes["available_models"]; !strings.Contains(got, "minimax-m2.7") || !strings.Contains(got, "glm-5") {
		t.Errorf("available_models missing expected ids: %q", got)
	}
	if got := snap.Attributes["auth_scope"]; got != "zen" {
		t.Errorf("auth_scope = %q, want zen", got)
	}
	if !strings.Contains(snap.Message, "3") {
		t.Errorf("message should reference the model count: %q", snap.Message)
	}
}

func TestFetch_AuthRequired_NoKey(t *testing.T) {
	acct := core.AccountConfig{
		ID:        "opencode",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_MISSING",
	}
	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %s, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_AuthFailed_401(t *testing.T) {
	server := startFakeZen(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)
	defer server.Close()

	snap, err := New().Fetch(context.Background(), newAcct(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %s, want AUTH on 401", snap.Status)
	}
	if !strings.Contains(snap.Message, "401") {
		t.Errorf("message = %q, expected to mention 401", snap.Message)
	}
}

func TestFetch_RateLimited_429(t *testing.T) {
	server := startFakeZen(t, http.StatusTooManyRequests, `{}`)
	defer server.Close()

	snap, err := New().Fetch(context.Background(), newAcct(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Errorf("status = %s, want LIMITED on 429", snap.Status)
	}
}

func TestFetch_ConsoleEnrichmentAutoDiscoversWorkspaceID(t *testing.T) {
	origLoadStoredSession := loadStoredSession
	origNewConsoleClient := newConsoleClient
	t.Cleanup(func() {
		loadStoredSession = origLoadStoredSession
		newConsoleClient = origNewConsoleClient
	})

	// enrichFromConsole calls loadStoredSession (a pure credentials-file
	// read), not the now-unused loadBrowserSession var — stub the seam
	// that's actually on the call path.
	loadStoredSession = func(accountID string) (config.BrowserSession, bool, error) {
		return config.BrowserSession{
			Value:         "test-cookie-value",
			CookieName:    "auth",
			SourceBrowser: "firefox",
		}, true, nil
	}

	billing := loadFixture(t, "seroval_c83b78a61468.txt")
	var discoveredWorkspaceID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == modelsPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(zenModelsBody()))
		case r.URL.Path == "/auth":
			http.Redirect(w, r, "/workspace/wrk_DISCOVERED", http.StatusFound)
		case r.URL.Path == "/_server":
			discoveredWorkspaceID = r.URL.Query().Get("args")
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write(billing)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	newConsoleClient = func(cookieValue, cookieName, workspaceID string) *ConsoleClient {
		client := NewConsoleClient(cookieValue, cookieName, workspaceID)
		client.baseURL = server.URL
		return client
	}

	acct := newAcct(t, server.URL)
	acct.BrowserCookie = &core.BrowserCookieRef{
		Domain:        ".opencode.ai",
		CookieName:    "auth",
		SourceBrowser: "firefox",
	}

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %s (msg=%q), want OK", snap.Status, snap.Message)
	}
	if got := snap.Attributes["auth_scope"]; got != "zen+console" {
		t.Fatalf("auth_scope = %q, want zen+console", got)
	}
	if _, ok := snap.Metrics["console_balance"]; !ok {
		t.Fatal("console_balance metric missing after workspace auto-discovery")
	}
	if strings.Contains(discoveredWorkspaceID, "wrk_DISCOVERED") == false {
		t.Fatalf("billing request args missing discovered workspace: %q", discoveredWorkspaceID)
	}
	if _, ok := snap.Diagnostics["opencode_console_workspace_error"]; ok {
		t.Fatalf("unexpected workspace discovery diagnostic: %+v", snap.Diagnostics)
	}
}

// TestFetch_BrowserSessionOnlyNoAPIKey_ConsoleFailureSurfacesAuthNotOK covers
// an account configured for browser-session auth only (no Zen API key) whose
// stored session is missing/expired: previously the Zen probe was skipped
// (no key to probe with) and enrichFromConsole's failure was silently
// swallowed, leaving snap.Status empty and shared.FinalizeStatus defaulting
// it to a false StatusOK. It must now surface the underlying auth-required
// status instead.
func TestFetch_BrowserSessionOnlyNoAPIKey_ConsoleFailureSurfacesAuthNotOK(t *testing.T) {
	origLoadStoredSession := loadStoredSession
	t.Cleanup(func() { loadStoredSession = origLoadStoredSession })

	loadStoredSession = func(accountID string) (config.BrowserSession, bool, error) {
		return config.BrowserSession{}, false, nil
	}

	acct := core.AccountConfig{
		ID:        "opencode-personal",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_MISSING_FOR_BROWSER_ONLY",
		BrowserCookie: &core.BrowserCookieRef{
			Domain:        ".opencode.ai",
			CookieName:    "auth",
			SourceBrowser: "safari",
		},
	}

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status == core.StatusOK {
		t.Fatalf("status = OK, want a non-OK status — account has neither a valid API key nor a working browser session (msg=%q)", snap.Message)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %s, want AUTH_REQUIRED", snap.Status)
	}
}

// TestFetch_ConsoleDoubleFailure_DoesNotFabricateZeroBalance covers the case
// where both the HTML usage-page scrape and the billing-RPC fallback fail
// (e.g. an expired session cookie). Previously both errors were discarded
// and a zero-valued billing struct was written into the snapshot as if it
// were real data ("$0.00 balance"). It must now surface the failure instead.
func TestFetch_ConsoleDoubleFailure_DoesNotFabricateZeroBalance(t *testing.T) {
	origLoadStoredSession := loadStoredSession
	origNewConsoleClient := newConsoleClient
	t.Cleanup(func() {
		loadStoredSession = origLoadStoredSession
		newConsoleClient = origNewConsoleClient
	})

	loadStoredSession = func(accountID string) (config.BrowserSession, bool, error) {
		return config.BrowserSession{
			Value:         "test-cookie-value",
			CookieName:    "auth",
			SourceBrowser: "firefox",
		}, true, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Both the go-usage-page fetch and the billing RPC fallback come
		// back unauthorized, simulating an expired session cookie.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	newConsoleClient = func(cookieValue, cookieName, workspaceID string) *ConsoleClient {
		client := NewConsoleClient(cookieValue, cookieName, workspaceID)
		client.baseURL = server.URL
		return client
	}

	acct := core.AccountConfig{
		ID:        "opencode-personal",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_MISSING_FOR_DOUBLE_FAILURE",
		BrowserCookie: &core.BrowserCookieRef{
			Domain:        ".opencode.ai",
			CookieName:    "auth",
			SourceBrowser: "firefox",
		},
	}
	acct.SetHint("opencode_workspace_id", "wrk_test")

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if _, ok := snap.Metrics["console_balance"]; ok {
		t.Fatalf("console_balance metric present despite both page scrape and billing fallback failing: %+v", snap.Metrics["console_balance"])
	}
	if snap.Status == core.StatusOK {
		t.Fatalf("status = OK, want a non-OK status when console enrichment fully failed (msg=%q)", snap.Message)
	}
	if _, ok := snap.Diagnostics["opencode_console_auth_error"]; !ok {
		t.Errorf("expected opencode_console_auth_error diagnostic, got diagnostics=%+v", snap.Diagnostics)
	}
}
