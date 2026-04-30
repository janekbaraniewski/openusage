package tui

import (
	"errors"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// browserSessionFakeServices augments fakeServices with browser-session-flow
// hooks so we can assert which calls the TUI made into the daemon-side
// service. Defined alongside fakeServices in
// telemetry_mapping_input_test.go; this file extends behaviour for new
// tests via embedding.
type browserSessionFakeServices struct {
	*fakeServices

	connectAccountID  string
	connectDomain     string
	connectCookieName string
	connectPreferred  string
	connectInfo       core.BrowserSessionInfo
	connectErr        error

	disconnectedAccountID string
	disconnectErr         error

	openedURL string
	openErr   error

	loadInfo core.BrowserSessionInfo
}

func (b *browserSessionFakeServices) ConnectBrowserSession(accountID, domain, cookieName, preferred string) (core.BrowserSessionInfo, error) {
	b.connectAccountID = accountID
	b.connectDomain = domain
	b.connectCookieName = cookieName
	b.connectPreferred = preferred
	if b.connectErr != nil {
		return core.BrowserSessionInfo{}, b.connectErr
	}
	return b.connectInfo, nil
}

func (b *browserSessionFakeServices) DisconnectBrowserSession(accountID string) error {
	b.disconnectedAccountID = accountID
	return b.disconnectErr
}

func (b *browserSessionFakeServices) LoadBrowserSessionInfo(string) core.BrowserSessionInfo {
	return b.loadInfo
}

func (b *browserSessionFakeServices) OpenProviderConsole(url string) error {
	b.openedURL = url
	return b.openErr
}

func newBrowserSessionFake() *browserSessionFakeServices {
	return &browserSessionFakeServices{fakeServices: &fakeServices{}}
}

// connectBrowserSessionCmd → on success the message carries info.
func TestConnectBrowserSessionCmd_Success(t *testing.T) {
	fake := newBrowserSessionFake()
	fake.connectInfo = core.BrowserSessionInfo{
		Connected:     true,
		Domain:        ".opencode.ai",
		CookieName:    "auth",
		SourceBrowser: "chrome",
	}
	m := Model{services: fake}

	cmd := m.connectBrowserSessionCmd("opencode-console", ".opencode.ai", "auth", "")
	msg := cmd().(browserSessionConnectedMsg)

	if msg.Err != nil {
		t.Fatalf("Err = %v, want nil", msg.Err)
	}
	if msg.AccountID != "opencode-console" {
		t.Errorf("AccountID = %q, want opencode-console", msg.AccountID)
	}
	if !msg.Info.Connected {
		t.Error("Info.Connected = false, want true")
	}
	if msg.Info.SourceBrowser != "chrome" {
		t.Errorf("SourceBrowser = %q, want chrome", msg.Info.SourceBrowser)
	}
	if fake.connectDomain != ".opencode.ai" || fake.connectCookieName != "auth" {
		t.Errorf("service called with wrong args: domain=%q cookie=%q", fake.connectDomain, fake.connectCookieName)
	}
}

// connectBrowserSessionCmd → propagates errors as msg.Err.
func TestConnectBrowserSessionCmd_FailurePropagated(t *testing.T) {
	fake := newBrowserSessionFake()
	fake.connectErr = errors.New("no cookie in any browser")
	m := Model{services: fake}

	cmd := m.connectBrowserSessionCmd("opencode-console", ".opencode.ai", "auth", "")
	msg := cmd().(browserSessionConnectedMsg)

	if msg.Err == nil || msg.Err.Error() != "no cookie in any browser" {
		t.Errorf("Err = %v, want 'no cookie in any browser'", msg.Err)
	}
}

// connectBrowserSessionCmd with nil services → returns error message rather
// than panicking. Daemon-disconnect path.
func TestConnectBrowserSessionCmd_NoServices(t *testing.T) {
	m := Model{services: nil}
	cmd := m.connectBrowserSessionCmd("x", ".x.com", "auth", "")
	msg := cmd().(browserSessionConnectedMsg)
	if msg.Err == nil {
		t.Fatal("expected error, got nil")
	}
}

// disconnectBrowserSessionCmd → calls service, propagates account ID.
func TestDisconnectBrowserSessionCmd(t *testing.T) {
	fake := newBrowserSessionFake()
	m := Model{services: fake}

	cmd := m.disconnectBrowserSessionCmd("opencode-console")
	msg := cmd().(browserSessionDisconnectedMsg)

	if msg.AccountID != "opencode-console" {
		t.Errorf("AccountID = %q", msg.AccountID)
	}
	if fake.disconnectedAccountID != "opencode-console" {
		t.Errorf("service not called with correct account: %q", fake.disconnectedAccountID)
	}
	if msg.Err != nil {
		t.Errorf("Err = %v, want nil", msg.Err)
	}
}

// openProviderConsoleCmd → invokes service with URL and propagates errors.
func TestOpenProviderConsoleCmd(t *testing.T) {
	fake := newBrowserSessionFake()
	m := Model{services: fake}

	cmd := m.openProviderConsoleCmd("https://opencode.ai/login")
	msg := cmd().(providerConsoleOpenedMsg)

	if fake.openedURL != "https://opencode.ai/login" {
		t.Errorf("openedURL = %q", fake.openedURL)
	}
	if msg.URL != "https://opencode.ai/login" {
		t.Errorf("msg.URL = %q", msg.URL)
	}
	if msg.Err != nil {
		t.Errorf("Err = %v", msg.Err)
	}
}
