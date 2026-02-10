package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ─── Help Overlay ───────────────────────────────────────────────────────────

// renderHelpOverlay draws a centered help popup explaining the TUI categories,
// status icons, and keybindings. Dismissed by pressing any key.
func (m Model) renderHelpOverlay(screenW, screenH int) string {
	// ── Build content sections ──

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	headingStyle := lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(colorSapphire)
	descStyle := lipgloss.NewStyle().Foreground(colorSubtext)
	tagDescStyle := lipgloss.NewStyle().Foreground(colorText)
	dimHintStyle := lipgloss.NewStyle().Foreground(colorDim).Italic(true)

	var lines []string

	// Title
	lines = append(lines, titleStyle.Render("  AgentUsage Help"))
	lines = append(lines, "")

	// ── Category Tags ──
	lines = append(lines, headingStyle.Render("  Category Tags"))
	lines = append(lines, descStyle.Render("  Each provider shows one tag based on what data is available:"))
	lines = append(lines, "")

	tags := []struct {
		emoji, label, color, desc string
	}{
		{"💰", "Spend", "peach", "Hard spending limit — $ used vs $ budget (e.g. Cursor team plans)"},
		{"📊", "Plan", "sapphire", "Plan-level spending — $ used against plan allowance"},
		{"💳", "Credits", "teal", "Prepaid credit balance — remaining vs total purchased"},
		{"⚡", "Rate", "yellow", "Rate limits — requests/tokens remaining in current window"},
		{"🔥", "Cost", "peach", "Running cost tracker — $ spent today or total (no hard limit)"},
		{"⏱", "Block", "sky", "Time-block cost — $ spent in a rolling window (e.g. 5h block)"},
		{"📊", "Quota", "sapphire", "Generic quota — % remaining of any limit (requests, tokens, etc.)"},
		{"💬", "Activity", "green", "Activity counter — messages, sessions, tool calls today"},
		{"📋", "Metrics", "subtext", "Raw metric — a single data point when nothing else matched"},
		{"ℹ", "Info", "lavender", "Informational message from the provider"},
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
		{"◐", "WARN", "Approaching limit (below warning threshold)", colorWarn},
		{"◌", "LIMIT", "At or over limit — requests may be rejected", colorCrit},
		{"◈", "AUTH", "Authentication required — check API key / login", colorAuth},
		{"✗", "ERR", "Error fetching data from provider", colorCrit},
		{"◇", "…", "Unknown or unsupported", colorDim},
	}

	for _, s := range statuses {
		iconStr := lipgloss.NewStyle().Foreground(s.color).Render(s.icon)
		badgeStr := lipgloss.NewStyle().Foreground(s.color).Bold(true).Render(padRight(s.badge, 7))
		lines = append(lines, "    "+iconStr+" "+badgeStr+tagDescStyle.Render(s.desc))
	}

	lines = append(lines, "")

	// ── Gauge Bar ──
	lines = append(lines, headingStyle.Render("  Gauge Bar"))
	lines = append(lines, "")
	lines = append(lines, "    "+tagDescStyle.Render("The progress bar shows % remaining (not used)."))
	lines = append(lines, "    "+RenderGauge(72, 20, 30, 15)+"  "+tagDescStyle.Render("← healthy"))
	lines = append(lines, "    "+RenderGauge(25, 20, 30, 15)+"  "+tagDescStyle.Render("← warning"))
	lines = append(lines, "    "+RenderGauge(8, 20, 30, 15)+"  "+tagDescStyle.Render("← critical"))
	lines = append(lines, "")

	// ── Screen Tabs ──
	lines = append(lines, headingStyle.Render("  Screen Tabs"))
	lines = append(lines, "")
	lines = append(lines, "    "+tagDescStyle.Render("Use Tab / Shift+Tab to cycle between screens:"))
	lines = append(lines, "")

	screenTabs := []struct{ key, desc string }{
		{"Tab", "Next screen (Dashboard → List → Analytics)"},
		{"Shift+Tab", "Previous screen"},
	}
	for _, st := range screenTabs {
		kStr := keyStyle.Render(padRight(st.key, 12))
		lines = append(lines, "    "+kStr+tagDescStyle.Render(st.desc))
	}
	lines = append(lines, "")

	// ── Dashboard Keybindings ──
	lines = append(lines, headingStyle.Render("  Dashboard Keys"))
	lines = append(lines, "")

	keys := []struct{ key, desc string }{
		{"↑↓ / j k", "Navigate providers"},
		{"← → / h l", "Navigate tiles / panels"},
		{"⏎ Enter", "Open detail view"},
		{"Esc / Backspace", "Back to list"},
		{"/", "Filter providers by name"},
		{"[ ]", "Switch detail tabs"},
		{"g / G", "Jump to top / bottom (detail)"},
		{"r", "Refresh all providers"},
	}

	for _, k := range keys {
		kStr := keyStyle.Render(padRight(k.key, 18))
		lines = append(lines, "    "+kStr+tagDescStyle.Render(k.desc))
	}

	lines = append(lines, "")

	// ── Analytics Keybindings ──
	lines = append(lines, headingStyle.Render("  Analytics Keys"))
	lines = append(lines, "")

	analyticsKeys := []struct{ key, desc string }{
		{"↑↓ / j k", "Scroll analytics content"},
		{"g / G", "Jump to top / bottom"},
		{"s", "Cycle sort: Cost ↓ → Name ↑ → Tokens ↓"},
		{"/", "Filter by provider or model name"},
		{"Esc", "Clear filter"},
	}

	for _, k := range analyticsKeys {
		kStr := keyStyle.Render(padRight(k.key, 18))
		lines = append(lines, "    "+kStr+tagDescStyle.Render(k.desc))
	}

	lines = append(lines, "")

	// ── Global ──
	lines = append(lines, headingStyle.Render("  Global"))
	lines = append(lines, "")

	globalKeys := []struct{ key, desc string }{
		{"?", "Toggle this help"},
		{"q / Ctrl+C", "Quit"},
	}

	for _, k := range globalKeys {
		kStr := keyStyle.Render(padRight(k.key, 18))
		lines = append(lines, "    "+kStr+tagDescStyle.Render(k.desc))
	}

	lines = append(lines, "")
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
	contentH := len(lines)

	// Add some padding to the box
	boxW := contentW + 4
	if boxW > screenW-4 {
		boxW = screenW - 4
	}
	boxH := contentH + 2
	if boxH > screenH-2 {
		boxH = screenH - 2
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

	return overlay.String()
}

// padRight pads a string with spaces to the given width.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
