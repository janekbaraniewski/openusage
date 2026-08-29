package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func (m Model) handleSettingsModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settings.creditLimitEditing {
		return m.handleCreditLimitEditKey(msg)
	}
	if m.settings.apiKeyEditing {
		return m.handleAPIKeyEditKey(msg)
	}
	if m.settings.tab == settingsTabTelemetry && m.settings.providerLinkPicker.active {
		return m.handleProviderLinkPickerKey(msg)
	}
	if m.settings.tab == settingsTabAPIKeys && m.settings.browserPicker.active {
		return m.handleBrowserPickerKey(msg)
	}

	ids := m.settingsIDs()
	if m.settings.tab == settingsTabAPIKeys {
		ids = m.apiKeysTabIDs()
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "backspace", ",", "S":
		m.closeSettingsModal()
		return m, nil
	case "tab", "right", "]":
		m.settings.tab = (m.settings.tab + 1) % settingsTabCount
		m.settings.bodyOffset = 0
		m.resetSettingsCursorForTab()
		return m, nil
	case "shift+tab", "left", "[":
		m.settings.tab = (m.settings.tab + settingsTabCount - 1) % settingsTabCount
		m.settings.bodyOffset = 0
		m.resetSettingsCursorForTab()
		return m, nil
	case "r":
		if m.settings.tab == settingsTabIntegrations {
			m.refreshIntegrationStatuses()
			m.settings.status = "integration status refreshed"
			return m, nil
		}
		m = m.requestRefresh()
		return m, nil
	}
	if len(msg.String()) == 1 {
		key := msg.String()[0]
		if key >= '1' && key <= '9' {
			idx := int(key - '1')
			if idx >= 0 && idx < int(settingsTabCount) {
				m.settings.tab = settingsModalTab(idx)
				m.settings.bodyOffset = 0
				m.resetSettingsCursorForTab()
				return m, nil
			}
		}
	}

	switch m.settings.tab {
	case settingsTabProviders:
		if next, cmd, handled := m.handleSettingsTabProvidersKey(msg, ids); handled {
			return next, cmd
		}
	case settingsTabWidgetSections:
		if next, cmd, handled := m.handleSettingsTabWidgetSectionsKey(msg); handled {
			return next, cmd
		}
	case settingsTabTheme:
		if next, cmd, handled := m.handleSettingsTabThemeKey(msg); handled {
			return next, cmd
		}
	case settingsTabView:
		if next, cmd, handled := m.handleSettingsTabViewKey(msg); handled {
			return next, cmd
		}
	case settingsTabAPIKeys:
		if next, cmd, handled := m.handleSettingsTabAPIKeysKey(msg, ids); handled {
			return next, cmd
		}
	case settingsTabTelemetry:
		if next, cmd, handled := m.handleSettingsTabTelemetryKey(msg); handled {
			return next, cmd
		}
	case settingsTabIntegrations:
		if next, cmd, handled := m.handleSettingsTabIntegrationsKey(msg); handled {
			return next, cmd
		}
	}

	return m, nil
}

func (m Model) handleCreditLimitEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.settings.creditLimitEditing = false
		m.settings.creditLimitInput = ""
		m.settings.status = "credit cap edit canceled"
		return m, nil
	case "enter":
		input := strings.TrimSpace(m.settings.creditLimitInput)
		var limit *float64
		if input != "" {
			value, err := strconv.ParseFloat(input, 64)
			if err != nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				m.settings.status = "enter a positive credit cap"
				return m, nil
			}
			limit = &value
		}
		accountID := m.settings.creditLimitEditAccountID
		m.settings.creditLimitEditing = false
		m.settings.creditLimitInput = ""
		m.settings.status = "saving credit cap..."
		return m, m.persistAccountCreditLimitCmd(accountID, limit)
	case "backspace":
		if len(m.settings.creditLimitInput) > 0 {
			m.settings.creditLimitInput = m.settings.creditLimitInput[:len(m.settings.creditLimitInput)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			for _, r := range msg.Runes {
				if (r >= '0' && r <= '9') || (r == '.' && !strings.Contains(m.settings.creditLimitInput, ".")) {
					m.settings.creditLimitInput += string(r)
				}
			}
		}
		return m, nil
	}
}

func cloneOptionalFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (m *Model) moveSelectedProvider(ids []string, delta int) tea.Cmd {
	if len(ids) == 0 || delta == 0 {
		return nil
	}
	from := clamp(m.settings.cursor, 0, len(ids)-1)
	to := from + delta
	if to < 0 || to >= len(ids) {
		return nil
	}

	m.providerOrder = loMove(m.providerOrder, from, to)
	m.settings.cursor = to
	m.settings.status = fmt.Sprintf("moved %s", ids[to])
	m.rebuildSortedIDs()
	return m.persistDashboardPrefsCmd()
}

func (m *Model) moveSelectedWidgetSection(delta int) tea.Cmd {
	if delta == 0 {
		return nil
	}
	entries := m.widgetSectionEntries()
	if len(entries) == 0 {
		return nil
	}

	from := clamp(m.settings.sectionRowCursor, 0, len(entries)-1)
	to := from + delta
	if to < 0 || to >= len(entries) {
		return nil
	}

	entries = loMove(entries, from, to)
	m.setWidgetSectionEntries(entries)
	m.settings.sectionRowCursor = to
	m.settings.status = fmt.Sprintf("moved %s", entries[to].ID)
	return m.persistDashboardWidgetSectionsCmd()
}

func (m *Model) toggleSelectedWidgetSection() tea.Cmd {
	entries := m.widgetSectionEntries()
	if len(entries) == 0 {
		return nil
	}
	idx := clamp(m.settings.sectionRowCursor, 0, len(entries)-1)
	entries[idx].Enabled = !entries[idx].Enabled
	m.setWidgetSectionEntries(entries)
	m.settings.status = "saving sections..."
	return m.persistDashboardWidgetSectionsCmd()
}

func (m *Model) moveSelectedDetailSection(delta int) tea.Cmd {
	if delta == 0 {
		return nil
	}
	entries := m.detailWidgetSectionEntries()
	if len(entries) == 0 {
		return nil
	}

	from := clamp(m.settings.sectionRowCursor, 0, len(entries)-1)
	to := from + delta
	if to < 0 || to >= len(entries) {
		return nil
	}

	entries = loMove(entries, from, to)
	m.setDetailWidgetSectionEntries(entries)
	m.settings.sectionRowCursor = to
	m.settings.status = fmt.Sprintf("moved %s", entries[to].ID)
	return m.persistDetailWidgetSectionsCmd()
}

func (m *Model) toggleSelectedDetailSection() tea.Cmd {
	entries := m.detailWidgetSectionEntries()
	if len(entries) == 0 {
		return nil
	}
	idx := clamp(m.settings.sectionRowCursor, 0, len(entries)-1)
	entries[idx].Enabled = !entries[idx].Enabled
	m.setDetailWidgetSectionEntries(entries)
	m.settings.status = "saving detail sections..."
	return m.persistDetailWidgetSectionsCmd()
}

func (m *Model) resetSettingsCursorForTab() {
	switch m.settings.tab {
	case settingsTabProviders, settingsTabAPIKeys, settingsTabIntegrations, settingsTabTelemetry:
		m.settings.cursor = 0
	case settingsTabWidgetSections:
		m.settings.sectionRowCursor = 0
		m.settings.sectionSubTab = 0
		m.settings.previewOffset = 0
	case settingsTabTheme:
		m.settings.themeCursor = clamp(ActiveThemeIndex(), 0, max(0, len(AvailableThemes())-1))
	case settingsTabView:
		m.settings.viewCursor = dashboardViewIndex(m.configuredDashboardView())
	}
}

