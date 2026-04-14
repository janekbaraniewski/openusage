package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type analyticsMetric struct {
	label  string
	value  string
	detail string
	color  lipgloss.Color
}

type analyticsRankRow struct {
	name   string
	value  string
	detail string
	series []core.TimePoint
	color  lipgloss.Color
}

func renderAnalyticsOverviewRedesign(data costData, summary analyticsSummary, w int) string {
	var sections []string

	sections = append(sections, renderAnalyticsContextLine(data, summary))
	sections = append(sections, renderAnalyticsMetricStrip([]analyticsMetric{
		{
			label:  "Window Spend",
			value:  formatUSD(data.totalCost),
			detail: analyticsWindowSubtitle(data),
			color:  colorTeal,
		},
		{
			label:  "Spend / Active Day",
			value:  formatUSD(analyticsPerActiveDay(data.totalCost, summary.activeDays)),
			detail: fmt.Sprintf("%d active days", summary.activeDays),
			color:  colorYellow,
		},
		{
			label:  "Token Volume",
			value:  formatTokens(data.totalInput + data.totalOutput),
			detail: analyticsTokenMixSubtitle(data),
			color:  colorSapphire,
		},
		{
			label:  "Spend Trend",
			value:  renderTrendPercent(summary.recentCostAvg, summary.previousCostAvg),
			detail: analyticsComparisonLabel(data.timeWindow),
			color:  colorPeach,
		},
		{
			label:  "Provider Concentration",
			value:  fmt.Sprintf("%.0f%%", summary.concentrationTop3*100),
			detail: "top 3 provider share",
			color:  colorRosewater,
		},
	}, w))

	if trend := renderTotalCostTrend(data, summary, w, 10); trend != "" {
		sections = append(sections, trend)
	}

	panels := []string{
		renderAnalyticsProviderLeaderboardPanel(data, w, 6),
		renderAnalyticsModelLeaderboardPanel(data, w, 6),
		renderAnalyticsInsightPanel(data, summary, w),
	}
	sections = append(sections, renderAnalyticsResponsivePanelGrid(panels, w))

	return strings.TrimRight(strings.Join(filterNonEmptyStrings(sections), "\n\n"), "\n")
}

func (m Model) renderAnalyticsModelsRedesign(data costData, w int) string {
	models := filterTokenModels(data.models)
	if len(models) == 0 {
		return "\n" + dimStyle.Render("  No model usage data available.")
	}
	sortModels(models, m.analyticsSortBy)
	if m.analyticsModelCursor < 0 {
		m.analyticsModelCursor = 0
	}
	if m.analyticsModelCursor >= len(models) {
		m.analyticsModelCursor = len(models) - 1
	}
	selected := models[m.analyticsModelCursor]

	header := renderAnalyticsMetricStrip([]analyticsMetric{
		{
			label:  "Tracked Models",
			value:  fmt.Sprintf("%d", len(models)),
			detail: fmt.Sprintf("%d providers", data.providerCount),
			color:  colorTeal,
		},
		{
			label:  "Selected Spend",
			value:  formatUSD(selected.cost),
			detail: analyticsShareLabel(selected.cost, data.totalCost),
			color:  colorPeach,
		},
		{
			label:  "Selected Tokens",
			value:  formatTokens(selected.inputTokens + selected.outputTokens),
			detail: analyticsModelEfficiencyLabel(selected),
			color:  colorSapphire,
		},
	}, w)

	listPanel := renderAnalyticsModelListPanel(models, m.analyticsModelCursor, m.analyticsSortBy, data.totalCost, w)
	detailPanel := renderAnalyticsSelectedModelPanel(selected, m.analyticsModelExpand[selected.name], data, w)
	trend := renderAnalyticsSelectedModelTrend(selected, data, w)

	if w >= 128 {
		gap := 2
		leftW := (w - gap) * 3 / 5
		rightW := w - gap - leftW
		left := listPanel
		rightBlocks := filterNonEmptyStrings([]string{detailPanel, trend})
		right := strings.Join(rightBlocks, "\n\n")
		return header + "\n\n  " + lipgloss.JoinHorizontal(lipgloss.Top, padRight(left, leftW), strings.Repeat(" ", gap), padRight(right, rightW))
	}

	sections := []string{header, listPanel, detailPanel}
	if trend != "" {
		sections = append(sections, trend)
	}
	return strings.TrimRight(strings.Join(filterNonEmptyStrings(sections), "\n\n"), "\n")
}

