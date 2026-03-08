package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
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
		case " ", "enter":
			if len(ids) == 0 {
				return m, nil
			}
			id := ids[clamp(m.settings.cursor, 0, len(ids)-1)]
			providerID := providerForAccountID(id, m.accountProviders)
			if isAPIKeyProvider(providerID) {
				m.settings.apiKeyEditing = true
				m.settings.apiKeyInput = ""
				m.settings.apiKeyEditAccountID = id
				m.settings.apiKeyStatus = ""
				// Ensure the provider mapping exists (for unregistered providers)
				m.accountProviders[id] = providerID
			}
		case "d":
			if len(ids) == 0 {
				return m, nil
			}
			id := ids[clamp(m.settings.cursor, 0, len(ids)-1)]
			providerID := providerForAccountID(id, m.accountProviders)
			if isAPIKeyProvider(providerID) {
				m.settings.status = "deleting key..."
				return m, m.deleteCredentialCmd(id)
			}
		}
	case settingsTabTelemetry:
		twCount := len(core.ValidTimeWindows)
		switch msg.String() {
		case "up", "k":
			if m.settings.cursor > 0 {
				m.settings.cursor--
			}
		case "down", "j":
			if m.settings.cursor < twCount-1 {
				m.settings.cursor++
			}
		case " ", "enter":
			if m.settings.cursor >= 0 && m.settings.cursor < twCount {
				tw := core.ValidTimeWindows[m.settings.cursor]
				m.timeWindow = tw
				if m.onTimeWindowChange != nil {
					m.onTimeWindowChange(string(tw))
				}
				m.refreshing = true
				if m.onRefresh != nil {
					m.onRefresh()
				}
				m.settings.status = "saving time window..."
				return m, m.persistTimeWindowCmd(string(tw))
			}
		case "pgup", "ctrl+u":
			m.settings.bodyOffset -= 4
			if m.settings.bodyOffset < 0 {
				m.settings.bodyOffset = 0
			}
		case "pgdown", "ctrl+d":
			m.settings.bodyOffset += 4
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
		case "i", " ", "enter":
			if len(m.settings.integrationStatus) == 0 {
				return m, nil
			}
			cursor := clamp(m.settings.cursor, 0, len(m.settings.integrationStatus)-1)
			entry := m.settings.integrationStatus[cursor]
			m.settings.status = "installing integration..."
			return m, m.installIntegrationCmd(entry.ID)
		case "u":
			if len(m.settings.integrationStatus) == 0 {
				return m, nil
			}
			cursor := clamp(m.settings.cursor, 0, len(m.settings.integrationStatus)-1)
			entry := m.settings.integrationStatus[cursor]
			if !entry.NeedsUpgrade {
				m.settings.status = "selected integration is already current"
				return m, nil
			}
			m.settings.status = "upgrading integration..."
			return m, m.installIntegrationCmd(entry.ID)
		}
	}

	return m, nil
}

func (m *Model) moveSelectedProvider(ids []string, delta int) tea.Cmd {
	if m == nil || len(ids) == 0 || delta == 0 {
		return nil
	}
	cursor := clamp(m.settings.cursor, 0, len(ids)-1)
	target := cursor + delta
	if target < 0 || target >= len(ids) {
		return nil
	}

	id := ids[cursor]
	swapID := ids[target]
	currIdx := m.providerOrderIndex(id)
	swapIdx := m.providerOrderIndex(swapID)
	if currIdx < 0 || swapIdx < 0 {
		return nil
	}

	m.providerOrder[currIdx], m.providerOrder[swapIdx] = m.providerOrder[swapIdx], m.providerOrder[currIdx]
	m.settings.cursor = target
	m.rebuildSortedIDs()
	m.settings.status = "saving order..."
	return m.persistDashboardPrefsCmd()
}

func (m *Model) moveSelectedWidgetSection(delta int) tea.Cmd {
	if m == nil || delta == 0 {
		return nil
	}
	entries := m.widgetSectionEntries()
	if len(entries) == 0 {
		return nil
	}

	cursor := clamp(m.settings.sectionRowCursor, 0, len(entries)-1)
	target := cursor + delta
	if target < 0 || target >= len(entries) {
		return nil
	}
	entries[cursor], entries[target] = entries[target], entries[cursor]
	m.settings.sectionRowCursor = target
	m.setWidgetSectionEntries(entries)
	m.settings.status = "saving sections..."
	return m.persistDashboardWidgetSectionsCmd()
}

func (m *Model) toggleSelectedWidgetSection() tea.Cmd {
	if m == nil {
		return nil
	}
	entries := m.widgetSectionEntries()
	if len(entries) == 0 {
		return nil
	}
	cursor := clamp(m.settings.sectionRowCursor, 0, len(entries)-1)
	entries[cursor].Enabled = !entries[cursor].Enabled
	m.setWidgetSectionEntries(entries)
	m.settings.status = "saving sections..."
	return m.persistDashboardWidgetSectionsCmd()
}

