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

type DetailTab int

const (
	TabAll  DetailTab = 0 // show everything
	TabDyn1 DetailTab = 1 // first dynamic group
)

func DetailTabs(snap core.UsageSnapshot) []string {
	tabs := []string{"All"}
	if len(snap.Metrics) > 0 {
		groups := groupMetrics(snap.Metrics, dashboardWidget(snap.ProviderID), detailWidget(snap.ProviderID))
		for _, g := range groups {
			tabs = append(tabs, g.title)
		}
	}
	if hasAnalyticsModelData(snap) {
		tabs = append(tabs, "Models")
	}
	if hasLanguageMetrics(snap) {
		tabs = append(tabs, "Languages")
	}
	if hasMCPMetrics(snap) {
		tabs = append(tabs, "MCP Usage")
	}
	if hasChartableSeries(snap.DailySeries) {
		tabs = append(tabs, "Trends")
	}
	if len(snap.Resets) > 0 {
		tabs = append(tabs, "Timers")
	}
	if len(snap.Attributes) > 0 || len(snap.Diagnostics) > 0 || len(snap.Raw) > 0 {
		tabs = append(tabs, "Info")
	}
	return tabs
}

func RenderDetailContent(snap core.UsageSnapshot, w int, warnThresh, critThresh float64, activeTab int) string {
	var sb strings.Builder
	widget := dashboardWidget(snap.ProviderID)
	details := detailWidget(snap.ProviderID)

	renderDetailHeader(&sb, snap, w)
	sb.WriteString("\n")

	tabs := DetailTabs(snap)
	if activeTab >= len(tabs) {
		activeTab = 0
	}

	renderTabBar(&sb, tabs, activeTab, w)

	if len(snap.Metrics) == 0 && activeTab == 0 {
		if snap.Message != "" {
			sb.WriteString("\n")
			sb.WriteString(dimStyle.Render("  " + snap.Message))
			sb.WriteString("\n")
		}
		return sb.String()
	}

	tabName := tabs[activeTab]
	showAll := tabName == "All"
	showTimers := tabName == "Timers" || showAll
	showInfo := tabName == "Info" || showAll

	// Extract burn rate from metrics for spending section.
	burnRate := float64(0)
	if brm, ok := snap.Metrics["burn_rate"]; ok && brm.Used != nil {
		burnRate = *brm.Used
	}

	if len(snap.Metrics) > 0 {
		groups := groupMetrics(snap.Metrics, widget, details)
		for _, group := range groups {
			if showAll || group.title == tabName {
				renderMetricGroup(&sb, snap, group, widget, details, w, warnThresh, critThresh, snap.DailySeries, burnRate)
			}
		}
	}

	showModels := tabName == "Models" || showAll
	if showModels && hasAnalyticsModelData(snap) {
		sb.WriteString("\n")
		renderDetailSectionHeader(&sb, "Models", w)
		renderModelsSection(&sb, snap, widget, w)
	}

	// Languages section — dispatched directly (needs full snapshot metrics).
	showLanguages := tabName == "Languages" || showAll
	if showLanguages && hasLanguageMetrics(snap) {
		sb.WriteString("\n")
		renderDetailSectionHeader(&sb, "Languages", w)
		renderLanguagesSection(&sb, snap, w)
	}

	// MCP Usage section — dispatched directly (needs full snapshot metrics).
	showMCP := tabName == "MCP Usage" || showAll
	if showMCP && hasMCPMetrics(snap) {
		sb.WriteString("\n")
		renderDetailSectionHeader(&sb, "MCP Usage", w)
		renderMCPSection(&sb, snap, w)
	}

	// Trends section — dispatched directly (needs full snapshot DailySeries).
	showTrends := tabName == "Trends" || showAll
	if showTrends && hasChartableSeries(snap.DailySeries) {
		sb.WriteString("\n")
		renderDetailSectionHeader(&sb, "Trends", w)
		renderTrendsSection(&sb, snap, widget, w)
	}

	if showTimers && len(snap.Resets) > 0 {
		sb.WriteString("\n")
		renderTimersSection(&sb, snap.Resets, widget, w)
	}

	hasInfoData := len(snap.Attributes) > 0 || len(snap.Diagnostics) > 0 || len(snap.Raw) > 0
	if showInfo && hasInfoData {
		sb.WriteString("\n")
		renderInfoSection(&sb, snap, widget, w)
	}

	age := time.Since(snap.Timestamp)
	if age > 60*time.Second {
		sb.WriteString("\n")
		warnBox := lipgloss.NewStyle().
			Foreground(colorYellow).
			Background(colorSurface0).
			Padding(0, 1).
			Bold(true).
			Render(fmt.Sprintf("⚠ Data is %s old — press r to refresh", formatDuration(age)))
		sb.WriteString("  " + warnBox + "\n")
	}

	return sb.String()
}

func renderTabBar(sb *strings.Builder, tabs []string, active int, w int) {
	if len(tabs) <= 1 {
		return // no point showing tabs when there's only "All"
	}

	var parts []string
	for i, t := range tabs {
		if i == active {
			parts = append(parts, tabActiveStyle.Render(t))
		} else {
			parts = append(parts, tabInactiveStyle.Render(t))
		}
	}

	tabLine := "  " + strings.Join(parts, "")
	sb.WriteString(tabLine + "\n")

	sepLen := w - 2
	if sepLen < 4 {
		sepLen = 4
	}
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface2).Render(strings.Repeat("─", sepLen)) + "\n")
}

