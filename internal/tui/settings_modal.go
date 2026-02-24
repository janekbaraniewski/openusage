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
	settingsTabTheme
	settingsTabAPIKeys
	settingsTabTelemetry
	settingsTabIntegrations
	settingsTabCount
)

var settingsTabNames = []string{
	"Providers",
	"Theme",
	"API Keys",
	"Telemetry",
	"Integrations",
}

func (m *Model) openSettingsModal() {
	m.showSettingsModal = true
	m.settingsStatus = ""
	m.settingsModalTab = settingsTabProviders
	m.apiKeyEditing = false
	m.apiKeyInput = ""
	m.apiKeyStatus = ""
	m.settingsBodyOffset = 0
	if len(m.providerOrder) > 0 {
		m.settingsCursor = clamp(m.settingsCursor, 0, len(m.providerOrder)-1)
	}
	if len(Themes) > 0 {
		m.settingsThemeCursor = clamp(ActiveThemeIdx, 0, len(Themes)-1)
	} else {
		m.settingsThemeCursor = 0
	}
	m.refreshIntegrationStatuses()
}

func (m *Model) closeSettingsModal() {
	m.showSettingsModal = false
	m.settingsStatus = ""
	m.apiKeyEditing = false
	m.apiKeyInput = ""
	m.apiKeyStatus = ""
	m.settingsBodyOffset = 0
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
	if m.apiKeyEditing {
		return m.handleAPIKeyEditKey(msg)
	}

	ids := m.settingsIDs()
	if m.settingsModalTab == settingsTabAPIKeys {
		ids = m.apiKeysTabIDs()
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "backspace", ",", "S":
		m.closeSettingsModal()
		return m, nil
	case "tab", "right", "l", "]":
		m.settingsModalTab = (m.settingsModalTab + 1) % settingsTabCount
		m.settingsBodyOffset = 0
		return m, nil
	case "shift+tab", "left", "h", "[":
		m.settingsModalTab = (m.settingsModalTab + settingsTabCount - 1) % settingsTabCount
		m.settingsBodyOffset = 0
		return m, nil
	case "r":
		if m.settingsModalTab == settingsTabIntegrations {
			m.refreshIntegrationStatuses()
			m.settingsStatus = "integration status refreshed"
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
				m.settingsModalTab = settingsModalTab(idx)
				m.settingsBodyOffset = 0
				return m, nil
			}
		}
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
			id := ids[clamp(m.settingsCursor, 0, len(ids)-1)]
			m.providerEnabled[id] = !m.isProviderEnabled(id)
			m.rebuildSortedIDs()
			m.settingsStatus = "saving settings..."
			return m, m.persistDashboardPrefsCmd()
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
	case settingsTabAPIKeys:
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
			providerID := providerForAccountID(id, m.accountProviders)
			if isAPIKeyProvider(providerID) {
				m.apiKeyEditing = true
				m.apiKeyInput = ""
				m.apiKeyEditAccountID = id
				m.apiKeyStatus = ""
				// Ensure the provider mapping exists (for unregistered providers)
				m.accountProviders[id] = providerID
			}
		case "d":
			if len(ids) == 0 {
				return m, nil
			}
			id := ids[clamp(m.settingsCursor, 0, len(ids)-1)]
			providerID := providerForAccountID(id, m.accountProviders)
			if isAPIKeyProvider(providerID) {
				m.settingsStatus = "deleting key..."
				return m, m.deleteCredentialCmd(id)
			}
		}
	case settingsTabTelemetry:
		switch msg.String() {
		case "up", "k":
			if m.settingsBodyOffset > 0 {
				m.settingsBodyOffset--
			}
		case "down", "j":
			m.settingsBodyOffset++
		case "pgup", "ctrl+u":
			m.settingsBodyOffset -= 4
			if m.settingsBodyOffset < 0 {
				m.settingsBodyOffset = 0
			}
		case "pgdown", "ctrl+d":
			m.settingsBodyOffset += 4
		case "home", "g":
			m.settingsBodyOffset = 0
		case "end", "G":
			m.settingsBodyOffset = 1 << 30
		}
	case settingsTabIntegrations:
		switch msg.String() {
		case "up", "k":
			if m.settingsCursor > 0 {
				m.settingsCursor--
			}
		case "down", "j":
			if m.settingsCursor < len(m.integrationStatuses)-1 {
				m.settingsCursor++
			}
		case "i", " ", "enter":
			if len(m.integrationStatuses) == 0 {
				return m, nil
			}
			cursor := clamp(m.settingsCursor, 0, len(m.integrationStatuses)-1)
			entry := m.integrationStatuses[cursor]
			m.settingsStatus = "installing integration..."
			return m, m.installIntegrationCmd(entry.ID)
		case "u":
			if len(m.integrationStatuses) == 0 {
				return m, nil
			}
			cursor := clamp(m.settingsCursor, 0, len(m.integrationStatuses)-1)
			entry := m.integrationStatuses[cursor]
			if !entry.NeedsUpgrade {
				m.settingsStatus = "selected integration is already current"
				return m, nil
			}
			m.settingsStatus = "upgrading integration..."
			return m, m.installIntegrationCmd(entry.ID)
		}
	}

	return m, nil
}

