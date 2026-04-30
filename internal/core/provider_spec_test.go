package core

import (
	"encoding/json"
	"testing"
)

func TestProviderAuthSpec_SupportsAuth(t *testing.T) {
	cases := []struct {
		name string
		spec ProviderAuthSpec
		t    ProviderAuthType
		want bool
	}{
		{
			name: "primary type matches",
			spec: ProviderAuthSpec{Type: ProviderAuthTypeAPIKey},
			t:    ProviderAuthTypeAPIKey,
			want: true,
		},
		{
			name: "supplemental browser_session matches",
			spec: ProviderAuthSpec{
				Type:              ProviderAuthTypeAPIKey,
				SupplementalTypes: []ProviderAuthType{ProviderAuthTypeBrowserSession},
			},
			t:    ProviderAuthTypeBrowserSession,
			want: true,
		},
		{
			name: "non-supported type",
			spec: ProviderAuthSpec{Type: ProviderAuthTypeAPIKey},
			t:    ProviderAuthTypeBrowserSession,
			want: false,
		},
		{
			name: "primary browser_session standalone",
			spec: ProviderAuthSpec{Type: ProviderAuthTypeBrowserSession},
			t:    ProviderAuthTypeBrowserSession,
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.spec.SupportsAuth(tc.t); got != tc.want {
				t.Errorf("SupportsAuth(%q) = %v, want %v", tc.t, got, tc.want)
			}
		})
	}
}

func TestBrowserCookieRef_JSONRoundtrip(t *testing.T) {
	in := BrowserCookieRef{
		Domain:        ".opencode.ai",
		CookieName:    "auth",
		SourceBrowser: "chrome",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var out BrowserCookieRef
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if out != in {
		t.Errorf("round-trip = %+v, want %+v", out, in)
	}
}

func TestBrowserCookieRef_OmitsEmpty(t *testing.T) {
	in := BrowserCookieRef{Domain: ".opencode.ai", CookieName: "auth"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != `{"domain":".opencode.ai","cookie_name":"auth"}` {
		t.Errorf("marshal = %s, want compact form without source_browser", got)
	}
}

func TestAccountConfig_BrowserCookieJSONRoundtrip(t *testing.T) {
	in := AccountConfig{
		ID:       "opencode-console",
		Provider: "opencode",
		Auth:     "browser_session",
		BrowserCookie: &BrowserCookieRef{
			Domain:        ".opencode.ai",
			CookieName:    "auth",
			SourceBrowser: "chrome",
		},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out AccountConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.BrowserCookie == nil {
		t.Fatal("BrowserCookie not preserved through round-trip")
	}
	if *out.BrowserCookie != *in.BrowserCookie {
		t.Errorf("BrowserCookie = %+v, want %+v", *out.BrowserCookie, *in.BrowserCookie)
	}
}

func TestAccountConfig_BrowserCookieOmittedWhenNil(t *testing.T) {
	in := AccountConfig{ID: "openai", Provider: "openai", Auth: "api_key"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	// Don't gate on the exact serialization; just ensure browser_cookie isn't there.
	for _, key := range []string{`"browser_cookie":null`, `"browser_cookie":{}`} {
		if contains(string(data), key) {
			t.Errorf("marshal unexpectedly contained %s: %s", key, data)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