func renderDetailHeader(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	di := computeDisplayInfo(snap, dashboardWidget(snap.ProviderID))

	innerW := w - 6 // card border + padding eats ~6 chars
	if innerW < 20 {
		innerW = 20
	}

	var cardLines []string

	statusPill := StatusPill(snap.Status)
	pillW := lipgloss.Width(statusPill)

	name := snap.AccountID
	maxName := innerW - pillW - 2
	if maxName < 8 {
		maxName = 8
	}
	if len(name) > maxName {
		name = name[:maxName-1] + "…"
	}

	nameRendered := detailHeroNameStyle.Render(name)
	nameW := lipgloss.Width(nameRendered)
	gap1 := innerW - nameW - pillW
	if gap1 < 1 {
		gap1 = 1
	}
	line1 := nameRendered + strings.Repeat(" ", gap1) + statusPill
	cardLines = append(cardLines, line1)

	var line2Parts []string
	if di.tagEmoji != "" && di.tagLabel != "" {
		line2Parts = append(line2Parts, CategoryTag(di.tagEmoji, di.tagLabel))
	}
	line2Parts = append(line2Parts, dimStyle.Render(snap.ProviderID))
	line2 := strings.Join(line2Parts, " "+dimStyle.Render("·")+" ")
	cardLines = append(cardLines, line2)

	var metaTags []string

	if email := snapshotMeta(snap, "account_email"); email != "" {
		metaTags = append(metaTags, MetaTagHighlight("✉", email))
	}

	if planName := snapshotMeta(snap, "plan_name"); planName != "" {
		metaTags = append(metaTags, MetaTag("◆", planName))
	}
	if planType := snapshotMeta(snap, "plan_type"); planType != "" {
		metaTags = append(metaTags, MetaTag("◇", planType))
	}
	if membership := snapshotMeta(snap, "membership_type"); membership != "" {
		metaTags = append(metaTags, MetaTag("👤", membership))
	}
	if team := snapshotMeta(snap, "team_membership"); team != "" {
		metaTags = append(metaTags, MetaTag("🏢", team))
	}
	if org := snapshotMeta(snap, "organization_name"); org != "" {
		metaTags = append(metaTags, MetaTag("🏢", org))
	}

	if model := snapshotMeta(snap, "active_model"); model != "" {
		metaTags = append(metaTags, MetaTag("⬡", model))
	}
	if cliVer := snapshotMeta(snap, "cli_version"); cliVer != "" {
		metaTags = append(metaTags, MetaTag("⌘", "v"+cliVer))
	}

	if planPrice := snapshotMeta(snap, "plan_price"); planPrice != "" {
		metaTags = append(metaTags, MetaTag("$", planPrice))
	}
	if credits := snapshotMeta(snap, "credits"); credits != "" {
		metaTags = append(metaTags, MetaTag("💳", credits))
	}

	if oauth := snapshotMeta(snap, "oauth_status"); oauth != "" {
		metaTags = append(metaTags, MetaTag("🔒", oauth))
	}
	if sub := snapshotMeta(snap, "subscription_status"); sub != "" {
		metaTags = append(metaTags, MetaTag("✓", sub))
	}

	if len(metaTags) > 0 {
		tagRows := wrapTags(metaTags, innerW)
		for _, row := range tagRows {
			cardLines = append(cardLines, row)
		}
	}

	cardLines = append(cardLines, "")

	if snap.Message != "" {
		msg := snap.Message
		if lipgloss.Width(msg) > innerW {
			msg = msg[:innerW-3] + "..."
		}
		cardLines = append(cardLines, lipgloss.NewStyle().Foreground(colorText).Italic(true).Render(msg))
	}

	if di.gaugePercent >= 0 {
		gaugeW := innerW - 10
		if gaugeW < 12 {
			gaugeW = 12
		}
		if gaugeW > 40 {
			gaugeW = 40
		}
		heroGauge := RenderGauge(di.gaugePercent, gaugeW, 0.3, 0.1) // use standard thresholds
		cardLines = append(cardLines, heroGauge)
		if di.summary != "" {
			summaryLine := heroLabelStyle.Render(di.summary)
			if di.detail != "" {
				summaryLine += dimStyle.Render("  ·  ") + heroLabelStyle.Render(di.detail)
			}
			cardLines = append(cardLines, summaryLine)
		}
	} else if di.summary != "" && snap.Message == "" {
		cardLines = append(cardLines, heroValueStyle.Render(di.summary))
		if di.detail != "" {
			cardLines = append(cardLines, heroLabelStyle.Render(di.detail))
		}
	}

	timeStr := snap.Timestamp.Format("15:04:05")
	age := time.Since(snap.Timestamp)
	if age > 60*time.Second {
		timeStr = fmt.Sprintf("%s (%s ago)", snap.Timestamp.Format("15:04:05"), formatDuration(age))
	}
	cardLines = append(cardLines, dimStyle.Render("⏱ "+timeStr))

	cardContent := strings.Join(cardLines, "\n")
	borderColor := StatusBorderColor(snap.Status)
	card := detailHeaderCardStyle.
		Width(innerW + 2). // +2 for padding
		BorderForeground(borderColor).
		Render(cardContent)

	sb.WriteString(card)
	sb.WriteString("\n")
}

func wrapTags(tags []string, maxWidth int) []string {
	if len(tags) == 0 {
		return nil
	}
	var rows []string
	currentRow := ""
	currentW := 0
	sep := " "
	sepW := 1

	for _, tag := range tags {
		tagW := lipgloss.Width(tag)
		if currentW > 0 && currentW+sepW+tagW > maxWidth {
			rows = append(rows, currentRow)
			currentRow = tag
			currentW = tagW
		} else {
			if currentW > 0 {
				currentRow += sep
				currentW += sepW
			}
			currentRow += tag
			currentW += tagW
		}
	}
	if currentRow != "" {
		rows = append(rows, currentRow)
	}
	return rows
}

type metricGroup struct {
	title   string
	entries []metricEntry
	order   int
}

type metricEntry struct {
	key    string
	label  string
	metric core.Metric
}

