package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/agentusage/internal/core"
)

// ─── Analytics Sub-Tabs ─────────────────────────────────────────────────────

const (
	analyticsTabOverview   = 0
	analyticsTabProviders  = 1
	analyticsTabModels     = 2
	analyticsTabBudget     = 3
	analyticsTabEfficiency = 4
	analyticsTabCount      = 5 // sentinel
)

var analyticsTabLabels = []string{"Overview", "Providers", "Models", "Budget", "Efficiency"}
var analyticsTabKeys = []string{"o", "p", "m", "b", "e"}

// ─── Sort Modes ─────────────────────────────────────────────────────────────

const (
	analyticsSortCostDesc   = 0
	analyticsSortNameAsc    = 1
	analyticsSortTokensDesc = 2
	analyticsSortCount      = 3 // sentinel
)

var sortByLabels = []string{"Cost ↓", "Name ↑", "Tokens ↓"}

// ─── Extracted Analytics Data ───────────────────────────────────────────────

type costData struct {
	totalCost     float64
	totalInput    float64
	totalOutput   float64
	burnRate      float64
	providerCount int
	activeCount   int
	modelCount    int
	providers     []providerCostEntry
	models        []modelCostEntry
	budgets       []budgetEntry
}

type providerCostEntry struct {
	name      string
	accountID string
	cost      float64
	color     lipgloss.Color
	models    []modelCostEntry // nested model breakdown
}

type modelCostEntry struct {
	name         string
	provider     string
	cost         float64
	inputTokens  float64
	outputTokens float64
	color        lipgloss.Color
}

type budgetEntry struct {
	name     string
	used     float64
	limit    float64
	percent  float64
	color    lipgloss.Color
	burnRate float64
}

// ─── Data Extraction ────────────────────────────────────────────────────────

func extractCostData(snapshots map[string]core.QuotaSnapshot, filter string) costData {
	var data costData
	lowerFilter := strings.ToLower(filter)

	modelIdx := 0

	for _, snap := range snapshots {
		// Apply filter
		if filter != "" {
			matches := strings.Contains(strings.ToLower(snap.AccountID), lowerFilter) ||
				strings.Contains(strings.ToLower(snap.ProviderID), lowerFilter)
			if !matches {
				continue
			}
		}

		data.providerCount++

		// Check active status
		if snap.Status == core.StatusOK || snap.Status == core.StatusNearLimit {
			data.activeCount++
		}

		// Extract provider-level cost
		cost := extractProviderCost(snap)
		provColor := ProviderColor(snap.ProviderID)

		// Extract burn rate
		br := extractBurnRate(snap)
		data.burnRate += br

		// Extract model-level costs
		models := extractModelCosts(snap, provColor, &modelIdx)
		for i := range models {
			models[i].provider = snap.AccountID
		}

		data.totalCost += cost

		// Sum model token totals
		for _, m := range models {
			data.totalInput += m.inputTokens
			data.totalOutput += m.outputTokens
		}

		data.providers = append(data.providers, providerCostEntry{
			name:      snap.AccountID,
			accountID: snap.AccountID,
			cost:      cost,
			color:     provColor,
			models:    models,
		})

		// Extract budgets
		budgets := extractBudgetEntries(snap, provColor, br)
		data.budgets = append(data.budgets, budgets...)
	}

	// Flatten all models across providers
	for _, prov := range data.providers {
		data.models = append(data.models, prov.models...)
	}
	data.modelCount = len(data.models)

	return data
}

func extractProviderCost(snap core.QuotaSnapshot) float64 {
	// Priority chain for extracting the most representative cost
	costKeys := []string{
		"daily_cost_usd",
		"total_cost_usd",
		"jsonl_total_cost_usd",
		"block_cost_usd",
	}
	for _, key := range costKeys {
		if m, ok := snap.Metrics[key]; ok && m.Used != nil {
			return *m.Used
		}
	}

	// Spend limit used amount
	if m, ok := snap.Metrics["spend_limit"]; ok && m.Used != nil {
		return *m.Used
	}

	// Plan spend
	if m, ok := snap.Metrics["plan_spend"]; ok && m.Used != nil {
		return *m.Used
	}
	if m, ok := snap.Metrics["plan_total_spend_usd"]; ok && m.Used != nil {
		return *m.Used
	}

	// Credits used
	if m, ok := snap.Metrics["credits"]; ok && m.Used != nil {
		return *m.Used
	}

	// Sum model costs if no top-level cost
	total := float64(0)
	for key, m := range snap.Metrics {
		if strings.HasSuffix(key, "_cost_usd") && strings.HasPrefix(key, "model_") && m.Used != nil {
			total += *m.Used
		}
	}

	return total
}

func extractBurnRate(snap core.QuotaSnapshot) float64 {
	if m, ok := snap.Metrics["burn_rate_usd_per_hour"]; ok && m.Used != nil {
		return *m.Used
	}
	return 0
}

