package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// buildDetailSections constructs all dashboard-style sections for the detail view.
// Sections are filtered and ordered according to effectiveDetailSectionOrder().
func buildDetailSections(snap core.UsageSnapshot, widget core.DashboardWidget, w int, warnThresh, critThresh float64, timeWindow core.TimeWindow) []detailSection {
	innerW := w - 8 // card borders + margins + padding
	if innerW < 30 {
		innerW = 30
	}

	// Build all candidate sections keyed by their DetailStandardSection ID.
	candidates := make(map[core.DetailStandardSection][]detailSection)

	// 1. Usage Overview — gauges and key metrics (NO summary/detail text — that's in compact header).
	if usageLines := buildDetailUsageSection(snap, widget, innerW, warnThresh, critThresh); len(usageLines) > 0 {
		candidates[core.DetailSectionUsage] = append(candidates[core.DetailSectionUsage],
			detailSection{id: "Usage", title: "Usage", icon: "⚡", color: colorYellow, lines: usageLines})
	}

	// 2. Cost & Credits — spending summary with projections.
	if costLines := buildDetailCostSection(snap, widget, innerW); len(costLines) > 0 {
		candidates[core.DetailSectionSpending] = append(candidates[core.DetailSectionSpending],
			detailSection{id: "Cost", title: "Spending", icon: "💰", color: colorTeal, lines: costLines})
	}

	// 3. Model Burn — composition bar with per-model breakdown + token detail.
	if modelLines, _ := buildProviderModelCompositionLines(snap, innerW, true); len(modelLines) > 0 {
		// Add per-model token breakdown if available.
		models := core.ExtractAnalyticsModelUsage(snap)
		for _, model := range models {
			if model.InputTokens <= 0 && model.OutputTokens <= 0 {
				continue
			}
			modelLines = append(modelLines, "")
			modelLines = append(modelLines, "  "+dimStyle.Render("Token breakdown: "+prettifyModelName(model.Name)))
			breakdown := RenderTokenBreakdown(model.InputTokens, model.OutputTokens, innerW-4)
			if breakdown != "" {
				modelLines = append(modelLines, strings.Split(strings.TrimRight(breakdown, "\n"), "\n")...)
			}
		}
		candidates[core.DetailSectionModels] = append(candidates[core.DetailSectionModels],
			detailSection{id: "Models", title: "Models", lines: modelLines, hasOwnHeader: true})
	}

	// 4. Client Burn — if provider supports it.
	if widget.ShowClientComposition {
		if clientLines, _ := buildProviderClientCompositionLinesWithWidget(snap, innerW, true, widget); len(clientLines) > 0 {
			candidates[core.DetailSectionClients] = append(candidates[core.DetailSectionClients],
				detailSection{id: "Models", title: "Clients", lines: clientLines, hasOwnHeader: true})
		}
	}

	// 5. Project Breakdown.
	if projectLines, _ := buildProviderProjectBreakdownLines(snap, innerW, true); len(projectLines) > 0 {
		candidates[core.DetailSectionProjects] = append(candidates[core.DetailSectionProjects],
			detailSection{id: "Projects", title: "Projects", lines: projectLines, hasOwnHeader: true})
	}

	// 6. Tool Usage.
	if toolLines := buildDetailToolSection(snap, widget, innerW); len(toolLines) > 0 {
		candidates[core.DetailSectionTools] = append(candidates[core.DetailSectionTools],
			detailSection{id: "Tools", title: "Tools", lines: toolLines, hasOwnHeader: true})
	}

	// 7. MCP Usage.
	if hasMCPMetrics(snap) {
		if mcpLines := buildDetailMCPLines(snap, innerW); len(mcpLines) > 0 {
			candidates[core.DetailSectionMCP] = append(candidates[core.DetailSectionMCP],
				detailSection{id: "MCP", title: "MCP Usage", icon: "🔌", color: colorSky, lines: mcpLines})
		}
	}

	// 8. Language breakdown.
	if hasLanguageMetrics(snap) {
		if langLines := buildDetailLanguageLines(snap, innerW); len(langLines) > 0 {
			candidates[core.DetailSectionLanguages] = append(candidates[core.DetailSectionLanguages],
				detailSection{id: "Languages", title: "Language", icon: "🗂", color: colorPeach, lines: langLines})
		}
	}

	// 9. Code Statistics.
	if widget.ShowCodeStatsComposition {
		if codeLines, _ := buildProviderCodeStatsLines(snap, widget, innerW); len(codeLines) > 0 {
			candidates[core.DetailSectionCodeStats] = append(candidates[core.DetailSectionCodeStats],
				detailSection{id: "Tools", title: "Code Stats", lines: codeLines, hasOwnHeader: true})
		}
	}

	// 10. Daily Usage & Trends (with zoom support).
	if trendLines := buildDetailTrendsSection(snap, widget, innerW, timeWindow); len(trendLines) > 0 {
		candidates[core.DetailSectionTrends] = append(candidates[core.DetailSectionTrends],
			detailSection{id: "Trends", title: "Trends", lines: trendLines, hasOwnHeader: true})
	}

	// 10b. Dual-axis cost + requests overlay (detail-only).
	if dualLines := buildDetailDualAxisChart(snap, widget, innerW, timeWindow); len(dualLines) > 0 {
		candidates[core.DetailSectionCostRequests] = append(candidates[core.DetailSectionCostRequests],
			detailSection{id: "Trends", title: "Overview", lines: dualLines, hasOwnHeader: true})
	}

	// 10c. Activity Heatmap.
	if heatLines := buildDetailActivityHeatmap(snap, innerW); len(heatLines) > 0 {
		candidates[core.DetailSectionActivityHeatmap] = append(candidates[core.DetailSectionActivityHeatmap],
			detailSection{id: "Trends", title: "Activity", icon: "📅", color: colorGreen, lines: heatLines})
	}

	// 11. Upstream / Hosting Providers.
	if upstreamLines, _ := buildUpstreamProviderCompositionLines(snap, innerW, true); len(upstreamLines) > 0 {
		candidates[core.DetailSectionUpstream] = append(candidates[core.DetailSectionUpstream],
			detailSection{id: "Cost", title: "Hosting", lines: upstreamLines, hasOwnHeader: true})
	}

	// 12. Provider Burn (vendor breakdown).
	if vendorLines, _ := buildProviderVendorCompositionLines(snap, innerW, true); len(vendorLines) > 0 {
		candidates[core.DetailSectionProviderBurn] = append(candidates[core.DetailSectionProviderBurn],
			detailSection{id: "Cost", title: "Providers", lines: vendorLines, hasOwnHeader: true})
	}

	// 13. Budget projection (detail-only data).
	if projLines := buildDetailProjectionSection(snap, innerW); len(projLines) > 0 {
		candidates[core.DetailSectionForecast] = append(candidates[core.DetailSectionForecast],
			detailSection{id: "Cost", title: "Forecast", icon: "📊", color: colorSapphire, lines: projLines})
	}

	// 14. Other metrics as dot-leader rows.
	if otherLines := buildDetailOtherMetrics(snap, widget, innerW); len(otherLines) > 0 {
		candidates[core.DetailSectionOtherData] = append(candidates[core.DetailSectionOtherData],
			detailSection{id: "Usage", title: "Other Data", icon: "›", color: colorDim, lines: otherLines})
	}

	// 15. Timers.
	if len(snap.Resets) > 0 {
		var timerSB strings.Builder
		renderTimersSection(&timerSB, snap.Resets, widget, innerW+4)
		if timerStr := timerSB.String(); strings.TrimSpace(timerStr) != "" {
			lines := strings.Split(strings.TrimRight(timerStr, "\n"), "\n")
			filtered := filterOutSectionHeader(lines)
			candidates[core.DetailSectionTimers] = append(candidates[core.DetailSectionTimers],
				detailSection{id: "Timers", title: "Timers", icon: "⏰", color: colorMaroon, lines: filtered})
		}
	}

	// 16. Info (Attributes, Diagnostics, Raw Data).
	if len(snap.Attributes) > 0 || len(snap.Diagnostics) > 0 || len(snap.Raw) > 0 {
		var infoSB strings.Builder
		renderInfoSection(&infoSB, snap, widget, innerW+4)
		if infoStr := infoSB.String(); strings.TrimSpace(infoStr) != "" {
			lines := strings.Split(strings.TrimRight(infoStr, "\n"), "\n")
			candidates[core.DetailSectionInfo] = append(candidates[core.DetailSectionInfo],
				detailSection{id: "Info", title: "Info", icon: "📋", color: colorBlue, lines: lines})
		}
	}

	// Emit sections in the configured order, skipping disabled ones.
	var sections []detailSection
	for _, sectionID := range effectiveDetailSectionOrder() {
		if secs, ok := candidates[sectionID]; ok {
			sections = append(sections, secs...)
		}
	}

	return sections
}