func renderAnalyticsSpendRedesign(data costData, summary analyticsSummary, w int) string {
	peakProvider, peakProviderCost := analyticsTopProvider(data)
	sections := []string{
		renderAnalyticsMetricStrip([]analyticsMetric{
			{
				label:  "Window Spend",
				value:  formatUSD(data.totalCost),
				detail: analyticsWindowSubtitle(data),
				color:  colorTeal,
			},
			{
				label:  "Peak Day",
				value:  formatUSD(summary.peakCost),
				detail: summary.peakCostDate,
				color:  colorPeach,
			},
			{
				label:  "Top Provider",
				value:  peakProvider,
				detail: analyticsShareLabel(peakProviderCost, data.totalCost),
				color:  colorRosewater,
			},
			{
				label:  "Trend",
				value:  renderTrendPercent(summary.recentCostAvg, summary.previousCostAvg),
				detail: analyticsComparisonLabel(data.timeWindow),
				color:  colorYellow,
			},
		}, w),
	}

	if stacked := renderProviderCostStackedChart(data, w, 10); stacked != "" {
		sections = append(sections, stacked)
	}

	panels := []string{
		renderAnalyticsProviderSpendPanel(data, summary, w),
		renderAnalyticsBudgetPressurePanel(data, w),
	}
	sections = append(sections, renderAnalyticsResponsivePanelGrid(panels, w))

	if eff := renderAnalyticsCostEfficiencyPanel(data, w, 10); eff != "" {
		sections = append(sections, eff)
	}

	return strings.TrimRight(strings.Join(filterNonEmptyStrings(sections), "\n\n"), "\n")
}

func renderAnalyticsActivityRedesign(data costData, summary analyticsSummary, w int) string {
	topClient, clientValue := analyticsTopClient(data)
	topProject, projectValue := analyticsTopProject(data)
	topMCP, mcpValue := analyticsTopMCP(data)

	sections := []string{
		renderAnalyticsMetricStrip([]analyticsMetric{
			{
				label:  "Token Volume",
				value:  formatTokens(data.totalInput + data.totalOutput),
				detail: analyticsTokenMixSubtitle(data),
				color:  colorSapphire,
			},
			{
				label:  "Top Client",
				value:  topClient,
				detail: analyticsHotspotValueLabel(clientValue, "tok"),
				color:  colorTeal,
			},
			{
				label:  "Top Project",
				value:  topProject,
				detail: analyticsHotspotValueLabel(projectValue, "req"),
				color:  colorPeach,
			},
			{
				label:  "Top MCP Server",
				value:  topMCP,
				detail: analyticsHotspotValueLabel(mcpValue, "calls"),
				color:  colorYellow,
			},
		}, w),
	}

	if tokenDist := renderDailyTokenDistributionChart(data, w, 10); tokenDist != "" {
		sections = append(sections, tokenDist)
	}

	panels := []string{
		renderAnalyticsClientPanel(data, w, 6),
		renderAnalyticsProjectPanel(data, w, 6),
		renderAnalyticsMCPPanel(data, w, 6),
	}
	sections = append(sections, renderAnalyticsResponsivePanelGrid(panels, w))

	if heat := renderAnalyticsActivityHeatmap(data, w); heat != "" {
		sections = append(sections, heat)
	}

	if summary.peakTokens > 0 {
		sections = append(sections, renderAnalyticsPanel(
			"Peak Activity",
			colorLavender,
			w-2,
			strings.Join([]string{
				renderDotLeaderRow("Peak token day", fmt.Sprintf("%s · %s", summary.peakTokenDate, formatTokens(summary.peakTokens)), w-8),
				renderDotLeaderRow("Token trend", renderTrendPercent(summary.recentTokensAvg, summary.previousTokensAvg), w-8),
			}, "\n"),
		))
	}

	return strings.TrimRight(strings.Join(filterNonEmptyStrings(sections), "\n\n"), "\n")
}

