package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/agentusage/internal/core"
)

// ─── Sort Modes ─────────────────────────────────────────────────────────────

const (
	analyticsSortCostDesc   = 0
	analyticsSortNameAsc    = 1
	analyticsSortTokensDesc = 2
	analyticsSortCount      = 3
)

var sortByLabels = []string{"Cost ↓", "Name ↑", "Tokens ↓"}

// ─── Extracted Data ─────────────────────────────────────────────────────────

type costData struct {
	totalCost     float64
	totalInput    float64
	totalOutput   float64
	burnRate      float64
	providerCount int
	activeCount   int
	providers     []providerCostEntry
	models        []modelCostEntry
	budgets       []budgetEntry
}

type providerCostEntry struct {
	name   string
	cost   float64
	color  lipgloss.Color
	models []modelCostEntry
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
	color    lipgloss.Color
	burnRate float64
}

// ═══════════════════════════════════════════════════════════════════════════
// DATA EXTRACTION — handles every provider's metric naming convention
// ═══════════════════════════════════════════════════════════════════════════

func extractCostData(snapshots map[string]core.QuotaSnapshot, filter string) costData {
	var data costData
	lowerFilter := strings.ToLower(filter)
	modelIdx := 0

	for _, snap := range snapshots {
		if filter != "" {
			if !strings.Contains(strings.ToLower(snap.AccountID), lowerFilter) &&
				!strings.Contains(strings.ToLower(snap.ProviderID), lowerFilter) {
				continue
			}
		}

		data.providerCount++
		if snap.Status == core.StatusOK || snap.Status == core.StatusNearLimit {
			data.activeCount++
		}

		provColor := ProviderColor(snap.ProviderID)
		cost := extractProviderCost(snap)
		data.totalCost += cost
		data.burnRate += extractBurnRate(snap)

		// Extract model-level data from ALL known patterns
		models := extractAllModels(snap, provColor, &modelIdx)
		for i := range models {
			data.totalInput += models[i].inputTokens
			data.totalOutput += models[i].outputTokens
		}

		data.providers = append(data.providers, providerCostEntry{
			name:   snap.AccountID,
			cost:   cost,
			color:  provColor,
			models: models,
		})

		// Budget data
		data.budgets = append(data.budgets, extractBudgets(snap, provColor, extractBurnRate(snap))...)
	}

	// Flatten models
	for _, p := range data.providers {
		data.models = append(data.models, p.models...)
	}

	return data
}