// buildDetailUsageSection builds the usage overview — gauges + compact metrics.
// Does NOT include summary/detail text (that's in the compact header now).
func buildDetailUsageSection(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, warnThresh, critThresh float64) []string {
	var lines []string

	// Usage gauge bars.
	gaugeLines := buildDetailGaugeLines(snap, widget, innerW, warnThresh, critThresh)
	lines = append(lines, gaugeLines...)

	// Compact metric summary rows (credits, messages, sessions, etc.).
	compactLines, _ := buildTileCompactMetricSummaryLines(snap, widget, innerW)
	if len(compactLines) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, compactLines...)
	}

	return lines
}

// buildDetailGaugeLines builds gauge bars for the detail view.
func buildDetailGaugeLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, warnThresh, critThresh float64) []string {
	maxLabelW := 18
	gaugeW := innerW - maxLabelW - 10
	if gaugeW < 8 {
		gaugeW = 8
	}
	if gaugeW > 50 {
		gaugeW = 50
	}
	maxLines := 6

	if len(snap.Metrics) == 0 {
		return nil
	}

	keys := core.SortedStringKeys(snap.Metrics)
	keys = prioritizeMetricKeys(keys, widget.GaugePriority)

	var gaugeAllowSet map[string]bool
	if len(widget.GaugePriority) > 0 {
		gaugeAllowSet = make(map[string]bool, len(widget.GaugePriority))
		for _, k := range widget.GaugePriority {
			gaugeAllowSet[k] = true
		}
	}

	var lines []string
	for _, key := range keys {
		if gaugeAllowSet != nil && !gaugeAllowSet[key] {
			continue
		}
		met := snap.Metrics[key]
		usedPct := metricUsedPercent(key, met)
		if usedPct < 0 {
			continue
		}
		label := gaugeLabel(widget, key, met.Window)
		if len(label) > maxLabelW {
			label = label[:maxLabelW-1] + "…"
		}
		gauge := RenderUsageGauge(usedPct, gaugeW, warnThresh, critThresh)
		labelR := lipgloss.NewStyle().Foreground(colorSubtext).Width(maxLabelW).Render(label)
		lines = append(lines, labelR+" "+gauge)
		if len(lines) >= maxLines {
			break
		}
	}
	return lines
}

