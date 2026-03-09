package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

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

	return strings.Join(parts, strings.Repeat(" ", gap))
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
