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

func (m Model) renderAnalyticsContent(w, h int) string {
	var statusBuf strings.Builder
	renderStatusBar(&statusBuf, m.analyticsSortBy, m.analyticsFilter.text, w)
	statusStr := statusBuf.String()

	content, hasData := m.cachedAnalyticsPageContent(w)
	if !hasData {
		empty := "\n" + dimStyle.Render("  No cost or usage data available.")
		empty += "\n" + dimStyle.Render("  Analytics requires providers that report spend, tokens, or budgets.")
		return statusStr + empty
	}

	lines := strings.Split(statusStr+content, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func renderStatusBar(sb *strings.Builder, sortBy int, filter string, w int) {
	parts := []string{
		analyticsSortLabelStyle.Render("↕ " + sortByLabels[sortBy]),
	}
	if filter != "" {
		parts = append(parts,
			lipgloss.NewStyle().Foreground(colorSapphire).Render("/ "+filter))
	}
	left := "  " + strings.Join(parts, "  "+dimStyle.Render("|")+"  ")
	hints := dimStyle.Render("s:sort  /:filter  ?:help")
	gap := w - lipgloss.Width(left) - lipgloss.Width(hints) - 2
	if gap < 1 {
		gap = 1
	}
	sb.WriteString(left + strings.Repeat(" ", gap) + hints + "\n")
}

func renderAnalyticsSinglePage(data costData, summary analyticsSummary, w int) string {
	var sb strings.Builder

	if kpis := renderAnalyticsKPIHeader(data, summary, w); kpis != "" {
		sb.WriteString(kpis)
		sb.WriteString("\n")
	}

	if totalCost := renderTotalCostTrend(data, summary, w, 9); totalCost != "" {
		sb.WriteString(totalCost)
		sb.WriteString("\n")
	}

	if stacked := renderProviderCostStackedChart(data, w, 8); stacked != "" {
		sb.WriteString(stacked)
		sb.WriteString("\n")
	}

	if tokenDist := renderDailyTokenDistributionChart(data, w, 12); tokenDist != "" {
		sb.WriteString(tokenDist)
		sb.WriteString("\n")
	}

	if bottom := renderAnalyticsBottomGrid(data, w); bottom != "" {
		sb.WriteString(bottom)
	}

	return strings.TrimRight(sb.String(), "\n")
}

func renderAnalyticsBottomGrid(data costData, w int) string {
	if w < 80 {
		var sb strings.Builder
		if heat := renderProviderModelDailyUsage(data, w, 12); heat != "" {
			sb.WriteString(heat)
			sb.WriteString("\n")
		}
		if table := renderTopModelsSummary(data.models, w, 10); table != "" {
			sb.WriteString(table)
			sb.WriteString("\n")
		}
		if costs := renderCostTable(data, w); costs != "" {
			sb.WriteString(costs)
		}
		return strings.TrimRight(sb.String(), "\n")
	}

	gap := 2
	colW := (w - 4 - gap) / 2
	if colW < 36 {
		colW = 36
	}
	left := strings.TrimRight(renderProviderModelDailyUsage(data, colW, 12), "\n")
	right := strings.TrimRight(renderTopModelsCompact(data.models, colW, 10), "\n")

	row1 := ""
	switch {
	case left != "" && right != "":
		row1 = lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
	case left != "":
		row1 = left
	case right != "":
		row1 = right
	}

	row2 := strings.TrimRight(renderCostTableCompact(data, w, 8), "\n")

	if row1 == "" {
		return row2
	}
	if row2 == "" {
		return row1
	}
	return row1 + "\n\n" + row2
}

func renderCostTable(data costData, w int) string {
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

	hasBudget := len(data.budgets) > 0

	if !hasCost && !hasBudget {
		return ""
	}

	var sb strings.Builder

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorRosewater)
	sb.WriteString("  " + sectionStyle.Render("COST & SPEND") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	provW := 20
	colW := 12
	budgetW := w - provW - colW*3 - 10
	if budgetW < 20 {
		budgetW = 20
	}

	headerStyle := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(headerStyle.Render("Provider"), provW) + " " +
		padLeft(headerStyle.Render("Today"), colW) + " " +
		padLeft(headerStyle.Render("7 Day"), colW) + " " +
		padLeft(headerStyle.Render("All-Time"), colW) + "  " +
		padRight(headerStyle.Render("Budget"), budgetW) + "\n")

	budgetMap := make(map[string]budgetEntry)
	for _, b := range data.budgets {
		base := strings.Split(b.name, " (")[0]
		if existing, ok := budgetMap[base]; !ok || b.limit > existing.limit {
			budgetMap[base] = b
		}
	}

	for _, p := range data.providers {
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
			provName += " " + lipgloss.NewStyle().Foreground(colorRed).Render("!")
		}

		todayStr := dimStyle.Render("—")
		if p.todayCost > 0 {
			todayStr = lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(p.todayCost))
		}

		weekStr := dimStyle.Render("—")
		if p.weekCost > 0 {
			weekStr = lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(p.weekCost))
		}

		allTimeStr := dimStyle.Render("—")
		if p.cost > 0 {
			allTimeStr = lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatUSD(p.cost))
		}

		budgetStr := dimStyle.Render("—")
		if b, ok := budgetMap[p.name]; ok && b.limit > 0 {
			pct := b.used / b.limit * 100
			gauge := RenderInlineGauge(pct, 10)
			budgetStr = gauge + " " +
				dimStyle.Render(fmt.Sprintf("%s/%s %.0f%%", formatUSD(b.used), formatUSD(b.limit), pct))
		}

		sb.WriteString("  " + padRight(provName, provW) + " " +
			padLeft(todayStr, colW) + " " +
			padLeft(weekStr, colW) + " " +
			padLeft(allTimeStr, colW) + "  " +
			padRight(budgetStr, budgetW) + "\n")
	}

	return sb.String()
}

