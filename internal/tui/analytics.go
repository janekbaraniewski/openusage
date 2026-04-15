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

// renderAnalyticsContent is the main entry point for the analytics screen.
func (m Model) renderAnalyticsContent(w, h int) string {
	header := m.renderAnalyticsHeader(w)
	headerH := strings.Count(header, "\n") + 1

	contentH := h - headerH
	if contentH < 3 {
		contentH = 3
	}

	content, hasData := m.cachedAnalyticsPageContent(w)
	if !hasData {
		empty := "\n" + dimStyle.Render("  No cost or usage data available.")
		empty += "\n" + dimStyle.Render("  Analytics requires providers that report spend, tokens, or budgets.")
		return header + "\n" + empty
	}

	lines := strings.Split(content, "\n")

	// Apply scroll offset for content.
	if maxScroll := len(lines) - contentH; maxScroll > 0 {
		start := m.analyticsScrollY
		if start < 0 {
			start = 0
		}
		if start > maxScroll {
			start = maxScroll
		}
		lines = lines[start:]
	}

	for len(lines) < contentH {
		lines = append(lines, "")
	}
	if len(lines) > contentH {
		lines = lines[:contentH]
	}
	for i := range lines {
		lines[i] = analyticsPadLine(lines[i], w)
	}

	return analyticsPadLine(header, w) + "\n" + strings.Join(lines, "\n")
}

func (m Model) renderAnalyticsHeader(w int) string {
	label := analyticsSubTabActiveStyle.Render(" Analytics ")
	hints := dimStyle.Render("j/k scroll  s:sort  /:filter  w:window  r:refresh")
	gap := w - lipgloss.Width("  "+label) - lipgloss.Width(hints) - 2
	if gap < 1 {
		gap = 1
	}
	return "  " + label + strings.Repeat(" ", gap) + hints
}

func (m Model) renderAnalyticsPageContent(data costData, summary analyticsSummary, w int) string {
	return renderAnalyticsUnifiedRedesign(data, summary, w)
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
		Title:             "TOTAL COST OVER TIME",
		Mode:              TimeChartBars,
		Series:            series,
		Height:            h,
		WindowDays:        analyticsWindowDays(data.timeWindow),
		ReferenceTime:     data.referenceTime,
		PreserveEmptySpan: true,
		YFmt:              formatCostAxis,
	}, w)
}

func renderDailyTokenDistributionChart(data costData, w int, limit int) string {
	series := buildProviderModelTokenDistributionSeries(data, limit)
	if len(series) == 0 {
		return ""
	}
	total := aggregateSeriesByDate(series)
	if !hasNonZeroData(total) {
		return ""
	}
	return RenderTimeChart(TimeChartSpec{
		Title:             "DAILY TOKEN VOLUME",
		Mode:              TimeChartBars,
		Series:            []BrailleSeries{{Label: "daily tokens", Color: colorSapphire, Points: total}},
		Height:            9,
		WindowDays:        analyticsWindowDays(data.timeWindow),
		ReferenceTime:     data.referenceTime,
		PreserveEmptySpan: true,
		YFmt:              formatChartValue,
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
		pts = analyticsCropSeries(pts, data.timeWindow, data.referenceTime)
		if len(pts) == 0 {
			continue
		}
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
				Points: analyticsCropSeries(pts, data.timeWindow, data.referenceTime),
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
			pts := analyticsCropSeries(named.Points, data.timeWindow, data.referenceTime)
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
		label   string
		summary string
		color   lipgloss.Color
		vals    map[string]float64
		total   float64
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
				label:   truncStr(g.providerName+" · "+model, 34),
				summary: shortCompact(total) + " tok",
				color:   stableModelColor(named.Name, g.providerID),
				vals:    vals,
				total:   total,
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
	if windowDays := analyticsWindowDays(data.timeWindow); windowDays > 0 {
		dates = clipDatesToRecent(dates, windowDays)
	} else if lastDays > 0 {
		dates = clipDatesToRecent(dates, lastDays)
	}

	labels := make([]string, len(rows))
	summaries := make([]string, len(rows))
	rowColors := make([]lipgloss.Color, len(rows))
	values := make([][]float64, len(rows))
	for i, r := range rows {
		labels[i] = r.label
		summaries[i] = r.summary
		rowColors[i] = r.color
		line := make([]float64, len(dates))
		for j, d := range dates {
			line[j] = r.vals[d]
		}
		values[i] = line
	}
	return HeatmapSpec{
		Title:      "DAILY USAGE HEATMAP (Provider · Model)",
		Rows:       labels,
		RowSummary: summaries,
		Cols:       dates,
		Values:     values,
		RowColors:  rowColors,
		MaxCols:    0,
		RowScale:   true,
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

	compareDays := analyticsComparisonWindowDays(data.timeWindow)
	s.recentCostAvg, s.previousCostAvg = splitWindowAverages(s.dailyCost, compareDays)
	s.recentTokensAvg, s.previousTokensAvg = splitWindowAverages(s.dailyTokens, compareDays)
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
