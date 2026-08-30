package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func providerThemeColor(providerID string) lipgloss.Color {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "antigravity":
		return colorMauve
	case "opencode":
		return colorBlue
	case "cursor":
		return colorLavender
	case "claude_code", "anthropic":
		return colorPeach
	case "copilot", "github-copilot":
		return colorGreen
	case "command_code", "cmdc":
		return colorTeal
	case "gemini_cli", "google":
		return colorSapphire
	default:
		return colorAccent
	}
}

type listItemHeightInfo struct {
	hasHeader bool
	height    int
}

func listVisibleWindow(snapshots map[string]core.UsageSnapshot, ids []string, cursor, h int) (int, int) {
	if len(ids) == 0 {
		return 0, 0
	}
	if h <= 0 {
		return 0, len(ids)
	}

	cursor = clamp(cursor, 0, len(ids)-1)
	infos := make([]listItemHeightInfo, len(ids))
	for i, id := range ids {
		snap := snapshots[id]
		pID := snap.ProviderID
		hasHeader := (i == 0) || (i > 0 && snapshots[ids[i-1]].ProviderID != pID)
		ht := 3
		if hasHeader {
			ht = 4
		}
		infos[i] = listItemHeightInfo{hasHeader: hasHeader, height: ht}
	}

	calcTotal := func(s, e int) int {
		linesCount := 0
		if s > 0 {
			linesCount++
		}
		for idx := s; idx < e; idx++ {
			linesCount += infos[idx].height
		}
		if e < len(ids) {
			linesCount++
		}
		return linesCount
	}

	totalAll := calcTotal(0, len(ids))
	if totalAll <= h {
		return 0, len(ids)
	}

	start := cursor
	end := cursor + 1

	for {
		expanded := false
		if start > 0 {
			if calcTotal(start-1, end) <= h {
				start--
				expanded = true
			}
		}
		if end < len(ids) {
			if calcTotal(start, end+1) <= h {
				end++
				expanded = true
			}
		}
		if !expanded {
			break
		}
	}

	return start, end
}

func (m Model) renderList(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 {
		empty := []string{
			"",
			dimStyle.Render("  Loading providers…"),
			"",
			labelStyle.Render("  Fetching usage and spend data."),
		}
		return padToSize(strings.Join(empty, "\n"), w, h)
	}

	selectedProvider := ""
	if m.cursor >= 0 && m.cursor < len(ids) {
		if curSnap, ok := m.snapshots[ids[m.cursor]]; ok {
			selectedProvider = curSnap.ProviderID
		}
	}

	providerCounts := make(map[string]int)
	for _, id := range ids {
		if snap, ok := m.snapshots[id]; ok {
			providerCounts[snap.ProviderID]++
		}
	}

	start, end := listVisibleWindow(m.snapshots, ids, m.cursor, h)

	var lines []string
	if start > 0 {
		lines = append(lines, dimStyle.Render("  ▲ "+fmt.Sprintf("%d more", start)))
	}

	for i := start; i < end; i++ {
		snap, ok := m.snapshots[ids[i]]
		if !ok {
			continue
		}
		pID := snap.ProviderID
		pColor := providerThemeColor(pID)
		isGroupActive := (pID == selectedProvider)
		hasMultiple := providerCounts[pID] > 1

		// Group header when entering a provider category
		isFirstInGroup := (i == 0) || (i > 0 && m.snapshots[ids[i-1]].ProviderID != pID)
		if isFirstInGroup {
			var headerStr string
			groupName := strings.ToUpper(pID)
			countStr := fmt.Sprintf("(%d)", providerCounts[pID])
			if isGroupActive {
				prefix := lipgloss.NewStyle().Bold(true).Foreground(pColor).Render("✦ ")
				title := lipgloss.NewStyle().Bold(true).Foreground(pColor).Render(groupName)
				count := lipgloss.NewStyle().Foreground(pColor).Render(" " + countStr)
				headerStr = " " + prefix + title + count
			} else {
				headerStr = " " + dimStyle.Render("◈ "+groupName+" "+countStr)
			}
			lines = append(lines, headerStr)
		}

		lines = append(lines, m.renderListItemWithGroup(snap, i == m.cursor, isGroupActive && hasMultiple, pColor, w))
	}

	if end < len(ids) {
		lines = append(lines, dimStyle.Render("  ▼ "+fmt.Sprintf("%d more", len(ids)-end)))
	}

	content := strings.Join(lines, "\n")
	out := padToSize(content, w, h)
	if len(ids) > (end-start) && h > 0 {
		rendered := strings.Split(out, "\n")
		if len(rendered) > 0 {
			rendered[len(rendered)-1] = renderVerticalScrollBarLine(w, start, end-start, len(ids))
			out = strings.Join(rendered, "\n")
		}
	}
	return out
}

func (m Model) renderSplitPanes(w, h int) string {
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
	if leftW < 10 {
		leftW = w / 2
	}
	rightW := w - leftW - 1
	if rightW < 10 {
		rightW = 10
	}

	left := m.renderList(leftW, h)
	right := m.renderDetailPanel(rightW, h)
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
	pColor := providerThemeColor(snap.ProviderID)
	return m.renderListItemWithGroup(snap, selected, false, pColor, w)
}

