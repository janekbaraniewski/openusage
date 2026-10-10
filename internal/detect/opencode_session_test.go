package detect

import (
	"errors"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/browsercookies"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// Keep every test in this package (including AutoDetect smoke tests) away
// from the developer's real browser stores and credentials.json.
func init() {
	opencodeCookieReader = func() browsercookies.Reader { return &browsercookies.FakeReader{Err: browsercookies.ErrNoCookieFound} }
	opencodeLoadSession = func(string) (config.BrowserSession, bool, error) { return config.BrowserSession{}, false, nil }
	opencodeSaveSession = func(string, config.BrowserSession) error { return errors.New("test: save disabled") }
}

func stubOpenCodeSession(t *testing.T, stored *config.BrowserSession, reader browsercookies.Reader) (saved map[string]config.BrowserSession) {
	t.Helper()
	saved = map[string]config.BrowserSession{}
	prevReader, prevLoad, prevSave, prevNow := opencodeCookieReader, opencodeLoadSession, opencodeSaveSession, opencodeNow
	opencodeProbeMu.Lock()
	opencodeLastProbe = time.Time{}
	opencodeProbeMu.Unlock()
	opencodeCookieReader = func() browsercookies.Reader { return reader }
	opencodeLoadSession = func(id string) (config.BrowserSession, bool, error) {
		if stored != nil {
			return *stored, true, nil
		}
		return config.BrowserSession{}, false, nil
	}
	opencodeSaveSession = func(id string, s config.BrowserSession) error { saved[id] = s; return nil }
	t.Cleanup(func() {
		opencodeCookieReader, opencodeLoadSession, opencodeSaveSession, opencodeNow = prevReader, prevLoad, prevSave, prevNow
	})
	return saved
}

func opencodeResult() *Result {
	r := &Result{}
	addAccount(r, core.AccountConfig{ID: "opencode", Provider: "opencode", Auth: "api_key"})
	return r
}

func TestDetectOpenCodeConsoleSession_ImportsFromKeychainFreeBrowser(t *testing.T) {
	reader := &browsercookies.FakeReader{Cookies: []browsercookies.Cookie{{
		Name: "auth", Value: "secret", Domain: ".opencode.ai", Source: "firefox",
		Expires: time.Now().Add(24 * time.Hour),
	}}}
	saved := stubOpenCodeSession(t, nil, reader)
	r := opencodeResult()

	detectOpenCodeConsoleSession(r)

	sess, ok := saved["opencode"]
	if !ok || sess.Value != "secret" || sess.SourceBrowser != "firefox" || sess.CookieName != "auth" {
		t.Fatalf("session not imported: %+v", saved)
	}
	acct := r.Accounts[0]
	if acct.BrowserCookie == nil || acct.BrowserCookie.Domain != ".opencode.ai" {
		t.Fatalf("BrowserCookie ref not attached: %+v", acct.BrowserCookie)
	}
	if got := acct.Hint("console_session_source", ""); got != "browser:firefox" {
		t.Fatalf("console_session_source = %q", got)
	}
}

func TestDetectOpenCodeConsoleSession_StoredSessionIsNotRefreshed(t *testing.T) {
	reader := &browsercookies.FakeReader{Cookies: []browsercookies.Cookie{{Name: "auth", Value: "newer", Domain: ".opencode.ai", Source: "safari"}}}
	saved := stubOpenCodeSession(t, &config.BrowserSession{Value: "stored", SourceBrowser: "chrome"}, reader)
	r := opencodeResult()

	detectOpenCodeConsoleSession(r)

	if reader.Calls() != 0 || len(saved) != 0 {
		t.Fatalf("stored session must not be refreshed from browser (calls=%d saved=%v)", reader.Calls(), saved)
	}
	if r.Accounts[0].BrowserCookie == nil || r.Accounts[0].Hint("console_session_source", "") != "credentials_json" {
		t.Fatalf("stored session not referenced: %+v", r.Accounts[0])
	}
}

func TestDetectOpenCodeConsoleSession_ProbeIsThrottled(t *testing.T) {
	reader := &browsercookies.FakeReader{Err: browsercookies.ErrNoCookieFound}
	stubOpenCodeSession(t, nil, reader)
	base := time.Now()
	opencodeNow = func() time.Time { return base }

	detectOpenCodeConsoleSession(opencodeResult())
	detectOpenCodeConsoleSession(opencodeResult())
	if reader.Calls() != 1 {
		t.Fatalf("calls = %d, want 1 within the throttle interval", reader.Calls())
	}
	opencodeNow = func() time.Time { return base.Add(opencodeSessionProbeInterval + time.Second) }
	detectOpenCodeConsoleSession(opencodeResult())
	if reader.Calls() != 2 {
		t.Fatalf("calls = %d, want 2 after the throttle interval", reader.Calls())
	}
}

func TestDetectOpenCodeConsoleSession_NoAccountNoProbe(t *testing.T) {
	reader := &browsercookies.FakeReader{}
	stubOpenCodeSession(t, nil, reader)
	detectOpenCodeConsoleSession(&Result{})
	if reader.Calls() != 0 {
		t.Fatalf("probed browsers without an opencode account")
	}
}