// buildDetailCostSection builds spending/credit summary with projections.
func buildDetailCostSection(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int) []string {
	var lines []string
	costSummary := core.ExtractAnalyticsCostSummary(snap)

	costKeys := []struct {
		key   string
		label string
	}{
		{"today_api_cost", ""},
		{"today_cost", ""},
		{"5h_block_cost", "5h Cost"},
		{"7d_api_cost", "7-Day Cost"},
		{"all_time_api_cost", "All-Time Cost"},
		{"total_cost_usd", "Total Cost"},
		{"window_cost", "Window Cost"},
		{"monthly_spend", "Monthly Spend"},
	}

	for _, ck := range costKeys {
		met, ok := snap.Metrics[ck.key]
		if !ok || met.Used == nil || *met.Used == 0 {
			continue
		}
		label := ck.label
		if label == "" {
			label = metricLabel(widget, ck.key)
		}
		value := formatUSD(*met.Used)
		if met.Window != "" && met.Window != "all_time" && met.Window != "current_period" {
			value += " " + dimStyle.Render("["+met.Window+"]")
		}
		lines = append(lines, renderDotLeaderRow(label, value, innerW))
	}

	// Burn rate.
	if costSummary.BurnRateUSD > 0 {
		lines = append(lines, renderDotLeaderRow("Burn Rate", fmt.Sprintf("$%.2f/h", costSummary.BurnRateUSD), innerW))
	}

	// Credit balance.
	if met, ok := snap.Metrics["credit_balance"]; ok && met.Remaining != nil {
		value := formatUSD(*met.Remaining)
		if met.Limit != nil {
			value = fmt.Sprintf("%s / %s", formatUSD(*met.Remaining), formatUSD(*met.Limit))
		}
		lines = append(lines, renderDotLeaderRow("Credit Balance", value, innerW))
	}

	// Spend limit with budget gauge.
	if met, ok := snap.Metrics["spend_limit"]; ok && met.Limit != nil && met.Used != nil {
		labelW := 16
		gaugeW := innerW - labelW - 14
		if gaugeW < 8 {
			gaugeW = 8
		}
		if gaugeW > 28 {
			gaugeW = 28
		}
		line := RenderBudgetGauge("Spend Limit", *met.Used, *met.Limit, gaugeW, labelW, colorTeal, costSummary.BurnRateUSD)
		lines = append(lines, line)
	}

	// Model cost breakdown.
	models := core.ExtractAnalyticsModelUsage(snap)
	if len(models) > 0 {
		var modelCostLines []string
		for _, model := range models {
			if model.CostUSD <= 0 {
				continue
			}
			name := prettifyModelName(model.Name)
			tokInfo := ""
			if model.InputTokens > 0 || model.OutputTokens > 0 {
				tokInfo = fmt.Sprintf(" · %s tok", shortCompact(model.InputTokens+model.OutputTokens))
			}
			value := formatUSD(model.CostUSD) + tokInfo
			modelCostLines = append(modelCostLines, renderDotLeaderRow("  "+name, value, innerW))
		}
		if len(modelCostLines) > 0 {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, subtextBoldStyle.Render("Model Cost Breakdown"))
			lines = append(lines, modelCostLines...)
		}
	}

	return lines
}

