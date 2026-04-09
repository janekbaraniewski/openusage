package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func renderModelsSection(sb *strings.Builder, snap core.UsageSnapshot, widget core.DashboardWidget, w int) {
	models := core.ExtractAnalyticsModelUsage(snap)
	if len(models) == 0 {
		return
	}

	if len(models) > 8 {
		models = models[:8]
	}

	items := make([]chartItem, 0, len(models))
	for i, model := range models {
		if model.CostUSD <= 0 {
			continue
		}
		subLabel := ""
		if i == 0 && model.InputTokens > 0 {
			subLabel = formatTokens(model.InputTokens) + " in"
		}
		items = append(items, chartItem{
			Label:    prettifyModelName(model.Name),
			Value:    model.CostUSD,
			Color:    stableModelColor(model.Name, snap.ProviderID),
			SubLabel: subLabel,
		})
	}

	if len(items) > 0 {
		labelW := 22
		if w < 55 {
			labelW = 16
		}
		barW := w - labelW - 20
		if barW < 8 {
			barW = 8
		}
		if barW > 30 {
			barW = 30
		}
		sb.WriteString(RenderHBarChart(items, barW, labelW) + "\n")
	}

	for _, model := range models {
		if model.InputTokens <= 0 && model.OutputTokens <= 0 {
			continue
		}
		sb.WriteString("\n")
		sb.WriteString("  " + dimStyle.Render("Token breakdown: "+prettifyModelName(model.Name)) + "\n")
		sb.WriteString(RenderTokenBreakdown(model.InputTokens, model.OutputTokens, w-4) + "\n")
		break
	}
}

func hasAnalyticsModelData(snap core.UsageSnapshot) bool {
	return len(core.ExtractAnalyticsModelUsage(snap)) > 0
}

func hasChartableSeries(series map[string][]core.TimePoint) bool {
	for _, pts := range series {
		if len(pts) >= 2 {
			return true
		}
	}
	return false
}

func hasLanguageMetrics(snap core.UsageSnapshot) bool {
	return core.HasLanguageUsage(snap)
}

func renderLanguagesSection(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	langs, _ := core.ExtractLanguageUsage(snap)
	if len(langs) == 0 {
		return
	}

	total := float64(0)
	for _, l := range langs {
		total += l.Requests
	}
	if total <= 0 {
		return
	}

	maxShow := 10
	if len(langs) > maxShow {
		langs = langs[:maxShow]
	}

	var items []chartItem
	for _, l := range langs {
		pct := l.Requests / total * 100
		items = append(items, chartItem{
			Label:     l.Name,
			Value:     l.Requests,
			Color:     stableModelColor("lang:"+l.Name, "languages"),
			ValueText: fmt.Sprintf("%4.1f%%  %s", pct, dimStyle.Render(formatNumber(l.Requests)+" req")),
		})
	}

	labelW := 18
	if w < 55 {
		labelW = 14
	}
	barW := w - labelW - 20
	if barW < 8 {
		barW = 8
	}
	if barW > 30 {
		barW = 30
	}

	sb.WriteString(RenderHBarChart(items, barW, labelW) + "\n")

	if len(langs) > maxShow {
		remaining := len(langs) - maxShow
		if remaining > 0 {
			sb.WriteString("  " + dimStyle.Render(fmt.Sprintf("+ %d more languages", remaining)) + "\n")
		}
	}
}

func hasMCPMetrics(snap core.UsageSnapshot) bool {
	return core.HasMCPUsage(snap)
}

