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

type DetailTab int

const (
	TabAll  DetailTab = 0 // show everything
	TabDyn1 DetailTab = 1 // first dynamic group
)

func DetailTabs(snap core.QuotaSnapshot) []string {
	tabs := []string{"All"}
	if len(snap.Metrics) > 0 {
		groups := groupMetrics(snap.Metrics, dashboardWidget(snap.ProviderID))
		for _, g := range groups {
			tabs = append(tabs, g.title)
		}
	}
	if len(snap.Resets) > 0 {
		tabs = append(tabs, "Timers")
	}
	if len(snap.Raw) > 0 {
		tabs = append(tabs, "Info")
	}
	return tabs
}

func RenderDetailContent(snap core.QuotaSnapshot, w int, warnThresh, critThresh float64, activeTab int) string {
	var sb strings.Builder
	widget := dashboardWidget(snap.ProviderID)

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

	if len(snap.Metrics) > 0 {
		groups := groupMetrics(snap.Metrics, widget)
		for _, group := range groups {
			if showAll || group.title == tabName {
				renderMetricGroup(&sb, group, widget, w, warnThresh, critThresh, snap.DailySeries)
			}
		}
	}

	if showTimers && len(snap.Resets) > 0 {
		sb.WriteString("\n")
		renderTimersSection(&sb, snap.Resets, widget, w)
	}

	if showInfo && len(snap.Raw) > 0 {
		sb.WriteString("\n")
		count := len(snap.Raw)
		renderDetailSectionHeader(&sb, fmt.Sprintf("› Details (%d entries)", count), w)
		renderRawData(&sb, snap.Raw, widget, w)
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

func renderDetailHeader(sb *strings.Builder, snap core.QuotaSnapshot, w int) {
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

	if email, ok := snap.Raw["account_email"]; ok && email != "" {
		metaTags = append(metaTags, MetaTagHighlight("✉", email))
	}

	if planName, ok := snap.Raw["plan_name"]; ok && planName != "" {
		metaTags = append(metaTags, MetaTag("◆", planName))
	}
	if planType, ok := snap.Raw["plan_type"]; ok && planType != "" {
		metaTags = append(metaTags, MetaTag("◇", planType))
	}
	if membership, ok := snap.Raw["membership_type"]; ok && membership != "" {
		metaTags = append(metaTags, MetaTag("👤", membership))
	}
	if team, ok := snap.Raw["team_membership"]; ok && team != "" {
		metaTags = append(metaTags, MetaTag("🏢", team))
	}
	if org, ok := snap.Raw["organization_name"]; ok && org != "" {
		metaTags = append(metaTags, MetaTag("🏢", org))
	}

	if model, ok := snap.Raw["active_model"]; ok && model != "" {
		metaTags = append(metaTags, MetaTag("⬡", model))
	}
	if cliVer, ok := snap.Raw["cli_version"]; ok && cliVer != "" {
		metaTags = append(metaTags, MetaTag("⌘", "v"+cliVer))
	}

	if planPrice, ok := snap.Raw["plan_price"]; ok && planPrice != "" {
		metaTags = append(metaTags, MetaTag("$", planPrice))
	}
	if credits, ok := snap.Raw["credits"]; ok && credits != "" {
		metaTags = append(metaTags, MetaTag("💳", credits))
	}

	if oauth, ok := snap.Raw["oauth_status"]; ok && oauth != "" {
		metaTags = append(metaTags, MetaTag("🔒", oauth))
	}
	if sub, ok := snap.Raw["subscription_status"]; ok && sub != "" {
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

func groupMetrics(metrics map[string]core.Metric, widget core.DashboardWidget) []metricGroup {
	groups := make(map[string]*metricGroup)

	for key, m := range metrics {
		groupName, label, order := classifyMetric(key, m, widget)
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

func classifyMetric(key string, m core.Metric, widget core.DashboardWidget) (group, label string, order int) {
	if override, ok := widget.MetricGroupOverrides[key]; ok && override.Group != "" {
		label = override.Label
		if label == "" {
			label = metricLabel(widget, key)
		}
		order = override.Order
		if order <= 0 {
			order = 4
		}
		return override.Group, label, order
	}

	lk := strings.ToLower(key)

	switch {

	case key == "rpm" || key == "tpm" || key == "rpd" || key == "tpd":
		return "Usage", strings.ToUpper(key), 1
	case strings.HasPrefix(key, "rate_limit_"):
		return "Usage", metricLabel(widget, strings.TrimPrefix(key, "rate_limit_")), 1
	case key == "rpm_headers" || key == "tpm_headers":
		return "Usage", metricLabel(widget, key), 1
	case key == "gh_api_rpm" || key == "copilot_chat":
		return "Usage", metricLabel(widget, key), 1

	case key == "plan_percent_used":
		return "Usage", "Plan Used", 1

	case key == "spend_limit":
		return "Usage", "Spend Limit", 1

	case key == "plan_spend":
		return "Usage", "Plan Spend", 1

	case key == "monthly_spend" && m.Limit != nil:
		return "Usage", "Monthly Spend", 1
	case key == "monthly_budget" && m.Limit != nil:
		return "Usage", "Monthly Budget", 1

	case (key == "credits" || key == "credit_balance") && m.Limit != nil:
		return "Usage", metricLabel(widget, key), 1

	case key == "context_window":
		return "Usage", "Context Window", 1

	// Time-bucketed usage (daily, weekly, monthly)
	case key == "usage_daily" || key == "usage_weekly" || key == "usage_monthly":
		return "Usage", metricLabel(widget, key), 1
	case key == "limit_remaining":
		return "Usage", "Limit Remaining", 1

	case m.Remaining != nil && m.Limit != nil && m.Unit != "%" && m.Unit != "USD":
		return "Usage", prettifyQuotaKey(key, widget), 1

	case m.Unit == "%" && (m.Used != nil || m.Remaining != nil):
		return "Usage", metricLabel(widget, key), 1

	case m.Used != nil && m.Limit != nil &&
		!strings.Contains(lk, "token") && m.Unit != "%" && m.Unit != "USD":
		return "Usage", metricLabel(widget, key), 1

	case strings.HasPrefix(key, "model_") &&
		!strings.HasSuffix(key, "_input_tokens") &&
		!strings.HasSuffix(key, "_output_tokens"):
		return "Spending", strings.TrimPrefix(key, "model_"), 2

	case key == "plan_included" || key == "plan_bonus" ||
		key == "plan_total_spend_usd" || key == "plan_limit_usd":
		return "Spending", metricLabel(widget, strings.TrimPrefix(key, "plan_")), 2

	case key == "individual_spend":
		return "Spending", "Individual Spend", 2

	case strings.Contains(lk, "cost") || strings.Contains(lk, "burn_rate"):
		return "Spending", metricLabel(widget, key), 2

	case key == "credits" || key == "credit_balance":
		return "Spending", metricLabel(widget, key), 2

	case key == "monthly_spend" || key == "monthly_budget":
		return "Spending", metricLabel(widget, key), 2

	case strings.HasSuffix(key, "_balance"):
		return "Spending", metricLabel(widget, key), 2

	case strings.HasPrefix(key, "input_tokens_") || strings.HasPrefix(key, "output_tokens_"):
		return "Tokens", key, 3

	case strings.HasPrefix(key, "model_") &&
		(strings.HasSuffix(key, "_input_tokens") || strings.HasSuffix(key, "_output_tokens")):
		return "Tokens", key, 3

	// Per-model reasoning and cached tokens
	case strings.HasPrefix(key, "model_") &&
		(strings.HasSuffix(key, "_reasoning_tokens") || strings.HasSuffix(key, "_cached_tokens") || strings.HasSuffix(key, "_image_tokens")):
		return "Tokens", metricLabel(widget, key), 3

	case strings.HasPrefix(key, "session_"):
		return "Tokens", metricLabel(widget, strings.TrimPrefix(key, "session_")), 3

	// Additional today metrics
	case strings.HasPrefix(key, "today_") && strings.Contains(lk, "token"):
		return "Tokens", metricLabel(widget, key), 3

	case strings.Contains(lk, "token"):
		return "Tokens", metricLabel(widget, key), 3

	case strings.HasPrefix(key, "tab_") || strings.HasPrefix(key, "composer_"):
		return "Activity", metricLabel(widget, key), 4

	case strings.Contains(lk, "message") || strings.Contains(lk, "session") ||
		strings.Contains(lk, "conversation") || strings.Contains(lk, "tool_call") ||
		strings.Contains(lk, "request"):
		return "Activity", metricLabel(widget, key), 4

	default:
		return "Activity", metricLabel(widget, key), 4
	}
}

func metricLabel(widget core.DashboardWidget, key string) string {
	if widget.MetricLabelOverrides != nil {
		if label, ok := widget.MetricLabelOverrides[key]; ok && label != "" {
			return label
		}
	}
	return prettifyKey(key)
}

func prettifyQuotaKey(key string, widget core.DashboardWidget) string {
	lastUnderscore := strings.LastIndex(key, "_")
	if lastUnderscore > 0 && lastUnderscore < len(key)-1 {
		suffix := key[lastUnderscore+1:]
		prefix := key[:lastUnderscore]
		if suffix == strings.ToUpper(suffix) && len(suffix) > 1 {
			return prettifyModelHyphens(prefix) + " " + titleCase(suffix)
		}
	}
	return metricLabel(widget, key)
}

func prettifyModelHyphens(name string) string {
	parts := strings.Split(name, "-")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		if p[0] >= '0' && p[0] <= '9' {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func titleCase(s string) string {
	if len(s) <= 1 {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func renderMetricGroup(sb *strings.Builder, group metricGroup, widget core.DashboardWidget, w int, warnThresh, critThresh float64, series map[string][]core.TimePoint) {
	sb.WriteString("\n")
	renderDetailSectionHeader(sb, group.title, w)

	switch group.title {
	case "Usage":
		renderUsageSection(sb, group.entries, w, warnThresh, critThresh)
	case "Spending":
		renderSpendingSection(sb, group.entries, w)
	case "Tokens":
		renderTokensSection(sb, group.entries, widget, w, series)
	case "Activity":
		renderActivitySection(sb, group.entries, widget, w, series)
	}
}

func renderUsageSection(sb *strings.Builder, entries []metricEntry, w int, warnThresh, critThresh float64) {
	labelW := sectionLabelWidth(w)

	var quotaEntries []metricEntry
	var gaugeEntries []metricEntry

	for _, e := range entries {
		m := e.metric
		if m.Remaining != nil && m.Limit != nil && m.Unit != "%" && m.Unit != "USD" {
			quotaEntries = append(quotaEntries, e)
		} else {
			gaugeEntries = append(gaugeEntries, e)
		}
	}

	for _, entry := range gaugeEntries {
		renderGaugeEntry(sb, entry, labelW, w, warnThresh, critThresh)
	}

	if len(quotaEntries) > 0 {
		if len(gaugeEntries) > 0 {
			sb.WriteString("\n")
		}
		renderQuotaTable(sb, quotaEntries, w, warnThresh, critThresh)
	}
}

func renderSpendingSection(sb *strings.Builder, entries []metricEntry, w int) {
	labelW := sectionLabelWidth(w)

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
		val := formatMetricValue(e.metric)
		vs := metricValueStyle
		if !strings.Contains(val, "$") && !strings.Contains(val, "USD") {
			vs = valueStyle
		}
		if e.metric.Used != nil && e.metric.Limit != nil && *e.metric.Limit > 0 {
			pct := ((*e.metric.Limit - *e.metric.Used) / *e.metric.Limit) * 100
			if pct < 0 {
				pct = 0
			}
			gauge := RenderMiniGauge(pct, 8)
			sb.WriteString(fmt.Sprintf("  %s %s %s\n",
				labelStyle.Width(labelW).Render(e.label), gauge, vs.Render(val)))
		} else {
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

func renderTokensSection(sb *strings.Builder, entries []metricEntry, widget core.DashboardWidget, w int, series map[string][]core.TimePoint) {
	labelW := sectionLabelWidth(w)

	var perModelTokens []metricEntry
	var otherTokens []metricEntry

	for _, e := range entries {
		if isPerModelTokenKey(e.key) {
			perModelTokens = append(perModelTokens, e)
		} else {
			otherTokens = append(otherTokens, e)
		}
	}

	for _, e := range otherTokens {
		val := formatMetricValue(e.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(labelW).Render(e.label), valueStyle.Render(val)))
	}

	if len(perModelTokens) > 0 {
		if len(otherTokens) > 0 {
			sb.WriteString("\n")
		}
		renderTokenUsageTable(sb, perModelTokens, w)
	}

	renderSectionSparklines(sb, widget, w, series, []string{
		"tokens_total", "tokens_input", "tokens_output",
	})
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

	timerKeys := make([]string, 0, len(resets))
	for k := range resets {
		timerKeys = append(timerKeys, k)
	}
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
	return strings.HasPrefix(key, "model_") &&
		(strings.HasSuffix(key, "_cost") || strings.HasSuffix(key, "_cost_usd"))
}

func isPerModelTokenKey(key string) bool {
	if strings.HasPrefix(key, "input_tokens_") || strings.HasPrefix(key, "output_tokens_") {
		return true
	}
	if strings.HasPrefix(key, "model_") &&
		(strings.HasSuffix(key, "_input_tokens") || strings.HasSuffix(key, "_output_tokens")) {
		return true
	}
	return false
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

func renderTokenUsageTable(sb *strings.Builder, entries []metricEntry, w int) {
	type tokenData struct {
		name         string
		inputTokens  float64
		outputTokens float64
	}

	models := make(map[string]*tokenData)
	var modelOrder []string

	for _, e := range entries {
		key := e.key // use the raw metric key for pattern matching
		var modelName string
		var isInput bool

		switch {
		case strings.HasPrefix(key, "input_tokens_"):
			modelName = strings.TrimPrefix(key, "input_tokens_")
			isInput = true
		case strings.HasPrefix(key, "output_tokens_"):
			modelName = strings.TrimPrefix(key, "output_tokens_")
			isInput = false
		case strings.HasSuffix(key, "_input_tokens"):
			modelName = strings.TrimPrefix(
				strings.TrimSuffix(key, "_input_tokens"), "model_")
			isInput = true
		case strings.HasSuffix(key, "_output_tokens"):
			modelName = strings.TrimPrefix(
				strings.TrimSuffix(key, "_output_tokens"), "model_")
			isInput = false
		default:
			continue
		}

		md, ok := models[modelName]
		if !ok {
			md = &tokenData{name: modelName}
			models[modelName] = md
			modelOrder = append(modelOrder, modelName)
		}
		if e.metric.Used != nil {
			if isInput {
				md.inputTokens = *e.metric.Used
			} else {
				md.outputTokens = *e.metric.Used
			}
		}
	}

	if len(modelOrder) == 0 {
		return
	}

	nameW := 26
	colW := 10
	if w < 55 {
		nameW = 18
		colW = 8
	}

	sb.WriteString(fmt.Sprintf("  %-*s %*s %*s\n",
		nameW, dimStyle.Bold(true).Render("Model"),
		colW, dimStyle.Bold(true).Render("Input"),
		colW, dimStyle.Bold(true).Render("Output"),
	))

	for _, name := range modelOrder {
		md := models[name]
		displayName := prettifyModelName(md.name)
		if len(displayName) > nameW {
			displayName = displayName[:nameW-1] + "…"
		}
		sb.WriteString(fmt.Sprintf("  %-*s %*s %*s\n",
			nameW, valueStyle.Render(displayName),
			colW, lipgloss.NewStyle().Foreground(colorSubtext).Render(formatTokens(md.inputTokens)),
			colW, lipgloss.NewStyle().Foreground(colorSubtext).Render(formatTokens(md.outputTokens)),
		))
	}
}

func renderQuotaTable(sb *strings.Builder, entries []metricEntry, w int, warnThresh, critThresh float64) {
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

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
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

var prettifyKeyOverrides = map[string]string{
	"plan_percent_used":    "Plan Used",
	"plan_total_spend_usd": "Total Plan Spend",
	"spend_limit":          "Spend Limit",
	"individual_spend":     "Individual Spend",
	"context_window":       "Context Window",
}

func prettifyKey(key string) string {
	if label, ok := prettifyKeyOverrides[key]; ok {
		return label
	}
	parts := strings.Split(key, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	result := strings.Join(parts, " ")
	for _, pair := range [][2]string{
		{"Usd", "USD"}, {"Rpm", "RPM"}, {"Tpm", "TPM"},
		{"Rpd", "RPD"}, {"Tpd", "TPD"}, {"Api", "API"},
	} {
		result = strings.ReplaceAll(result, pair[0], pair[1])
	}
	return result
}

func prettifyModelName(name string) string {
	result := strings.ReplaceAll(name, "_", "-")

	switch strings.ToLower(result) {
	case "default":
		return "default (auto)"
	case "composer-1":
		return "composer-1 (agent)"
	case "github-bugbot":
		return "github-bugbot (auto)"
	}
	return result
}
