package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func (m Model) handleSettingsModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settings.apiKeyEditing {
		return m.handleAPIKeyEditKey(msg)
	}
	if m.settings.tab == settingsTabTelemetry && m.settings.providerLinkPicker.active {
		return m.handleProviderLinkPickerKey(msg)
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
		switch msg.String() {
		case "up", "k":
			if m.settings.cursor > 0 {
				m.settings.cursor--
			}
		case "down", "j":
			if m.settings.cursor < len(ids)-1 {
				m.settings.cursor++
			}
		case "K", "shift+k", "shift+up", "ctrl+up", "alt+up":
			cmd := m.moveSelectedProvider(ids, -1)
			if cmd != nil {
				return m, cmd
			}
		case "J", "shift+j", "shift+down", "ctrl+down", "alt+down":
			cmd := m.moveSelectedProvider(ids, 1)
			if cmd != nil {
				return m, cmd
			}
		case " ", "enter":
			if len(ids) == 0 {
				return m, nil
			}
			id := ids[clamp(m.settings.cursor, 0, len(ids)-1)]
			m.providerEnabled[id] = !m.isProviderEnabled(id)
			m.rebuildSortedIDs()
			m.settings.status = "saving settings..."
			return m, m.persistDashboardPrefsCmd()
		}
	case settingsTabWidgetSections:
		switch msg.String() {
		case "<", ">":
			// Switch sub-tab between tile (0) and detail (1) sections.
			if msg.String() == "<" {
				if m.settings.sectionSubTab > 0 {
					m.settings.sectionSubTab--
				}
			} else {
				if m.settings.sectionSubTab < 1 {
					m.settings.sectionSubTab++
				}
			}
			m.settings.sectionRowCursor = 0
			m.settings.previewOffset = 0
			return m, nil
		case "up", "k":
			if m.settings.sectionSubTab == 1 {
				if m.settings.sectionRowCursor > 0 {
					m.settings.sectionRowCursor--
				}
			} else {
				if m.settings.sectionRowCursor > 0 {
					m.settings.sectionRowCursor--
				}
			}
		case "down", "j":
			if m.settings.sectionSubTab == 1 {
				entries := m.detailWidgetSectionEntries()
				if m.settings.sectionRowCursor < len(entries)-1 {
					m.settings.sectionRowCursor++
				}
			} else {
				entries := m.widgetSectionEntries()
				if m.settings.sectionRowCursor < len(entries)-1 {
					m.settings.sectionRowCursor++
				}
			}
		case "K", "shift+k", "shift+up", "ctrl+up", "alt+up":
			if m.settings.sectionSubTab == 1 {
				cmd := m.moveSelectedDetailSection(-1)
				if cmd != nil {
					return m, cmd
				}
			} else {
				cmd := m.moveSelectedWidgetSection(-1)
				if cmd != nil {
					return m, cmd
				}
			}
		case "J", "shift+j", "shift+down", "ctrl+down", "alt+down":
			if m.settings.sectionSubTab == 1 {
				cmd := m.moveSelectedDetailSection(1)
				if cmd != nil {
					return m, cmd
				}
			} else {
				cmd := m.moveSelectedWidgetSection(1)
				if cmd != nil {
					return m, cmd
				}
			}
		case " ", "enter":
			if m.settings.sectionSubTab == 1 {
				cmd := m.toggleSelectedDetailSection()
				if cmd != nil {
					return m, cmd
				}
			} else {
				cmd := m.toggleSelectedWidgetSection()
				if cmd != nil {
					return m, cmd
				}
			}
		case "h", "H":
			m.hideSectionsWithNoData = !m.hideSectionsWithNoData
			m.invalidateTileBodyCache()
			m.settings.status = "saving empty-state..."
			return m, m.persistDashboardHideSectionsWithNoDataCmd()
		case "pgup", "ctrl+u":
			m.settings.previewOffset -= 4
			if m.settings.previewOffset < 0 {
				m.settings.previewOffset = 0
			}
		case "pgdown", "ctrl+d":
			m.settings.previewOffset += 4
		}
	case settingsTabTheme:
		themes := AvailableThemes()
		switch msg.String() {
		case "up", "k":
			if m.settings.themeCursor > 0 {
				m.settings.themeCursor--
			}
		case "down", "j":
			if m.settings.themeCursor < len(themes)-1 {
				m.settings.themeCursor++
			}
		case " ", "enter":
			if len(themes) == 0 {
				return m, nil
			}
			m.settings.themeCursor = clamp(m.settings.themeCursor, 0, len(themes)-1)
			name := themes[m.settings.themeCursor].Name
			if SetThemeByName(name) {
				m.invalidateRenderCaches()
				m.settings.status = "saving theme..."
				return m, m.persistThemeCmd(name)
			}
		}
	case settingsTabView:
		switch msg.String() {
		case "up", "k":
			if m.settings.viewCursor > 0 {
				m.settings.viewCursor--
			}
		case "down", "j":
			if m.settings.viewCursor < len(dashboardViewOptions)-1 {
				m.settings.viewCursor++
			}
		case " ", "enter":
			if len(dashboardViewOptions) == 0 {
				return m, nil
			}
			selected := dashboardViewByIndex(m.settings.viewCursor)
			m.setDashboardView(selected)
			m.settings.viewCursor = dashboardViewIndex(selected)
			m.settings.status = "saving view..."
			return m, m.persistDashboardViewCmd()
		}
	case settingsTabAPIKeys:
		switch msg.String() {
		case "up", "k":
			if m.settings.cursor > 0 {
				m.settings.cursor--
			}
		case "down", "j":
			if m.settings.cursor < len(ids)-1 {
				m.settings.cursor++
			}
		case "enter":
			if len(ids) == 0 {
				return m, nil
			}
			m.settings.cursor = clamp(m.settings.cursor, 0, len(ids)-1)
			id := ids[m.settings.cursor]
			providerID := providerForAccountID(id, m.accountProviders)
			// Browser-session-auth providers route to the cookie-extraction
			// flow instead of api-key editing. Enter triggers an immediate
			// read attempt against any logged-in browser; on failure the
			// status text guides the user to "press b to open the site".
			if isBrowserSessionProvider(providerID) {
				domain, cookieName, _ := browserCookieRefForProvider(providerID)
				m.settings.apiKeyStatus = "reading cookie from browser..."
				preferred := ""
				if info := m.services.LoadBrowserSessionInfo(id); info.Connected {
					preferred = info.SourceBrowser
				}
				return m, m.connectBrowserSessionCmd(id, domain, cookieName, preferred)
			}
			m.settings.apiKeyEditing = true
			m.settings.apiKeyEditAccountID = id
			m.settings.apiKeyInput = ""
			m.settings.apiKeyStatus = ""
			return m, nil
		case "b":
			// Open the provider's console URL in the user's default browser.
			// Only meaningful for browser-session-auth providers — but
			// harmless on api-key rows (no console URL = no-op).
			if len(ids) == 0 {
				return m, nil
			}
			m.settings.cursor = clamp(m.settings.cursor, 0, len(ids)-1)
			id := ids[m.settings.cursor]
			providerID := providerForAccountID(id, m.accountProviders)
			if !isBrowserSessionProvider(providerID) {
				return m, nil
			}
			_, _, consoleURL := browserCookieRefForProvider(providerID)
			if consoleURL == "" {
				return m, nil
			}
			m.settings.apiKeyStatus = "opening " + consoleURL + "…"
			return m, m.openProviderConsoleCmd(consoleURL)
		case "x":
			// Disconnect the stored browser session for the current row.
			// Distinct from "d" / "backspace" (api-key delete) because the
			// underlying credential store entry is in Sessions, not Keys.
			if len(ids) == 0 {
				return m, nil
			}
			m.settings.cursor = clamp(m.settings.cursor, 0, len(ids)-1)
			id := ids[m.settings.cursor]
			providerID := providerForAccountID(id, m.accountProviders)
			if !isBrowserSessionProvider(providerID) {
				return m, nil
			}
			m.settings.apiKeyStatus = "disconnecting..."
			return m, m.disconnectBrowserSessionCmd(id)
		case "d", "backspace":
			if len(ids) == 0 {
				return m, nil
			}
			m.settings.cursor = clamp(m.settings.cursor, 0, len(ids)-1)
			id := ids[m.settings.cursor]
			m.settings.apiKeyStatus = "deleting..."
			return m, m.deleteCredentialCmd(id)
		}
	case settingsTabTelemetry:
		rows := m.telemetryRows()
		switch msg.String() {
		case "up", "k":
			if m.settings.cursor > 0 {
				m.settings.cursor--
			}
		case "down", "j":
			if m.settings.cursor < len(rows)-1 {
				m.settings.cursor++
			}
		case "w":
			if next, cmd, handled := m.applyTimeWindowAtCursor(rows); handled {
				return next, cmd
			}
		case " ", "enter":
			if next, cmd, handled := m.activateTelemetryRow(rows); handled {
				return next, cmd
			}
		case "m":
			if next, cmd, handled := m.openProviderLinkPickerAtCursor(rows); handled {
				return next, cmd
			}
		case "x":
			if next, cmd, handled := m.clearProviderLinkAtCursor(rows); handled {
				return next, cmd
			}
		}
	case settingsTabIntegrations:
		switch msg.String() {
		case "up", "k":
			if m.settings.cursor > 0 {
				m.settings.cursor--
			}
		case "down", "j":
			if m.settings.cursor < len(m.settings.integrationStatus)-1 {
				m.settings.cursor++
			}
		case " ", "enter":
			if len(m.settings.integrationStatus) == 0 {
				return m, nil
			}
			selected := m.settings.integrationStatus[clamp(m.settings.cursor, 0, len(m.settings.integrationStatus)-1)]
			m.settings.status = "installing integration..."
			return m, m.installIntegrationCmd(selected.ID)
		}
	}

	return m, nil
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
