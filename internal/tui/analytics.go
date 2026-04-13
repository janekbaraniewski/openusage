package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// Analytics sub-tab definitions.
const (
	analyticsTabOverview = 0
	analyticsTabModels   = 1
	analyticsTabSpend    = 2
	analyticsTabActivity = 3
	analyticsTabCount    = 4
)

var analyticsTabLabels = []string{"Overview", "Models", "Spend", "Activity"}

// renderAnalyticsContent is the main entry point for the analytics screen.
func (m Model) renderAnalyticsContent(w, h int) string {
	tabBar := m.renderAnalyticsTabBar(w)
	tabBarH := strings.Count(tabBar, "\n") + 1

	contentH := h - tabBarH
	if contentH < 3 {
		contentH = 3
	}

	content, hasData := m.cachedAnalyticsPageContent(w)
	if !hasData {
		empty := "\n" + dimStyle.Render("  No cost or usage data available.")
		empty += "\n" + dimStyle.Render("  Analytics requires providers that report spend, tokens, or budgets.")
		return tabBar + "\n" + empty
	}

	lines := strings.Split(content, "\n")

	// Apply scroll offset for content
	if m.analyticsScrollY > 0 && m.analyticsScrollY < len(lines) {
		lines = lines[m.analyticsScrollY:]
	}

	for len(lines) < contentH {
		lines = append(lines, "")
	}
	if len(lines) > contentH {
		lines = lines[:contentH]
	}

	return tabBar + "\n" + strings.Join(lines, "\n")
}

// renderAnalyticsTabBar renders the sub-tab bar for the analytics screen.
func (m Model) renderAnalyticsTabBar(w int) string {
	var parts []string
	for i, label := range analyticsTabLabels {
		tabStr := fmt.Sprintf(" %s ", label)
		if i == m.analyticsTab {
			parts = append(parts, tabActiveStyle.Render(tabStr))
		} else {
			parts = append(parts, tabInactiveStyle.Render(tabStr))
		}
	}
	tabs := "  " + strings.Join(parts, " ")

	// Right side: sort + filter hints
	hints := dimStyle.Render("[ ] tabs  s:sort  /:filter")
	gap := w - lipgloss.Width(tabs) - lipgloss.Width(hints) - 2
	if gap < 1 {
		gap = 1
	}
	return tabs + strings.Repeat(" ", gap) + hints
}

// renderAnalyticsTabContent dispatches to the active sub-tab renderer.
func (m Model) renderAnalyticsTabContent(data costData, summary analyticsSummary, w int) string {
	switch m.analyticsTab {
	case analyticsTabOverview:
		return renderAnalyticsOverview(data, summary, w)
	case analyticsTabModels:
		return m.renderAnalyticsModels(data, w)
	case analyticsTabSpend:
		return renderAnalyticsSpend(data, summary, w)
	case analyticsTabActivity:
		return renderAnalyticsActivity(data, w)
	default:
		return renderAnalyticsOverview(data, summary, w)
	}
}

// ─── Overview Tab ─────────────────────────────────────────────

