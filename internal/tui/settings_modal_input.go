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
		case "up", "k":
			if m.settings.sectionRowCursor > 0 {
				m.settings.sectionRowCursor--
			}
		case "down", "j":
			entries := m.widgetSectionEntries()
			if m.settings.sectionRowCursor < len(entries)-1 {
				m.settings.sectionRowCursor++
			}
		case "K", "shift+k", "shift+up", "ctrl+up", "alt+up":
			cmd := m.moveSelectedWidgetSection(-1)
			if cmd != nil {
				return m, cmd
			}
		case "J", "shift+j", "shift+down", "ctrl+down", "alt+down":
			cmd := m.moveSelectedWidgetSection(1)
			if cmd != nil {
				return m, cmd
			}
		case " ", "enter":
			cmd := m.toggleSelectedWidgetSection()
			if cmd != nil {
				return m, cmd
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
			m.settings.apiKeyEditing = true
			m.settings.apiKeyEditAccountID = id
			m.settings.apiKeyInput = ""
			m.settings.apiKeyStatus = ""
			return m, nil
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
		switch msg.String() {
		case "up", "k":
			if m.settings.cursor > 0 {
				m.settings.cursor--
			}
		case "down", "j":
			if m.settings.cursor < len(core.ValidTimeWindows)-1 {
				m.settings.cursor++
			}
		case " ", "enter", "w":
			tws := core.ValidTimeWindows
			if len(tws) == 0 {
				return m, nil
			}
			idx := clamp(m.settings.cursor, 0, len(tws)-1)
			selected := tws[idx]
			m.settings.cursor = idx
			m.settings.status = "saving time window..."
			m = m.beginTimeWindowRefresh(selected)
			return m, m.persistTimeWindowCmd(string(selected))
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

func (m *Model) resetSettingsCursorForTab() {
	switch m.settings.tab {
	case settingsTabProviders, settingsTabAPIKeys, settingsTabIntegrations, settingsTabTelemetry:
		m.settings.cursor = 0
	case settingsTabWidgetSections:
		m.settings.sectionRowCursor = 0
		m.settings.previewOffset = 0
	case settingsTabTheme:
		m.settings.themeCursor = clamp(ActiveThemeIndex(), 0, max(0, len(AvailableThemes())-1))
	case settingsTabView:
		m.settings.viewCursor = dashboardViewIndex(m.configuredDashboardView())
	}
}

func (m Model) currentTimeWindowIndex() int {
	for i, tw := range core.ValidTimeWindows {
		if tw == m.timeWindow {
			return i
		}
	}
	return 0
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
		providerID := m.accountProviders[id]
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