func groupMetrics(metrics map[string]core.Metric, widget core.DashboardWidget, details core.DetailWidget) []metricGroup {
	groups := make(map[string]*metricGroup)

	for key, m := range metrics {
		// MCP metrics are rendered in their own dedicated section.
		if strings.HasPrefix(key, "mcp_") {
			continue
		}
		groupName, label, order := classifyMetric(key, m, widget, details)
		g, ok := groups[groupName]
		if !ok {
			g = &metricGroup{title: groupName, order: order}
			groups[groupName] = g
		}
		g.entries = append(g.entries, metricEntry{key: key, label: label, metric: m})
	}

	result := make([]metricGroup, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g.entries, func(i, j int) bool {
			return g.entries[i].key < g.entries[j].key
		})
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].order != result[j].order {
			return result[i].order < result[j].order
		}
		return result[i].title < result[j].title
	})

	return result
}

func classifyMetric(key string, m core.Metric, widget core.DashboardWidget, details core.DetailWidget) (group, label string, order int) {
	return core.ClassifyDetailMetric(key, m, widget, details)
}

func metricLabel(widget core.DashboardWidget, key string) string {
	return core.MetricLabel(widget, key)
}

func titleCase(s string) string {
	if len(s) <= 1 {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func renderMetricGroup(sb *strings.Builder, snap core.UsageSnapshot, group metricGroup, widget core.DashboardWidget, details core.DetailWidget, w int, warnThresh, critThresh float64, series map[string][]core.TimePoint, burnRate float64) {
	sb.WriteString("\n")
	renderDetailSectionHeader(sb, group.title, w)

	// Zero-value suppression: filter out zero-value metrics when the provider opts in.
	entries := group.entries
	if widget.SuppressZeroNonUsageMetrics || len(widget.SuppressZeroMetricKeys) > 0 {
		entries = filterNonZeroEntries(entries, widget)
	}

	switch details.SectionStyle(group.title) {
	case core.DetailSectionStyleUsage:
		renderUsageSection(sb, entries, w, warnThresh, critThresh)
	case core.DetailSectionStyleSpending:
		renderSpendingSection(sb, entries, w, burnRate)
	case core.DetailSectionStyleTokens:
		renderTokensSection(sb, snap, entries, widget, w, series)
	case core.DetailSectionStyleActivity:
		renderActivitySection(sb, entries, widget, w, series)
	case core.DetailSectionStyleLanguages:
		renderListSection(sb, entries, w)
	default:
		renderListSection(sb, entries, w)
	}
}

func renderListSection(sb *strings.Builder, entries []metricEntry, w int) {
	labelW := sectionLabelWidth(w)
	for _, e := range entries {
		val := formatMetricValue(e.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(labelW).Render(e.label), valueStyle.Render(val)))
	}
}

func renderUsageSection(sb *strings.Builder, entries []metricEntry, w int, warnThresh, critThresh float64) {
	labelW := sectionLabelWidth(w)

	var usageEntries []metricEntry
	var gaugeEntries []metricEntry

	for _, e := range entries {
		m := e.metric
		if m.Remaining != nil && m.Limit != nil && m.Unit != "%" && m.Unit != "USD" {
			usageEntries = append(usageEntries, e)
		} else {
			gaugeEntries = append(gaugeEntries, e)
		}
	}

	for _, entry := range gaugeEntries {
		renderGaugeEntry(sb, entry, labelW, w, warnThresh, critThresh)
	}

	if len(usageEntries) > 0 {
		if len(gaugeEntries) > 0 {
			sb.WriteString("\n")
		}
		renderUsageTable(sb, usageEntries, w, warnThresh, critThresh)
	}
}

func renderSpendingSection(sb *strings.Builder, entries []metricEntry, w int, burnRate float64) {
	labelW := sectionLabelWidth(w)
	gaugeW := sectionGaugeWidth(w, labelW)

	var modelCosts []metricEntry
	var otherCosts []metricEntry

	for _, e := range entries {
		if isModelCostKey(e.key) {
			modelCosts = append(modelCosts, e)
		} else {
			otherCosts = append(otherCosts, e)
		}
	}

	for _, e := range otherCosts {
		if e.metric.Used != nil && e.metric.Limit != nil && *e.metric.Limit > 0 {
			color := colorTeal
			if *e.metric.Used >= *e.metric.Limit*0.8 {
				color = colorRed
			} else if *e.metric.Used >= *e.metric.Limit*0.5 {
				color = colorYellow
			}
			line := RenderBudgetGauge(e.label, *e.metric.Used, *e.metric.Limit, gaugeW, labelW, color, burnRate)
			sb.WriteString(line + "\n")
		} else {
			val := formatMetricValue(e.metric)
			vs := metricValueStyle
			if !strings.Contains(val, "$") && !strings.Contains(val, "USD") {
				vs = valueStyle
			}
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				labelStyle.Width(labelW).Render(e.label), vs.Render(val)))
		}
	}

	if len(modelCosts) > 0 {
		if len(otherCosts) > 0 {
			sb.WriteString("\n")
		}
		renderModelCostsTable(sb, modelCosts, w)
	}
}

func renderActivitySection(sb *strings.Builder, entries []metricEntry, widget core.DashboardWidget, w int, series map[string][]core.TimePoint) {
	labelW := sectionLabelWidth(w)

	for _, e := range entries {
		val := formatMetricValue(e.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(labelW).Render(e.label), valueStyle.Render(val)))
	}

	renderSectionSparklines(sb, widget, w, series, []string{
		"messages", "sessions", "tool_calls",
	})
}

