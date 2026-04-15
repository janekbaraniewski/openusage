package tui

import (
	"fmt"
	"strings"

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

func renderAnalyticsUnifiedRedesign(data costData, summary analyticsSummary, w int) string {
	sections := []string{
		renderAnalyticsContextLine(data, summary),
		renderAnalyticsMetricStrip([]analyticsMetric{
			{
				label:  "Window Spend",
				value:  formatUSD(data.totalCost),
				detail: analyticsWindowSubtitle(data),
				color:  colorTeal,
			},
			{
				label:  "Token Volume",
				value:  formatTokens(data.totalInput + data.totalOutput),
				detail: analyticsTokenMixSubtitle(data),
				color:  colorSapphire,
			},
			{
				label:  "Spend / Active Day",
				value:  formatUSD(analyticsPerActiveDay(data.totalCost, summary.activeDays)),
				detail: fmt.Sprintf("%d active days", summary.activeDays),
				color:  colorYellow,
			},
			{
				label:  "Spend Trend",
				value:  renderTrendPercent(summary.recentCostAvg, summary.previousCostAvg),
				detail: analyticsComparisonLabel(data.timeWindow),
				color:  colorPeach,
			},
		}, w),
	}

	if trend := renderTotalCostTrend(data, summary, w, 10); trend != "" {
		sections = append(sections, trend)
	}

	switch {
	case w >= 132:
		colW := analyticsColumnWidth(w, 3, 2)
		sections = append(sections, analyticsJoinColumns(
			renderAnalyticsProviderLeaderboardPanel(data, colW, 6),
			renderAnalyticsModelLeaderboardPanel(data, colW, 6),
			renderAnalyticsInsightPanel(data, summary, colW),
		))
	case w >= 92:
		colW := analyticsColumnWidth(w, 2, 2)
		sections = append(sections, analyticsJoinColumns(
			renderAnalyticsProviderLeaderboardPanel(data, colW, 6),
			renderAnalyticsModelLeaderboardPanel(data, colW, 6),
		))
		sections = append(sections, renderAnalyticsInsightPanel(data, summary, w))
	default:
		sections = append(sections,
			renderAnalyticsProviderLeaderboardPanel(data, w, 6),
			renderAnalyticsModelLeaderboardPanel(data, w, 6),
			renderAnalyticsInsightPanel(data, summary, w),
		)
	}

	if w >= 96 {
		colW := analyticsColumnWidth(w, 2, 2)
		sections = append(sections, analyticsJoinColumns(
			renderAnalyticsProviderSpendPanel(data, summary, colW),
			renderAnalyticsBudgetPressurePanel(data, colW),
		))
	} else {
		sections = append(sections,
			renderAnalyticsProviderSpendPanel(data, summary, w),
			renderAnalyticsBudgetPressurePanel(data, w),
		)
	}

	if eff := renderAnalyticsCostEfficiencyPanel(data, w, 10); eff != "" {
		sections = append(sections, eff)
	}

	if tokenDist := renderDailyTokenDistributionChart(data, w, 10); tokenDist != "" {
		sections = append(sections, tokenDist)
	}

	switch {
	case w >= 132:
		colW := analyticsColumnWidth(w, 3, 2)
		sections = append(sections, analyticsJoinColumns(
			renderAnalyticsClientPanel(data, colW, 6),
			renderAnalyticsProjectPanel(data, colW, 6),
			renderAnalyticsMCPPanel(data, colW, 6),
		))
	case w >= 92:
		colW := analyticsColumnWidth(w, 2, 2)
		sections = append(sections, analyticsJoinColumns(
			renderAnalyticsClientPanel(data, colW, 6),
			renderAnalyticsProjectPanel(data, colW, 6),
		))
		if mcp := renderAnalyticsMCPPanel(data, w, 6); mcp != "" {
			sections = append(sections, mcp)
		}
	default:
		sections = append(sections,
			renderAnalyticsClientPanel(data, w, 6),
			renderAnalyticsProjectPanel(data, w, 6),
			renderAnalyticsMCPPanel(data, w, 6),
		)
	}

	if heat := renderAnalyticsActivityHeatmap(data, w); heat != "" {
		sections = append(sections, heat)
	}

	if summary.peakTokens > 0 {
		sections = append(sections, renderAnalyticsPanel(
			"Peak Activity",
			colorLavender,
			w,
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