func renderAnalyticsOverview(data costData, summary analyticsSummary, w int) string {
	var sb strings.Builder

	// KPI header
	if kpis := renderAnalyticsKPIHeader(data, summary, w); kpis != "" {
		sb.WriteString(kpis)
		sb.WriteString("\n\n")
	}

	// Total cost trend
	if totalCost := renderTotalCostTrend(data, summary, w, 9); totalCost != "" {
		sb.WriteString(totalCost)
		sb.WriteString("\n")
	}

	// Two-column layout: top providers + top models
	if w >= 80 {
		gap := 2
		colW := (w - 4 - gap) / 2
		if colW < 36 {
			colW = 36
		}
		left := renderTopProvidersBars(data, colW, 8)
		right := renderTopModelsBars(data.models, colW, 8)

		if left != "" || right != "" {
			if left == "" {
				left = strings.Repeat(" ", colW)
			}
			if right == "" {
				right = strings.Repeat(" ", colW)
			}
			sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
				left, strings.Repeat(" ", gap), right))
			sb.WriteString("\n")
		}
	} else {
		if bars := renderTopProvidersBars(data, w, 6); bars != "" {
			sb.WriteString(bars)
			sb.WriteString("\n")
		}
		if bars := renderTopModelsBars(data.models, w, 6); bars != "" {
			sb.WriteString(bars)
			sb.WriteString("\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

func renderTopProvidersBars(data costData, w int, limit int) string {
	providers := make([]providerCostEntry, len(data.providers))
	copy(providers, data.providers)
	sort.Slice(providers, func(i, j int) bool { return providers[i].cost > providers[j].cost })

	var items []chartItem
	for _, p := range providers {
		if p.cost <= 0 {
			continue
		}
		items = append(items, chartItem{
			Label:     truncStr(p.name, 18),
			Value:     p.cost,
			Color:     p.color,
			ValueText: formatUSD(p.cost),
		})
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return ""
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorPeach)
	sb.WriteString("  " + sectionStyle.Render("TOP PROVIDERS BY SPEND") + "\n")
	sb.WriteString("  " + surface1Style.Render(strings.Repeat("─", w-4)) + "\n")

	labelW := 20
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
	sb.WriteString(RenderHBarChart(items, barW, labelW))
	return sb.String()
}

func renderTopModelsBars(models []modelCostEntry, w int, limit int) string {
	all := filterTokenModels(models)
	if len(all) == 0 {
		return ""
	}
	sort.Slice(all, func(i, j int) bool { return all[i].cost > all[j].cost })
	if len(all) > limit {
		all = all[:limit]
	}

	var items []chartItem
	for _, m := range all {
		if m.cost <= 0 {
			continue
		}
		sub := ""
		if len(m.providers) > 1 {
			sub = fmt.Sprintf("%d providers", len(m.providers))
		} else if len(m.providers) == 1 {
			sub = m.providers[0].provider
		}
		items = append(items, chartItem{
			Label:     truncStr(m.name, 18),
			Value:     m.cost,
			Color:     m.color,
			ValueText: formatUSD(m.cost),
			SubLabel:  sub,
		})
	}
	if len(items) == 0 {
		return ""
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorTeal)
	sb.WriteString("  " + sectionStyle.Render("TOP MODELS BY SPEND") + "\n")
	sb.WriteString("  " + surface1Style.Render(strings.Repeat("─", w-4)) + "\n")

	labelW := 20
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
	sb.WriteString(RenderHBarChart(items, barW, labelW))
	return sb.String()
}

// ─── Models Tab ───────────────────────────────────────────────

func (m Model) renderAnalyticsModels(data costData, w int) string {
	models := filterTokenModels(data.models)
	if len(models) == 0 {
		return "\n" + dimStyle.Render("  No model usage data available.")
	}
	sortModels(models, m.analyticsSortBy)

	var sb strings.Builder

	// Summary line
	totalModels := len(models)
	totalProviders := data.providerCount
	sb.WriteString("  " + dimStyle.Render(fmt.Sprintf("%d models across %d providers", totalModels, totalProviders)))
	if m.analyticsFilter.text != "" {
		sb.WriteString(dimStyle.Render(fmt.Sprintf(" · filtered: %q", m.analyticsFilter.text)))
	}
	sb.WriteString("\n")
	sb.WriteString("  " + surface1Style.Render(strings.Repeat("─", w-4)) + "\n")

	for i, model := range models {
		isSelected := i == m.analyticsModelCursor
		isExpanded := m.analyticsModelExpand[model.name]

		sb.WriteString(renderModelRow(model, isSelected, isExpanded, w))

		if isExpanded && len(model.providers) > 0 {
			sb.WriteString(renderModelProviderBreakdown(model, w))
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

func renderModelRow(model modelCostEntry, selected, expanded bool, w int) string {
	var sb strings.Builder

	// Cursor indicator
	cursor := "  "
	if selected {
		cursor = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▸ ")
	}

	// Expand/collapse indicator
	expandIcon := " "
	if len(model.providers) > 1 {
		if expanded {
			expandIcon = dimStyle.Render("▾")
		} else {
			expandIcon = dimStyle.Render("▸")
		}
	}

	// Model name with color
	nameW := clamp(w/3, 16, 30)
	nameStyle := lipgloss.NewStyle().Foreground(model.color)
	if selected {
		nameStyle = nameStyle.Bold(true)
	}
	name := nameStyle.Render(truncStr(model.name, nameW))

	// Cost
	costStr := tealBoldStyle.Render(formatUSD(model.cost))

	// Tokens
	tokens := model.inputTokens + model.outputTokens
	tokenStr := sapphireStyle.Render(formatTokens(tokens))

	// Provider count
	provCount := ""
	if len(model.providers) > 1 {
		provCount = dimStyle.Render(fmt.Sprintf("%d providers", len(model.providers)))
	} else if len(model.providers) == 1 {
		provCount = dimStyle.Render(model.providers[0].provider)
	}

	// Efficiency
	effStr := ""
	if model.cost > 0 && tokens > 0 {
		eff := model.cost / tokens * 1000
		effStr = yellowStyle.Render(fmt.Sprintf("$%.3f/1K", eff))
	}

	// Compose the row
	sb.WriteString(cursor + expandIcon + " ")
	sb.WriteString(padRight(name, nameW) + "  ")
	sb.WriteString(padLeft(costStr, 10) + "  ")
	sb.WriteString(padLeft(tokenStr, 10) + "  ")
	sb.WriteString(padLeft(effStr, 10) + "  ")
	sb.WriteString(provCount)
	sb.WriteString("\n")

	return sb.String()
}

func renderModelProviderBreakdown(model modelCostEntry, w int) string {
	var sb strings.Builder

	totalCost := model.cost
	totalTokens := model.inputTokens + model.outputTokens

	for i, split := range model.providers {
		// Tree connector
		connector := "├─"
		if i == len(model.providers)-1 {
			connector = "└─"
		}
		prefix := "     " + dimStyle.Render(connector) + " "

		// Provider name
		provNameW := clamp(w/4, 12, 20)
		provName := dimStyle.Copy().Bold(true).Render(truncStr(split.provider, provNameW))

		// Cost + percentage
		costStr := tealStyle.Render(formatUSD(split.cost))
		pctCost := ""
		if totalCost > 0 {
			pct := split.cost / totalCost * 100
			pctCost = dimStyle.Render(fmt.Sprintf("%.0f%%", pct))
		}

		// Mini gauge showing cost proportion
		gaugeW := clamp(w/5, 8, 16)
		gauge := ""
		if totalCost > 0 {
			pct := split.cost / totalCost * 100
			gauge = RenderInlineGauge(pct, gaugeW)
		}

		// Tokens
		splitTokens := split.inputTokens + split.outputTokens
		tokenStr := sapphireStyle.Render(formatTokens(splitTokens))

		// Efficiency
		effStr := ""
		if split.cost > 0 && splitTokens > 0 {
			eff := split.cost / splitTokens * 1000
			effStr = yellowStyle.Render(fmt.Sprintf("$%.3f/1K", eff))
		}

		_ = totalTokens

		sb.WriteString(prefix)
		sb.WriteString(padRight(provName, provNameW) + "  ")
		sb.WriteString(padLeft(costStr, 9) + " ")
		sb.WriteString(padLeft(pctCost, 4) + "  ")
		sb.WriteString(gauge + "  ")
		sb.WriteString(padLeft(tokenStr, 10) + "  ")
		sb.WriteString(padLeft(effStr, 10))
		sb.WriteString("\n")
	}

	// Token detail row for expanded model
	if model.inputTokens > 0 || model.outputTokens > 0 {
		sb.WriteString("     " + dimStyle.Render("   "))
		parts := []string{}
		if model.inputTokens > 0 {
			parts = append(parts, dimStyle.Render("in:")+sapphireStyle.Render(formatTokens(model.inputTokens)))
		}
		if model.outputTokens > 0 {
			parts = append(parts, dimStyle.Render("out:")+sapphireStyle.Render(formatTokens(model.outputTokens)))
		}
		sb.WriteString(strings.Join(parts, "  "))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	return sb.String()
}

// ─── Spend Tab ────────────────────────────────────────────────

func renderAnalyticsSpend(data costData, summary analyticsSummary, w int) string {
	var sb strings.Builder

	// KPI row for spend context
	if w >= 60 {
		kpis := []string{
			renderKPIBlock("Total", formatUSD(data.totalCost), fmt.Sprintf("%d providers", data.providerCount), colorTeal),
			renderKPIBlock("Trend", renderTrendPercent(summary.recentCostAvg, summary.previousCostAvg), "7d vs prior", colorYellow),
		}
		if summary.peakCost > 0 {
			kpis = append(kpis, renderKPIBlock("Peak Day", formatUSD(summary.peakCost), summary.peakCostDate, colorPeach))
		}
		sb.WriteString("  " + strings.Join(kpis, "  ") + "\n\n")
	}

	// Daily cost by provider stacked chart
	if stacked := renderProviderCostStackedChart(data, w, 9); stacked != "" {
		sb.WriteString(stacked)
		sb.WriteString("\n")
	}

	// Provider cost table
	if costs := renderCostTableFull(data, w); costs != "" {
		sb.WriteString(costs)
		sb.WriteString("\n")
	}

	// Cost efficiency table
	if eff := renderCostEfficiencyTable(data.models, w, 12); eff != "" {
		sb.WriteString(eff)
	}

	return strings.TrimRight(sb.String(), "\n")
}

func renderCostTableFull(data costData, w int) string {
	if len(data.providers) == 0 {
		return ""
	}

	hasCost := false
	for _, p := range data.providers {
		if p.cost > 0 || p.todayCost > 0 || p.weekCost > 0 {
			hasCost = true
			break
		}
	}
	if !hasCost && len(data.budgets) == 0 {
		return ""
	}

	var sb strings.Builder

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorRosewater)
	sb.WriteString("  " + sectionStyle.Render("PROVIDER SPEND") + "\n")
	sb.WriteString("  " + surface1Style.Render(strings.Repeat("─", w-4)) + "\n")

	provW := clamp(w/4, 14, 22)
	colW := clamp((w-provW-8)/4, 8, 12)

	head := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(head.Render("Provider"), provW) + " " +
		padLeft(head.Render("Today"), colW) + " " +
		padLeft(head.Render("7 Day"), colW) + " " +
		padLeft(head.Render("All-Time"), colW) + " " +
		padLeft(head.Render("Budget"), colW+6) + "\n")

	budgetMap := make(map[string]budgetEntry)
	for _, b := range data.budgets {
		base := strings.Split(b.name, " (")[0]
		if existing, ok := budgetMap[base]; !ok || b.limit > existing.limit {
			budgetMap[base] = b
		}
	}

	providers := make([]providerCostEntry, len(data.providers))
	copy(providers, data.providers)
	sort.Slice(providers, func(i, j int) bool { return providers[i].cost > providers[j].cost })

	for _, p := range providers {
		provColor := p.color
		switch p.status {
		case core.StatusLimited:
			provColor = colorRed
		case core.StatusNearLimit:
			provColor = colorYellow
		case core.StatusError, core.StatusAuth:
			provColor = colorRed
		}
		provStyle := lipgloss.NewStyle().Foreground(provColor).Bold(true)
		provName := provStyle.Render(truncStr(p.name, provW-2))
		if p.status == core.StatusLimited {
			provName += " " + redStyle.Render("!")
		}

		todayStr := dimStyle.Render("—")
		if p.todayCost > 0 {
			todayStr = tealStyle.Render(formatUSD(p.todayCost))
		}

		weekStr := dimStyle.Render("—")
		if p.weekCost > 0 {
			weekStr = tealStyle.Render(formatUSD(p.weekCost))
		}

		allTimeStr := dimStyle.Render("—")
		if p.cost > 0 {
			allTimeStr = tealBoldStyle.Render(formatUSD(p.cost))
		}

		budgetStr := dimStyle.Render("—")
		if b, ok := budgetMap[p.name]; ok && b.limit > 0 {
			pct := b.used / b.limit * 100
			gauge := RenderInlineGauge(pct, 8)
			budgetStr = gauge + " " + dimStyle.Render(fmt.Sprintf("%.0f%%", pct))
		}

		sb.WriteString("  " + padRight(provName, provW) + " " +
			padLeft(todayStr, colW) + " " +
			padLeft(weekStr, colW) + " " +
			padLeft(allTimeStr, colW) + " " +
			padLeft(budgetStr, colW+6) + "\n")
	}

	return sb.String()
}

func renderCostEfficiencyTable(models []modelCostEntry, w int, limit int) string {
	all := filterTokenModels(models)
	if len(all) == 0 {
		return ""
	}

	// Only show models with cost data
	var withCost []modelCostEntry
	for _, m := range all {
		if m.cost > 0 {
			withCost = append(withCost, m)
		}
	}
	if len(withCost) == 0 {
		return ""
	}

	sort.Slice(withCost, func(i, j int) bool {
		tokI := withCost[i].inputTokens + withCost[i].outputTokens
		tokJ := withCost[j].inputTokens + withCost[j].outputTokens
		if tokI <= 0 || tokJ <= 0 {
			return withCost[i].cost > withCost[j].cost
		}
		effI := withCost[i].cost / tokI
		effJ := withCost[j].cost / tokJ
		return effI < effJ // cheapest first
	})
	if len(withCost) > limit {
		withCost = withCost[:limit]
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorYellow)
	sb.WriteString("  " + sectionStyle.Render("COST EFFICIENCY (cheapest first)") + "\n")
	sb.WriteString("  " + surface1Style.Render(strings.Repeat("─", w-4)) + "\n")

	nameW := clamp(w/3, 16, 28)
	provW := clamp(w/5, 10, 18)

	head := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(head.Render("Model"), nameW) + " " +
		padRight(head.Render("Provider"), provW) + " " +
		padLeft(head.Render("$/1K tok"), 10) + " " +
		padLeft(head.Render("Cost"), 10) + " " +
		padLeft(head.Render("Tokens"), 10) + "\n")

	for _, m := range withCost {
		tokens := m.inputTokens + m.outputTokens
		eff := "—"
		if tokens > 0 {
			eff = fmt.Sprintf("$%.4f", m.cost/tokens*1000)
		}
		prov := primaryProvider(m)

		sb.WriteString("  " +
			padRight(lipgloss.NewStyle().Foreground(m.color).Render(truncStr(m.name, nameW)), nameW) + " " +
			padRight(dimStyle.Render(truncStr(prov, provW)), provW) + " " +
			padLeft(yellowStyle.Render(eff), 10) + " " +
			padLeft(tealStyle.Render(formatUSD(m.cost)), 10) + " " +
			padLeft(sapphireStyle.Render(formatTokens(tokens)), 10) + "\n")
	}
	return sb.String()
}

// ─── Activity Tab ─────────────────────────────────────────────

func renderAnalyticsActivity(data costData, w int) string {
	var sb strings.Builder

	// Token distribution chart
	if tokenDist := renderDailyTokenDistributionChart(data, w, 12); tokenDist != "" {
		sb.WriteString(tokenDist)
		sb.WriteString("\n")
	}

	// Two-column: heatmap + token/usage info
	if w >= 80 {
		gap := 2
		colW := (w - 4 - gap) / 2
		if colW < 36 {
			colW = 36
		}
		left := strings.TrimRight(renderProviderModelDailyUsage(data, colW, 12), "\n")
		right := strings.TrimRight(renderActivitySummary(data, colW), "\n")

		if left != "" || right != "" {
			if left != "" && right != "" {
				sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right))
			} else if left != "" {
				sb.WriteString(left)
			} else {
				sb.WriteString(right)
			}
			sb.WriteString("\n")
		}
	} else {
		if heat := renderProviderModelDailyUsage(data, w, 10); heat != "" {
			sb.WriteString(heat)
			sb.WriteString("\n")
		}
		if activity := renderActivitySummary(data, w); activity != "" {
			sb.WriteString(activity)
			sb.WriteString("\n")
		}
	}

	// Usage gauges
	if gauges := renderUsageGaugesSection(data, w); gauges != "" {
		sb.WriteString(gauges)
	}

	return strings.TrimRight(sb.String(), "\n")
}

func renderActivitySummary(data costData, w int) string {
	if len(data.tokenActivity) == 0 {
		return ""
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorSapphire)
	sb.WriteString("  " + sectionStyle.Render("TOKEN ACTIVITY") + "\n")
	sb.WriteString("  " + surface1Style.Render(strings.Repeat("─", w-4)) + "\n")

	nameW := clamp(w/3, 14, 24)
	for _, entry := range data.tokenActivity {
		label := truncStr(entry.provider+" · "+entry.name, nameW)
		provStyle := lipgloss.NewStyle().Foreground(entry.color)

		value := ""
		if entry.input > 0 || entry.output > 0 {
			parts := []string{}
			if entry.input > 0 {
				parts = append(parts, "in:"+formatTokens(entry.input))
			}
			if entry.output > 0 {
				parts = append(parts, "out:"+formatTokens(entry.output))
			}
			if entry.cached > 0 {
				parts = append(parts, "cache:"+formatTokens(entry.cached))
			}
			value = sapphireStyle.Render(strings.Join(parts, " "))
		} else if entry.total > 0 {
			value = sapphireStyle.Render(formatTokens(entry.total))
		}

		sb.WriteString("  " + padRight(provStyle.Render(label), nameW) + "  " + value + "\n")
	}
	return sb.String()
}

func renderUsageGaugesSection(data costData, w int) string {
	if len(data.usageGauges) == 0 {
		return ""
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorYellow)
	sb.WriteString("  " + sectionStyle.Render("RATE LIMITS & QUOTAS") + "\n")
	sb.WriteString("  " + surface1Style.Render(strings.Repeat("─", w-4)) + "\n")

	nameW := clamp(w/3, 14, 28)
	gaugeW := clamp(w/4, 10, 20)

	for _, g := range data.usageGauges {
		label := truncStr(g.provider+" · "+g.name, nameW)
		provStyle := lipgloss.NewStyle().Foreground(g.color)
		gauge := RenderInlineGauge(g.pctUsed, gaugeW)
		pctStr := dimStyle.Render(fmt.Sprintf("%.0f%% %s", g.pctUsed, g.window))

		sb.WriteString("  " + padRight(provStyle.Render(label), nameW) + "  " + gauge + " " + pctStr + "\n")
	}
	return sb.String()
}

// ─── Chart helpers (shared) ───────────────────────────────────

func renderAnalyticsKPIHeader(data costData, summary analyticsSummary, w int) string {
	if w < 40 {
		return ""
	}
	kpis := []string{
		renderKPIBlock("Total Cost", formatUSD(data.totalCost), fmt.Sprintf("%d providers", data.providerCount), colorTeal),
		renderKPIBlock("Total Tokens", formatTokens(data.totalInput+data.totalOutput), fmt.Sprintf("%d active days", summary.activeDays), colorSapphire),
		renderKPIBlock("Cost Trend", renderTrendPercent(summary.recentCostAvg, summary.previousCostAvg), "last 7d vs prior 7d", colorYellow),
		renderKPIBlock("Top-3 Concentration", fmt.Sprintf("%.0f%%", summary.concentrationTop3*100), "provider spend share", colorPeach),
	}
	return "  " + strings.Join(kpis, "  ")
}

func renderKPIBlock(title, value, subtitle string, accent lipgloss.Color) string {
	titleStr := analyticsCardTitleStyle.Render(title)
	valueStr := analyticsCardValueStyle.Copy().Foreground(accent).Render(value)
	subtitleStr := analyticsCardSubtitleStyle.Render(subtitle)
	return titleStr + " " + valueStr + " " + subtitleStr
}

func renderTrendPercent(current, previous float64) string {
	if current <= 0 && previous <= 0 {
		return "—"
	}
	if previous <= 0 {
		return "+∞"
	}
	delta := (current - previous) / previous * 100
	sign := "+"
	if delta < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.1f%%", sign, delta)
}

func renderProviderCostStackedChart(data costData, w, h int) string {
	series, observedCount, estimatedCount := buildProviderDailyCostSeries(data)
	if len(series) == 0 {
		return ""
	}

	chart := RenderTimeChart(TimeChartSpec{
		Title:      "DAILY COST BY PROVIDER",
		Mode:       TimeChartStacked,
		Series:     series,
		Height:     h,
		MaxSeries:  8,
		WindowDays: 30,
		YFmt:       formatCostAxis,
	}, w)
	if estimatedCount > 0 {
		chart += "  " + dimStyle.Render(fmt.Sprintf("Observed: %d provider(s). Estimated from activity: %d provider(s).", observedCount, estimatedCount)) + "\n"
	}
	return chart
}

func renderTotalCostTrend(data costData, summary analyticsSummary, w, h int) string {
	providerSeries, _, _ := buildProviderDailyCostSeries(data)
	daily := aggregateSeriesByDate(providerSeries)
	if !hasNonZeroData(daily) {
		daily = summary.dailyCost
	}
	if !hasNonZeroData(daily) {
		return ""
	}
	series := []BrailleSeries{
		{Label: "daily cost", Color: colorTeal, Points: daily},
	}
	return RenderTimeChart(TimeChartSpec{
		Title:      "TOTAL COST OVER TIME",
		Mode:       TimeChartBars,
		Series:     series,
		Height:     h,
		WindowDays: 30,
		YFmt:       formatCostAxis,
	}, w)
}

func renderProviderModelDailyUsage(data costData, w, maxRows int) string {
	spec, ok := buildProviderModelHeatmapSpec(data, maxRows, 18)
	if !ok {
		return ""
	}
	return RenderHeatmap(spec, w)
}

func renderDailyTokenDistributionChart(data costData, w int, limit int) string {
	series := buildProviderModelTokenDistributionSeries(data, limit)
	if len(series) == 0 {
		return ""
	}
	return RenderTimeChart(TimeChartSpec{
		Title:      "DAILY TOKEN DISTRIBUTION (Model · Provider)",
		Mode:       TimeChartStacked,
		Series:     series,
		Height:     9,
		MaxSeries:  limit,
		WindowDays: 30,
		YFmt:       formatChartValue,
	}, w)
}

// ─── Series builders ──────────────────────────────────────────

func buildProviderDailyCostSeries(data costData) ([]BrailleSeries, int, int) {
	groupByProvider := make(map[string]timeSeriesGroup, len(data.timeSeries))
	for _, g := range data.timeSeries {
		groupByProvider[g.providerName] = g
	}

	var out []BrailleSeries
	observedCount := 0
	estimatedCount := 0
	for _, p := range data.providers {
		if p.cost <= 0 && p.todayCost <= 0 && p.weekCost <= 0 {
			continue
		}
		var g *timeSeriesGroup
		if gg, ok := groupByProvider[p.name]; ok {
			g = &gg
		}
		pts, observed, estimated := deriveProviderDailyCostPoints(p, g, data.referenceTime)
		if !hasNonZeroData(pts) {
			continue
		}
		pts = clipSeriesPointsByRecentDates(pts, 30)
		if observed {
			observedCount++
		} else if estimated {
			estimatedCount++
		}
		out = append(out, BrailleSeries{
			Label:  truncStr(p.name, 20),
			Color:  p.color,
			Points: pts,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		li := seriesTotal(out[i].Points)
		lj := seriesTotal(out[j].Points)
		if li == lj {
			return out[i].Label < out[j].Label
		}
		return li > lj
	})
	if len(out) == 0 {
		for _, g := range data.timeSeries {
			pts, ok := g.series["cost"]
			if !ok || !hasNonZeroData(pts) {
				continue
			}
			observedCount++
			out = append(out, BrailleSeries{
				Label:  truncStr(g.providerName, 20),
				Color:  g.color,
				Points: clipSeriesPointsByRecentDates(pts, 30),
			})
		}
	}
	return out, observedCount, estimatedCount
}

func deriveProviderDailyCostPoints(p providerCostEntry, group *timeSeriesGroup, referenceTime time.Time) ([]core.TimePoint, bool, bool) {
	if group != nil {
		for _, key := range []string{"cost", "analytics_cost", "daily_cost"} {
			if pts, ok := group.series[key]; ok && hasNonZeroData(pts) {
				return pts, true, false
			}
		}
	}
	if referenceTime.IsZero() {
		referenceTime = time.Now()
	}
	nowDate := referenceTime.Format("2006-01-02")

	if p.todayCost > 0 {
		return []core.TimePoint{{Date: nowDate, Value: p.todayCost}}, true, false
	}

	if group != nil && p.weekCost > 0 {
		if activity := clipSeriesPointsByRecentDates(selectBestProviderCostWeightSeries(group.series), 7); hasNonZeroData(activity) {
			if scaled := scaleSeriesToTotal(activity, p.weekCost); hasNonZeroData(scaled) {
				return scaled, false, true
			}
		}
	}

	return nil, false, false
}

func scaleSeriesToTotal(activity []core.TimePoint, total float64) []core.TimePoint {
	if len(activity) == 0 || total <= 0 {
		return nil
	}
	sum := seriesTotal(activity)
	if sum <= 0 {
		return nil
	}
	out := make([]core.TimePoint, 0, len(activity))
	for _, a := range activity {
		out = append(out, core.TimePoint{
			Date:  a.Date,
			Value: total * (a.Value / sum),
		})
	}
	return out
}

func aggregateSeriesByDate(series []BrailleSeries) []core.TimePoint {
	if len(series) == 0 {
		return nil
	}
	byDate := make(map[string]float64)
	for _, s := range series {
		for _, p := range s.Points {
			if p.Value > 0 {
				byDate[p.Date] += p.Value
			}
		}
	}
	if len(byDate) == 0 {
		return nil
	}
	dates := core.SortedStringKeys(byDate)
	out := make([]core.TimePoint, 0, len(dates))
	for _, d := range dates {
		out = append(out, core.TimePoint{Date: d, Value: byDate[d]})
	}
	return out
}

func buildProviderModelTokenDistributionSeries(data costData, limit int) []BrailleSeries {
	type candidate struct {
		series BrailleSeries
		volume float64
	}
	var cands []candidate

	for _, g := range data.timeSeries {
		for _, named := range core.ExtractAnalyticsModelSeries(g.series) {
			pts := clipSeriesPointsByRecentDates(named.Points, 30)
			if !hasNonZeroData(pts) {
				continue
			}
			model := named.Name
			label := truncStr(prettifyModelName(model)+" · "+g.providerName, 34)

			cands = append(cands, candidate{
				series: BrailleSeries{
					Label:  label,
					Color:  stableModelColor(model, g.providerID),
					Points: pts,
				},
				volume: seriesTotal(pts),
			})
		}
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].volume == cands[j].volume {
			return cands[i].series.Label < cands[j].series.Label
		}
		return cands[i].volume > cands[j].volume
	})
	if limit > 0 && len(cands) > limit {
		cands = cands[:limit]
	}

	out := make([]BrailleSeries, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.series)
	}
	return out
}