func hasNonZeroData(pts []core.TimePoint) bool {
	for _, p := range pts {
		if p.Value > 0 {
			return true
		}
	}
	return false
}

func renderTopModelsSummary(models []modelCostEntry, w int, limit int) string {
	all := filterTokenModels(models)
	if len(all) == 0 {
		return ""
	}
	sort.Slice(all, func(i, j int) bool {
		li := all[i].inputTokens + all[i].outputTokens
		lj := all[j].inputTokens + all[j].outputTokens
		if li == lj {
			return all[i].cost > all[j].cost
		}
		return li > lj
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorTeal)
	sb.WriteString("  " + sectionStyle.Render("TOP MODELS (Daily volume & efficiency)") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	nameW := clamp(w/3, 20, 34)
	provW := clamp(w/5, 14, 22)
	tokW := 12
	costW := 10
	effW := 10

	head := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(head.Render("Model"), nameW) + " " +
		padRight(head.Render("Provider"), provW) + " " +
		padLeft(head.Render("Tokens"), tokW) + " " +
		padLeft(head.Render("Cost"), costW) + " " +
		padLeft(head.Render("$/1K"), effW) + "\n")

	for _, m := range all {
		tokens := m.inputTokens + m.outputTokens
		if tokens <= 0 {
			continue
		}
		eff := "—"
		if m.cost > 0 {
			eff = fmt.Sprintf("$%.4f", m.cost/tokens*1000)
		}
		sb.WriteString("  " +
			padRight(lipgloss.NewStyle().Foreground(m.color).Render(truncStr(m.name, nameW)), nameW) + " " +
			padRight(dimStyle.Render(truncStr(primaryProvider(m), provW)), provW) + " " +
			padLeft(lipgloss.NewStyle().Foreground(colorSapphire).Render(formatTokens(tokens)), tokW) + " " +
			padLeft(lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(m.cost)), costW) + " " +
			padLeft(lipgloss.NewStyle().Foreground(colorYellow).Render(eff), effW) + "\n")
	}
	return sb.String()
}

