package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

const (
	tileMinWidth            = 30
	tileMinHeight           = 7 // minimum content lines inside a tile
	tileGapH                = 2 // horizontal gap between tiles
	tileGapV                = 1 // vertical gap between tile rows
	tilePadH                = 1 // horizontal padding inside tile
	tileBorderV             = 2 // top + bottom border lines
	tileBorderH             = 2 // left + right border chars
	tileMaxColumns          = 3
	tileMinMultiColumnWidth = 62
	tableLabelMaxLenWide    = 26
	tableLabelMaxLenNarrow  = 24
)

func (m Model) tileGrid(contentW, contentH, n int) (cols, tileW, tileMaxHeight int) {
	if n == 0 {
		return 1, tileMinWidth, 0
	}

	if contentW <= 0 {
		contentW = tileMinWidth + tileBorderH + 2
	}

	usableW := contentW - 2
	maxCols := tileMaxColumns
	if n < maxCols {
		maxCols = n
	}

	// Evaluate all valid multi-column layouts and pick the most balanced one.
	// "Balanced" = fewest empty cells in the grid; ties broken by more columns.
	// Single column is a scrollable fallback used only when no multi-column fits.
	bestCols, bestW, bestH := 0, 0, 0
	bestEmpty := n + 1 // worse than any real candidate

	for c := 2; c <= maxCols; c++ {
		perCol := (usableW-(c-1)*tileGapH)/c - tileBorderH
		if perCol < tileMinWidth {
			continue
		}
		if perCol < tileMinMultiColumnWidth {
			continue
		}

		rows := (n + c - 1) / c
		usableH := contentH - (rows-1)*tileGapV
		if usableH <= tileBorderV {
			continue
		}
		perRowContentH := usableH/rows - tileBorderV
		if perRowContentH < tileMinHeight {
			continue
		}

		empty := rows*c - n
		if empty < bestEmpty || (empty == bestEmpty && c > bestCols) {
			bestCols, bestW, bestH, bestEmpty = c, perCol, perRowContentH, empty
		}
	}

	if bestCols > 0 {
		return bestCols, bestW, bestH
	}

	// Fallback: single scrollable column (no height cap).
	fallbackW := usableW - tileBorderH
	if fallbackW < tileMinWidth {
		fallbackW = tileMinWidth
	}
	return 1, fallbackW, 0
}

func (m Model) tileCols() int {
	switch m.activeDashboardView() {
	case dashboardViewStacked, dashboardViewTabs, dashboardViewSplit, dashboardViewCompare:
		return 1
	}

	n := len(m.filteredIDs())
	contentH := m.height - 3
	if contentH < 5 {
		contentH = 5
	}
	cols, _, _ := m.tileGrid(m.width, contentH, n)
	return cols
}

func tableLabelMaxLen(innerW int) int {
	if innerW < 60 {
		return tableLabelMaxLenNarrow
	}
	return tableLabelMaxLenWide
}

func (m Model) renderTiles(w, h int) string {
	return m.renderTilesWithColumns(w, h, 0)
}

func (m Model) renderTilesSingleColumn(w, h int) string {
	return m.renderTilesWithColumns(w, h, 1)
}