// buildDetailProjectionSection builds budget forecast projections (detail-only data).
func buildDetailProjectionSection(snap core.UsageSnapshot, innerW int) []string {
	costSummary := core.ExtractAnalyticsCostSummary(snap)
	if costSummary.BurnRateUSD <= 0 {
		return nil
	}

	var lines []string

	// Check spend limit.
	if met, ok := snap.Metrics["spend_limit"]; ok && met.Limit != nil {
		used := float64(0)
		if met.Used != nil {
			used = *met.Used
		}
		remaining := *met.Limit - used
		if met.Remaining != nil {
			remaining = *met.Remaining
		}
		if remaining > 0 {
			hoursLeft := remaining / costSummary.BurnRateUSD
			daysLeft := hoursLeft / 24
			var projStr string
			if daysLeft < 1 {
				projStr = fmt.Sprintf("%.0fh left at $%.2f/h", hoursLeft, costSummary.BurnRateUSD)
			} else {
				projStr = fmt.Sprintf("%.1f days left at $%.2f/h", daysLeft, costSummary.BurnRateUSD)
			}
			urgencyColor := colorGreen
			if daysLeft < 3 {
				urgencyColor = colorRed
			} else if daysLeft < 7 {
				urgencyColor = colorYellow
			}
			lines = append(lines, renderDotLeaderRow("Limit forecast",
				lipgloss.NewStyle().Foreground(urgencyColor).Bold(true).Render(projStr), innerW))
		}
	}

	// Check credit balance.
	if met, ok := snap.Metrics["credit_balance"]; ok && met.Remaining != nil && *met.Remaining > 0 {
		hoursLeft := *met.Remaining / costSummary.BurnRateUSD
		daysLeft := hoursLeft / 24
		var projStr string
		if daysLeft < 1 {
			projStr = fmt.Sprintf("%.0fh of credits left", hoursLeft)
		} else {
			projStr = fmt.Sprintf("%.1f days of credits left", daysLeft)
		}
		lines = append(lines, renderDotLeaderRow("Credits forecast", projStr, innerW))
	}

	// Daily cost projection.
	if costSummary.BurnRateUSD > 0 {
		dailyCost := costSummary.BurnRateUSD * 24
		weeklyCost := dailyCost * 7
		monthlyCost := dailyCost * 30
		lines = append(lines, renderDotLeaderRow("Projected daily", formatUSD(dailyCost), innerW))
		lines = append(lines, renderDotLeaderRow("Projected weekly", formatUSD(weeklyCost), innerW))
		lines = append(lines, renderDotLeaderRow("Projected monthly", formatUSD(monthlyCost), innerW))
	}

	return lines
}

