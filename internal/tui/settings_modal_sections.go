package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
	providerW := max(10, w-accountW-16)
	if accountW = max(12, w-providerW-16); accountW < 12 {
		accountW = 12
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf("    %-3s %-3s %-*s %-*s", "#", "ON", accountW, "ACCOUNT", providerW, "PROVIDER")))
	lines = append(lines, settingsBodyRule(w))
	if len(ids) == 0 {
		lines = append(lines, dimStyle.Render("No providers available."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.cursor, 0, len(ids)-1)
	start, end := listWindow(len(ids), cursor, max(1, h-len(lines)))
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
		lines = append(lines, fmt.Sprintf("%s%-3d %s %-*s %-*s",
			prefix, i+1, onStyle.Render(onText), accountW, truncateToWidth(id, accountW), providerW, truncateToWidth(providerID, providerW)))
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
	hideStyle := lipgloss.NewStyle().Foreground(colorRed)
	if m.hideSectionsWithNoData {
		hideBox = "☑"
		hideStyle = lipgloss.NewStyle().Foreground(colorGreen)
	}
	lines = append(lines, fmt.Sprintf("Hide sections with no data: %s  %s", hideStyle.Render(hideBox), dimStyle.Render("press h to toggle")), "")

	nameW := max(12, w-24)
	lines = append(lines, dimStyle.Render(fmt.Sprintf("    %-3s %-3s %-*s %s", "#", "ON", nameW, "SECTION", "ID")))
	lines = append(lines, settingsBodyRule(w))
	if len(entries) == 0 {
		lines = append(lines, dimStyle.Render("No dashboard sections available."))
		return padToSize(strings.Join(lines, "\n"), w, h)
	}

	cursor := clamp(m.settings.sectionRowCursor, 0, len(entries)-1)
	start, end := listWindow(len(entries), cursor, max(1, h-len(lines)))
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
		lines = append(lines, fmt.Sprintf("%s%-3d %s %-*s %s",
			prefix, i+1, onStyle.Render(onText), nameW, truncateToWidth(settingsSectionLabel(entry.ID), nameW), dimStyle.Render(string(entry.ID))))
	}
	return padToSize(strings.Join(lines, "\n"), w, h)
}

func (m Model) renderSettingsWidgetSectionsPreview(w, h int) string {
	if w < 24 || h < 5 {
		return padToSize(dimStyle.Render("Live preview unavailable at this size."), w, h)
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render("Live Preview"),
		dimStyle.Render("Claude Code preset · synthetic data · PgUp/PgDn scroll"),
		"",
	}
	tileW := max(tileMinWidth, w-2)
	all := append(lines, strings.Split(m.renderTile(settingsWidgetSectionsPreviewSnapshot(), false, false, tileW, 0, 0), "\n")...)
	maxOffset := max(0, len(all)-h)
	offset := clamp(m.settings.previewOffset, 0, maxOffset)
	visible := all
	if len(visible) > h {
		visible = visible[offset:min(offset+h, len(visible))]
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
	innerW := max(24, contentW-4)
	bodyH := max(4, contentH-1)
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorRosewater).Render("Widget Preview"),
		lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", innerW)),
		m.renderSettingsWidgetSectionsPreview(innerW, bodyH),
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
	maxBodyH := contentH
	if sideBySide {
		maxBodyH = m.height - 12
	} else {
		maxBodyH = (m.height - 12) / 2
	}
	maxBodyH = max(settingsWidgetPreviewMinBodyH, maxBodyH)
	targetBodyH := max(settingsWidgetPreviewMinBodyH, m.settingsWidgetPreviewContentLineCount(max(24, contentW-4)))
	return min(targetBodyH, maxBodyH) + 1
}

func (m Model) settingsWidgetPreviewContentLineCount(innerW int) int {
	if innerW < 24 {
		return 4
	}
	tileW := max(tileMinWidth, innerW-2)
	return 3 + len(strings.Split(m.renderTile(settingsWidgetSectionsPreviewSnapshot(), false, false, tileW, 0, 0), "\n"))
}

func centerPanelVertically(panel string, targetHeight int) string {
	current := lipgloss.Height(panel)
	if current >= targetHeight {
		return panel
	}
	diff := targetHeight - current
	return strings.Repeat("\n", diff/2) + panel + strings.Repeat("\n", diff-diff/2)
}
