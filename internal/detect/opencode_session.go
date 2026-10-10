package detect

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/browsercookies"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// OpenCode console session (opencode.ai "auth" cookie). The OpenCode Go
// quota windows (5h / weekly / monthly) and the Zen balance are only exposed
// through the web console, never to API keys, so the provider needs this
// cookie to render real usage meters.
const (
	opencodeConsoleAccountID  = "opencode"
	opencodeConsoleDomain     = ".opencode.ai"
	opencodeConsoleCookieName = "auth"

	// opencodeSessionProbeInterval throttles the browser-store probe while
	// no session has been imported yet. AutoDetect runs on every daemon
	// poll cycle; scanning cookie stores every 30s would be wasteful.
	opencodeSessionProbeInterval = 10 * time.Minute
)

// Seams for tests.
var (
	opencodeCookieReader = func() browsercookies.Reader {
		return browsercookies.NewWithTimeout(5 * time.Second)
	}
	opencodeLoadSession = config.LoadSession
	opencodeSaveSession = config.SaveSession
	opencodeNow         = time.Now

	opencodeProbeMu   sync.Mutex
	opencodeLastProbe time.Time
)

// detectOpenCodeConsoleSession attaches the OpenCode console session to the
// detected opencode account. When a session is already stored it is only
// referenced, never refreshed (refreshing from the live browser store can
// clobber a sibling account's cookie). When none is stored, it silently
// imports the cookie from browsers that can be read without an OS keychain
// prompt (Firefox, Safari) and persists it with config.SaveSession; the
// provider then reads it via config.LoadSession on each poll.
//
// Keychain-protected browsers (Chrome, Arc, Brave, Edge, ...) are never read
// here because that would pop a system dialog during background detection;
// users on those browsers connect once via Settings -> 5 KEYS -> c.
func detectOpenCodeConsoleSession(result *Result) {
	idx := -1
	for i := range result.Accounts {
		if result.Accounts[i].ID == opencodeConsoleAccountID && result.Accounts[i].Provider == "opencode" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	acct := &result.Accounts[idx]

	if sess, ok, err := opencodeLoadSession(acct.ID); err == nil && ok && sess.Value != "" {
		attachOpenCodeSessionRef(acct, sess.SourceBrowser)
		acct.SetHint("console_session_source", "credentials_json")
		return
	}

	opencodeProbeMu.Lock()
	now := opencodeNow()
	if !opencodeLastProbe.IsZero() && now.Sub(opencodeLastProbe) < opencodeSessionProbeInterval {
		opencodeProbeMu.Unlock()
		return
	}
	opencodeLastProbe = now
	opencodeProbeMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// browser "" = only the keychain-free stores (no OS prompt).
	cookie, err := opencodeCookieReader().ReadCookie(ctx, opencodeConsoleDomain, opencodeConsoleCookieName, "")
	if err != nil || cookie.Value == "" || cookie.IsExpired() {
		return
	}

	sess := config.BrowserSession{
		Domain:        opencodeConsoleDomain,
		CookieName:    opencodeConsoleCookieName,
		Value:         cookie.Value,
		SourceBrowser: cookie.Source,
		CapturedAt:    now.UTC().Format(time.RFC3339),
	}
	if !cookie.Expires.IsZero() {
		sess.ExpiresAt = cookie.Expires.UTC().Format(time.RFC3339)
	}
	if err := opencodeSaveSession(acct.ID, sess); err != nil {
		log.Printf("[detect] OpenCode console session: save failed: %v", err)
		return
	}
	attachOpenCodeSessionRef(acct, cookie.Source)
	acct.SetHint("console_session_source", "browser:"+cookie.Source)
	log.Printf("[detect] OpenCode console session imported from %s", cookie.Source)
}

func attachOpenCodeSessionRef(acct *core.AccountConfig, browser string) {
	acct.BrowserCookie = &core.BrowserCookieRef{
		Domain:        opencodeConsoleDomain,
		CookieName:    opencodeConsoleCookieName,
		SourceBrowser: browser,
	}
}