func extractModelCosts(snap core.QuotaSnapshot, provColor lipgloss.Color, idxCounter *int) []modelCostEntry {
	type modelData struct {
		name         string
		cost         float64
		inputTokens  float64
		outputTokens float64
		idx          int
	}

	models := make(map[string]*modelData)
	var modelOrder []string

	for key, m := range snap.Metrics {
		if !strings.HasPrefix(key, "model_") || strings.HasPrefix(key, "model_local_") {
			continue
		}

		name := strings.TrimPrefix(key, "model_")
		var metricType string

		switch {
		case strings.HasSuffix(name, "_input_tokens"):
			name = strings.TrimSuffix(name, "_input_tokens")
			metricType = "input"
		case strings.HasSuffix(name, "_output_tokens"):
			name = strings.TrimSuffix(name, "_output_tokens")
			metricType = "output"
		case strings.HasSuffix(name, "_cost_usd"):
			name = strings.TrimSuffix(name, "_cost_usd")
			metricType = "cost"
		default:
			continue
		}

		md, ok := models[name]
		if !ok {
			md = &modelData{name: name, idx: *idxCounter}
			*idxCounter++
			models[name] = md
			modelOrder = append(modelOrder, name)
		}
		if m.Used != nil {
			switch metricType {
			case "input":
				md.inputTokens = *m.Used
			case "output":
				md.outputTokens = *m.Used
			case "cost":
				md.cost = *m.Used
			}
		}
	}

	var result []modelCostEntry
	for _, name := range modelOrder {
		md := models[name]
		if md.cost > 0 || md.inputTokens > 0 || md.outputTokens > 0 {
			result = append(result, modelCostEntry{
				name:         prettifyModelName(name),
				cost:         md.cost,
				inputTokens:  md.inputTokens,
				outputTokens: md.outputTokens,
				color:        ModelColor(md.idx),
			})
		}
	}
	return result
}

func extractBudgetEntries(snap core.QuotaSnapshot, color lipgloss.Color, burnRate float64) []budgetEntry {
	var result []budgetEntry

	if m, ok := snap.Metrics["spend_limit"]; ok && m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		result = append(result, budgetEntry{
			name:     snap.AccountID,
			used:     *m.Used,
			limit:    *m.Limit,
			percent:  (*m.Used / *m.Limit) * 100,
			color:    color,
			burnRate: burnRate,
		})
	}

	if m, ok := snap.Metrics["plan_spend"]; ok && m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		// Avoid duplicate if we already have spend_limit
		if _, has := snap.Metrics["spend_limit"]; !has {
			result = append(result, budgetEntry{
				name:     snap.AccountID + " (plan)",
				used:     *m.Used,
				limit:    *m.Limit,
				percent:  (*m.Used / *m.Limit) * 100,
				color:    color,
				burnRate: burnRate,
			})
		}
	}

	if m, ok := snap.Metrics["credits"]; ok && m.Limit != nil && *m.Limit > 0 {
		used := float64(0)
		if m.Used != nil {
			used = *m.Used
		} else if m.Remaining != nil {
			used = *m.Limit - *m.Remaining
		}
		result = append(result, budgetEntry{
			name:     snap.AccountID + " (credits)",
			used:     used,
			limit:    *m.Limit,
			percent:  (used / *m.Limit) * 100,
			color:    color,
			burnRate: burnRate,
		})
	}

	return result
}

// ─── Sorting ────────────────────────────────────────────────────────────────

func sortProviders(providers []providerCostEntry, mode int) {
	switch mode {
	case analyticsSortCostDesc:
		sort.Slice(providers, func(i, j int) bool {
			return providers[i].cost > providers[j].cost
		})
	case analyticsSortNameAsc:
		sort.Slice(providers, func(i, j int) bool {
			return providers[i].name < providers[j].name
		})
	case analyticsSortTokensDesc:
		sort.Slice(providers, func(i, j int) bool {
			ti := float64(0)
			for _, m := range providers[i].models {
				ti += m.inputTokens + m.outputTokens
			}
			tj := float64(0)
			for _, m := range providers[j].models {
				tj += m.inputTokens + m.outputTokens
			}
			return ti > tj
		})
	}
}

