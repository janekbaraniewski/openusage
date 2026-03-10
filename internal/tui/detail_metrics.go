package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type metricGroup struct {
	title   string
	entries []metricEntry
	order   int
}

type metricEntry struct {
	key    string
	label  string
	metric core.Metric
}

func groupMetrics(metrics map[string]core.Metric, widget core.DashboardWidget, details core.DetailWidget) []metricGroup {
	groups := make(map[string]*metricGroup)

	for key, m := range metrics {
		if !core.IncludeDetailMetricKey(key) {
			continue
		}
		groupName, label, order := classifyMetric(key, m, widget, details)
		g, ok := groups[groupName]
		if !ok {
			g = &metricGroup{title: groupName, order: order}
			groups[groupName] = g
		}
		g.entries = append(g.entries, metricEntry{key: key, label: label, metric: m})
	}

	result := make([]metricGroup, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g.entries, func(i, j int) bool {
			return g.entries[i].key < g.entries[j].key
		})
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].order != result[j].order {
			return result[i].order < result[j].order
		}
		return result[i].title < result[j].title
	})

	return result
}

func classifyMetric(key string, m core.Metric, widget core.DashboardWidget, details core.DetailWidget) (group, label string, order int) {
	return core.ClassifyDetailMetric(key, m, widget, details)
}

func metricLabel(widget core.DashboardWidget, key string) string {
	return core.MetricLabel(widget, key)
}

func renderMetricGroup(sb *strings.Builder, snap core.UsageSnapshot, group metricGroup, widget core.DashboardWidget, details core.DetailWidget, w int, warnThresh, critThresh float64, series map[string][]core.TimePoint, burnRate float64) {
	sb.WriteString("\n")
	renderDetailSectionHeader(sb, group.title, w)

	entries := group.entries
	if widget.SuppressZeroNonUsageMetrics || len(widget.SuppressZeroMetricKeys) > 0 {
		entries = filterNonZeroEntries(entries, widget)
	}

	switch details.SectionStyle(group.title) {
	case core.DetailSectionStyleUsage:
		renderUsageSection(sb, entries, w, warnThresh, critThresh)
	case core.DetailSectionStyleSpending:
		renderSpendingSection(sb, entries, w, burnRate)
	case core.DetailSectionStyleTokens:
		renderTokensSection(sb, snap, entries, widget, w, series)
	case core.DetailSectionStyleActivity:
		renderActivitySection(sb, entries, widget, w, series)
	case core.DetailSectionStyleLanguages:
		renderListSection(sb, entries, w)
	default:
		renderListSection(sb, entries, w)
	}
}

func renderListSection(sb *strings.Builder, entries []metricEntry, w int) {
	labelW := sectionLabelWidth(w)
	for _, e := range entries {
		val := formatMetricValue(e.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(labelW).Render(e.label), valueStyle.Render(val)))
	}
}

func renderUsageSection(sb *strings.Builder, entries []metricEntry, w int, warnThresh, critThresh float64) {
	labelW := sectionLabelWidth(w)

	var usageEntries []metricEntry
	var gaugeEntries []metricEntry

	for _, e := range entries {
		m := e.metric
		if m.Remaining != nil && m.Limit != nil && m.Unit != "%" && m.Unit != "USD" {
			usageEntries = append(usageEntries, e)
		} else {
			gaugeEntries = append(gaugeEntries, e)
		}
	}

	for _, entry := range gaugeEntries {
		renderGaugeEntry(sb, entry, labelW, w, warnThresh, critThresh)
	}

	if len(usageEntries) > 0 {
		if len(gaugeEntries) > 0 {
			sb.WriteString("\n")
		}
		renderUsageTable(sb, usageEntries, w, warnThresh, critThresh)
	}
}

