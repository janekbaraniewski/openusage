// Package browsercookies extracts session cookies from the user's installed
// browsers (Chrome, Firefox, Safari, Edge, Brave). It is the foundation for
// openusage's browser-session-auth path — the credential-acquisition mechanism
// for providers whose billing / usage / account data lives behind dashboard
// session cookies and isn't reachable via API key.
//
// See docs/BROWSER_SESSION_AUTH_DESIGN.md for the rationale and the
// per-platform extraction details.
package browsercookies

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/browserutils/kooky"

	// Side-effect imports register Chrome / Firefox / Safari / Edge / Brave
	// stores. kooky is a registry — without these blank imports
	// FindAllCookieStores would only see what the consumer happened to
	// import. Importing here from a single place keeps the cookie surface
	// consistent for the whole binary.
	_ "github.com/browserutils/kooky/browser/all"
)

// Cookie is the openusage-internal representation of a browser cookie. We
// don't expose kooky's full type because we never need 95% of it — name,
// value, domain, path, expiry are all that matter for HTTP replay.
type Cookie struct {
	Name      string
	Value     string
	Domain    string
	Path      string
	Expires   time.Time
	HTTPOnly  bool
	Secure    bool
	Source    string // "chrome", "firefox", "safari", "edge", "brave"
	StorePath string // absolute path of the cookie store file the value came from (debugging only)
}

// IsExpired reports whether the cookie's Expires has already passed. Session
// cookies (Expires zero) are treated as not-expired — they're tied to the
// browser session, not a date.
func (c Cookie) IsExpired() bool {
	if c.Expires.IsZero() {
		return false
	}
	return time.Now().After(c.Expires)
}

// ErrNoCookieFound is returned when no matching cookie was found in any of
// the supported browsers' cookie stores. Callers should treat this as
// "the user is not currently logged into the relevant site in any browser
// openusage can read" — distinct from transport errors, OS-level keychain
// failures, or kooky panicking.
var ErrNoCookieFound = errors.New("browsercookies: no matching cookie found in any supported browser")

// Reader is a small surface around kooky for openusage's needs. The interface
// exists so tests can swap in a fake without spinning up a real browser
// store on disk. The concrete implementation is &kookyReader{}.
type Reader interface {
	// ReadCookie returns the freshest cookie matching the given (domain,
	// name) pair across all supported browsers' cookie stores. If multiple
	// browsers have a matching cookie, the one with the latest Expires
	// wins. The Source field on the returned Cookie identifies which
	// browser the value came from.
	//
	// preferredBrowser, if non-empty, is consulted first; on hit it
	// short-circuits the multi-browser scan. This is how the runtime
	// "remembers" where the user usually logs in (persisted on the
	// account's BrowserCookieRef) so we don't trigger an OS keychain
	// prompt for every browser on every poll.
	ReadCookie(ctx context.Context, domain, name, preferredBrowser string) (Cookie, error)

	// AvailableBrowsers reports which supported browsers have at least one
	// readable cookie store on this machine. Used by the TUI's connect
	// flow to show "we'll look in: Chrome, Firefox" without committing to
	// reading anything yet.
	AvailableBrowsers(ctx context.Context) ([]string, error)
}

// New returns the default Reader implementation backed by kooky. Cookie reads
// are bounded by readTimeout — kooky calls into the OS keychain on first
// Chrome read, which can hang or wait for Touch ID; we never want a poll to
// stall on this. 10s is generous enough for a real prompt-and-approve flow
// and tight enough to fall through to "no cookie found" if something is
// genuinely broken.
func New() Reader {
	return &kookyReader{readTimeout: 10 * time.Second}
}

// NewWithTimeout returns a Reader with a custom timeout, primarily for tests.
func NewWithTimeout(timeout time.Duration) Reader {
	return &kookyReader{readTimeout: timeout}
}

type kookyReader struct {
	readTimeout time.Duration
}

// normalizeDomain strips a leading dot so callers can pass either ".example.com"
// or "example.com" and we produce comparable keys. We never re-add the dot
// when matching — the cookie's stored Domain (with or without dot) is matched
// loosely below in matches().
func normalizeDomain(d string) string {
	d = strings.TrimSpace(d)
	d = strings.TrimPrefix(d, ".")
	return strings.ToLower(d)
}

// matches reports whether a cookie's stored Domain field covers the lookup
// domain, with the same loose-suffix semantics browsers use. ".example.com"
// matches "example.com" and any subdomain; "example.com" (no leading dot)
// matches only "example.com" exactly.
func matches(cookieDomain, lookupDomain string) bool {
	cookieDomain = strings.ToLower(strings.TrimSpace(cookieDomain))
	lookupDomain = normalizeDomain(lookupDomain)
	if cookieDomain == "" || lookupDomain == "" {
		return false
	}
	if strings.HasPrefix(cookieDomain, ".") {
		bare := strings.TrimPrefix(cookieDomain, ".")
		return lookupDomain == bare || strings.HasSuffix(lookupDomain, "."+bare)
	}
	return cookieDomain == lookupDomain
}

