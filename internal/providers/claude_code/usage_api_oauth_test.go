package claude_code

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// setTempHome points os.UserHomeDir at a fresh temp dir. HOME covers
// Unix-likes; USERPROFILE is what os.UserHomeDir reads on Windows.
func setTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// writeCredentials drops a fake ~/.claude/.credentials.json under a temp home
// directory.
func writeCredentials(t *testing.T, body string) {
	t.Helper()
	home := setTempHome(t)
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
}

func TestReadClaudeCodeOAuthToken(t *testing.T) {
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	past := strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10)

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "valid unexpired token",
			body: `{"claudeAiOauth":{"accessToken":"tok-123","expiresAt":` + future + `}}`,
			want: "tok-123",
		},
		{
			name: "no expiry field is accepted",
			body: `{"claudeAiOauth":{"accessToken":"tok-abc"}}`,
			want: "tok-abc",
		},
		{
			name:    "expired token rejected",
			body:    `{"claudeAiOauth":{"accessToken":"tok-123","expiresAt":` + past + `}}`,
			wantErr: true,
		},
		{
			name:    "empty token rejected",
			body:    `{"claudeAiOauth":{"accessToken":""}}`,
			wantErr: true,
		},
		{
			name:    "malformed json rejected",
			body:    `{not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeCredentials(t, tt.body)
			got, err := readClaudeCodeOAuthToken()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got token %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("token = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadClaudeCodeOAuthToken_MissingFile(t *testing.T) {
	setTempHome(t) // no .claude/.credentials.json
	if _, err := readClaudeCodeOAuthToken(); err == nil {
		t.Fatal("expected error for missing credentials file")
	}
}

func TestFetchUsageAPIWithAuth_OAuth(t *testing.T) {
	var gotAuth, gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":42,"resets_at":"2999-01-01T00:00:00Z"},` +
			`"seven_day":{"utilization":13,"resets_at":"2999-01-01T00:00:00Z"}}`))
	}))
	defer srv.Close()

	usage, err := fetchUsageAPIWithAuth(context.Background(), srv.URL, oauthAuthHeaders("tok-xyz"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.FiveHour == nil || usage.FiveHour.Utilization != 42 {
		t.Fatalf("five_hour utilization = %+v, want 42", usage.FiveHour)
	}
	if usage.SevenDay == nil || usage.SevenDay.Utilization != 13 {
		t.Fatalf("seven_day utilization = %+v, want 13", usage.SevenDay)
	}
	if gotAuth != "Bearer tok-xyz" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer tok-xyz")
	}
	if gotBeta != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta header = %q, want %q", gotBeta, "oauth-2025-04-20")
	}
}

func TestFetchUsageAPIWithAuth_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	defer srv.Close()

	if _, err := fetchUsageAPIWithAuth(context.Background(), srv.URL, oauthAuthHeaders("tok")); err == nil {
		t.Fatal("expected error on 401 response")
	}
}

// TestCookieAuthHeaders verifies the cookie/org source's scheme: session
// cookies plus browser-shaped headers, and specifically that it never sets
// the OAuth scheme's Authorization header — the two auth schemes must stay
// distinct since fetchUsageAPIWithAuth applies exactly one of them per call.
func TestCookieAuthHeaders(t *testing.T) {
	cookies := map[string]string{
		"sessionKey":    "sess-abc",
		"lastActiveOrg": "org-123",
	}
	req := httptest.NewRequest(http.MethodGet, "https://claude.ai/api/organizations/org-123/usage", nil)
	cookieAuthHeaders(cookies)(req)

	cookieHeader := req.Header.Get("Cookie")
	for name, value := range cookies {
		want := name + "=" + value
		if !strings.Contains(cookieHeader, want) {
			t.Errorf("Cookie header %q missing %q", cookieHeader, want)
		}
	}
	if got := req.Header.Get("Referer"); got != "https://claude.ai/settings/usage" {
		t.Errorf("Referer = %q, want %q", got, "https://claude.ai/settings/usage")
	}
	if got := req.Header.Get("anthropic-client-platform"); got != "web_claude_ai" {
		t.Errorf("anthropic-client-platform = %q, want %q", got, "web_claude_ai")
	}
	if req.Header.Get("User-Agent") == "" {
		t.Error("User-Agent not set")
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization header should not be set on the cookie scheme, got %q", got)
	}
	if got := req.Header.Get("anthropic-beta"); got != "" {
		t.Errorf("anthropic-beta header should not be set on the cookie scheme, got %q", got)
	}
}

// TestOAuthAuthHeaders verifies the OAuth source's scheme: a bearer token
// plus the oauth-beta header, and that it never sets the cookie scheme's
// Cookie/Referer/platform headers.
func TestOAuthAuthHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	oauthAuthHeaders("tok-xyz")(req)

	if got := req.Header.Get("Authorization"); got != "Bearer tok-xyz" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok-xyz")
	}
	if got := req.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta = %q, want %q", got, "oauth-2025-04-20")
	}
	if got := req.Header.Get("Cookie"); got != "" {
		t.Errorf("Cookie header should not be set on the oauth scheme, got %q", got)
	}
	if got := req.Header.Get("Referer"); got != "" {
		t.Errorf("Referer header should not be set on the oauth scheme, got %q", got)
	}
	if got := req.Header.Get("anthropic-client-platform"); got != "" {
		t.Errorf("anthropic-client-platform header should not be set on the oauth scheme, got %q", got)
	}
}