func renderTimersSection(sb *strings.Builder, resets map[string]time.Time, widget core.DashboardWidget, w int) {
	labelW := sectionLabelWidth(w)
	renderDetailSectionHeader(sb, "Timers", w)

	timerKeys := lo.Keys(resets)
	sort.Strings(timerKeys)

	for _, k := range timerKeys {
		t := resets[k]
		label := metricLabel(widget, k)
		remaining := time.Until(t)
		dateStr := t.Format("Jan 02 15:04")

		var urgency string
		if remaining <= 0 {
			urgency = dimStyle.Render("○")
			sb.WriteString(fmt.Sprintf("  %s  %s  %s (expired)\n",
				urgency,
				labelStyle.Width(labelW).Render(label),
				dimStyle.Render(dateStr),
			))
		} else {
			switch {
			case remaining < 15*time.Minute:
				urgency = lipgloss.NewStyle().Foreground(colorCrit).Render("●")
			case remaining < time.Hour:
				urgency = lipgloss.NewStyle().Foreground(colorWarn).Render("●")
			default:
				urgency = lipgloss.NewStyle().Foreground(colorOK).Render("●")
			}
			sb.WriteString(fmt.Sprintf("  %s  %s  %s (in %s)\n",
				urgency,
				labelStyle.Width(labelW).Render(label),
				valueStyle.Render(dateStr),
				tealStyle.Render(formatDuration(remaining)),
			))
		}
	}
}

func renderSectionSparklines(sb *strings.Builder, widget core.DashboardWidget, w int, series map[string][]core.TimePoint, candidates []string) {
	if len(series) == 0 {
		return
	}

	sparkW := w - 8
	if sparkW < 12 {
		sparkW = 12
	}
	if sparkW > 60 {
		sparkW = 60
	}

	colors := []lipgloss.Color{colorTeal, colorSapphire, colorGreen, colorPeach}
	colorIdx := 0

	for _, key := range candidates {
		points, ok := series[key]
		if !ok || len(points) < 2 {
			continue
		}
		values := make([]float64, len(points))
		for i, p := range points {
			values[i] = p.Value
		}
		c := colors[colorIdx%len(colors)]
		colorIdx++
		spark := RenderSparkline(values, sparkW, c)
		label := metricLabel(widget, key)
		sb.WriteString(fmt.Sprintf("  %s %s\n", dimStyle.Render(label), spark))
	}

	rendered := make(map[string]bool)
	for _, c := range candidates {
		rendered[c] = true
	}

	for _, candidate := range candidates {
		prefix := candidate
		if !strings.HasSuffix(prefix, "_") {
			prefix += "_"
		}
		for key, points := range series {
			if rendered[key] || len(points) < 2 {
				continue
			}
			if strings.HasPrefix(key, prefix) {
				rendered[key] = true
				values := make([]float64, len(points))
				for i, p := range points {
					values[i] = p.Value
				}
				c := colors[colorIdx%len(colors)]
				colorIdx++
				spark := RenderSparkline(values, sparkW, c)
				label := metricLabel(widget, key)
				sb.WriteString(fmt.Sprintf("  %s %s\n", dimStyle.Render(label), spark))
			}
		}
	}
}

func renderModelsSection(sb *strings.Builder, snap core.UsageSnapshot, widget core.DashboardWidget, w int) {
	models := core.ExtractAnalyticsModelUsage(snap)
	if len(models) == 0 {
		return
	}

	if len(models) > 8 {
		models = models[:8]
	}

	items := make([]chartItem, 0, len(models))
	for i, model := range models {
		if model.CostUSD <= 0 {
			continue
		}
		subLabel := ""
		if i == 0 && model.InputTokens > 0 {
			subLabel = formatTokens(model.InputTokens) + " in"
		}
		items = append(items, chartItem{
			Label:    prettifyModelName(model.Name),
			Value:    model.CostUSD,
			Color:    stableModelColor(model.Name, snap.ProviderID),
			SubLabel: subLabel,
		})
	}

	if len(items) > 0 {
		labelW := 22
		if w < 55 {
			labelW = 16
		}
		barW := w - labelW - 20
		if barW < 8 {
			barW = 8
		}
		if barW > 30 {
			barW = 30
		}
		sb.WriteString(RenderHBarChart(items, barW, labelW) + "\n")
	}

	for _, model := range models {
		if model.InputTokens <= 0 && model.OutputTokens <= 0 {
			continue
		}
		sb.WriteString("\n")
		sb.WriteString("  " + dimStyle.Render("Token breakdown: "+prettifyModelName(model.Name)) + "\n")
		sb.WriteString(RenderTokenBreakdown(model.InputTokens, model.OutputTokens, w-4) + "\n")
		break
	}
}

func hasAnalyticsModelData(snap core.UsageSnapshot) bool {
	return len(core.ExtractAnalyticsModelUsage(snap)) > 0
}

// hasChartableSeries returns true if at least one daily series has >= 2 data points.
func hasChartableSeries(series map[string][]core.TimePoint) bool {
	for _, pts := range series {
		if len(pts) >= 2 {
			return true
		}
	}
	return false
}

// hasLanguageMetrics checks if the snapshot contains lang_ metric keys.
func hasLanguageMetrics(snap core.UsageSnapshot) bool {
	langs, _ := core.ExtractLanguageUsage(snap)
	return len(langs) > 0
}