func (m Model) handleAPIKeyEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.settings.apiKeyEditing = false
		m.settings.apiKeyInput = ""
		m.settings.apiKeyStatus = ""
		return m, nil
	case "enter":
		if m.settings.apiKeyInput == "" || m.settings.apiKeyStatus == "validating..." {
			return m, nil
		}
		id := m.settings.apiKeyEditAccountID
		providerID := providerForAccountID(id, m.accountProviders)
		m.settings.apiKeyStatus = "validating..."
		return m, m.validateKeyCmd(id, providerID, m.settings.apiKeyInput)
	case "backspace":
		if len(m.settings.apiKeyInput) > 0 {
			m.settings.apiKeyInput = m.settings.apiKeyInput[:len(m.settings.apiKeyInput)-1]
		}
		m.settings.apiKeyStatus = ""
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.settings.apiKeyInput += string(msg.Runes)
			m.settings.apiKeyStatus = ""
		}
		return m, nil
	}
}

func (m Model) applyTimeWindowAtCursor(rows []telemetryRow) (Model, tea.Cmd, bool) {
	cursor := m.telemetryRowCursor()
	if cursor < 0 || cursor >= len(rows) {
		return m, nil, false
	}
	if rows[cursor].kind != telemetryRowKindTimeWindow {
		return m, nil, false
	}
	tws := core.ValidTimeWindows
	idx := clamp(rows[cursor].index, 0, len(tws)-1)
	selected := tws[idx]
	m.settings.status = "saving time window..."
	m = m.beginTimeWindowRefresh(selected)
	return m, m.persistTimeWindowCmd(string(selected)), true
}

func (m Model) activateTelemetryRow(rows []telemetryRow) (Model, tea.Cmd, bool) {
	cursor := m.telemetryRowCursor()
	if cursor < 0 || cursor >= len(rows) {
		return m, nil, false
	}
	switch rows[cursor].kind {
	case telemetryRowKindTimeWindow:
		return m.applyTimeWindowAtCursor(rows)
	case telemetryRowKindUnmapped:
		return m.openProviderLinkPickerAtCursor(rows)
	}
	return m, nil, false
}

func (m Model) openProviderLinkPickerAtCursor(rows []telemetryRow) (Model, tea.Cmd, bool) {
	cursor := m.telemetryRowCursor()
	if cursor < 0 || cursor >= len(rows) || rows[cursor].kind != telemetryRowKindUnmapped {
		return m, nil, false
	}
	details := m.telemetryUnmappedDetails()
	idx := rows[cursor].index
	if idx < 0 || idx >= len(details) {
		return m, nil, false
	}
	source := details[idx].Source
	choices := m.configuredProviderIDs()
	if len(choices) == 0 {
		m.settings.providerLinkPicker = providerLinkPickerState{
			active: true,
			source: source,
			status: "",
		}
		return m, nil, true
	}

	startCursor := 0
	if details[idx].Suggestion != "" {
		for i, c := range choices {
			if c == details[idx].Suggestion {
				startCursor = i
				break
			}
		}
	}
	m.settings.providerLinkPicker = providerLinkPickerState{
		active:  true,
		source:  source,
		choices: choices,
		cursor:  startCursor,
	}
	return m, nil, true
}

func (m Model) clearProviderLinkAtCursor(rows []telemetryRow) (Model, tea.Cmd, bool) {
	cursor := m.telemetryRowCursor()
	if cursor < 0 || cursor >= len(rows) || rows[cursor].kind != telemetryRowKindUnmapped {
		return m, nil, false
	}
	details := m.telemetryUnmappedDetails()
	idx := rows[cursor].index
	if idx < 0 || idx >= len(details) {
		return m, nil, false
	}
	source := details[idx].Source
	m.settings.providerLinkPicker.status = "clearing user mapping for " + source + "..."
	return m, m.deleteProviderLinkCmd(source), true
}

