package tui

import (
	"fmt"
)

type settingsModalTab int

const (
	settingsTabProviders settingsModalTab = iota
	settingsTabWidgetSections
	settingsTabTheme
	settingsTabView
	settingsTabAPIKeys
	settingsTabTelemetry
	settingsTabIntegrations
	settingsTabCount
)

const (
	settingsWidgetPreviewProviderID = "claude_code"
	settingsWidgetPreviewMinBodyH   = 12
)

var settingsTabNames = []string{
	"Providers",
	"Widget Sections",
	"Theme",
	"View",
	"API Keys",
	"Telemetry",
	"Integrations",
}

func (m *Model) openSettingsModal() {
	m.settings.show = true
	m.settings.status = ""
	m.settings.tab = settingsTabProviders
	m.settings.apiKeyEditing = false
	m.settings.apiKeyInput = ""
	m.settings.apiKeyStatus = ""
	m.settings.bodyOffset = 0
	if len(m.providerOrder) > 0 {
		m.settings.cursor = clamp(m.settings.cursor, 0, len(m.providerOrder)-1)
	}
	m.settings.sectionRowCursor = 0
	m.settings.previewOffset = 0
	themes := AvailableThemes()
	if len(themes) > 0 {
		m.settings.themeCursor = clamp(ActiveThemeIndex(), 0, len(themes)-1)
	} else {
		m.settings.themeCursor = 0
	}
	m.settings.viewCursor = dashboardViewIndex(m.configuredDashboardView())
	m.refreshIntegrationStatuses()
}

func (m *Model) closeSettingsModal() {
	m.settings.show = false
	m.settings.status = ""
	m.settings.apiKeyEditing = false
	m.settings.apiKeyInput = ""
	m.settings.apiKeyStatus = ""
	m.settings.bodyOffset = 0
	m.settings.sectionRowCursor = 0
	m.settings.previewOffset = 0
}

func (m Model) settingsModalInfo() string {
	ids := m.settingsIDs()
	active := 0
	for _, id := range ids {
		if m.isProviderEnabled(id) {
			active++
		}
	}

	tabName := "Settings"
	if int(m.settings.tab) >= 0 && int(m.settings.tab) < len(settingsTabNames) {
		tabName = settingsTabNames[m.settings.tab]
	}

	info := fmt.Sprintf("⚙ %s · %d/%d active", tabName, active, len(ids))
	if m.settings.status != "" {
		info += " · " + m.settings.status
	}
	return info
}