func renderLanguagesSection(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	langs, _ := core.ExtractLanguageUsage(snap)
	if len(langs) == 0 {
		return
	}

	total := float64(0)
	for _, l := range langs {
		total += l.Requests
	}
	if total <= 0 {
		return
	}

	maxShow := 10
	if len(langs) > maxShow {
		langs = langs[:maxShow]
	}

	var items []chartItem
	for _, l := range langs {
		items = append(items, chartItem{
			Label: l.Name,
			Value: l.Requests,
			Color: stableModelColor("lang:"+l.Name, "languages"),
		})
	}

	labelW := 18
	if w < 55 {
		labelW = 14
	}
	barW := w - labelW - 20
	if barW < 8 {
		barW = 8
	}
	if barW > 30 {
		barW = 30
	}

	for _, item := range items {
		pct := item.Value / total * 100
		label := item.Label
		if len(label) > labelW {
			label = label[:labelW-1] + "…"
		}

		barLen := int(item.Value / items[0].Value * float64(barW))
		if barLen < 1 && item.Value > 0 {
			barLen = 1
		}
		emptyLen := barW - barLen
		bar := lipgloss.NewStyle().Foreground(item.Color).Render(strings.Repeat("█", barLen))
		track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", emptyLen))

		pctStr := lipgloss.NewStyle().Foreground(item.Color).Render(fmt.Sprintf("%4.1f%%", pct))
		countStr := dimStyle.Render(formatNumber(item.Value) + " req")

		sb.WriteString(fmt.Sprintf("  %s %s%s  %s  %s\n",
			labelStyle.Width(labelW).Render(label),
			bar, track, pctStr, countStr))
	}

	if len(langs) > maxShow {
		remaining := len(langs) - maxShow
		if remaining > 0 {
			sb.WriteString("  " + dimStyle.Render(fmt.Sprintf("+ %d more languages", remaining)) + "\n")
		}
	}
}

// hasMCPMetrics checks if the snapshot contains any MCP metric keys.
func hasMCPMetrics(snap core.UsageSnapshot) bool {
	servers, _ := core.ExtractMCPUsage(snap)
	return len(servers) > 0
}

// renderMCPSection renders MCP server and function call metrics.
// Uses prettifyMCPServerName/prettifyMCPFunctionName from tiles.go (same package).
func renderMCPSection(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	rawServers, _ := core.ExtractMCPUsage(snap)
	servers := make([]struct {
		name  string
		calls float64
		funcs []struct {
			name  string
			calls float64
		}
	}, 0, len(rawServers))
	for _, rawServer := range rawServers {
		server := struct {
			name  string
			calls float64
			funcs []struct {
				name  string
				calls float64
			}
		}{
			name:  prettifyMCPServerName(rawServer.RawName),
			calls: rawServer.Calls,
		}
		for _, rawFunc := range rawServer.Functions {
			server.funcs = append(server.funcs, struct {
				name  string
				calls float64
			}{
				name:  prettifyMCPFunctionName(rawFunc.RawName),
				calls: rawFunc.Calls,
			})
		}
		servers = append(servers, server)
	}
	if len(servers) == 0 {
		return
	}

	var totalCalls float64
	for _, srv := range servers {
		totalCalls += srv.calls
	}
	if totalCalls <= 0 {
		return
	}

	// Render stacked bar.
	barW := w - 4
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	// Build color map using prettified names (same as tile).
	var allEntries []toolMixEntry
	for _, srv := range servers {
		allEntries = append(allEntries, toolMixEntry{name: srv.name, count: srv.calls})
	}
	toolColors := buildToolColorMap(allEntries, snap.AccountID)

	sb.WriteString(fmt.Sprintf("  %s\n", renderToolMixBar(allEntries, totalCalls, barW, toolColors)))

	// Render server + function rows.
	for i, srv := range servers {
		toolColor := colorForTool(toolColors, srv.name)
		colorDot := lipgloss.NewStyle().Foreground(toolColor).Render("■")
		serverLabel := fmt.Sprintf("%s %d %s", colorDot, i+1, srv.name)
		pct := srv.calls / totalCalls * 100
		valueStr := fmt.Sprintf("%2.0f%% %s calls", pct, shortCompact(srv.calls))
		sb.WriteString(renderDotLeaderRow(serverLabel, valueStr, w-2))
		sb.WriteString("\n")

		// Show up to 8 functions.
		maxFuncs := 8
		if len(srv.funcs) < maxFuncs {
			maxFuncs = len(srv.funcs)
		}
		for j := 0; j < maxFuncs; j++ {
			fn := srv.funcs[j]
			fnLabel := "    " + fn.name
			fnValue := fmt.Sprintf("%s calls", shortCompact(fn.calls))
			sb.WriteString(renderDotLeaderRow(fnLabel, fnValue, w-2))
			sb.WriteString("\n")
		}
		if len(srv.funcs) > 8 {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("    + %d more functions", len(srv.funcs)-8)))
			sb.WriteString("\n")
		}
	}

	// Footer.
	footer := fmt.Sprintf("%d servers · %.0f calls", len(servers), totalCalls)
	sb.WriteString("  " + dimStyle.Render(footer) + "\n")
}

// hasModelCostMetrics checks if the snapshot contains model cost metric keys.
func hasModelCostMetrics(snap core.UsageSnapshot) bool {
	for key := range snap.Metrics {
		if core.IsModelCostMetricKey(key) {
			return true
		}
	}
	return false
}