func extractProviderCost(snap core.QuotaSnapshot) float64 {
	// Try direct cost keys first
	for _, key := range []string{
		"daily_cost_usd", "total_cost_usd", "jsonl_total_cost_usd",
		"block_cost_usd", "spend_limit", "plan_spend",
		"plan_total_spend_usd", "credits",
	} {
		if m, ok := snap.Metrics[key]; ok && m.Used != nil && *m.Used > 0 {
			return *m.Used
		}
	}

	// Sum all model_*_cost and model_*_cost_usd
	total := 0.0
	for key, m := range snap.Metrics {
		if m.Used == nil || *m.Used <= 0 {
			continue
		}
		if strings.HasPrefix(key, "model_") && (strings.HasSuffix(key, "_cost") || strings.HasSuffix(key, "_cost_usd")) {
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

// extractAllModels pulls model data from every naming convention:
//
//   - model_<name>_cost_usd + model_<name>_input_tokens (OpenRouter: all in Metrics)
//   - model_<intent>_cost + model_<intent>_input_tokens in Raw (Cursor: cost in Metrics, tokens in Raw)
//   - input_tokens_<model> + output_tokens_<model> (Claude Code stats-cache)
func extractAllModels(snap core.QuotaSnapshot, provColor lipgloss.Color, idxCounter *int) []modelCostEntry {
	type md struct {
		cost   float64
		input  float64
		output float64
	}
	models := make(map[string]*md)
	var order []string

	ensure := func(name string) *md {
		if _, ok := models[name]; !ok {
			models[name] = &md{}
			order = append(order, name)
		}
		return models[name]
	}

	// ── Pattern 1: model_<X>_cost_usd / model_<X>_cost (Metrics) ──
	for key, m := range snap.Metrics {
		if !strings.HasPrefix(key, "model_") {
			continue
		}
		name := strings.TrimPrefix(key, "model_")

		switch {
		case strings.HasSuffix(name, "_cost_usd"):
			name = strings.TrimSuffix(name, "_cost_usd")
			if m.Used != nil && *m.Used > 0 {
				ensure(name).cost += *m.Used
			}
		case strings.HasSuffix(name, "_cost"):
			name = strings.TrimSuffix(name, "_cost")
			if m.Used != nil && *m.Used > 0 {
				ensure(name).cost += *m.Used
			}
		case strings.HasSuffix(name, "_input_tokens"):
			name = strings.TrimSuffix(name, "_input_tokens")
			if m.Used != nil {
				ensure(name).input += *m.Used
			}
		case strings.HasSuffix(name, "_output_tokens"):
			name = strings.TrimSuffix(name, "_output_tokens")
			if m.Used != nil {
				ensure(name).output += *m.Used
			}
		}
	}

	// ── Pattern 2: model_<X>_input_tokens / model_<X>_output_tokens in Raw (Cursor) ──
	for key, val := range snap.Raw {
		if !strings.HasPrefix(key, "model_") {
			continue
		}
		name := strings.TrimPrefix(key, "model_")

		switch {
		case strings.HasSuffix(name, "_input_tokens"):
			name = strings.TrimSuffix(name, "_input_tokens")
			if v, err := strconv.ParseFloat(val, 64); err == nil && v > 0 {
				m := ensure(name)
				if m.input == 0 { // don't override Metrics value
					m.input = v
				}
			}
		case strings.HasSuffix(name, "_output_tokens"):
			name = strings.TrimSuffix(name, "_output_tokens")
			if v, err := strconv.ParseFloat(val, 64); err == nil && v > 0 {
				m := ensure(name)
				if m.output == 0 {
					m.output = v
				}
			}
		}
	}

	// ── Pattern 3: input_tokens_<X> / output_tokens_<X> (Claude Code stats-cache) ──
	for key, m := range snap.Metrics {
		switch {
		case strings.HasPrefix(key, "input_tokens_"):
			name := strings.TrimPrefix(key, "input_tokens_")
			if m.Used != nil && *m.Used > 0 {
				ensure(name).input += *m.Used
			}
		case strings.HasPrefix(key, "output_tokens_"):
			name := strings.TrimPrefix(key, "output_tokens_")
			if m.Used != nil && *m.Used > 0 {
				ensure(name).output += *m.Used
			}
		}
	}

	// Build result, skip empties
	var result []modelCostEntry
	for _, name := range order {
		d := models[name]
		if d.cost > 0 || d.input > 0 || d.output > 0 {
			result = append(result, modelCostEntry{
				name:         prettifyModelName(name),
				provider:     snap.AccountID,
				cost:         d.cost,
				inputTokens:  d.input,
				outputTokens: d.output,
				color:        ModelColor(*idxCounter),
			})
			*idxCounter++
		}
	}
	return result
}

func extractBudgets(snap core.QuotaSnapshot, color lipgloss.Color, burnRate float64) []budgetEntry {
	var result []budgetEntry

	if m, ok := snap.Metrics["spend_limit"]; ok && m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		result = append(result, budgetEntry{
			name: snap.AccountID, used: *m.Used, limit: *m.Limit,
			color: color, burnRate: burnRate,
		})
	}

	if m, ok := snap.Metrics["plan_spend"]; ok && m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		if _, has := snap.Metrics["spend_limit"]; !has {
			result = append(result, budgetEntry{
				name: snap.AccountID + " (plan)", used: *m.Used, limit: *m.Limit,
				color: color, burnRate: burnRate,
			})
		}
	}

	if m, ok := snap.Metrics["credits"]; ok && m.Limit != nil && *m.Limit > 0 {
		used := 0.0
		if m.Used != nil {
			used = *m.Used
		} else if m.Remaining != nil {
			used = *m.Limit - *m.Remaining
		}
		result = append(result, budgetEntry{
			name: snap.AccountID + " (credits)", used: used, limit: *m.Limit,
			color: color, burnRate: burnRate,
		})
	}

	return result
}

// ─── Sorting ────────────────────────────────────────────────────────────────

func sortProviders(providers []providerCostEntry, mode int) {
	switch mode {
	case analyticsSortCostDesc:
		sort.Slice(providers, func(i, j int) bool { return providers[i].cost > providers[j].cost })
	case analyticsSortNameAsc:
		sort.Slice(providers, func(i, j int) bool { return providers[i].name < providers[j].name })
	case analyticsSortTokensDesc:
		sort.Slice(providers, func(i, j int) bool {
			return provTokens(providers[i]) > provTokens(providers[j])
		})
	}
}

func provTokens(p providerCostEntry) float64 {
	t := 0.0
	for _, m := range p.models {
		t += m.inputTokens + m.outputTokens
	}
	return t
}

func sortModels(models []modelCostEntry, mode int) {
	switch mode {
	case analyticsSortCostDesc:
		sort.Slice(models, func(i, j int) bool { return models[i].cost > models[j].cost })
	case analyticsSortNameAsc:
		sort.Slice(models, func(i, j int) bool { return models[i].name < models[j].name })
	case analyticsSortTokensDesc:
		sort.Slice(models, func(i, j int) bool {
			return (models[i].inputTokens + models[i].outputTokens) > (models[j].inputTokens + models[j].outputTokens)
		})
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// RENDERING — one scrollable analytics dashboard
// ═══════════════════════════════════════════════════════════════════════════

func (m Model) renderAnalyticsContent(w, h int) string {
	data := extractCostData(m.snapshots, m.analyticsFilter)
	sortProviders(data.providers, m.analyticsSortBy)
	sortModels(data.models, m.analyticsSortBy)

	var sb strings.Builder

	// ── Sort / filter status bar ──
	renderStatusBar(&sb, m.analyticsSortBy, m.analyticsFilter, w)

	// ── Summary cards ──
	renderCards(&sb, data, w)
	sb.WriteString("\n")

	// ── Provider cost chart (always shown if there's cost data) ──
	if len(data.providers) > 0 && data.totalCost > 0 {
		provItems := toProviderItems(data.providers, data.totalCost)

		renderSection(&sb, "💰", "Provider Spend", w)
		barW := w - 46
		barW = clampInt(barW, 10, 50)
		sb.WriteString(RenderHBarChart(provItems, barW, 18))
		sb.WriteString("\n\n")

		// Vertical bar chart for visual comparison
		if len(provItems) >= 2 {
			renderSection(&sb, "📊", "Provider Comparison", w)
			chartH := clampInt(len(provItems)*3, 8, 18)
			sb.WriteString(RenderVerticalBarChart(provItems, w-4, chartH, ""))
			sb.WriteString("\n")

			// Legend
			for _, item := range provItems {
				dot := lipgloss.NewStyle().Foreground(item.Color).Render("██")
				sb.WriteString(fmt.Sprintf("    %s %s  %s  %s\n",
					dot,
					lipgloss.NewStyle().Foreground(item.Color).Width(18).Render(item.Label),
					lipgloss.NewStyle().Foreground(colorRosewater).Bold(true).Render(formatUSD(item.Value)),
					dimStyle.Render(item.SubLabel)))
			}
			sb.WriteString("\n")
		}

		// Cost distribution (stacked bar)
		if len(provItems) >= 2 {
			renderSection(&sb, "🍩", "Cost Distribution", w)
			sb.WriteString(RenderDistributionBar(provItems, w-4))
			sb.WriteString("\n\n")
		}
	}

	// ── Model cost chart ──
	costModels := filterCostModels(data.models)
	if len(costModels) > 0 {
		modelItems := toModelItems(costModels)
		sortChartItems(modelItems)

		renderSection(&sb, "🤖", "Model Spend", w)
		barW := w - 46
		barW = clampInt(barW, 10, 50)
		sb.WriteString(RenderHBarChart(modelItems, barW, 22))
		sb.WriteString("\n\n")

		// Vertical bar chart if 2+
		if len(modelItems) >= 2 {
			renderSection(&sb, "📈", "Model Comparison", w)
			chartH := clampInt(len(modelItems)*2+4, 8, 18)
			sb.WriteString(RenderVerticalBarChart(modelItems, w-4, chartH, ""))
			sb.WriteString("\n")

			// Legend
			for _, item := range modelItems {
				dot := lipgloss.NewStyle().Foreground(item.Color).Render("██")
				sb.WriteString(fmt.Sprintf("    %s %s  %s  %s\n",
					dot,
					lipgloss.NewStyle().Foreground(item.Color).Width(22).Render(item.Label),
					lipgloss.NewStyle().Foreground(colorRosewater).Bold(true).Render(formatUSD(item.Value)),
					dimStyle.Render(item.SubLabel)))
			}
			sb.WriteString("\n")
		}

		// Leaderboard
		if len(modelItems) >= 3 {
			renderSection(&sb, "🏆", "Top Spenders", w)
			sb.WriteString(RenderLeaderboard(modelItems, w, 10, ""))
			sb.WriteString("\n")
		}
	}

	// ── Token usage table ──
	tokenModels := filterTokenModels(data.models)
	if len(tokenModels) > 0 {
		renderSection(&sb, "🔤", "Token Usage by Model", w)
		renderTokenTable(&sb, tokenModels, w)
		sb.WriteString("\n")

		// Token I/O breakdown
		if data.totalInput > 0 || data.totalOutput > 0 {
			renderSection(&sb, "📐", "Input vs Output Tokens", w)
			sb.WriteString(RenderTokenBreakdown(data.totalInput, data.totalOutput, w-4))
			sb.WriteString("\n\n")
		}
	}

	// ── Cost efficiency ──
	effItems := buildEfficiencyItems(data.models)
	if len(effItems) > 0 {
		renderSection(&sb, "⚙️", "Cost Efficiency ($/1K tokens)", w)
		barW := w - 46
		barW = clampInt(barW, 10, 40)
		sb.WriteString(RenderEfficiencyChart(effItems, barW, 22))
		sb.WriteString("\n\n")
	}

	// ── Budget utilization ──
	if len(data.budgets) > 0 {
		renderSection(&sb, "💳", "Budget Utilization", w)
		for _, b := range data.budgets {
			barW := w - 50
			barW = clampInt(barW, 10, 40)
			sb.WriteString(RenderBudgetGauge(b.name, b.used, b.limit, barW, 18, b.color, b.burnRate))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// ── Burn rate projection ──
	if data.burnRate > 0 {
		renderSection(&sb, "🔮", "Burn Rate Projection", w)
		daily := data.burnRate * 24
		weekly := daily * 7
		monthly := daily * 30
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			dimStyle.Width(22).Render("Current burn rate:"),
			lipgloss.NewStyle().Foreground(colorPeach).Bold(true).Render(fmt.Sprintf("$%.2f/hour", data.burnRate))))
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			dimStyle.Width(22).Render("Projected daily:"),
			lipgloss.NewStyle().Foreground(colorYellow).Render(formatUSD(daily))))
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			dimStyle.Width(22).Render("Projected weekly:"),
			lipgloss.NewStyle().Foreground(colorYellow).Render(formatUSD(weekly))))
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			dimStyle.Width(22).Render("Projected monthly:"),
			lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(formatUSD(monthly))))

		// Projected cumulative sparkline (30 days)
		var sparkData []float64
		for i := 1; i <= 30; i++ {
			sparkData = append(sparkData, data.burnRate*float64(i)*24)
		}
		sb.WriteString("\n")
		sb.WriteString(RenderAreaSparkline(sparkData, w-4, colorPeach, "30-day projected"))

		// Budget exhaustion warnings
		for _, b := range data.budgets {
			if b.burnRate > 0 {
				remaining := b.limit - b.used
				if remaining > 0 {
					daysLeft := remaining / b.burnRate / 24
					sb.WriteString(fmt.Sprintf("\n  %s: ",
						lipgloss.NewStyle().Foreground(b.color).Bold(true).Render(b.name)))
					if daysLeft < 3 {
						sb.WriteString(lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(
							fmt.Sprintf("⚠ %.0f hours left!", remaining/b.burnRate)))
					} else if daysLeft < 14 {
						sb.WriteString(lipgloss.NewStyle().Foreground(colorYellow).Render(
							fmt.Sprintf("⚠ ~%.0f days left", daysLeft)))
					} else {
						sb.WriteString(lipgloss.NewStyle().Foreground(colorGreen).Render(
							fmt.Sprintf("✓ ~%.0f days left", daysLeft)))
					}
				}
			}
		}
		sb.WriteString("\n\n")
	}

	// ── Per-provider drill-down (if models exist) ──
	for _, prov := range data.providers {
		if len(prov.models) == 0 {
			continue
		}
		dot := lipgloss.NewStyle().Foreground(prov.color).Render("████")
		name := lipgloss.NewStyle().Foreground(prov.color).Bold(true).Render(prov.name)
		cost := lipgloss.NewStyle().Foreground(colorRosewater).Bold(true).Render(formatUSD(prov.cost))
		pctStr := ""
		if data.totalCost > 0 {
			pctStr = dimStyle.Render(fmt.Sprintf("  (%.1f%%)", prov.cost/data.totalCost*100))
		}
		sb.WriteString(fmt.Sprintf("\n  %s %s  %s%s\n", dot, name, cost, pctStr))
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(prov.color).Render(strings.Repeat("─", w-6)) + "\n")

		// Model table for this provider
		nameW := 22
		colW := 10
		sb.WriteString(fmt.Sprintf("  %-*s %*s %*s %*s\n",
			nameW, dimStyle.Bold(true).Render("Model"),
			colW, dimStyle.Bold(true).Render("Input"),
			colW, dimStyle.Bold(true).Render("Output"),
			colW, dimStyle.Bold(true).Render("Cost")))
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", nameW+colW*3+3)) + "\n")

		for _, mdl := range prov.models {
			n := mdl.name
			if len(n) > nameW {
				n = n[:nameW-1] + "…"
			}
			sb.WriteString(fmt.Sprintf("  %s %*s %*s %*s\n",
				lipgloss.NewStyle().Foreground(mdl.color).Width(nameW).Render(n),
				colW, dimStyle.Render(formatTokens(mdl.inputTokens)),
				colW, dimStyle.Render(formatTokens(mdl.outputTokens)),
				colW, lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatUSD(mdl.cost))))
		}
		sb.WriteString("\n")
	}

	// ── Empty state ──
	if data.totalCost == 0 && len(data.models) == 0 && len(data.budgets) == 0 {
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render("  No cost or usage data available.\n"))
		sb.WriteString(dimStyle.Render("  Analytics requires providers that report spend, tokens, or budgets.\n"))
		sb.WriteString("\n")
	}

	// ── Scrolling ──
	content := sb.String()
	lines := strings.Split(content, "\n")
	total := len(lines)

	offset := m.analyticsScroll
	if offset > total-h {
		offset = total - h
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + h
	if end > total {
		end = total
	}

	visible := lines[offset:end]
	for len(visible) < h {
		visible = append(visible, "")
	}

	result := strings.Join(visible, "\n")

	rlines := strings.Split(result, "\n")
	if offset > 0 && len(rlines) > 0 {
		rlines[0] = lipgloss.NewStyle().Foreground(colorAccent).Render(
			fmt.Sprintf("  ▲ scroll up (%d lines)", offset))
	}
	if end < total && len(rlines) > 1 {
		rlines[len(rlines)-1] = lipgloss.NewStyle().Foreground(colorAccent).Render(
			fmt.Sprintf("  ▼ more below (%d lines)", total-end))
	}

	return strings.Join(rlines, "\n")
}