func (m *Model) moveSelectedProvider(ids []string, delta int) tea.Cmd {
	if m == nil || len(ids) == 0 || delta == 0 {
		return nil
	}
	cursor := clamp(m.settingsCursor, 0, len(ids)-1)
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
	m.settingsCursor = target
	m.rebuildSortedIDs()
	m.settingsStatus = "saving order..."
	return m.persistDashboardPrefsCmd()
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
		return "Up/Down: select  ·  Shift+↑/↓ or Shift+J/K: move item  ·  Space/Enter: enable/disable  ·  Left/Right: switch tab  ·  Esc: close"
	case settingsTabAPIKeys:
		if m.apiKeyEditing {
			return "Type API key  ·  Enter: validate & save  ·  Esc: cancel"
		}
		return "Up/Down: select  ·  Enter: edit key  ·  d: delete key  ·  Left/Right: switch tab  ·  Esc: close"
	case settingsTabTelemetry:
		return "Up/Down: scroll  ·  PgUp/PgDn: faster scroll  ·  Left/Right: switch tab  ·  Esc: close"
	case settingsTabIntegrations:
		return "Up/Down: select  ·  Enter/i: install/configure  ·  u: upgrade  ·  r: refresh  ·  Esc: close"
	default:
		return "Up/Down: select theme  ·  Space/Enter: apply theme  ·  Left/Right: switch tab  ·  Esc: close"
	}
}

func (m Model) renderSettingsModalBody(w, h int) string {
	switch m.settingsModalTab {
	case settingsTabProviders:
		return m.renderSettingsProvidersBody(w, h)
	case settingsTabAPIKeys:
		return m.renderSettingsAPIKeysBody(w, h)
	case settingsTabTelemetry:
		return m.renderSettingsTelemetryBody(w, h)
	case settingsTabIntegrations:
		return m.renderSettingsIntegrationsBody(w, h)
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
		line := fmt.Sprintf("%s%s %2d. %s  %s", prefix, boxStyle.Render(box), i+1, id, dimStyle.Render(providerID))
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
	if len(ids) == 0 {
		return padToSize(dimStyle.Render("No API-key providers available."), w, h)
	}

	cursor := clamp(m.settingsCursor, 0, len(ids)-1)
	start, end := listWindow(len(ids), cursor, h)
	lines := make([]string, 0, h)

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
			indicator := lipgloss.NewStyle().Foreground(colorDim).Render("○")
			label := dimStyle.Render("N/A")
			line := fmt.Sprintf("%s%s %s  %s", prefix, indicator, dimStyle.Render(id), label)
			lines = append(lines, line)
			continue
		}

		envVar := envVarForProvider(providerID)

		var indicator string
		if snap, ok := m.snapshots[id]; ok && snap.Status == core.StatusOK {
			indicator = lipgloss.NewStyle().Foreground(colorGreen).Render("✓")
		} else if envVar != "" && os.Getenv(envVar) != "" {
			indicator = lipgloss.NewStyle().Foreground(colorYellow).Render("env")
		} else {
			indicator = lipgloss.NewStyle().Foreground(colorRed).Render("✗")
		}

		envLabel := ""
		if envVar != "" {
			envLabel = "  " + dimStyle.Render(envVar)
		}

		if m.apiKeyEditing && i == cursor {
			masked := maskAPIKey(m.apiKeyInput)
			inputStyle := lipgloss.NewStyle().Foreground(colorSapphire)
			cursorChar := PulseChar("█", "▌", m.animFrame)
			line := fmt.Sprintf("%s%s %s  %s", prefix, indicator, id, inputStyle.Render(masked+cursorChar))
			if m.apiKeyStatus != "" {
				line += "  " + dimStyle.Render(m.apiKeyStatus)
			}
			lines = append(lines, line)
		} else {
			line := fmt.Sprintf("%s%s %s%s", prefix, indicator, id, envLabel)
			lines = append(lines, line)
		}
	}

	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) renderSettingsTelemetryBody(w, h int) string {
	unmapped := m.telemetryUnmappedProviders()
	hints := m.telemetryProviderLinkHints()
	configured := m.configuredProviderIDs()

	var lines []string
	if len(unmapped) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorGreen).Render("All telemetry providers are mapped."))
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("No action needed right now."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

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

	start, end := listWindow(len(lines), m.settingsBodyOffset, h)
	return padToSize(strings.Join(lines[start:end], "\n"), w, h)
}

func (m Model) renderSettingsIntegrationsBody(w, h int) string {
	statuses := m.integrationStatuses
	if len(statuses) == 0 {
		return padToSize(dimStyle.Render("No integration status available yet. Press r to refresh."), w, h)
	}

	cursor := clamp(m.settingsCursor, 0, len(statuses)-1)
	start, end := listWindow(len(statuses), cursor, h-4)
	lines := make([]string, 0, h)

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
		m.apiKeyEditing = false
		m.apiKeyInput = ""
		m.apiKeyStatus = ""
		return m, nil
	case "enter":
		if m.apiKeyInput == "" || m.apiKeyStatus == "validating..." {
			return m, nil
		}
		id := m.apiKeyEditAccountID
		providerID := m.accountProviders[id]
		m.apiKeyStatus = "validating..."
		return m, m.validateKeyCmd(id, providerID, m.apiKeyInput)
	case "backspace":
		if len(m.apiKeyInput) > 0 {
			m.apiKeyInput = m.apiKeyInput[:len(m.apiKeyInput)-1]
		}
		m.apiKeyStatus = ""
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.apiKeyInput += string(msg.Runes)
			m.apiKeyStatus = ""
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