func selectBestProviderCostWeightSeries(series map[string][]core.TimePoint) []core.TimePoint {
	if pts := core.SelectAnalyticsWeightSeries(series); hasNonZeroData(pts) {
		return pts
	}
	return nil
}

func buildProviderModelHeatmapSpec(data costData, maxRows int, lastDays int) (HeatmapSpec, bool) {
	type row struct {
		label string
		color lipgloss.Color
		vals  map[string]float64
		total float64
	}
	var rows []row
	dateSet := make(map[string]bool)

	for _, g := range data.timeSeries {
		for _, named := range core.ExtractAnalyticsModelSeries(g.series) {
			pts := named.Points
			total := seriesTotal(pts)
			if total <= 0 {
				continue
			}
			vals := make(map[string]float64, len(pts))
			for _, p := range pts {
				if p.Value > 0 {
					vals[p.Date] = p.Value
					dateSet[p.Date] = true
				}
			}
			model := prettifyModelName(named.Name)
			rows = append(rows, row{
				label: truncStr(g.providerName+" · "+model, 42),
				color: stableModelColor(named.Name, g.providerID),
				vals:  vals,
				total: total,
			})
		}
	}

	if len(rows) == 0 || len(dateSet) == 0 {
		return HeatmapSpec{}, false
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].total > rows[j].total })
	if maxRows > 0 && len(rows) > maxRows {
		rows = rows[:maxRows]
	}

	dates := core.SortedStringKeys(dateSet)
	dates = clipDatesToRecent(dates, lastDays)

	labels := make([]string, len(rows))
	rowColors := make([]lipgloss.Color, len(rows))
	values := make([][]float64, len(rows))
	for i, r := range rows {
		labels[i] = r.label
		rowColors[i] = r.color
		line := make([]float64, len(dates))
		for j, d := range dates {
			line[j] = r.vals[d]
		}
		values[i] = line
	}
	return HeatmapSpec{
		Title:     "DAILY USAGE HEATMAP (Provider · Model)",
		Rows:      labels,
		Cols:      dates,
		Values:    values,
		RowColors: rowColors,
		MaxCols:   0,
		RowScale:  true,
	}, true
}