// canonicalBrowser collapses kooky's `Browser()` strings (which vary across
// versions: "chromium", "google-chrome", "Chrome", etc.) into the small
// canonical set we expose. Predictability matters because we persist the
// chosen value as `BrowserCookieRef.SourceBrowser`.
func canonicalBrowser(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(raw, "firefox"):
		return "firefox"
	case strings.Contains(raw, "safari"):
		return "safari"
	case strings.Contains(raw, "edge"):
		return "edge"
	case strings.Contains(raw, "brave"):
		return "brave"
	case strings.Contains(raw, "vivaldi"):
		return "vivaldi"
	case strings.Contains(raw, "opera"):
		return "opera"
	case strings.Contains(raw, "chrom"):
		return "chrome"
	}
	return raw
}

func (r *kookyReader) ReadCookie(ctx context.Context, domain, name, preferredBrowser string) (Cookie, error) {
	if r.readTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.readTimeout)
		defer cancel()
	}

	preferredBrowser = strings.ToLower(strings.TrimSpace(preferredBrowser))

	// Use the top-level helper that fans out across all registered finders.
	// kooky returns errors PER STORE (locked Chrome DB while browser is
	// open, keychain prompt declined, etc.) but doesn't fail the whole
	// call — we just take whatever cookies it could read.
	cookies, err := kooky.ReadCookies(ctx, kooky.Name(name))
	if err != nil && len(cookies) == 0 {
		return Cookie{}, fmt.Errorf("kooky read: %w", err)
	}

	var best Cookie
	bestSet := false
	for _, kc := range cookies {
		if kc == nil {
			continue
		}
		if !matches(kc.Domain, domain) {
			continue
		}
		source := ""
		filePath := ""
		if kc.Browser != nil {
			source = canonicalBrowser(kc.Browser.Browser())
			filePath = kc.Browser.FilePath()
		}
		candidate := Cookie{
			Name:      kc.Name,
			Value:     kc.Value,
			Domain:    kc.Domain,
			Path:      kc.Path,
			Expires:   kc.Expires,
			HTTPOnly:  kc.HttpOnly,
			Secure:    kc.Secure,
			Source:    source,
			StorePath: filePath,
		}
		// Selection rule:
		//   1. preferred-browser hit always wins over non-preferred (so users
		//      who pin a browser don't accidentally get cookies from another
		//      profile they happen to have logged in to).
		//   2. otherwise, latest Expires wins — gives us the freshest session
		//      when multiple browsers have valid cookies.
		switch {
		case !bestSet:
			best = candidate
			bestSet = true
		case preferredBrowser != "" && candidate.Source == preferredBrowser && best.Source != preferredBrowser:
			best = candidate
		case preferredBrowser != "" && candidate.Source != preferredBrowser && best.Source == preferredBrowser:
			// keep current best
		case candidate.Expires.After(best.Expires):
			best = candidate
		}
	}

	if !bestSet {
		return Cookie{}, ErrNoCookieFound
	}
	return best, nil
}

func (r *kookyReader) AvailableBrowsers(ctx context.Context) ([]string, error) {
	stores := kooky.FindAllCookieStores(ctx)
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	for _, s := range stores {
		bn := canonicalBrowser(s.Browser())
		_ = s.Close()
		if bn == "" || seen[bn] {
			continue
		}
		seen[bn] = true
		out = append(out, bn)
	}
	return out, nil
}

// FakeReader is a test double for Reader. Tests populate Cookies and
// optionally Err; ReadCookie returns the first matching entry.
type FakeReader struct {
	Cookies []Cookie
	Err     error

	mu    sync.Mutex
	calls int
}

// Calls reports how many times ReadCookie has been invoked. Used by tests
// that care about caching / retry behavior in callers.
func (f *FakeReader) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *FakeReader) ReadCookie(_ context.Context, domain, name, _ string) (Cookie, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.Err != nil {
		return Cookie{}, f.Err
	}
	for _, c := range f.Cookies {
		if c.Name != name {
			continue
		}
		if !matches(c.Domain, domain) {
			continue
		}
		return c, nil
	}
	return Cookie{}, ErrNoCookieFound
}

func (f *FakeReader) AvailableBrowsers(_ context.Context) ([]string, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, c := range f.Cookies {
		if c.Source == "" || seen[c.Source] {
			continue
		}
		seen[c.Source] = true
		out = append(out, c.Source)
	}
	return out, nil
}
