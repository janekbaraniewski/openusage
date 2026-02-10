package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/agentusage/internal/core"
)

// ─── Tile Constants ─────────────────────────────────────────────────────────

const (
	tileMinWidth     = 30
	tileMinHeight    = 7 // minimum content lines inside a tile
	tileDefaultLines = 7 // default content lines (the 7-line layout)
	tileGapH         = 2 // horizontal gap between tiles
	tileGapV         = 1 // vertical gap between tile rows
	tilePadH         = 1 // horizontal padding inside tile
	tileBorderV      = 2 // top + bottom border lines
	tileBorderH      = 2 // left + right border chars
)

// Tile styles are defined in styles.go for theme support.

// ─── Grid Computation ───────────────────────────────────────────────────────

// tileGrid computes the optimal (cols, tileW, tileH) for the given content area
// and number of provider tiles. It tries every viable column count and picks
// the arrangement that best fills the screen (maximises min(fillW, fillH)).
func (m Model) tileGrid(contentW, contentH, n int) (cols, tileW, tileContentH int) {
	if n == 0 {
		return 1, tileMinWidth, tileDefaultLines
	}

	bestCols := 1
	bestScore := -1.0

	for c := 1; c <= n; c++ {
		rows := (n + c - 1) / c

		// Width each tile would get
		usableW := contentW - 2 - (c-1)*tileGapH // 2 = outer padding
		tw := usableW/c - tileBorderH
		if tw < tileMinWidth {
			break // can't fit this many columns
		}

		// Height each tile would get
		usableH := contentH - (rows-1)*tileGapV
		th := usableH/rows - tileBorderV // content lines inside tile
		if th < tileMinHeight {
			continue // tiles would be too short
		}

		// Score: how well does this arrangement fill the available area?
		// We want both dimensions well-utilised.
		totalTileW := c*(tw+tileBorderH) + (c-1)*tileGapH + 2
		totalTileH := rows*(th+tileBorderV) + (rows-1)*tileGapV
		fillW := float64(totalTileW) / float64(contentW)
		fillH := float64(totalTileH) / float64(contentH)
		if fillW > 1.0 {
			fillW = 1.0
		}
		if fillH > 1.0 {
			fillH = 1.0
		}
		// Geometric mean favours balanced fill across both axes
		score := fillW * fillH

		if score > bestScore {
			bestScore = score
			bestCols = c
		}
	}

	// Compute final tile dimensions for the chosen column count
	rows := (n + bestCols - 1) / bestCols
	usableW := contentW - 2 - (bestCols-1)*tileGapH
	tileW = usableW/bestCols - tileBorderH
	if tileW < tileMinWidth {
		tileW = tileMinWidth
	}

	usableH := contentH - (rows-1)*tileGapV
	tileContentH = usableH/rows - tileBorderV
	if tileContentH < tileMinHeight {
		tileContentH = tileMinHeight
	}

	return bestCols, tileW, tileContentH
}

// tileCols returns how many tile columns fit (quick accessor for navigation).
func (m Model) tileCols() int {
	n := len(m.filteredIDs())
	// Estimate content height (header=2 + footer=1 = 3 lines overhead)
	contentH := m.height - 3
	if contentH < 5 {
		contentH = 5
	}
	cols, _, _ := m.tileGrid(m.width, contentH, n)
	return cols
}

// ─── Tiles Renderer ─────────────────────────────────────────────────────────

func (m Model) renderTiles(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 {
		empty := []string{
			"",
			dimStyle.Render("  No providers detected."),
			"",
			lipgloss.NewStyle().Foreground(colorSubtext).Render("  Set API-key env vars or install AI tools."),
		}
		return padToSize(strings.Join(empty, "\n"), w, h)
	}

	cols, tileW, tileContentH := m.tileGrid(w, h, len(ids))

	// Render all tiles with dynamic dimensions
	var tiles []string
	for i, id := range ids {
		snap := m.snapshots[id]
		selected := i == m.cursor
		tiles = append(tiles, m.renderTile(snap, selected, tileW, tileContentH))
	}

	// Arrange tiles into rows
	var rows []string
	for i := 0; i < len(tiles); i += cols {
		end := i + cols
		if end > len(tiles) {
			end = len(tiles)
		}
		rowTiles := tiles[i:end]

		// Pad row to full column count with empty spacers for alignment
		for len(rowTiles) < cols {
			spacer := strings.Repeat(" ", tileW+tileBorderH)
			rowTiles = append(rowTiles, spacer)
		}

		row := lipgloss.JoinHorizontal(lipgloss.Top, intersperse(rowTiles, strings.Repeat(" ", tileGapH))...)
		rows = append(rows, row)
	}

	// Join rows with vertical gap
	gap := strings.Repeat("\n", tileGapV)
	joined := strings.Join(rows, "\n"+gap)
	// Prepend a small left margin to every line so the tiles are inset
	joinedLines := strings.Split(joined, "\n")
	for i, line := range joinedLines {
		joinedLines[i] = " " + line
	}
	content := strings.Join(joinedLines, "\n")

	// Scrolling: if content exceeds height, scroll based on cursor row
	contentLines := strings.Split(content, "\n")
	totalLines := len(contentLines)

	if totalLines <= h {
		return padToSize(content, w, h)
	}

	// Determine which row the cursor is on
	cursorRow := m.cursor / cols
	totalRows := (len(ids) + cols - 1) / cols
	rowHeight := tileContentH + tileBorderV + tileGapV

	// Scroll offset: keep cursor row visible
	scrollLine := cursorRow * rowHeight
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

	// Add scroll indicators
	if scrollLine > 0 {
		visible[0] = lipgloss.NewStyle().Foreground(colorDim).Render(
			fmt.Sprintf("  ▲ %d more row(s) above", cursorRow))
	}
	if endLine < totalLines {
		remainingRows := totalRows - cursorRow - 1
		if remainingRows < 1 {
			remainingRows = 1
		}
		visible[len(visible)-1] = lipgloss.NewStyle().Foreground(colorDim).Render(
			fmt.Sprintf("  ▼ %d more row(s) below", remainingRows))
	}

	return padToSize(strings.Join(visible, "\n"), w, h)
}