// ─── Status Bar ─────────────────────────────────────────────────────────────

func renderStatusBar(sb *strings.Builder, sortBy int, filter string, w int) {
	parts := []string{
		analyticsSortLabelStyle.Render("↕ " + sortByLabels[sortBy]),
	}
	if filter != "" {
		parts = append(parts,
			lipgloss.NewStyle().Foreground(colorSapphire).Render("🔍 "+filter))
	}
	left := "  " + strings.Join(parts, "  "+dimStyle.Render("│")+"  ")
	hints := dimStyle.Render("s:sort  /:filter  g/G:top/btm  ?:help")
	gap := w - lipgloss.Width(left) - lipgloss.Width(hints) - 2
	if gap < 1 {
		gap = 1
	}
	sb.WriteString(left + strings.Repeat(" ", gap) + hints + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")
}

// ─── Summary Cards ──────────────────────────────────────────────────────────

func renderCards(sb *strings.Builder, data costData, w int) {
	type card struct {
		title, value, sub string
		color             lipgloss.Color
	}

	cards := []card{
		{"Total Spend", formatUSD(data.totalCost),
			fmt.Sprintf("across %d providers", data.providerCount), colorRosewater},
		{"Active", fmt.Sprintf("%d / %d", data.activeCount, data.providerCount),
			"providers", colorGreen},
		{"Models", fmt.Sprintf("%d", len(data.models)),
			fmt.Sprintf("%.0fK tokens", (data.totalInput+data.totalOutput)/1000), colorSapphire},
	}
	if data.burnRate > 0 {
		cards = append(cards, card{
			"Burn Rate", fmt.Sprintf("$%.2f/h", data.burnRate), "current", colorPeach})
	}

	n := len(cards)
	cardW := (w - 2 - (n-1)*2) / n
	cardW = clampInt(cardW, 16, 24)

	var rendered []string
	for _, c := range cards {
		rendered = append(rendered, RenderSummaryCard(c.title, c.value, c.sub, cardW, c.color))
	}
	sb.WriteString(" " + lipgloss.JoinHorizontal(lipgloss.Top, intersperse(rendered, "  ")...))
	sb.WriteString("\n")
}

// ─── Section Header ─────────────────────────────────────────────────────────

func renderSection(sb *strings.Builder, icon, title string, w int) {
	styled := lipgloss.NewStyle().Bold(true).Foreground(colorBlue).Render("  " + icon + " " + title + " ")
	lineLen := w - lipgloss.Width(styled) - 1
	if lineLen < 2 {
		lineLen = 2
	}
	sb.WriteString(styled + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", lineLen)) + "\n")
}

// ─── Token Table ────────────────────────────────────────────────────────────

func renderTokenTable(sb *strings.Builder, models []modelCostEntry, w int) {
	nameW := 22
	colW := 12

	sb.WriteString(fmt.Sprintf("  %-*s %*s %*s %*s %*s\n",
		nameW, dimStyle.Bold(true).Render("Model"),
		colW, dimStyle.Bold(true).Render("Provider"),
		colW, dimStyle.Bold(true).Render("Input"),
		colW, dimStyle.Bold(true).Render("Output"),
		colW, dimStyle.Bold(true).Render("Cost")))
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(
		strings.Repeat("─", nameW+colW*4+4)) + "\n")

	for _, m := range models {
		n := m.name
		if len(n) > nameW {
			n = n[:nameW-1] + "…"
		}
		p := m.provider
		if len(p) > colW {
			p = p[:colW-1] + "…"
		}
		sb.WriteString(fmt.Sprintf("  %s %*s %*s %*s %*s\n",
			lipgloss.NewStyle().Foreground(m.color).Width(nameW).Render(n),
			colW, dimStyle.Render(p),
			colW, lipgloss.NewStyle().Foreground(colorSapphire).Render(formatTokens(m.inputTokens)),
			colW, lipgloss.NewStyle().Foreground(colorPeach).Render(formatTokens(m.outputTokens)),
			colW, lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatUSD(m.cost))))
	}
}

