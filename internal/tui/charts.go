package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/agentusage/internal/core"
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
		c.set(px, py-1, seriesIdx)
		c.set(px, py+1, seriesIdx)
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

	allDates := make([]string, 0, len(dateSet))
	for d := range dateSet {
		allDates = append(allDates, d)
	}
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

	yAxisW := 8
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

	if len(series) == 1 {
		canvas.fillBelow(0)
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

	numLabels := 5
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

	markers := []string{"●", "◆", "■", "▲", "★"}
	sb.WriteString("  ")
	for i, s := range series {
		if i > 0 {
			sb.WriteString("   ")
		}
		mk := markers[i%len(markers)]
		sb.WriteString(lipgloss.NewStyle().Foreground(s.Color).Render(mk))
		sb.WriteString(" " + dimStyle.Render(s.Label))
	}
	sb.WriteString("\n")

	return sb.String()
}
