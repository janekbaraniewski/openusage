package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// cropSeriesToWindow normalizes chart series to the selected detail window.
func cropSeriesToWindow(pts []core.TimePoint, window core.TimeWindow) []core.TimePoint {
	days := window.Days()
	if days <= 0 || len(pts) == 0 {
		return pts
	}
	return clipAndPadPointsByRecentDays(pts, days, time.Now().UTC())
}

// buildDetailTrendsSection builds the daily trends + charts section.
// Unlike the tile view which shows one chart + sparklines, the detail view
// renders a full Braille chart for EACH available data series.
func buildDetailTrendsSection(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, timeWindow core.TimeWindow) []string {
	var lines []string

	// Daily usage sparkline summary (compact overview).
	dailyLines := buildProviderDailyTrendLines(snap, innerW)
	lines = append(lines, dailyLines...)

	// Render a separate chart for each available series.
	seriesCandidates := []struct {
		keys  []string
		label string
		yFmt  func(float64) string
		color lipgloss.Color
	}{
		{keys: []string{"analytics_cost", "cost"}, label: "Cost", yFmt: formatCostAxis, color: colorTeal},
		{keys: []string{"analytics_requests", "requests"}, label: "Requests", yFmt: formatChartValue, color: colorYellow},
		{keys: []string{"analytics_tokens", "tokens_total"}, label: "Tokens", yFmt: formatChartValue, color: colorSapphire},
		{keys: []string{"messages"}, label: "Messages", yFmt: formatChartValue, color: colorGreen},
		{keys: []string{"sessions"}, label: "Sessions", yFmt: formatChartValue, color: colorPeach},
	}

	chartW := innerW - 4
	if chartW < 30 {
		chartW = 30
	}
	chartH := 10 // consistent height for all charts
	if innerW < 80 {
		chartH = 8
	}

	for _, candidate := range seriesCandidates {
		var pts []core.TimePoint
		var matchedKey string
		for _, key := range candidate.keys {
			if p, ok := snap.DailySeries[key]; ok && len(p) >= 2 {
				pts = p
				matchedKey = key
				break
			}
		}
		if len(pts) < 2 {
			continue
		}

		// Apply zoom.
		pts = cropSeriesToWindow(pts, timeWindow)
		if len(pts) < 2 {
			continue
		}

		series := []BrailleSeries{{
			Label:  metricLabel(widget, matchedKey),
			Color:  candidate.color,
			Points: pts,
		}}

		chart := RenderBrailleChart(candidate.label, series, chartW, chartH, candidate.yFmt)
		if chart != "" {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, strings.Split(strings.TrimRight(chart, "\n"), "\n")...)
		}
	}

	for _, breakdown := range buildDetailBreakdownTrendCharts(snap, widget) {
		// Apply zoom to breakdown series.
		for i := range breakdown.series {
			breakdown.series[i].Points = cropSeriesToWindow(breakdown.series[i].Points, timeWindow)
		}
		chart := RenderBrailleChart(breakdown.title, breakdown.series, chartW, chartH, breakdown.yFmt)
		if chart == "" {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, strings.Split(strings.TrimRight(chart, "\n"), "\n")...)
		if breakdown.hiddenCount > 0 {
			lines = append(lines, "  "+dimStyle.Render(fmt.Sprintf("+ %d more %s with daily series", breakdown.hiddenCount, breakdown.hiddenLabel)))
		}
	}

	return lines
}

type detailTrendBreakdownChart struct {
	title       string
	series      []BrailleSeries
	yFmt        func(float64) string
	hiddenCount int
	hiddenLabel string
}