func sortModels(models []modelCostEntry, mode int) {
	switch mode {
	case analyticsSortCostDesc:
		sort.Slice(models, func(i, j int) bool {
			return models[i].cost > models[j].cost
		})
	case analyticsSortNameAsc:
		sort.Slice(models, func(i, j int) bool {
			return models[i].name < models[j].name
		})
	case analyticsSortTokensDesc:
		sort.Slice(models, func(i, j int) bool {
			return (models[i].inputTokens + models[i].outputTokens) > (models[j].inputTokens + models[j].outputTokens)
		})
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// MAIN ANALYTICS RENDERING
// ═══════════════════════════════════════════════════════════════════════════

func (m Model) renderAnalyticsContent(w, h int) string {
	data := extractCostData(m.snapshots, m.analyticsFilter)

	// Apply sort
	sortProviders(data.providers, m.analyticsSortBy)
	sortModels(data.models, m.analyticsSortBy)

	var sb strings.Builder

	// ── Sub-tab bar ──
	renderAnalyticsTabBar(&sb, m.analyticsSubTab, w)

	// ── Status/config bar ──
	renderAnalyticsStatusBar(&sb, m.analyticsSortBy, m.analyticsFilter, w)

	// ── Tab content ──
	switch m.analyticsSubTab {
	case analyticsTabOverview:
		m.renderOverviewTab(&sb, data, w)
	case analyticsTabProviders:
		m.renderProvidersTab(&sb, data, w)
	case analyticsTabModels:
		m.renderModelsTab(&sb, data, w)
	case analyticsTabBudget:
		m.renderBudgetTab(&sb, data, w)
	case analyticsTabEfficiency:
		m.renderEfficiencyTab(&sb, data, w)
	}

	// ── Apply scrolling ──
	content := sb.String()
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	offset := m.analyticsScroll
	if offset > totalLines-h {
		offset = totalLines - h
	}
	if offset < 0 {
		offset = 0
	}

	end := offset + h
	if end > totalLines {
		end = totalLines
	}

	visible := lines[offset:end]
	for len(visible) < h {
		visible = append(visible, "")
	}

	result := strings.Join(visible, "\n")

	// Scroll indicators
	rlines := strings.Split(result, "\n")
	if offset > 0 && len(rlines) > 0 {
		arrow := lipgloss.NewStyle().Foreground(colorAccent).Render("  ▲ scroll up (" + fmt.Sprintf("%d", offset) + " lines)")
		rlines[0] = arrow
	}
	if end < totalLines && len(rlines) > 1 {
		remaining := totalLines - end
		arrow := lipgloss.NewStyle().Foreground(colorAccent).Render("  ▼ more below (" + fmt.Sprintf("%d", remaining) + " lines)")
		rlines[len(rlines)-1] = arrow
	}
	result = strings.Join(rlines, "\n")

	return result
}

// ─── Tab Bar ────────────────────────────────────────────────────────────────

func renderAnalyticsTabBar(sb *strings.Builder, activeTab int, w int) {
	var tabs []string
	for i, label := range analyticsTabLabels {
		key := analyticsTabKeys[i]
		tabStr := fmt.Sprintf(" %s:%s ", key, label)
		if i == activeTab {
			tabs = append(tabs, analyticsTabActiveStyle.Render(tabStr))
		} else {
			tabs = append(tabs, analyticsTabInactiveStyle.Render(tabStr))
		}
	}
	tabBar := "  " + strings.Join(tabs, "")
	sb.WriteString(tabBar + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("━", w-4)) + "\n")
}

// ─── Status / Config Bar ────────────────────────────────────────────────────

func renderAnalyticsStatusBar(sb *strings.Builder, sortBy int, filter string, w int) {
	sortLabel := sortByLabels[sortBy]

	parts := []string{
		analyticsSortLabelStyle.Render("↕ " + sortLabel),
	}

	if filter != "" {
		parts = append(parts,
			lipgloss.NewStyle().Foreground(colorSapphire).Render("🔍 "+filter))
	}

	left := "  " + strings.Join(parts, "  "+dimStyle.Render("│")+"  ")

	hints := dimStyle.Render("s:sort  /:filter  []:tab  ?:help")
	gap := w - lipgloss.Width(left) - lipgloss.Width(hints) - 2
	if gap < 1 {
		gap = 1
	}

	sb.WriteString(left + strings.Repeat(" ", gap) + hints + "\n\n")
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB 1: OVERVIEW
// ═══════════════════════════════════════════════════════════════════════════

func (m Model) renderOverviewTab(sb *strings.Builder, data costData, w int) {
	// ── Summary Cards ──
	renderSummaryCards(sb, data, w)
	sb.WriteString("\n")

	// ── Donut Chart (cost distribution) ──
	if len(data.providers) > 0 {
		renderSectionHeader(sb, "📊", "Cost Distribution", w)
		items := providerChartItems(data.providers, data.totalCost)
		if len(items) > 1 {
			sb.WriteString(RenderDonutChart(items, w, "total spend", formatUSD(data.totalCost)))
		} else if len(items) == 1 {
			sb.WriteString(RenderWaffleChart(items, w, ""))
		}
		sb.WriteString("\n")
	}

	// ── Vertical Bar Chart (provider comparison) ──
	if len(data.providers) > 0 && data.totalCost > 0 {
		renderSectionHeader(sb, "📈", "Provider Spend Comparison", w)
		items := providerChartItems(data.providers, data.totalCost)
		chartH := 12
		if len(items) > 6 {
			chartH = 16
		}
		sb.WriteString(RenderVerticalBarChart(items, w-4, chartH, ""))
		sb.WriteString("\n")

		// Legend
		sb.WriteString("  " + chartLegendTitleStyle.Render("Legend") + "\n")
		for _, item := range items {
			dot := lipgloss.NewStyle().Foreground(item.Color).Render("██")
			sb.WriteString(fmt.Sprintf("  %s %s  %s\n",
				dot,
				lipgloss.NewStyle().Foreground(item.Color).Render(item.Label),
				dimStyle.Render(item.SubLabel)))
		}
		sb.WriteString("\n")
	}

	// ── Top Spenders (leaderboard) ──
	if len(data.models) > 0 {
		costModels := make([]chartItem, 0)
		for _, m := range data.models {
			if m.cost > 0 {
				costModels = append(costModels, chartItem{
					Label:    m.name,
					Value:    m.cost,
					Color:    m.color,
					SubLabel: m.provider,
				})
			}
		}
		if len(costModels) > 0 {
			sort.Slice(costModels, func(i, j int) bool {
				return costModels[i].Value > costModels[j].Value
			})
			renderSectionHeader(sb, "🏆", "Top Spenders", w)
			sb.WriteString(RenderLeaderboard(costModels, w, 10, ""))
			sb.WriteString("\n")
		}
	}

	// ── Budget Overview ──
	if len(data.budgets) > 0 {
		renderSectionHeader(sb, "💳", "Budget Status", w)
		for _, b := range data.budgets {
			sb.WriteString(RenderBudgetGauge(b.name, b.used, b.limit, 30, 18, b.color, b.burnRate))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// ── Token Overview ──
	if data.totalInput > 0 || data.totalOutput > 0 {
		renderSectionHeader(sb, "🔤", "Token Usage", w)
		sb.WriteString(RenderTokenBreakdown(data.totalInput, data.totalOutput, w-4))
		sb.WriteString("\n\n")
	}

	// ── Empty state ──
	if data.totalCost == 0 && len(data.models) == 0 && len(data.budgets) == 0 {
		renderEmptyState(sb)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB 2: PROVIDERS (drill-down)
// ═══════════════════════════════════════════════════════════════════════════

func (m Model) renderProvidersTab(sb *strings.Builder, data costData, w int) {
	renderSummaryCards(sb, data, w)
	sb.WriteString("\n")

	if len(data.providers) == 0 {
		sb.WriteString(dimStyle.Render("  No providers with cost data.\n"))
		return
	}

	// ── Horizontal bar chart of all providers ──
	renderSectionHeader(sb, "💰", "Provider Spend", w)
	items := providerChartItems(data.providers, data.totalCost)
	barW := w - 46
	if barW < 10 {
		barW = 10
	}
	if barW > 50 {
		barW = 50
	}
	sb.WriteString(RenderHBarChart(items, barW, 18))
	sb.WriteString("\n\n")

	// ── Per-provider drill-down ──
	for _, prov := range data.providers {
		// Provider header card
		dot := lipgloss.NewStyle().Foreground(prov.color).Render("████")
		name := lipgloss.NewStyle().Foreground(prov.color).Bold(true).Render(prov.name)
		cost := lipgloss.NewStyle().Foreground(colorRosewater).Bold(true).Render(formatUSD(prov.cost))

		pctStr := ""
		if data.totalCost > 0 {
			pct := prov.cost / data.totalCost * 100
			pctStr = dimStyle.Render(fmt.Sprintf("  (%.1f%% of total)", pct))
		}

		sb.WriteString(fmt.Sprintf("\n  %s %s  %s%s\n", dot, name, cost, pctStr))
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(prov.color).Render(strings.Repeat("─", w-6)) + "\n")

		if len(prov.models) == 0 {
			sb.WriteString(dimStyle.Render("    No model breakdown available\n"))
			sb.WriteString("\n")
			continue
		}

		// Model breakdown bar chart within provider
		modelItems := make([]chartItem, 0, len(prov.models))
		for _, mdl := range prov.models {
			if mdl.cost > 0 {
				modelItems = append(modelItems, chartItem{
					Label: mdl.name,
					Value: mdl.cost,
					Color: mdl.color,
				})
			}
		}
		if len(modelItems) > 0 {
			innerBarW := w - 50
			if innerBarW < 8 {
				innerBarW = 8
			}
			if innerBarW > 30 {
				innerBarW = 30
			}
			sb.WriteString(RenderHBarChart(modelItems, innerBarW, 18))
			sb.WriteString("\n\n")
		}

		// Model detail table
		sb.WriteString("    " + dimStyle.Bold(true).Render(fmt.Sprintf("%-20s %10s %10s %10s %12s", "Model", "Input", "Output", "Total Tok", "Cost")) + "\n")
		sb.WriteString("    " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", 66)) + "\n")
		for _, mdl := range prov.models {
			nameStr := mdl.name
			if len(nameStr) > 20 {
				nameStr = nameStr[:19] + "…"
			}
			totalTok := mdl.inputTokens + mdl.outputTokens
			sb.WriteString(fmt.Sprintf("    %s %10s %10s %10s %12s\n",
				lipgloss.NewStyle().Foreground(mdl.color).Render(fmt.Sprintf("%-20s", nameStr)),
				dimStyle.Render(formatTokens(mdl.inputTokens)),
				dimStyle.Render(formatTokens(mdl.outputTokens)),
				lipgloss.NewStyle().Foreground(colorSubtext).Render(formatTokens(totalTok)),
				lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatUSD(mdl.cost))))
		}

		// Token breakdown mini chart
		totalIn, totalOut := float64(0), float64(0)
		for _, mdl := range prov.models {
			totalIn += mdl.inputTokens
			totalOut += mdl.outputTokens
		}
		if totalIn > 0 || totalOut > 0 {
			sb.WriteString("\n")
			sb.WriteString(RenderTokenBreakdown(totalIn, totalOut, w-8))
		}
		sb.WriteString("\n\n")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB 3: MODELS (comparison)
// ═══════════════════════════════════════════════════════════════════════════

func (m Model) renderModelsTab(sb *strings.Builder, data costData, w int) {
	renderSummaryCards(sb, data, w)
	sb.WriteString("\n")

	if len(data.models) == 0 {
		sb.WriteString(dimStyle.Render("  No model data available.\n"))
		return
	}

	// Aggregate by model name across providers
	type aggModel struct {
		name        string
		totalCost   float64
		totalInput  float64
		totalOutput float64
		providers   []string
		color       lipgloss.Color
	}

	agg := make(map[string]*aggModel)
	var order []string

	for _, mdl := range data.models {
		key := mdl.name
		a, ok := agg[key]
		if !ok {
			a = &aggModel{name: mdl.name, color: mdl.color}
			agg[key] = a
			order = append(order, key)
		}
		a.totalCost += mdl.cost
		a.totalInput += mdl.inputTokens
		a.totalOutput += mdl.outputTokens
		if mdl.provider != "" {
			a.providers = append(a.providers, mdl.provider)
		}
	}

	// Sort aggregated models
	switch m.analyticsSortBy {
	case analyticsSortCostDesc:
		sort.Slice(order, func(i, j int) bool {
			return agg[order[i]].totalCost > agg[order[j]].totalCost
		})
	case analyticsSortNameAsc:
		sort.Slice(order, func(i, j int) bool {
			return order[i] < order[j]
		})
	case analyticsSortTokensDesc:
		sort.Slice(order, func(i, j int) bool {
			ai := agg[order[i]]
			aj := agg[order[j]]
			return (ai.totalInput + ai.totalOutput) > (aj.totalInput + aj.totalOutput)
		})
	}

	// ── Vertical bar chart ──
	renderSectionHeader(sb, "🤖", "Model Cost Comparison", w)
	var chartItems []chartItem
	for _, key := range order {
		a := agg[key]
		if a.totalCost > 0 {
			chartItems = append(chartItems, chartItem{
				Label: a.name,
				Value: a.totalCost,
				Color: a.color,
			})
		}
	}
	if len(chartItems) > 0 {
		chartH := 12
		if len(chartItems) > 6 {
			chartH = 16
		}
		sb.WriteString(RenderVerticalBarChart(chartItems, w-4, chartH, ""))
		sb.WriteString("\n")
	}

	// ── Waffle distribution ──
	if len(chartItems) > 1 {
		renderSectionHeader(sb, "📊", "Model Cost Distribution", w)
		sb.WriteString(RenderWaffleChart(chartItems, w-4, ""))
		sb.WriteString("\n")
	}

	// ── Heatmap: model × provider ──
	if len(data.providers) > 1 && len(order) > 0 {
		renderSectionHeader(sb, "🗺️", "Cost Heatmap (Model × Provider)", w)

		// Build column labels (providers)
		var colLabels []string
		for _, p := range data.providers {
			colLabels = append(colLabels, p.name)
		}

		// Build rows
		maxCellVal := float64(0)
		var cells [][]heatmapCell
		var rowLabels []string
		for _, key := range order {
			rowLabels = append(rowLabels, key)
			var row []heatmapCell
			for _, p := range data.providers {
				found := false
				for _, mdl := range p.models {
					if mdl.name == key {
						row = append(row, heatmapCell{Value: mdl.cost, Label: formatUSD(mdl.cost)})
						if mdl.cost > maxCellVal {
							maxCellVal = mdl.cost
						}
						found = true
						break
					}
				}
				if !found {
					row = append(row, heatmapCell{Value: 0, Label: ""})
				}
			}
			cells = append(cells, row)
		}

		if maxCellVal == 0 {
			maxCellVal = 1
		}
		sb.WriteString(RenderHeatmap(rowLabels, colLabels, cells, w-4, maxCellVal, ""))
		sb.WriteString("\n")
	}

	// ── Detailed model cards ──
	renderSectionHeader(sb, "📋", "Model Details", w)
	for _, key := range order {
		a := agg[key]
		dot := lipgloss.NewStyle().Foreground(a.color).Render("████")
		name := lipgloss.NewStyle().Foreground(a.color).Bold(true).Render(a.name)

		sb.WriteString(fmt.Sprintf("\n  %s %s\n", dot, name))

		// Stats line
		var stats []string
		if a.totalCost > 0 {
			stats = append(stats, lipgloss.NewStyle().Foreground(colorRosewater).Bold(true).Render(formatUSD(a.totalCost)))
		}
		if a.totalInput > 0 {
			stats = append(stats, lipgloss.NewStyle().Foreground(colorSapphire).Render(fmt.Sprintf("%s in", formatTokens(a.totalInput))))
		}
		if a.totalOutput > 0 {
			stats = append(stats, lipgloss.NewStyle().Foreground(colorPeach).Render(fmt.Sprintf("%s out", formatTokens(a.totalOutput))))
		}
		totalTok := a.totalInput + a.totalOutput
		if totalTok > 0 && a.totalCost > 0 {
			eff := a.totalCost / (totalTok / 1000)
			stats = append(stats, lipgloss.NewStyle().Foreground(colorTeal).Render(fmt.Sprintf("$%.4f/1K tok", eff)))
		}
		if len(a.providers) > 0 {
			unique := uniqueStrings(a.providers)
			stats = append(stats, dimStyle.Render("via "+strings.Join(unique, ", ")))
		}
		if len(stats) > 0 {
			sb.WriteString("    " + strings.Join(stats, "  "+dimStyle.Render("·")+"  ") + "\n")
		}

		// Token breakdown
		if a.totalInput > 0 || a.totalOutput > 0 {
			sb.WriteString(RenderTokenBreakdown(a.totalInput, a.totalOutput, w-8))
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB 4: BUDGET (tracking + projections)
// ═══════════════════════════════════════════════════════════════════════════

func (m Model) renderBudgetTab(sb *strings.Builder, data costData, w int) {
	renderSummaryCards(sb, data, w)
	sb.WriteString("\n")

	if len(data.budgets) == 0 {
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render("  No budget data available.\n"))
		sb.WriteString(dimStyle.Render("  Budgets are shown for providers that report spend limits,\n"))
		sb.WriteString(dimStyle.Render("  plan spend caps, or credit balances.\n"))
		sb.WriteString("\n")
		return
	}

	// ── Budget Gauges ──
	renderSectionHeader(sb, "💳", "Budget Utilization", w)
	sb.WriteString("\n")

	for _, b := range data.budgets {
		barW := w - 50
		if barW < 10 {
			barW = 10
		}
		if barW > 40 {
			barW = 40
		}
		sb.WriteString(RenderBudgetGauge(b.name, b.used, b.limit, barW, 18, b.color, b.burnRate))
		sb.WriteString("\n\n")
	}

	// ── Budget Summary Ring ──
	renderSectionHeader(sb, "🎯", "Budget Overview", w)
	sb.WriteString("\n")

	for _, b := range data.budgets {
		sb.WriteString(fmt.Sprintf("  %s\n",
			lipgloss.NewStyle().Foreground(b.color).Bold(true).Render(b.name)))
		sb.WriteString(RenderProgressRing(b.name, b.percent, b.used, b.limit, w-4, b.color))
		sb.WriteString("\n\n")
	}

	// ── Projection Panel ──
	if data.burnRate > 0 {
		renderSectionHeader(sb, "🔮", "Burn Rate Projection", w)
		sb.WriteString("\n")

		// Daily cost
		dailyCost := data.burnRate * 24
		weeklyCost := dailyCost * 7
		monthlyCost := dailyCost * 30

		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			dimStyle.Render("Current burn rate:"),
			lipgloss.NewStyle().Foreground(colorPeach).Bold(true).Render(fmt.Sprintf("$%.2f/hour", data.burnRate))))
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			dimStyle.Render("Projected daily:  "),
			lipgloss.NewStyle().Foreground(colorYellow).Render(formatUSD(dailyCost))))
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			dimStyle.Render("Projected weekly: "),
			lipgloss.NewStyle().Foreground(colorYellow).Render(formatUSD(weeklyCost))))
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			dimStyle.Render("Projected monthly:"),
			lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(formatUSD(monthlyCost))))
		sb.WriteString("\n")

		// Generate a simulated burn sparkline (showing projected spending over 30 days)
		var burnData []float64
		for i := 0; i < 30; i++ {
			burnData = append(burnData, data.burnRate*float64(i+1)*24)
		}
		renderSectionHeader(sb, "📉", "30-Day Projected Cumulative Spend", w)
		sb.WriteString(RenderAreaSparkline(burnData, w-4, colorPeach, "Projected"))
		sb.WriteString("\n")

		// Budget exhaustion for each budget
		for _, b := range data.budgets {
			if b.burnRate > 0 {
				remaining := b.limit - b.used
				if remaining > 0 {
					hoursLeft := remaining / b.burnRate
					daysLeft := hoursLeft / 24

					sb.WriteString(fmt.Sprintf("  %s: ",
						lipgloss.NewStyle().Foreground(b.color).Bold(true).Render(b.name)))

					if daysLeft < 3 {
						sb.WriteString(lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(
							fmt.Sprintf("⚠ CRITICAL: Budget exhausted in %.0f hours (%.1f days)", hoursLeft, daysLeft)))
					} else if daysLeft < 14 {
						sb.WriteString(lipgloss.NewStyle().Foreground(colorYellow).Render(
							fmt.Sprintf("⚠ Budget exhausted in ~%.0f days", daysLeft)))
					} else {
						sb.WriteString(lipgloss.NewStyle().Foreground(colorGreen).Render(
							fmt.Sprintf("✓ ~%.0f days remaining", daysLeft)))
					}
					sb.WriteString("\n")
				} else {
					sb.WriteString(fmt.Sprintf("  %s: %s\n",
						lipgloss.NewStyle().Foreground(b.color).Bold(true).Render(b.name),
						lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render("🔴 BUDGET EXCEEDED")))
				}
			}
		}
		sb.WriteString("\n")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB 5: EFFICIENCY (cost per token analysis)
// ═══════════════════════════════════════════════════════════════════════════

func (m Model) renderEfficiencyTab(sb *strings.Builder, data costData, w int) {
	renderSummaryCards(sb, data, w)
	sb.WriteString("\n")

	// Build efficiency data
	type effEntry struct {
		name         string
		provider     string
		costPerK     float64
		totalTokens  float64
		cost         float64
		inputTokens  float64
		outputTokens float64
		color        lipgloss.Color
	}

	var entries []effEntry
	for _, mdl := range data.models {
		totalTok := mdl.inputTokens + mdl.outputTokens
		if totalTok > 0 && mdl.cost > 0 {
			entries = append(entries, effEntry{
				name:         mdl.name,
				provider:     mdl.provider,
				costPerK:     mdl.cost / (totalTok / 1000),
				totalTokens:  totalTok,
				cost:         mdl.cost,
				inputTokens:  mdl.inputTokens,
				outputTokens: mdl.outputTokens,
				color:        mdl.color,
			})
		}
	}

	if len(entries) == 0 {
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render("  No efficiency data available.\n"))
		sb.WriteString(dimStyle.Render("  Efficiency analysis requires models with both token usage and cost data.\n"))
		sb.WriteString("\n")
		return
	}

	// Sort by cost per 1K tokens (most expensive first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].costPerK > entries[j].costPerK
	})

	// ── Cost per 1K tokens chart ──
	renderSectionHeader(sb, "⚙️", "Cost per 1K Tokens (higher = more expensive)", w)
	var effItems []chartItem
	for _, e := range entries {
		effItems = append(effItems, chartItem{
			Label:    e.name,
			Value:    e.costPerK,
			Color:    e.color,
			SubLabel: fmt.Sprintf("%.0fK tok", e.totalTokens/1000),
		})
	}
	barW := w - 46
	if barW < 10 {
		barW = 10
	}
	if barW > 40 {
		barW = 40
	}
	sb.WriteString(RenderEfficiencyChart(effItems, barW, 22))
	sb.WriteString("\n\n")

	// ── Value for Money ranking ──
	// Sort by total cost for best "bang for buck"
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].costPerK < entries[j].costPerK
	})

	renderSectionHeader(sb, "💎", "Best Value (cheapest per 1K tokens first)", w)
	sb.WriteString("\n")

	for i, e := range entries {
		rank := dimStyle.Render(fmt.Sprintf(" %d.", i+1))
		if i == 0 {
			rank = lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render("🏆")
		}
		name := lipgloss.NewStyle().Foreground(e.color).Render(fmt.Sprintf("%-22s", e.name))
		costPerK := lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(fmt.Sprintf("$%.4f/1K", e.costPerK))
		totalCost := lipgloss.NewStyle().Foreground(colorRosewater).Render(formatUSD(e.cost))
		tokens := dimStyle.Render(fmt.Sprintf("%s tokens", formatTokens(e.totalTokens)))
		prov := dimStyle.Render(fmt.Sprintf("(%s)", e.provider))

		sb.WriteString(fmt.Sprintf("  %s %s %s  %s  %s  %s\n",
			rank, name, costPerK, totalCost, tokens, prov))
	}
	sb.WriteString("\n")

	// ── Comparison table if there are 2+ models ──
	if len(entries) >= 2 {
		renderSectionHeader(sb, "⚖️", "Head-to-Head Comparison", w)

		// Compare top 2 most used models
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].totalTokens > entries[j].totalTokens
		})

		a, b := entries[0], entries[1]
		rows := []comparisonRow{
			{Label: "Cost / 1K tokens", Left: fmt.Sprintf("$%.4f", a.costPerK), Right: fmt.Sprintf("$%.4f", b.costPerK), LeftV: a.costPerK, RightV: b.costPerK},
			{Label: "Total Cost", Left: formatUSD(a.cost), Right: formatUSD(b.cost), LeftV: a.cost, RightV: b.cost},
			{Label: "Input Tokens", Left: formatTokens(a.inputTokens), Right: formatTokens(b.inputTokens), LeftV: a.inputTokens, RightV: b.inputTokens},
			{Label: "Output Tokens", Left: formatTokens(a.outputTokens), Right: formatTokens(b.outputTokens), LeftV: a.outputTokens, RightV: b.outputTokens},
			{Label: "Total Tokens", Left: formatTokens(a.totalTokens), Right: formatTokens(b.totalTokens), LeftV: a.totalTokens, RightV: b.totalTokens},
			{Label: "Provider", Left: a.provider, Right: b.provider},
		}

		sb.WriteString(RenderComparisonTable(a.name, b.name, rows, w-4))
		sb.WriteString("\n")
	}

	// ── Input vs Output Ratio ──
	renderSectionHeader(sb, "📐", "Input vs Output Token Ratio", w)
	sb.WriteString("\n")

	for _, e := range entries {
		total := e.inputTokens + e.outputTokens
		if total == 0 {
			continue
		}
		inPct := e.inputTokens / total * 100
		outPct := e.outputTokens / total * 100

		name := lipgloss.NewStyle().Foreground(e.color).Render(fmt.Sprintf("%-18s", e.name))

		// Mini stacked bar for ratio
		ratioBarW := 20
		inW := int(math.Round(inPct / 100 * float64(ratioBarW)))
		if inW < 0 {
			inW = 0
		}
		outW := ratioBarW - inW

		inBar := lipgloss.NewStyle().Foreground(colorSapphire).Render(strings.Repeat("█", inW))
		outBar := lipgloss.NewStyle().Foreground(colorPeach).Render(strings.Repeat("█", outW))

		ratio := fmt.Sprintf("%s in / %s out",
			lipgloss.NewStyle().Foreground(colorSapphire).Render(fmt.Sprintf("%.0f%%", inPct)),
			lipgloss.NewStyle().Foreground(colorPeach).Render(fmt.Sprintf("%.0f%%", outPct)))

		sb.WriteString(fmt.Sprintf("  %s %s%s  %s\n", name, inBar, outBar, ratio))
	}
	sb.WriteString("\n")
	sb.WriteString("  " + dimStyle.Render("██ Input") + "  " + dimStyle.Render("██ Output") + "\n")
	sb.WriteString("\n")
}

