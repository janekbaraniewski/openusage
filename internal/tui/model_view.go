package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func (m Model) View() string {
	if m.width < 30 || m.height < 8 {
		return lipgloss.NewStyle().
			Foreground(colorDim).
			Render("\n  Terminal too small. Resize to at least 30×8.")
	}
	if !m.hasData {
		return m.renderSplash(m.width, m.height)
	}
	if m.showHelp {
		return m.renderHelpOverlay(m.width, m.height)
	}
	view := m.renderDashboard()
	if m.settings.show {
		return m.renderSettingsModalOverlay()
	}
	return view
}

func (m Model) renderDashboardContent(w, contentH int) string {
	if m.mode == modeDetail {
		return m.renderDetailPanel(w, contentH)
	}
	switch m.activeDashboardView() {
	case dashboardViewTabs:
		return m.renderTilesTabs(w, contentH)
	case dashboardViewSplit:
		return m.renderSplitPanes(w, contentH)
	case dashboardViewCompare:
		return m.renderComparePanes(w, contentH)
	case dashboardViewStacked:
		return m.renderTilesSingleColumn(w, contentH)
	default:
		return m.renderTiles(w, contentH)
	}
}

func (m Model) renderHeader(w int) string {
	bolt := PulseChar(
		accentBoldStyle.Render("⚡"),
		lipgloss.NewStyle().Foreground(colorDim).Bold(true).Render("⚡"),
		m.animFrame,
	)
	brandText := RenderGradientText("OpenUsage", m.animFrame)

	tabs := m.renderScreenTabs()

	spinnerStr := ""
	if m.refreshing {
		frame := m.animFrame % len(SpinnerFrames)
		spinnerStr = " " + lipgloss.NewStyle().Foreground(colorAccent).Render(SpinnerFrames[frame])
	}

	ids := m.filteredIDs()
	unmappedProviders := m.telemetryUnmappedProviders()

	okCount, warnCount, errCount := 0, 0, 0
	for _, id := range ids {
		snap, ok := m.snapshots[id]
		if !ok {
			continue
		}
		switch snap.Status {
		case core.StatusOK:
			okCount++
		case core.StatusNearLimit:
			warnCount++
		case core.StatusLimited, core.StatusError:
			errCount++
		}
	}

	var info string

	if m.settings.show {
		info = m.settingsModalInfo()
	} else {
		switch m.screen {
		case screenAnalytics:
			tabLabel := "analytics"
			if m.analyticsTab >= 0 && m.analyticsTab < len(analyticsTabLabels) {
				tabLabel = strings.ToLower(analyticsTabLabels[m.analyticsTab])
			}
			info = dimStyle.Render(tabLabel)
			if m.analyticsFilter.text != "" {
				info += " (filtered)"
			}
		default:
			info = fmt.Sprintf("⊞ %d providers", len(ids))
			if m.filter.text != "" {
				info += " (filtered)"
			}
			info += " · " + m.dashboardViewStatusLabel()
		}
	}
	if !m.settings.show {
		info += " · " + m.timeWindow.Label()
	}
	if !m.settings.show && len(unmappedProviders) > 0 {
		info += " · detected additional providers, check settings"
	}

	statusInfo := ""
	if okCount > 0 {
		dot := PulseChar("●", "◉", m.animFrame)
		statusInfo += greenStyle.Render(fmt.Sprintf(" %d%s", okCount, dot))
	}
	if warnCount > 0 {
		dot := PulseChar("◐", "◑", m.animFrame)
		statusInfo += yellowStyle.Render(fmt.Sprintf(" %d%s", warnCount, dot))
	}
	if errCount > 0 {
		dot := PulseChar("✗", "✕", m.animFrame)
		statusInfo += redStyle.Render(fmt.Sprintf(" %d%s", errCount, dot))
	}
	if len(unmappedProviders) > 0 {
		statusInfo += lipgloss.NewStyle().
			Foreground(colorPeach).
			Render(fmt.Sprintf(" ⚠ %d unmapped", len(unmappedProviders)))
	}

	infoRendered := labelStyle.Render(info)

	left := bolt + " " + brandText + " " + tabs + statusInfo + spinnerStr
	gap := w - lipgloss.Width(left) - lipgloss.Width(infoRendered)
	if gap < 1 {
		gap = 1
	}

	line := left + strings.Repeat(" ", gap) + infoRendered
	return line + "\n" + m.renderGradientSeparator(w)
}

func (m Model) renderGradientSeparator(w int) string {
	if w <= 0 {
		return ""
	}
	return surface1Style.Render(strings.Repeat("━", w))
}

