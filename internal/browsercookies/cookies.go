// Package browsercookies extracts session cookies from the user's installed
// browsers (Chrome, Firefox, Safari, Edge, Brave). It is the foundation for
// openusage's browser-session-auth path — the credential-acquisition mechanism
// for providers whose billing / usage / account data lives behind dashboard
// session cookies and isn't reachable via API key.
//
// Reads are always scoped to a single browser. The TUI picks one explicitly
// (so the user sees at most one OS keychain prompt — never a cascade across
// every Chromium-family browser on the system).
//
// See docs/BROWSER_SESSION_AUTH_DESIGN.md for the rationale and the
// per-platform extraction details.
package browsercookies

import (
	"context"
	"errors"
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

// ErrNoCookieFound is returned when no matching cookie was found in the
// browser the caller asked us to scan. Callers should treat this as
// "the user is not currently logged into the relevant site in <that browser>".
var ErrNoCookieFound = errors.New("browsercookies: no matching cookie found")

// keychainProtectedBrowsers is the set of browsers whose cookie stores
// require an OS-level secret (macOS Keychain item, libsecret, etc.) to
// decrypt. Each one prompts independently, so we MUST never fan out across
// more than one of these per ReadCookie call.
var keychainProtectedBrowsers = map[string]bool{
	"chrome":  true, // also covers chromium — canonicalBrowser collapses them
	"edge":    true,
	"brave":   true,
	"vivaldi": true,
	"opera":   true,
}

// noKeychainBrowsers is the set we can safely scan without triggering an OS
// secret prompt. Used as the auto-fallback when the caller didn't pre-pick
// a browser (rare — the TUI always picks one).
var noKeychainBrowsers = []string{"firefox", "safari"}

// Reader is a small surface around kooky for openusage's needs. The interface
// exists so tests can swap in a fake without spinning up a real browser
// store on disk. The concrete implementation is &kookyReader{}.
type Reader interface {
	// ReadCookie returns the freshest cookie matching (domain, name) inside
	// `browser`'s cookie stores. Reads NEVER fan out to other browsers —
	// callers must specify which browser to look in. This is the contract
	// that protects the user from a keychain-prompt cascade on macOS.
	//
	// If browser is empty, the reader scans only browsers that don't
	// require an OS secret (Firefox, Safari). Pass an explicit browser to
	// scan a Chromium-family store.
	ReadCookie(ctx context.Context, domain, name, browser string) (Cookie, error)

	// AvailableBrowsers reports which supported browsers have at least one
	// cookie store on disk. The TUI uses it to render a picker so the user
	// can choose where to look BEFORE we trigger any keychain prompt.
	AvailableBrowsers(ctx context.Context) ([]string, error)
}

// New returns the default Reader implementation backed by kooky. Cookie reads
// are bounded by readTimeout — kooky calls into the OS keychain on first
// Chrome read, which can hang or wait for Touch ID; we never want a poll to
// stall on this. 30s is generous enough for a real prompt-and-approve flow
// and tight enough to fall through to "no cookie found" if something is
// genuinely broken.
func New() Reader {
	return &kookyReader{readTimeout: 30 * time.Second}
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
// chosen value as `BrowserCookieRef.SourceBrowser` and use it as the picker
// key on subsequent connects.
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

// readFromStores reads cookies from the given pre-selected stores and picks
// the freshest match for (domain, name). Each store is decrypted exactly
// once — that's the keychain-prompt unit on macOS — so we trust callers to
// have already filtered down to a single browser.
func readFromStores(stores []kooky.CookieStore, domain, name string) (Cookie, bool) {
	var best Cookie
	bestSet := false
	for _, store := range stores {
		if store == nil {
			continue
		}
		// TraverseCookies on a single store reads only that store's file;
		// there's no fan-out beyond what we already filtered. The Name
		// filter narrows the in-memory pass; we still re-check below
		// because kooky's filter doesn't know about leading-dot domain
		// matching.
		for kc, err := range store.TraverseCookies(kooky.Name(name)) {
			if err != nil || kc == nil {
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
			if !bestSet || candidate.Expires.After(best.Expires) {
				best = candidate
				bestSet = true
			}
		}
	}
	return best, bestSet
}

func (r *kookyReader) ReadCookie(ctx context.Context, domain, name, browser string) (Cookie, error) {
	if r.readTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.readTimeout)
		defer cancel()
	}

	browser = canonicalBrowser(browser)

	// Enumerate stores by metadata only — this step DOES NOT decrypt
	// anything, so it never triggers a keychain prompt. We then pick which
	// stores to actually read from.
	all := kooky.FindAllCookieStores(ctx)
	defer func() {
		for _, s := range all {
			if s != nil {
				_ = s.Close()
			}
		}
	}()

	picked := pickStoresForBrowser(all, browser)
	if len(picked) == 0 {
		return Cookie{}, ErrNoCookieFound
	}

	cookie, ok := readFromStores(picked, domain, name)
	if !ok {
		return Cookie{}, ErrNoCookieFound
	}
	return cookie, nil
}

// pickStoresForBrowser filters the discovered stores down to a single browser.
// Empty `browser` means "auto" — pick stores that don't require an OS secret
// to decrypt (Firefox, Safari). For a Chromium-family browser, return only
// that browser's stores so we never cascade keychain prompts.
func pickStoresForBrowser(all []kooky.CookieStore, browser string) []kooky.CookieStore {
	if browser == "" {
		var out []kooky.CookieStore
		for _, b := range noKeychainBrowsers {
			for _, s := range all {
				if s == nil {
					continue
				}
				if canonicalBrowser(s.Browser()) == b {
					out = append(out, s)
				}
			}
		}
		return out
	}

	var out []kooky.CookieStore
	for _, s := range all {
		if s == nil {
			continue
		}
		if canonicalBrowser(s.Browser()) == browser {
			out = append(out, s)
		}
	}
	return out
}

// IsKeychainProtected reports whether the named browser will prompt for an
// OS-level secret on first read. The TUI uses this to warn the user before
// firing a connect attempt on Chrome/Edge/Brave/etc.
func IsKeychainProtected(browser string) bool {
	return keychainProtectedBrowsers[canonicalBrowser(browser)]
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
// optionally Err; ReadCookie returns the first matching entry whose Source
// matches the requested browser (or any source when browser is empty).
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

func (f *FakeReader) ReadCookie(_ context.Context, domain, name, browser string) (Cookie, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.Err != nil {
		return Cookie{}, f.Err
	}
	browser = canonicalBrowser(browser)
	for _, c := range f.Cookies {
		if c.Name != name {
			continue
		}
		if !matches(c.Domain, domain) {
			continue
		}
		if browser != "" && canonicalBrowser(c.Source) != browser {
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