// TestUsageAuthSources_NamesAndOrder pins down the fixture list itself:
// cookie/org is tried before oauth, matching the macOS-first, CLI-fallback
// priority documented on usageAuthSources.
func TestUsageAuthSources_NamesAndOrder(t *testing.T) {
	p := &Provider{}
	sources := p.usageAuthSources("org-uuid")
	if len(sources) != 2 {
		t.Fatalf("len(sources) = %d, want 2", len(sources))
	}
	if sources[0].name != "cookie" {
		t.Errorf("sources[0].name = %q, want %q", sources[0].name, "cookie")
	}
	if sources[1].name != "oauth" {
		t.Errorf("sources[1].name = %q, want %q", sources[1].name, "oauth")
	}
}

// TestUsageAuthSources_OAuthPrepare_UsesOAuthSchemeAndURL exercises the real
// "oauth" fixture's prepare() end to end: it should resolve to oauthUsageURL
// and a setAuth closure using the bearer scheme, never the cookie scheme.
func TestUsageAuthSources_OAuthPrepare_UsesOAuthSchemeAndURL(t *testing.T) {
	futureMs := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"tok-fixture","expiresAt":`+futureMs+`}}`)

	p := &Provider{}
	sources := p.usageAuthSources("org-uuid")
	src, ok := findUsageAuthSource(sources, "oauth")
	if !ok {
		t.Fatal(`findUsageAuthSource(sources, "oauth") = false`)
	}

	url, setAuth, err := src.prepare()
	if err != nil {
		t.Fatalf("prepare: unexpected error: %v", err)
	}
	if url != oauthUsageURL {
		t.Errorf("url = %q, want oauthUsageURL %q", url, oauthUsageURL)
	}

	req := httptest.NewRequest(http.MethodGet, url, nil)
	setAuth(req)
	if got := req.Header.Get("Authorization"); got != "Bearer tok-fixture" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok-fixture")
	}
	if got := req.Header.Get("Cookie"); got != "" {
		t.Errorf("Cookie header should not be set by the oauth fixture, got %q", got)
	}
}

// TestFindUsageAuthSource covers the pin-lookup helper readUsageAPI relies on
// to resume the previously successful source.
func TestFindUsageAuthSource(t *testing.T) {
	p := &Provider{}
	sources := p.usageAuthSources("org-uuid")

	if _, ok := findUsageAuthSource(sources, "oauth"); !ok {
		t.Error(`findUsageAuthSource(sources, "oauth") = false, want true`)
	}
	if _, ok := findUsageAuthSource(sources, "cookie"); !ok {
		t.Error(`findUsageAuthSource(sources, "cookie") = false, want true`)
	}
	if _, ok := findUsageAuthSource(sources, "does-not-exist"); ok {
		t.Error(`findUsageAuthSource(sources, "does-not-exist") = true, want false`)
	}
}

// TestReadUsageAPI_PinsSuccessfulAuthSource covers the second-call fast path:
// once an auth source has succeeded it's pinned, so later calls go straight
// to it; when the pinned source stops working, the pin clears and the call
// falls back to the last cached usage rather than erroring.
//
// Cookie extraction always fails on this (non-macOS) test runner, so oauth
// is the only source that can succeed here; that's enough to exercise pin
// set/reuse/clear without needing to fake a working cookie source too.
func TestReadUsageAPI_PinsSuccessfulAuthSource(t *testing.T) {
	var oauthCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		oauthCalls++
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2999-01-01T00:00:00Z"}}`))
	}))
	defer srv.Close()

	old := oauthUsageURL
	oauthUsageURL = srv.URL
	defer func() { oauthUsageURL = old }()

	futureMs := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"tok","expiresAt":`+futureMs+`}}`)

	p := &Provider{}

	snap := core.NewUsageSnapshot("claude_code", "acct")
	if err := p.readUsageAPI(context.Background(), "org-uuid", &snap); err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if got := p.getLastUsageAuthSource(); got != "oauth" {
		t.Fatalf("pinned source after first success = %q, want %q", got, "oauth")
	}
	if oauthCalls != 1 {
		t.Fatalf("oauth server calls after first call = %d, want 1", oauthCalls)
	}

	snap2 := core.NewUsageSnapshot("claude_code", "acct")
	if err := p.readUsageAPI(context.Background(), "org-uuid", &snap2); err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if got := p.getLastUsageAuthSource(); got != "oauth" {
		t.Fatalf("pinned source after second success = %q, want %q", got, "oauth")
	}
	if oauthCalls != 2 {
		t.Fatalf("oauth server calls after second call = %d, want 2", oauthCalls)
	}

	// Break the pinned source: point the home dir somewhere with no
	// credentials file, so readClaudeCodeOAuthToken fails before any HTTP call.
	setTempHome(t)

	snap3 := core.NewUsageSnapshot("claude_code", "acct")
	if err := p.readUsageAPI(context.Background(), "org-uuid", &snap3); err != nil {
		t.Fatalf("third call: expected cache fallback, got error: %v", err)
	}
	if snap3.Raw["usage_api_cached"] != "true" {
		t.Fatalf("third call: expected cached fallback, got Raw=%v", snap3.Raw)
	}
	if got := p.getLastUsageAuthSource(); got != "" {
		t.Fatalf("pin should clear once the pinned source fails, got %q", got)
	}
	if oauthCalls != 2 {
		t.Fatalf("oauth server calls after credential removal = %d, want still 2 (prepare should fail before any request)", oauthCalls)
	}
}
