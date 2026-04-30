package browsercookies

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNormalizeDomain(t *testing.T) {
	cases := map[string]string{
		"example.com":   "example.com",
		".example.com":  "example.com",
		"  Example.Com": "example.com",
		"":              "",
	}
	for in, want := range cases {
		if got := normalizeDomain(in); got != want {
			t.Errorf("normalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatches(t *testing.T) {
	cases := []struct {
		cookieDomain string
		lookupDomain string
		want         bool
	}{
		// Bare domain, no leading dot — exact match only.
		{"opencode.ai", "opencode.ai", true},
		{"opencode.ai", "console.opencode.ai", false},
		{"opencode.ai", ".opencode.ai", true},

		// Leading-dot domain — covers the bare host and any subdomain.
		{".opencode.ai", "opencode.ai", true},
		{".opencode.ai", "console.opencode.ai", true},
		{".opencode.ai", "deep.console.opencode.ai", true},
		{".opencode.ai", "evil-opencode.ai", false},

		// Non-matching.
		{"opencode.ai", "perplexity.ai", false},

		// Case-insensitive.
		{".OpenCode.AI", "opencode.ai", true},

		// Empty inputs.
		{"", "opencode.ai", false},
		{".opencode.ai", "", false},
	}
	for _, tc := range cases {
		if got := matches(tc.cookieDomain, tc.lookupDomain); got != tc.want {
			t.Errorf("matches(%q, %q) = %v, want %v", tc.cookieDomain, tc.lookupDomain, got, tc.want)
		}
	}
}

func TestCanonicalBrowser(t *testing.T) {
	cases := map[string]string{
		"chromium":           "chrome",
		"google-chrome":      "chrome",
		"Chrome":             "chrome",
		"firefox":            "firefox",
		"Mozilla Firefox":    "firefox",
		"safari":             "safari",
		"Microsoft Edge":     "edge",
		"Brave Browser":      "brave",
		"vivaldi":            "vivaldi",
		"opera":              "opera",
		"":                   "",
		"unknown-browser-99": "unknown-browser-99",
	}
	for in, want := range cases {
		if got := canonicalBrowser(in); got != want {
			t.Errorf("canonicalBrowser(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCookie_IsExpired(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		c    Cookie
		want bool
	}{
		{"future", Cookie{Expires: now.Add(time.Hour)}, false},
		{"past", Cookie{Expires: now.Add(-time.Hour)}, true},
		{"zero is session-cookie, not expired", Cookie{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.IsExpired(); got != tc.want {
				t.Errorf("IsExpired = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFakeReader_FindsCookieByDomainAndName(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	f := &FakeReader{
		Cookies: []Cookie{
			{Name: "noise", Value: "x", Domain: ".opencode.ai", Source: "chrome"},
			{Name: "auth", Value: "wanted", Domain: ".opencode.ai", Expires: exp, Source: "chrome"},
			{Name: "auth", Value: "different-domain", Domain: ".perplexity.ai", Source: "firefox"},
		},
	}
	got, err := f.ReadCookie(context.Background(), "opencode.ai", "auth", "")
	if err != nil {
		t.Fatalf("ReadCookie error: %v", err)
	}
	if got.Value != "wanted" {
		t.Errorf("Value = %q, want 'wanted'", got.Value)
	}
	if got.Source != "chrome" {
		t.Errorf("Source = %q, want chrome", got.Source)
	}
	if got.Expires != exp {
		t.Errorf("Expires = %v, want %v", got.Expires, exp)
	}
	if f.Calls() != 1 {
		t.Errorf("Calls = %d, want 1", f.Calls())
	}
}

func TestFakeReader_NoCookieReturnsErrNoCookieFound(t *testing.T) {
	f := &FakeReader{Cookies: []Cookie{
		{Name: "auth", Value: "wrong-domain", Domain: ".other.com", Source: "chrome"},
	}}
	_, err := f.ReadCookie(context.Background(), "opencode.ai", "auth", "")
	if !errors.Is(err, ErrNoCookieFound) {
		t.Errorf("err = %v, want ErrNoCookieFound", err)
	}
}

func TestFakeReader_PropagatesError(t *testing.T) {
	want := errors.New("simulated keychain failure")
	f := &FakeReader{Err: want}
	_, err := f.ReadCookie(context.Background(), "opencode.ai", "auth", "")
	if err != want {
		t.Errorf("err = %v, want %v", err, want)
	}
	browsers, err := f.AvailableBrowsers(context.Background())
	if err != want {
		t.Errorf("err = %v, want %v", err, want)
	}
	if browsers != nil {
		t.Errorf("AvailableBrowsers = %v, want nil on error", browsers)
	}
}

func TestFakeReader_AvailableBrowsersDistinct(t *testing.T) {
	f := &FakeReader{Cookies: []Cookie{
		{Name: "a", Domain: "x.com", Source: "chrome"},
		{Name: "b", Domain: "x.com", Source: "chrome"},
		{Name: "c", Domain: "x.com", Source: "firefox"},
		{Name: "d", Domain: "x.com", Source: ""},
	}}
	browsers, err := f.AvailableBrowsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"chrome": true, "firefox": true}
	if len(browsers) != len(want) {
		t.Fatalf("got %d, want %d: %v", len(browsers), len(want), browsers)
	}
	for _, b := range browsers {
		if !want[b] {
			t.Errorf("unexpected browser %q in result", b)
		}
	}
}

// New() returns a non-nil reader (this is a smoke test — we don't want the
// real kooky scan to run during unit tests because it triggers keychain
// prompts on macOS, but we do verify the constructor doesn't panic).
func TestNew_ReturnsReader(t *testing.T) {
	r := New()
	if r == nil {
		t.Fatal("New returned nil")
	}
	r2 := NewWithTimeout(time.Second)
	if r2 == nil {
		t.Fatal("NewWithTimeout returned nil")
	}
}