func (m Model) renderTilesWithColumns(w, h, forcedCols int) string {
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

	cols, tileW, tileMaxHeight := m.tileGrid(w, h, len(ids))
	if forcedCols == 1 {
		cols = 1
		tileMaxHeight = 0
		tileW = w - 2 - tileBorderH
		if tileW < tileMinWidth {
			tileW = tileMinWidth
		}
	}

	var tiles [][]string
	for i, id := range ids {
		snap := m.snapshots[id]
		selected := i == m.cursor
		modelMixExpanded := selected && m.expandedModelMixTiles[id]
		bodyOffset := 0
		if selected && m.activeDashboardView() == dashboardViewGrid && cols > 1 {
			bodyOffset = m.tileOffset
		}
		rendered := m.renderTile(snap, selected, modelMixExpanded, tileW, tileMaxHeight, bodyOffset)
		tiles = append(tiles, strings.Split(rendered, "\n"))
	}

	var rows []string
	var rowHeights []int
	gap := strings.Repeat("\n", tileGapV)

	for _, rowTiles := range lo.Chunk(tiles, cols) {
		for len(rowTiles) < cols {
			rowTiles = append(rowTiles, []string{strings.Repeat(" ", tileW+tileBorderH)})
		}

		maxLines := 0
		for _, tile := range rowTiles {
			if len(tile) > maxLines {
				maxLines = len(tile)
			}
		}
		if maxLines < tileMinHeight {
			maxLines = tileMinHeight
		}

		var padded []string
		for _, tile := range rowTiles {
			lines := append([]string(nil), tile...)
			for len(lines) < maxLines {
				lines = append(lines, strings.Repeat(" ", tileW+tileBorderH))
			}
			padded = append(padded, strings.Join(lines, "\n"))
		}

		row := lipgloss.JoinHorizontal(lipgloss.Top, intersperse(padded, strings.Repeat(" ", tileGapH))...)
		rows = append(rows, row)
		rowHeights = append(rowHeights, maxLines)
	}

	joined := strings.Join(rows, "\n"+gap)
	joinedLines := strings.Split(joined, "\n")
	for i, line := range joinedLines {
		joinedLines[i] = " " + line
	}
	content := strings.Join(joinedLines, "\n")

	contentLines := strings.Split(content, "\n")
	totalLines := len(contentLines)

	if totalLines <= h {
		return padToSize(content, w, h)
	}

	totalRows := len(rowHeights)
	rowOffsets := make([]int, totalRows)
	acc := 0
	for idx, cnt := range rowHeights {
		rowOffsets[idx] = acc
		acc += cnt
		if idx < totalRows-1 {
			acc += tileGapV
		}
	}

	cursorRow := m.cursor / cols
	if cursorRow >= totalRows {
		cursorRow = totalRows - 1
	}
	if cursorRow < 0 {
		cursorRow = 0
	}

	rowScrollOffset := 0
	if cols == 1 {
		rowScrollOffset = m.tileOffset
	}
	scrollLine := rowOffsets[cursorRow] + rowScrollOffset
	if scrollLine > totalLines-h {
		scrollLine = totalLines - h
	}
	if scrollLine < 0 {
		scrollLine = 0
	}

	endLine := scrollLine + h
	if endLine > totalLines {
		endLine = totalLines
	}

	visible := contentLines[scrollLine:endLine]

	if scrollLine > 0 {
		visible[0] = lipgloss.NewStyle().Foreground(colorDim).Render("  ▲ more above")
	}
	if bar := renderVerticalScrollBarLine(w, scrollLine, h, totalLines); bar != "" && len(visible) > 0 {
		visible[len(visible)-1] = bar
	} else if endLine < totalLines {
		visible[len(visible)-1] = lipgloss.NewStyle().Foreground(colorDim).Render("  ▼ more below")
	}

	return padToSize(strings.Join(visible, "\n"), w, h)
}

func (m Model) renderTilesTabs(w, h int) string {
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

	if h < 3 {
		return m.renderTilesSingleColumn(w, h)
	}

	tabsH := 2
	bodyH := h - tabsH
	if bodyH < 1 {
		bodyH = 1
	}

	var tabItems []string
	tabWidths := make([]int, 0, len(ids))
	for i, id := range ids {
		tabLabel := truncateToWidth(id, 20)
		tabText := " " + tabLabel + " "
		tabStyle := screenTabInactiveStyle
		if i == m.cursor {
			tabStyle = screenTabActiveStyle
		}
		rendered := tabStyle.Render(tabText)
		tabItems = append(tabItems, rendered)
		tabWidths = append(tabWidths, lipgloss.Width(rendered))
	}

	sepGap := " "
	tabsStrip := lipgloss.JoinHorizontal(lipgloss.Top, intersperse(tabItems, sepGap)...)

	tabStarts := make([]int, len(tabWidths))
	acc := 0
	for i, tw := range tabWidths {
		tabStarts[i] = acc
		acc += tw
		if i < len(tabWidths)-1 {
			acc += lipgloss.Width(sepGap)
		}
	}

	scrollX := 0
	active := clamp(m.cursor, 0, len(ids)-1)
	activeStart := tabStarts[active]
	activeEnd := activeStart + tabWidths[active]
	if activeEnd-scrollX > w {
		scrollX = activeEnd - w
	}
	if activeStart < scrollX {
		scrollX = activeStart
	}

	totalW := lipgloss.Width(tabsStrip)
	maxScroll := totalW - w
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollX > maxScroll {
		scrollX = maxScroll
	}

	visibleTabs := cropAnsiLine(tabsStrip, scrollX, w)
	window := m.renderWidgetPanelByIndex(m.cursor, w, bodyH, m.tileOffset, true)
	sep := renderHorizontalScrollBarLine(w, scrollX, w, totalW)
	if sep == "" && len(ids) > 1 {
		// Even when all tab labels fit, tabs view still supports horizontal pane
		// navigation; keep the affordance visible.
		sep = renderHorizontalScrollBarLine(w, clamp(m.cursor, 0, len(ids)-1), 1, len(ids))
	}
	if sep == "" {
		sep = lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w))
	}

	return padToSize(visibleTabs+"\n"+sep+"\n"+window, w, h)
}