// renderTrendsSection renders DailySeries data as a braille chart for the primary series
// and sparklines for secondary series.
func renderTrendsSection(sb *strings.Builder, snap core.UsageSnapshot, widget core.DashboardWidget, w int) {
	if len(snap.DailySeries) == 0 {
		return
	}

	// Pick primary series key.
	primaryCandidates := []string{"cost", "tokens_total", "messages", "requests", "sessions"}
	primaryKey := ""
	for _, key := range primaryCandidates {
		if pts, ok := snap.DailySeries[key]; ok && len(pts) >= 2 {
			primaryKey = key
			break
		}
	}

	// If no candidate found, pick the first series with enough points.
	if primaryKey == "" {
		for key, pts := range snap.DailySeries {
			if len(pts) >= 2 {
				primaryKey = key
				break
			}
		}
	}

	if primaryKey == "" {
		return
	}

	// Render primary series as braille chart.
	pts := snap.DailySeries[primaryKey]
	yFmt := formatChartValue
	if primaryKey == "cost" {
		yFmt = formatCostAxis
	}

	chartW := w - 4
	if chartW < 30 {
		chartW = 30
	}
	chartH := 6
	if w < 60 {
		chartH = 4
	}

	series := []BrailleSeries{{
		Label:  metricLabel(widget, primaryKey),
		Color:  colorTeal,
		Points: pts,
	}}

	chart := RenderBrailleChart(metricLabel(widget, primaryKey), series, chartW, chartH, yFmt)
	if chart != "" {
		sb.WriteString(chart)
	}

	// Render remaining series as sparklines.
	sparkW := w - 8
	if sparkW < 12 {
		sparkW = 12
	}
	if sparkW > 60 {
		sparkW = 60
	}

	colors := []lipgloss.Color{colorSapphire, colorGreen, colorPeach, colorLavender}
	colorIdx := 0

	for _, candidate := range primaryCandidates {
		if candidate == primaryKey {
			continue
		}
		seriesPts, ok := snap.DailySeries[candidate]
		if !ok || len(seriesPts) < 2 {
			continue
		}
		values := make([]float64, len(seriesPts))
		for i, p := range seriesPts {
			values[i] = p.Value
		}
		c := colors[colorIdx%len(colors)]
		colorIdx++
		spark := RenderSparkline(values, sparkW, c)
		label := metricLabel(widget, candidate)
		sb.WriteString(fmt.Sprintf("  %s %s\n", dimStyle.Render(label), spark))
	}
}

// filterNonZeroEntries removes entries where all numeric values are nil or zero,
// respecting the widget's suppression configuration.
func filterNonZeroEntries(entries []metricEntry, widget core.DashboardWidget) []metricEntry {
	suppressKeys := make(map[string]bool, len(widget.SuppressZeroMetricKeys))
	for _, k := range widget.SuppressZeroMetricKeys {
		suppressKeys[k] = true
	}

	var result []metricEntry
	for _, e := range entries {
		m := e.metric
		isZero := (m.Used == nil || *m.Used == 0) &&
			(m.Remaining == nil || *m.Remaining == 0) &&
			(m.Limit == nil || *m.Limit == 0)

		if isZero {
			if widget.SuppressZeroNonUsageMetrics {
				// Skip if it's not a quota/usage metric (has no limit).
				if m.Limit == nil {
					continue
				}
			}
			if suppressKeys[e.key] {
				continue
			}
		}
		result = append(result, e)
	}
	return result
}

// renderInfoSection renders Attributes, Diagnostics, and Raw as separate sub-sections.
func renderInfoSection(sb *strings.Builder, snap core.UsageSnapshot, widget core.DashboardWidget, w int) {
	labelW := sectionLabelWidth(w)
	maxValW := w - labelW - 6
	if maxValW < 20 {
		maxValW = 20
	}
	if maxValW > 45 {
		maxValW = 45
	}

	if len(snap.Attributes) > 0 {
		renderDetailSectionHeader(sb, "Attributes", w)
		renderKeyValuePairs(sb, snap.Attributes, labelW, maxValW, valueStyle)
	}

	if len(snap.Diagnostics) > 0 {
		if len(snap.Attributes) > 0 {
			sb.WriteString("\n")
		}
		renderDetailSectionHeader(sb, "Diagnostics", w)
		warnValueStyle := lipgloss.NewStyle().Foreground(colorYellow)
		renderKeyValuePairs(sb, snap.Diagnostics, labelW, maxValW, warnValueStyle)
	}

	if len(snap.Raw) > 0 {
		if len(snap.Attributes) > 0 || len(snap.Diagnostics) > 0 {
			sb.WriteString("\n")
		}
		renderDetailSectionHeader(sb, "Raw Data", w)
		renderRawData(sb, snap.Raw, widget, w)
	}
}

// renderKeyValuePairs renders a sorted key-value map with consistent formatting.
func renderKeyValuePairs(sb *strings.Builder, data map[string]string, labelW, maxValW int, vs lipgloss.Style) {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := smartFormatValue(data[k])
		if len(v) > maxValW {
			v = v[:maxValW-3] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			labelStyle.Width(labelW).Render(prettifyKey(k)),
			vs.Render(v),
		))
	}
}

func sectionLabelWidth(w int) int {
	switch {
	case w < 45:
		return 14
	case w < 55:
		return 18
	default:
		return 22
	}
}

func sectionGaugeWidth(w, labelW int) int {
	gw := w - labelW - 14
	if gw < 8 {
		gw = 8
	}
	if gw > 28 {
		gw = 28
	}
	return gw
}

func renderGaugeEntry(sb *strings.Builder, entry metricEntry, labelW, w int, warnThresh, critThresh float64) {
	m := entry.metric
	labelRendered := labelStyle.Width(labelW).Render(entry.label)
	gaugeW := sectionGaugeWidth(w, labelW)

	if m.Unit == "%" && m.Used != nil {
		gauge := RenderUsageGauge(*m.Used, gaugeW, warnThresh, critThresh)
		sb.WriteString(fmt.Sprintf("  %s %s\n", labelRendered, gauge))
		if detail := formatUsageDetail(m); detail != "" {
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				strings.Repeat(" ", labelW+2), dimStyle.Render(detail)))
		}
		return
	}

	if pct := m.Percent(); pct >= 0 {
		gauge := RenderGauge(pct, gaugeW, warnThresh, critThresh)
		sb.WriteString(fmt.Sprintf("  %s %s\n", labelRendered, gauge))
		if detail := formatMetricDetail(m); detail != "" {
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				strings.Repeat(" ", labelW+2), dimStyle.Render(detail)))
		}
		return
	}

	val := formatMetricValue(m)
	vs := valueStyle
	if strings.Contains(val, "$") || strings.Contains(val, "USD") {
		vs = metricValueStyle
	}
	sb.WriteString(fmt.Sprintf("  %s %s\n", labelRendered, vs.Render(val)))
}

