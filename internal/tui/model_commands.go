package tui

import (
	"fmt"
	"log"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/integrations"
)

func (m Model) persistThemeCmd(themeName string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return themePersistedMsg{err: fmt.Errorf("theme service unavailable")}
		}
		err := m.services.SaveTheme(themeName)
		if err != nil {
			log.Printf("theme persist: %v", err)
		}
		return themePersistedMsg{err: err}
	}
}

func (m Model) persistDashboardPrefsCmd() tea.Cmd {
	providers := m.dashboardConfigProviders()
	return func() tea.Msg {
		if m.services == nil {
			return dashboardPrefsPersistedMsg{err: fmt.Errorf("dashboard settings service unavailable")}
		}
		err := m.services.SaveDashboardProviders(providers)
		if err != nil {
			log.Printf("dashboard settings persist: %v", err)
		}
		return dashboardPrefsPersistedMsg{err: err}
	}
}

func (m Model) persistDashboardViewCmd() tea.Cmd {
	view := string(m.configuredDashboardView())
	return func() tea.Msg {
		if m.services == nil {
			return dashboardViewPersistedMsg{err: fmt.Errorf("dashboard view service unavailable")}
		}
		err := m.services.SaveDashboardView(view)
		if err != nil {
			log.Printf("dashboard view persist: %v", err)
		}
		return dashboardViewPersistedMsg{err: err}
	}
}

func (m Model) persistDashboardWidgetSectionsCmd() tea.Cmd {
	sections := m.dashboardWidgetSectionConfigEntries()
	return func() tea.Msg {
		if m.services == nil {
			return dashboardWidgetSectionsPersistedMsg{err: fmt.Errorf("dashboard sections service unavailable")}
		}
		err := m.services.SaveDashboardWidgetSections(sections)
		if err != nil {
			log.Printf("dashboard widget sections persist: %v", err)
		}
		return dashboardWidgetSectionsPersistedMsg{err: err}
	}
}

func (m Model) persistDetailWidgetSectionsCmd() tea.Cmd {
	sections := m.detailWidgetSectionConfigEntries()
	return func() tea.Msg {
		if m.services == nil {
			return detailWidgetSectionsPersistedMsg{err: fmt.Errorf("detail sections service unavailable")}
		}
		err := m.services.SaveDetailWidgetSections(sections)
		if err != nil {
			log.Printf("detail widget sections persist: %v", err)
		}
		return detailWidgetSectionsPersistedMsg{err: err}
	}
}

func (m Model) persistDashboardHideSectionsWithNoDataCmd() tea.Cmd {
	hide := m.hideSectionsWithNoData
	return func() tea.Msg {
		if m.services == nil {
			return dashboardHideSectionsWithNoDataPersistedMsg{err: fmt.Errorf("dashboard empty-state service unavailable")}
		}
		err := m.services.SaveDashboardHideSectionsWithNoData(hide)
		if err != nil {
			log.Printf("dashboard hide sections with no data persist: %v", err)
		}
		return dashboardHideSectionsWithNoDataPersistedMsg{err: err}
	}
}

func (m Model) persistTimeWindowCmd(window string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return timeWindowPersistedMsg{err: fmt.Errorf("time window service unavailable")}
		}
		err := m.services.SaveTimeWindow(window)
		if err != nil {
			log.Printf("time window persist: %v", err)
		}
		return timeWindowPersistedMsg{err: err}
	}
}

func (m Model) persistProviderLinkCmd(source, target string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return providerLinkPersistedMsg{source: source, target: target, err: fmt.Errorf("provider link service unavailable")}
		}
		err := m.services.SaveProviderLink(source, target)
		if err != nil {
			log.Printf("provider link persist (%s→%s): %v", source, target, err)
		}
		return providerLinkPersistedMsg{source: source, target: target, err: err}
	}
}

func (m Model) deleteProviderLinkCmd(source string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return providerLinkDeletedMsg{source: source, err: fmt.Errorf("provider link service unavailable")}
		}
		err := m.services.DeleteProviderLink(source)
		if err != nil {
			log.Printf("provider link delete (%s): %v", source, err)
		}
		return providerLinkDeletedMsg{source: source, err: err}
	}
}

func (m Model) validateKeyCmd(accountID, providerID, apiKey string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return validateKeyResultMsg{AccountID: accountID, Valid: false, Error: "validation service unavailable"}
		}
		valid, errMsg := m.services.ValidateAPIKey(accountID, providerID, apiKey)
		return validateKeyResultMsg{AccountID: accountID, Valid: valid, Error: errMsg}
	}
}

func (m Model) saveCredentialCmd(accountID, apiKey string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return credentialSavedMsg{AccountID: accountID, Err: fmt.Errorf("credential service unavailable")}
		}
		err := m.services.SaveCredential(accountID, apiKey)
		return credentialSavedMsg{AccountID: accountID, Err: err}
	}
}

func (m Model) deleteCredentialCmd(accountID string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return credentialDeletedMsg{AccountID: accountID, Err: fmt.Errorf("credential service unavailable")}
		}
		err := m.services.DeleteCredential(accountID)
		return credentialDeletedMsg{AccountID: accountID, Err: err}
	}
}

