package dashboardapp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/browsercookies"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/integrations"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

type Service struct {
	ctx           context.Context
	cookieReader  browsercookies.Reader
	browserOpener func(url string) error // overridable for tests
}

func NewService(ctx context.Context) *Service {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Service{
		ctx:           ctx,
		cookieReader:  browsercookies.New(),
		browserOpener: openInDefaultBrowser,
	}
}

// SetCookieReader is exposed for tests; production code uses the kooky-backed
// reader installed by NewService.
func (s *Service) SetCookieReader(r browsercookies.Reader) {
	if r != nil {
		s.cookieReader = r
	}
}

// SetBrowserOpener is exposed for tests so we don't actually launch a browser.
func (s *Service) SetBrowserOpener(fn func(string) error) {
	if fn != nil {
		s.browserOpener = fn
	}
}

func (s *Service) SaveTheme(themeName string) error {
	return config.SaveTheme(themeName)
}

func (s *Service) SaveDashboardProviders(providersCfg []config.DashboardProviderConfig) error {
	return config.SaveDashboardProviders(providersCfg)
}

func (s *Service) SaveDashboardView(view string) error {
	return config.SaveDashboardView(view)
}

func (s *Service) SaveDashboardWidgetSections(sections []config.DashboardWidgetSection) error {
	return config.SaveDashboardWidgetSections(sections)
}

func (s *Service) SaveDetailWidgetSections(sections []config.DetailWidgetSection) error {
	return config.SaveDetailWidgetSections(sections)
}

func (s *Service) SaveDashboardHideSectionsWithNoData(hide bool) error {
	return config.SaveDashboardHideSectionsWithNoData(hide)
}

func (s *Service) SaveTimeWindow(window string) error {
	return config.SaveTimeWindow(window)
}

func (s *Service) SaveProviderLink(source, target string) error {
	return config.SaveProviderLink(source, target)
}

func (s *Service) DeleteProviderLink(source string) error {
	return config.DeleteProviderLink(source)
}

func (s *Service) ValidateAPIKey(accountID, providerID, apiKey string) (bool, string) {
	var provider core.UsageProvider
	for _, p := range providers.AllProviders() {
		if p.ID() == providerID {
			provider = p
			break
		}
	}
	if provider == nil {
		return false, "unknown provider"
	}

	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	snap, err := provider.Fetch(ctx, core.AccountConfig{
		ID:       accountID,
		Provider: providerID,
		Token:    apiKey,
	})
	if err != nil {
		return false, err.Error()
	}
	if snap.Status == core.StatusAuth || snap.Status == core.StatusError {
		msg := strings.TrimSpace(snap.Message)
		if msg == "" {
			msg = string(snap.Status)
		}
		return false, msg
	}
	return true, ""
}

func (s *Service) SaveCredential(accountID, apiKey string) error {
	return config.SaveCredential(accountID, apiKey)
}

func (s *Service) DeleteCredential(accountID string) error {
	return config.DeleteCredential(accountID)
}

func (s *Service) InstallIntegration(id integrations.ID) ([]integrations.Status, error) {
	manager := integrations.NewDefaultManager()
	err := manager.Install(id)
	return manager.ListStatuses(), err
}

// LoadBrowserSessionInfo reads the stored session for an account and returns
// presentation data. Never returns the cookie value — that's daemon-only.
func (s *Service) LoadBrowserSessionInfo(accountID string) core.BrowserSessionInfo {
	info := core.BrowserSessionInfo{}
	sess, ok, err := config.LoadSession(accountID)
	if err != nil || !ok {
		return info
	}
	info.Connected = strings.TrimSpace(sess.Value) != ""
	info.Domain = sess.Domain
	info.CookieName = sess.CookieName
	info.SourceBrowser = sess.SourceBrowser
	info.CapturedAt = sess.CapturedAt
	info.ExpiresAt = sess.ExpiresAt
	if sess.ExpiresAt != "" {
		if t, perr := time.Parse(time.RFC3339, sess.ExpiresAt); perr == nil {
			info.Expired = time.Now().After(t)
		}
	}
	return info
}

// ConnectBrowserSession reads the cookie identified by (domain, cookieName)
// from the user's logged-in browsers and stores it under the given account.
// Returns the captured source browser on success. Used by the TUI's
// "Connect via browser" flow.
//
// preferredBrowser may be empty (first connect) or carry the persisted
// SourceBrowser hint from previous extractions. The reader still scans every
// browser; the hint just controls tie-breaking + reduces keychain prompts.
func (s *Service) ConnectBrowserSession(accountID, domain, cookieName, preferredBrowser string) (core.BrowserSessionInfo, error) {
	if strings.TrimSpace(accountID) == "" {
		return core.BrowserSessionInfo{}, errors.New("account ID required")
	}
	if strings.TrimSpace(domain) == "" || strings.TrimSpace(cookieName) == "" {
		return core.BrowserSessionInfo{}, errors.New("domain and cookie name required")
	}

	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()

	cookie, err := s.cookieReader.ReadCookie(ctx, domain, cookieName, preferredBrowser)
	if err != nil {
		if errors.Is(err, browsercookies.ErrNoCookieFound) {
			return core.BrowserSessionInfo{}, fmt.Errorf("no %s cookie found in any supported browser — log into the site and try again", cookieName)
		}
		return core.BrowserSessionInfo{}, fmt.Errorf("read cookie: %w", err)
	}

	now := time.Now().UTC()
	expires := ""
	if !cookie.Expires.IsZero() {
		expires = cookie.Expires.UTC().Format(time.RFC3339)
	}
	session := config.BrowserSession{
		Domain:        cookie.Domain,
		CookieName:    cookie.Name,
		Value:         cookie.Value,
		SourceBrowser: cookie.Source,
		CapturedAt:    now.Format(time.RFC3339),
		ExpiresAt:     expires,
	}
	if err := config.SaveSession(accountID, session); err != nil {
		return core.BrowserSessionInfo{}, fmt.Errorf("save session: %w", err)
	}

	return core.BrowserSessionInfo{
		Connected:     true,
		Domain:        session.Domain,
		CookieName:    session.CookieName,
		SourceBrowser: session.SourceBrowser,
		CapturedAt:    session.CapturedAt,
		ExpiresAt:     session.ExpiresAt,
		Expired:       false,
	}, nil
}

// DisconnectBrowserSession removes the stored cookie for an account. Used
// by the "x" key on a connected row to revoke openusage's stored credential
// (the browser session itself is unaffected).
func (s *Service) DisconnectBrowserSession(accountID string) error {
	if strings.TrimSpace(accountID) == "" {
		return errors.New("account ID required")
	}
	return config.DeleteSession(accountID)
}

// OpenProviderConsole launches the provider's login/console URL in the user's
// default browser. The "y" path of the connect modal — for users not currently
// logged in.
func (s *Service) OpenProviderConsole(url string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return errors.New("console URL is empty")
	}
	if s.browserOpener == nil {
		return errors.New("browser opener unavailable")
	}
	return s.browserOpener(url)
}

// AvailableBrowsers reports which browsers have a readable cookie store on
// this machine. Used by the connect modal to show "we'll look in: Chrome,
// Firefox" before the user commits.
func (s *Service) AvailableBrowsers() ([]string, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	return s.cookieReader.AvailableBrowsers(ctx)
}

// openInDefaultBrowser is the production browser-launcher. exec.Command
// shells out to the OS-specific URL handler. Tests override via
// SetBrowserOpener.
func openInDefaultBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
