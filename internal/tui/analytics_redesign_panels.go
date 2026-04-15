package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderAnalyticsProviderLeaderboardPanel(data costData, width, limit int) string {
	providers := append([]providerCostEntry(nil), data.providers...)
	sort.Slice(providers, func(i, j int) bool {
		left := providerAnalyticsRankValue(providers[i])
		right := providerAnalyticsRankValue(providers[j])
		if left == right {
			return providers[i].name < providers[j].name
		}
		return left > right
	})
	rows := make([]analyticsRankRow, 0, min(limit, len(providers)))
	for _, provider := range providers {
		valueText, detail := analyticsProviderRankLabel(provider, data.totalCost)
		if valueText == "" {
			continue
		}
		rows = append(rows, analyticsRankRow{
			name:   provider.name,
			value:  valueText,
			detail: detail,
			color:  provider.color,
		})
		if len(rows) >= limit {
			break
		}
	}
	return renderAnalyticsRankPanel("Provider Leaders", colorRosewater, rows, width, "Spend when present, otherwise token volume")
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
	return renderAnalyticsRankPanel("Model Leaders", colorTeal, rows, width, "The models driving most spend in the selected window")
}

func renderAnalyticsInsightPanel(data costData, summary analyticsSummary, width int) string {
	topProvider, topProviderCost := analyticsTopProvider(data)
	topClient, clientValue := analyticsTopClient(data)
	topProject, projectValue := analyticsTopProject(data)
	topMCP, mcpValue := analyticsTopMCP(data)

	lines := []string{
		renderDotLeaderRow("Window", data.timeWindow.Label(), width-4),
		renderDotLeaderRow("Spend trend", renderTrendPercent(summary.recentCostAvg, summary.previousCostAvg), width-4),
	}
	if summary.peakCost > 0 {
		lines = append(lines, renderDotLeaderRow("Peak spend day", fmt.Sprintf("%s · %s", summary.peakCostDate, formatUSD(summary.peakCost)), width-4))
	}
	if topProvider != "" {
		lines = append(lines, renderDotLeaderRow("Top provider", fmt.Sprintf("%s · %s", topProvider, analyticsShareLabel(topProviderCost, data.totalCost)), width-4))
	}
	if topClient != "" {
		lines = append(lines, renderDotLeaderRow("Top client", fmt.Sprintf("%s · %s", topClient, analyticsHotspotValueLabel(clientValue, "tok")), width-4))
	}
	if topProject != "" {
		lines = append(lines, renderDotLeaderRow("Top project", fmt.Sprintf("%s · %s", topProject, analyticsHotspotValueLabel(projectValue, "req")), width-4))
	}
	if topMCP != "" {
		lines = append(lines, renderDotLeaderRow("Top MCP", fmt.Sprintf("%s · %s", topMCP, analyticsHotspotValueLabel(mcpValue, "calls")), width-4))
	}

	return renderAnalyticsPanel("What Changed", colorLavender, width, strings.Join(lines, "\n"))
}

func renderAnalyticsProviderSpendPanel(data costData, summary analyticsSummary, width int) string {
	providers := append([]providerCostEntry(nil), data.providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].cost > providers[j].cost })
	innerW := width - 4
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
	return renderAnalyticsPanel("Provider Spend", colorRosewater, width, strings.Join(lines, "\n"))
}

func renderAnalyticsBudgetPressurePanel(data costData, width int) string {
	panelW := width
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
	return renderAnalyticsPanel("Efficiency", colorGreen, width, strings.Join(lines, "\n"))
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
	return renderAnalyticsRankPanel("Client Hotspots", colorTeal, rows, width, "Where requests and token volume originated")
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
	return renderAnalyticsRankPanel("Project Hotspots", colorPeach, rows, width, "Which projects generated the most usage")
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
	return renderAnalyticsRankPanel("MCP Hotspots", colorYellow, rows, width, "Server activity distribution across the selected window")
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
	titleText := " " + truncateToWidth(title, max(4, width-6)) + " "
	titleRendered := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(titleText)
	titleW := lipgloss.Width(titleRendered)
	rightBorderLen := width - 2 - 1 - titleW
	if rightBorderLen < 1 {
		rightBorderLen = 1
	}

	var sb strings.Builder
	sb.WriteString(
		lipgloss.NewStyle().Foreground(accent).Render("╭"+strings.Repeat("─", 1)) +
			titleRendered +
			lipgloss.NewStyle().Foreground(accent).Render(strings.Repeat("─", rightBorderLen)+"╮"),
	)
	sb.WriteString("\n")
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		sb.WriteString(lipgloss.NewStyle().Foreground(colorSurface1).Render("│ "))
		sb.WriteString(analyticsPadLine(line, innerW))
		sb.WriteString(lipgloss.NewStyle().Foreground(colorSurface1).Render(" │"))
		sb.WriteString("\n")
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(accent).Render("╰" + strings.Repeat("─", width-2) + "╯"))
	return sb.String()
}