func renderAnalyticsContextLine(data costData, summary analyticsSummary) string {
	parts := []string{
		"Window " + data.timeWindow.Label(),
		fmt.Sprintf("%d providers", data.providerCount),
		fmt.Sprintf("%d active", data.activeCount),
	}
	if summary.activeDays > 0 {
		parts = append(parts, fmt.Sprintf("%d active days", summary.activeDays))
	}
	return "  " + dimStyle.Render(strings.Join(parts, " · "))
}

func renderAnalyticsMetricStrip(metrics []analyticsMetric, w int) string {
	if len(metrics) == 0 {
		return ""
	}
	maxW := max(32, w-2)
	lines := []string{"  "}
	lineW := 2
	for _, metric := range metrics {
		block := renderKPIBlock(metric.label, metric.value, metric.detail, metric.color)
		bw := lipgloss.Width(block)
		sepW := 2
		if lineW > 2 && lineW+sepW+bw > maxW {
			lines = append(lines, "  "+block)
			lineW = 2 + bw
			continue
		}
		if lineW > 2 {
			lines[len(lines)-1] += "  "
			lineW += sepW
		}
		lines[len(lines)-1] += block
		lineW += bw
	}
	return strings.Join(lines, "\n")
}

func renderAnalyticsResponsivePanelGrid(panels []string, w int) string {
	panels = filterNonEmptyStrings(panels)
	if len(panels) == 0 {
		return ""
	}
	if len(panels) == 1 || w < 92 {
		return strings.Join(panels, "\n\n")
	}
	if len(panels) == 2 || w < 138 {
		gap := 2
		colW := (w - gap) / 2
		return "  " + lipgloss.JoinHorizontal(lipgloss.Top,
			padRight(panels[0], colW),
			strings.Repeat(" ", gap),
			padRight(panels[1], colW),
		)
	}

	gap := 2
	colW := (w - 2*gap) / 3
	row := []string{}
	for i := 0; i < len(panels); i += 3 {
		chunk := panels[i:min(i+3, len(panels))]
		parts := make([]string, 0, len(chunk)*2-1)
		for idx, panel := range chunk {
			if idx > 0 {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			parts = append(parts, padRight(panel, colW))
		}
		row = append(row, "  "+lipgloss.JoinHorizontal(lipgloss.Top, parts...))
	}
	return strings.Join(row, "\n\n")
}

func renderAnalyticsProviderLeaderboardPanel(data costData, width, limit int) string {
	providers := append([]providerCostEntry(nil), data.providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].cost > providers[j].cost })
	rows := make([]analyticsRankRow, 0, min(limit, len(providers)))
	for _, provider := range providers {
		if provider.cost <= 0 {
			continue
		}
		rows = append(rows, analyticsRankRow{
			name:   provider.name,
			value:  formatUSD(provider.cost),
			detail: analyticsShareLabel(provider.cost, data.totalCost),
			color:  provider.color,
		})
		if len(rows) >= limit {
			break
		}
	}
	return renderAnalyticsRankPanel("Provider Leaders", colorRosewater, rows, analyticsPanelWidth(width), "Where spend concentrated across providers")
}

func renderAnalyticsModelLeaderboardPanel(data costData, width, limit int) string {
	models := filterTokenModels(data.models)
	sort.Slice(models, func(i, j int) bool { return models[i].cost > models[j].cost })
	rows := make([]analyticsRankRow, 0, min(limit, len(models)))
	for _, model := range models {
		if model.cost <= 0 && model.inputTokens+model.outputTokens <= 0 {
			continue
		}
		rows = append(rows, analyticsRankRow{
			name:   prettifyModelName(model.name),
			value:  formatUSD(model.cost),
			detail: analyticsModelEfficiencyLabel(model),
			color:  model.color,
		})
		if len(rows) >= limit {
			break
		}
	}
	return renderAnalyticsRankPanel("Model Leaders", colorTeal, rows, analyticsPanelWidth(width), "The models driving most spend in the selected window")
}