// ─── Item Builders ──────────────────────────────────────────────────────────

func toProviderItems(providers []providerCostEntry, total float64) []chartItem {
	var items []chartItem
	for _, p := range providers {
		if p.cost <= 0 {
			continue
		}
		pct := ""
		if total > 0 {
			pct = fmt.Sprintf("(%.1f%%)", p.cost/total*100)
		}
		items = append(items, chartItem{Label: p.name, Value: p.cost, Color: p.color, SubLabel: pct})
	}
	return items
}

func toModelItems(models []modelCostEntry) []chartItem {
	var items []chartItem
	for _, m := range models {
		items = append(items, chartItem{
			Label: m.name, Value: m.cost, Color: m.color, SubLabel: m.provider,
		})
	}
	return items
}

func filterCostModels(models []modelCostEntry) []modelCostEntry {
	var out []modelCostEntry
	for _, m := range models {
		if m.cost > 0 {
			out = append(out, m)
		}
	}
	return out
}

func filterTokenModels(models []modelCostEntry) []modelCostEntry {
	var out []modelCostEntry
	for _, m := range models {
		if m.inputTokens > 0 || m.outputTokens > 0 || m.cost > 0 {
			out = append(out, m)
		}
	}
	return out
}

func buildEfficiencyItems(models []modelCostEntry) []chartItem {
	var items []chartItem
	for _, m := range models {
		tok := m.inputTokens + m.outputTokens
		if tok > 0 && m.cost > 0 {
			items = append(items, chartItem{
				Label:    m.name,
				Value:    m.cost / (tok / 1000),
				Color:    m.color,
				SubLabel: fmt.Sprintf("%.0fK tok", tok/1000),
			})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Value > items[j].Value })
	return items
}

func sortChartItems(items []chartItem) {
	sort.Slice(items, func(i, j int) bool { return items[i].Value > items[j].Value })
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
