package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func renderVerticalScrollBarLine(width, offset, visible, total int) string {
	return renderScrollBarLine(width, offset, visible, total, "vertical")
}

func renderHorizontalScrollBarLine(width, offset, visible, total int) string {
	return renderScrollBarLine(width, offset, visible, total, "horizontal")
}

func renderScrollBarLine(width, offset, visible, total int, axis string) string {
	if width <= 0 || visible <= 0 || total <= 0 || total <= visible {
		return ""
	}

	maxOffset := total - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset = clamp(offset, 0, maxOffset)

	prefix := "  ↕ "
	startArrow := "▲"
	endArrow := "▼"
	if axis == "horizontal" {
		prefix = "  ↔ "
		startArrow = "◀"
		endArrow = "▶"
	}

	trackW := width - lipgloss.Width(prefix) - 2
	if trackW < 6 {
		msg := fmt.Sprintf("%s%d/%d", prefix, offset, maxOffset)
		return fitAnsiWidth(msg, width)
	}

	thumbW := int(math.Round((float64(visible) / float64(total)) * float64(trackW)))
	if thumbW < 1 {
		thumbW = 1
	}
	if thumbW > trackW {
		thumbW = trackW
	}

	thumbPos := 0
	if maxOffset > 0 && trackW > thumbW {
		thumbPos = int(math.Round((float64(offset) / float64(maxOffset)) * float64(trackW-thumbW)))
	}

	railStyle := lipgloss.NewStyle().Foreground(colorSurface1)
	thumbStyle := lipgloss.NewStyle().Foreground(colorAccent)
	arrowStyle := lipgloss.NewStyle().Foreground(colorDim)

	leftRail := strings.Repeat("─", thumbPos)
	thumb := strings.Repeat("━", thumbW)
	rightRail := strings.Repeat("─", trackW-thumbPos-thumbW)

	line := prefix +
		arrowStyle.Render(startArrow) +
		railStyle.Render(leftRail) +
		thumbStyle.Render(thumb) +
		railStyle.Render(rightRail) +
		arrowStyle.Render(endArrow)

	return fitAnsiWidth(line, width)
}

func fitAnsiWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	out := ansi.Cut(s, 0, width)
	if pad := width - lipgloss.Width(out); pad > 0 {
		out += strings.Repeat(" ", pad)
	}
	return out
}