func isModelCostKey(key string) bool {
	return core.IsModelCostMetricKey(key)
}

func formatMetricValue(m core.Metric) string {
	var value string
	switch {
	case m.Used != nil && m.Limit != nil:
		value = fmt.Sprintf("%s / %s %s",
			formatNumber(*m.Used), formatNumber(*m.Limit), m.Unit)
	case m.Remaining != nil && m.Limit != nil:
		value = fmt.Sprintf("%s / %s %s remaining",
			formatNumber(*m.Remaining), formatNumber(*m.Limit), m.Unit)
	case m.Used != nil:
		value = fmt.Sprintf("%s %s", formatNumber(*m.Used), m.Unit)
	case m.Remaining != nil:
		value = fmt.Sprintf("%s %s remaining", formatNumber(*m.Remaining), m.Unit)
	}

	if m.Window != "" && m.Window != "all_time" && m.Window != "current_period" {
		value += " " + dimStyle.Render("["+m.Window+"]")
	}
	return value
}

func renderModelCostsTable(sb *strings.Builder, entries []metricEntry, w int) {
	type modelCost struct {
		name    string
		cost    float64
		window  string
		hasData bool
	}

	var models []modelCost
	var unmatched []metricEntry

	for _, e := range entries {
		label := e.label
		var modelName string
		switch {
		case strings.HasSuffix(label, "_cost"):
			modelName = strings.TrimSuffix(label, "_cost")
		case strings.HasSuffix(label, "_cost_usd"):
			modelName = strings.TrimSuffix(label, "_cost_usd")
		default:
			unmatched = append(unmatched, e)
			continue
		}

		cost := float64(0)
		if e.metric.Used != nil {
			cost = *e.metric.Used
		}
		models = append(models, modelCost{
			name:    prettifyModelName(modelName),
			cost:    cost,
			window:  e.metric.Window,
			hasData: true,
		})
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].cost > models[j].cost
	})

	if len(models) > 0 {
		nameW := 28
		if w < 55 {
			nameW = 20
		}

		windowHint := ""
		if len(models) > 0 && models[0].window != "" &&
			models[0].window != "all_time" && models[0].window != "current_period" {
			windowHint = " " + dimStyle.Render("["+models[0].window+"]")
		}

		sb.WriteString(fmt.Sprintf("  %-*s %10s%s\n",
			nameW, dimStyle.Bold(true).Render("Model"),
			dimStyle.Bold(true).Render("Cost"),
			windowHint,
		))

		for _, mc := range models {
			name := mc.name
			if len(name) > nameW {
				name = name[:nameW-1] + "…"
			}
			costStr := formatUSD(mc.cost)
			costStyle := tealStyle
			if mc.cost >= 10 {
				costStyle = metricValueStyle
			}
			sb.WriteString(fmt.Sprintf("  %-*s %10s\n",
				nameW, valueStyle.Render(name),
				costStyle.Render(costStr),
			))
		}
	}

	for _, e := range unmatched {
		val := formatMetricValue(e.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(22).Render(prettifyModelName(e.label)),
			valueStyle.Render(val),
		))
	}
}

func renderUsageTable(sb *strings.Builder, entries []metricEntry, w int, warnThresh, critThresh float64) {
	if len(entries) == 0 {
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		pi := entries[i].metric.Percent()
		pj := entries[j].metric.Percent()
		if pi < 0 {
			pi = 200
		}
		if pj < 0 {
			pj = 200
		}
		return pi < pj
	})

	nameW := 30
	gaugeW := 10
	if w < 65 {
		nameW = 22
		gaugeW = 8
	}
	if w < 50 {
		nameW = 16
		gaugeW = 6
	}

	for _, entry := range entries {
		m := entry.metric
		name := entry.label
		if len(name) > nameW {
			name = name[:nameW-1] + "…"
		}

		pct := m.Percent()
		gauge := ""
		pctStr := ""
		if pct >= 0 {
			gauge = RenderMiniGauge(pct, gaugeW)
			var color lipgloss.Color
			switch {
			case pct <= critThresh*100:
				color = colorCrit
			case pct <= warnThresh*100:
				color = colorWarn
			default:
				color = colorOK
			}
			pctStr = lipgloss.NewStyle().Foreground(color).Bold(true).Render(fmt.Sprintf("%5.1f%%", pct))
		}

		windowStr := ""
		if m.Window != "" && m.Window != "all_time" && m.Window != "current_period" {
			windowStr = dimStyle.Render(" [" + m.Window + "]")
		}

		sb.WriteString(fmt.Sprintf("  %-*s %s %s%s\n",
			nameW, labelStyle.Render(name),
			gauge, pctStr, windowStr,
		))
	}
}

func renderRawData(sb *strings.Builder, raw map[string]string, widget core.DashboardWidget, w int) {
	labelW := sectionLabelWidth(w)

	maxValW := w - labelW - 6
	if maxValW < 20 {
		maxValW = 20
	}
	if maxValW > 45 {
		maxValW = 45
	}

	rendered := make(map[string]bool)

	for _, g := range widget.RawGroups {
		hasAny := false
		for _, key := range g.Keys {
			if v, ok := raw[key]; ok && v != "" {
				hasAny = true
				_ = v
				break
			}
		}
		if !hasAny {
			continue
		}
		for _, key := range g.Keys {
			v, ok := raw[key]
			if !ok || v == "" {
				continue
			}
			rendered[key] = true
			fv := smartFormatValue(v)
			if len(fv) > maxValW {
				fv = fv[:maxValW-3] + "..."
			}
			sb.WriteString(fmt.Sprintf("  %s  %s\n",
				labelStyle.Width(labelW).Render(prettifyKey(key)),
				valueStyle.Render(fv),
			))
		}
	}

	keys := lo.Keys(raw)
	sort.Strings(keys)

	for _, k := range keys {
		if rendered[k] || strings.HasSuffix(k, "_error") {
			continue
		}
		v := smartFormatValue(raw[k])
		if len(v) > maxValW {
			v = v[:maxValW-3] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			labelStyle.Width(labelW).Render(prettifyKey(k)),
			dimStyle.Render(v),
		))
	}
}