// buildDetailActivityHeatmap builds a compact GitHub-contribution-graph style heatmap.
// Each cell is a single "▪" character. Rows = Mon-Sun, columns = weeks.
func buildDetailActivityHeatmap(snap core.UsageSnapshot, innerW int) []string {
	candidates := []string{"analytics_requests", "requests", "analytics_cost", "cost"}
	var pts []core.TimePoint
	for _, key := range candidates {
		if p, ok := snap.DailySeries[key]; ok && len(p) >= 7 {
			pts = p
			break
		}
	}
	if len(pts) < 7 {
		return nil
	}

	// Build date→value map.
	byDate := make(map[string]float64, len(pts))
	var minDate, maxDate time.Time
	first := true
	for _, p := range pts {
		t, err := time.Parse("2006-01-02", p.Date)
		if err != nil {
			continue
		}
		val := p.Value
		if val < 0 {
			val = 0
		}
		byDate[p.Date] = val
		if first || t.Before(minDate) {
			minDate = t
		}
		if first || t.After(maxDate) {
			maxDate = t
		}
		first = false
	}
	if first {
		return nil
	}

	// Align to week boundaries.
	for minDate.Weekday() != time.Monday {
		minDate = minDate.AddDate(0, 0, -1)
	}
	for maxDate.Weekday() != time.Sunday {
		maxDate = maxDate.AddDate(0, 0, 1)
	}

	totalDays := int(maxDate.Sub(minDate).Hours()/24) + 1
	numWeeks := totalDays / 7
	if numWeeks < 2 {
		return nil
	}

	// Each column = 2 chars (block + space). Row labels = 4 chars + space.
	labelW := 5 // "Mon " + space
	maxWeeks := (innerW - labelW - 2) / 2
	if maxWeeks < 4 {
		maxWeeks = 4
	}
	if numWeeks > maxWeeks {
		minDate = maxDate.AddDate(0, 0, -(maxWeeks*7 - 1))
		for minDate.Weekday() != time.Monday {
			minDate = minDate.AddDate(0, 0, -1)
		}
		numWeeks = maxWeeks
	}

	// Find global max for color scaling.
	globalMax := 0.0
	grid := make([][]float64, 7) // [dow][week]
	for dow := 0; dow < 7; dow++ {
		grid[dow] = make([]float64, numWeeks)
		for w := 0; w < numWeeks; w++ {
			date := minDate.AddDate(0, 0, w*7+dow)
			val := byDate[date.Format("2006-01-02")]
			grid[dow][w] = val
			if val > globalMax {
				globalMax = val
			}
		}
	}
	if globalMax <= 0 {
		return nil
	}

	// Color palette: 5 levels from empty to intense (GitHub-style).
	palette := []lipgloss.Color{colorSurface0, colorGreen, colorTeal, colorYellow, colorPeach}

	dayLabels := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

	// Build the heatmap grid as a string block.
	var gridSB strings.Builder
	for dow := 0; dow < 7; dow++ {
		labelColor := colorDim
		if dow < 5 {
			labelColor = colorSubtext
		}
		gridSB.WriteString(lipgloss.NewStyle().Foreground(labelColor).Width(labelW).Render(dayLabels[dow]))

		for w := 0; w < numWeeks; w++ {
			val := grid[dow][w]
			ci := 0
			if val > 0 {
				ci = 1 + int(val/globalMax*3.99)
				if ci >= len(palette) {
					ci = len(palette) - 1
				}
			}
			gridSB.WriteString(lipgloss.NewStyle().Foreground(palette[ci]).Render("■ "))
		}
		gridSB.WriteString("\n")
	}

	// Date labels.
	gridW := numWeeks * 2
	dateLine := make([]byte, gridW)
	for i := range dateLine {
		dateLine[i] = ' '
	}
	numLabels := 4
	if numWeeks < 8 {
		numLabels = 2
	}
	for i := 0; i < numLabels; i++ {
		wi := 0
		if numLabels > 1 {
			wi = i * (numWeeks - 1) / (numLabels - 1)
		}
		weekStart := minDate.AddDate(0, 0, wi*7)
		label := weekStart.Format("Jan 2")
		x := wi * 2
		if x+len(label) > gridW {
			x = gridW - len(label)
		}
		if x < 0 {
			x = 0
		}
		for j := 0; j < len(label) && x+j < gridW; j++ {
			dateLine[x+j] = label[j]
		}
	}
	gridSB.WriteString(strings.Repeat(" ", labelW) + dimStyle.Render(string(dateLine)))
	heatmapBlock := gridSB.String()

	// Build a summary stats panel for the right side.
	var statsSB strings.Builder
	totalVal := 0.0
	activeDays := 0
	peakVal := 0.0
	peakDate := ""
	for _, p := range pts {
		v := p.Value
		if v < 0 {
			v = 0
		}
		totalVal += v
		if v > 0 {
			activeDays++
		}
		if v > peakVal {
			peakVal = v
			peakDate = p.Date
		}
	}
	avgPerDay := 0.0
	if activeDays > 0 {
		avgPerDay = totalVal / float64(activeDays)
	}

	statsSB.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorSubtext).Render("Summary") + "\n\n")
	statsSB.WriteString(renderDotLeaderRow("Active days", fmt.Sprintf("%d", activeDays), 28) + "\n")
	statsSB.WriteString(renderDotLeaderRow("Total days", fmt.Sprintf("%d", numWeeks*7), 28) + "\n")
	if activeDays > 0 {
		pct := float64(activeDays) / float64(numWeeks*7) * 100
		statsSB.WriteString(renderDotLeaderRow("Activity rate", fmt.Sprintf("%.0f%%", pct), 28) + "\n")
	}
	statsSB.WriteString(renderDotLeaderRow("Avg/active day", shortCompact(avgPerDay), 28) + "\n")
	statsSB.WriteString(renderDotLeaderRow("Total", shortCompact(totalVal), 28) + "\n")
	if peakDate != "" {
		if t, err := time.Parse("2006-01-02", peakDate); err == nil {
			statsSB.WriteString(renderDotLeaderRow("Peak", t.Format("Jan 2"), 28) + "\n")
		}
	}
	statsBlock := statsSB.String()

	// Join heatmap and stats side by side.
	combined := lipgloss.JoinHorizontal(lipgloss.Top, heatmapBlock, "    ", statsBlock)
	return strings.Split(strings.TrimRight(combined, "\n"), "\n")
}