// ─── Utility functions ────────────────────────────────────────

func hasNonZeroData(pts []core.TimePoint) bool {
	for _, p := range pts {
		if p.Value > 0 {
			return true
		}
	}
	return false
}

func clipDatesToRecent(dates []string, days int) []string {
	if len(dates) == 0 || days <= 0 {
		return dates
	}
	maxDate, err := time.Parse("2006-01-02", dates[len(dates)-1])
	if err != nil {
		if len(dates) > days {
			return dates[len(dates)-days:]
		}
		return dates
	}
	cutoff := maxDate.AddDate(0, 0, -(days - 1))
	out := make([]string, 0, len(dates))
	for _, d := range dates {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		if t.Before(cutoff) || t.After(maxDate) {
			continue
		}
		out = append(out, d)
	}
	return out
}

func seriesTotal(points []core.TimePoint) float64 {
	total := 0.0
	for _, p := range points {
		total += p.Value
	}
	return total
}

func clipSeriesPointsByRecentDates(points []core.TimePoint, days int) []core.TimePoint {
	if len(points) == 0 || days <= 0 {
		return points
	}
	dates := make([]string, len(points))
	for i := range points {
		dates[i] = points[i].Date
	}
	dates = clipDatesToRecent(dates, days)
	if len(dates) == 0 {
		return points
	}
	allow := make(map[string]bool, len(dates))
	for _, d := range dates {
		allow[d] = true
	}
	out := make([]core.TimePoint, 0, len(points))
	for _, p := range points {
		if allow[p.Date] {
			out = append(out, p)
		}
	}
	return out
}

