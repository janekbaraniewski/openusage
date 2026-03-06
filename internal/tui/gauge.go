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

// renderGaugeBar draws a sub-cell-accurate gauge bar and returns the bar string.
// percent must be in [0, 100]. width is the bar width in terminal columns.
func renderGaugeBar(percent float64, width int, color lipgloss.Color) string {
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

func renderGaugeWithLabel(percent float64, width int, color lipgloss.Color) string {
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
	bar := renderGaugeBar(percent, width, color)
	pctStyle := lipgloss.NewStyle().Foreground(color).Bold(true)
	return fmt.Sprintf("%s %s", bar, pctStyle.Render(fmt.Sprintf("%5.1f%%", percent)))
}

func RenderGauge(percent float64, width int, warnThresh, critThresh float64) string {
	color := gaugeColor(percent, warnThresh, critThresh)
	return renderGaugeWithLabel(percent, width, color)
}

func RenderUsageGauge(usedPercent float64, width int, warnThresh, critThresh float64) string {
	color := usageGaugeColor(usedPercent, warnThresh, critThresh)
	return renderGaugeWithLabel(usedPercent, width, color)
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
	return renderGaugeBar(usedPercent, width, color)
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

	// Find the last non-empty segment index so we can avoid partial block
	// characters between segments (they leave visible gaps because the
	// unfilled part of the cell shows the terminal background).
	lastFilledIdx := -1
	for i := len(segUnits) - 1; i >= 0; i-- {
		if segUnits[i] > 0 {
			lastFilledIdx = i
			break
		}
	}

	var b strings.Builder
	usedCells := 0
	for i, units := range segUnits {
		if units <= 0 {
			continue
		}
		style := lipgloss.NewStyle().Foreground(segments[i].Color)
		fullCells := units / 8
		remainder := units % 8
		if i != lastFilledIdx && remainder > 0 {
			fullCells++
			remainder = 0
		}
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

// RenderShimmerGauge draws an animated empty gauge track with a moving bright
// spot, used as a loading placeholder before real data arrives.
func RenderShimmerGauge(width, frame int) string {
	if width < 5 {
		width = 5
	}

	trackStyle := lipgloss.NewStyle().Foreground(colorSurface1)
	shimmerStyle := lipgloss.NewStyle().Foreground(colorSurface2)

	// The shimmer is a 3-char bright spot that scrolls across the track.
	shimmerW := 3
	cycle := width + shimmerW
	pos := frame % cycle

	var b strings.Builder
	for i := 0; i < width; i++ {
		dist := i - (pos - shimmerW)
		if dist >= 0 && dist < shimmerW {
			b.WriteString(shimmerStyle.Render("░"))
		} else {
			b.WriteString(trackStyle.Render("░"))
		}
	}

	return b.String() + dimStyle.Render("   ···")
}
