package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func (m Model) renderList(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 {
		empty := []string{
			"",
			dimStyle.Render("  Loading providers…"),
			"",
			lipgloss.NewStyle().Foreground(colorSubtext).Render("  Fetching usage and spend data."),
		}
		return padToSize(strings.Join(empty, "\n"), w, h)
	}

	itemHeight := 3
	visibleItems := h / itemHeight
	if visibleItems < 1 {
		visibleItems = 1
	}

	scrollStart := 0
	if m.cursor >= visibleItems {
		scrollStart = m.cursor - visibleItems + 1
	}
	scrollEnd := scrollStart + visibleItems
	if scrollEnd > len(ids) {
		scrollEnd = len(ids)
		scrollStart = scrollEnd - visibleItems
		if scrollStart < 0 {
			scrollStart = 0
		}
	}

	var lines []string
	for i := scrollStart; i < scrollEnd; i++ {
		snap := m.snapshots[ids[i]]
		lines = append(lines, m.renderListItem(snap, i == m.cursor, w))
	}

	if scrollStart > 0 {
		lines = append([]string{lipgloss.NewStyle().Foreground(colorDim).Render("  ▲ " + fmt.Sprintf("%d more", scrollStart))}, lines...)
	}
	if scrollEnd < len(ids) {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Render("  ▼ "+fmt.Sprintf("%d more", len(ids)-scrollEnd)))
	}

	content := strings.Join(lines, "\n")
	out := padToSize(content, w, h)
	if len(ids) > visibleItems && h > 0 {
		rendered := strings.Split(out, "\n")
		if len(rendered) > 0 {
			rendered[len(rendered)-1] = renderVerticalScrollBarLine(w, scrollStart, visibleItems, len(ids))
			out = strings.Join(rendered, "\n")
		}
	}
	return out
}

func (m Model) renderSplitPanes(w, h int) string {
	if w < 70 {
		return m.renderTilesTabs(w, h)
	}

	leftW := w / 3
	if leftW < minLeftWidth {
		leftW = minLeftWidth
	}
	if leftW > maxLeftWidth {
		leftW = maxLeftWidth
	}
	if leftW > w-34 {
		leftW = w - 34
	}
	if leftW < minLeftWidth || w-leftW-1 < 30 {
		return m.renderTilesTabs(w, h)
	}

	left := m.renderList(leftW, h)
	rightW := w - leftW - 1
	right := m.renderWidgetPanelByIndex(m.cursor, rightW, h, m.tileOffset, true)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, renderVerticalSep(h), right)
}

func (m Model) renderComparePanes(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 {
		return m.renderTiles(w, h)
	}
	if len(ids) == 1 || w < 72 {
		return m.renderWidgetPanelByIndex(m.cursor, w, h, m.tileOffset, true)
	}

	gapW := tileGapH
	colW := (w - gapW) / 2
	if colW < 30 {
		return m.renderWidgetPanelByIndex(m.cursor, w, h, m.tileOffset, true)
	}

	primary := clamp(m.cursor, 0, len(ids)-1)
	secondary := primary + 1
	if secondary >= len(ids) {
		secondary = primary - 1
	}
	if secondary < 0 {
		secondary = primary
	}

	left := m.renderWidgetPanelByIndex(primary, colW, h, m.tileOffset, true)
	right := m.renderWidgetPanelByIndex(secondary, colW, h, 0, false)
	return padToSize(lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gapW), right), w, h)
}

func (m Model) renderWidgetPanelByIndex(index, w, h, bodyOffset int, selected bool) string {
	ids := m.filteredIDs()
	if len(ids) == 0 || index < 0 || index >= len(ids) {
		return padToSize("", w, h)
	}

	id := ids[index]
	snap := m.snapshots[id]
	modelMixExpanded := index == m.cursor && m.expandedModelMixTiles[id]

	tileW := w - 2 - tileBorderH
	if tileW < tileMinWidth {
		tileW = tileMinWidth
	}
	contentH := h - tileBorderV
	if contentH < tileMinHeight {
		contentH = tileMinHeight
	}

	rendered := m.renderTile(snap, selected, modelMixExpanded, tileW, contentH, bodyOffset)
	return normalizeAnsiBlock(rendered, w, h)
}

