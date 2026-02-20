package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type settingsModalTab int

const (
	settingsTabProviders settingsModalTab = iota
	settingsTabOrder
	settingsTabTheme
	settingsTabCount
)

var settingsTabNames = []string{
	"Providers",
	"Order",
	"Theme",
}

func (m *Model) openSettingsModal() {
	m.showSettingsModal = true
	m.settingsStatus = ""
	m.settingsModalTab = settingsTabProviders
	if len(m.providerOrder) > 0 {
		m.settingsCursor = clamp(m.settingsCursor, 0, len(m.providerOrder)-1)
	}
	if len(Themes) > 0 {
		m.settingsThemeCursor = clamp(ActiveThemeIdx, 0, len(Themes)-1)
	} else {
		m.settingsThemeCursor = 0
	}
}

func (m *Model) closeSettingsModal() {
	m.showSettingsModal = false
	m.settingsStatus = ""
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
	if int(m.settingsModalTab) >= 0 && int(m.settingsModalTab) < len(settingsTabNames) {
		tabName = settingsTabNames[m.settingsModalTab]
	}

	info := fmt.Sprintf("⚙ %s · %d/%d active", tabName, active, len(ids))
	if m.settingsStatus != "" {
		info += " · " + m.settingsStatus
	}
	return info
}

func (m Model) handleSettingsModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ids := m.settingsIDs()

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "backspace", ",", "S":
		m.closeSettingsModal()
		return m, nil
	case "tab", "right", "l", "]":
		m.settingsModalTab = (m.settingsModalTab + 1) % settingsTabCount
		return m, nil
	case "shift+tab", "left", "h", "[":
		m.settingsModalTab = (m.settingsModalTab + settingsTabCount - 1) % settingsTabCount
		return m, nil
	case "1", "2", "3":
		idx := int(msg.String()[0] - '1')
		if idx >= 0 && idx < int(settingsTabCount) {
			m.settingsModalTab = settingsModalTab(idx)
		}
		return m, nil
	case "r":
		m.refreshing = true
		return m, nil
	}

	switch m.settingsModalTab {
	case settingsTabProviders:
		switch msg.String() {
		case "up", "k":
			if m.settingsCursor > 0 {
				m.settingsCursor--
			}
		case "down", "j":
			if m.settingsCursor < len(ids)-1 {
				m.settingsCursor++
			}
		case " ", "enter":
			if len(ids) == 0 {
				return m, nil
			}
			id := ids[clamp(m.settingsCursor, 0, len(ids)-1)]
			m.providerEnabled[id] = !m.isProviderEnabled(id)
			m.rebuildSortedIDs()
			m.settingsStatus = "saving settings..."
			return m, m.persistDashboardPrefsCmd()
		}
	case settingsTabOrder:
		switch msg.String() {
		case "up", "k":
			if m.settingsCursor > 0 {
				m.settingsCursor--
			}
		case "down", "j":
			if m.settingsCursor < len(ids)-1 {
				m.settingsCursor++
			}
		case "K":
			if len(ids) == 0 || m.settingsCursor <= 0 {
				return m, nil
			}
			id := ids[m.settingsCursor]
			prevID := ids[m.settingsCursor-1]
			currIdx := m.providerOrderIndex(id)
			prevIdx := m.providerOrderIndex(prevID)
			if currIdx >= 0 && prevIdx >= 0 {
				m.providerOrder[currIdx], m.providerOrder[prevIdx] = m.providerOrder[prevIdx], m.providerOrder[currIdx]
				m.settingsCursor--
				m.rebuildSortedIDs()
				m.settingsStatus = "saving order..."
				return m, m.persistDashboardPrefsCmd()
			}
		case "J":
			if len(ids) == 0 || m.settingsCursor >= len(ids)-1 {
				return m, nil
			}
			id := ids[m.settingsCursor]
			nextID := ids[m.settingsCursor+1]
			currIdx := m.providerOrderIndex(id)
			nextIdx := m.providerOrderIndex(nextID)
			if currIdx >= 0 && nextIdx >= 0 {
				m.providerOrder[currIdx], m.providerOrder[nextIdx] = m.providerOrder[nextIdx], m.providerOrder[currIdx]
				m.settingsCursor++
				m.rebuildSortedIDs()
				m.settingsStatus = "saving order..."
				return m, m.persistDashboardPrefsCmd()
			}
		}
	case settingsTabTheme:
		switch msg.String() {
		case "up", "k":
			if m.settingsThemeCursor > 0 {
				m.settingsThemeCursor--
			}
		case "down", "j":
			if m.settingsThemeCursor < len(Themes)-1 {
				m.settingsThemeCursor++
			}
		case " ", "enter":
			if len(Themes) == 0 {
				return m, nil
			}
			m.settingsThemeCursor = clamp(m.settingsThemeCursor, 0, len(Themes)-1)
			name := Themes[m.settingsThemeCursor].Name
			if SetThemeByName(name) {
				m.settingsStatus = "saving theme..."
				return m, m.persistThemeCmd(name)
			}
		}
	}

	return m, nil
}