func (m Model) startBrowserSessionConnect(accountID, providerID string) (tea.Model, tea.Cmd) {
	if !supportsBrowserSessionProvider(providerID) {
		return m, nil
	}
	if !isBrowserSessionProvider(providerID) && !m.supplementalBrowserSessionReady(accountID, providerID) {
		m.settings.apiKeyStatus = "configure the API key first, then connect browser session"
		return m, nil
	}

	domain, cookieName, _ := browserCookieRefForProvider(providerID)
	if m.services != nil {
		if info := m.services.LoadBrowserSessionInfo(accountID); info.Connected && info.SourceBrowser != "" {
			m.settings.apiKeyStatus = fmt.Sprintf("re-reading cookie from %s...", info.SourceBrowser)
			return m, m.connectBrowserSessionCmd(accountID, domain, cookieName, info.SourceBrowser)
		}
	}

	m.settings.browserPicker = browserPickerState{
		active:     true,
		accountID:  accountID,
		domain:     domain,
		cookieName: cookieName,
		loading:    true,
		status:     "looking for installed browsers...",
	}
	return m, m.loadAvailableBrowsersCmd(accountID)
}

func (m Model) supplementalBrowserSessionReady(accountID, providerID string) bool {
	if isBrowserSessionProvider(providerID) {
		return true
	}
	if strings.TrimSpace(m.accountProviders[accountID]) != "" {
		return true
	}
	return hasConfiguredAPIKeyEnv(providerID)
}

// handleBrowserPickerKey routes input while the cookie-source browser
// picker is active. We hijack normal modal input here so the user can't
// accidentally fall through to the row beneath the overlay.
func (m Model) handleBrowserPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	picker := &m.settings.browserPicker
	switch msg.String() {
	case "esc", "q":
		// Cancel the picker. Keystroke choices made up to this point are
		// thrown away — no read happens, so no keychain prompt either.
		m.settings.browserPicker = browserPickerState{}
		m.settings.apiKeyStatus = ""
		return m, nil
	case "up", "k":
		if picker.cursor > 0 {
			picker.cursor--
		}
		return m, nil
	case "down", "j":
		if picker.cursor < len(picker.browsers)-1 {
			picker.cursor++
		}
		return m, nil
	case "enter", " ":
		if picker.loading || len(picker.browsers) == 0 {
			return m, nil
		}
		choice := picker.browsers[clamp(picker.cursor, 0, len(picker.browsers)-1)]
		account := picker.accountID
		domain := picker.domain
		cookieName := picker.cookieName
		// Tear down the picker and kick off the actual read against the
		// chosen browser only. This is the path that triggers at most one
		// keychain prompt — the one the user explicitly asked for.
		m.settings.browserPicker = browserPickerState{}
		m.settings.apiKeyStatus = fmt.Sprintf("reading cookie from %s...", choice)
		return m, m.connectBrowserSessionCmd(account, domain, cookieName, choice)
	}
	return m, nil
}

func (m Model) handleProviderLinkPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	picker := &m.settings.providerLinkPicker
	switch msg.String() {
	case "esc", "q":
		*picker = providerLinkPickerState{}
		return m, nil
	case "up", "k":
		if picker.cursor > 0 {
			picker.cursor--
		}
		return m, nil
	case "down", "j":
		if picker.cursor < len(picker.choices)-1 {
			picker.cursor++
		}
		return m, nil
	case "enter", " ":
		if len(picker.choices) == 0 {
			return m, nil
		}
		target := picker.choices[clamp(picker.cursor, 0, len(picker.choices)-1)]
		source := picker.source
		picker.status = "saving link " + source + " → " + target + "..."
		return m, m.persistProviderLinkCmd(source, target)
	}
	return m, nil
}

func listWindow(total, cursor, visible int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if visible <= 0 || visible > total {
		visible = total
	}

	start := 0
	if cursor >= visible {
		start = cursor - visible + 1
	}
	end := start + visible
	if end > total {
		end = total
		start = end - visible
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

func loMove[T any](items []T, from, to int) []T {
	if from == to || from < 0 || from >= len(items) || to < 0 || to >= len(items) {
		return items
	}
	out := append([]T(nil), items...)
	item := out[from]
	if from < to {
		copy(out[from:to], out[from+1:to+1])
	} else {
		copy(out[to+1:from+1], out[to:from])
	}
	out[to] = item
	return out
}
