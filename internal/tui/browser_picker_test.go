package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// availableBrowsersLoadedMsg → picker hydrates the browser list and clears
// its loading flag. Stale messages for a different account get dropped.
func TestAvailableBrowsersLoadedMsg_HydratesPicker(t *testing.T) {
	m := Model{}
	m.settings.show = true
	m.settings.tab = settingsTabAPIKeys
	m.settings.browserPicker = browserPickerState{
		active:    true,
		accountID: "perplexity",
		domain:    ".perplexity.ai",
		loading:   true,
	}

	updated, _ := m.Update(availableBrowsersLoadedMsg{
		AccountID: "perplexity",
		Browsers:  []string{"firefox", "chrome"},
	})
	got := updated.(Model).settings.browserPicker

	if got.loading {
		t.Error("loading still true after hydrate")
	}
	if len(got.browsers) != 2 || got.browsers[0] != "firefox" {
		t.Errorf("browsers = %v, want [firefox chrome]", got.browsers)
	}
	if got.cursor != 0 {
		t.Errorf("cursor = %d, want 0", got.cursor)
	}
}

// availableBrowsersLoadedMsg for a *different* account → no-op. The user may
// have hit Esc and re-opened the picker for a different row before the
// async scan returned; we must not let the stale result clobber the new
// picker.
func TestAvailableBrowsersLoadedMsg_StaleAccountDropped(t *testing.T) {
	m := Model{}
	m.settings.show = true
	m.settings.tab = settingsTabAPIKeys
	m.settings.browserPicker = browserPickerState{
		active:    true,
		accountID: "perplexity",
		loading:   true,
	}

	updated, _ := m.Update(availableBrowsersLoadedMsg{
		AccountID: "stale-account",
		Browsers:  []string{"chrome"},
	})
	got := updated.(Model).settings.browserPicker

	if !got.loading {
		t.Error("loading flipped on stale msg, want sticky")
	}
	if len(got.browsers) != 0 {
		t.Errorf("browsers populated by stale msg: %v", got.browsers)
	}
}

// handleBrowserPickerKey: Enter on a hydrated picker tears down the picker
// and fires connectBrowserSessionCmd with the chosen browser. This is the
// step that protects against the keychain cascade — connect now scopes to
// one browser, never fans out.
func TestHandleBrowserPickerKey_EnterFiresConnect(t *testing.T) {
	fake := newBrowserSessionFake()
	m := Model{services: fake}
	m.settings.show = true
	m.settings.tab = settingsTabAPIKeys
	m.settings.browserPicker = browserPickerState{
		active:     true,
		accountID:  "perplexity",
		domain:     ".perplexity.ai",
		cookieName: "__Secure-next-auth.session-token",
		browsers:   []string{"firefox", "chrome"},
		cursor:     1, // user picked chrome
	}

	updated, cmd := m.handleBrowserPickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected connect cmd, got nil")
	}
	if got := updated.(Model).settings.browserPicker.active; got {
		t.Error("picker still active after enter")
	}

	// Drain the command — confirms the chosen browser threads through.
	msg := cmd().(browserSessionConnectedMsg)
	if msg.AccountID != "perplexity" {
		t.Errorf("AccountID = %q", msg.AccountID)
	}
	if fake.connectPreferred != "chrome" {
		t.Errorf("connect called with browser = %q, want chrome", fake.connectPreferred)
	}
}

// handleBrowserPickerKey: Esc cancels without firing a connect. No keychain
// prompt, no message; picker is fully reset.
func TestHandleBrowserPickerKey_EscCancels(t *testing.T) {
	m := Model{}
	m.settings.show = true
	m.settings.tab = settingsTabAPIKeys
	m.settings.browserPicker = browserPickerState{
		active:    true,
		accountID: "perplexity",
		browsers:  []string{"chrome"},
	}

	updated, cmd := m.handleBrowserPickerKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Errorf("esc fired a command: %T", cmd())
	}
	if updated.(Model).settings.browserPicker.active {
		t.Error("picker still active after esc")
	}
}

// handleBrowserPickerKey: Enter while loading is a no-op so the user can't
// race the async browser-list fetch.
func TestHandleBrowserPickerKey_EnterWhileLoadingIsNoop(t *testing.T) {
	m := Model{services: newBrowserSessionFake()}
	m.settings.show = true
	m.settings.tab = settingsTabAPIKeys
	m.settings.browserPicker = browserPickerState{
		active:    true,
		accountID: "perplexity",
		loading:   true,
	}

	_, cmd := m.handleBrowserPickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("enter fired cmd while loading: %T", cmd())
	}
}
