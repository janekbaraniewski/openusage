package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func metricLabel(widget core.DashboardWidget, key string) string {
	return core.MetricLabel(widget, key)
}

func renderTimersSection(sb *strings.Builder, resets map[string]time.Time, widget core.DashboardWidget, w int) {
	labelW := sectionLabelWidth(w)
	renderDetailSectionHeader(sb, "Timers", w)

	for _, key := range core.SortedStringKeys(resets) {
		resetAt := resets[key]
		label := metricLabel(widget, key)
		remaining := time.Until(resetAt)
		dateStr := resetAt.Format("Jan 02 15:04")

		if remaining <= 0 {
			sb.WriteString(fmt.Sprintf("  %s  %s  %s (expired)\n",
				dimStyle.Render("○"),
				labelStyle.Width(labelW).Render(label),
				dimStyle.Render(dateStr),
			))
			continue
		}

		urgency := lipgloss.NewStyle().Foreground(colorOK).Render("●")
		switch {
		case remaining < 15*time.Minute:
			urgency = lipgloss.NewStyle().Foreground(colorCrit).Render("●")
		case remaining < time.Hour:
			urgency = lipgloss.NewStyle().Foreground(colorWarn).Render("●")
		}
		sb.WriteString(fmt.Sprintf("  %s  %s  %s (in %s)\n",
			urgency,
			labelStyle.Width(labelW).Render(label),
			valueStyle.Render(dateStr),
			tealStyle.Render(formatDuration(remaining)),
		))
	}
}

func sectionLabelWidth(w int) int {
	switch {
	case w < 45:
		return 14
	case w < 55:
		return 18
	default:
		return 22
	}
}