// buildDetailDualAxisChart builds an overlay chart showing cost and requests
// together on a single chart. Uses left Y-axis for cost and colors to distinguish.
func buildDetailDualAxisChart(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, timeWindow core.TimeWindow) []string {
	var costPts, reqPts []core.TimePoint
	for _, key := range []string{"analytics_cost", "cost"} {
		if p, ok := snap.DailySeries[key]; ok && len(p) >= 2 {
			costPts = p
			break
		}
	}
	for _, key := range []string{"analytics_requests", "requests"} {
		if p, ok := snap.DailySeries[key]; ok && len(p) >= 2 {
			reqPts = p
			break
		}
	}
	// Only show if we have BOTH series.
	if len(costPts) < 2 || len(reqPts) < 2 {
		return nil
	}

	costPts = cropSeriesToWindow(costPts, timeWindow)
	reqPts = cropSeriesToWindow(reqPts, timeWindow)
	if len(costPts) < 2 || len(reqPts) < 2 {
		return nil
	}

	chartW := innerW - 4
	if chartW < 30 {
		chartW = 30
	}
	chartH := 10
	if innerW < 80 {
		chartH = 8
	}

	series := []BrailleSeries{
		{Label: "Cost ($)", Color: colorTeal, Points: costPts},
		{Label: "Requests", Color: colorYellow, Points: reqPts},
	}

	chart := RenderBrailleChart("Cost & Requests", series, chartW, chartH, nil)
	if chart == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(chart, "\n"), "\n")
}

func buildDetailBreakdownTrendCharts(snap core.UsageSnapshot, widget core.DashboardWidget) []detailTrendBreakdownChart {
	const maxSeries = 4

	var charts []detailTrendBreakdownChart

	if chart, ok := buildModelBreakdownTrendChart(snap, maxSeries); ok {
		charts = append(charts, chart)
	}
	if widget.ShowClientComposition {
		if chart, ok := buildClientBreakdownTrendChart(snap, widget, maxSeries); ok {
			charts = append(charts, chart)
		}
	}
	if chart, ok := buildProjectBreakdownTrendChart(snap, maxSeries); ok {
		charts = append(charts, chart)
	}
	if chart, ok := buildMCPBreakdownTrendChart(snap, maxSeries); ok {
		charts = append(charts, chart)
	}

	return charts
}