func renderSpendingSection(sb *strings.Builder, entries []metricEntry, w int, burnRate float64) {
	labelW := sectionLabelWidth(w)
	gaugeW := sectionGaugeWidth(w, labelW)

	var modelCosts []metricEntry
	var otherCosts []metricEntry

	for _, e := range entries {
		if isModelCostKey(e.key) {
			modelCosts = append(modelCosts, e)
		} else {
			otherCosts = append(otherCosts, e)
		}
	}

	for _, e := range otherCosts {
		if e.metric.Used != nil && e.metric.Limit != nil && *e.metric.Limit > 0 {
			color := colorTeal
			if *e.metric.Used >= *e.metric.Limit*0.8 {
				color = colorRed
			} else if *e.metric.Used >= *e.metric.Limit*0.5 {
				color = colorYellow
			}
			line := RenderBudgetGauge(e.label, *e.metric.Used, *e.metric.Limit, gaugeW, labelW, color, burnRate)
			sb.WriteString(line + "\n")
		} else {
			val := formatMetricValue(e.metric)
			vs := metricValueStyle
			if !strings.Contains(val, "$") && !strings.Contains(val, "USD") {
				vs = valueStyle
			}
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				labelStyle.Width(labelW).Render(e.label), vs.Render(val)))
		}
	}

	if len(modelCosts) > 0 {
		if len(otherCosts) > 0 {
			sb.WriteString("\n")
		}
		renderModelCostsTable(sb, modelCosts, w)
	}
}

func renderActivitySection(sb *strings.Builder, entries []metricEntry, widget core.DashboardWidget, w int, series map[string][]core.TimePoint) {
	labelW := sectionLabelWidth(w)

	for _, e := range entries {
		val := formatMetricValue(e.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(labelW).Render(e.label), valueStyle.Render(val)))
	}

	renderSectionSparklines(sb, widget, w, series, []string{
		"messages", "sessions", "tool_calls",
	})
}

func renderTimersSection(sb *strings.Builder, resets map[string]time.Time, widget core.DashboardWidget, w int) {
	labelW := sectionLabelWidth(w)
	renderDetailSectionHeader(sb, "Timers", w)

	timerKeys := core.SortedStringKeys(resets)

	for _, k := range timerKeys {
		t := resets[k]
		label := metricLabel(widget, k)
		remaining := time.Until(t)
		dateStr := t.Format("Jan 02 15:04")

		var urgency string
		if remaining <= 0 {
			urgency = dimStyle.Render("○")
			sb.WriteString(fmt.Sprintf("  %s  %s  %s (expired)\n",
				urgency,
				labelStyle.Width(labelW).Render(label),
				dimStyle.Render(dateStr),
			))
		} else {
			switch {
			case remaining < 15*time.Minute:
				urgency = lipgloss.NewStyle().Foreground(colorCrit).Render("●")
			case remaining < time.Hour:
				urgency = lipgloss.NewStyle().Foreground(colorWarn).Render("●")
			default:
				urgency = lipgloss.NewStyle().Foreground(colorOK).Render("●")
			}
			sb.WriteString(fmt.Sprintf("  %s  %s  %s (in %s)\n",
				urgency,
				labelStyle.Width(labelW).Render(label),
				valueStyle.Render(dateStr),
				tealStyle.Render(formatDuration(remaining)),
			))
		}
	}
}

