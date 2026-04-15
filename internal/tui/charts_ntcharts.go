package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	ntbarchart "github.com/NimbleMarkets/ntcharts/barchart"
	"github.com/NimbleMarkets/ntcharts/canvas/runes"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	ntsparkline "github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type ntBarSegment struct {
	Value float64
	Color lipgloss.Color
}

func renderNTSparkline(values []float64, w int, color lipgloss.Color) string {
	if len(values) == 0 || w < 1 {
		return ""
	}

	sampled := sampleSparklineValues(values, w)
	sp := ntsparkline.New(max(1, w), 1)
	sp.Style = lipgloss.NewStyle().Foreground(color)
	sp.PushAll(sampled)
	sp.DrawBraille()
	return strings.ReplaceAll(strings.TrimRight(sp.View(), "\n"), "\n", "")
}

func renderNTHBarChart(items []chartItem, maxBarW, labelW int) string {
	if len(items) == 0 {
		return dimStyle.Render("  No data available")
	}
	if maxBarW < 4 {
		maxBarW = 4
	}

	maxVal := 0.0
	data := make([]ntbarchart.BarData, 0, len(items))
	for _, item := range items {
		if item.Value > maxVal {
			maxVal = item.Value
		}
		data = append(data, ntbarchart.BarData{
			Label: item.Label,
			Values: []ntbarchart.BarValue{{
				Name:  item.Label,
				Value: item.Value,
				Style: lipgloss.NewStyle().Foreground(item.Color),
			}},
		})
	}
	if maxVal <= 0 {
		maxVal = 1
	}

	chart := ntbarchart.New(
		maxBarW,
		len(items),
		ntbarchart.WithHorizontalBars(),
		ntbarchart.WithNoAxis(),
		ntbarchart.WithNoAutoBarWidth(),
		ntbarchart.WithBarWidth(1),
		ntbarchart.WithBarGap(0),
		ntbarchart.WithMaxValue(maxVal),
	)
	chart.PushAll(data)
	chart.Draw()

	barLines := strings.Split(strings.TrimRight(chart.View(), "\n"), "\n")
	for len(barLines) < len(items) {
		barLines = append(barLines, "")
	}

	var lines []string
	for i, item := range items {
		label := item.Label
		if len(label) > labelW {
			label = label[:labelW-1] + "…"
		}

		labelRendered := labelStyle.Width(labelW).Render(label)
		valueStr := lipgloss.NewStyle().Foreground(item.Color).Bold(true).Render(formatUSD(item.Value))
		if item.ValueText != "" {
			valueStr = item.ValueText
		}
		line := fmt.Sprintf("  %s %s  %s", labelRendered, barLines[i], valueStr)
		if item.SubLabel != "" {
			line += "  " + dimStyle.Render(item.SubLabel)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func renderNTStackedBar(segments []ntBarSegment, total float64, width int) string {
	if len(segments) == 0 || total <= 0 || width <= 0 {
		return ""
	}

	filtered := make([]ntBarSegment, 0, len(segments))
	for _, segment := range segments {
		if segment.Value <= 0 {
			continue
		}
		filtered = append(filtered, segment)
	}
	if len(filtered) == 0 {
		return ""
	}

	segmentWidths := distributeNTBarWidths(filtered, total, width)
	var sb strings.Builder
	for i, segment := range filtered {
		cells := segmentWidths[i]
		if cells <= 0 {
			continue
		}
		sb.WriteString(lipgloss.NewStyle().
			Background(segment.Color).
			Render(strings.Repeat(" ", cells)))
	}
	return sb.String()
}

func renderNTBrailleChart(title string, series []BrailleSeries, w, h int, yFmt func(float64) string) string {
	filtered := filterChartSeries(series)
	if len(filtered) == 0 {
		return ""
	}

	// Sanitize: clamp negatives, dedup, and fill date gaps with zeros.
	// Do not trim leading/trailing zeros here; callers may have intentionally
	// padded the series to match the selected time window.
	for i := range filtered {
		filtered[i].Points = fillSeriesDateGaps(
			dedupeSeriesPoints(
				sanitizeSeriesPoints(filtered[i].Points)))
	}
	filtered = filterChartSeries(filtered) // re-filter after sanitization

	chartW := max(20, w-4)
	chartH := max(4, h)
	minTime, maxTime, minY, maxY, ok := chartSeriesBounds(filtered)
	if !ok {
		return ""
	}

	// Count total data points to choose rendering mode.
	totalPoints := 0
	for _, s := range filtered {
		totalPoints += len(s.Points)
	}

	opts := []timeserieslinechart.Option{
		timeserieslinechart.WithTimeRange(minTime, maxTime),
		timeserieslinechart.WithYRange(minY, maxY),
		timeserieslinechart.WithXYSteps(timeChartXStep(chartW), timeChartYStep(chartH)),
		timeserieslinechart.WithXLabelFormatter(func(_ int, v float64) string {
			return formatDateLabel(time.Unix(int64(v), 0).UTC().Format("2006-01-02"))
		}),
		timeserieslinechart.WithYLabelFormatter(func(_ int, v float64) string {
			if yFmt == nil {
				return formatChartValue(v)
			}
			return yFmt(v)
		}),
		timeserieslinechart.WithAxesStyles(surface1Style, dimStyle),
	}

	// Use smooth arc lines for dense data, braille dots for sparse.
	useBraille := totalPoints <= 14
	if !useBraille {
		opts = append(opts, timeserieslinechart.WithLineStyle(runes.ArcLineStyle))
	}

	ts := timeserieslinechart.New(chartW, chartH, opts...)

	for _, s := range filtered {
		style := lipgloss.NewStyle().Foreground(s.Color)
		ts.SetDataSetStyle(s.Label, style)
		for _, p := range s.Points {
			t, err := time.Parse("2006-01-02", p.Date)
			if err != nil {
				continue
			}
			ts.PushDataSet(s.Label, timeserieslinechart.TimePoint{Time: t.UTC(), Value: p.Value})
		}
	}

	if useBraille {
		ts.DrawBrailleAll()
	} else {
		ts.DrawAll()
	}

	return renderNTChartBlock(title, ts.View(), chartW, renderWrappedLegend(filtered, w))
}

func renderNTTimeChart(spec TimeChartSpec, w int) string {
	if len(spec.Series) == 0 {
		return ""
	}

	h := spec.Height
	if h <= 0 {
		h = 10
	}
	yFmt := spec.YFmt
	if yFmt == nil {
		yFmt = formatChartValue
	}

	series := make([]BrailleSeries, len(spec.Series))
	copy(series, spec.Series)
	if spec.WindowDays > 0 {
		series = cropSeriesToRecentDays(series, spec.WindowDays, spec.ReferenceTime)
	}
	if spec.MaxSeries > 0 && len(series) > spec.MaxSeries {
		sort.Slice(series, func(i, j int) bool {
			li := seriesVolume(series[i])
			lj := seriesVolume(series[j])
			if li == lj {
				return series[i].Label < series[j].Label
			}
			return li > lj
		})
		series = series[:spec.MaxSeries]
	}

	switch spec.Mode {
	case TimeChartStacked:
		return renderNTTimeBars(spec.Title, series, w, h, yFmt, true, spec.PreserveEmptySpan || spec.WindowDays > 0)
	case TimeChartBars:
		return renderNTTimeBars(spec.Title, series[:1], w, h, yFmt, false, spec.PreserveEmptySpan || spec.WindowDays > 0)
	default:
		return renderNTBrailleChart(spec.Title, series, w, h, yFmt)
	}
}

func renderNTTimeBars(title string, series []BrailleSeries, w, h int, yFmt func(float64) string, stacked bool, preserveEmptySpan bool) string {
	if len(series) == 0 {
		return ""
	}

	// Sanitize: clamp negatives before bar aggregation.
	for i := range series {
		series[i].Points = sanitizeSeriesPoints(series[i].Points)
	}

	dates, values := alignSeriesByDate(series, true)
	if !stacked {
		dates, values = alignSeriesByDate(series[:1], true)
	}
	if len(dates) == 0 || len(values) == 0 {
		return ""
	}

	if !preserveEmptySpan {
		dates, values = trimAlignedDateSpan(dates, values, 1)
		if len(dates) == 0 || len(values) == 0 {
			return ""
		}
	}

	chartW := max(20, w-4)
	targetCols := len(dates)
	if targetCols > chartW/2 {
		targetCols = max(3, chartW/2)
	}
	labels, binnedValues := binSeriesValues(dates, values, targetCols)
	if len(labels) == 0 {
		return ""
	}

	maxVal := 0.0
	data := make([]ntbarchart.BarData, 0, len(labels))
	for di, label := range labels {
		bar := ntbarchart.BarData{Label: label}
		total := 0.0
		for si, s := range series {
			if si >= len(binnedValues) || di >= len(binnedValues[si]) {
				continue
			}
			v := binnedValues[si][di]
			if v <= 0 {
				continue
			}
			total += v
			name := s.Label
			if !stacked && si > 0 {
				continue
			}
			bar.Values = append(bar.Values, ntbarchart.BarValue{
				Name:  name,
				Value: v,
				Style: lipgloss.NewStyle().Foreground(s.Color),
			})
		}
		if total > maxVal {
			maxVal = total
		}
		if !stacked && len(bar.Values) > 1 {
			bar.Values = bar.Values[:1]
		}
		data = append(data, bar)
	}
	if maxVal <= 0 {
		return ""
	}

	chart := ntbarchart.New(
		chartW,
		max(4, h),
		ntbarchart.WithNoAxis(),
		ntbarchart.WithMaxValue(maxVal),
		ntbarchart.WithBarGap(0),
	)
	chart.PushAll(data)
	chart.Draw()

	body := renderNTChartBlock(title, chart.View(), chartW, renderNTDateLegend(labels, chartW))
	if stacked {
		return body + renderWrappedLegend(series, w)
	}
	return body + "  " + lipgloss.NewStyle().Foreground(series[0].Color).Render("■") + " " + dimStyle.Render(series[0].Label) + "\n"
}

func renderNTHeatmap(spec HeatmapSpec, w int) string {
	if len(spec.Rows) == 0 || len(spec.Cols) == 0 || len(spec.Values) == 0 {
		return ""
	}

	cols := append([]string(nil), spec.Cols...)
	values := make([][]float64, len(spec.Values))
	for i := range spec.Values {
		values[i] = append([]float64(nil), spec.Values[i]...)
	}

	rowLabelW := clamp(w/5, 16, 28)
	maxCols := spec.MaxCols
	if maxCols <= 0 {
		maxCols = clamp(w-rowLabelW-8, 20, 80)
	}
	if len(cols) > maxCols {
		step := float64(len(cols)) / float64(maxCols)
		newCols := make([]string, 0, maxCols)
		newVals := make([][]float64, len(values))
		for i := range newVals {
			newVals[i] = make([]float64, 0, maxCols)
		}
		for c := 0; c < maxCols; c++ {
			idx := int(float64(c) * step)
			if idx >= len(cols) {
				idx = len(cols) - 1
			}
			newCols = append(newCols, cols[idx])
			for r := range values {
				v := 0.0
				if idx < len(values[r]) {
					v = values[r][idx]
				}
				newVals[r] = append(newVals[r], v)
			}
		}
		cols = newCols
		values = newVals
	}

	globalMax := 0.0
	rowMax := make([]float64, len(values))
	for r := range values {
		for c := range values[r] {
			v := values[r][c]
			if v > rowMax[r] {
				rowMax[r] = v
			}
			if v > globalMax {
				globalMax = v
			}
		}
	}
	if globalMax <= 0 {
		return ""
	}

	// Render as a custom text-based heatmap instead of using ntcharts heatmap
	// (which sizes its grid in data-columns, not terminal chars). We render
	// one character per data cell with colored block characters.
	numCols := len(cols)
	numRows := len(spec.Rows)
	intensityGlyphs := []string{"·", "░", "▒", "▓", "█"}

	// Compute cell width: distribute available space evenly across columns.
	summaryW := 0
	if len(spec.RowSummary) > 0 {
		summaryW = 10
	}
	gridAvail := w - rowLabelW - summaryW - 8 // margins + padding
	if gridAvail < numCols {
		gridAvail = numCols
	}
	cellW := gridAvail / numCols
	if cellW < 1 {
		cellW = 1
	}
	if cellW > 2 {
		cellW = 2
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	sb.WriteString("  " + sectionStyle.Render(spec.Title) + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	// Render each row.
	for r := 0; r < numRows; r++ {
		labelColor := colorDim
		if r < len(spec.RowColors) && spec.RowColors[r] != "" {
			labelColor = spec.RowColors[r]
		}
		sb.WriteString("  ")
		sb.WriteString(lipgloss.NewStyle().Foreground(labelColor).Render(padRight(truncStr(spec.Rows[r], rowLabelW), rowLabelW)))
		sb.WriteString(" ")

		for c := 0; c < numCols; c++ {
			raw := 0.0
			if r < len(values) && c < len(values[r]) {
				raw = values[r][c]
			}
			scale := globalMax
			if spec.RowScale && rowMax[r] > 0 {
				scale = rowMax[r]
			}
			norm := 0.0
			if scale > 0 {
				norm = raw / scale
			}

			ci := int(math.Round(norm * float64(len(intensityGlyphs)-1)))
			if ci < 0 {
				ci = 0
			}
			if ci >= len(intensityGlyphs) {
				ci = len(intensityGlyphs) - 1
			}
			glyph := strings.Repeat(intensityGlyphs[ci], cellW)
			style := lipgloss.NewStyle().Foreground(labelColor)
			if ci == 0 {
				style = lipgloss.NewStyle().Foreground(colorDim)
			}
			sb.WriteString(style.Render(glyph))
		}
		if summaryW > 0 && r < len(spec.RowSummary) {
			sb.WriteString(" ")
			sb.WriteString(dimStyle.Render(padLeft(truncStr(spec.RowSummary[r], summaryW), summaryW)))
		}
		sb.WriteString("\n")
	}

	// Column labels: show evenly spaced date markers.
	totalGridW := numCols * cellW
	numLabels := 5
	if totalGridW < 40 {
		numLabels = 3
	}
	if numLabels > numCols {
		numLabels = numCols
	}
	dateLine := make([]byte, totalGridW)
	for i := range dateLine {
		dateLine[i] = ' '
	}
	for i := 0; i < numLabels; i++ {
		ci := 0
		if numLabels > 1 {
			ci = i * (numCols - 1) / (numLabels - 1)
		}
		label := formatDateLabel(cols[ci])
		x := ci * cellW
		start := x
		if start+len(label) > totalGridW {
			start = totalGridW - len(label)
		}
		if start < 0 {
			start = 0
		}
		for j := 0; j < len(label) && start+j < totalGridW; j++ {
			dateLine[start+j] = label[j]
		}
	}
	sb.WriteString("  " + strings.Repeat(" ", rowLabelW+1) + dimStyle.Render(string(dateLine)) + "\n")

	// Legend.
	sb.WriteString("  " + dimStyle.Render("      low "))
	for i, glyph := range intensityGlyphs {
		style := lipgloss.NewStyle().Foreground(colorDim)
		if i > 0 {
			style = lipgloss.NewStyle().Foreground(colorYellow)
		}
		sb.WriteString(style.Render(glyph + " "))
	}
	sb.WriteString(dimStyle.Render("high\n"))
	return sb.String()
}

func renderNTChartBlock(title, body string, bodyW int, footer string) string {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return ""
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	sb.WriteString("  " + sectionStyle.Render(title) + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", max(1, bodyW))) + "\n")
	for _, line := range strings.Split(body, "\n") {
		sb.WriteString("  " + line + "\n")
	}
	if footer != "" {
		sb.WriteString(footer)
	}
	return sb.String()
}

func distributeNTBarWidths(segments []ntBarSegment, total float64, width int) []int {
	widths := make([]int, len(segments))
	if len(segments) == 0 || total <= 0 || width <= 0 {
		return widths
	}

	type remainder struct {
		idx   int
		frac  float64
		value float64
	}

	remainders := make([]remainder, 0, len(segments))
	used := 0
	for i, segment := range segments {
		exact := segment.Value / total * float64(width)
		whole := int(math.Floor(exact))
		widths[i] = whole
		used += whole
		remainders = append(remainders, remainder{
			idx:   i,
			frac:  exact - float64(whole),
			value: segment.Value,
		})
	}

	sort.Slice(remainders, func(i, j int) bool {
		if remainders[i].frac == remainders[j].frac {
			if remainders[i].value == remainders[j].value {
				return remainders[i].idx < remainders[j].idx
			}
			return remainders[i].value > remainders[j].value
		}
		return remainders[i].frac > remainders[j].frac
	})

	for i := 0; used < width && i < len(remainders); i++ {
		widths[remainders[i].idx]++
		used++
	}

	return widths
}

func filterChartSeries(series []BrailleSeries) []BrailleSeries {
	var filtered []BrailleSeries
	for _, s := range series {
		points := dedupeSeriesPoints(s.Points)
		hasValue := false
		for _, p := range points {
			if p.Value != 0 {
				hasValue = true
				break
			}
		}
		if hasValue {
			s.Points = points
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func chartSeriesBounds(series []BrailleSeries) (time.Time, time.Time, float64, float64, bool) {
	var minTime, maxTime time.Time
	minY := 0.0
	maxY := 0.0
	found := false
	for _, s := range series {
		for _, p := range s.Points {
			t, err := time.Parse("2006-01-02", p.Date)
			if err != nil {
				continue
			}
			if !found {
				minTime = t
				maxTime = t
				minY = p.Value
				maxY = p.Value
				found = true
				continue
			}
			if t.Before(minTime) {
				minTime = t
			}
			if t.After(maxTime) {
				maxTime = t
			}
			if p.Value < minY {
				minY = p.Value
			}
			if p.Value > maxY {
				maxY = p.Value
			}
		}
	}
	if !found {
		return time.Time{}, time.Time{}, 0, 0, false
	}
	if minTime.Equal(maxTime) {
		maxTime = maxTime.Add(24 * time.Hour)
	}
	if minY == maxY {
		if minY == 0 {
			maxY = 1
		} else {
			delta := math.Abs(minY) * 0.1
			if delta == 0 {
				delta = 1
			}
			minY -= delta
			maxY += delta
		}
	} else {
		pad := (maxY - minY) * 0.1
		if pad == 0 {
			pad = 1
		}
		minY -= pad
		maxY += pad
	}
	// Floor at zero: negative values are data quality artifacts, not meaningful.
	if minY < 0 {
		minY = 0
	}
	return minTime.UTC(), maxTime.UTC(), minY, maxY, true
}

func dedupeSeriesPoints(points []core.TimePoint) []core.TimePoint {
	if len(points) == 0 {
		return nil
	}
	byDate := make(map[string]core.TimePoint, len(points))
	for _, p := range points {
		if strings.TrimSpace(p.Date) == "" {
			continue
		}
		byDate[p.Date] = p
	}
	dates := core.SortedStringKeys(byDate)
	out := make([]core.TimePoint, 0, len(dates))
	for _, date := range dates {
		out = append(out, byDate[date])
	}
	return out
}

func sampleSparklineValues(values []float64, w int) []float64 {
	if len(values) == 0 || w < 1 {
		return nil
	}
	if len(values) <= w {
		return append([]float64(nil), values...)
	}
	step := float64(len(values)) / float64(w)
	sampled := make([]float64, w)
	for i := 0; i < w; i++ {
		idx := int(float64(i) * step)
		if idx >= len(values) {
			idx = len(values) - 1
		}
		sampled[i] = values[idx]
	}
	return sampled
}

func timeChartXStep(chartW int) int {
	switch {
	case chartW >= 100:
		return 18
	case chartW >= 70:
		return 14
	case chartW >= 50:
		return 10
	default:
		return 8
	}
}

func timeChartYStep(chartH int) int {
	if chartH >= 10 {
		return 2
	}
	return 1
}

func renderNTDateLegend(labels []string, width int) string {
	if len(labels) == 0 || width <= 0 {
		return ""
	}
	if width < 3 {
		width = 3
	}
	line := make([]byte, width)
	for i := range line {
		line[i] = ' '
	}
	put := func(text string, pos int) {
		if text == "" {
			return
		}
		start := pos - len(text)/2
		if start < 0 {
			start = 0
		}
		if start+len(text) > len(line) {
			start = len(line) - len(text)
		}
		if start < 0 {
			start = 0
		}
		for i := 0; i < len(text) && start+i < len(line); i++ {
			line[start+i] = text[i]
		}
	}

	first := formatDateLabel(labels[0])
	mid := formatDateLabel(labels[len(labels)/2])
	last := formatDateLabel(labels[len(labels)-1])
	put(first, 0)
	put(mid, len(line)/2)
	put(last, len(line)-1)
	return "  " + dimStyle.Render(string(line)) + "\n"
}

// fillSeriesDateGaps inserts zero-value entries for any calendar days missing between
// the first and last date in a sorted series. Without this, chart libraries draw a
// straight line between e.g. Apr 3 and Apr 7, making it look like usage continued
// during days when there was actually none.
func fillSeriesDateGaps(pts []core.TimePoint) []core.TimePoint {
	if len(pts) < 2 {
		return pts
	}
	first, err1 := time.Parse("2006-01-02", pts[0].Date)
	last, err2 := time.Parse("2006-01-02", pts[len(pts)-1].Date)
	if err1 != nil || err2 != nil {
		return pts
	}
	days := int(last.Sub(first).Hours()/24) + 1
	if days <= len(pts) || days > 400 {
		return pts // no gaps, or unreasonably large range
	}

	byDate := make(map[string]float64, len(pts))
	for _, p := range pts {
		byDate[p.Date] = p.Value
	}

	out := make([]core.TimePoint, 0, days)
	for d := 0; d < days; d++ {
		date := first.AddDate(0, 0, d).Format("2006-01-02")
		val := byDate[date] // zero if absent
		out = append(out, core.TimePoint{Date: date, Value: val})
	}
	return out
}

// sanitizeSeriesPoints clamps negative values to zero. Negative values in cost/token
// metrics represent data quality issues (refunds, reconciliation adjustments) rather
// than meaningful data. The original slice is not modified.
func sanitizeSeriesPoints(pts []core.TimePoint) []core.TimePoint {
	out := make([]core.TimePoint, len(pts))
	for i, p := range pts {
		out[i] = p
		if p.Value < 0 {
			out[i].Value = 0
		}
	}
	return out
}