func renderAnalyticsInsightPanel(data costData, summary analyticsSummary, width int) string {
	topProvider, topProviderCost := analyticsTopProvider(data)
	topClient, clientValue := analyticsTopClient(data)
	topProject, projectValue := analyticsTopProject(data)
	topMCP, mcpValue := analyticsTopMCP(data)

	lines := []string{
		renderDotLeaderRow("Window", data.timeWindow.Label(), analyticsPanelWidth(width)-4),
		renderDotLeaderRow("Spend trend", renderTrendPercent(summary.recentCostAvg, summary.previousCostAvg), analyticsPanelWidth(width)-4),
	}
	if summary.peakCost > 0 {
		lines = append(lines, renderDotLeaderRow("Peak spend day", fmt.Sprintf("%s · %s", summary.peakCostDate, formatUSD(summary.peakCost)), analyticsPanelWidth(width)-4))
	}
	if topProvider != "" {
		lines = append(lines, renderDotLeaderRow("Top provider", fmt.Sprintf("%s · %s", topProvider, analyticsShareLabel(topProviderCost, data.totalCost)), analyticsPanelWidth(width)-4))
	}
	if topClient != "" {
		lines = append(lines, renderDotLeaderRow("Top client", fmt.Sprintf("%s · %s", topClient, analyticsHotspotValueLabel(clientValue, "tok")), analyticsPanelWidth(width)-4))
	}
	if topProject != "" {
		lines = append(lines, renderDotLeaderRow("Top project", fmt.Sprintf("%s · %s", topProject, analyticsHotspotValueLabel(projectValue, "req")), analyticsPanelWidth(width)-4))
	}
	if topMCP != "" {
		lines = append(lines, renderDotLeaderRow("Top MCP", fmt.Sprintf("%s · %s", topMCP, analyticsHotspotValueLabel(mcpValue, "calls")), analyticsPanelWidth(width)-4))
	}

	return renderAnalyticsPanel("What Changed", colorLavender, analyticsPanelWidth(width), strings.Join(lines, "\n"))
}

func renderAnalyticsModelListPanel(models []modelCostEntry, cursor int, sortMode int, totalCost float64, width int) string {
	innerW := analyticsPanelWidth(width) - 4
	nameW := clamp(innerW/2, 18, 34)
	var lines []string
	lines = append(lines, dimStyle.Render(fmt.Sprintf("Sort: %s · j/k move · Enter expands provider splits", sortByLabels[sortMode])))
	lines = append(lines, surface1Style.Render(strings.Repeat("─", innerW)))
	head := dimStyle.Copy().Bold(true)
	lines = append(lines,
		padRight(head.Render("Model"), nameW)+" "+
			padLeft(head.Render("Spend"), 10)+" "+
			padLeft(head.Render("Share"), 8)+" "+
			padLeft(head.Render("Tokens"), 10))
	for i, model := range models {
		cursorMark := " "
		nameStyle := lipgloss.NewStyle().Foreground(model.color)
		if i == cursor {
			cursorMark = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▸")
			nameStyle = nameStyle.Bold(true)
		}
		share := analyticsShareText(model.cost, totalCost)
		line := fmt.Sprintf("%s %s %s %s %s",
			cursorMark,
			padRight(nameStyle.Render(truncStr(prettifyModelName(model.name), nameW-2)), nameW),
			padLeft(tealStyle.Render(formatUSD(model.cost)), 10),
			padLeft(dimStyle.Render(share), 8),
			padLeft(sapphireStyle.Render(formatTokens(model.inputTokens+model.outputTokens)), 10),
		)
		lines = append(lines, line)
	}
	return renderAnalyticsPanel("Model Leaderboard", colorTeal, analyticsPanelWidth(width), strings.Join(lines, "\n"))
}

