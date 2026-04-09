package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type DetailTab int

const (
	TabAll  DetailTab = 0 // show everything
	TabDyn1 DetailTab = 1 // first dynamic group
)

// detailSection represents a renderable section in the detail view.
type detailSection struct {
	id           string
	title        string
	icon         string
	color        lipgloss.Color
	lines        []string
	hasOwnHeader bool // true when lines already contain a styled heading (composition sections)
}

func DetailTabs(snap core.UsageSnapshot) []string {
	// Single scrollable dashboard — no tabs needed.
	// All sections are shown in a well-organized card layout.
	return []string{"All"}
}

func RenderDetailContent(snap core.UsageSnapshot, w int, warnThresh, critThresh float64, activeTab int, chartZoom ...int) string {
	var sb strings.Builder
	widget := dashboardWidget(snap.ProviderID)

	zoom := 0
	if len(chartZoom) > 0 {
		zoom = chartZoom[0]
	}

	// ── Compact top bar ──
	renderDetailCompactHeader(&sb, snap, w)

	if len(snap.Metrics) == 0 && len(snap.ModelUsage) == 0 {
		if snap.Message != "" {
			sb.WriteString("\n")
			sb.WriteString(dimStyle.Render("  " + snap.Message))
			sb.WriteString("\n")
		}
		return sb.String()
	}

	// Build and render all sections as bordered cards.
	sections := buildDetailSections(snap, widget, w, warnThresh, critThresh, zoom)
	for _, sec := range sections {
		renderDetailCard(&sb, sec, w)
	}

	return sb.String()
}

// ── Compact Header ─────────────────────────────────────────────────────────
// Replaces the old bordered card header. Shows essential info in 2 lines.

func renderDetailCompactHeader(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	di := computeDisplayInfo(snap, dashboardWidget(snap.ProviderID))

	// Line 1: status icon + name (left) ... provider + meta + status badge (right)
	statusIcon := lipgloss.NewStyle().Foreground(StatusColor(snap.Status)).Render(StatusIcon(snap.Status))
	name := lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(snap.AccountID)

	var rightParts []string
	if di.tagEmoji != "" && di.tagLabel != "" {
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(tagColor(di.tagLabel)).Render(di.tagEmoji+" "+di.tagLabel))
	}
	rightParts = append(rightParts, dimStyle.Render(snap.ProviderID))
	if email := snapshotMeta(snap, "account_email"); email != "" {
		rightParts = append(rightParts, dimStyle.Render(email))
	}
	if planName := snapshotMeta(snap, "plan_name"); planName != "" {
		rightParts = append(rightParts, dimStyle.Render(planName))
	}
	rightParts = append(rightParts, StatusBadge(snap.Status))

	left := "  " + statusIcon + " " + name
	right := strings.Join(rightParts, dimStyle.Render(" · "))
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := w - leftW - rightW - 1
	if gap < 1 {
		gap = 1
	}
	sb.WriteString(left + strings.Repeat(" ", gap) + right + "\n")

	// Line 2: summary info (left) ... timestamp (right)
	var summaryParts []string
	if di.summary != "" {
		summaryParts = append(summaryParts, lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(di.summary))
	}
	if di.detail != "" {
		summaryParts = append(summaryParts, dimStyle.Render(di.detail))
	}
	if snap.Message != "" && di.summary == "" {
		summaryParts = append(summaryParts, lipgloss.NewStyle().Italic(true).Foreground(colorSubtext).Render(snap.Message))
	}
	summaryLeft := "  " + strings.Join(summaryParts, dimStyle.Render("  ·  "))

	timeStr := snap.Timestamp.Format("15:04:05")
	age := time.Since(snap.Timestamp)
	if age > 60*time.Second {
		timeStr = fmt.Sprintf("%s (%s ago)", snap.Timestamp.Format("15:04:05"), formatDuration(age))
	}
	summaryRight := dimStyle.Render("⏱ " + timeStr)
	sLeftW := lipgloss.Width(summaryLeft)
	sRightW := lipgloss.Width(summaryRight)
	sGap := w - sLeftW - sRightW - 1
	if sGap < 1 {
		sGap = 1
	}
	sb.WriteString(summaryLeft + strings.Repeat(" ", sGap) + summaryRight + "\n")

	// Accent separator colored by status.
	sepColor := StatusBorderColor(snap.Status)
	sepLen := w - 2
	if sepLen < 4 {
		sepLen = 4
	}
	sb.WriteString(" " + lipgloss.NewStyle().Foreground(sepColor).Render(strings.Repeat("━", sepLen)) + "\n")
}