func (m Model) renderScreenTabs() string {
	screens := m.availableScreens()
	if len(screens) <= 1 {
		return ""
	}
	var parts []string
	for i, screen := range screens {
		label := screenLabelByTab[screen]
		tabStr := fmt.Sprintf("%d:%s", i+1, label)
		if screen == m.screen {
			parts = append(parts, screenTabActiveStyle.Render(tabStr))
		} else {
			parts = append(parts, screenTabInactiveStyle.Render(tabStr))
		}
	}
	return strings.Join(parts, "")
}

func (m Model) renderFooter(w int) string {
	sep := surface1Style.Render(strings.Repeat("━", w))
	statusLine := m.renderFooterStatusLine(w)
	return sep + "\n" + statusLine
}

func (m Model) renderFooterStatusLine(w int) string {
	searchStyle := sapphireStyle

	switch {
	case m.settings.show:
		if m.settings.status != "" {
			return " " + dimStyle.Render(m.settings.status)
		}
		return " " + helpStyle.Render("? help")
	case m.screen == screenAnalytics:
		if m.analyticsFilter.active {
			cursor := PulseChar("█", "▌", m.animFrame)
			return " " + dimStyle.Render("search: ") + searchStyle.Render(m.analyticsFilter.text+cursor)
		}
		if m.analyticsFilter.text != "" {
			return " " + dimStyle.Render("filter: ") + searchStyle.Render(m.analyticsFilter.text)
		}
		if m.analyticsTab == analyticsTabModels {
			return " " + dimStyle.Render("j/k navigate · Enter expand/collapse · [ ] tabs · s sort · / filter · r refresh")
		}
		return " " + dimStyle.Render("j/k scroll · [ ] tabs · 1-4 jump · s sort · / filter · r refresh")
	default:
		if m.mode == modeDetail && m.screen == screenDashboard {
			zoomHint := "+/- zoom"
			if m.detailChartZoom > 0 {
				zoomLabels := []string{"", "90d", "30d", "14d", "7d", "3d"}
				zoomHint = fmt.Sprintf("+/- zoom (%s)", zoomLabels[m.detailChartZoom])
			}
			hint := fmt.Sprintf("j/k scroll · PgUp/PgDn page · %s · Ctrl+wheel zoom · 0 reset · r refresh · Esc back", zoomHint)
			return " " + dimStyle.Render(hint)
		}
		if m.filter.active {
			cursor := PulseChar("█", "▌", m.animFrame)
			return " " + dimStyle.Render("search: ") + searchStyle.Render(m.filter.text+cursor)
		}
		if m.filter.text != "" {
			return " " + dimStyle.Render("filter: ") + searchStyle.Render(m.filter.text)
		}
		if m.activeDashboardView() == dashboardViewTabs && m.mode == modeList {
			return " " + dimStyle.Render("tabs view · ←/→ switch tab · PgUp/PgDn scroll widget · Enter detail")
		}
		if m.activeDashboardView() == dashboardViewSplit && m.mode == modeList {
			return " " + dimStyle.Render("split view · ↑/↓ select provider · PgUp/PgDn scroll pane · Enter detail")
		}
		if m.activeDashboardView() == dashboardViewCompare && m.mode == modeList {
			return " " + dimStyle.Render("compare view · ←/→ switch provider · PgUp/PgDn scroll active pane")
		}
		if m.mode == modeList && m.shouldUseWidgetScroll() && m.tileOffset > 0 {
			return " " + dimStyle.Render("widget scroll active · PgUp/PgDn · Ctrl+U/Ctrl+D")
		}
		if m.mode == modeList && m.shouldUsePanelScroll() && m.tileOffset > 0 {
			return " " + dimStyle.Render("panel scroll active · PgUp/PgDn · Home/End")
		}
	}

	if m.hasAppUpdateNotice() {
		msg := "Update available: " + m.daemon.appUpdateCurrent + " -> " + m.daemon.appUpdateLatest
		if action := m.appUpdateAction(); action != "" {
			msg += " · " + action
		}
		if w > 2 {
			msg = truncateToWidth(msg, w-2)
		}
		return " " + yellowStyle.Render(msg)
	}

	return " " + helpStyle.Render("? help")
}

func (m Model) hasAppUpdateNotice() bool {
	return strings.TrimSpace(m.daemon.appUpdateCurrent) != "" && strings.TrimSpace(m.daemon.appUpdateLatest) != ""
}

func (m Model) appUpdateHeadline() string {
	if !m.hasAppUpdateNotice() {
		return ""
	}
	return "OpenUsage update available: " + m.daemon.appUpdateCurrent + " -> " + m.daemon.appUpdateLatest
}

func (m Model) appUpdateAction() string {
	hint := strings.TrimSpace(m.daemon.appUpdateHint)
	if hint == "" {
		return ""
	}
	return "Run: " + hint
}