func renderAnalyticsSelectedModelPanel(model modelCostEntry, expanded bool, data costData, width int) string {
	panelW := analyticsPanelWidth(width)
	innerW := panelW - 4
	lines := []string{
		lipgloss.NewStyle().Foreground(model.color).Bold(true).Render(prettifyModelName(model.name)),
		dimStyle.Render(fmt.Sprintf("%s · %d providers", analyticsModelEfficiencyLabel(model), len(model.providers))),
		surface1Style.Render(strings.Repeat("─", innerW)),
		renderDotLeaderRow("Window spend", formatUSD(model.cost), innerW),
		renderDotLeaderRow("Token volume", formatTokens(model.inputTokens+model.outputTokens), innerW),
		renderDotLeaderRow("Input / Output", fmt.Sprintf("%s / %s", formatTokens(model.inputTokens), formatTokens(model.outputTokens)), innerW),
	}

	limit := 4
	if expanded {
		limit = len(model.providers)
	}
	if len(model.providers) > 0 {
		lines = append(lines, "", dimStyle.Render("Provider splits"))
		for i, split := range model.providers {
			if i >= limit {
				break
			}
			lines = append(lines, renderDotLeaderRow(
				truncStr(split.provider, innerW/2),
				fmt.Sprintf("%s · %s", formatUSD(split.cost), analyticsShareText(split.cost, model.cost)),
				innerW,
			))
		}
		if !expanded && len(model.providers) > limit {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  + %d more providers", len(model.providers)-limit)))
		}
	}
	return renderAnalyticsPanel("Selected Model", colorSapphire, panelW, strings.Join(lines, "\n"))
}

func renderAnalyticsSelectedModelTrend(model modelCostEntry, data costData, width int) string {
	pts := analyticsSelectedModelSeries(data, model.name)
	if !hasNonZeroData(pts) {
		return ""
	}
	return RenderTimeChart(TimeChartSpec{
		Title:             "SELECTED MODEL DAILY TOKENS",
		Mode:              TimeChartBars,
		Series:            []BrailleSeries{{Label: prettifyModelName(model.name), Color: model.color, Points: pts}},
		Height:            8,
		WindowDays:        analyticsWindowDays(data.timeWindow),
		ReferenceTime:     data.referenceTime,
		PreserveEmptySpan: true,
		YFmt:              formatChartValue,
	}, analyticsPanelWidth(width))
}

func renderAnalyticsProviderSpendPanel(data costData, summary analyticsSummary, width int) string {
	providers := append([]providerCostEntry(nil), data.providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].cost > providers[j].cost })
	innerW := analyticsPanelWidth(width) - 4
	lines := []string{
		dimStyle.Render("Spend, share of window, and normalized burn by provider"),
		surface1Style.Render(strings.Repeat("─", innerW)),
	}
	for _, provider := range providers {
		if provider.cost <= 0 {
			continue
		}
		lines = append(lines, renderDotLeaderRow(provider.name, fmt.Sprintf("%s · %s", formatUSD(provider.cost), analyticsShareText(provider.cost, data.totalCost)), innerW))
		lines = append(lines, "  "+dimStyle.Render(fmt.Sprintf("avg/day %s · today %s", formatUSD(analyticsPerActiveDay(provider.cost, summary.activeDays)), formatUSD(provider.todayCost))))
	}
	return renderAnalyticsPanel("Provider Spend", colorRosewater, analyticsPanelWidth(width), strings.Join(lines, "\n"))
}