// ═══════════════════════════════════════════════════════════════════════════
// SHARED COMPONENTS
// ═══════════════════════════════════════════════════════════════════════════

func renderSummaryCards(sb *strings.Builder, data costData, w int) {
	cards := []struct {
		title, value, subtitle string
		color                  lipgloss.Color
	}{
		{
			"Total Spend",
			formatUSD(data.totalCost),
			fmt.Sprintf("across %d accounts", data.providerCount),
			colorRosewater,
		},
		{
			"Providers",
			fmt.Sprintf("%d", data.providerCount),
			fmt.Sprintf("%d active", data.activeCount),
			colorGreen,
		},
		{
			"Models",
			fmt.Sprintf("%d", data.modelCount),
			fmt.Sprintf("%.0fK tokens", (data.totalInput+data.totalOutput)/1000),
			colorSapphire,
		},
	}

	// Add burn rate card if available
	if data.burnRate > 0 {
		cards = append(cards, struct {
			title, value, subtitle string
			color                  lipgloss.Color
		}{
			"Burn Rate",
			fmt.Sprintf("$%.2f/h", data.burnRate),
			"current rate",
			colorPeach,
		})
	}

	numCards := len(cards)
	cardW := (w - 2 - (numCards-1)*2) / numCards
	if cardW < 16 {
		cardW = 16
	}
	if cardW > 24 {
		cardW = 24
	}

	// Determine layout: single row or wrap
	totalRowW := numCards*cardW + (numCards-1)*2
	cardsPerRow := numCards
	if totalRowW > w-2 {
		cardsPerRow = (w - 2) / (cardW + 2)
		if cardsPerRow < 1 {
			cardsPerRow = 1
		}
	}

	var rows []string
	for i := 0; i < numCards; i += cardsPerRow {
		end := i + cardsPerRow
		if end > numCards {
			end = numCards
		}
		var rowCards []string
		for j := i; j < end; j++ {
			c := cards[j]
			rowCards = append(rowCards, RenderSummaryCard(c.title, c.value, c.subtitle, cardW, c.color))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top,
			intersperse(rowCards, "  ")...))
	}

	sb.WriteString(" " + strings.Join(rows, "\n "))
	sb.WriteString("\n")
}