func computeAnalyticsSummary(data costData) analyticsSummary {
	var s analyticsSummary
	costByDate := make(map[string]float64)
	tokensByDate := make(map[string]float64)
	messagesByDate := make(map[string]float64)

	for _, g := range data.timeSeries {
		if pts, ok := g.series["cost"]; ok {
			for _, p := range pts {
				costByDate[p.Date] += p.Value
			}
		}

		hasTotalTokens := false
		if pts, ok := g.series["tokens_total"]; ok {
			hasTotalTokens = true
			for _, p := range pts {
				tokensByDate[p.Date] += p.Value
			}
		}
		if !hasTotalTokens {
			for _, named := range core.ExtractAnalyticsModelSeries(g.series) {
				pts := named.Points
				for _, p := range pts {
					tokensByDate[p.Date] += p.Value
				}
			}
		}

		if pts, ok := g.series["messages"]; ok {
			for _, p := range pts {
				messagesByDate[p.Date] += p.Value
			}
		}
	}

	s.dailyCost = core.SortedTimePoints(costByDate)
	s.dailyTokens = core.SortedTimePoints(tokensByDate)
	s.dailyMessages = core.SortedTimePoints(messagesByDate)
	s.activeDays = countNonZeroDays(s.dailyCost, s.dailyTokens, s.dailyMessages)

	s.peakCostDate, s.peakCost = maxPoint(s.dailyCost)
	s.peakTokenDate, s.peakTokens = maxPoint(s.dailyTokens)

	s.recentCostAvg, s.previousCostAvg = splitWindowAverages(s.dailyCost, 7)
	s.recentTokensAvg, s.previousTokensAvg = splitWindowAverages(s.dailyTokens, 7)
	s.costVolatility = coefficientOfVariation(s.dailyCost)
	s.tokenVolatility = coefficientOfVariation(s.dailyTokens)
	s.concentrationTop3 = providerConcentration(data.providers, 3)

	for _, p := range s.dailyCost {
		t, err := time.Parse("2006-01-02", p.Date)
		if err != nil {
			continue
		}
		wd := int(t.Weekday())
		s.dayOfWeekCost[wd] += p.Value
		s.dayOfWeekCount[wd]++
	}
	return s
}

