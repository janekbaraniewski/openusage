package tui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
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

	for c := maxCols; c >= 1; c-- {
		perCol := (usableW-(c-1)*tileGapH)/c - tileBorderH
		if perCol < tileMinWidth {
			continue
		}

		if c == 1 {
			return 1, perCol, 0
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

		return c, perCol, perRowContentH
	}

	fallbackW := usableW - tileBorderH
	if fallbackW < tileMinWidth {
		fallbackW = tileMinWidth
	}
	return 1, fallbackW, 0
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
			lipgloss.NewStyle().Foreground(colorSubtext).Render("  Fetching usage and spend data."),
		}
		return padToSize(strings.Join(empty, "\n"), w, h)
	}

	cols, tileW, tileMaxHeight := m.tileGrid(w, h, len(ids))

	var tiles [][]string
	for i, id := range ids {
		snap := m.snapshots[id]
		selected := i == m.cursor
		modelMixExpanded := selected && m.expandedModelMixTiles[id]
		rendered := m.renderTile(snap, selected, modelMixExpanded, tileW, tileMaxHeight)
		tiles = append(tiles, strings.Split(rendered, "\n"))
	}

	var rows []string
	var rowHeights []int
	gap := strings.Repeat("\n", tileGapV)

	for i := 0; i < len(tiles); i += cols {
		end := i + cols
		if end > len(tiles) {
			end = len(tiles)
		}
		rowTiles := tiles[i:end]

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

	scrollLine := rowOffsets[cursorRow] + m.tileOffset
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
	if endLine < totalLines {
		visible[len(visible)-1] = lipgloss.NewStyle().Foreground(colorDim).Render("  ▼ more below")
	}

	return padToSize(strings.Join(visible, "\n"), w, h)
}