func renderSectionHeader(sb *strings.Builder, icon, title string, w int) {
	styled := lipgloss.NewStyle().Bold(true).Foreground(colorBlue).Render("  " + icon + " " + title + " ")
	lineLen := w - lipgloss.Width(styled) - 1
	if lineLen < 2 {
		lineLen = 2
	}
	line := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", lineLen))
	sb.WriteString(styled + line + "\n")
}

func renderEmptyState(sb *strings.Builder) {
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("  No cost or usage data available.\n"))
	sb.WriteString(dimStyle.Render("  Cost analytics require providers that report spend, token usage,\n"))
	sb.WriteString(dimStyle.Render("  or budget metrics (e.g., Cursor, Claude Code, OpenAI, Anthropic).\n"))
	sb.WriteString("\n")
}

// ─── Chart Item Builders ────────────────────────────────────────────────────

func providerChartItems(providers []providerCostEntry, total float64) []chartItem {
	var items []chartItem
	for _, p := range providers {
		if p.cost <= 0 {
			continue
		}
		pctStr := ""
		if total > 0 {
			pctStr = fmt.Sprintf("(%.1f%%)", p.cost/total*100)
		}
		items = append(items, chartItem{
			Label:    p.name,
			Value:    p.cost,
			Color:    p.color,
			SubLabel: pctStr,
		})
	}
	return items
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