// connectBrowserSessionCmd kicks off the cookie-extraction → save flow.
// On success the resulting BrowserSessionInfo is delivered as a
// browserSessionConnectedMsg; the TUI uses it to flip the row's status to
// connected and trigger a fresh poll so the tile picks up the new auth.
//
// `browser` is the user's choice from the browser picker. It scopes the
// cookie read to one browser's stores so we never trigger more than a
// single OS keychain prompt per connect attempt.
func (m Model) connectBrowserSessionCmd(accountID, domain, cookieName, browser string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return browserSessionConnectedMsg{AccountID: accountID, Err: fmt.Errorf("browser session service unavailable")}
		}
		info, err := m.services.ConnectBrowserSession(accountID, domain, cookieName, browser)
		if err != nil {
			log.Printf("browser session connect (%s): %v", accountID, err)
		}
		return browserSessionConnectedMsg{AccountID: accountID, Info: info, Err: err}
	}
}

// loadAvailableBrowsersCmd asks the cookie-reader which browsers have a
// cookie store on disk. The picker uses the result to populate its choice
// list. We do this asynchronously because file enumeration on a system with
// many profiles can take a few hundred ms and we don't want the keystroke
// that opens the picker to block the UI.
func (m Model) loadAvailableBrowsersCmd(accountID string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return availableBrowsersLoadedMsg{AccountID: accountID, Err: fmt.Errorf("browser session service unavailable")}
		}
		browsers, err := m.services.AvailableBrowsers()
		return availableBrowsersLoadedMsg{AccountID: accountID, Browsers: browsers, Err: err}
	}
}

// disconnectBrowserSessionCmd removes openusage's stored cookie for the
// account. Doesn't touch the user's browser session.
func (m Model) disconnectBrowserSessionCmd(accountID string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return browserSessionDisconnectedMsg{AccountID: accountID, Err: fmt.Errorf("browser session service unavailable")}
		}
		err := m.services.DisconnectBrowserSession(accountID)
		if err != nil {
			log.Printf("browser session disconnect (%s): %v", accountID, err)
		}
		return browserSessionDisconnectedMsg{AccountID: accountID, Err: err}
	}
}

// openProviderConsoleCmd asks the OS to launch the provider's login URL in
// the user's default browser. Used when the user picks "open browser" in
// the connect modal because they're not currently logged in.
func (m Model) openProviderConsoleCmd(url string) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return providerConsoleOpenedMsg{Err: fmt.Errorf("browser opener unavailable")}
		}
		err := m.services.OpenProviderConsole(url)
		if err != nil {
			log.Printf("open provider console (%s): %v", url, err)
		}
		return providerConsoleOpenedMsg{URL: url, Err: err}
	}
}

func (m Model) installIntegrationCmd(id integrations.ID) tea.Cmd {
	return func() tea.Msg {
		if m.services == nil {
			return integrationInstallResultMsg{IntegrationID: id, Err: fmt.Errorf("integration service unavailable")}
		}
		statuses, err := m.services.InstallIntegration(id)
		return integrationInstallResultMsg{
			IntegrationID: id,
			Statuses:      statuses,
			Err:           err,
		}
	}
}

func (m Model) cycleTimeWindow() (tea.Model, tea.Cmd) {
	next := core.NextTimeWindow(m.timeWindow)
	m = m.beginTimeWindowRefresh(next)
	return m, m.persistTimeWindowCmd(string(next))
}

func (m Model) requestRefresh() Model {
	m.refreshing = true
	if m.onRefresh != nil {
		m.onRefresh(m.timeWindow)
	}
	return m
}

// enterDetailMode switches to detail view while preserving the selected time window.
func (m Model) enterDetailMode() Model {
	m.mode = modeDetail
	m.detailOffset = 0
	return m
}

// exitDetailMode returns to list view.
func (m Model) exitDetailMode() Model {
	m.mode = modeList
	return m
}

func (m Model) beginTimeWindowRefresh(window core.TimeWindow) Model {
	m.timeWindow = window
	m.invalidateRenderCaches()
	if m.onTimeWindowChange != nil {
		m.onTimeWindowChange(window)
	}
	m.refreshing = true
	if m.onRefresh != nil {
		m.onRefresh(window)
	}
	return m
}

func (m Model) installDaemonCmd() tea.Cmd {
	fn := m.onInstallDaemon
	return func() tea.Msg {
		if fn == nil {
			return daemonInstallResultMsg{err: fmt.Errorf("install callback not configured")}
		}
		return daemonInstallResultMsg{err: fn()}
	}
}

func snapshotsReady(snaps map[string]core.UsageSnapshot) bool {
	if len(snaps) == 0 {
		return false
	}
	for _, snap := range snaps {
		if snap.Status != core.StatusUnknown {
			return true
		}
		if len(snap.Metrics) > 0 ||
			len(snap.Resets) > 0 ||
			len(snap.DailySeries) > 0 ||
			len(snap.ModelUsage) > 0 {
			return true
		}
	}
	return false
}

func (m Model) renderDashboard() string {
	w, h := m.width, m.height

	header := m.renderHeader(w)
	headerH := strings.Count(header, "\n") + 1

	footer := m.renderFooter(w)
	footerH := strings.Count(footer, "\n") + 1

	contentH := h - headerH - footerH
	if contentH < 3 {
		contentH = 3
	}

	var content string

	switch m.screen {
	case screenAnalytics:
		content = m.renderAnalyticsContent(w, contentH)
	default:
		content = m.renderDashboardContent(w, contentH)
	}

	return header + "\n" + content + "\n" + footer
}