func renderAnalyticsBudgetPressurePanel(data costData, width int) string {
	panelW := analyticsPanelWidth(width)
	innerW := panelW - 4
	var lines []string

	if len(data.budgets) > 0 {
		lines = append(lines, dimStyle.Render("Budgets"))
		budgets := append([]budgetEntry(nil), data.budgets...)
		sort.Slice(budgets, func(i, j int) bool {
			pi := 0.0
			if budgets[i].limit > 0 {
				pi = budgets[i].used / budgets[i].limit
			}
			pj := 0.0
			if budgets[j].limit > 0 {
				pj = budgets[j].used / budgets[j].limit
			}
			return pi > pj
		})
		for i, budget := range budgets {
			if i >= 4 || budget.limit <= 0 {
				break
			}
			lines = append(lines, renderDotLeaderRow(
				budget.name,
				fmt.Sprintf("%s / %s", formatUSD(budget.used), formatUSD(budget.limit)),
				innerW,
			))
		}
	}

	if len(data.usageGauges) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, dimStyle.Render("Quotas"))
		gauges := append([]usageGaugeEntry(nil), data.usageGauges...)
		sort.Slice(gauges, func(i, j int) bool { return gauges[i].pctUsed > gauges[j].pctUsed })
		for i, gauge := range gauges {
			if i >= 6 {
				break
			}
			lines = append(lines, renderDotLeaderRow(
				truncStr(gauge.provider+" · "+gauge.name, innerW/2),
				fmt.Sprintf("%.0f%% %s", gauge.pctUsed, gauge.window),
				innerW,
			))
		}
	}

	if len(lines) == 0 {
		lines = append(lines, dimStyle.Render("No budget or quota telemetry available."))
	}
	return renderAnalyticsPanel("Budget & Quota Pressure", colorYellow, panelW, strings.Join(lines, "\n"))
}

func renderAnalyticsCostEfficiencyPanel(data costData, width, limit int) string {
	models := filterTokenModels(data.models)
	var withCost []modelCostEntry
	for _, model := range models {
		if model.cost > 0 && model.inputTokens+model.outputTokens > 0 {
			withCost = append(withCost, model)
		}
	}
	if len(withCost) == 0 {
		return ""
	}
	sort.Slice(withCost, func(i, j int) bool {
		effi := withCost[i].cost / (withCost[i].inputTokens + withCost[i].outputTokens)
		effj := withCost[j].cost / (withCost[j].inputTokens + withCost[j].outputTokens)
		return effi < effj
	})
	if len(withCost) > limit {
		withCost = withCost[:limit]
	}
	innerW := width - 6
	lines := []string{
		dimStyle.Render("Cheapest models by $ / 1K tokens in the selected window"),
		surface1Style.Render(strings.Repeat("─", innerW)),
	}
	for _, model := range withCost {
		lines = append(lines, renderDotLeaderRow(prettifyModelName(model.name), analyticsModelEfficiencyLabel(model), innerW))
		lines = append(lines, "  "+dimStyle.Render(fmt.Sprintf("%s · %s", primaryProvider(model), formatUSD(model.cost))))
	}
	return renderAnalyticsPanel("Efficiency", colorGreen, width-2, strings.Join(lines, "\n"))
}

func renderAnalyticsClientPanel(data costData, width, limit int) string {
	rows := make([]analyticsRankRow, 0, min(limit, len(data.clients)))
	for _, client := range data.clients {
		value := client.total
		unit := "tok"
		if value <= 0 {
			value = client.requests
			unit = "req"
		}
		if value <= 0 {
			continue
		}
		detail := analyticsHotspotValueLabel(value, unit)
		if client.sessions > 0 {
			detail += fmt.Sprintf(" · %s sess", shortCompact(client.sessions))
		}
		rows = append(rows, analyticsRankRow{
			name:   client.name,
			value:  shortCompact(value) + " " + unit,
			detail: detail,
			series: analyticsCropSeries(client.series, data.timeWindow, data.referenceTime),
			color:  client.color,
		})
		if len(rows) >= limit {
			break
		}
	}
	return renderAnalyticsRankPanel("Client Hotspots", colorTeal, rows, analyticsPanelWidth(width), "Where requests and token volume originated")
}