func (m Model) renderListItemWithGroup(snap core.UsageSnapshot, selected bool, inActiveGroup bool, pColor lipgloss.Color, w int) string {
	di := computeDisplayInfo(snap, dashboardWidget(snap.ProviderID), m.resolveHideCosts(snap), m.usageMode)

	iconStr := lipgloss.NewStyle().Foreground(StatusColor(core.EffectiveStatus(snap))).Render(StatusIcon(core.EffectiveStatus(snap)))
	nameStyle := lipgloss.NewStyle().Foreground(colorText)
	if selected {
		nameStyle = nameStyle.Bold(true).Foreground(pColor)
	} else if inActiveGroup {
		nameStyle = nameStyle.Foreground(colorText)
	}

	badge := SnapshotStatusBadge(snap)
	rightPart := badge
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

	summaryLine := m.renderListSummaryRow(snap, di, w)

	sepStyle := surface1Style
	if inActiveGroup || selected {
		sepStyle = lipgloss.NewStyle().Foreground(pColor)
	}

	result := line1 + "\n" +
		summaryLine + "\n" +
		"  " + sepStyle.Render(strings.Repeat("─", w-4))

	var indicator string
	if selected {
		indicator = lipgloss.NewStyle().Bold(true).Foreground(pColor).Render("┃")
	} else if inActiveGroup {
		indicator = lipgloss.NewStyle().Foreground(pColor).Render("│")
	}

	if indicator == "" {
		return result
	}

	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if len(line) > 0 {
			lines[i] = indicator + line[1:]
		}
	}
	return strings.Join(lines, "\n")
}

const sidebarSummaryMinWidth = 7 // room for e.g. "93.00%"

func (m Model) renderListSummaryRow(snap core.UsageSnapshot, di providerDisplayInfo, w int) string {
	now := m.viewNow()
	entries := collectCycleResetEntries(snap)

	stripW := 0
	if di.gaugePercent >= 0 {
		stripW = compactBlockStripWidth + 2
	}

	summary := di.summary
	summaryMaxW := w - 5 - stripW - 1
	if summaryMaxW < sidebarSummaryMinWidth {
		summaryMaxW = sidebarSummaryMinWidth
	}
	if len(summary) > summaryMaxW {
		summary = summary[:summaryMaxW-1] + "…"
	}

	left := m.renderListSummary(summary, di.gaugePercent, snap)
	leftPart := "   " + left
	if len(entries) == 0 {
		return leftPart
	}

	d := entries[0].at.Sub(now)
	dur := formatCycleResetDuration(d)
	for _, candidate := range []string{
		formatCycleResetIn(entries[0].at, now),
		"Resets in " + dur,
		"in " + dur,
		dur,
	} {
		if candidate == "" {
			continue
		}
		suffix := dimStyle.Render(" · ") + tileCycleResetStyle.Render(candidate)
		combined := leftPart + suffix
		if lipgloss.Width(combined) <= w-1 {
			return combined
		}
	}

	return leftPart
}

func (m Model) renderListSummary(summary string, gaugePercent float64, snap core.UsageSnapshot) string {
	color := listSummaryColor(
		gaugePercent,
		m.isUsageModeUsed(),
		m.warnThreshold,
		m.critThreshold,
		core.EffectiveStatus(snap),
	)
	style := lipgloss.NewStyle().Bold(true).Foreground(color)
	if gaugePercent < 0 {
		return style.Render(summary)
	}
	strip := RenderCompactBlockStrip(gaugePercent, compactBlockStripWidth, color)
	return strip + "  " + style.Render(summary)
}

// listSummaryColor picks a traffic-light color for sidebar summary text.
// When no gauge metric is available, status drives the tint instead.
func listSummaryColor(gaugePercent float64, isUsedMode bool, warnThresh, critThresh float64, status core.Status) lipgloss.Color {
	if gaugePercent >= 0 {
		if isUsedMode {
			return usageGaugeColor(gaugePercent, warnThresh, critThresh)
		}
		return gaugeColor(gaugePercent, warnThresh, critThresh)
	}
	switch status {
	case core.StatusLimited:
		return colorPeach
	case core.StatusError:
		return colorCrit
	case core.StatusAuth:
		return colorYellow
	default:
		return colorText
	}
}

func (m Model) renderDetailPanel(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 || m.cursor >= len(ids) {
		return padToSize("", w, h)
	}

	snap := m.snapshots[ids[m.cursor]]
	activeTab := clamp(m.detailTab, 0, len(DetailTabs(snap))-1)
	content := m.cachedDetailContent(ids[m.cursor], snap, w-2, activeTab)
	if m.refreshing {
		content = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("  "+m.renderFetchingStatus()) + "\n" + content
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	offset := clamp(m.detailOffset, 0, max(0, totalLines-h))
	end := min(offset+h, totalLines)
	visible := append([]string(nil), lines[offset:end]...)
	for len(visible) < h {
		visible = append(visible, "")
	}

	result := strings.Join(visible, "\n")
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

	return lipgloss.NewStyle().Width(w).Padding(0, 1).Render(result)
}

func renderVerticalSep(h int) string {
	lines := make([]string, h)
	for i := range lines {
		lines[i] = surface1Style.Render("┃")
	}
	return strings.Join(lines, "\n")
}
