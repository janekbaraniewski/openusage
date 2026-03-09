package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func renderTokensSection(sb *strings.Builder, snap core.UsageSnapshot, entries []metricEntry, widget core.DashboardWidget, w int, series map[string][]core.TimePoint) {
	labelW := sectionLabelWidth(w)

	var otherTokens []metricEntry
	for _, entry := range entries {
		if !isPerModelTokenKey(entry.key) {
			otherTokens = append(otherTokens, entry)
		}
	}

	for _, entry := range otherTokens {
		val := formatMetricValue(entry.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(labelW).Render(entry.label), valueStyle.Render(val)))
	}

	models := core.ExtractAnalyticsModelUsage(snap)
	hasPerModelTokens := false
	for _, model := range models {
		if model.InputTokens > 0 || model.OutputTokens > 0 {
			hasPerModelTokens = true
			break
		}
	}
	if hasPerModelTokens {
		if len(otherTokens) > 0 {
			sb.WriteString("\n")
		}
		renderTokenUsageTable(sb, models, w)
	}

	renderSectionSparklines(sb, widget, w, series, []string{
		"tokens_total", "tokens_input", "tokens_output",
	})
}

func isPerModelTokenKey(key string) bool {
	return core.IsPerModelTokenMetricKey(key)
}

func renderTokenUsageTable(sb *strings.Builder, models []core.AnalyticsModelUsageEntry, w int) {
	rows := make([]core.AnalyticsModelUsageEntry, 0, len(models))
	for _, model := range models {
		if model.InputTokens <= 0 && model.OutputTokens <= 0 {
			continue
		}
		rows = append(rows, model)
	}
	if len(rows) == 0 {
		return
	}

	nameW := 26
	colW := 10
	if w < 55 {
		nameW = 18
		colW = 8
	}

	sb.WriteString(fmt.Sprintf("  %-*s %*s %*s\n",
		nameW, dimStyle.Bold(true).Render("Model"),
		colW, dimStyle.Bold(true).Render("Input"),
		colW, dimStyle.Bold(true).Render("Output"),
	))

	for _, model := range rows {
		displayName := prettifyModelName(model.Name)
		if len(displayName) > nameW {
			displayName = displayName[:nameW-1] + "…"
		}
		sb.WriteString(fmt.Sprintf("  %-*s %*s %*s\n",
			nameW, valueStyle.Render(displayName),
			colW, lipgloss.NewStyle().Foreground(colorSubtext).Render(formatTokens(model.InputTokens)),
			colW, lipgloss.NewStyle().Foreground(colorSubtext).Render(formatTokens(model.OutputTokens)),
		))
	}
}