func renderAnalyticsProjectPanel(data costData, width, limit int) string {
	rows := make([]analyticsRankRow, 0, min(limit, len(data.projects)))
	for _, project := range data.projects {
		if project.requests <= 0 {
			continue
		}
		rows = append(rows, analyticsRankRow{
			name:   project.name,
			value:  shortCompact(project.requests) + " req",
			detail: analyticsHotspotValueLabel(project.requests, "req"),
			series: analyticsCropSeries(project.series, data.timeWindow, data.referenceTime),
			color:  project.color,
		})
		if len(rows) >= limit {
			break
		}
	}
	return renderAnalyticsRankPanel("Project Hotspots", colorPeach, rows, analyticsPanelWidth(width), "Which projects generated the most usage")
}

func renderAnalyticsMCPPanel(data costData, width, limit int) string {
	rows := make([]analyticsRankRow, 0, min(limit, len(data.mcpServers)))
	for _, server := range data.mcpServers {
		if server.calls <= 0 {
			continue
		}
		rows = append(rows, analyticsRankRow{
			name:   server.name,
			value:  shortCompact(server.calls) + " calls",
			detail: analyticsHotspotValueLabel(server.calls, "calls"),
			series: analyticsCropSeries(server.series, data.timeWindow, data.referenceTime),
			color:  server.color,
		})
		if len(rows) >= limit {
			break
		}
	}
	return renderAnalyticsRankPanel("MCP Hotspots", colorYellow, rows, analyticsPanelWidth(width), "Server activity distribution across the selected window")
}

func renderAnalyticsActivityHeatmap(data costData, width int) string {
	spec, ok := buildProviderModelHeatmapSpec(data, 8, 0)
	if !ok {
		return ""
	}
	spec.Title = "MODEL HOTSPOTS OVER TIME"
	return RenderHeatmap(spec, width)
}

func renderAnalyticsRankPanel(title string, accent lipgloss.Color, rows []analyticsRankRow, width int, subtitle string) string {
	if len(rows) == 0 {
		return ""
	}
	innerW := width - 4
	lines := []string{}
	if subtitle != "" {
		lines = append(lines, dimStyle.Render(subtitle))
		lines = append(lines, surface1Style.Render(strings.Repeat("─", innerW)))
	}
	for _, row := range rows {
		label := lipgloss.NewStyle().Foreground(row.color).Render("●") + " " + truncStr(row.name, max(12, innerW/2))
		lines = append(lines, renderDotLeaderRow(label, row.value, innerW))
		details := strings.TrimSpace(row.detail)
		if spark := analyticsSparkline(row.series, clamp(innerW/3, 8, 16), row.color); spark != "" {
			if details != "" {
				details += "  "
			}
			details += spark
		}
		if details != "" {
			lines = append(lines, "  "+dimStyle.Render(details))
		}
	}
	return renderAnalyticsPanel(title, accent, width, strings.Join(lines, "\n"))
}

func renderAnalyticsPanel(title string, accent lipgloss.Color, width int, body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	if width < 28 {
		width = 28
	}
	innerW := width - 4
	titleText := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(" " + title + " ")
	titleW := lipgloss.Width(titleText)
	rightBorderLen := width - 2 - 1 - titleW
	if rightBorderLen < 1 {
		rightBorderLen = 1
	}

	var sb strings.Builder
	sb.WriteString(
		lipgloss.NewStyle().Foreground(accent).Render("╭"+strings.Repeat("─", 1)) +
			titleText +
			lipgloss.NewStyle().Foreground(accent).Render(strings.Repeat("─", rightBorderLen)+"╮"),
	)
	sb.WriteString("\n")
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		sb.WriteString(lipgloss.NewStyle().Foreground(colorSurface1).Render("│ "))
		sb.WriteString(padRight(truncStr(line, innerW), innerW))
		sb.WriteString(lipgloss.NewStyle().Foreground(colorSurface1).Render(" │"))
		sb.WriteString("\n")
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(accent).Render("╰" + strings.Repeat("─", width-2) + "╯"))
	return sb.String()
}

func analyticsWindowDays(window core.TimeWindow) int {
	if window == core.TimeWindowAll {
		return 0
	}
	return window.Days()
}