func maxPoint(points []core.TimePoint) (string, float64) {
	bestDate := ""
	best := 0.0
	for _, p := range points {
		if p.Value > best {
			bestDate = p.Date
			best = p.Value
		}
	}
	return bestDate, best
}

func splitWindowAverages(points []core.TimePoint, window int) (float64, float64) {
	if len(points) == 0 || window <= 0 {
		return 0, 0
	}
	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.Value
	}

	recentStart := len(values) - window
	if recentStart < 0 {
		recentStart = 0
	}
	recent := avg(values[recentStart:])

	prevStart := recentStart - window
	if prevStart < 0 {
		prevStart = 0
	}
	prevEnd := recentStart
	if prevEnd < prevStart {
		prevEnd = prevStart
	}
	prev := avg(values[prevStart:prevEnd])

	return recent, prev
}

func avg(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

func stddev(v []float64, mean float64) float64 {
	if len(v) < 2 {
		return 0
	}
	sum := 0.0
	for _, x := range v {
		d := x - mean
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(v)))
}

func coefficientOfVariation(points []core.TimePoint) float64 {
	if len(points) < 2 {
		return 0
	}
	values := make([]float64, 0, len(points))
	for _, p := range points {
		if p.Value > 0 {
			values = append(values, p.Value)
		}
	}
	if len(values) < 2 {
		return 0
	}
	m := avg(values)
	if m <= 0 {
		return 0
	}
	return stddev(values, m) / m
}

