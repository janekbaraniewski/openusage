package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func (m Model) renderSettingsThemeBody(w, h int) string {
	themes := AvailableThemes()
	activeThemeIdx := ActiveThemeIndex()
	activeThemeName := "none"
	if activeThemeIdx >= 0 && activeThemeIdx < len(themes) {
		activeThemeName = themes[activeThemeIdx].Name
	}
	lines := settingsBodyHeaderLines("Theme Selection", fmt.Sprintf("%d themes available · active: %s", len(themes), activeThemeName))
	nameW := max(12, w-16)
	lines = append(lines, dimStyle.Render(fmt.Sprintf("    %-3s %-3s %-3s %-*s", "#", "CUR", "ACT", nameW, "THEME")), settingsBodyRule(w))
	if len(themes) == 0 {
		lines = append(lines, dimStyle.Render("No themes available."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.themeCursor, 0, len(themes)-1)
	start, end := listWindow(len(themes), cursor, max(1, h-len(lines)))
	for i := start; i < end; i++ {
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
		lines = append(lines, fmt.Sprintf("%s%-3d %-3s %-3s %-*s", prefix, i+1, selected, current, nameW, truncateToWidth(themes[i].Name, nameW)))
	}
	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) renderSettingsViewBody(w, h int) string {
	configured := m.configuredDashboardView()
	active := m.activeDashboardView()
	lines := settingsBodyHeaderLines("Dashboard View Mode", fmt.Sprintf("configured: %s · active: %s", configured, active))
	lines = append(lines, dimStyle.Render("    CUR  MODE"), settingsBodyRule(w))
	if len(dashboardViewOptions) == 0 {
		lines = append(lines, dimStyle.Render("No dashboard views available."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.viewCursor, 0, len(dashboardViewOptions)-1)
	start, end := listWindow(len(dashboardViewOptions), cursor, max(1, h-len(lines)))
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
		lines = append(lines, fmt.Sprintf("%s%s%s", prefix, current, label), "    "+dimStyle.Render(option.Description))
	}
	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) apiKeysTabIDs() []string {
	registered := make(map[string]bool)
	var ids []string
	for _, id := range m.providerOrder {
		providerID := m.accountProviders[id]
		if isAPIKeyProvider(providerID) {
			ids = append(ids, id)
			registered[providerID] = true
		}
	}
	for _, entry := range apiKeyProviderEntries() {
		if !registered[entry.ProviderID] {
			ids = append(ids, entry.AccountID)
		}
	}
	return ids
}

func providerForAccountID(accountID string, accountProviders map[string]string) string {
	if providerID := strings.TrimSpace(accountProviders[accountID]); providerID != "" {
		return providerID
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

	lines := settingsBodyHeaderLines("API Key Management", fmt.Sprintf("%d/%d configured (env or validated)", configuredCount, len(ids)))
	accountW := 20
	envW := max(10, w-accountW-18)
	if accountW = max(10, w-envW-18); accountW < 10 {
		accountW = 10
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf("    %-3s %-5s %-*s %-*s", "#", "STAT", accountW, "ACCOUNT", envW, "ENV VAR")), settingsBodyRule(w))
	if len(ids) == 0 {
		lines = append(lines, dimStyle.Render("No API-key providers available."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.cursor, 0, len(ids)-1)
	start, end := listWindow(len(ids), cursor, max(1, h-len(lines)))
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
			lines = append(lines, fmt.Sprintf("%s%-3d %-5s %-*s %-*s", prefix, i+1, "N/A", accountW, truncateToWidth(id, accountW), envW, "-"))
			continue
		}

		envLabel := truncateToWidth(core.FirstNonEmpty(envVarForProvider(providerID), "-"), envW)
		statusText := "MISS"
		if snap, ok := m.snapshots[id]; ok && snap.Status == core.StatusOK {
			statusText = "OK"
		} else if envVar := envVarForProvider(providerID); envVar != "" && os.Getenv(envVar) != "" {
			statusText = "ENV"
		}
		lines = append(lines, fmt.Sprintf("%s%-3d %-5s %-*s %-*s", prefix, i+1, statusText, accountW, truncateToWidth(id, accountW), envW, envLabel))
		if m.settings.apiKeyEditing && i == cursor {
			cursorChar := PulseChar("█", "▌", m.animFrame)
			keyLine := fmt.Sprintf("     key: %s", lipgloss.NewStyle().Foreground(colorSapphire).Render(maskAPIKey(m.settings.apiKeyInput)+cursorChar))
			if m.settings.apiKeyStatus != "" {
				keyLine += "  " + dimStyle.Render(m.settings.apiKeyStatus)
			}
			lines = append(lines, keyLine)
		}
	}
	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) renderSettingsTelemetryBody(w, h int) string {
	lines := settingsBodyHeaderLines("Telemetry & Time Window", "Choose aggregation window and map raw telemetry providers")
	lines = append(lines, settingsBodyRule(w), "", lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render("Time Window")+"  "+dimStyle.Render("press w or select below"), "")
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

	unmapped := m.telemetryUnmappedProviders()
	if len(unmapped) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorGreen).Render("All telemetry providers are mapped."))
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorPeach).Bold(true).Render("Detected additional telemetry providers:"))
		for _, providerID := range unmapped {
			lines = append(lines, "  - "+providerID)
		}
		lines = append(lines, "", "Map them in settings.json under telemetry.provider_links:", "  <source_provider>=<configured_provider_id>")
		if hints := m.telemetryProviderLinkHints(); len(hints) > 0 {
			lines = append(lines, "", "Hint:", "  "+hints[0])
		}
		if configured := m.configuredProviderIDs(); len(configured) > 0 {
			lines = append(lines, "", "Configured provider IDs:", "  "+strings.Join(configured, ", "))
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
	lines := settingsBodyHeaderLines("Integrations", fmt.Sprintf("%d total · %d ready · %d need attention", len(statuses), ready, outdated))
	lines = append(lines, settingsBodyRule(w))
	if len(statuses) == 0 {
		lines = append(lines, dimStyle.Render("No integration status available yet. Press r to refresh."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.cursor, 0, len(statuses)-1)
	start, end := listWindow(len(statuses), cursor, max(1, h-len(lines)-4))
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
		versionText := core.FirstNonEmpty(strings.TrimSpace(entry.InstalledVersion), entry.DesiredVersion)
		lines = append(lines,
			fmt.Sprintf("%s%s  %s  %s", prefix, entry.Name, lipgloss.NewStyle().Foreground(stateColor).Render(strings.ToUpper(entry.State)), dimStyle.Render("v"+versionText)),
			"    "+dimStyle.Render(entry.Summary),
		)
	}

	selected := statuses[cursor]
	lines = append(lines, "", "Selected:", fmt.Sprintf("  %s · installed=%t configured=%t", selected.Name, selected.Installed, selected.Configured))
	if selected.NeedsUpgrade {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorYellow).Render("Upgrade recommended: installed version differs from current integration version"))
	}
	lines = append(lines, "  Install/configure command writes plugin/hook files and updates tool configs automatically.")
	return padToSize(strings.Join(lines, "\n"), w, h)
}
