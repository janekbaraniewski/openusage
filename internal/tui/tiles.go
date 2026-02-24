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

func tableLabelMaxLen(innerW int) int {
	if innerW < 60 {
		return tableLabelMaxLenNarrow
	}
	return tableLabelMaxLenWide
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

func (m Model) renderTile(snap core.UsageSnapshot, selected, modelMixExpanded bool, tileW, tileContentH int) string {
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
	sectionsByID := make(map[core.DashboardStandardSection]section)
	withSectionPadding := func(lines []string) []string {
		if len(lines) == 0 {
			return nil
		}
		s := []string{""}
		s = append(s, lines...)
		return s
	}
	addUsedKeys := func(dst map[string]bool, src map[string]bool) map[string]bool {
		if len(src) == 0 {
			return dst
		}
		if dst == nil {
			dst = make(map[string]bool, len(src))
		}
		for k := range src {
			dst[k] = true
		}
		return dst
	}
	appendOtherGroup := func(dst []string, lines []string) []string {
		if len(lines) == 0 {
			return dst
		}
		if len(dst) > 0 {
			dst = append(dst, "")
		}
		dst = append(dst, lines...)
		return dst
	}

	topUsageLines := m.buildTileGaugeLines(snap, widget, innerW)
	if di.summary != "" {
		topUsageLines = append(topUsageLines, tileHeroStyle.Render(truncate(di.summary)))
	}
	if di.detail != "" {
		topUsageLines = append(topUsageLines, tileSummaryStyle.Render(truncate(di.detail)))
	}
	if len(topUsageLines) > 0 {
		sectionsByID[core.DashboardSectionTopUsageProgress] = section{withSectionPadding(topUsageLines)}
	}

	compactMetricLines, compactMetricKeys := buildTileCompactMetricSummaryLines(snap, widget, innerW)

	modelBurnLines, modelBurnKeys := buildProviderModelCompositionLines(snap, innerW, modelMixExpanded)
	if len(modelBurnLines) > 0 {
		sectionsByID[core.DashboardSectionModelBurn] = section{withSectionPadding(modelBurnLines)}
	}
	compactMetricKeys = addUsedKeys(compactMetricKeys, modelBurnKeys)

	var clientBurnLines []string
	var clientBurnKeys map[string]bool
	if widget.ShowClientComposition {
		clientBurnLines, clientBurnKeys = buildProviderClientCompositionLines(snap, innerW, modelMixExpanded)
		if len(clientBurnLines) > 0 {
			sectionsByID[core.DashboardSectionClientBurn] = section{withSectionPadding(clientBurnLines)}
		}
	}
	compactMetricKeys = addUsedKeys(compactMetricKeys, clientBurnKeys)

	var toolBurnLines []string
	var toolBurnKeys map[string]bool
	if widget.ShowToolComposition {
		toolBurnLines, toolBurnKeys = buildProviderToolCompositionLines(snap, innerW, modelMixExpanded)
		if len(toolBurnLines) > 0 {
			sectionsByID[core.DashboardSectionToolUsage] = section{withSectionPadding(toolBurnLines)}
		}
	}
	compactMetricKeys = addUsedKeys(compactMetricKeys, toolBurnKeys)

	dailyUsageLines := buildProviderDailyTrendLines(snap, innerW)
	if len(dailyUsageLines) > 0 {
		sectionsByID[core.DashboardSectionDailyUsage] = section{withSectionPadding(dailyUsageLines)}
	}

	providerBurnLines, providerBurnKeys := buildProviderVendorCompositionLines(snap, innerW, modelMixExpanded)
	if len(providerBurnLines) > 0 {
		sectionsByID[core.DashboardSectionProviderBurn] = section{withSectionPadding(providerBurnLines)}
	}
	compactMetricKeys = addUsedKeys(compactMetricKeys, providerBurnKeys)

	var otherLines []string
	otherLines = appendOtherGroup(otherLines, compactMetricLines)

	geminiQuotaLines, geminiQuotaKeys := buildGeminiOtherQuotaLines(snap, innerW)
	otherLines = appendOtherGroup(otherLines, geminiQuotaLines)
	compactMetricKeys = addUsedKeys(compactMetricKeys, geminiQuotaKeys)

	metricLines := m.buildTileMetricLines(snap, widget, innerW, compactMetricKeys)
	otherLines = appendOtherGroup(otherLines, metricLines)

	if snap.Message != "" && snap.Status != core.StatusError {
		msg := snap.Message
		if len(msg) > innerW-3 {
			msg = msg[:innerW-6] + "..."
		}
		otherLines = appendOtherGroup(otherLines, []string{
			lipgloss.NewStyle().Foreground(colorSubtext).Italic(true).Render(msg),
		})
	}

	metaLines := buildTileMetaLines(snap, innerW)
	otherLines = appendOtherGroup(otherLines, metaLines)

	if len(headerMeta) == 0 {
		resetLines := buildTileResetLines(snap, widget, innerW, m.animFrame)
		otherLines = appendOtherGroup(otherLines, resetLines)
	}
	if len(otherLines) > 0 {
		sectionsByID[core.DashboardSectionOtherData] = section{withSectionPadding(otherLines)}
	}

	var sections []section
	for _, sectionID := range widget.EffectiveStandardSectionOrder() {
		if sectionID == core.DashboardSectionHeader {
			continue
		}
		sec, ok := sectionsByID[sectionID]
		if !ok || len(sec.lines) == 0 {
			continue
		}
		sections = append(sections, sec)
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

func (m Model) buildTileGaugeLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int) []string {
	if len(snap.Metrics) == 0 {
		return nil
	}

	keys := lo.Keys(snap.Metrics)
	sort.Strings(keys)
	keys = prioritizeMetricKeys(keys, widget.GaugePriority)

	// When GaugePriority is set, treat it as an allowlist — only those
	// metrics are eligible for gauge rendering.
	var gaugeAllowSet map[string]bool
	if len(widget.GaugePriority) > 0 {
		gaugeAllowSet = make(map[string]bool, len(widget.GaugePriority))
		for _, k := range widget.GaugePriority {
			gaugeAllowSet[k] = true
		}
	}

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
		if gaugeAllowSet != nil && !gaugeAllowSet[key] {
			continue
		}
		met := snap.Metrics[key]
		usedPct := metricUsedPercent(key, met)
		if usedPct < 0 {
			continue
		}

		label := gaugeLabel(widget, key, met.Window)
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

func gaugeLabel(widget core.DashboardWidget, key string, window ...string) string {
	overrides := map[string]string{
		"plan_percent_used":    "Plan Used",
		"plan_spend":           "Credits",
		"plan_total_spend_usd": "Total Credits",
		"spend_limit":          "Credit Limit",
		"individual_spend":     "My Credits",
	}

	if strings.HasPrefix(key, "rate_limit_") {
		w := ""
		if len(window) > 0 {
			w = window[0]
		}
		if w != "" {
			return "Usage " + w
		}
		return "Usage " + metricLabel(widget, strings.TrimPrefix(key, "rate_limit_"))
	}
	if label, ok := overrides[key]; ok {
		return label
	}
	return metricLabel(widget, key)
}

func metricUsedPercent(key string, met core.Metric) float64 {
	return core.MetricUsedPercent(key, met)
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

func buildTileCompactMetricSummaryLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int) ([]string, map[string]bool) {
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
		segments, usedKeys := collectCompactMetricSegments(spec, widget, snap.Metrics, consumed)
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

func collectCompactMetricSegments(spec compactMetricRowSpec, widget core.DashboardWidget, metrics map[string]core.Metric, consumed map[string]bool) ([]string, []string) {
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
		segment := compactMetricSegment(widget, key, met)
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
		keys := lo.Keys(metrics)
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

func compactMetricSegment(widget core.DashboardWidget, key string, met core.Metric) string {
	value := compactMetricValue(key, met)
	if value == "" {
		return ""
	}
	label := compactMetricLabel(widget, key)
	if label == "" {
		return value
	}
	return label + " " + value
}

func compactMetricLabel(widget core.DashboardWidget, key string) string {
	if widget.CompactMetricLabelOverrides != nil {
		if label, ok := widget.CompactMetricLabelOverrides[key]; ok && label != "" {
			return label
		}
	}

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
		"plan_spend":           "plan",
		"plan_included":        "incl",
		"plan_bonus":           "bonus",
		"spend_limit":          "cap",
		"individual_spend":     "mine",
		"plan_percent_used":    "used",
		"plan_total_spend_usd": "plan",
		"plan_limit_usd":       "limit",
		"credit_balance":       "balance",
		"credits":              "credits",
		"monthly_spend":        "month",
		"context_window":       "ctx",
		"messages_today":       "msgs",
		"sessions_today":       "sess",
		"tool_calls_today":     "tools",
		"chat_quota":           "chat",
		"completions_quota":    "comp",
		"rpm":                  "rpm",
		"tpm":                  "tpm",
		"rpd":                  "rpd",
		"tpd":                  "tpd",
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

func (m Model) buildTileMetricLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, skipKeys map[string]bool) []string {
	if len(snap.Metrics) == 0 {
		return nil
	}

	keys := lo.Keys(snap.Metrics)
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

		label := metricLabel(widget, key)
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

	if widget.SuppressZeroNonUsageMetrics && met.Used != nil && *met.Used == 0 && met.Limit == nil && met.Remaining == nil {
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

func buildTileMetaLines(snap core.UsageSnapshot, innerW int) []string {
	meta := snapshotMetaEntries(snap)
	if len(meta) == 0 {
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
		val, ok := meta[e.key]
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

func buildTileHeaderMetaLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, animFrame int) []string {
	var pills []string
	pills = append(pills, buildTileCyclePills(snap)...)
	pills = append(pills, buildTileResetPills(snap, widget, animFrame)...)
	return wrapTilePills(pills, innerW)
}

func buildTileCyclePills(snap core.UsageSnapshot) []string {
	var pills []string
	if pill := buildTileCyclePill("Billing", snapshotMeta(snap, "billing_cycle_start"), snapshotMeta(snap, "billing_cycle_end")); pill != "" {
		pills = append(pills, pill)
	}
	if pill := buildTileCyclePill("Usage 5h", snapshotMeta(snap, "block_start"), snapshotMeta(snap, "block_end")); pill != "" {
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
	"billing_block":        "Usage 5h",
	"billing_cycle_end":    "Billing",
	"quota_reset":          "Usage",
	"usage_five_hour":      "Usage 5h",
	"usage_one_day":        "Usage 1d",
	"usage_seven_day":      "Usage 7d",
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

func collectActiveResetEntries(snap core.UsageSnapshot, widget core.DashboardWidget) []resetEntry {
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
		pi := resetSortPriority(entries[i].key)
		pj := resetSortPriority(entries[j].key)
		if pi != pj {
			return pi < pj
		}
		if !entries[i].at.Equal(entries[j].at) {
			return entries[i].at.Before(entries[j].at)
		}
		return entries[i].label < entries[j].label
	})
	return entries
}

func resetSortPriority(key string) int {
	k := strings.TrimSpace(strings.TrimSuffix(key, "_reset"))
	order := map[string]int{
		"rate_limit_primary":               10,
		"rate_limit_secondary":             11,
		"rate_limit_code_review_primary":   12,
		"rate_limit_code_review_secondary": 13,
		"gh_core_rpm":                      20,
		"gh_search_rpm":                    21,
		"gh_graphql_rpm":                   22,
		"usage_five_hour":                  30,
		"usage_one_day":                    31,
		"usage_seven_day":                  32,
		"billing_block":                    40,
		"billing_cycle_end":                41,
		"quota_reset":                      42,
		"limit_reset":                      43,
		"key_expires":                      44,
		"rpm":                              50,
		"tpm":                              51,
		"rpd":                              52,
		"tpd":                              53,
		"rpm_headers":                      54,
		"tpm_headers":                      55,
	}
	if p, ok := order[k]; ok {
		return p
	}
	return 999
}

func resetLabelForKey(snap core.UsageSnapshot, widget core.DashboardWidget, key string) string {
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
		return metricLabel(widget, trimmed)
	}
	if met, ok := snap.Metrics[key]; ok && met.Window != "" {
		return metricLabel(widget, key)
	}
	return metricLabel(widget, trimmed)
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

func buildTileResetPills(snap core.UsageSnapshot, widget core.DashboardWidget, animFrame int) []string {
	_ = animFrame
	entries := collectActiveResetEntries(snap, widget)
	if len(entries) == 0 {
		return nil
	}
	if snap.ProviderID == "gemini_cli" {
		entries = filterGeminiPrimaryQuotaReset(entries, snap)
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

func buildTileResetLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, animFrame int) []string {
	return wrapTilePills(buildTileResetPills(snap, widget, animFrame), innerW)
}

type geminiQuotaEntry struct {
	key         string
	label       string
	usedPercent float64
	resetKey    string
	resetAt     time.Time
	hasReset    bool
}

func collectGeminiQuotaEntries(snap core.UsageSnapshot) []geminiQuotaEntry {
	if snap.ProviderID != "gemini_cli" {
		return nil
	}

	entries := make([]geminiQuotaEntry, 0)
	for key, metric := range snap.Metrics {
		if !strings.HasPrefix(key, "quota_model_") {
			continue
		}
		usedPct := metricUsedPercent(key, metric)
		if usedPct < 0 {
			continue
		}

		entry := geminiQuotaEntry{
			key:         key,
			label:       geminiQuotaLabelFromMetricKey(key),
			usedPercent: usedPct,
			resetKey:    key + "_reset",
		}
		if resetAt, ok := snap.Resets[entry.resetKey]; ok && !resetAt.IsZero() {
			entry.hasReset = true
			entry.resetAt = resetAt
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].usedPercent != entries[j].usedPercent {
			return entries[i].usedPercent > entries[j].usedPercent
		}
		return entries[i].label < entries[j].label
	})
	return entries
}

func geminiQuotaLabelFromMetricKey(metricKey string) string {
	base := strings.TrimPrefix(metricKey, "quota_model_")
	if base == "" {
		return metricKey
	}

	modelPart := base
	tokenType := ""
	if idx := strings.LastIndex(base, "_"); idx > 0 {
		modelPart = base[:idx]
		tokenType = base[idx+1:]
	}

	modelLabel := prettifyModelName(strings.ReplaceAll(modelPart, "_", "-"))
	tokenLabel := tokenType
	switch tokenType {
	case "requests":
		tokenLabel = "req"
	case "tokens":
		tokenLabel = "tok"
	}
	if tokenLabel == "" {
		return truncateToWidth(modelLabel, 28)
	}
	return truncateToWidth(modelLabel+" "+tokenLabel, 28)
}

func geminiPrimaryQuotaMetricKey(snap core.UsageSnapshot) string {
	entries := collectGeminiQuotaEntries(snap)
	if len(entries) > 0 {
		return entries[0].key
	}

	bestKey := ""
	bestUsed := -1.0
	for _, key := range []string{"quota", "quota_pro", "quota_flash"} {
		metric, ok := snap.Metrics[key]
		if !ok {
			continue
		}
		usedPct := metricUsedPercent(key, metric)
		if usedPct > bestUsed {
			bestUsed = usedPct
			bestKey = key
		}
	}
	return bestKey
}

func isGeminiQuotaResetKey(key string) bool {
	switch key {
	case "quota_reset", "quota_pro_reset", "quota_flash_reset":
		return true
	}
	return strings.HasPrefix(key, "quota_model_")
}

func filterGeminiPrimaryQuotaReset(entries []resetEntry, snap core.UsageSnapshot) []resetEntry {
	if len(entries) == 0 {
		return nil
	}

	primaryMetricKey := geminiPrimaryQuotaMetricKey(snap)
	primaryResetKey := ""
	if primaryMetricKey != "" {
		primaryResetKey = primaryMetricKey + "_reset"
	}

	var quotaEntries []resetEntry
	filtered := make([]resetEntry, 0, len(entries))
	for _, entry := range entries {
		if isGeminiQuotaResetKey(entry.key) {
			quotaEntries = append(quotaEntries, entry)
			continue
		}
		filtered = append(filtered, entry)
	}
	if len(quotaEntries) == 0 {
		return entries
	}

	chosen := quotaEntries[0]
	found := false
	if primaryResetKey != "" {
		for _, entry := range quotaEntries {
			if entry.key == primaryResetKey {
				chosen = entry
				found = true
				break
			}
		}
	}
	if !found {
		for _, fallbackKey := range []string{"quota_reset", "quota_pro_reset", "quota_flash_reset"} {
			for _, entry := range quotaEntries {
				if entry.key == fallbackKey {
					chosen = entry
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}

	filtered = append(filtered, chosen)
	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].at.Equal(filtered[j].at) {
			return filtered[i].at.Before(filtered[j].at)
		}
		return filtered[i].label < filtered[j].label
	})
	return filtered
}

func buildGeminiOtherQuotaLines(snap core.UsageSnapshot, innerW int) ([]string, map[string]bool) {
	entries := collectGeminiQuotaEntries(snap)
	if len(entries) <= 1 {
		return nil, nil
	}

	primaryKey := geminiPrimaryQuotaMetricKey(snap)
	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Other Usage"),
	}
	usedKeys := make(map[string]bool, len(entries))

	maxLabel := innerW / 2
	if maxLabel < 14 {
		maxLabel = 14
	}
	for _, entry := range entries {
		if entry.key == primaryKey {
			continue
		}

		value := fmt.Sprintf("%.1f%% used", entry.usedPercent)
		if entry.hasReset {
			remaining := time.Until(entry.resetAt)
			if remaining > 0 {
				value += " · " + formatHeaderDuration(remaining)
			}
		}

		lines = append(lines, renderDotLeaderRow(truncateToWidth(entry.label, maxLabel), value, innerW))
		usedKeys[entry.key] = true
	}

	if len(lines) <= 1 {
		return nil, nil
	}
	return lines, usedKeys
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
	name       string
	cost       float64
	input      float64
	output     float64
	requests   float64
	requests1d float64
	series     []core.TimePoint
}

type providerMixEntry struct {
	name     string
	cost     float64
	input    float64
	output   float64
	requests float64
}

type clientMixEntry struct {
	name       string
	total      float64
	input      float64
	output     float64
	cached     float64
	reasoning  float64
	requests   float64
	sessions   float64
	seriesKind string
	series     []core.TimePoint
}

type sourceMixEntry struct {
	name       string
	requests   float64
	requests1d float64
	series     []core.TimePoint
}

func buildProviderModelCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allModels, usedKeys := collectProviderModelMix(snap)
	if len(allModels) == 0 {
		return nil, nil
	}
	models, hiddenCount := limitModelMix(allModels, expanded, 5)
	modelColors := buildModelColorMap(allModels, snap.AccountID)

	totalCost := float64(0)
	totalTokens := float64(0)
	totalRequests := float64(0)
	for _, m := range allModels {
		totalCost += m.cost
		totalTokens += m.input + m.output
		totalRequests += m.requests
	}

	mode, total := selectBurnMode(totalTokens, totalCost, totalRequests)
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

	heading := "Model Burn (tokens)"
	switch mode {
	case "requests":
		heading = "Model Activity (requests)"
	case "cost":
		heading = "Model Burn (credits)"
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(heading),
		"  " + renderModelMixBar(allModels, total, barW, mode, modelColors),
	}

	for idx, model := range models {
		value := modelMixValue(model, mode)
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		label := prettifyModelName(model.name)
		colorDot := lipgloss.NewStyle().Foreground(colorForModel(modelColors, model.name)).Render("■")
		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)
		valueStr := fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(model.requests))
		switch mode {
		case "tokens":
			valueStr = fmt.Sprintf("%2.0f%% %s tok",
				pct,
				shortCompact(model.input+model.output),
			)
			if model.cost > 0 {
				valueStr += fmt.Sprintf(" · %s", formatUSD(model.cost))
			}
		case "cost":
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s",
				pct,
				shortCompact(model.input+model.output),
				formatUSD(model.cost),
			)
		case "requests":
			if model.requests1d > 0 {
				valueStr += fmt.Sprintf(" · today %s", shortCompact(model.requests1d))
			}
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	trendEntries := limitModelTrendEntries(models, expanded)
	if len(trendEntries) > 0 {
		lines = append(lines, dimStyle.Render("  Trend (daily by model)"))

		labelW := 12
		if innerW < 55 {
			labelW = 10
		}
		sparkW := innerW - labelW - 5
		if sparkW < 10 {
			sparkW = 10
		}
		if sparkW > 28 {
			sparkW = 28
		}

		for _, model := range trendEntries {
			values := make([]float64, 0, len(model.series))
			for _, point := range model.series {
				values = append(values, point.Value)
			}
			if len(values) < 2 {
				continue
			}
			label := truncateToWidth(prettifyModelName(model.name), labelW)
			spark := RenderSparkline(values, sparkW, colorForModel(modelColors, model.name))
			lines = append(lines, fmt.Sprintf("  %s %s",
				lipgloss.NewStyle().Foreground(colorSubtext).Width(labelW).Render(label),
				spark,
			))
		}
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

func limitModelTrendEntries(models []modelMixEntry, expanded bool) []modelMixEntry {
	maxVisible := 2
	if expanded {
		maxVisible = 4
	}

	trend := make([]modelMixEntry, 0, maxVisible)
	for _, model := range models {
		if len(model.series) < 2 {
			continue
		}
		trend = append(trend, model)
		if len(trend) >= maxVisible {
			break
		}
	}
	return trend
}

func buildModelColorMap(models []modelMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(models))
	if len(models) == 0 {
		return colors
	}

	base := stablePaletteOffset("model", providerID)
	for i, model := range models {
		colors[model.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForModel(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("model:"+name, "model")
}

func modelMixValue(model modelMixEntry, mode string) float64 {
	switch mode {
	case "tokens":
		return model.input + model.output
	case "cost":
		return model.cost
	default:
		return model.requests
	}
}

func selectBurnMode(totalTokens, totalCost, totalRequests float64) (mode string, total float64) {
	switch {
	case totalCost > 0:
		return "cost", totalCost
	case totalTokens > 0:
		return "tokens", totalTokens
	default:
		return "requests", totalRequests
	}
}

func collectProviderModelMix(snap core.UsageSnapshot) ([]modelMixEntry, map[string]bool) {
	type agg struct {
		cost       float64
		input      float64
		output     float64
		requests   float64
		requests1d float64
		series     []core.TimePoint
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
	recordRequests := func(name string, v float64, key string) {
		ensure(name).requests += v
		usedKeys[key] = true
	}
	recordRequests1d := func(name string, v float64, key string) {
		ensure(name).requests1d += v
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
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_requests_today"):
			recordRequests1d(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_requests_today"), *met.Used, key)
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_requests"):
			recordRequests(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_requests"), *met.Used, key)
		case strings.HasPrefix(key, "input_tokens_"):
			recordInput(strings.TrimPrefix(key, "input_tokens_"), *met.Used, key)
		case strings.HasPrefix(key, "output_tokens_"):
			recordOutput(strings.TrimPrefix(key, "output_tokens_"), *met.Used, key)
		}
	}

	for key, points := range snap.DailySeries {
		const prefix = "usage_model_"
		if !strings.HasPrefix(key, prefix) || len(points) == 0 {
			continue
		}
		name := strings.TrimPrefix(key, prefix)
		if name == "" {
			continue
		}
		m := ensure(name)
		m.series = points
		if m.requests <= 0 {
			m.requests = sumSeriesValues(points)
		}
	}

	models := make([]modelMixEntry, 0, len(byModel))
	for name, v := range byModel {
		if v.cost <= 0 && v.input <= 0 && v.output <= 0 && v.requests <= 0 && len(v.series) == 0 {
			continue
		}
		models = append(models, modelMixEntry{
			name:       name,
			cost:       v.cost,
			input:      v.input,
			output:     v.output,
			requests:   v.requests,
			requests1d: v.requests1d,
			series:     v.series,
		})
	}

	sort.Slice(models, func(i, j int) bool {
		ti := models[i].input + models[i].output
		tj := models[j].input + models[j].output
		if ti != tj {
			return ti > tj
		}
		if models[i].cost != models[j].cost {
			return models[i].cost > models[j].cost
		}
		if models[i].requests != models[j].requests {
			return models[i].requests > models[j].requests
		}
		return models[i].name < models[j].name
	})
	return models, usedKeys
}

func buildProviderVendorCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allProviders, usedKeys := collectProviderVendorMix(snap)
	if len(allProviders) == 0 {
		return nil, nil
	}
	providers, hiddenCount := limitProviderMix(allProviders, expanded, 4)
	providerColors := buildProviderColorMap(allProviders, snap.AccountID)

	totalCost := float64(0)
	totalTokens := float64(0)
	totalRequests := float64(0)
	for _, p := range allProviders {
		totalCost += p.cost
		totalTokens += p.input + p.output
		totalRequests += p.requests
	}

	mode, total := selectBurnMode(totalTokens, totalCost, totalRequests)
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

	heading := "Provider Burn (tokens)"
	if mode == "cost" {
		heading = "Provider Burn (credits)"
	} else if mode == "requests" {
		heading = "Provider Activity (requests)"
	}

	providerClients := make([]clientMixEntry, 0, len(allProviders))
	for _, p := range allProviders {
		value := p.requests
		if mode == "cost" {
			value = p.cost
		} else if mode == "tokens" {
			value = p.input + p.output
		}
		if value <= 0 {
			continue
		}
		providerClients = append(providerClients, clientMixEntry{name: p.name, total: value})
	}
	if len(providerClients) == 0 {
		return nil, nil
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(heading),
		"  " + renderClientMixBar(providerClients, total, barW, providerColors, "tokens"),
	}

	for idx, provider := range providers {
		value := provider.requests
		if mode == "cost" {
			value = provider.cost
		} else if mode == "tokens" {
			value = provider.input + provider.output
		}
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		label := prettifyModelName(provider.name)
		colorDot := lipgloss.NewStyle().Foreground(providerColors[provider.name]).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)

		valueStr := fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(provider.requests))
		if mode == "tokens" {
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s req",
				pct,
				shortCompact(provider.input+provider.output),
				shortCompact(provider.requests),
			)
			if provider.cost > 0 {
				valueStr += fmt.Sprintf(" · %s", formatUSD(provider.cost))
			}
		} else if mode == "cost" {
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s req · %s",
				pct,
				shortCompact(provider.input+provider.output),
				shortCompact(provider.requests),
				formatUSD(provider.cost),
			)
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}
	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more providers (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func collectProviderVendorMix(snap core.UsageSnapshot) ([]providerMixEntry, map[string]bool) {
	type agg struct {
		cost     float64
		input    float64
		output   float64
		requests float64
	}
	type providerFieldState struct {
		cost     bool
		input    bool
		output   bool
		requests bool
	}
	byProvider := make(map[string]*agg)
	usedKeys := make(map[string]bool)
	fieldState := make(map[string]*providerFieldState)

	ensure := func(name string) *agg {
		if _, ok := byProvider[name]; !ok {
			byProvider[name] = &agg{}
		}
		return byProvider[name]
	}
	ensureFieldState := func(name string) *providerFieldState {
		if _, ok := fieldState[name]; !ok {
			fieldState[name] = &providerFieldState{}
		}
		return fieldState[name]
	}

	recordCost := func(name string, v float64, key string) {
		ensure(name).cost += v
		ensureFieldState(name).cost = true
		usedKeys[key] = true
	}
	recordInput := func(name string, v float64, key string) {
		ensure(name).input += v
		ensureFieldState(name).input = true
		usedKeys[key] = true
	}
	recordOutput := func(name string, v float64, key string) {
		ensure(name).output += v
		ensureFieldState(name).output = true
		usedKeys[key] = true
	}
	recordRequests := func(name string, v float64, key string) {
		ensure(name).requests += v
		ensureFieldState(name).requests = true
		usedKeys[key] = true
	}

	// Pass 1: primary metrics (including non-BYOK cost) so BYOK fallback logic is order-independent.
	for key, met := range snap.Metrics {
		if met.Used == nil || !strings.HasPrefix(key, "provider_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_cost_usd"):
			recordCost(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_cost_usd"), *met.Used, key)
		case strings.HasSuffix(key, "_cost") && !strings.HasSuffix(key, "_byok_cost"):
			recordCost(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_cost"), *met.Used, key)
		case strings.HasSuffix(key, "_input_tokens"):
			recordInput(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_input_tokens"), *met.Used, key)
		case strings.HasSuffix(key, "_output_tokens"):
			recordOutput(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_output_tokens"), *met.Used, key)
		case strings.HasSuffix(key, "_requests"):
			recordRequests(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_requests"), *met.Used, key)
		}
	}
	// Pass 2: BYOK cost only when primary provider cost is absent.
	for key, met := range snap.Metrics {
		if met.Used == nil || !strings.HasPrefix(key, "provider_") || !strings.HasSuffix(key, "_byok_cost") {
			continue
		}
		base := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_byok_cost")
		if base == "" || ensureFieldState(base).cost {
			continue
		}
		recordCost(base, *met.Used, key)
	}

	meta := snapshotMetaEntries(snap)
	// Pass 3: raw fallback for primary cost fields (excluding BYOK), tokens, requests.
	for key, raw := range meta {
		if usedKeys[key] || !strings.HasPrefix(key, "provider_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_cost") && !strings.HasSuffix(key, "_byok_cost"):
			if v, ok := parseTileNumeric(raw); ok {
				baseKey := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_cost")
				if baseKey == "" || ensureFieldState(baseKey).cost {
					continue
				}
				recordCost(baseKey, v, key)
			}
		case strings.HasSuffix(key, "_input_tokens"), strings.HasSuffix(key, "_prompt_tokens"):
			if v, ok := parseTileNumeric(raw); ok {
				baseKey := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_input_tokens")
				baseKey = strings.TrimSuffix(baseKey, "_prompt_tokens")
				if baseKey == "" || ensureFieldState(baseKey).input {
					continue
				}
				recordInput(baseKey, v, key)
			}
		case strings.HasSuffix(key, "_output_tokens"), strings.HasSuffix(key, "_completion_tokens"):
			if v, ok := parseTileNumeric(raw); ok {
				baseKey := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_output_tokens")
				baseKey = strings.TrimSuffix(baseKey, "_completion_tokens")
				if baseKey == "" || ensureFieldState(baseKey).output {
					continue
				}
				recordOutput(baseKey, v, key)
			}
		case strings.HasSuffix(key, "_requests"):
			if v, ok := parseTileNumeric(raw); ok {
				baseKey := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_requests")
				if baseKey == "" || ensureFieldState(baseKey).requests {
					continue
				}
				recordRequests(baseKey, v, key)
			}
		}
	}
	// Pass 4: raw fallback for BYOK cost only when no primary cost exists.
	for key, raw := range meta {
		if usedKeys[key] || !strings.HasPrefix(key, "provider_") || !strings.HasSuffix(key, "_byok_cost") {
			continue
		}
		if v, ok := parseTileNumeric(raw); ok {
			baseKey := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_byok_cost")
			if baseKey == "" || ensureFieldState(baseKey).cost {
				continue
			}
			recordCost(baseKey, v, key)
		}
	}

	providers := make([]providerMixEntry, 0, len(byProvider))
	for name, v := range byProvider {
		if v.cost <= 0 && v.input <= 0 && v.output <= 0 && v.requests <= 0 {
			continue
		}
		providers = append(providers, providerMixEntry{
			name:     name,
			cost:     v.cost,
			input:    v.input,
			output:   v.output,
			requests: v.requests,
		})
	}

	sort.Slice(providers, func(i, j int) bool {
		ti := providers[i].input + providers[i].output
		tj := providers[j].input + providers[j].output
		if ti != tj {
			return ti > tj
		}
		if providers[i].cost != providers[j].cost {
			return providers[i].cost > providers[j].cost
		}
		return providers[i].requests > providers[j].requests
	})
	return providers, usedKeys
}

func limitProviderMix(providers []providerMixEntry, expanded bool, maxVisible int) ([]providerMixEntry, int) {
	if expanded || maxVisible <= 0 || len(providers) <= maxVisible {
		return providers, 0
	}
	return providers[:maxVisible], len(providers) - maxVisible
}

func buildProviderColorMap(providers []providerMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(providers))
	if len(providers) == 0 {
		return colors
	}

	base := stablePaletteOffset("provider", providerID)
	for i, provider := range providers {
		colors[provider.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func buildProviderDailyTrendLines(snap core.UsageSnapshot, innerW int) []string {
	type trendDef struct {
		label string
		keys  []string
		color lipgloss.Color
		unit  string
	}
	defs := []trendDef{
		{label: "Cost", keys: []string{"analytics_cost", "cost"}, color: colorTeal, unit: "USD"},
		{label: "Req", keys: []string{"analytics_requests", "requests"}, color: colorYellow, unit: "requests"},
		{label: "Tokens", keys: []string{"analytics_tokens"}, color: colorSapphire, unit: "tokens"},
	}

	lines := []string{}
	labelW := 8
	if innerW < 55 {
		labelW = 6
	}
	sparkW := innerW - labelW - 14
	if sparkW < 10 {
		sparkW = 10
	}
	if sparkW > 30 {
		sparkW = 30
	}

	for _, def := range defs {
		var points []core.TimePoint
		for _, key := range def.keys {
			if got, ok := snap.DailySeries[key]; ok && len(got) > 1 {
				points = got
				break
			}
		}
		if len(points) < 2 {
			continue
		}
		values := tailSeriesValues(points, 14)
		if len(values) < 2 {
			continue
		}

		last := values[len(values)-1]
		lastLabel := shortCompact(last)
		if def.unit == "USD" {
			lastLabel = formatUSD(last)
		}

		if len(lines) == 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Daily Usage"))
		}

		label := lipgloss.NewStyle().Foreground(colorSubtext).Width(labelW).Render(def.label)
		spark := RenderSparkline(values, sparkW, def.color)
		lines = append(lines, fmt.Sprintf("  %s %s %s", label, spark, dimStyle.Render(lastLabel)))
	}

	if len(lines) == 0 {
		return nil
	}
	return lines
}

func tailSeriesValues(points []core.TimePoint, max int) []float64 {
	if len(points) == 0 {
		return nil
	}
	if max > 0 && len(points) > max {
		points = points[len(points)-max:]
	}
	values := make([]float64, 0, len(points))
	for _, p := range points {
		values = append(values, p.Value)
	}
	return values
}

func buildProviderSourceCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allSources, usedKeys := collectProviderSourceMix(snap)
	if len(allSources) == 0 {
		return nil, nil
	}

	sources, hiddenCount := limitSourceMix(allSources, expanded, 6)
	sourceColors := buildSourceColorMap(allSources, snap.AccountID)

	total := float64(0)
	for _, source := range allSources {
		total += source.requests
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

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Source Mix (requests)"),
		"  " + renderSourceMixBar(allSources, total, barW, sourceColors),
	}

	for idx, source := range sources {
		if source.requests <= 0 {
			continue
		}
		pct := source.requests / total * 100
		label := prettifySourceName(source.name)
		sourceColor := colorForSource(sourceColors, source.name)
		colorDot := lipgloss.NewStyle().Foreground(sourceColor).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)

		valueStr := fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(source.requests))
		if source.requests1d > 0 {
			valueStr += fmt.Sprintf(" · today %s", shortCompact(source.requests1d))
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	trendEntries := limitSourceTrendEntries(sources, expanded)
	if len(trendEntries) > 0 {
		lines = append(lines, dimStyle.Render("  Trend (daily by source)"))

		labelW := 12
		if innerW < 55 {
			labelW = 10
		}
		sparkW := innerW - labelW - 5
		if sparkW < 10 {
			sparkW = 10
		}
		if sparkW > 28 {
			sparkW = 28
		}

		for _, source := range trendEntries {
			values := make([]float64, 0, len(source.series))
			for _, point := range source.series {
				values = append(values, point.Value)
			}
			if len(values) < 2 {
				continue
			}
			label := truncateToWidth(prettifySourceName(source.name), labelW)
			spark := RenderSparkline(values, sparkW, colorForSource(sourceColors, source.name))
			lines = append(lines, fmt.Sprintf("  %s %s",
				lipgloss.NewStyle().Foreground(colorSubtext).Width(labelW).Render(label),
				spark,
			))
		}
	}

	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more sources (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func collectProviderSourceMix(snap core.UsageSnapshot) ([]sourceMixEntry, map[string]bool) {
	bySource := make(map[string]*sourceMixEntry)
	usedKeys := make(map[string]bool)

	ensure := func(name string) *sourceMixEntry {
		if _, ok := bySource[name]; !ok {
			bySource[name] = &sourceMixEntry{name: name}
		}
		return bySource[name]
	}

	for key, met := range snap.Metrics {
		if met.Used == nil || !strings.HasPrefix(key, "source_") {
			continue
		}
		name, field, ok := parseSourceMetricKey(key)
		if !ok {
			continue
		}
		source := ensure(name)
		switch field {
		case "requests":
			source.requests = *met.Used
		case "requests_today":
			source.requests1d = *met.Used
		}
		usedKeys[key] = true
	}

	for key, points := range snap.DailySeries {
		const prefix = "usage_source_"
		if !strings.HasPrefix(key, prefix) || len(points) == 0 {
			continue
		}
		name := strings.TrimPrefix(key, prefix)
		if name == "" {
			continue
		}
		source := ensure(name)
		source.series = points
		if source.requests <= 0 {
			source.requests = sumSeriesValues(points)
		}
	}

	sources := make([]sourceMixEntry, 0, len(bySource))
	for _, source := range bySource {
		if source.requests <= 0 && source.requests1d <= 0 && len(source.series) == 0 {
			continue
		}
		sources = append(sources, *source)
	}

	sort.Slice(sources, func(i, j int) bool {
		if sources[i].requests == sources[j].requests {
			if sources[i].requests1d == sources[j].requests1d {
				return sources[i].name < sources[j].name
			}
			return sources[i].requests1d > sources[j].requests1d
		}
		return sources[i].requests > sources[j].requests
	})

	return sources, usedKeys
}

func parseSourceMetricKey(key string) (name, field string, ok bool) {
	const prefix = "source_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	for _, suffix := range []string{
		"_requests_today",
		"_requests",
	} {
		if strings.HasSuffix(rest, suffix) {
			return strings.TrimSuffix(rest, suffix), strings.TrimPrefix(suffix, "_"), true
		}
	}
	return "", "", false
}

func sourceAsClientBucket(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	if s == "" || s == "unknown" {
		return "other"
	}

	switch s {
	case "composer", "tab", "human", "vscode", "ide", "editor":
		return "ide"
	case "cli", "terminal", "background_agent", "agent", "agents", "cli_agents":
		return "cli_agents"
	case "desktop", "desktop_app":
		return "desktop_app"
	}

	if strings.Contains(s, "cli") || strings.Contains(s, "terminal") || strings.Contains(s, "agent") {
		return "cli_agents"
	}
	if strings.Contains(s, "compose") || strings.Contains(s, "tab") || strings.Contains(s, "ide") || strings.Contains(s, "editor") {
		return "ide"
	}
	return s
}

func limitSourceMix(sources []sourceMixEntry, expanded bool, maxVisible int) ([]sourceMixEntry, int) {
	if expanded || maxVisible <= 0 || len(sources) <= maxVisible {
		return sources, 0
	}
	return sources[:maxVisible], len(sources) - maxVisible
}

func limitSourceTrendEntries(sources []sourceMixEntry, expanded bool) []sourceMixEntry {
	maxVisible := 2
	if expanded {
		maxVisible = 4
	}

	trend := make([]sourceMixEntry, 0, maxVisible)
	for _, source := range sources {
		if len(source.series) < 2 {
			continue
		}
		trend = append(trend, source)
		if len(trend) >= maxVisible {
			break
		}
	}
	return trend
}

func prettifySourceName(name string) string {
	switch name {
	case "tab":
		return "Tab"
	case "composer":
		return "Composer"
	case "human":
		return "Human"
	case "cli":
		return "CLI"
	case "cli_agents":
		return "CLI Agents"
	case "agents":
		return "Agents"
	case "terminal":
		return "Terminal"
	case "unknown":
		return "Unknown"
	}

	parts := strings.Split(name, "_")
	for i := range parts {
		switch parts[i] {
		case "cli":
			parts[i] = "CLI"
		case "ide":
			parts[i] = "IDE"
		default:
			parts[i] = titleCase(parts[i])
		}
	}
	return strings.Join(parts, " ")
}

func buildSourceColorMap(sources []sourceMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(sources))
	if len(sources) == 0 {
		return colors
	}

	base := stablePaletteOffset("source", providerID)
	for i, source := range sources {
		colors[source.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForSource(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("source:"+name, "source")
}

func renderSourceMixBar(top []sourceMixEntry, total float64, barW int, colors map[string]lipgloss.Color) string {
	if len(top) == 0 || total <= 0 {
		return ""
	}

	type seg struct {
		val   float64
		color lipgloss.Color
	}

	segs := make([]seg, 0, len(top)+1)
	sumTop := float64(0)
	for _, source := range top {
		if source.requests <= 0 {
			continue
		}
		sumTop += source.requests
		segs = append(segs, seg{
			val:   source.requests,
			color: colorForSource(colors, source.name),
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

func buildProviderClientCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allClients, usedKeys := collectProviderClientMix(snap)
	if len(allClients) == 0 {
		return nil, nil
	}

	clients, hiddenCount := limitClientMix(allClients, expanded, 4)
	clientColors := buildClientColorMap(allClients, snap.AccountID)

	mode, total := selectClientMixMode(allClients)
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

	heading := "Client Burn (tokens)"
	if mode == "requests" {
		heading = "Client Activity (requests)"
	} else if mode == "sessions" {
		heading = "Client Activity (sessions)"
	}
	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(heading),
		"  " + renderClientMixBar(allClients, total, barW, clientColors, mode),
	}

	for idx, client := range clients {
		value := clientDisplayValue(client, mode)
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		label := prettifyClientName(client.name)
		clientColor := colorForClient(clientColors, client.name)
		colorDot := lipgloss.NewStyle().Foreground(clientColor).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)

		valueStr := fmt.Sprintf("%2.0f%% %s tok", pct, shortCompact(value))
		switch mode {
		case "requests":
			valueStr = fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(value))
			if client.sessions > 0 {
				valueStr += fmt.Sprintf(" · %s sess", shortCompact(client.sessions))
			}
		case "sessions":
			valueStr = fmt.Sprintf("%2.0f%% %s sess", pct, shortCompact(value))
		default:
			if client.requests > 0 {
				valueStr += fmt.Sprintf(" · %s req", shortCompact(client.requests))
			} else if client.sessions > 0 {
				valueStr += fmt.Sprintf(" · %s sess", shortCompact(client.sessions))
			}
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	trendEntries := limitClientTrendEntries(clients, expanded)
	if len(trendEntries) > 0 {
		lines = append(lines, dimStyle.Render("  Trend (daily by client)"))

		labelW := 12
		if innerW < 55 {
			labelW = 10
		}
		sparkW := innerW - labelW - 5
		if sparkW < 10 {
			sparkW = 10
		}
		if sparkW > 28 {
			sparkW = 28
		}

		for _, client := range trendEntries {
			values := make([]float64, 0, len(client.series))
			for _, point := range client.series {
				values = append(values, point.Value)
			}
			if len(values) < 2 {
				continue
			}
			label := truncateToWidth(prettifyClientName(client.name), labelW)
			spark := RenderSparkline(values, sparkW, colorForClient(clientColors, client.name))
			lines = append(lines, fmt.Sprintf("  %s %s",
				lipgloss.NewStyle().Foreground(colorSubtext).Width(labelW).Render(label),
				spark,
			))
		}
	}

	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more clients (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func collectProviderClientMix(snap core.UsageSnapshot) ([]clientMixEntry, map[string]bool) {
	byClient := make(map[string]*clientMixEntry)
	usedKeys := make(map[string]bool)

	ensure := func(name string) *clientMixEntry {
		if _, ok := byClient[name]; !ok {
			byClient[name] = &clientMixEntry{name: name}
		}
		return byClient[name]
	}
	tokenSeriesByClient := make(map[string]map[string]float64)
	usageClientSeriesByClient := make(map[string]map[string]float64)
	usageSourceSeriesByClient := make(map[string]map[string]float64)
	hasAllTimeRequests := make(map[string]bool)
	requestsTodayFallback := make(map[string]float64)

	for key, met := range snap.Metrics {
		if met.Used == nil {
			continue
		}
		if strings.HasPrefix(key, "client_") {
			name, field, ok := parseClientMetricKey(key)
			if !ok {
				continue
			}
			client := ensure(name)
			switch field {
			case "total_tokens":
				client.total = *met.Used
			case "input_tokens":
				client.input = *met.Used
			case "output_tokens":
				client.output = *met.Used
			case "cached_tokens":
				client.cached = *met.Used
			case "reasoning_tokens":
				client.reasoning = *met.Used
			case "requests":
				client.requests = *met.Used
				hasAllTimeRequests[name] = true
			case "sessions":
				client.sessions = *met.Used
			}
			usedKeys[key] = true
			continue
		}
		if strings.HasPrefix(key, "source_") {
			sourceName, field, ok := parseSourceMetricKey(key)
			if !ok {
				continue
			}
			clientName := sourceAsClientBucket(sourceName)
			client := ensure(clientName)
			switch field {
			case "requests":
				client.requests += *met.Used
				hasAllTimeRequests[clientName] = true
			case "requests_today":
				requestsTodayFallback[clientName] += *met.Used
			}
			usedKeys[key] = true
		}
	}
	for clientName, value := range requestsTodayFallback {
		if hasAllTimeRequests[clientName] {
			continue
		}
		client := ensure(clientName)
		if client.requests <= 0 {
			client.requests = value
		}
	}

	for key, points := range snap.DailySeries {
		if len(points) == 0 {
			continue
		}

		switch {
		case strings.HasPrefix(key, "tokens_client_"):
			name := strings.TrimPrefix(key, "tokens_client_")
			if name == "" {
				continue
			}
			mergeSeriesByDay(tokenSeriesByClient, name, points)
		case strings.HasPrefix(key, "usage_client_"):
			name := strings.TrimPrefix(key, "usage_client_")
			if name == "" {
				continue
			}
			mergeSeriesByDay(usageClientSeriesByClient, name, points)
		case strings.HasPrefix(key, "usage_source_"):
			name := sourceAsClientBucket(strings.TrimPrefix(key, "usage_source_"))
			if name == "" {
				continue
			}
			mergeSeriesByDay(usageSourceSeriesByClient, name, points)
		default:
			continue
		}
	}

	for name, pointsByDay := range tokenSeriesByClient {
		client := ensure(name)
		client.series = sortedSeriesFromByDay(pointsByDay)
		client.seriesKind = "tokens"
		if client.total <= 0 {
			client.total = sumSeriesValues(client.series)
		}
	}
	for name, pointsByDay := range usageClientSeriesByClient {
		client := ensure(name)
		if client.seriesKind == "tokens" {
			continue
		}
		client.series = sortedSeriesFromByDay(pointsByDay)
		client.seriesKind = "requests"
		if client.requests <= 0 {
			client.requests = sumSeriesValues(client.series)
		}
	}
	for name, pointsByDay := range usageSourceSeriesByClient {
		client := ensure(name)
		if client.seriesKind != "" {
			continue
		}
		client.series = sortedSeriesFromByDay(pointsByDay)
		client.seriesKind = "requests"
		if client.requests <= 0 {
			client.requests = sumSeriesValues(client.series)
		}
	}

	clients := make([]clientMixEntry, 0, len(byClient))
	for _, client := range byClient {
		if clientMixValue(*client) <= 0 && client.sessions <= 0 && client.requests <= 0 && len(client.series) == 0 {
			continue
		}
		clients = append(clients, *client)
	}

	sort.Slice(clients, func(i, j int) bool {
		vi := clientTokenValue(clients[i])
		vj := clientTokenValue(clients[j])
		if vi == vj {
			if clients[i].requests == clients[j].requests {
				if clients[i].sessions == clients[j].sessions {
					return clients[i].name < clients[j].name
				}
				return clients[i].sessions > clients[j].sessions
			}
			return clients[i].requests > clients[j].requests
		}
		return vi > vj
	})

	return clients, usedKeys
}

func parseClientMetricKey(key string) (name, field string, ok bool) {
	const prefix = "client_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	for _, suffix := range []string{
		"_total_tokens", "_input_tokens", "_output_tokens",
		"_cached_tokens", "_reasoning_tokens", "_requests", "_sessions",
	} {
		if strings.HasSuffix(rest, suffix) {
			return strings.TrimSuffix(rest, suffix), strings.TrimPrefix(suffix, "_"), true
		}
	}
	return "", "", false
}

func clientTokenValue(client clientMixEntry) float64 {
	if client.total > 0 {
		return client.total
	}
	if client.input > 0 || client.output > 0 || client.cached > 0 || client.reasoning > 0 {
		return client.input + client.output + client.cached + client.reasoning
	}
	return 0
}

func clientMixValue(client clientMixEntry) float64 {
	if v := clientTokenValue(client); v > 0 {
		return v
	}
	if client.requests > 0 {
		return client.requests
	}
	if len(client.series) > 0 {
		return sumSeriesValues(client.series)
	}
	return 0
}

func clientDisplayValue(client clientMixEntry, mode string) float64 {
	switch mode {
	case "sessions":
		return client.sessions
	case "requests":
		if client.requests > 0 {
			return client.requests
		}
		return sumSeriesValues(client.series)
	default:
		return clientMixValue(client)
	}
}

func selectClientMixMode(clients []clientMixEntry) (mode string, total float64) {
	totalTokens := float64(0)
	totalRequests := float64(0)
	totalSessions := float64(0)
	for _, client := range clients {
		totalTokens += clientTokenValue(client)
		totalRequests += client.requests
		totalSessions += client.sessions
	}
	if totalTokens > 0 {
		return "tokens", totalTokens
	}
	if totalRequests > 0 {
		return "requests", totalRequests
	}
	return "sessions", totalSessions
}

func sumSeriesValues(points []core.TimePoint) float64 {
	total := float64(0)
	for _, p := range points {
		total += p.Value
	}
	return total
}

func mergeSeriesByDay(seriesByClient map[string]map[string]float64, client string, points []core.TimePoint) {
	if client == "" || len(points) == 0 {
		return
	}
	if seriesByClient[client] == nil {
		seriesByClient[client] = make(map[string]float64)
	}
	for _, point := range points {
		if point.Date == "" {
			continue
		}
		seriesByClient[client][point.Date] += point.Value
	}
}

func sortedSeriesFromByDay(pointsByDay map[string]float64) []core.TimePoint {
	if len(pointsByDay) == 0 {
		return nil
	}
	days := lo.Keys(pointsByDay)
	sort.Strings(days)

	points := make([]core.TimePoint, 0, len(days))
	for _, day := range days {
		points = append(points, core.TimePoint{
			Date:  day,
			Value: pointsByDay[day],
		})
	}
	return points
}

func limitClientMix(clients []clientMixEntry, expanded bool, maxVisible int) ([]clientMixEntry, int) {
	if expanded || maxVisible <= 0 || len(clients) <= maxVisible {
		return clients, 0
	}
	return clients[:maxVisible], len(clients) - maxVisible
}

func limitClientTrendEntries(clients []clientMixEntry, expanded bool) []clientMixEntry {
	maxVisible := 2
	if expanded {
		maxVisible = 4
	}

	trend := make([]clientMixEntry, 0, maxVisible)
	for _, client := range clients {
		if len(client.series) < 2 {
			continue
		}
		trend = append(trend, client)
		if len(trend) >= maxVisible {
			break
		}
	}
	return trend
}

func prettifyClientName(name string) string {
	switch name {
	case "cli":
		return "CLI"
	case "ide":
		return "IDE"
	case "exec":
		return "Exec"
	case "desktop_app":
		return "Desktop App"
	case "other":
		return "Other"
	}

	parts := strings.Split(name, "_")
	for i := range parts {
		switch parts[i] {
		case "cli":
			parts[i] = "CLI"
		case "ide":
			parts[i] = "IDE"
		case "api":
			parts[i] = "API"
		default:
			parts[i] = titleCase(parts[i])
		}
	}
	return strings.Join(parts, " ")
}

func buildClientColorMap(clients []clientMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(clients))
	if len(clients) == 0 {
		return colors
	}

	base := stablePaletteOffset("client", providerID)
	for i, client := range clients {
		colors[client.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForClient(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("client:"+name, "client")
}

func stablePaletteOffset(prefix, value string) int {
	key := prefix + ":" + value
	hash := 0
	for _, ch := range key {
		hash = hash*31 + int(ch)
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}

func distributedPaletteColor(base, position int) lipgloss.Color {
	if len(modelColorPalette) == 0 {
		return colorSubtext
	}
	idx := distributedPaletteIndex(base, position, len(modelColorPalette))
	return modelColorPalette[idx]
}

func distributedPaletteIndex(base, position, size int) int {
	if size <= 0 {
		return 0
	}
	base %= size
	if base < 0 {
		base += size
	}
	step := distributedPaletteStep(size)
	idx := (base + position*step) % size
	if idx < 0 {
		idx += size
	}
	return idx
}

func distributedPaletteStep(size int) int {
	if size <= 1 {
		return 1
	}
	step := size/2 + 1
	for gcdInt(step, size) != 1 {
		step++
	}
	return step
}

func gcdInt(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func renderClientMixBar(top []clientMixEntry, total float64, barW int, colors map[string]lipgloss.Color, mode string) string {
	if len(top) == 0 || total <= 0 {
		return ""
	}

	type seg struct {
		val   float64
		color lipgloss.Color
	}

	segs := make([]seg, 0, len(top)+1)
	sumTop := float64(0)
	for _, client := range top {
		value := clientDisplayValue(client, mode)
		if value <= 0 {
			continue
		}
		sumTop += value
		segs = append(segs, seg{
			val:   value,
			color: colorForClient(colors, client.name),
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

func parseTileNumeric(raw string) (float64, bool) {
	s := strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if s == "" {
		return 0, false
	}
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSuffix(s, "%")
	if idx := strings.IndexByte(s, ' '); idx > 0 {
		s = s[:idx]
	}
	if idx := strings.IndexByte(s, '/'); idx > 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func renderModelMixBar(models []modelMixEntry, total float64, barW int, mode string, colors map[string]lipgloss.Color) string {
	if len(models) == 0 || total <= 0 {
		return ""
	}

	type seg struct {
		val   float64
		color lipgloss.Color
	}
	segs := make([]seg, 0, len(models)+1)
	sumTop := float64(0)
	for _, m := range models {
		v := modelMixValue(m, mode)
		if v <= 0 {
			continue
		}
		sumTop += v
		segs = append(segs, seg{
			val:   v,
			color: colorForModel(colors, m.name),
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

type toolMixEntry struct {
	name  string
	count float64
}

func buildProviderToolCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allTools, usedKeys := collectProviderToolMix(snap)
	if len(allTools) == 0 {
		return nil, nil
	}

	tools, hiddenCount := limitToolMix(allTools, expanded, 4)
	toolColors := buildToolColorMap(allTools, snap.AccountID)

	totalCalls := float64(0)
	for _, tool := range allTools {
		totalCalls += tool.count
	}
	if totalCalls <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Tool Usage (calls)"),
		"  " + renderToolMixBar(allTools, totalCalls, barW, toolColors),
	}

	for idx, tool := range tools {
		if tool.count <= 0 {
			continue
		}
		pct := tool.count / totalCalls * 100
		label := tool.name
		toolColor := colorForTool(toolColors, tool.name)
		colorDot := lipgloss.NewStyle().Foreground(toolColor).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)

		valueStr := fmt.Sprintf("%2.0f%% %s calls", pct, shortCompact(tool.count))
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more tools (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func collectProviderToolMix(snap core.UsageSnapshot) ([]toolMixEntry, map[string]bool) {
	byTool := make(map[string]float64)
	usedKeys := make(map[string]bool)

	for key, met := range snap.Metrics {
		if met.Used == nil || !strings.HasPrefix(key, "tool_") || strings.HasSuffix(key, "_today") {
			continue
		}
		name := strings.TrimPrefix(key, "tool_")
		if name == "" {
			continue
		}
		byTool[name] = *met.Used
		usedKeys[key] = true
	}

	tools := make([]toolMixEntry, 0, len(byTool))
	for name, count := range byTool {
		if count <= 0 {
			continue
		}
		tools = append(tools, toolMixEntry{name: name, count: count})
	}

	sort.Slice(tools, func(i, j int) bool {
		if tools[i].count == tools[j].count {
			return tools[i].name < tools[j].name
		}
		return tools[i].count > tools[j].count
	})

	return tools, usedKeys
}

func limitToolMix(tools []toolMixEntry, expanded bool, maxVisible int) ([]toolMixEntry, int) {
	if expanded || maxVisible <= 0 || len(tools) <= maxVisible {
		return tools, 0
	}
	return tools[:maxVisible], len(tools) - maxVisible
}

func buildToolColorMap(tools []toolMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(tools))
	if len(tools) == 0 {
		return colors
	}

	base := stablePaletteOffset("tool", providerID)
	for i, tool := range tools {
		colors[tool.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForTool(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("tool:"+name, "tool")
}

func renderToolMixBar(top []toolMixEntry, total float64, barW int, colors map[string]lipgloss.Color) string {
	if len(top) == 0 || total <= 0 {
		return ""
	}

	type seg struct {
		val   float64
		color lipgloss.Color
	}

	segs := make([]seg, 0, len(top)+1)
	sumTop := float64(0)
	for _, tool := range top {
		if tool.count <= 0 {
			continue
		}
		sumTop += tool.count
		segs = append(segs, seg{
			val:   tool.count,
			color: colorForTool(colors, tool.name),
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