func renderMCPSection(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	rawServers, _ := core.ExtractMCPUsage(snap)
	servers := make([]struct {
		name  string
		calls float64
		funcs []struct {
			name  string
			calls float64
		}
	}, 0, len(rawServers))
	for _, rawServer := range rawServers {
		server := struct {
			name  string
			calls float64
			funcs []struct {
				name  string
				calls float64
			}
		}{
			name:  prettifyMCPServerName(rawServer.RawName),
			calls: rawServer.Calls,
		}
		for _, rawFunc := range rawServer.Functions {
			server.funcs = append(server.funcs, struct {
				name  string
				calls float64
			}{
				name:  prettifyMCPFunctionName(rawFunc.RawName),
				calls: rawFunc.Calls,
			})
		}
		servers = append(servers, server)
	}
	if len(servers) == 0 {
		return
	}

	var totalCalls float64
	for _, srv := range servers {
		totalCalls += srv.calls
	}
	if totalCalls <= 0 {
		return
	}

	barW := w - 4
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	var allEntries []toolMixEntry
	for _, srv := range servers {
		allEntries = append(allEntries, toolMixEntry{name: srv.name, count: srv.calls})
	}
	toolColors := buildToolColorMap(allEntries, snap.AccountID)

	sb.WriteString(fmt.Sprintf("  %s\n", renderToolMixBar(allEntries, totalCalls, barW, toolColors)))

	for i, srv := range servers {
		toolColor := colorForTool(toolColors, srv.name)
		colorDot := lipgloss.NewStyle().Foreground(toolColor).Render("■")
		serverLabel := fmt.Sprintf("%s %d %s", colorDot, i+1, srv.name)
		pct := srv.calls / totalCalls * 100
		valueStr := fmt.Sprintf("%2.0f%% %s calls", pct, shortCompact(srv.calls))
		sb.WriteString(renderDotLeaderRow(serverLabel, valueStr, w-2))
		sb.WriteString("\n")

		maxFuncs := 8
		if len(srv.funcs) < maxFuncs {
			maxFuncs = len(srv.funcs)
		}
		for j := 0; j < maxFuncs; j++ {
			fn := srv.funcs[j]
			fnLabel := "    " + fn.name
			fnValue := fmt.Sprintf("%s calls", shortCompact(fn.calls))
			sb.WriteString(renderDotLeaderRow(fnLabel, fnValue, w-2))
			sb.WriteString("\n")
		}
		if len(srv.funcs) > 8 {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("    + %d more functions", len(srv.funcs)-8)))
			sb.WriteString("\n")
		}
	}

	footer := fmt.Sprintf("%d servers · %.0f calls", len(servers), totalCalls)
	sb.WriteString("  " + dimStyle.Render(footer) + "\n")
}

func hasModelCostMetrics(snap core.UsageSnapshot) bool {
	return core.HasModelCostUsage(snap)
}

func renderTrendsSection(sb *strings.Builder, snap core.UsageSnapshot, widget core.DashboardWidget, w int) {
	if len(snap.DailySeries) == 0 {
		return
	}

	primaryCandidates := []string{"analytics_cost", "cost", "tokens_total", "analytics_tokens", "messages", "analytics_requests", "requests", "sessions"}
	primaryKey := ""
	for _, key := range primaryCandidates {
		if pts, ok := snap.DailySeries[key]; ok && len(pts) >= 2 {
			primaryKey = key
			break
		}
	}

	if primaryKey == "" {
		// Use sorted keys for deterministic selection (map iteration is random).
		for _, key := range core.SortedStringKeys(snap.DailySeries) {
			if pts := snap.DailySeries[key]; len(pts) >= 2 {
				primaryKey = key
				break
			}
		}
	}

	if primaryKey == "" {
		return
	}

	pts := snap.DailySeries[primaryKey]
	yFmt := formatChartValue
	if primaryKey == "cost" || primaryKey == "analytics_cost" {
		yFmt = formatCostAxis
	}

	chartW := w - 4
	if chartW < 30 {
		chartW = 30
	}
	chartH := 6
	if w < 60 {
		chartH = 4
	}

	series := []BrailleSeries{{
		Label:  metricLabel(widget, primaryKey),
		Color:  colorTeal,
		Points: pts,
	}}

	chart := RenderBrailleChart(metricLabel(widget, primaryKey), series, chartW, chartH, yFmt)
	if chart != "" {
		sb.WriteString(chart)
	}

	sparkW := w - 8
	if sparkW < 12 {
		sparkW = 12
	}
	if sparkW > 60 {
		sparkW = 60
	}

	colors := []lipgloss.Color{colorSapphire, colorGreen, colorPeach, colorLavender}
	colorIdx := 0

	for _, candidate := range primaryCandidates {
		if candidate == primaryKey {
			continue
		}
		seriesPts, ok := snap.DailySeries[candidate]
		if !ok || len(seriesPts) < 2 {
			continue
		}
		values := make([]float64, len(seriesPts))
		for i, p := range seriesPts {
			values[i] = p.Value
		}
		c := colors[colorIdx%len(colors)]
		colorIdx++
		spark := RenderSparkline(values, sparkW, c)
		label := metricLabel(widget, candidate)
		sb.WriteString(fmt.Sprintf("  %s %s\n", dimStyle.Render(label), spark))
	}
}
