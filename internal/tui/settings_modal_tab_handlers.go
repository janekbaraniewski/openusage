package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Per-tab handlers extracted from handleSettingsModalKey, which used to be a
// single 348-line function with seven nested switch blocks. Each handler
// returns (model, cmd, handled). When handled is false the dispatcher falls
// through to its default no-op return so the modal still consumes the key.

func (m Model) handleSettingsTabProvidersKey(msg tea.KeyMsg, ids []string) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if m.settings.cursor > 0 {
			m.settings.cursor--
		}
		return m, nil, true
	case "down", "j":
		if m.settings.cursor < len(ids)-1 {
			m.settings.cursor++
		}
		return m, nil, true
	case "K", "shift+k", "shift+up", "ctrl+up", "alt+up":
		if cmd := m.moveSelectedProvider(ids, -1); cmd != nil {
			return m, cmd, true
		}
		return m, nil, true
	case "J", "shift+j", "shift+down", "ctrl+down", "alt+down":
		if cmd := m.moveSelectedProvider(ids, 1); cmd != nil {
			return m, cmd, true
		}
		return m, nil, true
	case " ", "enter":
		if len(ids) == 0 {
			return m, nil, true
		}
		id := ids[clamp(m.settings.cursor, 0, len(ids)-1)]
		m.providerEnabled[id] = !m.isProviderEnabled(id)
		m.rebuildSortedIDs()
		m.settings.status = "saving settings..."
		return m, m.persistDashboardPrefsCmd(), true
	case "l":
		if len(ids) == 0 {
			return m, nil, false
		}
		id := ids[clamp(m.settings.cursor, 0, len(ids)-1)]
		if m.accountProviders[id] != "codex" {
			return m, nil, false
		}
		m.settings.creditLimitEditing = true
		m.settings.creditLimitEditAccountID = id
		m.settings.creditLimitInput = ""
		if limit := m.accountCreditLimits[id]; limit != nil {
			m.settings.creditLimitInput = fmt.Sprintf("%g", *limit)
		}
		m.settings.status = "enter a credit cap; empty clears"
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) handleSettingsTabWidgetSectionsKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "<", ">":
		// Switch sub-tab between tile (0) and detail (1) sections.
		if msg.String() == "<" {
			if m.settings.sectionSubTab > 0 {
				m.settings.sectionSubTab--
			}
		} else if m.settings.sectionSubTab < 1 {
			m.settings.sectionSubTab++
		}
		m.settings.sectionRowCursor = 0
		m.settings.previewOffset = 0
		return m, nil, true
	case "up", "k":
		if m.settings.sectionRowCursor > 0 {
			m.settings.sectionRowCursor--
		}
		return m, nil, true
	case "down", "j":
		entries := m.activeSectionEntryCount()
		if m.settings.sectionRowCursor < entries-1 {
			m.settings.sectionRowCursor++
		}
		return m, nil, true
	case "K", "shift+k", "shift+up", "ctrl+up", "alt+up":
		if cmd := m.moveSelectedActiveSection(-1); cmd != nil {
			return m, cmd, true
		}
		return m, nil, true
	case "J", "shift+j", "shift+down", "ctrl+down", "alt+down":
		if cmd := m.moveSelectedActiveSection(1); cmd != nil {
			return m, cmd, true
		}
		return m, nil, true
	case " ", "enter":
		if cmd := m.toggleSelectedActiveSection(); cmd != nil {
			return m, cmd, true
		}
		return m, nil, true
	case "h", "H":
		m.hideSectionsWithNoData = !m.hideSectionsWithNoData
		m.invalidateTileBodyCache()
		m.settings.status = "saving empty-state..."
		return m, m.persistDashboardHideSectionsWithNoDataCmd(), true
	case "pgup", "ctrl+u":
		m.settings.previewOffset -= 4
		if m.settings.previewOffset < 0 {
			m.settings.previewOffset = 0
		}
		return m, nil, true
	case "pgdown", "ctrl+d":
		m.settings.previewOffset += 4
		return m, nil, true
	}
	return m, nil, false
}

// activeSectionEntryCount returns how many entries the currently selected
// widget-sections sub-tab (tile or detail) has.
func (m Model) activeSectionEntryCount() int {
	if m.settings.sectionSubTab == 1 {
		return len(m.detailWidgetSectionEntries())
	}
	return len(m.widgetSectionEntries())
}

func (m *Model) moveSelectedActiveSection(delta int) tea.Cmd {
	if m.settings.sectionSubTab == 1 {
		return m.moveSelectedDetailSection(delta)
	}
	return m.moveSelectedWidgetSection(delta)
}

func (m *Model) toggleSelectedActiveSection() tea.Cmd {
	if m.settings.sectionSubTab == 1 {
		return m.toggleSelectedDetailSection()
	}
	return m.toggleSelectedWidgetSection()
}

func (m Model) handleSettingsTabThemeKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	themes := AvailableThemes()
	switch msg.String() {
	case "up", "k":
		if m.settings.themeCursor > 0 {
			m.settings.themeCursor--
		}
		return m, nil, true
	case "down", "j":
		if m.settings.themeCursor < len(themes)-1 {
			m.settings.themeCursor++
		}
		return m, nil, true
	case " ", "enter":
		if len(themes) == 0 {
			return m, nil, true
		}
		m.settings.themeCursor = clamp(m.settings.themeCursor, 0, len(themes)-1)
		name := themes[m.settings.themeCursor].Name
		if SetThemeByName(name) {
			m.invalidateRenderCaches()
			m.settings.status = "saving theme..."
			return m, m.persistThemeCmd(name), true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) handleSettingsTabViewKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if m.settings.viewCursor > 0 {
			m.settings.viewCursor--
		}
		return m, nil, true
	case "down", "j":
		if m.settings.viewCursor < len(dashboardViewOptions)-1 {
			m.settings.viewCursor++
		}
		return m, nil, true
	case " ", "enter":
		if len(dashboardViewOptions) == 0 {
			return m, nil, true
		}
		selected := dashboardViewByIndex(m.settings.viewCursor)
		m.setDashboardView(selected)
		m.settings.viewCursor = dashboardViewIndex(selected)
		m.settings.status = "saving view..."
		return m, m.persistDashboardViewCmd(), true
	}
	return m, nil, false
}

