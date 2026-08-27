package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/samber/lo"
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

// RenderUsageGaugeWithProjection renders a usage gauge with an optional dim
// annotation line below it showing time-until-reset and/or projected time to
// 100% based on the supplied pace.
//
// paceFraction is the fraction of the window consumed per minute (so 0.005
// means 0.5% per minute). The caller computes it from current% and elapsed.
// resetIn is the time remaining until the window resets; zero or negative
// values suppress the reset half of the annotation.
//
// The annotation is skipped (plain gauge returned) when:
//   - paceFraction is NaN, ±Inf, or <= 0
//   - usedPercent >= 100 (already at limit, projection is meaningless)
//
// If only one of {reset, projection} is meaningful, only that piece renders.
//
// When the projected time to 100% exceeds the time remaining in the window,
// the projection half switches from "projected 100% in X" to "projected
// ~N% by reset", where N is the linearly extrapolated percent at reset
// (capped at 99 so the wording never contradicts the branch).
func RenderUsageGaugeWithProjection(usedPercent float64, width int, warnThresh, critThresh float64, paceFraction float64, resetIn time.Duration) string {
	gauge := RenderUsageGauge(usedPercent, width, warnThresh, critThresh)

	resetPart := ""
	if resetIn > 0 {
		resetPart = "resets in " + formatDurationShort(resetIn)
	}

	projPart := ""
	paceValid := !math.IsNaN(paceFraction) && !math.IsInf(paceFraction, 0) && paceFraction > 0
	if paceValid && usedPercent < 100 {
		remainingPct := 100 - usedPercent
		// paceFraction is fraction-per-minute, convert to %-per-minute.
		pctPerMinute := paceFraction * 100
		if pctPerMinute > 0 {
			minutesTo100 := remainingPct / pctPerMinute
			d := time.Duration(minutesTo100 * float64(time.Minute))
			if d > 0 {
				// If the window will reset before we project hitting 100%,
				// show the projected percent at reset instead of an
				// impossible "100% in X" claim that overshoots the window.
				if resetIn > 0 && d > resetIn {
					projectedPct := usedPercent + pctPerMinute*resetIn.Minutes()
					n := int(math.Round(projectedPct))
					if n < 0 {
						n = 0
					}
					// Cap below 100 so this branch never contradicts itself
					// by claiming the projection somehow lands at 100%.
					if n >= 100 {
						n = 99
					}
					projPart = fmt.Sprintf("projected ~%d%% by reset", n)
				} else {
					projPart = "projected 100% in " + formatDurationShort(d)
				}
			}
		}
	}

	annotation := joinAnnotationParts(resetPart, projPart)
	if annotation == "" {
		return gauge
	}
	return gauge + "\n" + dimStyle.Render(annotation)
}

// joinAnnotationParts joins non-empty annotation fragments with " · ", dropping
// any empty entries. Used by gauge renderers that compose a dim
// "resets … · projected …" line from independently-computed parts.
func joinAnnotationParts(parts ...string) string {
	return strings.Join(lo.Compact(parts), " · ")
}

// gaugeWindowDuration returns the duration of a windowed metric (5h / 7d / 1d
// / 30d) so projection math can compute elapsed time. Returns (0, false) when
// the window string isn't recognized — in that case we render the plain gauge.
func gaugeWindowDuration(window string) (time.Duration, bool) {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "5h", "rolling-5h":
		return 5 * time.Hour, true
	case "1d", "24h", "today":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	case "30d", "month":
		return 30 * 24 * time.Hour, true
	}
	return 0, false
}

// formatDurationShort renders a duration as a compact human string like
// "2d 3h", "1h 23m", "42m", or "5s". Used by gauge projections / reset
// countdowns.
func formatDurationShort(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	if d >= 24*time.Hour {
		days := int(d / (24 * time.Hour))
		hours := int((d % (24 * time.Hour)) / time.Hour)
		if hours == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if d >= time.Hour {
		h := int(d / time.Hour)
		m := int((d % time.Hour) / time.Minute)
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
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