func normalizeAnsiBlock(block string, width, height int) string {
	lines := strings.Split(block, "\n")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = cropAnsiLine(line, 0, width)
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func cropAnsiLine(line string, left, width int) string {
	if width <= 0 {
		return ""
	}
	if left < 0 {
		left = 0
	}
	cut := ansi.Cut(line, left, left+width)
	visualW := lipgloss.Width(cut)
	if visualW < width {
		cut += strings.Repeat(" ", width-visualW)
	}
	return cut
}

func (m Model) renderTile(snap core.UsageSnapshot, selected, modelMixExpanded bool, tileW, tileContentH, bodyOffset int) string {
	innerW := tileW - 2*tilePadH
	if innerW < 10 {
		innerW = 10
	}
	truncate := func(s string) string {
		if lipgloss.Width(s) > innerW {
			return s[:innerW-1] + "…"
		}
		return s
	}

	widget := dashboardWidget(snap.ProviderID)
	di := computeDisplayInfo(snap, widget)
	provColor := ProviderColor(snap.ProviderID)
	accentSep := lipgloss.NewStyle().Foreground(provColor).Render(strings.Repeat("━", innerW))
	dimSep := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", innerW))

	icon := StatusIcon(snap.Status)
	iconStr := lipgloss.NewStyle().Foreground(StatusColor(snap.Status)).Render(icon)
	nameStyle := tileNameStyle
	if selected {
		nameStyle = tileNameSelectedStyle
	}
	badge := StatusBadge(snap.Status)
	badgeW := lipgloss.Width(badge)

	// Time window pill for top-right corner (next to status badge).
	twPill := lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Render("⏱ " + m.timeWindow.Label())
	if m.refreshing {
		frame := m.animFrame % len(SpinnerFrames)
		twPill += " " + lipgloss.NewStyle().Foreground(colorAccent).Render(SpinnerFrames[frame])
	}
	twPillW := lipgloss.Width(twPill)
	rightW := twPillW + 1 + badgeW // pill + space + badge

	name := snap.AccountID
	maxName := innerW - rightW - 4
	if maxName < 5 {
		maxName = 5
	}
	if len(name) > maxName {
		name = name[:maxName-1] + "…"
	}
	hdrLeft := fmt.Sprintf("%s %s", iconStr, nameStyle.Render(name))
	gap := innerW - lipgloss.Width(hdrLeft) - rightW
	if gap < 1 {
		gap = 1
	}
	hdrLine1 := hdrLeft + strings.Repeat(" ", gap) + twPill + " " + badge

	var hdrLine2 string
	provID := snap.ProviderID
	if di.tagEmoji != "" && di.tagLabel != "" {
		tc := tagColor(di.tagLabel)
		tag := lipgloss.NewStyle().Foreground(tc).Bold(true).Render(di.tagEmoji + " " + di.tagLabel)
		maxProv := innerW - lipgloss.Width(tag) - 4
		if maxProv < 1 {
			maxProv = 1
		}
		if len(provID) > maxProv {
			provID = provID[:maxProv-1] + "…"
		}
		hdrLine2 = tag + " " + dimStyle.Render("· "+provID)
	} else {
		hdrLine2 = dimStyle.Render(truncate(provID))
	}
	headerMeta := buildTileHeaderMetaLines(snap, widget, innerW, m.animFrame)

	header := []string{hdrLine1, hdrLine2}
	if len(headerMeta) > 0 {
		header = append(header, headerMeta...)
	}
	header = append(header, accentSep)

	age := time.Since(snap.Timestamp)
	var timeStr string
	if age > 60*time.Second {
		timeStr = formatDuration(age) + " ago"
	} else if !snap.Timestamp.IsZero() {
		timeStr = snap.Timestamp.Format("15:04:05")
	}
	footerLine := tileTimestampStyle.Render(timeStr)
	footer := []string{dimSep, footerLine}

	bodyBudget := -1
	if tileContentH > 0 {
		bodyBudget = tileContentH - len(header) - len(footer)
		if bodyBudget < 0 {
			bodyBudget = 0
		}
	}

	renderWithBody := func(body []string) string {
		if bodyBudget >= 0 {
			for len(body) < bodyBudget {
				body = append(body, "")
			}
		}

		all := make([]string, 0, len(header)+len(body)+len(footer))
		all = append(all, header...)
		all = append(all, body...)
		all = append(all, footer...)

		content := strings.Join(all, "\n")

		border := tileBorderStyle.Width(tileW)
		if selected {
			border = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(provColor).
				Padding(0, tilePadH).
				Width(tileW)
		}
		return border.Render(content)
	}

	if m.tileShouldRenderLoading(snap) {
		return renderWithBody(m.buildTileLoadingBody(innerW, bodyBudget, snap))
	}
	fullBody := m.cachedTileBodyLines(snap, widget, di, innerW, modelMixExpanded)
	if len(headerMeta) == 0 {
		resetLines := buildTileResetLines(snap, widget, innerW, m.animFrame)
		if len(resetLines) > 0 {
			if len(fullBody) > 0 {
				fullBody = append(fullBody, "")
			}
			fullBody = append(fullBody, resetLines...)
		}
	}

	if bodyBudget < 0 {
		return renderWithBody(fullBody)
	}

	maxOffset := len(fullBody) - bodyBudget
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := clamp(bodyOffset, 0, maxOffset)

	body := fullBody
	if offset > 0 && offset < len(fullBody) {
		body = fullBody[offset:]
	}
	if bodyBudget >= 0 && len(body) > bodyBudget {
		body = body[:bodyBudget]
	}

	if bodyBudget > 0 && len(fullBody) > bodyBudget && len(body) > 0 {
		if offset > 0 {
			body[0] = dimStyle.Render("  ▲ more above")
		}
		if bar := renderVerticalScrollBarLine(innerW, offset, bodyBudget, len(fullBody)); bar != "" {
			body[len(body)-1] = bar
		} else if offset+bodyBudget < len(fullBody) {
			body[len(body)-1] = dimStyle.Render("  ▼ more below")
		}
	}

	return renderWithBody(body)
}

func buildEmptyTileSectionLines(sectionID core.DashboardStandardSection, widget core.DashboardWidget) []string {
	heading, message := emptyTileSectionContent(sectionID, widget)
	if heading == "" || message == "" {
		return nil
	}
	return []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(heading),
		dimStyle.Render("  " + message),
	}
}

