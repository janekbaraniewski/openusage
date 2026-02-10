package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderGauge produces a text-based gauge bar of the given width.
// percent should be 0-100 (remaining). If < 0, renders a dimmed track with "N/A".
func RenderGauge(percent float64, width int, warnThresh, critThresh float64) string {
	if width < 5 {
		width = 5
	}

	if percent < 0 {
		return gaugeTrackStyle.Render(strings.Repeat("─", width)) + dimStyle.Render(" N/A")
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent / 100 * float64(width))
	empty := width - filled

	var color lipgloss.Color
	switch {
	case percent <= critThresh*100:
		color = colorCrit
	case percent <= warnThresh*100:
		color = colorWarn
	default:
		color = colorOK
	}

	filledStyle := lipgloss.NewStyle().Foreground(color)
	trackStyle := lipgloss.NewStyle().Foreground(colorSurface1)

	bar := filledStyle.Render(strings.Repeat("━", filled)) +
		trackStyle.Render(strings.Repeat("━", empty))

	pctStyle := lipgloss.NewStyle().Foreground(color).Bold(true)
	return fmt.Sprintf("%s %s", bar, pctStyle.Render(fmt.Sprintf("%5.1f%%", percent)))
}

// RenderUsageGauge produces a text-based gauge that fills from left to right
// as usage increases (0=empty, 100=full). Colors shift green→yellow→red.
// This is the inverse of RenderGauge which shows "remaining".
func RenderUsageGauge(usedPercent float64, width int, warnThresh, critThresh float64) string {
	if width < 5 {
		width = 5
	}

	if usedPercent < 0 {
		return gaugeTrackStyle.Render(strings.Repeat("─", width)) + dimStyle.Render(" N/A")
	}
	if usedPercent > 100 {
		usedPercent = 100
	}

	filled := int(usedPercent / 100 * float64(width))
	empty := width - filled

	// Color thresholds are expressed as "remaining" fractions (e.g. 0.1 = 10% remaining = crit).
	// Convert to "used" thresholds: crit when used >= (1-critThresh)*100
	var color lipgloss.Color
	switch {
	case usedPercent >= (1-critThresh)*100: // e.g. ≥ 90% used
		color = colorCrit
	case usedPercent >= (1-warnThresh)*100: // e.g. ≥ 70% used
		color = colorWarn
	default:
		color = colorOK
	}

	filledStyle := lipgloss.NewStyle().Foreground(color)
	trackStyle := lipgloss.NewStyle().Foreground(colorSurface1)

	bar := filledStyle.Render(strings.Repeat("━", filled)) +
		trackStyle.Render(strings.Repeat("━", empty))

	pctStyle := lipgloss.NewStyle().Foreground(color).Bold(true)
	return fmt.Sprintf("%s %s", bar, pctStyle.Render(fmt.Sprintf("%5.1f%%", usedPercent)))
}

// RenderMiniGauge produces a compact inline gauge (no percentage label).
func RenderMiniGauge(percent float64, width int) string {
	if width < 3 {
		width = 3
	}
	if percent < 0 {
		return lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("━", width))
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent / 100 * float64(width))
	empty := width - filled

	var color lipgloss.Color
	switch {
	case percent >= 80:
		color = colorOK
	case percent >= 20:
		color = colorWarn
	default:
		color = colorCrit
	}

	filledStyle := lipgloss.NewStyle().Foreground(color)
	trackStyle := lipgloss.NewStyle().Foreground(colorSurface1)
	return filledStyle.Render(strings.Repeat("━", filled)) +
		trackStyle.Render(strings.Repeat("━", empty))
}