func (m Model) renderSettingsModalOverlay() string {
	if m.width < 40 || m.height < 12 {
		return m.renderDashboard()
	}

	contentW := m.width - 24
	if contentW < 50 {
		contentW = 50
	}
	if contentW > 92 {
		contentW = 92
	}

	contentH := m.height - 14
	if contentH < 8 {
		contentH = 8
	}
	if contentH > 16 {
		contentH = 16
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(colorRosewater).Render("Settings")
	tabs := m.renderSettingsModalTabs()
	body := m.renderSettingsModalBody(contentW, contentH)
	hint := dimStyle.Render(m.settingsModalHint())

	status := ""
	if m.settingsStatus != "" {
		status = lipgloss.NewStyle().Foreground(colorSapphire).Render(m.settingsStatus)
	}

	lines := []string{
		title,
		tabs,
		lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", contentW)),
		body,
		lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", contentW)),
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

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m Model) renderSettingsModalTabs() string {
	parts := make([]string, 0, len(settingsTabNames))
	for i, name := range settingsTabNames {
		label := fmt.Sprintf("%d:%s", i+1, name)
		if settingsModalTab(i) == m.settingsModalTab {
			parts = append(parts, screenTabActiveStyle.Render(label))
		} else {
			parts = append(parts, screenTabInactiveStyle.Render(label))
		}
	}
	return strings.Join(parts, "")
}

func (m Model) settingsModalHint() string {
	switch m.settingsModalTab {
	case settingsTabProviders:
		return "Up/Down: select  ·  Space/Enter: enable/disable  ·  Left/Right: switch tab  ·  Esc: close"
	case settingsTabOrder:
		return "Up/Down: select  ·  Shift+K/J: move item  ·  Left/Right: switch tab  ·  Esc: close"
	default:
		return "Up/Down: select theme  ·  Space/Enter: apply theme  ·  Left/Right: switch tab  ·  Esc: close"
	}
}

func (m Model) renderSettingsModalBody(w, h int) string {
	switch m.settingsModalTab {
	case settingsTabProviders:
		return m.renderSettingsProvidersBody(w, h)
	case settingsTabOrder:
		return m.renderSettingsOrderBody(w, h)
	default:
		return m.renderSettingsThemeBody(w, h)
	}
}

func (m Model) renderSettingsProvidersBody(w, h int) string {
	ids := m.settingsIDs()
	if len(ids) == 0 {
		return padToSize(dimStyle.Render("No providers available."), w, h)
	}

	cursor := clamp(m.settingsCursor, 0, len(ids)-1)
	start, end := listWindow(len(ids), cursor, h)
	lines := make([]string, 0, h)

	for i := start; i < end; i++ {
		id := ids[i]
		providerID := m.accountProviders[id]
		if snap, ok := m.snapshots[id]; ok && snap.ProviderID != "" {
			providerID = snap.ProviderID
		}
		if providerID == "" {
			providerID = "unknown"
		}

		box := "☐"
		boxStyle := lipgloss.NewStyle().Foreground(colorRed)
		if m.isProviderEnabled(id) {
			box = "☑"
			boxStyle = lipgloss.NewStyle().Foreground(colorGreen)
		}

		prefix := "  "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("➤ ")
		}
		line := fmt.Sprintf("%s%s %s  %s", prefix, boxStyle.Render(box), id, dimStyle.Render(providerID))
		lines = append(lines, line)
	}

	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) renderSettingsOrderBody(w, h int) string {
	ids := m.settingsIDs()
	if len(ids) == 0 {
		return padToSize(dimStyle.Render("No providers available."), w, h)
	}

	cursor := clamp(m.settingsCursor, 0, len(ids)-1)
	start, end := listWindow(len(ids), cursor, h)
	lines := make([]string, 0, h)

	for i := start; i < end; i++ {
		id := ids[i]
		prefix := "  "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("➤ ")
		}
		line := fmt.Sprintf("%s%2d. %s", prefix, i+1, id)
		lines = append(lines, line)
	}

	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) renderSettingsThemeBody(w, h int) string {
	if len(Themes) == 0 {
		return padToSize(dimStyle.Render("No themes available."), w, h)
	}

	cursor := clamp(m.settingsThemeCursor, 0, len(Themes)-1)
	start, end := listWindow(len(Themes), cursor, h)
	lines := make([]string, 0, h)

	for i := start; i < end; i++ {
		theme := Themes[i]
		prefix := "  "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("➤ ")
		}

		current := "  "
		if i == ActiveThemeIdx {
			current = lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render("● ")
		}
		lines = append(lines, fmt.Sprintf("%s%s%s %s", prefix, current, theme.Icon, theme.Name))
	}

	return padToSize(strings.Join(lines, "\n"), w, h)
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