func emptyTileSectionContent(sectionID core.DashboardStandardSection, widget core.DashboardWidget) (heading string, message string) {
	switch sectionID {
	case core.DashboardSectionTopUsageProgress:
		return "Usage", "No usage data for this time range"
	case core.DashboardSectionModelBurn:
		return "Model Burn", "No model data for this time range"
	case core.DashboardSectionClientBurn:
		heading = "Client Burn"
		if strings.TrimSpace(widget.ClientCompositionHeading) != "" {
			heading = strings.TrimSpace(widget.ClientCompositionHeading)
		}
		return heading, "No client data for this time range"
	case core.DashboardSectionProjectBreakdown:
		return "Project Breakdown", "No project data for this time range"
	case core.DashboardSectionToolUsage:
		heading = "Tool Usage"
		if strings.TrimSpace(widget.ToolCompositionHeading) != "" {
			heading = strings.TrimSpace(widget.ToolCompositionHeading)
		}
		return heading, "No tool data for this time range"
	case core.DashboardSectionMCPUsage:
		return "MCP Usage", "No MCP data for this time range"
	case core.DashboardSectionLanguageBurn:
		return "Language", "No language data for this time range"
	case core.DashboardSectionCodeStats:
		return "Code Statistics", "No code stats for this time range"
	case core.DashboardSectionDailyUsage:
		return "Daily Usage", "No daily usage data for this time range"
	case core.DashboardSectionProviderBurn:
		return "Provider Burn", "No provider data for this time range"
	case core.DashboardSectionUpstreamProviders:
		return "Hosting Providers", "No hosting provider data for this time range"
	case core.DashboardSectionOtherData:
		return "Other Data", "No additional data for this time range"
	default:
		return "", ""
	}
}

func (m Model) tileShouldRenderLoading(snap core.UsageSnapshot) bool {
	switch snap.Status {
	case core.StatusError, core.StatusAuth, core.StatusLimited:
		return false
	}
	if len(snap.Metrics) > 0 || len(snap.ModelUsage) > 0 || len(snap.DailySeries) > 0 || len(snap.Resets) > 0 {
		return false
	}
	return true
}

func (m Model) buildTileLoadingBody(innerW, bodyBudget int, snap core.UsageSnapshot) []string {
	center := func(line string) string {
		lineW := lipgloss.Width(line)
		if lineW >= innerW {
			return line
		}
		pad := (innerW - lineW) / 2
		if pad < 0 {
			pad = 0
		}
		return strings.Repeat(" ", pad) + line
	}

	lines := m.brandedLoaderLines(innerW, snap.Message, "Syncing telemetry...")
	for i := range lines {
		lines[i] = center(lines[i])
	}
	if bodyBudget > len(lines) {
		padTop := (bodyBudget - len(lines)) / 2
		if padTop > 0 {
			padded := make([]string, 0, len(lines)+padTop)
			for i := 0; i < padTop; i++ {
				padded = append(padded, "")
			}
			padded = append(padded, lines...)
			lines = padded
		}
	}

	if bodyBudget > 0 && len(lines) > bodyBudget {
		lines = lines[:bodyBudget]
	}
	return lines
}