// renderTile renders a single provider tile card, filling all available space
// with useful data. The layout is priority-based: header and footer are fixed,
// and the body area between them is packed with gauge, summary, detail,
// individual metrics, metadata, reset timers, and the provider message.
func (m Model) renderTile(snap core.QuotaSnapshot, selected bool, tileW, tileContentH int) string {
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

	di := computeDisplayInfo(snap)
	sepLine := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", innerW))

	// ──────────────────────────────────────────────────────────────────────
	// HEADER (3 lines): name+badge, tag+provider, thin rule
	// ──────────────────────────────────────────────────────────────────────
	icon := StatusIcon(snap.Status)
	iconStr := lipgloss.NewStyle().Foreground(StatusColor(snap.Status)).Render(icon)
	nameStyle := tileNameStyle
	if selected {
		nameStyle = tileNameSelectedStyle
	}
	badge := StatusBadge(snap.Status)
	badgeW := lipgloss.Width(badge)
	name := snap.AccountID
	maxName := innerW - badgeW - 4
	if maxName < 5 {
		maxName = 5
	}
	if len(name) > maxName {
		name = name[:maxName-1] + "…"
	}
	hdrLeft := fmt.Sprintf("%s %s", iconStr, nameStyle.Render(name))
	gap := innerW - lipgloss.Width(hdrLeft) - badgeW
	if gap < 1 {
		gap = 1
	}
	hdrLine1 := hdrLeft + strings.Repeat(" ", gap) + badge

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
	header := []string{hdrLine1, hdrLine2, sepLine}

	// ──────────────────────────────────────────────────────────────────────
	// FOOTER (2 lines): thin rule + timestamp
	// ──────────────────────────────────────────────────────────────────────
	age := time.Since(snap.Timestamp)
	var timeStr string
	if age > 60*time.Second {
		timeStr = fmt.Sprintf("Updated %s ago", formatDuration(age))
	} else if !snap.Timestamp.IsZero() {
		timeStr = fmt.Sprintf("Updated %s", snap.Timestamp.Format("15:04:05"))
	}
	footer := []string{sepLine, tileTimestampStyle.Render(timeStr)}

	// ──────────────────────────────────────────────────────────────────────
	// BODY — priority-ordered content that fills the remaining space
	// ──────────────────────────────────────────────────────────────────────
	bodyBudget := tileContentH - len(header) - len(footer)
	if bodyBudget < 1 {
		bodyBudget = 1
	}

	// We build body as sections; each section is a slice of lines.
	// Sections are appended in priority order until budget is exhausted.
	type section struct {
		lines []string
	}
	var sections []section

	// ── S1: Gauge bar ──
	if di.gaugePercent >= 0 {
		gaugeW := innerW - 8
		if gaugeW < 6 {
			gaugeW = 6
		}
		sections = append(sections, section{[]string{
			"",
			RenderGauge(di.gaugePercent, gaugeW, m.warnThreshold, m.critThreshold),
		}})
	}

	// ── S2: Summary text ──
	if di.summary != "" {
		s := truncate(di.summary)
		if di.gaugePercent >= 0 {
			sections = append(sections, section{[]string{tileSummaryStyle.Render(s)}})
		} else {
			sections = append(sections, section{[]string{
				"",
				lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(s),
			}})
		}
	}

	// ── S3: Detail text ──
	if di.detail != "" {
		sections = append(sections, section{[]string{
			tileSummaryStyle.Render(truncate(di.detail)),
		}})
	}

	// ── S4: Provider message ──
	if snap.Message != "" && snap.Status != core.StatusError {
		msg := snap.Message
		if len(msg) > innerW-3 {
			msg = msg[:innerW-6] + "..."
		}
		sections = append(sections, section{[]string{
			"",
			lipgloss.NewStyle().Foreground(colorSubtext).Italic(true).Render(msg),
		}})
	}

	// ── S5: Individual metrics breakdown ──
	metricLines := m.buildTileMetricLines(snap, innerW)
	if len(metricLines) > 0 {
		s := []string{""}
		s = append(s, metricLines...)
		sections = append(sections, section{s})
	}

	// ── S6: Metadata from Raw ──
	metaLines := buildTileMetaLines(snap, innerW)
	if len(metaLines) > 0 {
		s := []string{""}
		s = append(s, metaLines...)
		sections = append(sections, section{s})
	}

	// ── S7: Reset timers ──
	resetLines := buildTileResetLines(snap, innerW)
	if len(resetLines) > 0 {
		s := []string{""}
		s = append(s, resetLines...)
		sections = append(sections, section{s})
	}

	// Fill body from sections, respecting budget
	var body []string
	for _, sec := range sections {
		if len(body)+len(sec.lines) <= bodyBudget {
			body = append(body, sec.lines...)
		} else {
			// Fit as many lines from this section as possible
			remaining := bodyBudget - len(body)
			if remaining > 0 {
				body = append(body, sec.lines[:remaining]...)
			}
			break
		}
	}

	// Pad remaining space with blanks
	for len(body) < bodyBudget {
		body = append(body, "")
	}

	// ── Assemble ──
	all := make([]string, 0, tileContentH)
	all = append(all, header...)
	all = append(all, body...)
	all = append(all, footer...)

	content := strings.Join(all, "\n")

	// Status-glow effect: selected tiles get border colored by their status
	border := tileBorderStyle.Width(tileW)
	if selected {
		statusGlow := StatusBorderColor(snap.Status)
		border = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(statusGlow).
			Padding(0, tilePadH).
			Width(tileW)
	}
	return border.Render(content)
}

