package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ─── Chart Primitives ───────────────────────────────────────────────────────

// chartItem is a single data point used across chart renderers.
type chartItem struct {
	Label    string
	Value    float64
	Color    lipgloss.Color
	SubLabel string // optional secondary annotation (e.g. percentage)
}

// ═══════════════════════════════════════════════════════════════════════════
// VERTICAL BAR CHART
// ═══════════════════════════════════════════════════════════════════════════
//
// Renders a proper vertical bar chart with Y-axis, filled bars and labels:
//
//	 $1.4K ┤ ████
//	 $1.0K ┤ ████
//	  $500 ┤ ████
//	    $0 ┤ ████  ▂▂▂▂
//	       └──────────
//	        cur  cla

var vBarBlocks = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

func RenderVerticalBarChart(items []chartItem, w, h int, title string) string {
	if len(items) == 0 {
		return dimStyle.Render("  No data available\n")
	}

	var sb strings.Builder

	// Title
	if title != "" {
		sb.WriteString("  " + chartTitleStyle.Render(title) + "\n")
	}

	// Layout calculations
	yAxisW := 8     // width for Y-axis labels
	barW := 6       // width of each bar
	barGap := 2     // gap between bars
	chartH := h - 4 // reserve for x-axis, labels, title, legend
	if chartH < 4 {
		chartH = 4
	}

	maxItems := (w - yAxisW - 4) / (barW + barGap)
	if maxItems > len(items) {
		maxItems = len(items)
	}
	if maxItems < 1 {
		maxItems = 1
	}
	displayItems := items[:maxItems]

	// Find max value
	maxVal := float64(0)
	for _, item := range displayItems {
		if item.Value > maxVal {
			maxVal = item.Value
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	// Y-axis tick positions (show 4 ticks)
	numTicks := 4
	if chartH < 6 {
		numTicks = 2
	}

	// Render from top to bottom
	for row := chartH; row >= 1; row-- {
		// Y-axis label
		yLabel := ""
		for t := numTicks; t >= 0; t-- {
			tickRow := int(float64(t) / float64(numTicks) * float64(chartH))
			if row == tickRow || (row == chartH && t == numTicks) || (row == 1 && t == 0) {
				yLabel = fmtAxisVal(maxVal * float64(t) / float64(numTicks))
				break
			}
		}

		yAxis := chartAxisStyle.Render(fmt.Sprintf("%*s ┤", yAxisW-2, yLabel))

		// Bar cells
		var barLine strings.Builder
		for _, item := range displayItems {
			barHeight := item.Value / maxVal * float64(chartH)

			if float64(row) <= barHeight-1 {
				// Full block row
				barLine.WriteString(lipgloss.NewStyle().Foreground(item.Color).Render(
					strings.Repeat("█", barW)))
			} else if float64(row-1) < barHeight && float64(row) > barHeight-1 {
				// Fractional top row
				frac := barHeight - float64(row-1)
				blockIdx := int(frac * float64(len(vBarBlocks)-1))
				if blockIdx >= len(vBarBlocks) {
					blockIdx = len(vBarBlocks) - 1
				}
				if blockIdx < 1 {
					blockIdx = 1
				}
				barLine.WriteString(lipgloss.NewStyle().Foreground(item.Color).Render(
					strings.Repeat(vBarBlocks[blockIdx], barW)))
			} else {
				barLine.WriteString(strings.Repeat(" ", barW))
			}
			barLine.WriteString(strings.Repeat(" ", barGap))
		}

		sb.WriteString("  " + yAxis + barLine.String() + "\n")
	}

	// X-axis
	axisLen := len(displayItems)*(barW+barGap) + 1
	sb.WriteString("  " + chartAxisStyle.Render(fmt.Sprintf("%*s └%s", yAxisW-2, "", strings.Repeat("─", axisLen))) + "\n")

	// X-axis labels
	var labelParts strings.Builder
	labelParts.WriteString(strings.Repeat(" ", yAxisW+2))
	for _, item := range displayItems {
		label := item.Label
		cellW := barW + barGap
		if len(label) > cellW {
			label = label[:cellW-1] + "…"
		}
		labelParts.WriteString(fmt.Sprintf("%-*s", cellW, label))
	}
	sb.WriteString(dimStyle.Render(labelParts.String()) + "\n")

	// Value labels under bars
	var valParts strings.Builder
	valParts.WriteString(strings.Repeat(" ", yAxisW+2))
	for _, item := range displayItems {
		val := formatUSD(item.Value)
		cellW := barW + barGap
		if len(val) > cellW {
			val = val[:cellW-1]
		}
		valParts.WriteString(lipgloss.NewStyle().Foreground(item.Color).Bold(true).Render(
			fmt.Sprintf("%-*s", cellW, val)))
	}
	sb.WriteString("  " + valParts.String() + "\n")

	return sb.String()
}

func fmtAxisVal(v float64) string {
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
	return fmt.Sprintf("$%.1f", v)
}

// ═══════════════════════════════════════════════════════════════════════════
// HORIZONTAL BAR CHART (improved)
// ═══════════════════════════════════════════════════════════════════════════
//
//	cursor-ide    ████████████████████████████  $1,421  99.6%
//	claude-code   ▏                              $6.27   0.4%

func RenderHBarChart(items []chartItem, maxBarW, labelW int) string {
	if len(items) == 0 {
		return dimStyle.Render("  No data available")
	}
	if maxBarW < 4 {
		maxBarW = 4
	}

	// Find max value for scaling
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

		// Use thick block chars for better visual impact
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

// ═══════════════════════════════════════════════════════════════════════════
// WAFFLE CHART (proportional grid — replaces pie chart)
// ═══════════════════════════════════════════════════════════════════════════
//
//	████████████████████████████████████████████████████████████████████████████████████████████████████░
//
//	██ cursor-ide     99.6%  $1,421
//	░░ claude-code     0.4%    $6.27

func RenderWaffleChart(items []chartItem, w int, title string) string {
	if len(items) == 0 {
		return dimStyle.Render("  No data available\n")
	}

	var sb strings.Builder
	if title != "" {
		sb.WriteString("  " + chartTitleStyle.Render(title) + "\n")
	}

	total := float64(0)
	for _, item := range items {
		total += item.Value
	}
	if total == 0 {
		return dimStyle.Render("  No spend data\n")
	}

	// Waffle grid: use full width, multiple rows if needed
	gridW := w - 4 // margin
	if gridW < 20 {
		gridW = 20
	}
	gridRows := 3
	totalCells := gridW * gridRows

	// Assign cells proportionally
	type cellAssignment struct {
		cells int
		color lipgloss.Color
	}
	var assignments []cellAssignment
	assigned := 0
	for i, item := range items {
		pct := item.Value / total
		cells := int(math.Round(pct * float64(totalCells)))
		if cells < 1 && item.Value > 0 {
			cells = 1
		}
		if i == len(items)-1 {
			cells = totalCells - assigned
			if cells < 0 {
				cells = 0
			}
		}
		assigned += cells
		assignments = append(assignments, cellAssignment{cells: cells, color: item.Color})
	}

	// Render grid rows
	cellIdx := 0
	for row := 0; row < gridRows; row++ {
		var rowStr strings.Builder
		rowStr.WriteString("  ")
		for col := 0; col < gridW && cellIdx < totalCells; col++ {
			// Find which item this cell belongs to
			running := 0
			rendered := false
			for _, a := range assignments {
				running += a.cells
				if cellIdx < running {
					rowStr.WriteString(lipgloss.NewStyle().Foreground(a.color).Render("█"))
					rendered = true
					break
				}
			}
			if !rendered {
				rowStr.WriteString("░")
			}
			cellIdx++
		}
		sb.WriteString(rowStr.String() + "\n")
	}

	sb.WriteString("\n")

	// Legend
	for _, item := range items {
		pct := item.Value / total * 100
		dot := lipgloss.NewStyle().Foreground(item.Color).Render("██")
		name := item.Label
		if len(name) > 18 {
			name = name[:17] + "…"
		}
		cost := lipgloss.NewStyle().Foreground(item.Color).Bold(true).Render(formatUSD(item.Value))
		pctStr := dimStyle.Render(fmt.Sprintf("%5.1f%%", pct))

		sb.WriteString(fmt.Sprintf("  %s %-18s %s  %s\n", dot, name, pctStr, cost))
	}

	return sb.String()
}

// ═══════════════════════════════════════════════════════════════════════════
// STACKED DISTRIBUTION BAR
// ═══════════════════════════════════════════════════════════════════════════

func RenderDistributionBar(items []chartItem, totalW int) string {
	if len(items) == 0 {
		return dimStyle.Render("  No data available")
	}

	total := float64(0)
	for _, item := range items {
		total += item.Value
	}
	if total == 0 {
		return dimStyle.Render("  No spend data")
	}

	barW := totalW - 4
	if barW < 10 {
		barW = 10
	}

	// Stacked bar
	var barParts []string
	assigned := 0
	for i, item := range items {
		pct := item.Value / total
		segW := int(math.Round(pct * float64(barW)))
		if segW < 1 && item.Value > 0 {
			segW = 1
		}
		if i == len(items)-1 {
			segW = barW - assigned
			if segW < 0 {
				segW = 0
			}
		}
		assigned += segW
		barParts = append(barParts, lipgloss.NewStyle().Foreground(item.Color).Render(strings.Repeat("█", segW)))
	}

	stackedBar := "  " + strings.Join(barParts, "")

	// Legend
	var legendLines []string
	for _, item := range items {
		pct := item.Value / total * 100
		dot := lipgloss.NewStyle().Foreground(item.Color).Render("██")
		name := item.Label
		if len(name) > 16 {
			name = name[:15] + "…"
		}
		legendLine := fmt.Sprintf("  %s %-16s %5.1f%%  %s",
			dot, name, pct, formatUSD(item.Value))
		legendLines = append(legendLines, lipgloss.NewStyle().Foreground(colorSubtext).Render(legendLine))
	}

	return stackedBar + "\n" + strings.Join(legendLines, "\n")
}

// ═══════════════════════════════════════════════════════════════════════════
// SUMMARY CARD
// ═══════════════════════════════════════════════════════════════════════════

func RenderSummaryCard(title, value, subtitle string, w int, accent lipgloss.Color) string {
	titleRendered := analyticsCardTitleStyle.Render(title)
	valueRendered := analyticsCardValueStyle.Foreground(accent).Render(value)
	subtitleRendered := analyticsCardSubtitleStyle.Render(subtitle)

	content := titleRendered + "\n" + valueRendered + "\n" + subtitleRendered

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorSurface1).
		Width(w).
		Align(lipgloss.Center).
		Padding(0, 1).
		Render(content)
}

// ═══════════════════════════════════════════════════════════════════════════
// BUDGET GAUGE (enhanced with projection)
// ═══════════════════════════════════════════════════════════════════════════
//
//	cursor-ide  ████████████████░░░░░░░░░░░░░░░░  $1,421 / $3,600  39%
//	            ⚠ ~34 days until limit at $2.63/h

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

	// Color: green < 50%, yellow 50-80%, red > 80%
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

	// Add projection line if burn rate available
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

// ═══════════════════════════════════════════════════════════════════════════
// SPARKLINE
// ═══════════════════════════════════════════════════════════════════════════

var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func RenderSparkline(values []float64, w int, color lipgloss.Color) string {
	if len(values) == 0 || w < 1 {
		return ""
	}

	// If we have more values than width, downsample
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

// ═══════════════════════════════════════════════════════════════════════════
// EFFICIENCY / COMPARISON CHART
// ═══════════════════════════════════════════════════════════════════════════
//
//	gpt-4o         ████████████████████  $0.0680/1K  42K tok
//	claude-opus    ████████████████████████  $0.0740/1K  38K tok

func RenderEfficiencyChart(items []chartItem, maxBarW, labelW int) string {
	if len(items) == 0 {
		return dimStyle.Render("  No token data")
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

		valStr := fmt.Sprintf("$%.4f/1K", item.Value)
		valueRendered := lipgloss.NewStyle().Foreground(item.Color).Bold(true).Render(valStr)

		line := fmt.Sprintf("  %s %s%s  %s", labelRendered, bar, track, valueRendered)

		if item.SubLabel != "" {
			line += "  " + dimStyle.Render(item.SubLabel)
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// ═══════════════════════════════════════════════════════════════════════════
// HEATMAP TABLE
// ═══════════════════════════════════════════════════════════════════════════
//
// Renders a model × provider cost heatmap:
//
//	              cursor-ide  claude-code
//	gpt-4o           🟥          ──
//	claude-3.5       ──          🟨
//	sonnet           ──          🟩

type heatmapCell struct {
	Value float64
	Label string
}

func RenderHeatmap(rowLabels []string, colLabels []string, cells [][]heatmapCell, w int, maxVal float64, title string) string {
	if len(rowLabels) == 0 || len(colLabels) == 0 {
		return ""
	}

	var sb strings.Builder
	if title != "" {
		sb.WriteString("  " + chartTitleStyle.Render(title) + "\n\n")
	}

	rowLabelW := 18
	colW := 14
	if len(colLabels)*(colW+1)+rowLabelW+4 > w {
		colW = (w - rowLabelW - 4) / len(colLabels)
		if colW < 8 {
			colW = 8
		}
	}

	// Column headers
	header := fmt.Sprintf("  %*s", rowLabelW, "")
	for _, col := range colLabels {
		name := col
		if len(name) > colW-1 {
			name = name[:colW-2] + "…"
		}
		header += fmt.Sprintf(" %*s", colW-1, dimStyle.Bold(true).Render(name))
	}
	sb.WriteString(header + "\n")

	// Separator
	sb.WriteString("  " + strings.Repeat(" ", rowLabelW) +
		lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", len(colLabels)*colW)) + "\n")

	// Data rows
	for i, rowLabel := range rowLabels {
		name := rowLabel
		if len(name) > rowLabelW-1 {
			name = name[:rowLabelW-2] + "…"
		}
		row := fmt.Sprintf("  %-*s", rowLabelW, lipgloss.NewStyle().Foreground(colorSubtext).Render(name))

		for j := range colLabels {
			if i < len(cells) && j < len(cells[i]) {
				cell := cells[i][j]
				if cell.Value > 0 {
					// Heat color based on intensity
					intensity := cell.Value / maxVal
					heatColor := heatColor(intensity)
					block := lipgloss.NewStyle().Foreground(heatColor).Render("██")
					val := formatUSD(cell.Value)
					row += fmt.Sprintf(" %s %*s", block, colW-4, lipgloss.NewStyle().Foreground(heatColor).Render(val))
				} else {
					row += fmt.Sprintf(" %*s", colW-1, dimStyle.Render("──"))
				}
			} else {
				row += fmt.Sprintf(" %*s", colW-1, dimStyle.Render("──"))
			}
		}
		sb.WriteString(row + "\n")
	}

	// Heat legend
	sb.WriteString("\n  ")
	sb.WriteString(dimStyle.Render("  low "))
	for i := 0; i <= 4; i++ {
		c := heatColor(float64(i) / 4.0)
		sb.WriteString(lipgloss.NewStyle().Foreground(c).Render("██"))
	}
	sb.WriteString(dimStyle.Render(" high"))
	sb.WriteString("\n")

	return sb.String()
}

func heatColor(intensity float64) lipgloss.Color {
	if intensity <= 0 {
		return colorSurface1
	}
	if intensity < 0.2 {
		return colorGreen
	}
	if intensity < 0.4 {
		return colorTeal
	}
	if intensity < 0.6 {
		return colorYellow
	}
	if intensity < 0.8 {
		return colorPeach
	}
	return colorRed
}

// ═══════════════════════════════════════════════════════════════════════════
// AREA SPARKLINE (multi-row sparkline with fills)
// ═══════════════════════════════════════════════════════════════════════════
//
// Uses braille-like block chars to create a mini area chart:
//
//	    ▁▂▃▅▇█▇▅▃▂▁▁▂▃▅▇█▆▄▂▁
//	min: $1.2   avg: $4.5   max: $8.2

func RenderAreaSparkline(values []float64, w int, color lipgloss.Color, label string) string {
	if len(values) == 0 {
		return ""
	}

	var sb strings.Builder

	if label != "" {
		sb.WriteString("  " + dimStyle.Render(label) + "  ")
	}

	// Sparkline
	spark := RenderSparkline(values, w-lipgloss.Width(label)-6, color)
	sb.WriteString(spark + "\n")

	// Stats
	minV, maxV, sum := values[0], values[0], float64(0)
	for _, v := range values {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
		sum += v
	}
	avg := sum / float64(len(values))

	stats := fmt.Sprintf("  %s  min:%s  avg:%s  max:%s",
		strings.Repeat(" ", lipgloss.Width(label)),
		lipgloss.NewStyle().Foreground(colorGreen).Render(formatUSD(minV)),
		lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(avg)),
		lipgloss.NewStyle().Foreground(colorRed).Render(formatUSD(maxV)))
	sb.WriteString(dimStyle.Render(stats) + "\n")

	return sb.String()
}

// ═══════════════════════════════════════════════════════════════════════════
// DONUT CHART (ASCII approximation)
// ═══════════════════════════════════════════════════════════════════════════
//
// Uses Unicode chars to approximate a donut chart:
//
//	     ╭─────╮
//	   ╱ ░░░░░░░ ╲
//	  │  $1,428   │
//	  │   total   │
//	   ╲ ░░░░░░░ ╱
//	     ╰─────╯

func RenderDonutChart(items []chartItem, w int, centerLabel, centerValue string) string {
	if len(items) == 0 {
		return ""
	}

	total := float64(0)
	for _, item := range items {
		total += item.Value
	}
	if total == 0 {
		return ""
	}

	// Use a ring of characters
	// Ring radius in chars
	ringChars := 24
	if w < 50 {
		ringChars = 16
	}

	var sb strings.Builder

	// Build a simple ring representation using colored blocks
	// Top arc
	arcW := ringChars
	var arcParts []string
	assigned := 0
	for i, item := range items {
		pct := item.Value / total
		segW := int(math.Round(pct * float64(arcW)))
		if segW < 1 && item.Value > 0 {
			segW = 1
		}
		if i == len(items)-1 {
			segW = arcW - assigned
			if segW < 0 {
				segW = 0
			}
		}
		assigned += segW
		arcParts = append(arcParts, lipgloss.NewStyle().Foreground(item.Color).Render(strings.Repeat("█", segW)))
	}
	arc := strings.Join(arcParts, "")

	// Center block
	pad := (w - arcW) / 2
	if pad < 2 {
		pad = 2
	}
	padStr := strings.Repeat(" ", pad)

	sb.WriteString(padStr + "  ╭" + strings.Repeat("─", arcW) + "╮\n")
	sb.WriteString(padStr + "  │" + arc + "│\n")

	// Center value
	centerLine := centerValue
	if len(centerLine) > arcW-2 {
		centerLine = centerLine[:arcW-2]
	}
	centerPad := (arcW - lipgloss.Width(centerLine)) / 2
	if centerPad < 0 {
		centerPad = 0
	}
	sb.WriteString(padStr + "  │" +
		strings.Repeat(" ", centerPad) +
		lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(centerLine) +
		strings.Repeat(" ", arcW-centerPad-lipgloss.Width(centerLine)) +
		"│\n")

	// Center label
	labelLine := centerLabel
	if len(labelLine) > arcW-2 {
		labelLine = labelLine[:arcW-2]
	}
	labelPad := (arcW - lipgloss.Width(labelLine)) / 2
	if labelPad < 0 {
		labelPad = 0
	}
	sb.WriteString(padStr + "  │" +
		strings.Repeat(" ", labelPad) +
		dimStyle.Render(labelLine) +
		strings.Repeat(" ", arcW-labelPad-lipgloss.Width(labelLine)) +
		"│\n")

	// Bottom arc
	var arcParts2 []string
	assigned = 0
	for i, item := range items {
		pct := item.Value / total
		segW := int(math.Round(pct * float64(arcW)))
		if segW < 1 && item.Value > 0 {
			segW = 1
		}
		if i == len(items)-1 {
			segW = arcW - assigned
			if segW < 0 {
				segW = 0
			}
		}
		assigned += segW
		arcParts2 = append(arcParts2, lipgloss.NewStyle().Foreground(item.Color).Render(strings.Repeat("█", segW)))
	}
	arc2 := strings.Join(arcParts2, "")

	sb.WriteString(padStr + "  │" + arc2 + "│\n")
	sb.WriteString(padStr + "  ╰" + strings.Repeat("─", arcW) + "╯\n")

	// Legend to the right or below
	sb.WriteString("\n")
	for _, item := range items {
		pct := item.Value / total * 100
		dot := lipgloss.NewStyle().Foreground(item.Color).Render("██")
		name := item.Label
		if len(name) > 18 {
			name = name[:17] + "…"
		}
		cost := lipgloss.NewStyle().Foreground(item.Color).Bold(true).Render(formatUSD(item.Value))
		sb.WriteString(fmt.Sprintf("  %s %-18s %s  %s\n",
			dot, name, dimStyle.Render(fmt.Sprintf("%5.1f%%", pct)), cost))
	}

	return sb.String()
}

// ═══════════════════════════════════════════════════════════════════════════
// COMPARISON TABLE (side-by-side)
// ═══════════════════════════════════════════════════════════════════════════

type comparisonRow struct {
	Label  string
	Left   string
	Right  string
	LeftV  float64
	RightV float64
}

func RenderComparisonTable(leftTitle, rightTitle string, rows []comparisonRow, w int) string {
	if len(rows) == 0 {
		return ""
	}

	var sb strings.Builder

	labelW := 20
	colW := (w - labelW - 8) / 2
	if colW < 10 {
		colW = 10
	}

	// Headers
	sb.WriteString(fmt.Sprintf("  %-*s %*s  │  %-*s\n",
		labelW, "",
		colW, lipgloss.NewStyle().Foreground(colorSapphire).Bold(true).Render(leftTitle),
		colW, lipgloss.NewStyle().Foreground(colorPeach).Bold(true).Render(rightTitle)))
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(
		strings.Repeat("─", labelW+colW*2+5)) + "\n")

	for _, row := range rows {
		label := row.Label
		if len(label) > labelW-1 {
			label = label[:labelW-2] + "…"
		}

		leftStyle := lipgloss.NewStyle().Foreground(colorSubtext)
		rightStyle := lipgloss.NewStyle().Foreground(colorSubtext)

		// Highlight the larger value
		if row.LeftV > row.RightV && row.LeftV > 0 {
			leftStyle = leftStyle.Bold(true).Foreground(colorSapphire)
		} else if row.RightV > row.LeftV && row.RightV > 0 {
			rightStyle = rightStyle.Bold(true).Foreground(colorPeach)
		}

		sb.WriteString(fmt.Sprintf("  %-*s %*s  │  %-*s\n",
			labelW, dimStyle.Render(label),
			colW, leftStyle.Render(row.Left),
			colW, rightStyle.Render(row.Right)))
	}

	return sb.String()
}

// ═══════════════════════════════════════════════════════════════════════════
// RANKED LIST / LEADERBOARD
// ═══════════════════════════════════════════════════════════════════════════

func RenderLeaderboard(items []chartItem, w, maxShow int, title string) string {
	if len(items) == 0 {
		return ""
	}

	var sb strings.Builder
	if title != "" {
		sb.WriteString("  " + chartTitleStyle.Render(title) + "\n\n")
	}

	show := maxShow
	if show > len(items) {
		show = len(items)
	}

	// Find max for mini-bar
	maxVal := float64(0)
	for i := 0; i < show; i++ {
		if items[i].Value > maxVal {
			maxVal = items[i].Value
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	miniBarW := 16
	nameW := 30
	if nameW+miniBarW+30 > w {
		nameW = w - miniBarW - 30
		if nameW < 10 {
			nameW = 10
		}
	}

	for i := 0; i < show; i++ {
		item := items[i]

		// Rank medal
		var rankStr string
		switch i {
		case 0:
			rankStr = lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render("🥇")
		case 1:
			rankStr = lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("🥈")
		case 2:
			rankStr = lipgloss.NewStyle().Foreground(colorPeach).Bold(true).Render("🥉")
		default:
			rankStr = dimStyle.Render(fmt.Sprintf(" %d.", i+1))
		}

		name := item.Label
		if len(name) > nameW {
			name = name[:nameW-1] + "…"
		}
		nameRendered := lipgloss.NewStyle().Foreground(item.Color).Render(fmt.Sprintf("%-*s", nameW, name))

		// Mini bar
		barLen := int(item.Value / maxVal * float64(miniBarW))
		if barLen < 1 {
			barLen = 1
		}
		emptyLen := miniBarW - barLen
		bar := lipgloss.NewStyle().Foreground(item.Color).Render(strings.Repeat("█", barLen))
		track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", emptyLen))

		cost := lipgloss.NewStyle().Foreground(item.Color).Bold(true).Render(formatUSD(item.Value))

		line := fmt.Sprintf("  %s %s %s%s  %s", rankStr, nameRendered, bar, track, cost)
		if item.SubLabel != "" {
			line += "  " + dimStyle.Render(item.SubLabel)
		}

		sb.WriteString(line + "\n")
	}

	return sb.String()
}

// ═══════════════════════════════════════════════════════════════════════════
// PROGRESS RING (single metric)
// ═══════════════════════════════════════════════════════════════════════════
//
// A compact budget ring:
//
//	[████████████████░░░░░░░░░░░░] 39%  $1,421 / $3,600

func RenderProgressRing(label string, pct float64, used, limit float64, w int, color lipgloss.Color) string {
	barW := w - 30
	if barW < 8 {
		barW = 8
	}
	if barW > 40 {
		barW = 40
	}

	if pct > 100 {
		pct = 100
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

	pctStr := lipgloss.NewStyle().Foreground(barColor).Bold(true).Render(fmt.Sprintf("%.0f%%", pct))
	detail := dimStyle.Render(fmt.Sprintf("%s / %s", formatUSD(used), formatUSD(limit)))

	return fmt.Sprintf("  [%s%s] %s  %s", bar, track, pctStr, detail)
}

// ═══════════════════════════════════════════════════════════════════════════
// TOKEN BREAKDOWN MINI-CHART
// ═══════════════════════════════════════════════════════════════════════════
//
//	Input  ████████████████████████  2.4M tokens
//	Output ██████████                0.8M tokens

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

	maxVal := math.Max(input, output)
	if maxVal == 0 {
		maxVal = 1
	}

	// Input bar
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

	// Output bar
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

// ═══════════════════════════════════════════════════════════════════════════
// MULTI-SERIES TIME-SERIES LINE CHART
// ═══════════════════════════════════════════════════════════════════════════
//
// Renders a multi-series chart with a Y-axis, date X-axis, and legend:
//
//	  100 ┤          ●·····●
//	   75 ┤    ●····●      ○····○
//	   50 ┤   ○···○          ●
//	   25 ┤ ●                  ○
//	    0 ┤─────────────────────────
//	      Jan 1    Jan 15    Feb 1
//
//	● claude-code  ○ cursor-ide

// TimeSeriesLine represents one series in a multi-line chart.
type TimeSeriesLine struct {
	Label  string
	Points []TimeSeriesPoint
	Color  lipgloss.Color
}

// TimeSeriesPoint is one (date, value) point.
type TimeSeriesPoint struct {
	Date  string // "2025-01-15"
	Value float64
}

// RenderTimeSeriesChart draws a multi-series chart with Y-axis, date X-axis, and legend.
// chartW is the total available width; chartH is the number of Y-axis rows.
func RenderTimeSeriesChart(series []TimeSeriesLine, chartW, chartH int) string {
	if len(series) == 0 || chartW < 30 {
		return ""
	}
	if chartH < 5 {
		chartH = 5
	}
	if chartH > 20 {
		chartH = 20
	}

	// ── 1. Collect all unique dates and global min/max ──
	dateSet := make(map[string]bool)
	globalMax := float64(0)
	for _, s := range series {
		for _, p := range s.Points {
			dateSet[p.Date] = true
			if p.Value > globalMax {
				globalMax = p.Value
			}
		}
	}
	if globalMax == 0 {
		globalMax = 1
	}

	// Sort dates
	allDates := make([]string, 0, len(dateSet))
	for d := range dateSet {
		allDates = append(allDates, d)
	}
	sortStrings(allDates)

	if len(allDates) == 0 {
		return ""
	}

	// ── 2. Sizing ──
	yAxisW := 8 // width for Y-axis labels
	plotW := chartW - yAxisW - 4
	if plotW < 10 {
		plotW = 10
	}

	// Map dates to X positions (columns in the plot area)
	dateToX := make(map[string]int)
	if len(allDates) == 1 {
		dateToX[allDates[0]] = plotW / 2
	} else {
		for i, d := range allDates {
			dateToX[d] = int(float64(i) / float64(len(allDates)-1) * float64(plotW-1))
		}
	}

	// Build lookup for each series: X column → Y value
	type xyPoint struct {
		x, y int
	}
	seriesPoints := make([][]xyPoint, len(series))
	for si, s := range series {
		lookup := make(map[string]float64)
		for _, p := range s.Points {
			lookup[p.Date] = p.Value
		}
		for _, d := range allDates {
			if v, ok := lookup[d]; ok {
				x := dateToX[d]
				yRow := int(v / globalMax * float64(chartH-1))
				if yRow >= chartH {
					yRow = chartH - 1
				}
				seriesPoints[si] = append(seriesPoints[si], xyPoint{x: x, y: yRow})
			}
		}
	}

	// ── 3. Render grid ──
	// grid[row][col] = index of series (-1 = empty)
	// Row 0 = top (max value), Row chartH-1 = bottom (0 value)
	grid := make([][]int, chartH)
	for r := range grid {
		grid[r] = make([]int, plotW)
		for c := range grid[r] {
			grid[r][c] = -1
		}
	}

	// Also track connector segments between points
	type connector struct {
		col      int
		seriesID int
	}
	connectors := make(map[int]map[int]int) // row → col → seriesID

	for si, pts := range seriesPoints {
		// Place points
		for _, p := range pts {
			row := (chartH - 1) - p.y // flip: row 0 = top
			if row >= 0 && row < chartH && p.x >= 0 && p.x < plotW {
				grid[row][p.x] = si
			}
		}

		// Draw connections between adjacent points
		for i := 0; i < len(pts)-1; i++ {
			p1, p2 := pts[i], pts[i+1]
			r1 := (chartH - 1) - p1.y
			r2 := (chartH - 1) - p2.y

			// Interpolate between p1 and p2
			dx := p2.x - p1.x
			if dx <= 1 {
				continue
			}
			dy := float64(r2 - r1)
			for cx := p1.x + 1; cx < p2.x; cx++ {
				t := float64(cx-p1.x) / float64(dx)
				cr := r1 + int(math.Round(dy*t))
				if cr >= 0 && cr < chartH && cx >= 0 && cx < plotW {
					if grid[cr][cx] == -1 { // don't overwrite actual points
						if connectors[cr] == nil {
							connectors[cr] = make(map[int]int)
						}
						connectors[cr][cx] = si
					}
				}
			}
		}
	}

	// ── 4. Render output ──
	var sb strings.Builder

	// Series markers (different chars per series)
	markers := []string{"●", "◆", "■", "▲", "★", "◉", "⬟", "⬢"}
	connChars := []string{"·", "·", "·", "·", "·", "·", "·", "·"}

	for row := 0; row < chartH; row++ {
		// Y-axis label
		yVal := globalMax * float64(chartH-1-row) / float64(chartH-1)
		yLabel := formatChartValue(yVal)
		sb.WriteString(fmt.Sprintf("  %*s ┤", yAxisW-2, dimStyle.Render(yLabel)))

		// Plot columns
		for col := 0; col < plotW; col++ {
			si := grid[row][col]
			if si >= 0 && si < len(series) {
				mk := markers[si%len(markers)]
				sb.WriteString(lipgloss.NewStyle().Foreground(series[si].Color).Render(mk))
			} else if cSi, ok := connectors[row][col]; ok && cSi < len(series) {
				ch := connChars[cSi%len(connChars)]
				sb.WriteString(lipgloss.NewStyle().Foreground(series[cSi].Color).Render(ch))
			} else {
				sb.WriteString(" ")
			}
		}
		sb.WriteString("\n")
	}

	// X-axis line
	sb.WriteString(fmt.Sprintf("  %*s └", yAxisW-2, ""))
	sb.WriteString(strings.Repeat("─", plotW))
	sb.WriteString("\n")

	// X-axis date labels
	numLabels := 5
	if len(allDates) < numLabels {
		numLabels = len(allDates)
	}
	if numLabels > 0 {
		// Pick evenly spaced dates
		labelRow := make([]byte, plotW)
		for i := range labelRow {
			labelRow[i] = ' '
		}

		for i := 0; i < numLabels; i++ {
			var dateIdx int
			if numLabels == 1 {
				dateIdx = 0
			} else {
				dateIdx = i * (len(allDates) - 1) / (numLabels - 1)
			}
			d := allDates[dateIdx]
			label := formatDateLabel(d)
			x := dateToX[d]

			// Place label centered at x
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
			for j, ch := range []byte(label) {
				pos := start + j
				if pos >= 0 && pos < plotW {
					labelRow[pos] = ch
				}
			}
		}

		sb.WriteString(fmt.Sprintf("  %*s  ", yAxisW-2, ""))
		sb.WriteString(dimStyle.Render(string(labelRow)))
		sb.WriteString("\n")
	}

	// ── 5. Legend ──
	sb.WriteString("\n  ")
	for i, s := range series {
		if i > 0 {
			sb.WriteString("   ")
		}
		mk := markers[i%len(markers)]
		sb.WriteString(lipgloss.NewStyle().Foreground(s.Color).Render(mk))
		sb.WriteString(" ")
		sb.WriteString(dimStyle.Render(s.Label))
	}
	sb.WriteString("\n")

	return sb.String()
}

// formatChartValue formats a numeric value for Y-axis labels (compact).
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

// formatDateLabel formats a "2025-01-15" date string as "Jan 15".
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
	// Strip leading zero from day
	if day[0] == '0' {
		day = day[1:]
	}
	return month + " " + day
}

// sortStrings is a simple insertion sort for small string slices (avoids
// importing sort in this file).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
