package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type chartItem struct {
	Label     string
	Value     float64
	Color     lipgloss.Color
	ValueText string
	SubLabel  string
}

func RenderSparkline(values []float64, w int, color lipgloss.Color) string {
	return renderNTSparkline(values, w, color)
}

func RenderHBarChart(items []chartItem, maxBarW, labelW int) string {
	return renderNTHBarChart(items, maxBarW, labelW)
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
	barW := w - 30
	if barW < 8 {
		barW = 8
	}
	if barW > 30 {
		barW = 30
	}

	items := []chartItem{
		{
			Label:     "Input",
			Value:     input,
			Color:     colorSapphire,
			ValueText: dimStyle.Render(formatTokens(input) + " tok"),
		},
		{
			Label:     "Output",
			Value:     output,
			Color:     colorPeach,
			ValueText: dimStyle.Render(formatTokens(output) + " tok"),
		},
	}
	return renderNTHBarChart(items, barW, 8)
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
	Title             string
	Mode              TimeChartMode
	Series            []BrailleSeries
	Height            int
	MaxSeries         int
	WindowDays        int
	ReferenceTime     time.Time
	PreserveEmptySpan bool
	YFmt              func(float64) string
}

type HeatmapSpec struct {
	Title      string
	Rows       []string
	RowSummary []string
	Cols       []string
	Values     [][]float64 // [row][col]
	MaxCols    int
	RowColors  []lipgloss.Color
	RowScale   bool
}

func RenderBrailleChart(title string, series []BrailleSeries, w, h int, yFmt func(float64) string) string {
	return renderNTBrailleChart(title, series, w, h, yFmt)
}

func RenderTimeChart(spec TimeChartSpec, w int) string {
	return renderNTTimeChart(spec, w)
}

func seriesVolume(s BrailleSeries) float64 {
	total := 0.0
	for _, p := range s.Points {
		total += p.Value
	}
	return total
}

func cropSeriesToRecentDays(series []BrailleSeries, days int, reference time.Time) []BrailleSeries {
	if days <= 0 || len(series) == 0 {
		return series
	}
	out := make([]BrailleSeries, 0, len(series))
	for _, s := range series {
		pts := clipAndPadPointsByRecentDays(s.Points, days, reference)
		if len(pts) == 0 {
			continue
		}
		s.Points = pts
		out = append(out, s)
	}
	return out
}

func clipAndPadPointsByRecentDays(points []core.TimePoint, days int, reference time.Time) []core.TimePoint {
	if len(points) == 0 || days <= 0 {
		return points
	}
	if reference.IsZero() {
		reference = time.Now().UTC()
	}
	end := reference.UTC()
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -(days - 1))

	byDate := make(map[string]float64, len(points))
	for _, p := range points {
		t, err := time.Parse("2006-01-02", p.Date)
		if err != nil {
			continue
		}
		if t.Before(start) || t.After(end) {
			continue
		}
		byDate[p.Date] += p.Value
	}
	if len(byDate) == 0 {
		return nil
	}

	out := make([]core.TimePoint, 0, days)
	for day := 0; day < days; day++ {
		date := start.AddDate(0, 0, day).Format("2006-01-02")
		out = append(out, core.TimePoint{Date: date, Value: byDate[date]})
	}
	return out
}

func renderWrappedLegend(series []BrailleSeries, w int) string {
	if len(series) == 0 {
		return ""
	}
	markers := []string{"●", "◆", "■", "▲", "★"}
	// Adapt label truncation to available width.
	maxLabel := 26
	if w < 60 {
		maxLabel = 16
	} else if w < 40 {
		maxLabel = 10
	}
	parts := make([]string, 0, len(series))
	for i, s := range series {
		m := markers[i%len(markers)]
		part := lipgloss.NewStyle().Foreground(s.Color).Render(m) + " " + dimStyle.Render(truncStr(s.Label, maxLabel))
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
	dates := core.SortedStringKeys(dateSet)
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

		for si := range values {
			sum := 0.0
			row := values[si]
			for di := start; di < end && di < len(row); di++ {
				sum += row[di]
			}
			binned[si][col] = sum
		}
	}

	return labels, binned
}

func RenderHeatmap(spec HeatmapSpec, w int) string {
	return renderNTHeatmap(spec, w)
}