// renderDetailFusedTabBar kept for backward compatibility — no-op when only one tab.
func renderDetailFusedTabBar(sb *strings.Builder, tabs []string, active int, w int) {
	if len(tabs) <= 1 {
		return
	}
}

// ── Bordered Card Sections ─────────────────────────────────────────────────
// Each section is rendered inside a bordered card with a title in the top border.

func renderDetailCard(sb *strings.Builder, sec detailSection, w int) {
	if len(sec.lines) == 0 {
		return
	}

	cardW := w - 4 // outer margins
	if cardW < 30 {
		cardW = 30
	}
	innerW := cardW - 4 // border + padding

	color := sec.color
	if color == "" {
		color = sectionColor(sec.title)
	}
	icon := sec.icon
	if icon == "" {
		icon = sectionIcon(sec.title)
	}

	sb.WriteString("\n")

	if sec.hasOwnHeader {
		// Composition sections already have their own styled heading.
		// Wrap in a subtle bordered card without a title in the border.
		topBorder := "  " + lipgloss.NewStyle().Foreground(colorSurface1).Render("╭"+strings.Repeat("─", cardW-2)+"╮")
		sb.WriteString(topBorder + "\n")

		for _, line := range sec.lines {
			// Pad each line to fit inside the card.
			lineW := lipgloss.Width(line)
			pad := innerW - lineW
			if pad < 0 {
				pad = 0
			}
			sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render("│") + " " + line + strings.Repeat(" ", pad) + " " + lipgloss.NewStyle().Foreground(colorSurface1).Render("│") + "\n")
		}

		botBorder := "  " + lipgloss.NewStyle().Foreground(colorSurface1).Render("╰"+strings.Repeat("─", cardW-2)+"╯")
		sb.WriteString(botBorder + "\n")
		return
	}

	// Build card with title embedded in the top border.
	titleStr := " " + icon + " " + sec.title + " "
	titleRendered := lipgloss.NewStyle().Foreground(color).Bold(true).Render(titleStr)
	titleW := lipgloss.Width(titleRendered)

	// Top border: ╭─ Title ─────────────────╮
	leftBorderLen := 1 // after ╭
	rightBorderLen := cardW - 2 - leftBorderLen - titleW
	if rightBorderLen < 1 {
		rightBorderLen = 1
	}
	topBorder := "  " +
		lipgloss.NewStyle().Foreground(color).Render("╭"+strings.Repeat("─", leftBorderLen)) +
		titleRendered +
		lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("─", rightBorderLen)+"╮")
	sb.WriteString(topBorder + "\n")

	// Body lines.
	borderChar := lipgloss.NewStyle().Foreground(color).Render("│")
	for _, line := range sec.lines {
		lineW := lipgloss.Width(line)
		pad := innerW - lineW
		if pad < 0 {
			pad = 0
		}
		sb.WriteString("  " + borderChar + " " + line + strings.Repeat(" ", pad) + " " + borderChar + "\n")
	}

	// Bottom border.
	botBorder := "  " + lipgloss.NewStyle().Foreground(color).Render("╰"+strings.Repeat("─", cardW-2)+"╯")
	sb.WriteString(botBorder + "\n")
}

// ── Section Builders ───────────────────────────────────────────────────────