func (m Model) handleSettingsTabAPIKeysKey(msg tea.KeyMsg, ids []string) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if m.settings.cursor > 0 {
			m.settings.cursor--
		}
		return m, nil, true
	case "down", "j":
		if m.settings.cursor < len(ids)-1 {
			m.settings.cursor++
		}
		return m, nil, true
	case "enter":
		if len(ids) == 0 {
			return m, nil, true
		}
		m.settings.cursor = clamp(m.settings.cursor, 0, len(ids)-1)
		id := ids[m.settings.cursor]
		providerID := providerForAccountID(id, m.accountProviders)
		if isBrowserSessionProvider(providerID) {
			next, cmd := m.startBrowserSessionConnect(id, providerID)
			return next.(Model), cmd, true
		}
		m.settings.apiKeyEditing = true
		m.settings.apiKeyEditAccountID = id
		m.settings.apiKeyInput = ""
		m.settings.apiKeyStatus = ""
		return m, nil, true
	case "c":
		if len(ids) == 0 {
			return m, nil, true
		}
		m.settings.cursor = clamp(m.settings.cursor, 0, len(ids)-1)
		id := ids[m.settings.cursor]
		providerID := providerForAccountID(id, m.accountProviders)
		if !supportsBrowserSessionProvider(providerID) {
			return m, nil, true
		}
		next, cmd := m.startBrowserSessionConnect(id, providerID)
		return next.(Model), cmd, true
	case "b":
		// Open the provider's console URL in the user's default browser.
		// Only meaningful for browser-session-auth providers — but
		// harmless on api-key rows (no console URL = no-op).
		if len(ids) == 0 {
			return m, nil, true
		}
		m.settings.cursor = clamp(m.settings.cursor, 0, len(ids)-1)
		id := ids[m.settings.cursor]
		providerID := providerForAccountID(id, m.accountProviders)
		if !supportsBrowserSessionProvider(providerID) {
			return m, nil, true
		}
		_, _, consoleURL := browserCookieRefForProvider(providerID)
		if consoleURL == "" {
			return m, nil, true
		}
		m.settings.apiKeyStatus = "opening " + consoleURL + "…"
		return m, m.openProviderConsoleCmd(consoleURL), true
	case "x":
		// Disconnect the stored browser session for the current row.
		// Distinct from "d" / "backspace" (api-key delete) because the
		// underlying credential store entry is in Sessions, not Keys.
		if len(ids) == 0 {
			return m, nil, true
		}
		m.settings.cursor = clamp(m.settings.cursor, 0, len(ids)-1)
		id := ids[m.settings.cursor]
		providerID := providerForAccountID(id, m.accountProviders)
		if !supportsBrowserSessionProvider(providerID) {
			return m, nil, true
		}
		m.settings.apiKeyStatus = "disconnecting..."
		return m, m.disconnectBrowserSessionCmd(id), true
	case "d", "backspace":
		if len(ids) == 0 {
			return m, nil, true
		}
		m.settings.cursor = clamp(m.settings.cursor, 0, len(ids)-1)
		id := ids[m.settings.cursor]
		providerID := providerForAccountID(id, m.accountProviders)
		if !isAPIKeyProvider(providerID) {
			return m, nil, true
		}
		m.settings.apiKeyStatus = "deleting..."
		return m, m.deleteCredentialCmd(id), true
	}
	return m, nil, false
}

func (m Model) handleSettingsTabTelemetryKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	rows := m.telemetryRows()
	switch msg.String() {
	case "up", "k":
		if m.settings.cursor > 0 {
			m.settings.cursor--
		}
		return m, nil, true
	case "down", "j":
		if m.settings.cursor < len(rows)-1 {
			m.settings.cursor++
		}
		return m, nil, true
	case "w":
		if next, cmd, handled := m.applyTimeWindowAtCursor(rows); handled {
			return next, cmd, true
		}
		return m, nil, true
	case " ", "enter":
		if next, cmd, handled := m.activateTelemetryRow(rows); handled {
			return next, cmd, true
		}
		return m, nil, true
	case "m":
		if next, cmd, handled := m.openProviderLinkPickerAtCursor(rows); handled {
			return next, cmd, true
		}
		return m, nil, true
	case "x":
		if next, cmd, handled := m.clearProviderLinkAtCursor(rows); handled {
			return next, cmd, true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) handleSettingsTabIntegrationsKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if m.settings.cursor > 0 {
			m.settings.cursor--
		}
		return m, nil, true
	case "down", "j":
		if m.settings.cursor < len(m.settings.integrationStatus)-1 {
			m.settings.cursor++
		}
		return m, nil, true
	case " ", "enter":
		if len(m.settings.integrationStatus) == 0 {
			return m, nil, true
		}
		selected := m.settings.integrationStatus[clamp(m.settings.cursor, 0, len(m.settings.integrationStatus)-1)]
		m.settings.status = "installing integration..."
		return m, m.installIntegrationCmd(selected.ID), true
	}
	return m, nil, false
}