// buildTileMetricLines returns formatted lines for each metric in the snapshot.
// Each metric gets its own row showing label, value, and unit.
func (m Model) buildTileMetricLines(snap core.QuotaSnapshot, innerW int) []string {
	if len(snap.Metrics) == 0 {
		return nil
	}

	keys := make([]string, 0, len(snap.Metrics))
	for k := range snap.Metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, key := range keys {
		met := snap.Metrics[key]
		label := prettifyKey(key)
		if len(label) > 14 {
			label = label[:14]
		}

		var line string
		if met.Limit != nil && met.Used != nil {
			line = fmt.Sprintf("%-14s %s / %s %s", label, formatNumber(*met.Used), formatNumber(*met.Limit), met.Unit)
		} else if met.Limit != nil && met.Remaining != nil {
			pctStr := ""
			if pct := met.Percent(); pct >= 0 {
				pctStr = fmt.Sprintf(" (%.0f%%)", pct)
			}
			line = fmt.Sprintf("%-14s %s / %s%s", label, formatNumber(*met.Remaining), formatNumber(*met.Limit), pctStr)
		} else if met.Used != nil {
			line = fmt.Sprintf("%-14s %s %s", label, formatNumber(*met.Used), met.Unit)
		} else if met.Remaining != nil {
			line = fmt.Sprintf("%-14s %s remaining", label, formatNumber(*met.Remaining))
		} else {
			continue
		}

		if lipgloss.Width(line) > innerW {
			line = line[:innerW]
		}
		lines = append(lines, dimStyle.Render(line))
	}
	return lines
}

// buildTileMetaLines returns formatted metadata lines from snap.Raw.
func buildTileMetaLines(snap core.QuotaSnapshot, innerW int) []string {
	if len(snap.Raw) == 0 {
		return nil
	}

	type rawEntry struct {
		icon, key string
	}
	order := []rawEntry{
		{"✉", "account_email"},
		{"◆", "plan_name"},
		{"◇", "plan_type"},
		{"👤", "membership_type"},
		{"🏢", "team_membership"},
		{"🏢", "organization_name"},
		{"⬡", "active_model"},
		{"⌘", "cli_version"},
		{"$", "plan_price"},
		{"✓", "subscription_status"},
	}

	var lines []string
	for _, e := range order {
		val, ok := snap.Raw[e.key]
		if !ok || val == "" {
			continue
		}
		rendered := e.icon + " " + val
		if lipgloss.Width(rendered) > innerW {
			rendered = rendered[:innerW]
		}
		lines = append(lines, dimStyle.Render(rendered))
	}
	return lines
}

// buildTileResetLines returns formatted reset timer lines.
func buildTileResetLines(snap core.QuotaSnapshot, innerW int) []string {
	if len(snap.Resets) == 0 {
		return nil
	}

	keys := make([]string, 0, len(snap.Resets))
	for k := range snap.Resets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, key := range keys {
		t := snap.Resets[key]
		dur := time.Until(t)
		if dur < 0 {
			continue
		}
		label := prettifyKey(key)
		entry := fmt.Sprintf("⟳ %s resets in %s", label, formatDuration(dur))
		if lipgloss.Width(entry) > innerW {
			entry = entry[:innerW]
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(colorYellow).Render(entry))
	}
	return lines
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// intersperse inserts sep between each element when joining horizontally.
func intersperse(items []string, sep string) []string {
	if len(items) <= 1 {
		return items
	}
	result := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			result = append(result, sep)
		}
		result = append(result, item)
	}
	return result
}