func (m *Model) resetSettingsCursorForTab() {
	switch m.settings.tab {
	case settingsTabTelemetry:
		m.settings.cursor = m.currentTimeWindowIndex()
	case settingsTabView:
		m.settings.viewCursor = dashboardViewIndex(m.configuredDashboardView())
	case settingsTabWidgetSections:
		m.settings.sectionRowCursor = 0
		m.settings.previewOffset = 0
	default:
		m.settings.cursor = 0
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

func (m Model) renderSettingsModalOverlay() string {
	if m.width < 40 || m.height < 12 {
		return m.renderDashboard()
	}

	contentW := m.width - 24
	if contentW < 68 {
		contentW = 68
	}
	if contentW > 92 {
		contentW = 92
	}
	panelInnerW := contentW - 4
	if panelInnerW < 40 {
		panelInnerW = 40
	}

	const modalBodyHeight = 20
	contentH := modalBodyHeight
	maxAllowed := m.height - 14
	if maxAllowed < 8 {
		maxAllowed = 8
	}
	if contentH > maxAllowed {
		contentH = maxAllowed
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(colorRosewater).Render("Settings")
	tabs := m.renderSettingsModalTabs(panelInnerW)
	body := m.renderSettingsModalBody(panelInnerW, contentH)
	hint := dimStyle.Render(m.settingsModalHint())

	status := ""
	if m.settings.status != "" {
		status = lipgloss.NewStyle().Foreground(colorSapphire).Render(m.settings.status)
	}

	lines := []string{
		title,
		tabs,
		lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", panelInnerW)),
		body,
		lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", panelInnerW)),
		hint,
	}
	if status != "" {
		lines = append(lines, status)
	}

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Background(colorBase).
		Padding(1, 2).
		Width(contentW).
		Render(strings.Join(lines, "\n"))
	if m.settings.tab != settingsTabWidgetSections {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
	}

	previewBodyH := contentH
	sideBySide := m.width >= contentW*2+12
	previewBodyH = m.settingsWidgetPreviewBodyHeight(contentW, contentH, sideBySide)
	previewPanel := m.renderSettingsWidgetPreviewPanel(contentW, previewBodyH)

	combined := ""
	// Render side-by-side when terminal width allows two panels comfortably.
	if sideBySide {
		panelH := lipgloss.Height(panel)
		previewH := lipgloss.Height(previewPanel)
		if panelH < previewH {
			panel = centerPanelVertically(panel, previewH)
		} else if previewH < panelH {
			previewPanel = centerPanelVertically(previewPanel, panelH)
		}
		combined = lipgloss.JoinHorizontal(lipgloss.Top, panel, "  ", previewPanel)
	} else {
		combined = lipgloss.JoinVertical(lipgloss.Left, panel, "", previewPanel)
	}

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, combined)
}

func (m Model) renderSettingsModalTabs(w int) string {
	if len(settingsTabNames) == 0 {
		return ""
	}
	if w < 40 {
		w = 40
	}

	n := len(settingsTabNames)
	gap := 1
	cellW := (w - gap*(n-1)) / n
	if cellW < 6 {
		cellW = 6
		gap = 0
		cellW = w / n
	}

	tabTokens := []string{"PROV", "SECT", "THEME", "VIEW", "KEYS", "TELEM", "INTEG"}
	if len(tabTokens) < n {
		tabTokens = append(tabTokens, settingsTabNames[len(tabTokens):]...)
	}

	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(colorMantle).Background(colorAccent)
	inactiveStyle := lipgloss.NewStyle().Foreground(colorSubtext)

	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		token := settingsTabNames[i]
		if i < len(tabTokens) {
			token = tabTokens[i]
		}
		label := fmt.Sprintf("%d %s", i+1, token)
		if lipgloss.Width(label) > cellW {
			label = truncateToWidth(label, cellW)
		}
		if pad := cellW - lipgloss.Width(label); pad > 0 {
			left := pad / 2
			right := pad - left
			label = strings.Repeat(" ", left) + label + strings.Repeat(" ", right)
		}
		if settingsModalTab(i) == m.settings.tab {
			parts = append(parts, activeStyle.Render(label))
		} else {
			parts = append(parts, inactiveStyle.Render(label))
		}
	}

	line := strings.Join(parts, strings.Repeat(" ", gap))
	return line
}

func (m Model) settingsModalHint() string {
	switch m.settings.tab {
	case settingsTabProviders:
		return "Up/Down: select  ·  Shift+↑/↓ or Shift+J/K: move item  ·  Space/Enter: enable/disable  ·  Left/Right: switch tab  ·  Esc: close"
	case settingsTabWidgetSections:
		return "Up/Down: select section  ·  Shift+↑/↓ or Shift+J/K: reorder  ·  Space/Enter: show/hide  ·  h: toggle hide empty sections  ·  PgUp/PgDn or Ctrl+U/D: scroll preview  ·  Esc: close"
	case settingsTabAPIKeys:
		if m.settings.apiKeyEditing {
			return "Type API key  ·  Enter: validate & save  ·  Esc: cancel"
		}
		return "Up/Down: select  ·  Enter: edit key  ·  d: delete key  ·  Left/Right: switch tab  ·  Esc: close"
	case settingsTabView:
		return "Up/Down: select view  ·  Space/Enter: apply  ·  v/Shift+V: cycle outside settings  ·  Esc: close"
	case settingsTabTelemetry:
		return "Up/Down: select  ·  Space/Enter: apply time window  ·  Left/Right: switch tab  ·  Esc: close"
	case settingsTabIntegrations:
		return "Up/Down: select  ·  Enter/i: install/configure  ·  u: upgrade  ·  r: refresh  ·  Esc: close"
	default:
		return "Up/Down: select theme  ·  Space/Enter: apply theme  ·  Left/Right: switch tab  ·  Esc: close"
	}
}

func (m Model) renderSettingsModalBody(w, h int) string {
	switch m.settings.tab {
	case settingsTabProviders:
		return m.renderSettingsProvidersBody(w, h)
	case settingsTabWidgetSections:
		return m.renderSettingsWidgetSectionsBody(w, h)
	case settingsTabAPIKeys:
		return m.renderSettingsAPIKeysBody(w, h)
	case settingsTabView:
		return m.renderSettingsViewBody(w, h)
	case settingsTabTelemetry:
		return m.renderSettingsTelemetryBody(w, h)
	case settingsTabIntegrations:
		return m.renderSettingsIntegrationsBody(w, h)
	default:
		return m.renderSettingsThemeBody(w, h)
	}
}

func settingsBodyHeaderLines(title, subtitle string) []string {
	lines := []string{
		lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(title),
	}
	if strings.TrimSpace(subtitle) != "" {
		lines = append(lines, dimStyle.Render(subtitle))
	}
	lines = append(lines, "")
	return lines
}

func settingsBodyRule(w int) string {
	if w < 8 {
		w = 8
	}
	return dimStyle.Render(strings.Repeat("─", w-2))
}

func settingsSectionLabel(id core.DashboardStandardSection) string {
	switch id {
	case core.DashboardSectionTopUsageProgress:
		return "Top Usage Progress"
	case core.DashboardSectionModelBurn:
		return "Model Burn"
	case core.DashboardSectionClientBurn:
		return "Client Burn"
	case core.DashboardSectionProjectBreakdown:
		return "Project Breakdown"
	case core.DashboardSectionToolUsage:
		return "Tool Usage"
	case core.DashboardSectionMCPUsage:
		return "MCP Usage"
	case core.DashboardSectionLanguageBurn:
		return "Language"
	case core.DashboardSectionCodeStats:
		return "Code Statistics"
	case core.DashboardSectionDailyUsage:
		return "Daily Usage"
	case core.DashboardSectionProviderBurn:
		return "Provider Burn"
	case core.DashboardSectionUpstreamProviders:
		return "Upstream Providers"
	case core.DashboardSectionOtherData:
		return "Other Data"
	default:
		raw := strings.TrimSpace(strings.ReplaceAll(string(id), "_", " "))
		if raw == "" {
			return "Unknown"
		}
		parts := strings.Fields(raw)
		for i := range parts {
			parts[i] = titleCase(parts[i])
		}
		return strings.Join(parts, " ")
	}
}

func (m Model) renderSettingsProvidersBody(w, h int) string {
	ids := m.settingsIDs()

	enabledCount := 0
	for _, id := range ids {
		if m.isProviderEnabled(id) {
			enabledCount++
		}
	}

	lines := settingsBodyHeaderLines(
		"Provider Visibility & Order",
		fmt.Sprintf("%d/%d enabled · Shift+J/K reorder · Enter toggle", enabledCount, len(ids)),
	)
	accountW := 26
	providerW := w - accountW - 16
	if providerW < 10 {
		providerW = 10
		accountW = w - providerW - 16
	}
	if accountW < 12 {
		accountW = 12
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf("    %-3s %-3s %-*s %-*s", "#", "ON", accountW, "ACCOUNT", providerW, "PROVIDER")))
	lines = append(lines, settingsBodyRule(w))
	if len(ids) == 0 {
		lines = append(lines, dimStyle.Render("No providers available."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.cursor, 0, len(ids)-1)
	listHeight := h - len(lines)
	if listHeight < 1 {
		listHeight = 1
	}
	start, end := listWindow(len(ids), cursor, listHeight)

	for i := start; i < end; i++ {
		id := ids[i]
		providerID := m.accountProviders[id]
		if snap, ok := m.snapshots[id]; ok && snap.ProviderID != "" {
			providerID = snap.ProviderID
		}
		if providerID == "" {
			providerID = "unknown"
		}

		onText := "OFF"
		onStyle := lipgloss.NewStyle().Foreground(colorRed)
		if m.isProviderEnabled(id) {
			onText = "ON "
			onStyle = lipgloss.NewStyle().Foreground(colorGreen)
		}

		prefix := "  "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("➤ ")
		}
		account := truncateToWidth(id, accountW)
		provider := truncateToWidth(providerID, providerW)
		line := fmt.Sprintf("%s%-3d %s %-*s %-*s", prefix, i+1, onStyle.Render(onText), accountW, account, providerW, provider)
		lines = append(lines, line)
	}

	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) renderSettingsWidgetSectionsBody(w, h int) string {
	return m.renderSettingsWidgetSectionsList(w, h)
}

func (m Model) renderSettingsWidgetSectionsList(w, h int) string {
	entries := m.widgetSectionEntries()

	visibleCount := 0
	for _, entry := range entries {
		if entry.Enabled {
			visibleCount++
		}
	}

	lines := settingsBodyHeaderLines(
		"Global Widget Sections",
		fmt.Sprintf("%d/%d sections visible · applies to all providers", visibleCount, len(entries)),
	)
	hideBox := "☐"
	hideBoxStyle := lipgloss.NewStyle().Foreground(colorRed)
	if m.hideSectionsWithNoData {
		hideBox = "☑"
		hideBoxStyle = lipgloss.NewStyle().Foreground(colorGreen)
	}
	lines = append(lines, fmt.Sprintf("Hide sections with no data: %s  %s", hideBoxStyle.Render(hideBox), dimStyle.Render("press h to toggle")))
	lines = append(lines, "")
	nameW := w - 24
	if nameW < 12 {
		nameW = 12
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf("    %-3s %-3s %-*s %s", "#", "ON", nameW, "SECTION", "ID")))
	lines = append(lines, settingsBodyRule(w))
	if len(entries) == 0 {
		lines = append(lines, dimStyle.Render("No dashboard sections available."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.sectionRowCursor, 0, len(entries)-1)
	listHeight := h - len(lines)
	if listHeight < 1 {
		listHeight = 1
	}
	start, end := listWindow(len(entries), cursor, listHeight)

	for i := start; i < end; i++ {
		entry := entries[i]
		prefix := "  "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("➤ ")
		}

		onText := "OFF"
		onStyle := lipgloss.NewStyle().Foreground(colorRed)
		if entry.Enabled {
			onText = "ON "
			onStyle = lipgloss.NewStyle().Foreground(colorGreen)
		}

		name := settingsSectionLabel(entry.ID)
		name = truncateToWidth(name, nameW)
		line := fmt.Sprintf("%s%-3d %s %-*s %s", prefix, i+1, onStyle.Render(onText), nameW, name, dimStyle.Render(string(entry.ID)))
		lines = append(lines, line)
	}

	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) renderSettingsWidgetSectionsPreview(w, h int) string {
	if w < 24 || h < 5 {
		return padToSize(dimStyle.Render("Live preview unavailable at this size."), w, h)
	}

	title := lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render("Live Preview")
	hint := dimStyle.Render("Claude Code preset · synthetic data · PgUp/PgDn scroll")
	lines := []string{title, hint, ""}

	tileW := w
	if tileW > 2 {
		tileW -= 2
	}
	if tileW < tileMinWidth {
		tileW = tileMinWidth
	}

	// Render full tile content to avoid nested-scroll artifacts inside the preview panel.
	previewTile := m.renderTile(settingsWidgetSectionsPreviewSnapshot(), false, false, tileW, 0, 0)
	all := append(lines, strings.Split(previewTile, "\n")...)
	maxOffset := len(all) - h
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := clamp(m.settings.previewOffset, 0, maxOffset)
	visible := all
	if len(visible) > h {
		visible = visible[offset:]
		if len(visible) > h {
			visible = visible[:h]
		}
	}
	if len(visible) > 0 && offset > 0 {
		visible[0] = dimStyle.Render("  ▲ preview above")
	}
	if len(visible) > 0 && offset+h < len(all) {
		visible[len(visible)-1] = dimStyle.Render("  ▼ preview below")
	}
	return padToSize(strings.Join(visible, "\n"), w, h)
}

func (m Model) renderSettingsWidgetPreviewPanel(contentW, contentH int) string {
	innerW := contentW - 4
	if innerW < 24 {
		innerW = contentW
	}
	bodyH := contentH - 1
	if bodyH < 4 {
		bodyH = 4
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(colorRosewater).Render("Widget Preview")
	body := m.renderSettingsWidgetSectionsPreview(innerW, bodyH)
	lines := []string{
		title,
		lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", innerW)),
		body,
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Background(colorBase).
		Padding(1, 2, 0, 2).
		Width(contentW).
		Render(strings.Join(lines, "\n"))
}

func (m Model) settingsWidgetPreviewBodyHeight(contentW, contentH int, sideBySide bool) int {
	minBodyH := settingsWidgetPreviewMinBodyH
	maxBodyH := contentH
	if sideBySide {
		// Keep breathing room around the combined modal while allowing growth.
		maxBodyH = m.height - 12
	} else {
		// Stacked layout should stay balanced and avoid dominating the viewport.
		maxBodyH = (m.height - 12) / 2
	}
	if maxBodyH < minBodyH {
		maxBodyH = minBodyH
	}

	innerW := contentW - 4
	if innerW < 24 {
		innerW = 24
	}
	targetBodyH := m.settingsWidgetPreviewContentLineCount(innerW)
	if targetBodyH < minBodyH {
		targetBodyH = minBodyH
	}
	if targetBodyH > maxBodyH {
		targetBodyH = maxBodyH
	}

	// renderSettingsWidgetPreviewPanel reserves one line for panel internals.
	return targetBodyH + 1
}

func (m Model) settingsWidgetPreviewContentLineCount(innerW int) int {
	if innerW < 24 {
		return 4
	}
	tileW := innerW
	if tileW > 2 {
		tileW -= 2
	}
	if tileW < tileMinWidth {
		tileW = tileMinWidth
	}
	previewTile := m.renderTile(settingsWidgetSectionsPreviewSnapshot(), false, false, tileW, 0, 0)
	// Includes preview title line, hint line, and spacing line.
	return 3 + len(strings.Split(previewTile, "\n"))
}

func centerPanelVertically(panel string, targetHeight int) string {
	current := lipgloss.Height(panel)
	if current >= targetHeight {
		return panel
	}
	diff := targetHeight - current
	top := diff / 2
	bottom := diff - top
	return strings.Repeat("\n", top) + panel + strings.Repeat("\n", bottom)
}

func settingsWidgetSectionsPreviewSnapshot() core.UsageSnapshot {
	usedMetric := func(used float64, unit, window string) core.Metric {
		return core.Metric{
			Used:   &used,
			Unit:   unit,
			Window: window,
		}
	}
	limitMetric := func(limit, used float64, unit, window string) core.Metric {
		remaining := limit - used
		return core.Metric{
			Limit:     &limit,
			Used:      &used,
			Remaining: &remaining,
			Unit:      unit,
			Window:    window,
		}
	}

	snap := core.NewUsageSnapshot(settingsWidgetPreviewProviderID, "claude-preview")
	snap.Status = core.StatusOK
	snap.Message = "Settings preview"
	snap.Attributes = map[string]string{
		"telemetry_view": "canonical",
	}
	snap.Metrics = map[string]core.Metric{
		"usage_five_hour":                       limitMetric(200, 62, "requests", "5h"),
		"usage_seven_day":                       limitMetric(5000, 1730, "requests", "7d"),
		"today_api_cost":                        usedMetric(5.20, "USD", "1d"),
		"7d_api_cost":                           usedMetric(28.40, "USD", "7d"),
		"all_time_api_cost":                     usedMetric(412.30, "USD", "all"),
		"messages_today":                        usedMetric(37, "requests", "1d"),
		"sessions_today":                        usedMetric(6, "sessions", "1d"),
		"tool_calls_today":                      usedMetric(52, "requests", "1d"),
		"7d_tool_calls":                         usedMetric(281, "requests", "7d"),
		"today_input_tokens":                    usedMetric(182000, "tokens", "1d"),
		"today_output_tokens":                   usedMetric(64000, "tokens", "1d"),
		"7d_input_tokens":                       usedMetric(1230000, "tokens", "7d"),
		"7d_output_tokens":                      usedMetric(421000, "tokens", "7d"),
		"model_claude_sonnet_4_5_input_tokens":  usedMetric(820000, "tokens", "7d"),
		"model_claude_sonnet_4_5_output_tokens": usedMetric(286000, "tokens", "7d"),
		"model_claude_sonnet_4_5_requests":      usedMetric(932, "requests", "7d"),
		"model_claude_sonnet_4_5_cost_usd":      usedMetric(22.30, "USD", "7d"),
		"model_claude_haiku_3_5_input_tokens":   usedMetric(210000, "tokens", "7d"),
		"model_claude_haiku_3_5_output_tokens":  usedMetric(83000, "tokens", "7d"),
		"model_claude_haiku_3_5_requests":       usedMetric(511, "requests", "7d"),
		"model_claude_haiku_3_5_cost_usd":       usedMetric(4.10, "USD", "7d"),
		"client_claude_code_total_tokens":       usedMetric(900000, "tokens", "7d"),
		"client_claude_code_requests":           usedMetric(1020, "requests", "7d"),
		"client_claude_code_sessions":           usedMetric(19, "sessions", "7d"),
		"client_ide_total_tokens":               usedMetric(330000, "tokens", "7d"),
		"client_ide_requests":                   usedMetric(423, "requests", "7d"),
		"client_ide_sessions":                   usedMetric(11, "sessions", "7d"),
		"tool_edit":                             usedMetric(32, "requests", "7d"),
		"tool_bash":                             usedMetric(18, "requests", "7d"),
		"tool_read":                             usedMetric(24, "requests", "7d"),
		"tool_success_rate":                     usedMetric(94, "percent", "7d"),
		"mcp_github_total":                      usedMetric(16, "requests", "7d"),
		"mcp_github_search_repositories":        usedMetric(9, "requests", "7d"),
		"mcp_github_get_pull_request":           usedMetric(7, "requests", "7d"),
		"lang_go":                               usedMetric(58, "requests", "7d"),
		"lang_typescript":                       usedMetric(35, "requests", "7d"),
		"lang_markdown":                         usedMetric(14, "requests", "7d"),
		"composer_lines_added":                  usedMetric(980, "lines", "7d"),
		"composer_lines_removed":                usedMetric(420, "lines", "7d"),
		"composer_files_changed":                usedMetric(37, "files", "7d"),
		"scored_commits":                        usedMetric(9, "commits", "7d"),
		"ai_code_percentage":                    usedMetric(63, "percent", "7d"),
		"total_prompts":                         usedMetric(241, "requests", "7d"),
		"interface_bash":                        usedMetric(31, "requests", "7d"),
		"interface_edit":                        usedMetric(44, "requests", "7d"),
		"provider_anthropic_input_tokens":       usedMetric(1100000, "tokens", "7d"),
		"provider_anthropic_output_tokens":      usedMetric(369000, "tokens", "7d"),
		"provider_anthropic_requests":           usedMetric(1450, "requests", "7d"),
		"provider_anthropic_cost_usd":           usedMetric(26.40, "USD", "7d"),
		"upstream_aws_bedrock_input_tokens":     usedMetric(510000, "tokens", "7d"),
		"upstream_aws_bedrock_output_tokens":    usedMetric(177000, "tokens", "7d"),
		"upstream_aws_bedrock_requests":         usedMetric(742, "requests", "7d"),
		"upstream_aws_bedrock_cost_usd":         usedMetric(12.40, "USD", "7d"),
		"upstream_anthropic_input_tokens":       usedMetric(590000, "tokens", "7d"),
		"upstream_anthropic_output_tokens":      usedMetric(192000, "tokens", "7d"),
		"upstream_anthropic_requests":           usedMetric(708, "requests", "7d"),
		"upstream_anthropic_cost_usd":           usedMetric(14.00, "USD", "7d"),
	}
	snap.DailySeries = map[string][]core.TimePoint{
		"analytics_cost": {
			{Date: "2026-03-01", Value: 2.8},
			{Date: "2026-03-02", Value: 3.2},
			{Date: "2026-03-03", Value: 4.1},
			{Date: "2026-03-04", Value: 3.7},
			{Date: "2026-03-05", Value: 5.2},
		},
		"analytics_requests": {
			{Date: "2026-03-01", Value: 210},
			{Date: "2026-03-02", Value: 238},
			{Date: "2026-03-03", Value: 290},
			{Date: "2026-03-04", Value: 256},
			{Date: "2026-03-05", Value: 311},
		},
		"usage_model_claude_sonnet_4_5": {
			{Date: "2026-03-01", Value: 154},
			{Date: "2026-03-02", Value: 183},
			{Date: "2026-03-03", Value: 201},
			{Date: "2026-03-04", Value: 176},
			{Date: "2026-03-05", Value: 218},
		},
		"usage_model_claude_haiku_3_5": {
			{Date: "2026-03-01", Value: 91},
			{Date: "2026-03-02", Value: 88},
			{Date: "2026-03-03", Value: 103},
			{Date: "2026-03-04", Value: 97},
			{Date: "2026-03-05", Value: 111},
		},
		"usage_client_claude_code": {
			{Date: "2026-03-01", Value: 160},
			{Date: "2026-03-02", Value: 182},
			{Date: "2026-03-03", Value: 211},
			{Date: "2026-03-04", Value: 189},
			{Date: "2026-03-05", Value: 229},
		},
		"usage_client_ide": {
			{Date: "2026-03-01", Value: 63},
			{Date: "2026-03-02", Value: 71},
			{Date: "2026-03-03", Value: 79},
			{Date: "2026-03-04", Value: 67},
			{Date: "2026-03-05", Value: 82},
		},
		"usage_source_bedrock": {
			{Date: "2026-03-01", Value: 108},
			{Date: "2026-03-02", Value: 114},
			{Date: "2026-03-03", Value: 128},
			{Date: "2026-03-04", Value: 121},
			{Date: "2026-03-05", Value: 133},
		},
		"usage_source_claude": {
			{Date: "2026-03-01", Value: 102},
			{Date: "2026-03-02", Value: 124},
			{Date: "2026-03-03", Value: 146},
			{Date: "2026-03-04", Value: 135},
			{Date: "2026-03-05", Value: 152},
		},
	}
	return snap
}

func (m Model) renderSettingsThemeBody(w, h int) string {
	themes := AvailableThemes()
	activeThemeIdx := ActiveThemeIndex()
	activeThemeName := "none"
	if activeThemeIdx >= 0 && activeThemeIdx < len(themes) {
		activeThemeName = themes[activeThemeIdx].Name
	}
	lines := settingsBodyHeaderLines(
		"Theme Selection",
		fmt.Sprintf("%d themes available · active: %s", len(themes), activeThemeName),
	)
	nameW := w - 16
	if nameW < 12 {
		nameW = 12
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf("    %-3s %-3s %-3s %-*s", "#", "CUR", "ACT", nameW, "THEME")))
	lines = append(lines, settingsBodyRule(w))
	if len(themes) == 0 {
		lines = append(lines, dimStyle.Render("No themes available."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.themeCursor, 0, len(themes)-1)
	listHeight := h - len(lines)
	if listHeight < 1 {
		listHeight = 1
	}
	start, end := listWindow(len(themes), cursor, listHeight)

	for i := start; i < end; i++ {
		theme := themes[i]
		prefix := "  "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("➤ ")
		}

		current := "."
		if i == activeThemeIdx {
			current = "*"
		}
		selected := "."
		if i == cursor {
			selected = ">"
		}
		name := truncateToWidth(theme.Name, nameW)
		lines = append(lines, fmt.Sprintf("%s%-3d %-3s %-3s %-*s", prefix, i+1, selected, current, nameW, name))
	}

	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) renderSettingsViewBody(w, h int) string {
	configured := m.configuredDashboardView()
	active := m.activeDashboardView()
	lines := settingsBodyHeaderLines(
		"Dashboard View Mode",
		fmt.Sprintf("configured: %s · active: %s", configured, active),
	)
	lines = append(lines, dimStyle.Render("    CUR  MODE"))
	lines = append(lines, settingsBodyRule(w))
	if len(dashboardViewOptions) == 0 {
		lines = append(lines, dimStyle.Render("No dashboard views available."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.viewCursor, 0, len(dashboardViewOptions)-1)
	listHeight := h - len(lines)
	if listHeight < 1 {
		listHeight = 1
	}
	start, end := listWindow(len(dashboardViewOptions), cursor, listHeight)

	for i := start; i < end; i++ {
		option := dashboardViewOptions[i]

		prefix := "  "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("➤ ")
		}

		current := "  "
		if option.ID == configured {
			current = lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render("● ")
		}

		label := option.Label
		if option.ID == active && option.ID != configured {
			label += " (auto)"
		}

		lines = append(lines, fmt.Sprintf("%s%s%s", prefix, current, label))
		lines = append(lines, "    "+dimStyle.Render(option.Description))
	}

	return padToSize(strings.Join(lines, "\n"), w, h)
}

// apiKeysTabIDs returns account IDs for the API Keys tab, including
// unregistered API-key providers that the user can configure.
func (m Model) apiKeysTabIDs() []string {
	registeredProviders := make(map[string]bool)
	var ids []string
	for _, id := range m.providerOrder {
		providerID := m.accountProviders[id]
		if isAPIKeyProvider(providerID) {
			ids = append(ids, id)
			registeredProviders[providerID] = true
		}
	}
	for _, entry := range apiKeyProviderEntries() {
		if registeredProviders[entry.ProviderID] {
			continue
		}
		ids = append(ids, entry.AccountID)
	}
	return ids
}

// providerForAccountID looks up the provider ID for an account, falling back
// to the default API-key account mapping for unregistered providers.
func providerForAccountID(accountID string, accountProviders map[string]string) string {
	if p, ok := accountProviders[accountID]; ok && p != "" {
		return p
	}
	for _, entry := range apiKeyProviderEntries() {
		if entry.AccountID == accountID {
			return entry.ProviderID
		}
	}
	return ""
}

func maskAPIKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}

func (m Model) renderSettingsAPIKeysBody(w, h int) string {
	ids := m.apiKeysTabIDs()

	configuredCount := 0
	for _, id := range ids {
		providerID := providerForAccountID(id, m.accountProviders)
		if !isAPIKeyProvider(providerID) {
			continue
		}
		if envVar := envVarForProvider(providerID); envVar != "" && os.Getenv(envVar) != "" {
			configuredCount++
			continue
		}
		if snap, ok := m.snapshots[id]; ok && snap.Status == core.StatusOK {
			configuredCount++
		}
	}

	lines := settingsBodyHeaderLines(
		"API Key Management",
		fmt.Sprintf("%d/%d configured (env or validated)", configuredCount, len(ids)),
	)
	accountW := 20
	envW := w - accountW - 18
	if envW < 10 {
		envW = 10
		accountW = w - envW - 18
	}
	if accountW < 10 {
		accountW = 10
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf("    %-3s %-5s %-*s %-*s", "#", "STAT", accountW, "ACCOUNT", envW, "ENV VAR")))
	lines = append(lines, settingsBodyRule(w))
	if len(ids) == 0 {
		lines = append(lines, dimStyle.Render("No API-key providers available."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.cursor, 0, len(ids)-1)
	listHeight := h - len(lines)
	if listHeight < 1 {
		listHeight = 1
	}
	start, end := listWindow(len(ids), cursor, listHeight)

	for i := start; i < end; i++ {
		id := ids[i]
		providerID := providerForAccountID(id, m.accountProviders)
		if snap, ok := m.snapshots[id]; ok && snap.ProviderID != "" {
			providerID = snap.ProviderID
		}
		if providerID == "" {
			providerID = "unknown"
		}

		prefix := "  "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("➤ ")
		}

		if !isAPIKeyProvider(providerID) {
			line := fmt.Sprintf("%s%-3d %-5s %-*s %-*s", prefix, i+1, "N/A", accountW, truncateToWidth(id, accountW), envW, "-")
			lines = append(lines, line)
			continue
		}

		envVar := envVarForProvider(providerID)

		var statusText string
		if snap, ok := m.snapshots[id]; ok && snap.Status == core.StatusOK {
			statusText = "OK"
		} else if envVar != "" && os.Getenv(envVar) != "" {
			statusText = "ENV"
		} else {
			statusText = "MISS"
		}

		account := truncateToWidth(id, accountW)
		envLabel := "-"
		if envVar != "" {
			envLabel = envVar
		}
		envLabel = truncateToWidth(envLabel, envW)

		if m.settings.apiKeyEditing && i == cursor {
			masked := maskAPIKey(m.settings.apiKeyInput)
			inputStyle := lipgloss.NewStyle().Foreground(colorSapphire)
			cursorChar := PulseChar("█", "▌", m.animFrame)
			line := fmt.Sprintf("%s%-3d %-5s %-*s %-*s", prefix, i+1, statusText, accountW, account, envW, envLabel)
			lines = append(lines, line)
			keyLine := fmt.Sprintf("     key: %s", inputStyle.Render(masked+cursorChar))
			if m.settings.apiKeyStatus != "" {
				keyLine += "  " + dimStyle.Render(m.settings.apiKeyStatus)
			}
			lines = append(lines, keyLine)
		} else {
			line := fmt.Sprintf("%s%-3d %-5s %-*s %-*s", prefix, i+1, statusText, accountW, account, envW, envLabel)
			lines = append(lines, line)
		}
	}

	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) renderSettingsTelemetryBody(w, h int) string {
	lines := settingsBodyHeaderLines(
		"Telemetry & Time Window",
		"Choose aggregation window and map raw telemetry providers",
	)
	lines = append(lines, settingsBodyRule(w))
	lines = append(lines, "")

	// Time window selector
	lines = append(lines, lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render("Time Window")+"  "+dimStyle.Render("press w or select below"))
	lines = append(lines, "")
	for i, tw := range core.ValidTimeWindows {
		prefix := "  "
		if i == m.settings.cursor {
			prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("➤ ")
		}
		current := "  "
		if tw == m.timeWindow {
			current = lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render("● ")
		}
		lines = append(lines, fmt.Sprintf("%s%s%s", prefix, current, tw.Label()))
	}
	lines = append(lines, "")

	// Telemetry provider mapping section
	unmapped := m.telemetryUnmappedProviders()
	hints := m.telemetryProviderLinkHints()
	configured := m.configuredProviderIDs()

	if len(unmapped) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorGreen).Render("All telemetry providers are mapped."))
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorPeach).Bold(true).Render("Detected additional telemetry providers:"))
		for _, providerID := range unmapped {
			lines = append(lines, "  - "+providerID)
		}
		lines = append(lines, "")
		lines = append(lines, "Map them in settings.json under telemetry.provider_links:")
		lines = append(lines, "  <source_provider>=<configured_provider_id>")
		if len(hints) > 0 {
			lines = append(lines, "")
			lines = append(lines, "Hint:")
			lines = append(lines, "  "+hints[0])
		}
		if len(configured) > 0 {
			lines = append(lines, "")
			lines = append(lines, "Configured provider IDs:")
			lines = append(lines, "  "+strings.Join(configured, ", "))
		}
	}

	start, end := listWindow(len(lines), m.settings.bodyOffset, h)
	return padToSize(strings.Join(lines[start:end], "\n"), w, h)
}