// buildDetailToolSection builds the tool usage section.
func buildDetailToolSection(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int) []string {
	actualLines, _ := buildActualToolUsageLines(snap, innerW, true)
	if len(actualLines) > 0 {
		return actualLines
	}
	if widget.ShowToolComposition {
		toolLines, _ := buildProviderToolCompositionLines(snap, innerW, true, widget)
		return toolLines
	}
	return nil
}

// buildDetailMCPLines renders MCP usage into lines.
func buildDetailMCPLines(snap core.UsageSnapshot, innerW int) []string {
	var sb strings.Builder
	renderMCPSection(&sb, snap, innerW)
	out := sb.String()
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// buildDetailLanguageLines renders language breakdown into lines.
func buildDetailLanguageLines(snap core.UsageSnapshot, innerW int) []string {
	var sb strings.Builder
	renderLanguagesSection(&sb, snap, innerW)
	out := sb.String()
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// buildDetailOtherMetrics renders remaining metrics not covered by other sections.
func buildDetailOtherMetrics(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int) []string {
	if len(snap.Metrics) == 0 {
		return nil
	}

	skipKeys := make(map[string]bool)

	for _, key := range core.SortedStringKeys(snap.Metrics) {
		if metricHasGauge(key, snap.Metrics[key]) {
			skipKeys[key] = true
		}
	}

	for _, ck := range []string{"today_api_cost", "today_cost", "5h_block_cost", "7d_api_cost",
		"all_time_api_cost", "total_cost_usd", "window_cost", "monthly_spend",
		"credit_balance", "spend_limit", "plan_spend", "plan_total_spend_usd",
		"plan_limit_usd", "plan_percent_used", "individual_spend", "burn_rate"} {
		skipKeys[ck] = true
	}

	_, compactKeys := buildTileCompactMetricSummaryLines(snap, widget, innerW)
	for k := range compactKeys {
		skipKeys[k] = true
	}
	_, modelKeys := buildProviderModelCompositionLines(snap, innerW, true)
	for k := range modelKeys {
		skipKeys[k] = true
	}
	_, projectKeys := buildProviderProjectBreakdownLines(snap, innerW, true)
	for k := range projectKeys {
		skipKeys[k] = true
	}
	_, toolKeys := buildActualToolUsageLines(snap, innerW, true)
	for k := range toolKeys {
		skipKeys[k] = true
	}

	keys := core.SortedStringKeys(snap.Metrics)
	var lines []string
	maxLabel := innerW/2 - 1
	if maxLabel < 8 {
		maxLabel = 8
	}

	for _, key := range keys {
		if skipKeys[key] {
			continue
		}
		if hasAnyPrefix(key, widget.HideMetricPrefixes) {
			continue
		}
		met := snap.Metrics[key]
		if !core.IncludeDetailMetricKey(key) {
			continue
		}
		value := formatTileMetricValue(key, met)
		if value == "" {
			continue
		}
		label := metricLabel(widget, key)
		if len(label) > maxLabel {
			label = label[:maxLabel-1] + "…"
		}
		lines = append(lines, renderDotLeaderRow(label, value, innerW))
	}
	return lines
}

func filterOutSectionHeader(lines []string) []string {
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && len(result) == 0 {
			continue
		}
		if strings.Contains(trimmed, "──") && (strings.Contains(trimmed, "⏰") || strings.Contains(trimmed, "Timers")) {
			continue
		}
		result = append(result, line)
	}
	return result
}
