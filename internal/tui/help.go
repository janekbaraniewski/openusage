package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderHelpOverlay(screenW, screenH int) string {
	headingStyle := lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(colorSapphire)
	descStyle := lipgloss.NewStyle().Foreground(colorSubtext)
	tagDescStyle := lipgloss.NewStyle().Foreground(colorText)
	dimHintStyle := lipgloss.NewStyle().Foreground(colorDim).Italic(true)

	var lines []string

	banner := ASCIIBanner(m.animFrame)
	for _, bl := range strings.Split(banner, "\n") {
		lines = append(lines, "  "+bl)
	}
	lines = append(lines, "")

	subtitle := lipgloss.NewStyle().Foreground(colorSubtext).Italic(true).
		Render("  AI provider usage and spend dashboard")
	lines = append(lines, subtitle)
	lines = append(lines, "")

	lines = append(lines, headingStyle.Render("  Themes")+"  "+
		dimHintStyle.Render("press t to cycle"))
	lines = append(lines, "")

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
	for i := 0; i < len(themePills); i += 3 {
		end := i + 3
		if end > len(themePills) {
			end = len(themePills)
		}
		lines = append(lines, "    "+strings.Join(themePills[i:end], " "))
	}
	lines = append(lines, "")

	lines = append(lines, headingStyle.Render("  Billing Types"))
	lines = append(lines, "")

	tags := []struct {
		emoji, label, desc string
	}{
		{"💰", "Credits", "Token/API spend model — billed per usage amount"},
		{"⚡", "Usage", "Quota/window model — available usage over a reset period"},
	}

	for _, t := range tags {
		tc := tagColor(t.label)
		tagStr := lipgloss.NewStyle().Foreground(tc).Bold(true).Render(t.emoji + " " + padRight(t.label, 10))
		lines = append(lines, "    "+tagStr+tagDescStyle.Render(t.desc))
	}
	lines = append(lines, "")

	lines = append(lines, headingStyle.Render("  Status Badges"))
	lines = append(lines, "")

	statuses := []struct {
		icon, badge, desc string
		color             lipgloss.Color
	}{
		{"●", "OK", "All good — usage/spend healthy", colorOK},
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

	lines = append(lines, headingStyle.Render("  Gauge Bar"))
	lines = append(lines, "")
	lines = append(lines, "    "+RenderGauge(85, 16, 0.30, 0.15)+"  "+tagDescStyle.Render("healthy"))
	lines = append(lines, "    "+RenderGauge(25, 16, 0.30, 0.15)+"  "+tagDescStyle.Render("warning"))
	lines = append(lines, "    "+RenderGauge(8, 16, 0.30, 0.15)+"  "+tagDescStyle.Render("critical"))
	lines = append(lines, "")

	lines = append(lines, headingStyle.Render("  Keybindings"))
	lines = append(lines, "")

	type keyGroup struct {
		title string
		keys  []struct{ key, desc string }
	}

	navKeys := []struct{ key, desc string }{
		{"↑↓ / j k", "Move cursor"},
		{"← → / h l", "Navigate tiles/panels"},
		{"⏎ Enter", "Open detail"},
		{"Esc", "Back"},
	}
	navKeys = append(navKeys, struct{ key, desc string }{"Tab / Shift+Tab", "Switch screen"})

	actionKeys := []struct{ key, desc string }{
		{", / Shift+S", "Open settings modal"},
		{"/", "Filter providers"},
		{"Mouse wheel", "Scroll (single-column tiles/detail)"},
		{"PgUp/PgDn", "Scroll selected tile"},
		{"Ctrl+U / Ctrl+D", "Fast tile scroll"},
		{"Ctrl+O", "Expand/collapse usage breakdowns"},
		{"[ ]", "Switch detail tabs"},
		{fmt.Sprintf("1-%d / ←→", settingsTabCount), "Switch settings tabs"},
		{"Space / Enter", "Apply setting in modal"},
		{"Shift+J/K", "Reorder providers (order tab)"},
	}
	if m.experimentalAnalytics {
		actionKeys = append(actionKeys,
			struct{ key, desc string }{"s", "Cycle sort (analytics)"},
		)
	}
	actionKeys = append(actionKeys,
		struct{ key, desc string }{"r", "Refresh"},
		struct{ key, desc string }{"t", "Cycle theme"},
		struct{ key, desc string }{"w", "Cycle time window"},
	)

	groups := []keyGroup{
		{title: "Navigation", keys: navKeys},
		{title: "Actions", keys: actionKeys},
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

	content := strings.Join(lines, "\n")

	contentW := 0
	for _, line := range lines {
		w := lipgloss.Width(line)
		if w > contentW {
			contentW = w
		}
	}

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

	renderedLines := padTop + boxRenderedH
	for renderedLines < screenH {
		overlay.WriteString("\n")
		renderedLines++
	}

	creditLine := fmt.Sprintf("%s  •  %s",
		dimHintStyle.Render("OpenUsage"),
		dimHintStyle.Render(ThemeName()),
	)
	creditW := lipgloss.Width(creditLine)
	creditPad := (screenW - creditW) / 2
	if creditPad < 0 {
		creditPad = 0
	}
	result := overlay.String()
	resultLines := strings.Split(result, "\n")
	if len(resultLines) > 1 {
		resultLines[len(resultLines)-1] = strings.Repeat(" ", creditPad) + creditLine
		result = strings.Join(resultLines, "\n")
	}

	return result
}

func (m Model) renderSplash(screenW, screenH int) string {
	dimHint := lipgloss.NewStyle().Foreground(colorSubtext).Italic(true)

	// Build banner lines.
	var bannerLines []string
	banner := ASCIIBanner(m.animFrame)
	for _, bl := range strings.Split(banner, "\n") {
		bannerLines = append(bannerLines, "  "+bl)
	}

	// Build content lines (progress + hint).
	var contentLines []string
	contentLines = append(contentLines, m.splashProgressLines()...)
	contentLines = append(contentLines, "")
	contentLines = append(contentLines, "  "+dimHint.Render("Press q to quit"))

	// Horizontal centering based on banner width only — banner is the anchor.
	// Content aligns to the same left edge; if wider, it extends right.
	bannerMaxW := 0
	for _, l := range bannerLines {
		if w := lipgloss.Width(l); w > bannerMaxW {
			bannerMaxW = w
		}
	}
	padLeft := (screenW - bannerMaxW) / 2
	if padLeft < 0 {
		padLeft = 0
	}

	// Fixed banner vertical position at ~1/3 from top.
	bannerH := len(bannerLines)
	bannerTop := (screenH / 3) - (bannerH / 2)
	if bannerTop < 1 {
		bannerTop = 1
	}

	var out strings.Builder

	for i := 0; i < bannerTop; i++ {
		out.WriteRune('\n')
	}
	for i, line := range bannerLines {
		if i > 0 {
			out.WriteRune('\n')
		}
		out.WriteString(strings.Repeat(" ", padLeft))
		out.WriteString(line)
	}
	out.WriteRune('\n')
	for _, line := range contentLines {
		out.WriteRune('\n')
		out.WriteString(strings.Repeat(" ", padLeft))
		out.WriteString(line)
	}

	return out.String()
}

func (m Model) splashProgressLines() []string {
	accent := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(colorSubtext).Italic(true)
	warn := lipgloss.NewStyle().Foreground(colorYellow)
	errStyle := lipgloss.NewStyle().Foreground(colorRed)
	hint := lipgloss.NewStyle().Foreground(colorGreen)
	ok := lipgloss.NewStyle().Foreground(colorGreen)

	spinner := accent.Render(SpinnerFrames[m.animFrame%len(SpinnerFrames)])

	done := func(text string) string { return "  " + ok.Render("✓") + " " + text }
	spin := func(text string) string { return "  " + spinner + " " + dim.Render(text) }

	var lines []string

	// Step 1: Config — always done.
	lines = append(lines, done("Configuration loaded"))

	// Step 2: Providers.
	n := len(m.providerOrder)
	if n > 0 {
		lines = append(lines, done(fmt.Sprintf("%d providers detected", n)))
	} else {
		lines = append(lines, "  "+dim.Render("· No providers detected"))
	}

	if m.hasAppUpdateNotice() {
		lines = append(lines, "")
		lines = append(lines, "  "+warn.Render(m.appUpdateHeadline()))
		if action := m.appUpdateAction(); action != "" {
			lines = append(lines, "  "+hint.Render("▸ "+action))
		}
	}

	// Step 3+: Helper lifecycle — show accumulated progress.
	switch m.daemonStatus {
	case DaemonConnecting:
		lines = append(lines, spin("Connecting to background helper..."))

	case DaemonNotInstalled:
		if m.daemonInstalling {
			lines = append(lines, spin("Setting up background helper..."))
		} else {
			lines = append(lines, "")
			lines = append(lines, "  "+dim.Render("OpenUsage uses a small background helper to"))
			lines = append(lines, "  "+dim.Render("collect and cache usage data from your providers."))
			lines = append(lines, "")
			lines = append(lines, "  "+hint.Render("▸ Press Enter to set it up"))
			lines = append(lines, "  "+dim.Render("  or run: openusage telemetry daemon install"))
		}

	case DaemonOutdated:
		if m.daemonInstalling {
			lines = append(lines, spin("Updating background helper..."))
		} else {
			lines = append(lines, "  "+warn.Render("Background helper needs an update."))
			lines = append(lines, "")
			lines = append(lines, "  "+hint.Render("▸ Press Enter to update"))
		}

	case DaemonStarting:
		if m.daemonInstallDone {
			lines = append(lines, done("Background helper installed"))
		}
		lines = append(lines, spin("Starting background helper..."))

	case DaemonError:
		if m.daemonInstallDone {
			lines = append(lines, done("Background helper installed"))
		}
		msg := "Could not connect to background helper."
		if m.daemonMessage != "" {
			msg = m.daemonMessage
			if idx := strings.IndexByte(msg, '\n'); idx >= 0 {
				msg = msg[:idx]
			}
			if len(msg) > 60 {
				msg = msg[:57] + "..."
			}
		}
		lines = append(lines, "  "+errStyle.Render("✗")+" "+errStyle.Render(msg))
		lines = append(lines, "  "+dim.Render("Try: openusage telemetry daemon status"))
		lines = append(lines, "  "+dim.Render("If needed: openusage telemetry daemon install"))

	default: // DaemonRunning or any other state.
		if m.daemonInstallDone {
			lines = append(lines, done("Background helper installed"))
		}
		lines = append(lines, done("Background helper running"))
		if !m.hasData {
			lines = append(lines, spin("Fetching usage data..."))
		}
	}

	return lines
}

func (m Model) resolveLoadingMessage(message, fallback string) string {
	msg := strings.TrimSpace(message)
	if msg != "" && !strings.EqualFold(msg, "connected") {
		return msg
	}
	fb := strings.TrimSpace(fallback)
	if fb != "" {
		return fb
	}
	return "Connecting to telemetry daemon..."
}

func (m Model) brandedLoaderLines(maxWidth int, message, fallback string) []string {
	msg := m.resolveLoadingMessage(message, fallback)
	if maxWidth > 4 && lipgloss.Width(msg) > maxWidth-4 {
		msg = truncateToWidth(msg, maxWidth-4)
	}

	spinner := lipgloss.NewStyle().
		Foreground(colorAccent).
		Bold(true).
		Render(SpinnerFrames[m.animFrame%len(SpinnerFrames)])
	sub := lipgloss.NewStyle().
		Foreground(colorSubtext).
		Italic(true).
		Render(spinner + " " + msg)

	lines := strings.Split(ASCIIBanner(m.animFrame), "\n")
	lines = append(lines, "")
	lines = append(lines, sub)
	return lines
}

func padRight(s string, width int) string {
	vw := lipgloss.Width(s)
	if vw >= width {
		return s
	}
	return s + strings.Repeat(" ", width-vw)
}
