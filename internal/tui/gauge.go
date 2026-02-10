package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ─── Block Characters for Sub-cell Precision ────────────────────────────────
// These horizontal-fill block elements give 8 levels of fill per character,
// creating btop-style smooth gauges.
var blockChars = []string{" ", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// gaugeColor returns the appropriate color for a "remaining" percentage gauge.
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

// usageGaugeColor returns the appropriate color for a "used" percentage gauge.
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

// ═══════════════════════════════════════════════════════════════════════════════
// SMOOTH GAUGES (primary — block elements for sub-character precision)
// ═══════════════════════════════════════════════════════════════════════════════

// RenderGauge produces a smooth block-element gauge bar.
// percent should be 0-100 (remaining). If < 0, renders a dimmed track with "N/A".
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

	// Sub-character precision: each cell = 8 units
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

// RenderUsageGauge produces a smooth gauge that fills from left to right
// as usage increases (0=empty, 100=full). Colors shift green→yellow→red.
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

// ═══════════════════════════════════════════════════════════════════════════════
// MINI GAUGES (compact inline)
// ═══════════════════════════════════════════════════════════════════════════════

// RenderMiniGauge produces a compact inline gauge (no percentage label).
func RenderMiniGauge(percent float64, width int) string {
	if width < 3 {
		width = 3
	}
	if percent < 0 {
		return lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", width))
	}
	if percent > 100 {
		percent = 100
	}

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

// ═══════════════════════════════════════════════════════════════════════════════
// GRADIENT GAUGE (for special emphasis)
// ═══════════════════════════════════════════════════════════════════════════════

// RenderGradientGauge renders a gauge with a color gradient across the filled portion.
// Used for hero metrics and analytics summaries.
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