func (m Model) renderSettingsIntegrationsBody(w, h int) string {
	statuses := m.settings.integrationStatus
	ready := 0
	outdated := 0
	for _, entry := range statuses {
		if entry.State == "ready" {
			ready++
		}
		if entry.NeedsUpgrade || entry.State == "outdated" {
			outdated++
		}
	}
	lines := settingsBodyHeaderLines(
		"Integrations",
		fmt.Sprintf("%d total · %d ready · %d need attention", len(statuses), ready, outdated),
	)
	lines = append(lines, settingsBodyRule(w))
	if len(statuses) == 0 {
		lines = append(lines, dimStyle.Render("No integration status available yet. Press r to refresh."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.cursor, 0, len(statuses)-1)
	listHeight := h - len(lines) - 4
	if listHeight < 1 {
		listHeight = 1
	}
	start, end := listWindow(len(statuses), cursor, listHeight)

	for i := start; i < end; i++ {
		entry := statuses[i]
		prefix := "  "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("➤ ")
		}

		stateColor := colorRed
		switch entry.State {
		case "ready":
			stateColor = colorGreen
		case "outdated":
			stateColor = colorYellow
		case "partial":
			stateColor = colorPeach
		}

		versionText := entry.DesiredVersion
		if strings.TrimSpace(entry.InstalledVersion) != "" {
			versionText = entry.InstalledVersion
		}
		stateText := lipgloss.NewStyle().Foreground(stateColor).Render(strings.ToUpper(entry.State))
		line := fmt.Sprintf("%s%s  %s  %s", prefix, entry.Name, stateText, dimStyle.Render("v"+versionText))
		lines = append(lines, line)
		lines = append(lines, "    "+dimStyle.Render(entry.Summary))
	}

	selected := statuses[cursor]
	lines = append(lines, "")
	lines = append(lines, "Selected:")
	lines = append(lines, fmt.Sprintf("  %s · installed=%t configured=%t", selected.Name, selected.Installed, selected.Configured))
	if selected.NeedsUpgrade {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorYellow).Render("Upgrade recommended: installed version differs from current integration version"))
	}
	lines = append(lines, "  Install/configure command writes plugin/hook files and updates tool configs automatically.")

	return padToSize(strings.Join(lines, "\n"), w, h)
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