func (m Model) renderListItem(snap core.UsageSnapshot, selected bool, w int) string {
	di := computeDisplayInfo(snap, dashboardWidget(snap.ProviderID))

	iconStr := lipgloss.NewStyle().Foreground(StatusColor(snap.Status)).Render(StatusIcon(snap.Status))
	nameStyle := lipgloss.NewStyle().Foreground(colorText)
	if selected {
		nameStyle = nameStyle.Bold(true).Foreground(colorLavender)
	}

	badge := StatusBadge(snap.Status)
	tagRendered := ""
	if di.tagEmoji != "" && di.tagLabel != "" {
		tagRendered = lipgloss.NewStyle().Foreground(tagColor(di.tagLabel)).Render(di.tagEmoji+" "+di.tagLabel) + " "
	}
	rightPart := tagRendered + badge
	rightW := lipgloss.Width(rightPart)

	name := snap.AccountID
	maxName := w - rightW - 6
	if maxName < 5 {
		maxName = 5
	}
	if len(name) > maxName {
		name = name[:maxName-1] + "…"
	}

	namePart := fmt.Sprintf(" %s %s", iconStr, nameStyle.Render(name))
	gapLen := w - lipgloss.Width(namePart) - rightW - 1
	if gapLen < 1 {
		gapLen = 1
	}
	line1 := namePart + strings.Repeat(" ", gapLen) + rightPart

	summary := di.summary
	miniGauge := ""
	if di.gaugePercent >= 0 && w > 25 {
		gaugeW := 8
		if w < 35 {
			gaugeW = 5
		}
		miniGauge = " " + RenderMiniGauge(di.gaugePercent, gaugeW)
	}
	summaryMaxW := w - 5 - lipgloss.Width(miniGauge)
	if summaryMaxW < 5 {
		summaryMaxW = 5
	}
	if len(summary) > summaryMaxW {
		summary = summary[:summaryMaxW-1] + "…"
	}

	result := line1 + "\n" +
		"   " + lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(summary) + miniGauge + "\n" +
		"  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4))

	if !selected {
		return result
	}

	indicator := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("┃")
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if len(line) > 0 {
			lines[i] = indicator + line[1:]
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderDetailPanel(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 || m.cursor >= len(ids) {
		return padToSize("", w, h)
	}

	snap := m.snapshots[ids[m.cursor]]
	activeTab := clamp(m.detailTab, 0, len(DetailTabs(snap))-1)
	content := m.cachedDetailContent(ids[m.cursor], snap, w-2, activeTab)

	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	offset := clamp(m.detailOffset, 0, max(0, totalLines-h))
	end := min(offset+h, totalLines)
	visible := append([]string(nil), lines[offset:end]...)
	for len(visible) < h {
		visible = append(visible, "")
	}

	result := strings.Join(visible, "\n")
	if m.mode == modeDetail {
		rendered := strings.Split(result, "\n")
		if offset > 0 && len(rendered) > 0 {
			rendered[0] = lipgloss.NewStyle().Foreground(colorAccent).Render("  ▲ scroll up")
		}
		if len(rendered) > 1 {
			if bar := renderVerticalScrollBarLine(w-2, offset, h, totalLines); bar != "" {
				rendered[len(rendered)-1] = bar
			} else if end < totalLines {
				rendered[len(rendered)-1] = lipgloss.NewStyle().Foreground(colorAccent).Render("  ▼ more below")
			}
		}
		result = strings.Join(rendered, "\n")
	}

	return lipgloss.NewStyle().Width(w).Padding(0, 1).Render(result)
}

func renderVerticalSep(h int) string {
	style := lipgloss.NewStyle().Foreground(colorSurface1)
	lines := make([]string, h)
	for i := range lines {
		lines[i] = style.Render("┃")
	}
	return strings.Join(lines, "\n")
}
