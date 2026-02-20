package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/agentusage/internal/core"
)

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

func (m Model) tileGrid(contentW, contentH, n int) (cols, tileW, tileContentH int) {
	if n == 0 {
		return 1, tileMinWidth, tileDefaultLines
	}

	bestCols := 2
	if n == 1 {
		bestCols = 1
	}

	usableW := contentW - 2 - (bestCols-1)*tileGapH // 2 = outer padding
	tw := usableW/bestCols - tileBorderH
	if tw < tileMinWidth {
		bestCols = 1
		usableW = contentW - 2
		tw = usableW - tileBorderH
		if tw < tileMinWidth {
			tw = tileMinWidth
		}
	}

	rows := (n + bestCols - 1) / bestCols
	usableW = contentW - 2 - (bestCols-1)*tileGapH
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

func (m Model) tileCols() int {
	n := len(m.filteredIDs())
	contentH := m.height - 3
	if contentH < 5 {
		contentH = 5
	}
	cols, _, _ := m.tileGrid(m.width, contentH, n)
	return cols
}

func (m Model) renderTiles(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 {
		empty := []string{
			"",
			dimStyle.Render("  Loading providers…"),
			"",
			lipgloss.NewStyle().Foreground(colorSubtext).Render("  Fetching quotas and rate limits."),
		}
		return padToSize(strings.Join(empty, "\n"), w, h)
	}

	cols, tileW, tileContentH := m.tileGrid(w, h, len(ids))

	var tiles []string
	for i, id := range ids {
		snap := m.snapshots[id]
		selected := i == m.cursor
		tiles = append(tiles, m.renderTile(snap, selected, tileW, tileContentH))
	}

	var rows []string
	for i := 0; i < len(tiles); i += cols {
		end := i + cols
		if end > len(tiles) {
			end = len(tiles)
		}
		rowTiles := tiles[i:end]

		for len(rowTiles) < cols {
			spacer := strings.Repeat(" ", tileW+tileBorderH)
			rowTiles = append(rowTiles, spacer)
		}

		row := lipgloss.JoinHorizontal(lipgloss.Top, intersperse(rowTiles, strings.Repeat(" ", tileGapH))...)
		rows = append(rows, row)
	}

	gap := strings.Repeat("\n", tileGapV)
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

	cursorRow := m.cursor / cols
	totalRows := (len(ids) + cols - 1) / cols
	rowHeight := tileContentH + tileBorderV + tileGapV

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
	// Build a prominent reset-time line for the header showing the most urgent reset.
	var hdrResetLine string
	if len(snap.Resets) > 0 {
		var soonestLabel string
		var soonestDur time.Duration
		first := true
		for key, t := range snap.Resets {
			dur := time.Until(t)
			if dur < 0 {
				continue
			}
			if first || dur < soonestDur {
				soonestDur = dur
				soonestLabel = resetLabelMap[key]
				if soonestLabel == "" {
					if met, ok := snap.Metrics[key]; ok && met.Window != "" {
						soonestLabel = "Rate " + met.Window
					} else {
						soonestLabel = prettifyKey(key)
					}
				}
				first = false
			}
		}
		if !first {
			clockFrames := []string{"◴", "◷", "◶", "◵"}
			clock := clockFrames[(m.animFrame/3)%len(clockFrames)]

			durColor := colorTeal
			if soonestDur < 10*time.Minute {
				durColor = colorPeach
			} else if soonestDur < 30*time.Minute {
				durColor = colorYellow
			}

			durStr := formatDuration(soonestDur)
			resetPill := lipgloss.NewStyle().Foreground(durColor).Render(clock) +
				lipgloss.NewStyle().Foreground(colorSubtext).Render(" "+soonestLabel+" resets in ") +
				lipgloss.NewStyle().Foreground(durColor).Bold(true).Render(durStr)

			// Right-align the reset pill on the header line
			pillW := lipgloss.Width(resetPill)
			pad := innerW - pillW
			if pad < 0 {
				pad = 0
			}
			hdrResetLine = strings.Repeat(" ", pad) + resetPill
		}
	}

	header := []string{hdrLine1, hdrLine2}
	if hdrResetLine != "" {
		header = append(header, hdrResetLine)
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

	bodyBudget := tileContentH - len(header) - len(footer)
	if bodyBudget < 1 {
		bodyBudget = 1
	}

	type section struct {
		lines []string
	}
	var sections []section

	gaugeLines := m.buildTileGaugeLines(snap, innerW)
	if len(gaugeLines) > 0 {
		s := []string{""}
		s = append(s, gaugeLines...)
		sections = append(sections, section{s})
	}

	if di.summary != "" {
		s := truncate(di.summary)
		if len(gaugeLines) > 0 {
			sections = append(sections, section{[]string{tileHeroStyle.Render(s)}})
		} else {
			sections = append(sections, section{[]string{
				"",
				tileHeroStyle.Render(s),
			}})
		}
	}

	if di.detail != "" {
		sections = append(sections, section{[]string{
			tileSummaryStyle.Render(truncate(di.detail)),
		}})
	}

	metricLines := m.buildTileMetricLines(snap, innerW)
	if len(metricLines) > 0 {
		s := []string{""}
		s = append(s, metricLines...)
		sections = append(sections, section{s})
	}

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

	metaLines := buildTileMetaLines(snap, innerW)
	if len(metaLines) > 0 {
		s := []string{""}
		s = append(s, metaLines...)
		sections = append(sections, section{s})
	}

	resetLines := buildTileResetLines(snap, innerW, m.animFrame)
	if len(resetLines) > 0 {
		s := []string{""}
		s = append(s, resetLines...)
		sections = append(sections, section{s})
	}

	var body []string
	for _, sec := range sections {
		if len(body)+len(sec.lines) <= bodyBudget {
			body = append(body, sec.lines...)
		} else {
			remaining := bodyBudget - len(body)
			if remaining > 0 {
				body = append(body, sec.lines[:remaining]...)
			}
			break
		}
	}

	for len(body) < bodyBudget {
		body = append(body, "")
	}

	all := make([]string, 0, tileContentH)
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

func (m Model) buildTileGaugeLines(snap core.QuotaSnapshot, innerW int) []string {
	if len(snap.Metrics) == 0 {
		return nil
	}

	keys := make([]string, 0, len(snap.Metrics))
	for k := range snap.Metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	maxLabelW := 14
	gaugeW := innerW - maxLabelW - 10 // label + gauge + " XX.X%" + spaces
	if gaugeW < 6 {
		gaugeW = 6
	}

	var lines []string
	for _, key := range keys {
		met := snap.Metrics[key]
		usedPct := metricUsedPercent(key, met)
		if usedPct < 0 {
			continue
		}

		label := gaugeLabel(key, met.Window)
		if len(label) > maxLabelW {
			label = label[:maxLabelW-1] + "…"
		}

		gauge := RenderUsageGauge(usedPct, gaugeW, m.warnThreshold, m.critThreshold)
		labelR := lipgloss.NewStyle().Foreground(colorSubtext).Width(maxLabelW).Render(label)
		lines = append(lines, labelR+" "+gauge)
	}
	return lines
}

func gaugeLabel(key string, window ...string) string {
	overrides := map[string]string{
		"usage_five_hour":        "5 Hour",
		"usage_seven_day":        "7 Day",
		"usage_seven_day_sonnet": "7d Sonnet",
		"usage_seven_day_opus":   "7d Opus",
		"usage_seven_day_cowork": "7d Cowork",
		"extra_usage":            "Extra",
		"plan_percent_used":      "Plan Used",
		"plan_spend":             "Spend",
		"plan_total_spend_usd":   "Total Spend",
		"spend_limit":            "Spend Limit",
		"individual_spend":       "My Spend",
	}

	if strings.HasPrefix(key, "rate_limit_") {
		w := ""
		if len(window) > 0 {
			w = window[0]
		}
		if w != "" {
			return "Rate " + w
		}
		return "Rate " + prettifyKey(strings.TrimPrefix(key, "rate_limit_"))
	}
	if label, ok := overrides[key]; ok {
		return label
	}
	if label, ok := prettifyKeyOverrides[key]; ok {
		return label
	}

	clean := key
	for _, prefix := range []string{"usage_", "rate_limit_"} {
		if strings.HasPrefix(clean, prefix) {
			clean = clean[len(prefix):]
			break
		}
	}

	if strings.Contains(clean, "-") {
		parts := strings.Split(clean, "-")
		for i, p := range parts {
			if len(p) > 0 {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			}
		}
		return strings.Join(parts, " ")
	}

	return prettifyKey(clean)
}

var metricsNoGauge = map[string]bool{
	"context_window": true,
}

func metricUsedPercent(key string, met core.Metric) float64 {
	if metricsNoGauge[key] {
		return -1
	}
	if met.Unit == "%" && met.Used != nil {
		return *met.Used
	}
	if met.Limit != nil && met.Remaining != nil && *met.Limit > 0 {
		return (*met.Limit - *met.Remaining) / *met.Limit * 100
	}
	if met.Limit != nil && met.Used != nil && *met.Limit > 0 {
		return *met.Used / *met.Limit * 100
	}
	return -1
}

func metricHasGauge(key string, met core.Metric) bool {
	return metricUsedPercent(key, met) >= 0
}

func (m Model) buildTileMetricLines(snap core.QuotaSnapshot, innerW int) []string {
	if len(snap.Metrics) == 0 {
		return nil
	}

	keys := make([]string, 0, len(snap.Metrics))
	for k := range snap.Metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	maxLabel := innerW/2 - 1
	if maxLabel < 8 {
		maxLabel = 8
	}

	var lines []string
	for _, key := range keys {
		met := snap.Metrics[key]
		if metricHasGauge(key, met) {
			continue
		}
		value := formatTileMetricValue(key, met)
		if value == "" {
			continue
		}

		label := prettifyKey(key)
		if len(label) > maxLabel {
			label = label[:maxLabel-1] + "…"
		}

		lines = append(lines, renderDotLeaderRow(label, value, innerW))
	}
	return lines
}

func buildTileMetaLines(snap core.QuotaSnapshot, innerW int) []string {
	if len(snap.Raw) == 0 {
		return nil
	}

	type metaEntry struct {
		label, key string
	}
	order := []metaEntry{
		{"Account", "account_email"},
		{"Plan", "plan_name"},
		{"Type", "plan_type"},
		{"Role", "membership_type"},
		{"Team", "team_membership"},
		{"Org", "organization_name"},
		{"Model", "active_model"},
		{"Version", "cli_version"},
		{"Price", "plan_price"},
		{"Status", "subscription_status"},
	}

	var lines []string
	for _, e := range order {
		val, ok := snap.Raw[e.key]
		if !ok || val == "" {
			continue
		}
		maxVal := innerW - len(e.label) - 5
		if maxVal < 5 {
			maxVal = 5
		}
		if len(val) > maxVal {
			val = val[:maxVal-1] + "…"
		}
		lines = append(lines, renderDotLeaderRow(e.label, val, innerW))
	}
	return lines
}

var resetLabelMap = map[string]string{
	"billing_block":   "5h Block",
	"usage_five_hour": "5h Quota",
	"usage_seven_day": "7d Quota",
}

func buildTileResetLines(snap core.QuotaSnapshot, innerW int, animFrame int) []string {
	if len(snap.Resets) == 0 {
		return nil
	}

	keys := make([]string, 0, len(snap.Resets))
	for k := range snap.Resets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type resetEntry struct {
		label string
		dur   time.Duration
	}
	var entries []resetEntry
	for _, key := range keys {
		t := snap.Resets[key]
		dur := time.Until(t)
		if dur < 0 {
			continue
		}
		label := resetLabelMap[key]
		if label == "" {
			if met, ok := snap.Metrics[key]; ok && met.Window != "" {
				label = "Rate " + met.Window
			} else {
				label = prettifyKey(key)
			}
		}
		entries = append(entries, resetEntry{label: label, dur: dur})
	}

	if len(entries) == 0 {
		return nil
	}

	dimSep := lipgloss.NewStyle().Foreground(colorSurface2).Render(" │ ")

	var pills []string
	for _, e := range entries {
		durStr := formatDuration(e.dur)

		durColor := colorTeal
		if e.dur < 10*time.Minute {
			durColor = colorPeach
		} else if e.dur < 30*time.Minute {
			durColor = colorYellow
		}

		clockFrames := []string{"◴", "◷", "◶", "◵"}
		clock := clockFrames[(animFrame/3)%len(clockFrames)]

		pill := lipgloss.NewStyle().Foreground(colorSubtext).Render(clock+" "+e.label+" ") +
			lipgloss.NewStyle().Foreground(durColor).Bold(true).Render(durStr)
		pills = append(pills, pill)
	}

	oneLine := strings.Join(pills, dimSep)
	if lipgloss.Width(oneLine) <= innerW {
		return []string{oneLine}
	}

	var lines []string
	for _, pill := range pills {
		if lipgloss.Width(pill) > innerW {
			pill = pill[:innerW]
		}
		lines = append(lines, pill)
	}
	return lines
}

func renderDotLeaderRow(label, value string, totalW int) string {
	labelR := lipgloss.NewStyle().Foreground(colorSubtext).Render(label)
	valueR := lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(value)
	lw := lipgloss.Width(labelR)
	vw := lipgloss.Width(valueR)
	dotsW := totalW - lw - vw - 2
	if dotsW < 1 {
		dotsW = 1
	}
	dots := tileDotLeaderStyle.Render(strings.Repeat("·", dotsW))
	return labelR + " " + dots + " " + valueR
}

func formatTileMetricValue(key string, met core.Metric) string {
	isUSD := met.Unit == "USD" || strings.HasSuffix(key, "_usd") ||
		strings.Contains(key, "cost") || strings.Contains(key, "spend") ||
		strings.Contains(key, "price")
	isPct := met.Unit == "%"

	if met.Limit != nil && met.Used != nil {
		if isUSD {
			return fmt.Sprintf("$%s / $%s", formatNumber(*met.Used), formatNumber(*met.Limit))
		}
		if isPct {
			return fmt.Sprintf("%.0f%%", *met.Used)
		}
		unit := met.Unit
		switch unit {
		case "tokens":
			unit = "tok"
		case "requests":
			unit = "req"
		case "messages":
			unit = "messages"
		}
		if unit != "" {
			return fmt.Sprintf("%s / %s %s", formatNumber(*met.Used), formatNumber(*met.Limit), unit)
		}
		return fmt.Sprintf("%s / %s", formatNumber(*met.Used), formatNumber(*met.Limit))
	}
	if met.Limit != nil && met.Remaining != nil {
		used := *met.Limit - *met.Remaining
		usedPct := used / *met.Limit * 100
		return fmt.Sprintf("%s / %s (%.0f%%)", formatNumber(used), formatNumber(*met.Limit), usedPct)
	}
	if met.Used != nil {
		if isUSD {
			return fmt.Sprintf("$%s", formatNumber(*met.Used))
		}
		if isPct {
			return fmt.Sprintf("%.0f%%", *met.Used)
		}
		unit := met.Unit
		switch unit {
		case "tokens":
			unit = "tok"
		case "requests":
			unit = "req"
		}
		if unit == "" {
			return formatNumber(*met.Used)
		}
		return fmt.Sprintf("%s %s", formatNumber(*met.Used), unit)
	}
	if met.Remaining != nil {
		return fmt.Sprintf("%s avail", formatNumber(*met.Remaining))
	}
	return ""
}

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
