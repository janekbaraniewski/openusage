package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var blockChars = []string{" ", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

func gaugeColor(percent, warnThresh, critThresh float64) lipgloss.Color {
	switch {
	case percent <= critThresh*100:
		return colorCrit
	case percent <= warnThresh*100:
		return colorWarn
	default:
		return colorOK
	}
}

func usageGaugeColor(usedPercent, warnThresh, critThresh float64) lipgloss.Color {
	switch {
	case usedPercent >= (1-critThresh)*100:
		return colorCrit
	case usedPercent >= (1-warnThresh)*100:
		return colorWarn
	default:
		return colorOK
	}
}

func RenderGauge(percent float64, width int, warnThresh, critThresh float64) string {
	if width < 5 {
		width = 5
	}

	if percent < 0 {
		track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", width))
		return track + dimStyle.Render(" N/A")
	}
	if percent > 100 {
		percent = 100
	}

	color := gaugeColor(percent, warnThresh, critThresh)
	filledStyle := lipgloss.NewStyle().Foreground(color)
	trackStyle := lipgloss.NewStyle().Foreground(colorSurface1)

	totalUnits := width * 8
	fillUnits := int(percent / 100 * float64(totalUnits))

	fullCells := fillUnits / 8
	remainder := fillUnits % 8
	hasPartial := remainder > 0
	emptyCells := width - fullCells
	if hasPartial {
		emptyCells--
	}
	if emptyCells < 0 {
		emptyCells = 0
	}

	var b strings.Builder
	b.WriteString(filledStyle.Render(strings.Repeat("█", fullCells)))
	if hasPartial {
		b.WriteString(filledStyle.Render(blockChars[remainder]))
	}
	b.WriteString(trackStyle.Render(strings.Repeat("░", emptyCells)))

	pctStyle := lipgloss.NewStyle().Foreground(color).Bold(true)
	return fmt.Sprintf("%s %s", b.String(), pctStyle.Render(fmt.Sprintf("%5.1f%%", percent)))
}

func RenderUsageGauge(usedPercent float64, width int, warnThresh, critThresh float64) string {
	if width < 5 {
		width = 5
	}

	if usedPercent < 0 {
		track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", width))
		return track + dimStyle.Render(" N/A")
	}
	if usedPercent > 100 {
		usedPercent = 100
	}

	color := usageGaugeColor(usedPercent, warnThresh, critThresh)
	filledStyle := lipgloss.NewStyle().Foreground(color)
	trackStyle := lipgloss.NewStyle().Foreground(colorSurface1)

	totalUnits := width * 8
	fillUnits := int(usedPercent / 100 * float64(totalUnits))

	fullCells := fillUnits / 8
	remainder := fillUnits % 8
	hasPartial := remainder > 0
	emptyCells := width - fullCells
	if hasPartial {
		emptyCells--
	}
	if emptyCells < 0 {
		emptyCells = 0
	}

	var b strings.Builder
	b.WriteString(filledStyle.Render(strings.Repeat("█", fullCells)))
	if hasPartial {
		b.WriteString(filledStyle.Render(blockChars[remainder]))
	}
	b.WriteString(trackStyle.Render(strings.Repeat("░", emptyCells)))

	pctStyle := lipgloss.NewStyle().Foreground(color).Bold(true)
	return fmt.Sprintf("%s %s", b.String(), pctStyle.Render(fmt.Sprintf("%5.1f%%", usedPercent)))
}

func RenderMiniGauge(usedPercent float64, width int) string {
	if width < 3 {
		width = 3
	}
	if usedPercent < 0 {
		return lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", width))
	}
	if usedPercent > 100 {
		usedPercent = 100
	}

	var color lipgloss.Color
	switch {
	case usedPercent >= 80:
		color = colorCrit
	case usedPercent >= 50:
		color = colorWarn
	default:
		color = colorOK
	}
	percent := usedPercent

	filledStyle := lipgloss.NewStyle().Foreground(color)
	trackStyle := lipgloss.NewStyle().Foreground(colorSurface1)

	totalUnits := width * 8
	fillUnits := int(percent / 100 * float64(totalUnits))

	fullCells := fillUnits / 8
	remainder := fillUnits % 8
	hasPartial := remainder > 0
	emptyCells := width - fullCells
	if hasPartial {
		emptyCells--
	}
	if emptyCells < 0 {
		emptyCells = 0
	}

	var b strings.Builder
	b.WriteString(filledStyle.Render(strings.Repeat("█", fullCells)))
	if hasPartial {
		b.WriteString(filledStyle.Render(blockChars[remainder]))
	}
	b.WriteString(trackStyle.Render(strings.Repeat("░", emptyCells)))
	return b.String()
}

// GaugeSegment represents one colored segment of a stacked gauge bar.
type GaugeSegment struct {
	Percent float64
	Color   lipgloss.Color
}

// RenderStackedUsageGauge draws a multi-segment usage gauge bar.
// Each segment occupies a proportional share of the filled area.
// totalPercent is the overall usage percentage shown in the label.
func RenderStackedUsageGauge(segments []GaugeSegment, totalPercent float64, width int) string {
	if width < 5 {
		width = 5
	}

	if totalPercent < 0 {
		track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", width))
		return track + dimStyle.Render(" N/A")
	}
	if totalPercent > 100 {
		totalPercent = 100
	}

	totalUnits := width * 8
	fillUnits := int(totalPercent / 100 * float64(totalUnits))

	// Distribute fill units across segments proportionally.
	segUnits := make([]int, len(segments))
	if totalPercent > 0 {
		assigned := 0
		for i, seg := range segments {
			segUnits[i] = int(seg.Percent / totalPercent * float64(fillUnits))
			assigned += segUnits[i]
		}
		// Assign rounding remainder to the last segment.
		if len(segUnits) > 0 {
			segUnits[len(segUnits)-1] += fillUnits - assigned
		}
	}

	trackStyle := lipgloss.NewStyle().Foreground(colorSurface1)

	var b strings.Builder
	usedCells := 0
	for i, units := range segUnits {
		if units <= 0 {
			continue
		}
		style := lipgloss.NewStyle().Foreground(segments[i].Color)
		fullCells := units / 8
		remainder := units % 8
		b.WriteString(style.Render(strings.Repeat("█", fullCells)))
		usedCells += fullCells
		if remainder > 0 {
			b.WriteString(style.Render(blockChars[remainder]))
			usedCells++
		}
	}

	emptyCells := width - usedCells
	if emptyCells < 0 {
		emptyCells = 0
	}
	b.WriteString(trackStyle.Render(strings.Repeat("░", emptyCells)))

	const warnThresh = 0.30
	const critThresh = 0.15
	color := usageGaugeColor(totalPercent, warnThresh, critThresh)
	pctStyle := lipgloss.NewStyle().Foreground(color).Bold(true)
	return fmt.Sprintf("%s %s", b.String(), pctStyle.Render(fmt.Sprintf("%5.1f%%", totalPercent)))
}

func RenderGradientGauge(percent float64, width int, colors []lipgloss.Color) string {
	if width < 5 {
		width = 5
	}
	if percent < 0 || len(colors) == 0 {
		return lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", width))
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent / 100 * float64(width))
	empty := width - filled
	if empty < 0 {
		empty = 0
	}

	trackStyle := lipgloss.NewStyle().Foreground(colorSurface1)

	var b strings.Builder
	for i := 0; i < filled; i++ {
		ci := i * len(colors) / (filled + 1)
		if ci >= len(colors) {
			ci = len(colors) - 1
		}
		b.WriteString(lipgloss.NewStyle().Foreground(colors[ci]).Render("█"))
	}
	b.WriteString(trackStyle.Render(strings.Repeat("░", empty)))
	return b.String()
}