func (m Model) renderTile(snap core.QuotaSnapshot, selected, modelMixExpanded bool, tileW, tileContentH int) string {
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

	type section struct {
		lines []string
	}
	var sections []section

	gaugeLines := m.buildTileGaugeLines(snap, widget, innerW)
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

	compactMetricLines, compactMetricKeys := buildTileCompactMetricSummaryLines(snap, widget, innerW)

	modelBurnLines, modelBurnKeys := buildProviderModelCompositionLines(snap, innerW, modelMixExpanded)
	if len(modelBurnLines) > 0 {
		s := []string{""}
		s = append(s, modelBurnLines...)
		sections = append(sections, section{s})
	}
	if len(modelBurnKeys) > 0 {
		if compactMetricKeys == nil {
			compactMetricKeys = make(map[string]bool)
		}
		for k := range modelBurnKeys {
			compactMetricKeys[k] = true
		}
	}

	if len(compactMetricLines) > 0 {
		s := []string{""}
		s = append(s, compactMetricLines...)
		sections = append(sections, section{s})
	}

	metricLines := m.buildTileMetricLines(snap, widget, innerW, compactMetricKeys)
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

	if len(headerMeta) == 0 {
		resetLines := buildTileResetLines(snap, widget, innerW, m.animFrame)
		if len(resetLines) > 0 {
			s := []string{""}
			s = append(s, resetLines...)
			sections = append(sections, section{s})
		}
	}

	var body []string
	for _, sec := range sections {
		if bodyBudget < 0 {
			body = append(body, sec.lines...)
			continue
		}

		if len(body)+len(sec.lines) <= bodyBudget {
			body = append(body, sec.lines...)
			continue
		}

		remaining := bodyBudget - len(body)
		if remaining > 0 {
			body = append(body, sec.lines[:remaining]...)
		}
		break
	}

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

func (m Model) buildTileGaugeLines(snap core.QuotaSnapshot, widget core.DashboardWidget, innerW int) []string {
	if len(snap.Metrics) == 0 {
		return nil
	}

	keys := make([]string, 0, len(snap.Metrics))
	for k := range snap.Metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	keys = prioritizeMetricKeys(keys, widget.GaugePriority)

	maxLabelW := 14
	gaugeW := innerW - maxLabelW - 10 // label + gauge + " XX.X%" + spaces
	if gaugeW < 6 {
		gaugeW = 6
	}
	maxLines := widget.GaugeMaxLines
	if maxLines <= 0 {
		maxLines = 2
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
		if maxLines > 0 && len(lines) >= maxLines {
			break
		}
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
		"plan_spend":             "Credits",
		"plan_total_spend_usd":   "Total Credits",
		"spend_limit":            "Credit Limit",
		"individual_spend":       "My Credits",
	}

	if strings.HasPrefix(key, "rate_limit_") {
		w := ""
		if len(window) > 0 {
			w = window[0]
		}
		if w != "" {
			return "Usage " + w
		}
		return "Usage " + prettifyKey(strings.TrimPrefix(key, "rate_limit_"))
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

type compactMetricRowSpec struct {
	label       string
	keys        []string
	match       func(string, core.Metric) bool
	maxSegments int
}

func buildTileCompactMetricSummaryLines(snap core.QuotaSnapshot, widget core.DashboardWidget, innerW int) ([]string, map[string]bool) {
	if len(snap.Metrics) == 0 || len(widget.CompactRows) == 0 {
		return nil, nil
	}

	specs := make([]compactMetricRowSpec, 0, len(widget.CompactRows))
	for _, row := range widget.CompactRows {
		spec := compactMetricRowSpec{
			label:       row.Label,
			keys:        row.Keys,
			maxSegments: row.MaxSegments,
		}
		if row.Matcher.Prefix != "" || row.Matcher.Suffix != "" {
			prefix := row.Matcher.Prefix
			suffix := row.Matcher.Suffix
			spec.match = func(key string, _ core.Metric) bool {
				if prefix != "" && !strings.HasPrefix(key, prefix) {
					return false
				}
				if suffix != "" && !strings.HasSuffix(key, suffix) {
					return false
				}
				return true
			}
		}
		specs = append(specs, spec)
	}

	consumed := make(map[string]bool)
	var lines []string
	for _, spec := range specs {
		segments, usedKeys := collectCompactMetricSegments(spec, snap.Metrics, consumed)
		if len(segments) == 0 {
			continue
		}

		value := strings.Join(segments, " · ")
		maxValueW := innerW - lipgloss.Width(spec.label) - 6
		if maxValueW < 12 {
			maxValueW = 12
		}
		value = truncateToWidth(value, maxValueW)

		lines = append(lines, renderDotLeaderRow(spec.label, value, innerW))
		for _, key := range usedKeys {
			consumed[key] = true
		}
	}

	if len(lines) == 0 {
		return nil, nil
	}
	return lines, consumed
}

func collectCompactMetricSegments(spec compactMetricRowSpec, metrics map[string]core.Metric, consumed map[string]bool) ([]string, []string) {
	maxSegments := spec.maxSegments
	if maxSegments <= 0 {
		maxSegments = 4
	}

	var segments []string
	var used []string
	add := func(key string, met core.Metric) {
		if len(segments) >= maxSegments {
			return
		}
		segment := compactMetricSegment(key, met)
		if segment == "" {
			return
		}
		segments = append(segments, segment)
		used = append(used, key)
	}

	for _, key := range spec.keys {
		if len(segments) >= maxSegments {
			break
		}
		if consumed[key] {
			continue
		}
		met, ok := metrics[key]
		if !ok {
			continue
		}
		add(key, met)
	}

	if spec.match != nil && len(segments) < maxSegments {
		keys := make([]string, 0, len(metrics))
		for key := range metrics {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if len(segments) >= maxSegments {
				break
			}
			if consumed[key] || stringInSlice(key, spec.keys) {
				continue
			}
			met := metrics[key]
			if !spec.match(key, met) {
				continue
			}
			add(key, met)
		}
	}

	return segments, used
}

func stringInSlice(s string, items []string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}

func compactMetricSegment(key string, met core.Metric) string {
	value := compactMetricValue(key, met)
	if value == "" {
		return ""
	}
	label := compactMetricLabel(key)
	if label == "" {
		return value
	}
	return label + " " + value
}

func compactMetricLabel(key string) string {
	if strings.HasPrefix(key, "org_") && strings.HasSuffix(key, "_seats") {
		org := strings.TrimSuffix(strings.TrimPrefix(key, "org_"), "_seats")
		if org != "" {
			return truncateToWidth(org, 8)
		}
		return "seats"
	}

	if strings.HasPrefix(key, "rate_limit_") {
		return strings.TrimPrefix(key, "rate_limit_")
	}

	labels := map[string]string{
		"plan_spend":               "plan",
		"plan_included":            "incl",
		"plan_bonus":               "bonus",
		"spend_limit":              "cap",
		"individual_spend":         "mine",
		"plan_percent_used":        "used",
		"plan_total_spend_usd":     "plan",
		"plan_limit_usd":           "limit",
		"today_api_cost":           "today",
		"7d_api_cost":              "7d",
		"all_time_api_cost":        "all",
		"5h_block_cost":            "5h",
		"burn_rate":                "burn",
		"credit_balance":           "balance",
		"credits":                  "credits",
		"monthly_spend":            "month",
		"usage_five_hour":          "5h",
		"usage_seven_day":          "7d",
		"session_input_tokens":     "in",
		"session_output_tokens":    "out",
		"session_cached_tokens":    "cached",
		"session_reasoning_tokens": "reason",
		"context_window":           "ctx",
		"messages_today":           "msgs",
		"sessions_today":           "sess",
		"tool_calls_today":         "tools",
		"7d_messages":              "7d msgs",
		"5h_block_input":           "in5h",
		"5h_block_output":          "out5h",
		"7d_input_tokens":          "in7d",
		"7d_output_tokens":         "out7d",
		"chat_quota":               "chat",
		"completions_quota":        "comp",
		"gh_core_rpm":              "core",
		"gh_search_rpm":            "search",
		"gh_graphql_rpm":           "graphql",
		"rpm":                      "rpm",
		"tpm":                      "tpm",
		"rpd":                      "rpd",
		"tpd":                      "tpd",
	}
	return labels[key]
}

func compactMetricValue(key string, met core.Metric) string {
	if key == "burn_rate" && met.Used != nil {
		return fmt.Sprintf("%s/h", formatUSD(*met.Used))
	}

	used, hasUsed := metricUsedValue(met)
	isUSD := isTileUSDMetric(key, met)
	isPct := met.Unit == "%"

	if met.Limit != nil {
		if hasUsed {
			if isPct {
				return fmt.Sprintf("%.0f%%", used)
			}
			if isUSD {
				return fmt.Sprintf("%s/%s", formatUSD(used), formatUSD(*met.Limit))
			}
			return fmt.Sprintf("%s/%s", compactMetricAmount(used, met.Unit), compactMetricAmount(*met.Limit, met.Unit))
		}
		if met.Remaining != nil && isPct {
			return fmt.Sprintf("%.0f%%", 100-*met.Remaining)
		}
	}

	if hasUsed {
		if isPct {
			return fmt.Sprintf("%.0f%%", used)
		}
		if isUSD {
			return formatUSD(used)
		}
		return compactMetricAmount(used, met.Unit)
	}

	if met.Remaining != nil {
		if isPct {
			return fmt.Sprintf("%.0f%% left", *met.Remaining)
		}
		if isUSD {
			return fmt.Sprintf("%s left", formatUSD(*met.Remaining))
		}
		return fmt.Sprintf("%s left", compactMetricAmount(*met.Remaining, met.Unit))
	}

	return ""
}

func metricUsedValue(met core.Metric) (float64, bool) {
	if met.Used != nil {
		return *met.Used, true
	}
	if met.Limit != nil && met.Remaining != nil {
		return *met.Limit - *met.Remaining, true
	}
	return 0, false
}

func isTileUSDMetric(key string, met core.Metric) bool {
	return met.Unit == "USD" || strings.HasSuffix(key, "_usd") ||
		strings.Contains(key, "cost") || strings.Contains(key, "spend") ||
		strings.Contains(key, "price")
}

func compactMetricAmount(v float64, unit string) string {
	switch unit {
	case "tokens", "requests", "messages", "completions", "conversations", "seats", "quota", "lines":
		return shortCompact(v)
	case "":
		return shortCompact(v)
	default:
		return fmt.Sprintf("%s %s", shortCompact(v), unit)
	}
}

func (m Model) buildTileMetricLines(snap core.QuotaSnapshot, widget core.DashboardWidget, innerW int, skipKeys map[string]bool) []string {
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
		if skipKeys != nil && skipKeys[key] {
			continue
		}
		if hasAnyPrefix(key, widget.HideMetricPrefixes) || containsString(widget.HideMetricKeys, key) {
			continue
		}
		met := snap.Metrics[key]
		if shouldSuppressMetricLine(widget, key, met, snap.Metrics) {
			continue
		}
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

func shouldSuppressMetricLine(widget core.DashboardWidget, key string, met core.Metric, all map[string]core.Metric) bool {
	// Key-level usage on /key is often zero/no-limit even when account has non-zero /credits totals.
	// Hide noisy zero rows and prefer the higher-signal credit_balance summary.
	if widget.HideCreditsWhenBalancePresent && key == "credits" {
		if _, hasBalance := all["credit_balance"]; hasBalance {
			return true
		}
	}

	if containsString(widget.SuppressZeroMetricKeys, key) {
		if met.Used == nil || *met.Used == 0 {
			return true
		}
	}

	if widget.SuppressZeroNonQuotaMetrics && met.Used != nil && *met.Used == 0 && met.Limit == nil && met.Remaining == nil {
		return true
	}

	return false
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
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
		{"Key", "key_label"},
		{"Key Name", "key_name"},
		{"Key Type", "key_type"},
		{"Tier", "tier"},
		{"Plan", "plan_name"},
		{"Type", "plan_type"},
		{"Role", "membership_type"},
		{"Team", "team_membership"},
		{"Org", "organization_name"},
		{"Model", "active_model"},
		{"Version", "cli_version"},
		{"Price", "plan_price"},
		{"Status", "subscription_status"},
		{"Reset", "limit_reset"},
		{"Expires", "expires_at"},
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

func buildTileHeaderMetaLines(snap core.QuotaSnapshot, widget core.DashboardWidget, innerW int, animFrame int) []string {
	var pills []string
	pills = append(pills, buildTileCyclePills(snap)...)
	pills = append(pills, buildTileResetPills(snap, widget, animFrame)...)
	return wrapTilePills(pills, innerW)
}

func buildTileCyclePills(snap core.QuotaSnapshot) []string {
	var pills []string
	if pill := buildTileCyclePill("Billing", snap.Raw["billing_cycle_start"], snap.Raw["billing_cycle_end"]); pill != "" {
		pills = append(pills, pill)
	}
	if pill := buildTileCyclePill("5h Block", snap.Raw["block_start"], snap.Raw["block_end"]); pill != "" {
		pills = append(pills, pill)
	}
	return pills
}

func buildTileCyclePill(label, startRaw, endRaw string) string {
	start, hasStart := parseTileTimestamp(startRaw)
	end, hasEnd := parseTileTimestamp(endRaw)
	if !hasStart && !hasEnd {
		return ""
	}

	var span string
	switch {
	case hasStart && hasEnd:
		span = fmt.Sprintf("%s→%s", formatTileTimestamp(start), formatTileTimestamp(end))
	case hasEnd:
		span = "ends " + formatTileTimestamp(end)
	default:
		span = "since " + formatTileTimestamp(start)
	}

	return lipgloss.NewStyle().Foreground(colorLavender).Bold(true).Render("◷ "+label) +
		" " + lipgloss.NewStyle().Foreground(colorSubtext).Render(span)
}

func parseTileTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}

	if unixVal, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if unixVal > 1e12 {
			return time.Unix(unixVal/1000, (unixVal%1000)*1e6), true
		}
		return time.Unix(unixVal, 0), true
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02",
		"Jan 02, 2006 15:04 MST",
		"Jan 02, 2006 15:04",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}

	return time.Time{}, false
}

func formatTileTimestamp(t time.Time) string {
	now := time.Now()
	isDateOnly := t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0
	if isDateOnly {
		if t.Year() == now.Year() {
			return t.Format("Jan 02")
		}
		return t.Format("2006-01-02")
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 02 15:04")
	}
	return t.Format("2006-01-02 15:04")
}

func wrapTilePills(pills []string, innerW int) []string {
	if len(pills) == 0 {
		return nil
	}

	sep := dimStyle.Render(" · ")
	sepW := lipgloss.Width(sep)

	var lines []string
	var line string
	lineW := 0

	for _, pill := range pills {
		pillW := lipgloss.Width(pill)
		if lineW == 0 {
			line = pill
			lineW = pillW
			continue
		}
		if lineW+sepW+pillW <= innerW {
			line += sep + pill
			lineW += sepW + pillW
			continue
		}
		lines = append(lines, line)
		line = pill
		lineW = pillW
	}

	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

type resetEntry struct {
	key   string
	label string
	dur   time.Duration
	at    time.Time
}

var resetLabelMap = map[string]string{
	"billing_block":        "5h Block",
	"billing_cycle_end":    "Billing",
	"quota_reset":          "Quota",
	"usage_five_hour":      "5h Usage",
	"usage_seven_day":      "7d Usage",
	"limit_reset":          "Limit",
	"key_expires":          "Key Exp",
	"rate_limit_primary":   "Primary",
	"rate_limit_secondary": "Secondary",
	"rpm":                  "RPM",
	"tpm":                  "TPM",
	"rpd":                  "RPD",
	"tpd":                  "TPD",
	"rpm_headers":          "Req",
	"tpm_headers":          "Tok",
	"gh_core_rpm":          "Core",
	"gh_search_rpm":        "Search",
	"gh_graphql_rpm":       "GraphQL",
}

func collectActiveResetEntries(snap core.QuotaSnapshot, widget core.DashboardWidget) []resetEntry {
	if len(snap.Resets) == 0 {
		return nil
	}

	var entries []resetEntry
	for key, t := range snap.Resets {
		dur := time.Until(t)
		if dur < 0 {
			continue
		}
		entries = append(entries, resetEntry{
			key:   key,
			label: resetLabelForKey(snap, widget, key),
			dur:   dur,
			at:    t,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].at.Equal(entries[j].at) {
			return entries[i].at.Before(entries[j].at)
		}
		return entries[i].label < entries[j].label
	})
	return entries
}

func resetLabelForKey(snap core.QuotaSnapshot, widget core.DashboardWidget, key string) string {
	if widget.ResetStyle == core.DashboardResetStyleCompactModelResets {
		if label := compactModelResetLabel(strings.TrimSuffix(key, "_reset")); label != "" {
			return label
		}
	}
	if label := resetLabelMap[key]; label != "" {
		return label
	}
	trimmed := strings.TrimSuffix(key, "_reset")
	if label := resetLabelMap[trimmed]; label != "" {
		return label
	}
	if met, ok := snap.Metrics[trimmed]; ok && met.Window != "" {
		return prettifyKey(trimmed)
	}
	if met, ok := snap.Metrics[key]; ok && met.Window != "" {
		return prettifyKey(key)
	}
	return prettifyKey(trimmed)
}

func compactModelResetLabel(key string) string {
	model := key
	token := ""
	if idx := strings.LastIndex(key, "_"); idx > 0 {
		model = key[:idx]
		token = key[idx+1:]
	}

	model = strings.ToLower(model)
	model = strings.ReplaceAll(model, "_", "-")

	model = truncateToWidth(model, 18)
	if token == "" {
		return model
	}

	tokenMap := map[string]string{
		"requests": "req",
		"tokens":   "tok",
		"quota":    "quota",
	}
	if short, ok := tokenMap[token]; ok {
		token = short
	}
	return model + " " + token
}

func formatHeaderDuration(d time.Duration) string {
	if d <= 0 {
		return "<1m"
	}
	if d < time.Hour {
		mins := int(math.Ceil(d.Minutes()))
		if mins < 1 {
			mins = 1
		}
		return fmt.Sprintf("%dm", mins)
	}
	if d < 24*time.Hour {
		totalMins := int(math.Ceil(d.Minutes()))
		h := totalMins / 60
		m := totalMins % 60
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	totalHours := int(math.Ceil(d.Hours()))
	return fmt.Sprintf("%dd%02dh", totalHours/24, totalHours%24)
}

func buildCompactModelResetPills(entries []resetEntry) []string {
	if len(entries) == 0 {
		return nil
	}

	type group struct {
		at     time.Time
		labels []string
		minDur time.Duration
	}
	groups := make(map[int64]*group)
	for _, e := range entries {
		bucket := e.at.Unix() / 60
		g, ok := groups[bucket]
		if !ok {
			g = &group{at: e.at, minDur: e.dur}
			groups[bucket] = g
		}
		if e.dur < g.minDur {
			g.minDur = e.dur
		}
		g.labels = append(g.labels, e.label)
	}

	ordered := make([]*group, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g.labels)
		ordered = append(ordered, g)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].at.Before(ordered[j].at) })

	var pills []string
	for _, g := range ordered {
		durColor := colorTeal
		if g.minDur < 10*time.Minute {
			durColor = colorPeach
		} else if g.minDur < 30*time.Minute {
			durColor = colorYellow
		}

		label := "Model quotas"
		if len(g.labels) <= 2 {
			label = strings.Join(g.labels, ", ")
		} else {
			label = fmt.Sprintf("Model quotas (%d models)", len(g.labels))
		}

		pill := lipgloss.NewStyle().Foreground(colorSubtext).Render("◷ "+label+" ") +
			lipgloss.NewStyle().Foreground(durColor).Bold(true).Render(formatHeaderDuration(g.minDur))
		pills = append(pills, pill)
	}
	return pills
}

func buildTileResetPills(snap core.QuotaSnapshot, widget core.DashboardWidget, animFrame int) []string {
	_ = animFrame
	entries := collectActiveResetEntries(snap, widget)
	if len(entries) == 0 {
		return nil
	}

	if widget.ResetStyle == core.DashboardResetStyleCompactModelResets {
		threshold := widget.ResetCompactThreshold
		if threshold <= 0 {
			threshold = 4
		}
		if len(entries) >= threshold {
			return buildCompactModelResetPills(entries)
		}
	}

	pills := make([]string, 0, len(entries))
	for _, e := range entries {
		durColor := colorTeal
		if e.dur < 10*time.Minute {
			durColor = colorPeach
		} else if e.dur < 30*time.Minute {
			durColor = colorYellow
		}
		pill := lipgloss.NewStyle().Foreground(colorSubtext).Render("◷ "+e.label+" ") +
			lipgloss.NewStyle().Foreground(durColor).Bold(true).Render(formatHeaderDuration(e.dur))
		pills = append(pills, pill)
	}
	return pills
}

func buildTileResetLines(snap core.QuotaSnapshot, widget core.DashboardWidget, innerW int, animFrame int) []string {
	return wrapTilePills(buildTileResetPills(snap, widget, animFrame), innerW)
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

func prioritizeMetricKeys(keys, priority []string) []string {
	if len(priority) == 0 || len(keys) == 0 {
		return keys
	}
	seen := make(map[string]bool, len(keys))
	ordered := make([]string, 0, len(keys))
	for _, key := range priority {
		for _, existing := range keys {
			if existing != key || seen[existing] {
				continue
			}
			ordered = append(ordered, existing)
			seen[existing] = true
			break
		}
	}
	for _, key := range keys {
		if seen[key] {
			continue
		}
		ordered = append(ordered, key)
	}
	return ordered
}

type modelMixEntry struct {
	name   string
	cost   float64
	input  float64
	output float64
}

func buildProviderModelCompositionLines(snap core.QuotaSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allModels, usedKeys := collectProviderModelMix(snap)
	if len(allModels) == 0 {
		return nil, nil
	}
	models, hiddenCount := limitModelMix(allModels, expanded, 5)

	totalCost := float64(0)
	totalTokens := float64(0)
	for _, m := range allModels {
		totalCost += m.cost
		totalTokens += m.input + m.output
	}

	useCost := totalCost > 0
	total := totalTokens
	if useCost {
		total = totalCost
	}
	if total <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	heading := "Model Burn (credits)"
	if !useCost {
		heading = "Model Burn (tokens)"
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(heading),
		"  " + renderModelMixBar(models, total, barW, useCost, snap.AccountID),
	}

	for idx, model := range models {
		value := model.input + model.output
		if useCost {
			value = model.cost
		}
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		label := prettifyModelName(model.name)
		colorDot := lipgloss.NewStyle().Foreground(stableModelColor(model.name, snap.AccountID)).Render("■")
		maxLabelLen := 16
		if innerW < 60 {
			maxLabelLen = 14
		}
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)
		valueStr := fmt.Sprintf("%2.0f%% %s %s/%s tok",
			pct,
			formatUSD(model.cost),
			shortCompact(model.input),
			shortCompact(model.output),
		)
		if !useCost {
			valueStr = fmt.Sprintf("%2.0f%% %s tok", pct, shortCompact(model.input+model.output))
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}
	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more models (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func limitModelMix(models []modelMixEntry, expanded bool, maxVisible int) ([]modelMixEntry, int) {
	if expanded || maxVisible <= 0 || len(models) <= maxVisible {
		return models, 0
	}
	return models[:maxVisible], len(models) - maxVisible
}

func collectProviderModelMix(snap core.QuotaSnapshot) ([]modelMixEntry, map[string]bool) {
	type agg struct {
		cost   float64
		input  float64
		output float64
	}
	byModel := make(map[string]*agg)
	usedKeys := make(map[string]bool)

	ensure := func(name string) *agg {
		if _, ok := byModel[name]; !ok {
			byModel[name] = &agg{}
		}
		return byModel[name]
	}

	recordCost := func(name string, v float64, key string) {
		ensure(name).cost += v
		usedKeys[key] = true
	}
	recordInput := func(name string, v float64, key string) {
		ensure(name).input += v
		usedKeys[key] = true
	}
	recordOutput := func(name string, v float64, key string) {
		ensure(name).output += v
		usedKeys[key] = true
	}

	for key, met := range snap.Metrics {
		if met.Used == nil {
			continue
		}
		switch {
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost_usd"):
			recordCost(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost_usd"), *met.Used, key)
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost"):
			recordCost(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost"), *met.Used, key)
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_input_tokens"):
			recordInput(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_input_tokens"), *met.Used, key)
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_output_tokens"):
			recordOutput(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_output_tokens"), *met.Used, key)
		case strings.HasPrefix(key, "input_tokens_"):
			recordInput(strings.TrimPrefix(key, "input_tokens_"), *met.Used, key)
		case strings.HasPrefix(key, "output_tokens_"):
			recordOutput(strings.TrimPrefix(key, "output_tokens_"), *met.Used, key)
		}
	}

	for key, raw := range snap.Raw {
		if strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_input_tokens") {
			if v, ok := parseTileNumeric(raw); ok {
				recordInput(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_input_tokens"), v, key)
			}
		}
		if strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_output_tokens") {
			if v, ok := parseTileNumeric(raw); ok {
				recordOutput(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_output_tokens"), v, key)
			}
		}
	}

	models := make([]modelMixEntry, 0, len(byModel))
	for name, v := range byModel {
		if v.cost <= 0 && v.input <= 0 && v.output <= 0 {
			continue
		}
		models = append(models, modelMixEntry{
			name:   name,
			cost:   v.cost,
			input:  v.input,
			output: v.output,
		})
	}

	sort.Slice(models, func(i, j int) bool {
		if models[i].cost != models[j].cost {
			return models[i].cost > models[j].cost
		}
		return (models[i].input + models[i].output) > (models[j].input + models[j].output)
	})
	return models, usedKeys
}

func parseTileNumeric(raw string) (float64, bool) {
	s := strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func renderModelMixBar(top []modelMixEntry, total float64, barW int, useCost bool, providerID string) string {
	if len(top) == 0 || total <= 0 {
		return ""
	}

	type seg struct {
		val   float64
		color lipgloss.Color
	}
	segs := make([]seg, 0, len(top)+1)
	sumTop := float64(0)
	for _, m := range top {
		v := m.input + m.output
		if useCost {
			v = m.cost
		}
		if v <= 0 {
			continue
		}
		sumTop += v
		segs = append(segs, seg{
			val:   v,
			color: stableModelColor(m.name, providerID),
		})
	}
	if sumTop < total {
		segs = append(segs, seg{
			val:   total - sumTop,
			color: colorSurface1,
		})
	}
	if len(segs) == 0 {
		return ""
	}

	var sb strings.Builder
	remainingW := barW
	remainingTotal := total
	for i, s := range segs {
		if remainingW <= 0 {
			break
		}
		segW := remainingW
		if i < len(segs)-1 {
			segW = int(math.Round(s.val / remainingTotal * float64(remainingW)))
			if segW < 1 && s.val > 0 {
				segW = 1
			}
			if segW > remainingW {
				segW = remainingW
			}
		}
		if segW <= 0 {
			continue
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(s.color).Render(strings.Repeat("█", segW)))
		remainingW -= segW
		remainingTotal -= s.val
		if remainingTotal <= 0 {
			remainingTotal = 1
		}
	}
	if remainingW > 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", remainingW)))
	}
	return sb.String()
}

func shortCompact(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.1fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%.1fk", v/1_000)
	}
	return fmt.Sprintf("%.0f", v)
}

func truncateToWidth(s string, maxW int) string {
	if maxW <= 0 || lipgloss.Width(s) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)+"…") > maxW {
		r = r[:len(r)-1]
	}
	if len(r) == 0 {
		return "…"
	}
	return string(r) + "…"
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
