package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

type chartItem struct {
	Label    string
	Value    float64
	Color    lipgloss.Color
	SubLabel string
}

func RenderInlineGauge(pct float64, w int) string {
	if w < 4 {
		w = 4
	}
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}

	filled := int(pct / 100 * float64(w))
	if filled < 1 && pct > 0 {
		filled = 1
	}
	empty := w - filled

	barColor := colorGreen
	if pct >= 80 {
		barColor = colorRed
	} else if pct >= 50 {
		barColor = colorYellow
	}

	bar := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("█", filled))
	track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", empty))

	return bar + track
}

var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func RenderSparkline(values []float64, w int, color lipgloss.Color) string {
	if len(values) == 0 || w < 1 {
		return ""
	}

	if len(values) > w {
		step := float64(len(values)) / float64(w)
		sampled := make([]float64, w)
		for i := 0; i < w; i++ {
			idx := int(float64(i) * step)
			if idx >= len(values) {
				idx = len(values) - 1
			}
			sampled[i] = values[idx]
		}
		values = sampled
	}

	minV, maxV := values[0], values[0]
	for _, v := range values {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}

	rng := maxV - minV
	if rng == 0 {
		rng = 1
	}

	var sb strings.Builder
	for _, v := range values {
		idx := int((v - minV) / rng * float64(len(sparkBlocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		sb.WriteRune(sparkBlocks[idx])
	}

	return lipgloss.NewStyle().Foreground(color).Render(sb.String())
}

func RenderHBarChart(items []chartItem, maxBarW, labelW int) string {
	if len(items) == 0 {
		return dimStyle.Render("  No data available")
	}
	if maxBarW < 4 {
		maxBarW = 4
	}

	maxVal := float64(0)
	for _, item := range items {
		if item.Value > maxVal {
			maxVal = item.Value
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	var lines []string
	for _, item := range items {
		label := item.Label
		if len(label) > labelW {
			label = label[:labelW-1] + "…"
		}

		labelRendered := labelStyle.Width(labelW).Render(label)

		barLen := int(item.Value / maxVal * float64(maxBarW))
		if barLen < 1 && item.Value > 0 {
			barLen = 1
		}
		emptyLen := maxBarW - barLen

		bar := lipgloss.NewStyle().Foreground(item.Color).Render(strings.Repeat("█", barLen))
		track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", emptyLen))

		valueStr := lipgloss.NewStyle().Foreground(item.Color).Bold(true).Render(formatUSD(item.Value))

		line := fmt.Sprintf("  %s %s%s  %s", labelRendered, bar, track, valueStr)

		if item.SubLabel != "" {
			line += "  " + dimStyle.Render(item.SubLabel)
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func RenderBudgetGauge(label string, used, limit float64, barW, labelW int, color lipgloss.Color, burnRate float64) string {
	if barW < 4 {
		barW = 4
	}
	if limit <= 0 {
		limit = 1
	}

	pct := used / limit * 100
	if pct > 100 {
		pct = 100
	}

	lbl := label
	if len(lbl) > labelW {
		lbl = lbl[:labelW-1] + "…"
	}

	filled := int(pct / 100 * float64(barW))
	if filled < 1 && used > 0 {
		filled = 1
	}
	empty := barW - filled

	barColor := colorGreen
	switch {
	case pct >= 80:
		barColor = colorRed
	case pct >= 50:
		barColor = colorYellow
	}

	bar := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("█", filled))
	track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", empty))

	detail := fmt.Sprintf("%s / %s  %.0f%%", formatUSD(used), formatUSD(limit), pct)
	detailRendered := lipgloss.NewStyle().Foreground(color).Bold(true).Render(detail)

	line := fmt.Sprintf("  %s %s%s  %s",
		labelStyle.Width(labelW).Render(lbl),
		bar, track, detailRendered)

	if burnRate > 0 {
		remaining := limit - used
		if remaining > 0 {
			hoursLeft := remaining / burnRate
			daysLeft := hoursLeft / 24
			projStr := ""
			icon := "⚠"
			if daysLeft < 3 {
				icon = "🔴"
				projStr = fmt.Sprintf("%.0f hours until limit at $%.2f/h", hoursLeft, burnRate)
			} else if daysLeft < 14 {
				icon = "🟡"
				projStr = fmt.Sprintf("~%.0f days until limit at $%.2f/h", daysLeft, burnRate)
			} else {
				icon = "🟢"
				projStr = fmt.Sprintf("~%.0f days remaining at $%.2f/h", daysLeft, burnRate)
			}
			projection := fmt.Sprintf("  %s %s %s",
				strings.Repeat(" ", labelW),
				lipgloss.NewStyle().Foreground(barColor).Render(icon),
				dimStyle.Render(projStr))
			line += "\n" + projection
		}
	}

	return line
}

func RenderTokenBreakdown(input, output float64, w int) string {
	if input == 0 && output == 0 {
		return ""
	}

	var sb strings.Builder

	barW := w - 30
	if barW < 8 {
		barW = 8
	}
	if barW > 30 {
		barW = 30
	}

	maxVal := input
	if output > maxVal {
		maxVal = output
	}
	if maxVal == 0 {
		maxVal = 1
	}

	inLen := int(input / maxVal * float64(barW))
	if inLen < 1 && input > 0 {
		inLen = 1
	}
	inBar := lipgloss.NewStyle().Foreground(colorSapphire).Render(strings.Repeat("█", inLen))
	inTrack := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", barW-inLen))
	sb.WriteString(fmt.Sprintf("  %s %s%s  %s\n",
		lipgloss.NewStyle().Foreground(colorSapphire).Width(8).Render("Input"),
		inBar, inTrack,
		dimStyle.Render(formatTokens(input)+" tok")))

	outLen := int(output / maxVal * float64(barW))
	if outLen < 1 && output > 0 {
		outLen = 1
	}
	outBar := lipgloss.NewStyle().Foreground(colorPeach).Render(strings.Repeat("█", outLen))
	outTrack := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", barW-outLen))
	sb.WriteString(fmt.Sprintf("  %s %s%s  %s",
		lipgloss.NewStyle().Foreground(colorPeach).Width(8).Render("Output"),
		outBar, outTrack,
		dimStyle.Render(formatTokens(output)+" tok")))

	return sb.String()
}

func formatChartValue(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.1fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%.1fK", v/1_000)
	}
	if v == float64(int(v)) {
		return fmt.Sprintf("%d", int(v))
	}
	return fmt.Sprintf("%.1f", v)
}

func formatDateLabel(d string) string {
	if len(d) < 10 {
		return d
	}
	months := map[string]string{
		"01": "Jan", "02": "Feb", "03": "Mar", "04": "Apr",
		"05": "May", "06": "Jun", "07": "Jul", "08": "Aug",
		"09": "Sep", "10": "Oct", "11": "Nov", "12": "Dec",
	}
	month := months[d[5:7]]
	if month == "" {
		month = d[5:7]
	}
	day := d[8:10]
	if day[0] == '0' {
		day = day[1:]
	}
	return month + " " + day
}

func formatCostAxis(v float64) string {
	if v == 0 {
		return "$0"
	}
	if v >= 10000 {
		return fmt.Sprintf("$%.0fK", v/1000)
	}
	if v >= 1000 {
		return fmt.Sprintf("$%.1fK", v/1000)
	}
	if v >= 100 {
		return fmt.Sprintf("$%.0f", v)
	}
	if v >= 1 {
		return fmt.Sprintf("$%.1f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

type BrailleSeries struct {
	Label  string
	Color  lipgloss.Color
	Points []core.TimePoint
}

type TimeChartMode int

const (
	TimeChartLine TimeChartMode = iota
	TimeChartStacked
	TimeChartBars
)

type TimeChartSpec struct {
	Title      string
	Mode       TimeChartMode
	Series     []BrailleSeries
	Height     int
	MaxSeries  int
	WindowDays int
	YFmt       func(float64) string
}

type HeatmapSpec struct {
	Title     string
	Rows      []string
	Cols      []string
	Values    [][]float64 // [row][col]
	MaxCols   int
	RowColors []lipgloss.Color
	RowScale  bool
}

var brailleDots = [4][2]rune{
	{0x01, 0x08}, // top
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80}, // bottom
}

type brailleCanvas struct {
	cw, ch int   // character dimensions
	pw, ph int   // pixel dimensions (cw*2, ch*4)
	grid   []int // flat [ph*pw], series index per pixel (-1 = empty)
}

func newBrailleCanvas(cw, ch int) *brailleCanvas {
	pw, ph := cw*2, ch*4
	grid := make([]int, pw*ph)
	for i := range grid {
		grid[i] = -1
	}
	return &brailleCanvas{cw: cw, ch: ch, pw: pw, ph: ph, grid: grid}
}

func (c *brailleCanvas) set(px, py, seriesIdx int) {
	if px >= 0 && px < c.pw && py >= 0 && py < c.ph {
		c.grid[py*c.pw+px] = seriesIdx
	}
}

func (c *brailleCanvas) drawLine(x0, y0, x1, y1, seriesIdx int) {
	dx := float64(x1 - x0)
	dy := float64(y1 - y0)
	steps := math.Abs(dx)
	if math.Abs(dy) > steps {
		steps = math.Abs(dy)
	}
	if steps == 0 {
		c.set(x0, y0, seriesIdx)
		return
	}
	xInc := dx / steps
	yInc := dy / steps
	x, y := float64(x0), float64(y0)
	for i := 0; i <= int(steps); i++ {
		px := int(math.Round(x))
		py := int(math.Round(y))
		c.set(px, py, seriesIdx)
		x += xInc
		y += yInc
	}
}

func (c *brailleCanvas) fillBelow(seriesIdx int) {
	for px := 0; px < c.pw; px++ {
		topMost := -1
		for py := 0; py < c.ph; py++ {
			if c.grid[py*c.pw+px] == seriesIdx {
				topMost = py
				break
			}
		}
		if topMost >= 0 {
			for py := topMost; py < c.ph; py++ {
				if c.grid[py*c.pw+px] < 0 {
					c.grid[py*c.pw+px] = seriesIdx
				}
			}
		}
	}
}

func (c *brailleCanvas) render(colors []lipgloss.Color) []string {
	lines := make([]string, c.ch)
	for cy := 0; cy < c.ch; cy++ {
		var sb strings.Builder
		for cx := 0; cx < c.cw; cx++ {
			pattern := rune(0x2800)
			counts := make(map[int]int)

			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					py := cy*4 + dy
					px := cx*2 + dx
					si := c.grid[py*c.pw+px]
					if si >= 0 {
						pattern |= brailleDots[dy][dx]
						counts[si]++
					}
				}
			}

			if pattern == 0x2800 {
				sb.WriteRune(' ')
			} else {
				bestSi, bestCnt := 0, 0
				for si, cnt := range counts {
					if cnt > bestCnt {
						bestSi = si
						bestCnt = cnt
					}
				}
				color := colorSubtext
				if bestSi < len(colors) {
					color = colors[bestSi]
				}
				sb.WriteString(lipgloss.NewStyle().Foreground(color).Render(string(pattern)))
			}
		}
		lines[cy] = sb.String()
	}
	return lines
}

func RenderBrailleChart(title string, series []BrailleSeries, w, h int, yFmt func(float64) string) string {
	if len(series) == 0 {
		return ""
	}

	var filtered []BrailleSeries
	for _, s := range series {
		for _, p := range s.Points {
			if p.Value > 0 {
				filtered = append(filtered, s)
				break
			}
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	series = filtered

	dateSet := make(map[string]bool)
	dateHasNonZero := make(map[string]bool)
	maxY := float64(0)
	for _, s := range series {
		for _, p := range s.Points {
			dateSet[p.Date] = true
			if p.Value > 0 {
				dateHasNonZero[p.Date] = true
			}
			if p.Value > maxY {
				maxY = p.Value
			}
		}
	}

	allDates := lo.Keys(dateSet)
	sort.Strings(allDates)

	startIdx, endIdx := 0, len(allDates)-1
	for startIdx < endIdx && !dateHasNonZero[allDates[startIdx]] {
		startIdx++
	}
	for endIdx > startIdx && !dateHasNonZero[allDates[endIdx]] {
		endIdx--
	}
	if startIdx > 0 {
		startIdx--
	}
	if endIdx < len(allDates)-1 {
		endIdx++
	}
	allDates = allDates[startIdx : endIdx+1]

	if len(allDates) == 0 {
		return ""
	}
	if len(allDates) == 1 {
		if t, err := time.Parse("2006-01-02", allDates[0]); err == nil {
			allDates = append([]string{t.AddDate(0, 0, -1).Format("2006-01-02")}, allDates...)
		} else {
			return ""
		}
	}
	if maxY == 0 {
		maxY = 1
	}
	maxY *= 1.1

	yAxisW := estimateYAxisWidth(maxY, yFmt)
	plotW := w - yAxisW - 4
	if plotW < 20 {
		plotW = 20
	}

	dateIdx := make(map[string]int, len(allDates))
	for i, d := range allDates {
		dateIdx[d] = i
	}
	numDates := len(allDates)

	canvas := newBrailleCanvas(plotW, h)

	for si, s := range series {
		var pts []core.TimePoint
		for _, p := range s.Points {
			if _, ok := dateIdx[p.Date]; ok {
				pts = append(pts, p)
			}
		}
		sort.Slice(pts, func(i, j int) bool { return pts[i].Date < pts[j].Date })

		var prevPX, prevPY int
		first := true

		for _, p := range pts {
			di := dateIdx[p.Date]
			px := int(float64(di) / float64(numDates-1) * float64(canvas.pw-1))
			py := (canvas.ph - 1) - int(p.Value/maxY*float64(canvas.ph-1))
			if py < 0 {
				py = 0
			}
			if py >= canvas.ph {
				py = canvas.ph - 1
			}

			canvas.set(px, py, si)

			if !first {
				canvas.drawLine(prevPX, prevPY, px, py, si)
			}
			prevPX, prevPY = px, py
			first = false
		}
	}

	colors := make([]lipgloss.Color, len(series))
	for i, s := range series {
		colors[i] = s.Color
	}
	plotLines := canvas.render(colors)

	var sb strings.Builder

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	sb.WriteString("  " + sectionStyle.Render(title) + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	numTicks := 5
	if h < 6 {
		numTicks = 3
	}
	tickRows := make(map[int]float64, numTicks)
	for t := 0; t < numTicks; t++ {
		row := t * (h - 1) / (numTicks - 1)
		val := maxY / 1.1 * float64(numTicks-1-t) / float64(numTicks-1)
		tickRows[row] = val
	}

	axisStyle := lipgloss.NewStyle().Foreground(colorSurface1)
	for row := 0; row < h; row++ {
		label := ""
		if val, ok := tickRows[row]; ok {
			label = yFmt(val)
		}
		sb.WriteString(fmt.Sprintf("  %*s %s%s\n",
			yAxisW-2, dimStyle.Render(label),
			axisStyle.Render("┤"),
			plotLines[row]))
	}

	sb.WriteString(fmt.Sprintf("  %*s %s%s\n", yAxisW-2, "",
		axisStyle.Render("└"),
		axisStyle.Render(strings.Repeat("─", plotW))))

	numLabels := clampInt(plotW/22, 3, 6)
	if len(allDates) < numLabels {
		numLabels = len(allDates)
	}

	dateLine := make([]byte, plotW)
	for i := range dateLine {
		dateLine[i] = ' '
	}

	for i := 0; i < numLabels; i++ {
		di := 0
		if numLabels > 1 {
			di = i * (len(allDates) - 1) / (numLabels - 1)
		}
		label := formatDateLabel(allDates[di])
		x := int(float64(di) / float64(numDates-1) * float64(plotW-1))
		start := x - len(label)/2
		if start < 0 {
			start = 0
		}
		if start+len(label) > plotW {
			start = plotW - len(label)
		}
		if start < 0 {
			start = 0
		}
		for j := 0; j < len(label) && start+j < plotW; j++ {
			dateLine[start+j] = label[j]
		}
	}
	sb.WriteString(fmt.Sprintf("  %*s  %s\n", yAxisW-2, "", dimStyle.Render(string(dateLine))))

	sb.WriteString(renderWrappedLegend(series, w))

	return sb.String()
}

func RenderTimeChart(spec TimeChartSpec, w int) string {
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
		series = cropSeriesToRecentDays(series, spec.WindowDays)
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
		return renderStackedTimeChart(spec.Title, series, w, h, yFmt)
	case TimeChartBars:
		return renderBarTimeChart(spec.Title, series, w, h, yFmt)
	default:
		return RenderBrailleChart(spec.Title, series, w, h, yFmt)
	}
}

func seriesVolume(s BrailleSeries) float64 {
	total := 0.0
	for _, p := range s.Points {
		total += p.Value
	}
	return total
}

func cropSeriesToRecentDays(series []BrailleSeries, days int) []BrailleSeries {
	if days <= 0 || len(series) == 0 {
		return series
	}
	var maxDate time.Time
	found := false
	for _, s := range series {
		for _, p := range s.Points {
			t, err := time.Parse("2006-01-02", p.Date)
			if err != nil {
				continue
			}
			if !found || t.After(maxDate) {
				maxDate = t
				found = true
			}
		}
	}
	if !found {
		return series
	}
	cutoff := maxDate.AddDate(0, 0, -(days - 1))
	out := make([]BrailleSeries, 0, len(series))
	for _, s := range series {
		pts := make([]core.TimePoint, 0, len(s.Points))
		for _, p := range s.Points {
			t, err := time.Parse("2006-01-02", p.Date)
			if err != nil {
				continue
			}
			if t.Before(cutoff) || t.After(maxDate) {
				continue
			}
			pts = append(pts, p)
		}
		if len(pts) == 0 {
			continue
		}
		s.Points = pts
		out = append(out, s)
	}
	return out
}

func renderStackedTimeChart(title string, series []BrailleSeries, w, h int, yFmt func(float64) string) string {
	if len(series) == 0 {
		return ""
	}
	dates, values := alignSeriesByDate(series, true)
	if len(dates) == 0 {
		return ""
	}
	dates, values = trimAlignedDateSpan(dates, values, 1)
	if len(dates) == 0 {
		return ""
	}
	if h < 6 {
		h = 6
	}

	plotW := w - 12 - 4
	if plotW < 20 {
		plotW = 20
	}
	labels, binnedValues := binSeriesValues(dates, values, plotW)
	if len(labels) == 0 {
		return ""
	}
	totals := make([]float64, len(labels))
	maxTotal := 0.0
	for i := range labels {
		sum := 0.0
		for si := range series {
			sum += binnedValues[si][i]
		}
		totals[i] = sum
		if sum > maxTotal {
			maxTotal = sum
		}
	}
	if maxTotal <= 0 {
		return ""
	}
	yAxisW := estimateYAxisWidth(maxTotal, yFmt)
	plotW = w - yAxisW - 4
	if plotW < 20 {
		plotW = 20
	}
	labels, binnedValues = binSeriesValues(dates, values, plotW)
	if len(labels) == 0 {
		return ""
	}
	colStarts, colEnds := layoutColumns(plotW, len(labels))
	totals = make([]float64, len(labels))
	maxTotal = 0.0
	for i := range labels {
		sum := 0.0
		for si := range series {
			sum += binnedValues[si][i]
		}
		totals[i] = sum
		if sum > maxTotal {
			maxTotal = sum
		}
	}
	if maxTotal <= 0 {
		return ""
	}

	axisStyle := lipgloss.NewStyle().Foreground(colorSurface1)
	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	sb.WriteString("  " + sectionStyle.Render(title) + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	numTicks := 5
	tickRows := make(map[int]float64, numTicks)
	for t := 0; t < numTicks; t++ {
		row := t * (h - 1) / (numTicks - 1)
		val := maxTotal * float64(numTicks-1-t) / float64(numTicks-1)
		tickRows[row] = val
	}

	for row := 0; row < h; row++ {
		label := ""
		if val, ok := tickRows[row]; ok {
			label = yFmt(val)
		}
		sb.WriteString(fmt.Sprintf("  %*s %s", yAxisW-2, dimStyle.Render(label), axisStyle.Render("┤")))
		threshold := maxTotal * float64(h-1-row) / float64(h-1)
		for di := range labels {
			cum := 0.0
			cell := " "
			for si := range series {
				next := cum + binnedValues[si][di]
				if threshold <= next && totals[di] >= threshold && binnedValues[si][di] > 0 {
					cell = lipgloss.NewStyle().Foreground(series[si].Color).Render("█")
					break
				}
				cum = next
			}
			for x := colStarts[di]; x < colEnds[di]; x++ {
				sb.WriteString(cell)
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("  %*s %s%s\n", yAxisW-2, "", axisStyle.Render("└"), axisStyle.Render(strings.Repeat("─", plotW))))

	numLabels := clampInt(plotW/22, 3, 6)
	if len(labels) < numLabels {
		numLabels = len(labels)
	}
	dateLine := make([]byte, plotW)
	for i := range dateLine {
		dateLine[i] = ' '
	}
	for i := 0; i < numLabels; i++ {
		di := 0
		if numLabels > 1 {
			di = i * (len(labels) - 1) / (numLabels - 1)
		}
		label := formatDateLabel(labels[di])
		x := (colStarts[di] + colEnds[di] - 1) / 2
		start := x - len(label)/2
		if start < 0 {
			start = 0
		}
		if start+len(label) > plotW {
			start = plotW - len(label)
		}
		for j := 0; j < len(label) && start+j < plotW; j++ {
			dateLine[start+j] = label[j]
		}
	}
	sb.WriteString(fmt.Sprintf("  %*s  %s\n", yAxisW-2, "", dimStyle.Render(string(dateLine))))

	sb.WriteString(renderWrappedLegend(series, w))
	return sb.String()
}

func renderBarTimeChart(title string, series []BrailleSeries, w, h int, yFmt func(float64) string) string {
	if len(series) == 0 {
		return ""
	}
	// For bar mode we visualize the first series.
	base := series[0]
	dates, values := alignSeriesByDate([]BrailleSeries{base}, true)
	if len(dates) == 0 || len(values) == 0 || len(values[0]) == 0 {
		return ""
	}
	dates, values = trimAlignedDateSpan(dates, values, 1)
	if len(dates) == 0 || len(values) == 0 || len(values[0]) == 0 {
		return ""
	}
	if h < 6 {
		h = 6
	}
	plotW := w - 12 - 4
	if plotW < 20 {
		plotW = 20
	}
	labels, binnedValues := binSeriesValues(dates, values, plotW)
	if len(labels) == 0 || len(binnedValues) == 0 || len(binnedValues[0]) == 0 {
		return ""
	}
	vals := binnedValues[0]
	maxY := 0.0
	for _, v := range vals {
		if v > maxY {
			maxY = v
		}
	}
	if maxY <= 0 {
		return ""
	}
	yAxisW := estimateYAxisWidth(maxY, yFmt)
	plotW = w - yAxisW - 4
	if plotW < 20 {
		plotW = 20
	}
	labels, binnedValues = binSeriesValues(dates, values, plotW)
	if len(labels) == 0 || len(binnedValues) == 0 || len(binnedValues[0]) == 0 {
		return ""
	}
	vals = binnedValues[0]
	colStarts, colEnds := layoutColumns(plotW, len(labels))
	maxY = 0.0
	for _, v := range vals {
		if v > maxY {
			maxY = v
		}
	}
	if maxY <= 0 {
		return ""
	}

	axisStyle := lipgloss.NewStyle().Foreground(colorSurface1)
	barStyle := lipgloss.NewStyle().Foreground(base.Color)

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	sb.WriteString("  " + sectionStyle.Render(title) + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	numTicks := 5
	tickRows := make(map[int]float64, numTicks)
	for t := 0; t < numTicks; t++ {
		row := t * (h - 1) / (numTicks - 1)
		val := maxY * float64(numTicks-1-t) / float64(numTicks-1)
		tickRows[row] = val
	}

	for row := 0; row < h; row++ {
		label := ""
		if val, ok := tickRows[row]; ok {
			label = yFmt(val)
		}
		sb.WriteString(fmt.Sprintf("  %*s %s", yAxisW-2, dimStyle.Render(label), axisStyle.Render("┤")))
		threshold := maxY * float64(h-1-row) / float64(h-1)
		for di := range labels {
			v := vals[di]
			if v >= threshold && v > 0 {
				for x := colStarts[di]; x < colEnds[di]; x++ {
					sb.WriteString(barStyle.Render("█"))
				}
			} else {
				for x := colStarts[di]; x < colEnds[di]; x++ {
					sb.WriteString(" ")
				}
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("  %*s %s%s\n", yAxisW-2, "", axisStyle.Render("└"), axisStyle.Render(strings.Repeat("─", plotW))))

	numLabels := clampInt(plotW/22, 3, 6)
	if len(labels) < numLabels {
		numLabels = len(labels)
	}
	dateLine := make([]byte, plotW)
	for i := range dateLine {
		dateLine[i] = ' '
	}
	for i := 0; i < numLabels; i++ {
		di := 0
		if numLabels > 1 {
			di = i * (len(labels) - 1) / (numLabels - 1)
		}
		label := formatDateLabel(labels[di])
		x := (colStarts[di] + colEnds[di] - 1) / 2
		start := x - len(label)/2
		if start < 0 {
			start = 0
		}
		if start+len(label) > plotW {
			start = plotW - len(label)
		}
		for j := 0; j < len(label) && start+j < plotW; j++ {
			dateLine[start+j] = label[j]
		}
	}
	sb.WriteString(fmt.Sprintf("  %*s  %s\n", yAxisW-2, "", dimStyle.Render(string(dateLine))))
	sb.WriteString("  " + barStyle.Render("■") + " " + dimStyle.Render(base.Label) + "\n")
	return sb.String()
}

func estimateYAxisWidth(maxY float64, yFmt func(float64) string) int {
	if yFmt == nil {
		yFmt = formatChartValue
	}
	cands := []float64{0, maxY * 0.25, maxY * 0.5, maxY * 0.75, maxY}
	maxW := 0
	for _, v := range cands {
		w := lipgloss.Width(yFmt(v))
		if w > maxW {
			maxW = w
		}
	}
	axisW := maxW + 3
	if axisW < 8 {
		axisW = 8
	}
	if axisW > 12 {
		axisW = 12
	}
	return axisW
}

func renderWrappedLegend(series []BrailleSeries, w int) string {
	if len(series) == 0 {
		return ""
	}
	markers := []string{"●", "◆", "■", "▲", "★"}
	parts := make([]string, 0, len(series))
	for i, s := range series {
		m := markers[i%len(markers)]
		part := lipgloss.NewStyle().Foreground(s.Color).Render(m) + " " + dimStyle.Render(truncStr(s.Label, 26))
		parts = append(parts, part)
	}

	maxW := w - 4
	if maxW < 20 {
		maxW = 20
	}
	lines := []string{"  "}
	lineW := 2
	for _, p := range parts {
		pw := lipgloss.Width(p)
		addSep := 0
		if lineW > 2 {
			addSep = 3
		}
		if lineW+addSep+pw > maxW {
			lines = append(lines, "  "+p)
			lineW = 2 + pw
			continue
		}
		if addSep > 0 {
			lines[len(lines)-1] += "   "
			lineW += 3
		}
		lines[len(lines)-1] += p
		lineW += pw
	}
	return strings.Join(lines, "\n") + "\n"
}

func alignSeriesByDate(series []BrailleSeries, continuous bool) ([]string, [][]float64) {
	dateSet := make(map[string]bool)
	for _, s := range series {
		for _, p := range s.Points {
			dateSet[p.Date] = true
		}
	}
	dates := lo.Keys(dateSet)
	sort.Strings(dates)
	if len(dates) == 0 {
		return nil, nil
	}
	if continuous {
		dates = fillContinuousDates(dates)
	}
	dateIdx := make(map[string]int, len(dates))
	for i, d := range dates {
		dateIdx[d] = i
	}
	values := make([][]float64, len(series))
	for si, s := range series {
		row := make([]float64, len(dates))
		for _, p := range s.Points {
			if di, ok := dateIdx[p.Date]; ok {
				row[di] = p.Value
			}
		}
		values[si] = row
	}
	return dates, values
}

func fillContinuousDates(sortedDates []string) []string {
	if len(sortedDates) == 0 {
		return nil
	}
	start, errStart := time.Parse("2006-01-02", sortedDates[0])
	end, errEnd := time.Parse("2006-01-02", sortedDates[len(sortedDates)-1])
	if errStart != nil || errEnd != nil || end.Before(start) {
		return sortedDates
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days <= 0 || days > 370 {
		return sortedDates
	}
	out := make([]string, 0, days)
	for d := 0; d < days; d++ {
		out = append(out, start.AddDate(0, 0, d).Format("2006-01-02"))
	}
	return out
}

func trimAlignedDateSpan(dates []string, values [][]float64, pad int) ([]string, [][]float64) {
	if len(dates) == 0 || len(values) == 0 {
		return dates, values
	}
	start := -1
	end := -1
	for di := range dates {
		sum := 0.0
		for si := range values {
			if di < len(values[si]) {
				sum += values[si][di]
			}
		}
		if sum > 0 {
			if start == -1 {
				start = di
			}
			end = di
		}
	}
	if start == -1 || end == -1 {
		return dates, values
	}
	if pad > 0 {
		start -= pad
		end += pad
		if start < 0 {
			start = 0
		}
		if end >= len(dates) {
			end = len(dates) - 1
		}
	}
	newDates := append([]string(nil), dates[start:end+1]...)
	newValues := make([][]float64, len(values))
	for si := range values {
		if start >= len(values[si]) {
			newValues[si] = nil
			continue
		}
		localEnd := end
		if localEnd >= len(values[si]) {
			localEnd = len(values[si]) - 1
		}
		newValues[si] = append([]float64(nil), values[si][start:localEnd+1]...)
	}
	return newDates, newValues
}

func binSeriesValues(dates []string, values [][]float64, targetCols int) ([]string, [][]float64) {
	if len(dates) == 0 || len(values) == 0 || targetCols <= 0 {
		return nil, nil
	}
	cols := len(dates)
	if cols > targetCols {
		cols = targetCols
	}
	if cols <= 0 {
		return nil, nil
	}

	labels := make([]string, cols)
	binned := make([][]float64, len(values))
	for si := range binned {
		binned[si] = make([]float64, cols)
	}

	for col := 0; col < cols; col++ {
		start := col * len(dates) / cols
		end := (col + 1) * len(dates) / cols
		if end <= start {
			end = start + 1
		}
		if end > len(dates) {
			end = len(dates)
		}
		mid := start + (end-start)/2
		if mid >= len(dates) {
			mid = len(dates) - 1
		}
		labels[col] = dates[mid]

		span := float64(end - start)
		if span <= 0 {
			span = 1
		}
		for si := range values {
			sum := 0.0
			row := values[si]
			for di := start; di < end && di < len(row); di++ {
				sum += row[di]
			}
			binned[si][col] = sum / span
		}
	}

	return labels, binned
}

func layoutColumns(plotW, cols int) ([]int, []int) {
	if plotW <= 0 || cols <= 0 {
		return nil, nil
	}
	starts := make([]int, cols)
	ends := make([]int, cols)

	// For sparse timelines, keep narrow bars to avoid giant stretched blocks.
	if cols < plotW/2 {
		prev := -1
		for i := 0; i < cols; i++ {
			x := 0
			if cols == 1 {
				x = plotW / 2
			} else {
				x = int(math.Round(float64(i) / float64(cols-1) * float64(plotW-1)))
			}
			if x <= prev {
				x = prev + 1
			}
			if x >= plotW {
				x = plotW - 1
			}
			starts[i] = x
			ends[i] = x + 1
			prev = x
		}
		return starts, ends
	}

	for i := 0; i < cols; i++ {
		s := int(math.Floor(float64(i) * float64(plotW) / float64(cols)))
		e := int(math.Floor(float64(i+1) * float64(plotW) / float64(cols)))
		if e <= s {
			e = s + 1
		}
		if e > plotW {
			e = plotW
		}
		starts[i] = s
		ends[i] = e
	}
	return starts, ends
}

func RenderHeatmap(spec HeatmapSpec, w int) string {
	if len(spec.Rows) == 0 || len(spec.Cols) == 0 || len(spec.Values) == 0 {
		return ""
	}
	cols := make([]string, len(spec.Cols))
	copy(cols, spec.Cols)
	values := make([][]float64, len(spec.Values))
	for i := range spec.Values {
		values[i] = make([]float64, len(spec.Values[i]))
		copy(values[i], spec.Values[i])
	}

	rowLabelW := clampInt(w/5, 16, 28)
	maxCols := spec.MaxCols
	if maxCols <= 0 {
		maxCols = clampInt(w-rowLabelW-8, 20, 80)
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

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	sb.WriteString("  " + sectionStyle.Render(spec.Title) + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	for r := range spec.Rows {
		labelColor := colorDim
		if r < len(spec.RowColors) && spec.RowColors[r] != "" {
			labelColor = spec.RowColors[r]
		}
		sb.WriteString("  ")
		sb.WriteString(lipgloss.NewStyle().Foreground(labelColor).Render(padRight(truncStr(spec.Rows[r], rowLabelW), rowLabelW)))
		sb.WriteString(" ")
		for c := range cols {
			v := 0.0
			if r < len(values) && c < len(values[r]) {
				v = values[r][c]
			}
			scale := globalMax
			if spec.RowScale && rowMax[r] > 0 {
				scale = rowMax[r]
			}
			intensity := 0.0
			if scale > 0 {
				intensity = v / scale
			}
			sb.WriteString(heatCell(v, intensity))
		}
		sb.WriteString("\n")
	}

	labelLine := strings.Repeat(" ", rowLabelW+3)
	if len(cols) >= 3 {
		first := formatDateLabel(cols[0])
		mid := formatDateLabel(cols[len(cols)/2])
		last := formatDateLabel(cols[len(cols)-1])
		cells := len(cols)
		line := make([]byte, cells)
		for i := range line {
			line[i] = ' '
		}
		put := func(text string, pos int) {
			start := pos - len(text)/2
			if start < 0 {
				start = 0
			}
			if start+len(text) > cells {
				start = cells - len(text)
			}
			if start < 0 {
				start = 0
			}
			for i := 0; i < len(text) && start+i < cells; i++ {
				line[start+i] = text[i]
			}
		}
		put(first, 0)
		put(mid, cells/2)
		put(last, cells-1)
		labelLine += dimStyle.Render(string(line))
	}
	sb.WriteString("  " + labelLine + "\n")
	sb.WriteString("  " + dimStyle.Render("      low ") + lipgloss.NewStyle().Foreground(heatColor(0.2)).Render("██") +
		lipgloss.NewStyle().Foreground(heatColor(0.5)).Render("██") +
		lipgloss.NewStyle().Foreground(heatColor(0.8)).Render("██") +
		dimStyle.Render(" high"))
	sb.WriteString("\n")
	return sb.String()
}

func heatColor(intensity float64) lipgloss.Color {
	switch {
	case intensity >= 0.85:
		return colorRed
	case intensity >= 0.65:
		return colorPeach
	case intensity >= 0.45:
		return colorYellow
	case intensity >= 0.25:
		return colorGreen
	default:
		return colorSurface1
	}
}

func heatCell(value, intensity float64) string {
	if value <= 0 {
		return lipgloss.NewStyle().Foreground(colorSurface1).Render("·")
	}
	r := "█"
	switch {
	case intensity < 0.22:
		r = "░"
	case intensity < 0.48:
		r = "▒"
	case intensity < 0.75:
		r = "▓"
	}
	return lipgloss.NewStyle().Foreground(heatColor(intensity)).Render(r)
}