func smartFormatValue(v string) string {
	trimmed := strings.TrimSpace(v)

	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 1e12 && n < 2e13 {
		t := time.Unix(n/1000, 0)
		return t.Format("Jan 02, 2006 15:04")
	}

	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 1e9 && n < 2e10 {
		t := time.Unix(n, 0)
		return t.Format("Jan 02, 2006 15:04")
	}

	return v
}

func renderDetailSectionHeader(sb *strings.Builder, title string, w int) {
	icon := sectionIcon(title)
	sc := sectionColor(title)

	iconStyled := lipgloss.NewStyle().Foreground(sc).Render(icon)
	titleStyled := lipgloss.NewStyle().Bold(true).Foreground(sc).Render(" " + title + " ")
	left := "  " + iconStyled + titleStyled

	lineLen := w - lipgloss.Width(left) - 2
	if lineLen < 4 {
		lineLen = 4
	}
	line := lipgloss.NewStyle().Foreground(sc).Render(strings.Repeat("─", lineLen))
	sb.WriteString(left + line + "\n")
}

func sectionIcon(title string) string {
	switch title {
	case "Usage":
		return "⚡"
	case "Spending":
		return "💰"
	case "Tokens":
		return "📊"
	case "Activity":
		return "📈"
	case "Timers":
		return "⏰"
	case "Models":
		return "🤖"
	case "Languages":
		return "🗂"
	case "Trends":
		return "📈"
	case "MCP Usage":
		return "🔌"
	case "Attributes":
		return "📋"
	case "Diagnostics":
		return "⚠"
	case "Raw Data":
		return "🔧"
	default:
		return "›"
	}
}

func sectionColor(title string) lipgloss.Color {
	switch title {
	case "Usage":
		return colorYellow
	case "Spending":
		return colorTeal
	case "Tokens":
		return colorSapphire
	case "Activity":
		return colorGreen
	case "Timers":
		return colorMaroon
	case "Models":
		return colorLavender
	case "Languages":
		return colorPeach
	case "Trends":
		return colorSapphire
	case "MCP Usage":
		return colorSky
	case "Attributes":
		return colorBlue
	case "Diagnostics":
		return colorYellow
	case "Raw Data":
		return colorDim
	default:
		return colorBlue
	}
}

func formatUsageDetail(m core.Metric) string {
	var parts []string

	if m.Remaining != nil {
		parts = append(parts, fmt.Sprintf("%.0f%% remaining", *m.Remaining))
	} else if m.Used != nil && m.Limit != nil {
		rem := *m.Limit - *m.Used
		parts = append(parts, fmt.Sprintf("%.0f%% remaining", rem))
	}

	if m.Window != "" && m.Window != "all_time" && m.Window != "current_period" {
		parts = append(parts, "["+m.Window+"]")
	}

	return strings.Join(parts, " ")
}

func formatMetricDetail(m core.Metric) string {
	var parts []string
	switch {
	case m.Used != nil && m.Limit != nil:
		parts = append(parts, fmt.Sprintf("%s / %s %s",
			formatNumber(*m.Used), formatNumber(*m.Limit), m.Unit))
	case m.Remaining != nil && m.Limit != nil:
		parts = append(parts, fmt.Sprintf("%s / %s %s remaining",
			formatNumber(*m.Remaining), formatNumber(*m.Limit), m.Unit))
	case m.Used != nil:
		parts = append(parts, fmt.Sprintf("%s %s", formatNumber(*m.Used), m.Unit))
	case m.Remaining != nil:
		parts = append(parts, fmt.Sprintf("%s %s remaining", formatNumber(*m.Remaining), m.Unit))
	}

	if m.Window != "" && m.Window != "all_time" && m.Window != "current_period" {
		parts = append(parts, "["+m.Window+"]")
	}

	return strings.Join(parts, " ")
}

func formatNumber(n float64) string {
	if n == 0 {
		return "0"
	}
	abs := math.Abs(n)
	switch {
	case abs >= 1_000_000:
		return fmt.Sprintf("%.1fM", n/1_000_000)
	case abs >= 10_000:
		return fmt.Sprintf("%.1fK", n/1_000)
	case abs >= 1_000:
		return fmt.Sprintf("%.0f", n)
	case abs == math.Floor(abs):
		return fmt.Sprintf("%.0f", n)
	default:
		return fmt.Sprintf("%.2f", n)
	}
}

func formatTokens(n float64) string {
	if n == 0 {
		return "-"
	}
	return formatNumber(n)
}

func formatUSD(n float64) string {
	if n == 0 {
		return "-"
	}
	if n >= 1000 {
		return fmt.Sprintf("$%.0f", n)
	}
	return fmt.Sprintf("$%.2f", n)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

func prettifyKey(key string) string {
	return core.PrettifyMetricKey(key)
}

func prettifyModelName(name string) string {
	result := strings.ReplaceAll(name, "_", "-")

	switch strings.ToLower(result) {
	case "unattributed":
		return "unmapped spend (missing historical mapping)"
	case "default":
		return "default (auto)"
	case "composer-1":
		return "composer-1 (agent)"
	case "github-bugbot":
		return "github-bugbot (auto)"
	}
	return result
}