func renderTopModelsCompact(models []modelCostEntry, w int, limit int) string {
	all := filterTokenModels(models)
	if len(all) == 0 {
		return ""
	}
	sort.Slice(all, func(i, j int) bool {
		li := all[i].inputTokens + all[i].outputTokens
		lj := all[j].inputTokens + all[j].outputTokens
		if li == lj {
			return all[i].cost > all[j].cost
		}
		return li > lj
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorTeal)
	sb.WriteString("  " + sectionStyle.Render("TOP MODELS (compact)") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	nameW := clamp(w/2, 16, 26)
	provW := clamp(w/4, 10, 16)
	tokW := 9
	effW := 9

	head := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(head.Render("Model"), nameW) + " " +
		padRight(head.Render("Provider"), provW) + " " +
		padLeft(head.Render("Tokens"), tokW) + " " +
		padLeft(head.Render("$/1K"), effW) + "\n")

	for _, m := range all {
		tokens := m.inputTokens + m.outputTokens
		if tokens <= 0 {
			continue
		}
		eff := "—"
		if m.cost > 0 {
			eff = fmt.Sprintf("$%.4f", m.cost/tokens*1000)
		}
		sb.WriteString("  " +
			padRight(lipgloss.NewStyle().Foreground(m.color).Render(truncStr(m.name, nameW)), nameW) + " " +
			padRight(dimStyle.Render(truncStr(primaryProvider(m), provW)), provW) + " " +
			padLeft(lipgloss.NewStyle().Foreground(colorSapphire).Render(formatTokens(tokens)), tokW) + " " +
			padLeft(lipgloss.NewStyle().Foreground(colorYellow).Render(eff), effW) + "\n")
	}
	return sb.String()
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

func renderCostTableCompact(data costData, w int, limit int) string {
	if len(data.providers) == 0 {
		return ""
	}
	providers := make([]providerCostEntry, len(data.providers))
	copy(providers, data.providers)
	sort.Slice(providers, func(i, j int) bool {
		li := providers[i].weekCost
		lj := providers[j].weekCost
		if li == 0 && providers[i].todayCost > 0 {
			li = providers[i].todayCost
		}
		if lj == 0 && providers[j].todayCost > 0 {
			lj = providers[j].todayCost
		}
		if li == lj {
			return providers[i].name < providers[j].name
		}
		return li > lj
	})
	if limit > 0 && len(providers) > limit {
		providers = providers[:limit]
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorRosewater)
	sb.WriteString("  " + sectionStyle.Render("COST & SPEND (compact)") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	provW := clamp(w/3, 14, 24)
	colW := clamp((w-provW-8)/3, 8, 12)
	head := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(head.Render("Provider"), provW) + " " +
		padLeft(head.Render("Today"), colW) + " " +
		padLeft(head.Render("7d"), colW) + " " +
		padLeft(head.Render("All"), colW) + "\n")

	for _, p := range providers {
		provColor := p.color
		if p.status == core.StatusLimited || p.status == core.StatusError || p.status == core.StatusAuth {
			provColor = colorRed
		}
		name := lipgloss.NewStyle().Foreground(provColor).Bold(true).Render(truncStr(p.name, provW))
		todayStr := dimStyle.Render("—")
		if p.todayCost > 0 {
			todayStr = lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(p.todayCost))
		}
		weekStr := dimStyle.Render("—")
		if p.weekCost > 0 {
			weekStr = lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(p.weekCost))
		}
		allStr := dimStyle.Render("—")
		if p.cost > 0 {
			allStr = lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatUSD(p.cost))
		}
		sb.WriteString("  " + padRight(name, provW) + " " +
			padLeft(todayStr, colW) + " " +
			padLeft(weekStr, colW) + " " +
			padLeft(allStr, colW) + "\n")
	}
	return sb.String()
}

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
		chart += "  " + dimStyle.Render(fmt.Sprintf("Observed daily cost: %d provider(s). Estimated from activity shape: %d provider(s).", observedCount, estimatedCount)) + "\n"
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

// coarse bucket to avoid float noise.

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

func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func sortedMetricKeys(m map[string]core.Metric) []string {
	return core.SortedStringKeys(m)
}