func buildModelBreakdownTrendChart(snap core.UsageSnapshot, maxSeries int) (detailTrendBreakdownChart, bool) {
	models, _ := collectProviderModelMix(snap)
	if len(models) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	colors := buildModelColorMap(models, snap.AccountID)
	series, hidden := collectDetailTrendSeries(maxSeries, len(models), func(idx int) (BrailleSeries, bool) {
		model := models[idx]
		if len(model.series) < 2 {
			return BrailleSeries{}, false
		}
		return BrailleSeries{
			Label:  prettifyModelName(model.name),
			Color:  colorForModel(colors, model.name),
			Points: model.series,
		}, true
	})
	if len(series) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	return detailTrendBreakdownChart{
		title:       "Model Breakdown",
		series:      series,
		yFmt:        formatChartValue,
		hiddenCount: hidden,
		hiddenLabel: "models",
	}, true
}

func buildClientBreakdownTrendChart(snap core.UsageSnapshot, widget core.DashboardWidget, maxSeries int) (detailTrendBreakdownChart, bool) {
	clients, _ := collectProviderClientMix(snap)
	if widget.ClientCompositionIncludeInterfaces {
		if interfaceClients, _ := collectInterfaceAsClients(snap); len(interfaceClients) > 0 {
			clients = interfaceClients
		}
	}
	if len(clients) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	colors := buildClientColorMap(clients, snap.AccountID)
	series, hidden := collectDetailTrendSeries(maxSeries, len(clients), func(idx int) (BrailleSeries, bool) {
		client := clients[idx]
		if len(client.series) < 2 {
			return BrailleSeries{}, false
		}
		return BrailleSeries{
			Label:  prettifyClientName(client.name),
			Color:  colorForClient(colors, client.name),
			Points: client.series,
		}, true
	})
	if len(series) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	return detailTrendBreakdownChart{
		title:       "Client Breakdown",
		series:      series,
		yFmt:        formatChartValue,
		hiddenCount: hidden,
		hiddenLabel: "clients",
	}, true
}

func buildProjectBreakdownTrendChart(snap core.UsageSnapshot, maxSeries int) (detailTrendBreakdownChart, bool) {
	projects, _ := collectProviderProjectMix(snap)
	if len(projects) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	colors := buildProjectColorMap(projects, snap.AccountID)
	series, hidden := collectDetailTrendSeries(maxSeries, len(projects), func(idx int) (BrailleSeries, bool) {
		project := projects[idx]
		if len(project.series) < 2 {
			return BrailleSeries{}, false
		}
		return BrailleSeries{
			Label:  project.name,
			Color:  colorForProject(colors, project.name),
			Points: project.series,
		}, true
	})
	if len(series) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	return detailTrendBreakdownChart{
		title:       "Project Breakdown",
		series:      series,
		yFmt:        formatChartValue,
		hiddenCount: hidden,
		hiddenLabel: "projects",
	}, true
}

func buildMCPBreakdownTrendChart(snap core.UsageSnapshot, maxSeries int) (detailTrendBreakdownChart, bool) {
	servers, _ := core.ExtractMCPBreakdown(snap)
	if len(servers) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	colorEntries := make([]toolMixEntry, 0, len(servers))
	for _, server := range servers {
		colorEntries = append(colorEntries, toolMixEntry{name: server.RawName, count: server.Calls})
	}
	colors := buildToolColorMap(colorEntries, snap.AccountID)
	series, hidden := collectDetailTrendSeries(maxSeries, len(servers), func(idx int) (BrailleSeries, bool) {
		server := servers[idx]
		if len(server.Series) < 2 {
			return BrailleSeries{}, false
		}
		return BrailleSeries{
			Label:  prettifyMCPServerName(server.RawName),
			Color:  colorForTool(colors, server.RawName),
			Points: server.Series,
		}, true
	})
	if len(series) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	return detailTrendBreakdownChart{
		title:       "MCP Usage",
		series:      series,
		yFmt:        formatChartValue,
		hiddenCount: hidden,
		hiddenLabel: "servers",
	}, true
}

func collectDetailTrendSeries(maxSeries, total int, build func(int) (BrailleSeries, bool)) ([]BrailleSeries, int) {
	if maxSeries <= 0 {
		maxSeries = 1
	}

	series := make([]BrailleSeries, 0, min(maxSeries, total))
	matched := 0
	for i := 0; i < total; i++ {
		entry, ok := build(i)
		if !ok {
			continue
		}
		matched++
		if len(series) >= maxSeries {
			continue
		}
		series = append(series, entry)
	}
	return series, max(0, matched-len(series))
}