func renderSectionSparklines(sb *strings.Builder, widget core.DashboardWidget, w int, series map[string][]core.TimePoint, candidates []string) {
	if len(series) == 0 {
		return
	}

	sparkW := w - 8
	if sparkW < 12 {
		sparkW = 12
	}
	if sparkW > 60 {
		sparkW = 60
	}

	colors := []lipgloss.Color{colorTeal, colorSapphire, colorGreen, colorPeach}
	colorIdx := 0

	for _, key := range candidates {
		points, ok := series[key]
		if !ok || len(points) < 2 {
			continue
		}
		values := make([]float64, len(points))
		for i, p := range points {
			values[i] = p.Value
		}
		c := colors[colorIdx%len(colors)]
		colorIdx++
		spark := RenderSparkline(values, sparkW, c)
		label := metricLabel(widget, key)
		sb.WriteString(fmt.Sprintf("  %s %s\n", dimStyle.Render(label), spark))
	}

	rendered := make(map[string]bool)
	for _, c := range candidates {
		rendered[c] = true
	}

	for _, candidate := range candidates {
		prefix := candidate
		if !strings.HasSuffix(prefix, "_") {
			prefix += "_"
		}
		for key, points := range series {
			if rendered[key] || len(points) < 2 {
				continue
			}
			if strings.HasPrefix(key, prefix) {
				rendered[key] = true
				values := make([]float64, len(points))
				for i, p := range points {
					values[i] = p.Value
				}
				c := colors[colorIdx%len(colors)]
				colorIdx++
				spark := RenderSparkline(values, sparkW, c)
				label := metricLabel(widget, key)
				sb.WriteString(fmt.Sprintf("  %s %s\n", dimStyle.Render(label), spark))
			}
		}
	}
}

func filterNonZeroEntries(entries []metricEntry, widget core.DashboardWidget) []metricEntry {
	suppressKeys := make(map[string]bool, len(widget.SuppressZeroMetricKeys))
	for _, k := range widget.SuppressZeroMetricKeys {
		suppressKeys[k] = true
	}

	var result []metricEntry
	for _, e := range entries {
		m := e.metric
		isZero := (m.Used == nil || *m.Used == 0) &&
			(m.Remaining == nil || *m.Remaining == 0) &&
			(m.Limit == nil || *m.Limit == 0)

		if isZero {
			if widget.SuppressZeroNonUsageMetrics && m.Limit == nil {
				continue
			}
			if suppressKeys[e.key] {
				continue
			}
		}
		result = append(result, e)
	}
	return result
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

func sectionGaugeWidth(w, labelW int) int {
	gw := w - labelW - 14
	if gw < 8 {
		gw = 8
	}
	if gw > 28 {
		gw = 28
	}
	return gw
}

func renderGaugeEntry(sb *strings.Builder, entry metricEntry, labelW, w int, warnThresh, critThresh float64) {
	m := entry.metric
	labelRendered := labelStyle.Width(labelW).Render(entry.label)
	gaugeW := sectionGaugeWidth(w, labelW)

	if m.Unit == "%" && m.Used != nil {
		gauge := RenderUsageGauge(*m.Used, gaugeW, warnThresh, critThresh)
		sb.WriteString(fmt.Sprintf("  %s %s\n", labelRendered, gauge))
		if detail := formatUsageDetail(m); detail != "" {
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				strings.Repeat(" ", labelW+2), dimStyle.Render(detail)))
		}
		return
	}

	if pct := m.Percent(); pct >= 0 {
		gauge := RenderGauge(pct, gaugeW, warnThresh, critThresh)
		sb.WriteString(fmt.Sprintf("  %s %s\n", labelRendered, gauge))
		if detail := formatMetricDetail(m); detail != "" {
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				strings.Repeat(" ", labelW+2), dimStyle.Render(detail)))
		}
		return
	}

	val := formatMetricValue(m)
	sb.WriteString(fmt.Sprintf("  %s %s\n", labelRendered, valueStyle.Render(val)))
}

func isModelCostKey(key string) bool {
	return core.IsModelCostMetricKey(key)
}

func formatMetricValue(m core.Metric) string {
	var value string
	switch {
	case m.Used != nil && m.Limit != nil:
		value = fmt.Sprintf("%s / %s %s",
			formatNumber(*m.Used), formatNumber(*m.Limit), m.Unit)
	case m.Remaining != nil && m.Limit != nil:
		value = fmt.Sprintf("%s / %s %s remaining",
			formatNumber(*m.Remaining), formatNumber(*m.Limit), m.Unit)
	case m.Used != nil:
		value = fmt.Sprintf("%s %s", formatNumber(*m.Used), m.Unit)
	case m.Remaining != nil:
		value = fmt.Sprintf("%s %s remaining", formatNumber(*m.Remaining), m.Unit)
	}

	if m.Window != "" && m.Window != "all_time" && m.Window != "current_period" {
		value += " " + dimStyle.Render("["+m.Window+"]")
	}
	return value
}