func providerConcentration(providers []providerCostEntry, topN int) float64 {
	if len(providers) == 0 || topN <= 0 {
		return 0
	}
	vals := make([]float64, 0, len(providers))
	total := 0.0
	for _, p := range providers {
		if p.cost <= 0 {
			continue
		}
		vals = append(vals, p.cost)
		total += p.cost
	}
	if total <= 0 || len(vals) == 0 {
		return 0
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] > vals[j] })
	if len(vals) < topN {
		topN = len(vals)
	}
	top := 0.0
	for i := 0; i < topN; i++ {
		top += vals[i]
	}
	return top / total
}

func countNonZeroDays(series ...[]core.TimePoint) int {
	days := make(map[string]bool)
	for _, pts := range series {
		for _, p := range pts {
			if p.Value > 0 {
				days[p.Date] = true
			}
		}
	}
	return len(days)
}

func padLeft(s string, w int) string {
	vw := lipgloss.Width(s)
	if vw >= w {
		return s
	}
	return strings.Repeat(" ", w-vw) + s
}

func filterTokenModels(models []modelCostEntry) []modelCostEntry {
	var out []modelCostEntry
	for _, m := range models {
		if m.inputTokens > 0 || m.outputTokens > 0 || m.cost > 0 {
			out = append(out, m)
		}
	}
	return out
}

func primaryProvider(m modelCostEntry) string {
	if len(m.providers) > 0 {
		return m.providers[0].provider
	}
	if m.provider != "" {
		return m.provider
	}
	return "—"
}

func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func sortedMetricKeys(m map[string]core.Metric) []string {
	return core.SortedStringKeys(m)
}
