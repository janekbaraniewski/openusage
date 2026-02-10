package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ─── Help Overlay ───────────────────────────────────────────────────────────

// renderHelpOverlay draws a centered help popup with animated ASCII banner,
// theme gallery, category/status reference, and keybindings.
// Dismissed by pressing any key.
func (m Model) renderHelpOverlay(screenW, screenH int) string {
	headingStyle := lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(colorSapphire)
	descStyle := lipgloss.NewStyle().Foreground(colorSubtext)
	tagDescStyle := lipgloss.NewStyle().Foreground(colorText)
	dimHintStyle := lipgloss.NewStyle().Foreground(colorDim).Italic(true)

	var lines []string

	// ── Animated ASCII Art Banner ──
	banner := ASCIIBanner(m.animFrame)
	for _, bl := range strings.Split(banner, "\n") {
		lines = append(lines, "  "+bl)
	}
	lines = append(lines, "")

	// Subtitle
	subtitle := lipgloss.NewStyle().Foreground(colorSubtext).Italic(true).
		Render("  AI provider quota dashboard")
	lines = append(lines, subtitle)
	lines = append(lines, "")

	// ── Active Theme ──
	lines = append(lines, headingStyle.Render("  Themes")+"  "+
		dimHintStyle.Render("press t to cycle"))
	lines = append(lines, "")

	// Theme gallery — show all themes with active highlighted
	var themePills []string
	for i, t := range Themes {
		pill := t.Icon + " " + t.Name
		if i == ActiveThemeIdx {
			themePills = append(themePills, lipgloss.NewStyle().
				Bold(true).
				Foreground(colorMantle).
				Background(colorAccent).
				Padding(0, 1).
				Render(pill))
		} else {
			themePills = append(themePills, lipgloss.NewStyle().
				Foreground(colorSubtext).
				Background(colorSurface0).
				Padding(0, 1).
				Render(pill))
		}
	}
	// Wrap themes into rows of 3
	for i := 0; i < len(themePills); i += 3 {
		end := i + 3
		if end > len(themePills) {
			end = len(themePills)
		}
		lines = append(lines, "    "+strings.Join(themePills[i:end], " "))
	}
	lines = append(lines, "")

	// ── Category Tags ──
	lines = append(lines, headingStyle.Render("  Category Tags"))
	lines = append(lines, "")

	tags := []struct {
		emoji, label, desc string
	}{
		{"💰", "Spend", "Hard spending limit — $ used vs $ budget"},
		{"📊", "Plan", "Plan-level spending — $ used against allowance"},
		{"💳", "Credits", "Prepaid credit balance — remaining vs total"},
		{"⚡", "Rate", "Rate limits — requests/tokens remaining"},
		{"🔥", "Cost", "Running cost tracker — $ spent today or total"},
		{"⏱", "Block", "Time-block cost — $ spent in rolling window"},
		{"📊", "Quota", "Generic quota — % remaining of any limit"},
		{"💬", "Activity", "Activity counter — messages, sessions, tools"},
	}

	for _, t := range tags {
		tc := tagColor(t.label)
		tagStr := lipgloss.NewStyle().Foreground(tc).Bold(true).Render(t.emoji + " " + padRight(t.label, 10))
		lines = append(lines, "    "+tagStr+tagDescStyle.Render(t.desc))
	}
	lines = append(lines, "")

	// ── Status Icons ──
	lines = append(lines, headingStyle.Render("  Status Badges"))
	lines = append(lines, "")

	statuses := []struct {
		icon, badge, desc string
		color             lipgloss.Color
	}{
		{"●", "OK", "All good — quota/limits healthy", colorOK},
		{"◐", "WARN", "Approaching limit", colorWarn},
		{"◌", "LIMIT", "At or over limit", colorCrit},
		{"◈", "AUTH", "Authentication required", colorAuth},
		{"✗", "ERR", "Error fetching data", colorCrit},
		{"◇", "…", "Unknown or unsupported", colorDim},
	}

	for _, s := range statuses {
		iconStr := lipgloss.NewStyle().Foreground(s.color).Render(s.icon)
		badgeStr := lipgloss.NewStyle().Foreground(s.color).Bold(true).Render(padRight(s.badge, 7))
		lines = append(lines, "    "+iconStr+" "+badgeStr+tagDescStyle.Render(s.desc))
	}
	lines = append(lines, "")

	// ── Gauge Demo ──
	lines = append(lines, headingStyle.Render("  Gauge Bar"))
	lines = append(lines, "")
	lines = append(lines, "    "+RenderGauge(85, 16, 0.30, 0.15)+"  "+tagDescStyle.Render("healthy"))
	lines = append(lines, "    "+RenderGauge(25, 16, 0.30, 0.15)+"  "+tagDescStyle.Render("warning"))
	lines = append(lines, "    "+RenderGauge(8, 16, 0.30, 0.15)+"  "+tagDescStyle.Render("critical"))
	lines = append(lines, "")

	// ── Keybindings (compact) ──
	lines = append(lines, headingStyle.Render("  Keybindings"))
	lines = append(lines, "")

	type keyGroup struct {
		title string
		keys  []struct{ key, desc string }
	}

	groups := []keyGroup{
		{
			title: "Navigation",
			keys: []struct{ key, desc string }{
				{"↑↓ / j k", "Move cursor"},
				{"← → / h l", "Navigate tiles/panels"},
				{"⏎ Enter", "Open detail"},
				{"Esc", "Back"},
				{"Tab", "Next screen"},
			},
		},
		{
			title: "Actions",
			keys: []struct{ key, desc string }{
				{"/", "Filter providers"},
				{"[ ]", "Switch detail tabs"},
				{"s", "Cycle sort (analytics)"},
				{"g / G", "Top / bottom"},
				{"r", "Refresh"},
				{"t", "Cycle theme"},
			},
		},
		{
			title: "Global",
			keys: []struct{ key, desc string }{
				{"?", "Toggle help"},
				{"q", "Quit"},
			},
		},
	}

	for _, g := range groups {
		lines = append(lines, "    "+lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(g.title))
		for _, k := range g.keys {
			kStr := keyStyle.Render(padRight(k.key, 14))
			lines = append(lines, "      "+kStr+descStyle.Render(k.desc))
		}
		lines = append(lines, "")
	}

	lines = append(lines, "  "+dimHintStyle.Render("Press any key to dismiss"))

	// ── Build the overlay box ──
	content := strings.Join(lines, "\n")

	// Measure content dimensions
	contentW := 0
	for _, line := range lines {
		w := lipgloss.Width(line)
		if w > contentW {
			contentW = w
		}
	}

	// Add some padding to the box
	boxW := contentW + 4
	if boxW > screenW-4 {
		boxW = screenW - 4
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Background(colorBase).
		Padding(1, 2).
		Width(boxW)

	box := boxStyle.Render(content)

	// Center the box on screen
	boxRenderedW := lipgloss.Width(box)
	boxRenderedH := strings.Count(box, "\n") + 1

	padTop := (screenH - boxRenderedH) / 2
	if padTop < 0 {
		padTop = 0
	}
	padLeft := (screenW - boxRenderedW) / 2
	if padLeft < 0 {
		padLeft = 0
	}

	// Build the full overlay
	var overlay strings.Builder
	for i := 0; i < padTop; i++ {
		overlay.WriteString("\n")
	}
	for i, line := range strings.Split(box, "\n") {
		if i > 0 {
			overlay.WriteString("\n")
		}
		overlay.WriteString(strings.Repeat(" ", padLeft))
		overlay.WriteString(line)
	}

	// Pad remaining height
	renderedLines := padTop + boxRenderedH
	for renderedLines < screenH {
		overlay.WriteString("\n")
		renderedLines++
	}

	// Also add a version/credit line at the bottom
	creditLine := fmt.Sprintf("%s  •  %s",
		dimHintStyle.Render("AgentUsage"),
		dimHintStyle.Render(ThemeName()),
	)
	creditW := lipgloss.Width(creditLine)
	creditPad := (screenW - creditW) / 2
	if creditPad < 0 {
		creditPad = 0
	}
	// Replace the last empty line with credit
	result := overlay.String()
	resultLines := strings.Split(result, "\n")
	if len(resultLines) > 1 {
		resultLines[len(resultLines)-1] = strings.Repeat(" ", creditPad) + creditLine
		result = strings.Join(resultLines, "\n")
	}

	return result
}

// padRight pads a string with spaces to the given width.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