func renderModelCostsTable(sb *strings.Builder, entries []metricEntry, w int) {
	type modelCost struct {
		name    string
		cost    float64
		window  string
		hasData bool
	}

	var models []modelCost
	var unmatched []metricEntry

	for _, e := range entries {
		label := e.label
		var modelName string
		switch {
		case strings.HasSuffix(label, "_cost"):
			modelName = strings.TrimSuffix(label, "_cost")
		case strings.HasSuffix(label, "_cost_usd"):
			modelName = strings.TrimSuffix(label, "_cost_usd")
		default:
			unmatched = append(unmatched, e)
			continue
		}

		cost := float64(0)
		if e.metric.Used != nil {
			cost = *e.metric.Used
		}
		models = append(models, modelCost{
			name:    prettifyModelName(modelName),
			cost:    cost,
			window:  e.metric.Window,
			hasData: true,
		})
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].cost > models[j].cost
	})

	if len(models) > 0 {
		nameW := 28
		if w < 55 {
			nameW = 20
		}

		windowHint := ""
		if len(models) > 0 && models[0].window != "" &&
			models[0].window != "all_time" && models[0].window != "current_period" {
			windowHint = " " + dimStyle.Render("["+models[0].window+"]")
		}

		sb.WriteString(fmt.Sprintf("  %-*s %10s%s\n",
			nameW, dimStyle.Bold(true).Render("Model"),
			dimStyle.Bold(true).Render("Cost"),
			windowHint,
		))

		for _, mc := range models {
			name := mc.name
			if len(name) > nameW {
				name = name[:nameW-1] + "…"
			}
			costStr := formatUSD(mc.cost)
			costStyle := tealStyle
			if mc.cost >= 10 {
				costStyle = metricValueStyle
			}
			sb.WriteString(fmt.Sprintf("  %-*s %10s\n",
				nameW, valueStyle.Render(name),
				costStyle.Render(costStr),
			))
		}
	}

	for _, e := range unmatched {
		val := formatMetricValue(e.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(22).Render(prettifyModelName(e.label)),
			valueStyle.Render(val),
		))
	}
}

func renderUsageTable(sb *strings.Builder, entries []metricEntry, w int, warnThresh, critThresh float64) {
	if len(entries) == 0 {
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		pi := entries[i].metric.Percent()
		pj := entries[j].metric.Percent()
		if pi < 0 {
			pi = 200
		}
		if pj < 0 {
			pj = 200
		}
		return pi < pj
	})

	nameW := 30
	gaugeW := 10
	if w < 65 {
		nameW = 22
		gaugeW = 8
	}
	if w < 50 {
		nameW = 16
		gaugeW = 6
	}

	for _, entry := range entries {
		m := entry.metric
		name := entry.label
		if len(name) > nameW {
			name = name[:nameW-1] + "…"
		}

		pct := m.Percent()
		gauge := ""
		pctStr := ""
		if pct >= 0 {
			gauge = RenderMiniGauge(pct, gaugeW)
			var color lipgloss.Color
			switch {
			case pct <= critThresh*100:
				color = colorCrit
			case pct <= warnThresh*100:
				color = colorWarn
			default:
				color = colorOK
			}
			pctStr = lipgloss.NewStyle().Foreground(color).Bold(true).Render(fmt.Sprintf("%5.1f%%", pct))
		}

		windowStr := ""
		if m.Window != "" && m.Window != "all_time" && m.Window != "current_period" {
			windowStr = dimStyle.Render(" [" + m.Window + "]")
		}

		sb.WriteString(fmt.Sprintf("  %-*s %s %s%s\n",
			nameW, labelStyle.Render(name),
			gauge, pctStr, windowStr,
		))
	}
}