// buildDetailSections constructs all dashboard-style sections for the detail view.
func buildDetailSections(snap core.UsageSnapshot, widget core.DashboardWidget, w int, warnThresh, critThresh float64, chartZoom int) []detailSection {
	innerW := w - 8 // card borders + margins + padding
	if innerW < 30 {
		innerW = 30
	}

	var sections []detailSection

	// 1. Usage Overview — gauges and key metrics (NO summary/detail text — that's in compact header).
	if usageLines := buildDetailUsageSection(snap, widget, innerW, warnThresh, critThresh); len(usageLines) > 0 {
		sections = append(sections, detailSection{id: "Usage", title: "Usage", icon: "⚡", color: colorYellow, lines: usageLines})
	}

	// 2. Cost & Credits — spending summary with projections.
	if costLines := buildDetailCostSection(snap, widget, innerW); len(costLines) > 0 {
		sections = append(sections, detailSection{id: "Cost", title: "Spending", icon: "💰", color: colorTeal, lines: costLines})
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
		sections = append(sections, detailSection{id: "Models", title: "Models", lines: modelLines, hasOwnHeader: true})
	}

	// 4. Client Burn — if provider supports it.
	if widget.ShowClientComposition {
		if clientLines, _ := buildProviderClientCompositionLinesWithWidget(snap, innerW, true, widget); len(clientLines) > 0 {
			sections = append(sections, detailSection{id: "Models", title: "Clients", lines: clientLines, hasOwnHeader: true})
		}
	}

	// 5. Project Breakdown.
	if projectLines, _ := buildProviderProjectBreakdownLines(snap, innerW, true); len(projectLines) > 0 {
		sections = append(sections, detailSection{id: "Projects", title: "Projects", lines: projectLines, hasOwnHeader: true})
	}

	// 6. Tool Usage.
	if toolLines := buildDetailToolSection(snap, widget, innerW); len(toolLines) > 0 {
		sections = append(sections, detailSection{id: "Tools", title: "Tools", lines: toolLines, hasOwnHeader: true})
	}

	// 7. MCP Usage.
	if hasMCPMetrics(snap) {
		if mcpLines := buildDetailMCPLines(snap, innerW); len(mcpLines) > 0 {
			sections = append(sections, detailSection{id: "MCP", title: "MCP Usage", icon: "🔌", color: colorSky, lines: mcpLines})
		}
	}

	// 8. Language breakdown.
	if hasLanguageMetrics(snap) {
		if langLines := buildDetailLanguageLines(snap, innerW); len(langLines) > 0 {
			sections = append(sections, detailSection{id: "Languages", title: "Language", icon: "🗂", color: colorPeach, lines: langLines})
		}
	}

	// 9. Code Statistics.
	if widget.ShowCodeStatsComposition {
		if codeLines, _ := buildProviderCodeStatsLines(snap, widget, innerW); len(codeLines) > 0 {
			sections = append(sections, detailSection{id: "Tools", title: "Code Stats", lines: codeLines, hasOwnHeader: true})
		}
	}

	// 10. Daily Usage & Trends (with zoom support).
	if trendLines := buildDetailTrendsSection(snap, widget, innerW, chartZoom); len(trendLines) > 0 {
		sections = append(sections, detailSection{id: "Trends", title: "Trends", lines: trendLines, hasOwnHeader: true})
	}

	// 10b. Activity Heatmap — day-of-week intensity (detail-only).
	if heatLines := buildDetailActivityHeatmap(snap, innerW); len(heatLines) > 0 {
		sections = append(sections, detailSection{id: "Trends", title: "Activity", icon: "📅", color: colorGreen, lines: heatLines})
	}

	// 10c. Dual-axis cost + requests overlay (detail-only).
	if dualLines := buildDetailDualAxisChart(snap, widget, innerW, chartZoom); len(dualLines) > 0 {
		sections = append(sections, detailSection{id: "Trends", title: "Overview", lines: dualLines, hasOwnHeader: true})
	}

	// 11. Upstream / Hosting Providers.
	if upstreamLines, _ := buildUpstreamProviderCompositionLines(snap, innerW, true); len(upstreamLines) > 0 {
		sections = append(sections, detailSection{id: "Cost", title: "Hosting", lines: upstreamLines, hasOwnHeader: true})
	}

	// 12. Provider Burn (vendor breakdown).
	if vendorLines, _ := buildProviderVendorCompositionLines(snap, innerW, true); len(vendorLines) > 0 {
		sections = append(sections, detailSection{id: "Cost", title: "Providers", lines: vendorLines, hasOwnHeader: true})
	}

	// 13. Budget projection (detail-only data).
	if projLines := buildDetailProjectionSection(snap, innerW); len(projLines) > 0 {
		sections = append(sections, detailSection{id: "Cost", title: "Forecast", icon: "📊", color: colorSapphire, lines: projLines})
	}

	// 14. Other metrics as dot-leader rows.
	if otherLines := buildDetailOtherMetrics(snap, widget, innerW); len(otherLines) > 0 {
		sections = append(sections, detailSection{id: "Usage", title: "Other Data", icon: "›", color: colorDim, lines: otherLines})
	}

	// 15. Timers.
	if len(snap.Resets) > 0 {
		var timerSB strings.Builder
		renderTimersSection(&timerSB, snap.Resets, widget, innerW+4)
		if timerStr := timerSB.String(); strings.TrimSpace(timerStr) != "" {
			lines := strings.Split(strings.TrimRight(timerStr, "\n"), "\n")
			filtered := filterOutSectionHeader(lines)
			sections = append(sections, detailSection{id: "Timers", title: "Timers", icon: "⏰", color: colorMaroon, lines: filtered})
		}
	}

	// 16. Info (Attributes, Diagnostics, Raw Data).
	if len(snap.Attributes) > 0 || len(snap.Diagnostics) > 0 || len(snap.Raw) > 0 {
		var infoSB strings.Builder
		renderInfoSection(&infoSB, snap, widget, innerW+4)
		if infoStr := infoSB.String(); strings.TrimSpace(infoStr) != "" {
			lines := strings.Split(strings.TrimRight(infoStr, "\n"), "\n")
			sections = append(sections, detailSection{id: "Info", title: "Info", icon: "📋", color: colorBlue, lines: lines})
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

// chartZoomDays maps zoom levels to the number of recent days to show.
// 0 = all data (no crop).
var chartZoomDays = []int{0, 90, 30, 14, 7, 3}

// cropSeriesToZoom crops series points to the given zoom level.
func cropSeriesToZoom(pts []core.TimePoint, zoom int) []core.TimePoint {
	if zoom <= 0 || zoom >= len(chartZoomDays) {
		return pts
	}
	days := chartZoomDays[zoom]
	if days <= 0 || len(pts) == 0 {
		return pts
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	start := 0
	for start < len(pts) && pts[start].Date < cutoff {
		start++
	}
	if start >= len(pts) {
		return pts // zoom too narrow, show all
	}
	return pts[start:]
}

// buildDetailTrendsSection builds the daily trends + charts section.
// Unlike the tile view which shows one chart + sparklines, the detail view
// renders a full Braille chart for EACH available data series.
func buildDetailTrendsSection(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, chartZoom int) []string {
	var lines []string

	// Daily usage sparkline summary (compact overview).
	dailyLines := buildProviderDailyTrendLines(snap, innerW)
	lines = append(lines, dailyLines...)

	// Render a separate chart for each available series.
	seriesCandidates := []struct {
		keys  []string
		label string
		yFmt  func(float64) string
		color lipgloss.Color
	}{
		{keys: []string{"analytics_cost", "cost"}, label: "Cost", yFmt: formatCostAxis, color: colorTeal},
		{keys: []string{"analytics_requests", "requests"}, label: "Requests", yFmt: formatChartValue, color: colorYellow},
		{keys: []string{"analytics_tokens", "tokens_total"}, label: "Tokens", yFmt: formatChartValue, color: colorSapphire},
		{keys: []string{"messages"}, label: "Messages", yFmt: formatChartValue, color: colorGreen},
		{keys: []string{"sessions"}, label: "Sessions", yFmt: formatChartValue, color: colorPeach},
	}

	chartW := innerW - 4
	if chartW < 30 {
		chartW = 30
	}
	chartH := 10 // consistent height for all charts
	if innerW < 80 {
		chartH = 8
	}

	for _, candidate := range seriesCandidates {
		var pts []core.TimePoint
		var matchedKey string
		for _, key := range candidate.keys {
			if p, ok := snap.DailySeries[key]; ok && len(p) >= 2 {
				pts = p
				matchedKey = key
				break
			}
		}
		if len(pts) < 2 {
			continue
		}

		// Apply zoom.
		pts = cropSeriesToZoom(pts, chartZoom)
		if len(pts) < 2 {
			continue
		}

		series := []BrailleSeries{{
			Label:  metricLabel(widget, matchedKey),
			Color:  candidate.color,
			Points: pts,
		}}

		chart := RenderBrailleChart(candidate.label, series, chartW, chartH, candidate.yFmt)
		if chart != "" {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, strings.Split(strings.TrimRight(chart, "\n"), "\n")...)
		}
	}

	for _, breakdown := range buildDetailBreakdownTrendCharts(snap, widget) {
		// Apply zoom to breakdown series.
		for i := range breakdown.series {
			breakdown.series[i].Points = cropSeriesToZoom(breakdown.series[i].Points, chartZoom)
		}
		chart := RenderBrailleChart(breakdown.title, breakdown.series, chartW, chartH, breakdown.yFmt)
		if chart == "" {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, strings.Split(strings.TrimRight(chart, "\n"), "\n")...)
		if breakdown.hiddenCount > 0 {
			lines = append(lines, "  "+dimStyle.Render(fmt.Sprintf("+ %d more %s with daily series", breakdown.hiddenCount, breakdown.hiddenLabel)))
		}
	}

	return lines
}

type detailTrendBreakdownChart struct {
	title       string
	series      []BrailleSeries
	yFmt        func(float64) string
	hiddenCount int
	hiddenLabel string
}

// buildDetailActivityHeatmap builds a day-of-week activity heatmap from DailySeries.
// Shows usage intensity across weekdays, giving a quick visual of work patterns.
func buildDetailActivityHeatmap(snap core.UsageSnapshot, innerW int) []string {
	// Find the best series for the heatmap.
	candidates := []string{"analytics_requests", "requests", "analytics_cost", "cost"}
	var pts []core.TimePoint
	for _, key := range candidates {
		if p, ok := snap.DailySeries[key]; ok && len(p) >= 7 {
			pts = p
			break
		}
	}
	if len(pts) < 7 {
		return nil
	}

	// Bucket by day of week.
	dayNames := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	// Aggregate into 4-week rolling buckets (rows=weekday, cols=week numbers).
	type weekDay struct {
		week int
		dow  int // 0=Mon, 6=Sun
	}

	// Parse all dates and find week range.
	var entries []struct {
		date  time.Time
		value float64
	}
	for _, p := range pts {
		t, err := time.Parse("2006-01-02", p.Date)
		if err != nil {
			continue
		}
		if p.Value < 0 {
			continue
		}
		entries = append(entries, struct {
			date  time.Time
			value float64
		}{t, p.Value})
	}
	if len(entries) < 7 {
		return nil
	}

	// Group by ISO week and day-of-week.
	type cellKey struct{ week, dow int }
	cells := make(map[cellKey]float64)
	weekSet := make(map[int]bool)
	for _, e := range entries {
		_, wk := e.date.ISOWeek()
		dow := int(e.date.Weekday())
		if dow == 0 {
			dow = 6 // Sunday → 6
		} else {
			dow-- // Monday=0, Tuesday=1, ...
		}
		key := cellKey{wk, dow}
		cells[key] += e.value
		weekSet[wk] = true
	}

	// Sort weeks, keep last 8 for readability.
	var weeks []int
	for w := range weekSet {
		weeks = append(weeks, w)
	}
	sort.Ints(weeks)
	if len(weeks) > 8 {
		weeks = weeks[len(weeks)-8:]
	}
	if len(weeks) < 2 {
		return nil
	}

	// Build heatmap grid: rows=days, cols=weeks.
	cols := make([]string, len(weeks))
	for i, w := range weeks {
		cols[i] = fmt.Sprintf("W%d", w)
	}
	values := make([][]float64, 7)
	for dow := 0; dow < 7; dow++ {
		values[dow] = make([]float64, len(weeks))
		for ci, w := range weeks {
			values[dow][ci] = cells[cellKey{w, dow}]
		}
	}

	rowColors := make([]lipgloss.Color, 7)
	for i := range rowColors {
		if i < 5 {
			rowColors[i] = colorSubtext
		} else {
			rowColors[i] = colorDim // dim weekends
		}
	}

	chartW := innerW - 4
	if chartW < 30 {
		chartW = 30
	}
	out := RenderHeatmap(HeatmapSpec{
		Title:     "Weekly Activity",
		Rows:      dayNames,
		Cols:      cols,
		Values:    values,
		RowColors: rowColors,
	}, chartW)
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// buildDetailDualAxisChart builds an overlay chart showing cost and requests
// together on a single chart. Uses left Y-axis for cost and colors to distinguish.
func buildDetailDualAxisChart(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, chartZoom int) []string {
	var costPts, reqPts []core.TimePoint
	for _, key := range []string{"analytics_cost", "cost"} {
		if p, ok := snap.DailySeries[key]; ok && len(p) >= 2 {
			costPts = p
			break
		}
	}
	for _, key := range []string{"analytics_requests", "requests"} {
		if p, ok := snap.DailySeries[key]; ok && len(p) >= 2 {
			reqPts = p
			break
		}
	}
	// Only show if we have BOTH series.
	if len(costPts) < 2 || len(reqPts) < 2 {
		return nil
	}

	costPts = cropSeriesToZoom(costPts, chartZoom)
	reqPts = cropSeriesToZoom(reqPts, chartZoom)
	if len(costPts) < 2 || len(reqPts) < 2 {
		return nil
	}

	chartW := innerW - 4
	if chartW < 30 {
		chartW = 30
	}
	chartH := 10
	if innerW < 80 {
		chartH = 8
	}

	series := []BrailleSeries{
		{Label: "Cost ($)", Color: colorTeal, Points: costPts},
		{Label: "Requests", Color: colorYellow, Points: reqPts},
	}

	chart := RenderBrailleChart("Cost & Requests", series, chartW, chartH, nil)
	if chart == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(chart, "\n"), "\n")
}

func buildDetailBreakdownTrendCharts(snap core.UsageSnapshot, widget core.DashboardWidget) []detailTrendBreakdownChart {
	const maxSeries = 4

	var charts []detailTrendBreakdownChart

	if chart, ok := buildModelBreakdownTrendChart(snap, maxSeries); ok {
		charts = append(charts, chart)
	}
	if widget.ShowClientComposition {
		if chart, ok := buildClientBreakdownTrendChart(snap, widget, maxSeries); ok {
			charts = append(charts, chart)
		}
	}
	if chart, ok := buildProjectBreakdownTrendChart(snap, maxSeries); ok {
		charts = append(charts, chart)
	}

	return charts
}

func buildModelBreakdownTrendChart(snap core.UsageSnapshot, maxSeries int) (detailTrendBreakdownChart, bool) {
	models, _ := collectProviderModelMix(snap)
	if len(models) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	colors := buildModelColorMap(models, snap.AccountID)
	series, hidden := collectDetailTrendSeries(maxSeries, len(models), func(idx int) (BrailleSeries, bool) {
		model := models[idx]
		if len(model.series) < 2 {
			return BrailleSeries{}, false
		}
		return BrailleSeries{
			Label:  prettifyModelName(model.name),
			Color:  colorForModel(colors, model.name),
			Points: model.series,
		}, true
	})
	if len(series) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	return detailTrendBreakdownChart{
		title:       "Model Breakdown",
		series:      series,
		yFmt:        formatChartValue,
		hiddenCount: hidden,
		hiddenLabel: "models",
	}, true
}

func buildClientBreakdownTrendChart(snap core.UsageSnapshot, widget core.DashboardWidget, maxSeries int) (detailTrendBreakdownChart, bool) {
	clients, _ := collectProviderClientMix(snap)
	if widget.ClientCompositionIncludeInterfaces {
		if interfaceClients, _ := collectInterfaceAsClients(snap); len(interfaceClients) > 0 {
			clients = interfaceClients
		}
	}
	if len(clients) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	colors := buildClientColorMap(clients, snap.AccountID)
	series, hidden := collectDetailTrendSeries(maxSeries, len(clients), func(idx int) (BrailleSeries, bool) {
		client := clients[idx]
		if len(client.series) < 2 {
			return BrailleSeries{}, false
		}
		return BrailleSeries{
			Label:  prettifyClientName(client.name),
			Color:  colorForClient(colors, client.name),
			Points: client.series,
		}, true
	})
	if len(series) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	return detailTrendBreakdownChart{
		title:       "Client Breakdown",
		series:      series,
		yFmt:        formatChartValue,
		hiddenCount: hidden,
		hiddenLabel: "clients",
	}, true
}

func buildProjectBreakdownTrendChart(snap core.UsageSnapshot, maxSeries int) (detailTrendBreakdownChart, bool) {
	projects, _ := collectProviderProjectMix(snap)
	if len(projects) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	colors := buildProjectColorMap(projects, snap.AccountID)
	series, hidden := collectDetailTrendSeries(maxSeries, len(projects), func(idx int) (BrailleSeries, bool) {
		project := projects[idx]
		if len(project.series) < 2 {
			return BrailleSeries{}, false
		}
		return BrailleSeries{
			Label:  project.name,
			Color:  colorForProject(colors, project.name),
			Points: project.series,
		}, true
	})
	if len(series) == 0 {
		return detailTrendBreakdownChart{}, false
	}

	return detailTrendBreakdownChart{
		title:       "Project Breakdown",
		series:      series,
		yFmt:        formatChartValue,
		hiddenCount: hidden,
		hiddenLabel: "projects",
	}, true
}

func collectDetailTrendSeries(maxSeries, total int, build func(int) (BrailleSeries, bool)) ([]BrailleSeries, int) {
	if maxSeries <= 0 {
		maxSeries = 1
	}

	series := make([]BrailleSeries, 0, min(maxSeries, total))
	matched := 0
	for i := 0; i < total; i++ {
		entry, ok := build(i)
		if !ok {
			continue
		}
		matched++
		if len(series) >= maxSeries {
			continue
		}
		series = append(series, entry)
	}
	return series, max(0, matched-len(series))
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

// ── Data presence checks ───────────────────────────────────────────────────

func hasDetailUsageData(snap core.UsageSnapshot, widget core.DashboardWidget) bool {
	for _, key := range core.SortedStringKeys(snap.Metrics) {
		if metricUsedPercent(key, snap.Metrics[key]) >= 0 {
			return true
		}
	}
	return len(widget.CompactRows) > 0 && len(snap.Metrics) > 0
}

func hasDetailCostData(snap core.UsageSnapshot) bool {
	costKeys := []string{"today_api_cost", "today_cost", "5h_block_cost", "7d_api_cost",
		"all_time_api_cost", "total_cost_usd", "window_cost", "monthly_spend",
		"credit_balance", "spend_limit", "plan_spend", "plan_total_spend_usd"}
	for _, k := range costKeys {
		if met, ok := snap.Metrics[k]; ok && (met.Used != nil || met.Remaining != nil || met.Limit != nil) {
			return true
		}
	}
	return len(core.ExtractAnalyticsModelUsage(snap)) > 0
}

func hasDetailProjectData(snap core.UsageSnapshot) bool {
	lines, _ := buildProviderProjectBreakdownLines(snap, 80, false)
	return len(lines) > 0
}

func hasDetailToolData(snap core.UsageSnapshot, widget core.DashboardWidget) bool {
	actualLines, _ := buildActualToolUsageLines(snap, 80, false)
	if len(actualLines) > 0 {
		return true
	}
	if widget.ShowToolComposition {
		toolLines, _ := buildProviderToolCompositionLines(snap, 80, false, widget)
		return len(toolLines) > 0
	}
	return false
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

// ── Legacy compatibility ───────────────────────────────────────────────────
// These are kept for backward compatibility with code that references them.

func renderTabBar(sb *strings.Builder, tabs []string, active int, w int) {
	renderDetailFusedTabBar(sb, tabs, active, w)
}

func renderDetailHeader(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	renderDetailCompactHeader(sb, snap, w)
}