func analyticsComparisonWindowDays(window core.TimeWindow) int {
	switch window {
	case core.TimeWindow1d:
		return 1
	case core.TimeWindow3d:
		return 3
	case core.TimeWindow7d:
		return 7
	case core.TimeWindow30d:
		return 30
	default:
		return 14
	}
}

func analyticsComparisonLabel(window core.TimeWindow) string {
	days := analyticsComparisonWindowDays(window)
	if days <= 1 {
		return "today vs prior day"
	}
	return fmt.Sprintf("last %dd vs prior %dd", days, days)
}

func analyticsWindowSubtitle(data costData) string {
	if data.timeWindow == core.TimeWindowAll {
		return "all retained telemetry"
	}
	return data.timeWindow.Label()
}

func analyticsTokenMixSubtitle(data costData) string {
	if data.totalInput <= 0 && data.totalOutput <= 0 {
		return "no token mix"
	}
	return fmt.Sprintf("in %s · out %s", shortCompact(data.totalInput), shortCompact(data.totalOutput))
}

func analyticsShareText(value, total float64) string {
	if value <= 0 || total <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", value/total*100)
}

func analyticsShareLabel(value, total float64) string {
	share := analyticsShareText(value, total)
	if share == "—" {
		return "no share"
	}
	return share + " of window"
}

func analyticsPerActiveDay(total float64, activeDays int) float64 {
	if total <= 0 {
		return 0
	}
	if activeDays <= 0 {
		return total
	}
	return total / float64(activeDays)
}

func analyticsModelEfficiencyLabel(model modelCostEntry) string {
	totalTokens := model.inputTokens + model.outputTokens
	if model.cost <= 0 || totalTokens <= 0 {
		return "no efficiency signal"
	}
	return fmt.Sprintf("$%.3f / 1K tok", model.cost/totalTokens*1000)
}

func analyticsSparkline(points []core.TimePoint, width int, color lipgloss.Color) string {
	if len(points) < 2 {
		return ""
	}
	values := make([]float64, 0, len(points))
	for _, point := range points {
		values = append(values, point.Value)
	}
	return RenderSparkline(values, width, color)
}

func analyticsCropSeries(points []core.TimePoint, window core.TimeWindow, referenceTime time.Time) []core.TimePoint {
	if analyticsWindowDays(window) <= 0 {
		return append([]core.TimePoint(nil), points...)
	}
	return clipAndPadPointsByRecentDays(points, analyticsWindowDays(window), referenceTime)
}

func analyticsSelectedModelSeries(data costData, modelName string) []core.TimePoint {
	byDate := make(map[string]float64)
	for _, group := range data.timeSeries {
		for _, named := range core.ExtractAnalyticsModelSeries(group.series) {
			if named.Name != modelName {
				continue
			}
			for _, point := range analyticsCropSeries(named.Points, data.timeWindow, data.referenceTime) {
				byDate[point.Date] += point.Value
			}
		}
	}
	return core.SortedTimePoints(byDate)
}

func analyticsTopProvider(data costData) (string, float64) {
	for _, provider := range data.providers {
		if provider.cost > 0 {
			return provider.name, provider.cost
		}
	}
	return "—", 0
}

func analyticsTopClient(data costData) (string, float64) {
	for _, client := range data.clients {
		if client.total > 0 {
			return client.name, client.total
		}
		if client.requests > 0 {
			return client.name, client.requests
		}
	}
	return "—", 0
}

func analyticsTopProject(data costData) (string, float64) {
	for _, project := range data.projects {
		if project.requests > 0 {
			return project.name, project.requests
		}
	}
	return "—", 0
}

func analyticsTopMCP(data costData) (string, float64) {
	for _, server := range data.mcpServers {
		if server.calls > 0 {
			return server.name, server.calls
		}
	}
	return "—", 0
}

func analyticsHotspotValueLabel(value float64, unit string) string {
	if value <= 0 {
		return "no data"
	}
	return shortCompact(value) + " " + unit
}

func analyticsPanelWidth(width int) int {
	return max(28, width-2)
}

func filterNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}
