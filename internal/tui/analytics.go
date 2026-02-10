package tui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/agentusage/internal/core"
)

const (
	analyticsSortCostDesc   = 0
	analyticsSortNameAsc    = 1
	analyticsSortTokensDesc = 2
	analyticsSortCount      = 3
)

var sortByLabels = []string{"Cost ↓", "Name ↑", "Tokens ↓"}

type costData struct {
	totalCost     float64
	totalInput    float64
	totalOutput   float64
	providerCount int
	activeCount   int
	providers     []providerCostEntry
	models        []modelCostEntry
	budgets       []budgetEntry
	usageGauges   []usageGaugeEntry
	tokenActivity []tokenActivityEntry
	timeSeries    []timeSeriesGroup
	snapshots     map[string]core.QuotaSnapshot
}

type timeSeriesGroup struct {
	providerID   string
	providerName string
	color        lipgloss.Color
	series       map[string][]core.TimePoint
}

type providerCostEntry struct {
	name       string
	providerID string
	cost       float64
	todayCost  float64
	weekCost   float64
	color      lipgloss.Color
	models     []modelCostEntry
	status     core.Status
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
	name  string
	used  float64
	limit float64
	color lipgloss.Color
}

type usageGaugeEntry struct {
	provider string
	name     string
	pctUsed  float64
	window   string
	color    lipgloss.Color
}

type tokenActivityEntry struct {
	provider string
	name     string
	input    float64
	output   float64
	cached   float64
	total    float64
	window   string
	color    lipgloss.Color
}

type collapsedGaugeGroup struct {
	provider string
	name     string
	count    int
	pctUsed  float64
	window   string
	color    lipgloss.Color
	resetIn  string
}

func extractCostData(snapshots map[string]core.QuotaSnapshot, filter string) costData {
	var data costData
	data.snapshots = snapshots
	lowerFilter := strings.ToLower(filter)

	keys := make([]string, 0, len(snapshots))
	for k := range snapshots {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		snap := snapshots[k]
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

		models := extractAllModels(snap, provColor)
		for i := range models {
			data.totalInput += models[i].inputTokens
			data.totalOutput += models[i].outputTokens
		}

		data.providers = append(data.providers, providerCostEntry{
			name:       snap.AccountID,
			providerID: snap.ProviderID,
			cost:       cost,
			todayCost:  extractTodayCost(snap),
			weekCost:   extract7DayCost(snap),
			color:      provColor,
			models:     models,
			status:     snap.Status,
		})

		data.budgets = append(data.budgets, extractBudgets(snap, provColor)...)
		data.usageGauges = append(data.usageGauges, extractUsageGauges(snap, provColor)...)
		data.tokenActivity = append(data.tokenActivity, extractTokenActivity(snap, provColor)...)

		if len(snap.DailySeries) > 0 {
			data.timeSeries = append(data.timeSeries, timeSeriesGroup{
				providerID:   snap.ProviderID,
				providerName: snap.AccountID,
				color:        provColor,
				series:       snap.DailySeries,
			})
		}
	}

	for _, p := range data.providers {
		data.models = append(data.models, p.models...)
	}

	return data
}

func extractProviderCost(snap core.QuotaSnapshot) float64 {
	modelTotal := 0.0
	for key, m := range snap.Metrics {
		if m.Used == nil || *m.Used <= 0 {
			continue
		}
		if strings.HasPrefix(key, "model_") && (strings.HasSuffix(key, "_cost") || strings.HasSuffix(key, "_cost_usd")) {
			modelTotal += *m.Used
		}
	}
	if modelTotal > 0 {
		return modelTotal
	}

	for _, key := range []string{
		"total_cost_usd",
		"plan_total_spend_usd",
		"all_time_api_cost",
		"jsonl_total_cost_usd",
		"today_api_cost",
		"daily_cost_usd",
		"5h_block_cost",
		"block_cost_usd",
		"individual_spend",
		"credits",
	} {
		if m, ok := snap.Metrics[key]; ok && m.Used != nil && *m.Used > 0 {
			return *m.Used
		}
	}

	return 0
}

func extractTodayCost(snap core.QuotaSnapshot) float64 {
	for _, key := range []string{"today_api_cost", "daily_cost_usd"} {
		if m, ok := snap.Metrics[key]; ok && m.Used != nil && *m.Used > 0 {
			return *m.Used
		}
	}
	return 0
}

func extract7DayCost(snap core.QuotaSnapshot) float64 {
	for _, key := range []string{"7d_api_cost"} {
		if m, ok := snap.Metrics[key]; ok && m.Used != nil && *m.Used > 0 {
			return *m.Used
		}
	}
	return 0
}

func extractAllModels(snap core.QuotaSnapshot, provColor lipgloss.Color) []modelCostEntry {
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
				if m.input == 0 {
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
				color:        stableModelColor(name, snap.AccountID),
			})
		}
	}
	return result
}

func extractBudgets(snap core.QuotaSnapshot, color lipgloss.Color) []budgetEntry {
	var result []budgetEntry

	if m, ok := snap.Metrics["spend_limit"]; ok && m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		result = append(result, budgetEntry{
			name: snap.AccountID + " (team)", used: *m.Used, limit: *m.Limit, color: color,
		})
		if ind, ok2 := snap.Metrics["individual_spend"]; ok2 && ind.Used != nil && *ind.Used > 0 {
			result = append(result, budgetEntry{
				name: snap.AccountID + " (you)", used: *ind.Used, limit: *m.Limit, color: color,
			})
		}
	}

	if m, ok := snap.Metrics["plan_spend"]; ok && m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		if _, has := snap.Metrics["spend_limit"]; !has {
			result = append(result, budgetEntry{
				name: snap.AccountID + " (plan)", used: *m.Used, limit: *m.Limit, color: color,
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
			name: snap.AccountID + " (credits)", used: used, limit: *m.Limit, color: color,
		})
	}

	return result
}

func extractUsageGauges(snap core.QuotaSnapshot, color lipgloss.Color) []usageGaugeEntry {
	var result []usageGaugeEntry

	mkeys := sortedMetricKeys(snap.Metrics)
	for _, key := range mkeys {
		m := snap.Metrics[key]
		pctUsed := metricUsedPercent(key, m)
		if pctUsed < 0 {
			continue
		}
		if pctUsed < 1 {
			continue
		}

		window := m.Window
		if window == "" {
			window = "current"
		}

		result = append(result, usageGaugeEntry{
			provider: snap.AccountID,
			name:     gaugeLabel(key, m.Window),
			pctUsed:  pctUsed,
			window:   window,
			color:    color,
		})
	}
	return result
}

func extractTokenActivity(snap core.QuotaSnapshot, color lipgloss.Color) []tokenActivityEntry {
	var result []tokenActivityEntry

	sessionIn, sessionOut, sessionCached, sessionTotal := float64(0), float64(0), float64(0), float64(0)
	if m, ok := snap.Metrics["session_input_tokens"]; ok && m.Used != nil {
		sessionIn = *m.Used
	}
	if m, ok := snap.Metrics["session_output_tokens"]; ok && m.Used != nil {
		sessionOut = *m.Used
	}
	if m, ok := snap.Metrics["session_cached_tokens"]; ok && m.Used != nil {
		sessionCached = *m.Used
	}
	if m, ok := snap.Metrics["session_total_tokens"]; ok && m.Used != nil {
		sessionTotal = *m.Used
	}
	if sessionIn > 0 || sessionOut > 0 || sessionTotal > 0 {
		result = append(result, tokenActivityEntry{
			provider: snap.AccountID, name: "Session tokens",
			input: sessionIn, output: sessionOut, cached: sessionCached,
			total: sessionTotal, window: "session", color: color,
		})
	}

	if m, ok := snap.Metrics["session_reasoning_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		result = append(result, tokenActivityEntry{
			provider: snap.AccountID, name: "Reasoning tokens",
			output: *m.Used, total: *m.Used, window: "session", color: color,
		})
	}

	if m, ok := snap.Metrics["context_window"]; ok && m.Limit != nil && m.Used != nil {
		result = append(result, tokenActivityEntry{
			provider: snap.AccountID, name: "Context window",
			input: *m.Used, total: *m.Limit, window: "current", color: color,
		})
	}

	for _, pair := range []struct{ key, label, window string }{
		{"messages_today", "Messages today", "1d"},
		{"total_conversations", "Conversations", "all-time"},
		{"total_messages", "Total messages", "all-time"},
		{"total_sessions", "Total sessions", "all-time"},
	} {
		if m, ok := snap.Metrics[pair.key]; ok && m.Used != nil && *m.Used > 0 {
			result = append(result, tokenActivityEntry{
				provider: snap.AccountID, name: pair.label,
				total: *m.Used, window: pair.window, color: color,
			})
		}
	}

	return result
}

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

func (m Model) renderAnalyticsContent(w, h int) string {
	data := extractCostData(m.snapshots, m.analyticsFilter)
	sortProviders(data.providers, m.analyticsSortBy)
	sortModels(data.models, m.analyticsSortBy)

	var statusBuf strings.Builder
	renderStatusBar(&statusBuf, m.analyticsSortBy, m.analyticsFilter, w)
	statusStr := statusBuf.String()

	hasData := data.totalCost > 0 || len(data.models) > 0 || len(data.budgets) > 0 ||
		len(data.usageGauges) > 0 || len(data.tokenActivity) > 0 || len(data.timeSeries) > 0

	if !hasData {
		empty := "\n" + dimStyle.Render("  No cost or usage data available.")
		empty += "\n" + dimStyle.Render("  Analytics requires providers that report spend, tokens, or budgets.")
		return statusStr + empty
	}

	var sb strings.Builder

	costTable := renderCostTable(data, w)
	if costTable != "" {
		sb.WriteString(costTable)
		sb.WriteString("\n")
	}

	tsSection := renderTimeSeriesCharts(data, w)
	if tsSection != "" {
		sb.WriteString(tsSection)
		sb.WriteString("\n")
	}

	modelsTable := renderModelsTable(data, w)
	if modelsTable != "" {
		sb.WriteString(modelsTable)
		sb.WriteString("\n")
	}

	bottomSection := renderBottomSection(data, w)
	if bottomSection != "" {
		sb.WriteString(bottomSection)
	}

	content := sb.String()

	lines := strings.Split(statusStr+content, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func renderStatusBar(sb *strings.Builder, sortBy int, filter string, w int) {
	parts := []string{
		analyticsSortLabelStyle.Render("↕ " + sortByLabels[sortBy]),
	}
	if filter != "" {
		parts = append(parts,
			lipgloss.NewStyle().Foreground(colorSapphire).Render("/ "+filter))
	}
	left := "  " + strings.Join(parts, "  "+dimStyle.Render("|")+"  ")
	hints := dimStyle.Render("s:sort  /:filter  ?:help")
	gap := w - lipgloss.Width(left) - lipgloss.Width(hints) - 2
	if gap < 1 {
		gap = 1
	}
	sb.WriteString(left + strings.Repeat(" ", gap) + hints + "\n")
}

func renderCostTable(data costData, w int) string {
	if len(data.providers) == 0 {
		return ""
	}

	hasCost := false
	for _, p := range data.providers {
		if p.cost > 0 || p.todayCost > 0 || p.weekCost > 0 {
			hasCost = true
			break
		}
	}

	hasBudget := len(data.budgets) > 0

	if !hasCost && !hasBudget {
		return ""
	}

	var sb strings.Builder

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorRosewater)
	sb.WriteString("  " + sectionStyle.Render("COST & SPEND") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	provW := 20
	colW := 12
	budgetW := w - provW - colW*3 - 10
	if budgetW < 20 {
		budgetW = 20
	}

	headerStyle := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(headerStyle.Render("Provider"), provW) + " " +
		padLeft(headerStyle.Render("Today"), colW) + " " +
		padLeft(headerStyle.Render("7 Day"), colW) + " " +
		padLeft(headerStyle.Render("All-Time"), colW) + "  " +
		padRight(headerStyle.Render("Budget"), budgetW) + "\n")

	budgetMap := make(map[string]budgetEntry)
	for _, b := range data.budgets {
		base := strings.Split(b.name, " (")[0]
		if existing, ok := budgetMap[base]; !ok || b.limit > existing.limit {
			budgetMap[base] = b
		}
	}

	for _, p := range data.providers {
		provColor := p.color
		switch p.status {
		case core.StatusLimited:
			provColor = colorRed
		case core.StatusNearLimit:
			provColor = colorYellow
		case core.StatusError, core.StatusAuth:
			provColor = colorRed
		}
		provStyle := lipgloss.NewStyle().Foreground(provColor).Bold(true)
		provName := provStyle.Render(truncStr(p.name, provW-2))
		if p.status == core.StatusLimited {
			provName += " " + lipgloss.NewStyle().Foreground(colorRed).Render("!")
		}

		todayStr := dimStyle.Render("—")
		if p.todayCost > 0 {
			todayStr = lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(p.todayCost))
		}

		weekStr := dimStyle.Render("—")
		if p.weekCost > 0 {
			weekStr = lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(p.weekCost))
		}

		allTimeStr := dimStyle.Render("—")
		if p.cost > 0 {
			allTimeStr = lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatUSD(p.cost))
		}

		budgetStr := dimStyle.Render("—")
		if b, ok := budgetMap[p.name]; ok && b.limit > 0 {
			pct := b.used / b.limit * 100
			gauge := RenderInlineGauge(pct, 10)
			budgetStr = gauge + " " +
				dimStyle.Render(fmt.Sprintf("%s/%s %.0f%%", formatUSD(b.used), formatUSD(b.limit), pct))
		}

		sb.WriteString("  " + padRight(provName, provW) + " " +
			padLeft(todayStr, colW) + " " +
			padLeft(weekStr, colW) + " " +
			padLeft(allTimeStr, colW) + "  " +
			padRight(budgetStr, budgetW) + "\n")
	}

	return sb.String()
}

func hasNonZeroData(pts []core.TimePoint) bool {
	for _, p := range pts {
		if p.Value > 0 {
			return true
		}
	}
	return false
}

func renderTimeSeriesCharts(data costData, w int) string {
	if len(data.timeSeries) == 0 {
		return ""
	}

	chartH := 12
	multi := len(data.timeSeries) > 1

	var costSeries []BrailleSeries
	var tokenSeries []BrailleSeries
	var activitySeries []BrailleSeries

	for _, g := range data.timeSeries {
		provLabel := g.providerName

		if pts, ok := g.series["cost"]; ok && hasNonZeroData(pts) {
			label := "daily cost"
			if multi {
				label = provLabel
			}
			costSeries = append(costSeries, BrailleSeries{
				Label: label, Color: g.color, Points: pts,
			})
		}

		type modelVol struct {
			key string
			pts []core.TimePoint
			vol float64
		}
		var perModel []modelVol
		for key, pts := range g.series {
			if strings.HasPrefix(key, "tokens_") && key != "tokens_total" && hasNonZeroData(pts) {
				total := 0.0
				for _, p := range pts {
					total += p.Value
				}
				perModel = append(perModel, modelVol{key: key, pts: pts, vol: total})
			}
		}
		sort.Slice(perModel, func(i, j int) bool { return perModel[i].vol > perModel[j].vol })

		if len(perModel) > 0 {
			cap := 4
			if len(perModel) < cap {
				cap = len(perModel)
			}
			for i := 0; i < cap; i++ {
				md := perModel[i]
				name := prettifyModelName(strings.TrimPrefix(md.key, "tokens_"))
				if multi {
					name = provLabel + " " + name
				}
				tokenSeries = append(tokenSeries, BrailleSeries{
					Label: name, Color: stableModelColor(md.key, g.providerID), Points: md.pts,
				})
			}
		} else if pts, ok := g.series["tokens_total"]; ok && hasNonZeroData(pts) {
			label := "tokens"
			if multi {
				label = provLabel + " tokens"
			}
			tokenSeries = append(tokenSeries, BrailleSeries{
				Label: label, Color: g.color, Points: pts,
			})
		}

		if pts, ok := g.series["messages"]; ok && hasNonZeroData(pts) {
			label := "messages"
			if multi {
				label = provLabel + " messages"
			}
			activitySeries = append(activitySeries, BrailleSeries{
				Label: label, Color: g.color, Points: pts,
			})
		}
		if pts, ok := g.series["tool_calls"]; ok && hasNonZeroData(pts) {
			label := "tool calls"
			if multi {
				label = provLabel + " tools"
			}
			activitySeries = append(activitySeries, BrailleSeries{
				Label: label, Color: shiftChartColor(g.color, 30), Points: pts,
			})
		}
		if pts, ok := g.series["sessions"]; ok && hasNonZeroData(pts) {
			label := "sessions"
			if multi {
				label = provLabel + " sessions"
			}
			activitySeries = append(activitySeries, BrailleSeries{
				Label: label, Color: shiftChartColor(g.color, 60), Points: pts,
			})
		}
	}

	type namedChart struct {
		title  string
		series []BrailleSeries
		yFmt   func(float64) string
	}
	var charts []namedChart

	if len(costSeries) > 0 {
		charts = append(charts, namedChart{
			title: "DAILY COST (estimated API-equivalent)", series: costSeries, yFmt: formatCostAxis,
		})
	}
	if len(tokenSeries) > 0 {
		charts = append(charts, namedChart{
			title: "TOKEN USAGE BY MODEL", series: tokenSeries,
		})
	}
	if len(activitySeries) > 0 {
		charts = append(charts, namedChart{
			title: "ACTIVITY OVER TIME", series: activitySeries,
		})
	}

	if len(charts) == 0 {
		return ""
	}

	maxCharts := 2
	if len(charts) < maxCharts {
		maxCharts = len(charts)
	}

	var sb strings.Builder
	for i := 0; i < maxCharts; i++ {
		c := charts[i]
		yFmt := formatChartValue
		if c.yFmt != nil {
			yFmt = c.yFmt
		}
		sb.WriteString(RenderBrailleChart(c.title, c.series, w, chartH, yFmt))
		if i < maxCharts-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func shiftChartColor(base lipgloss.Color, offset int) lipgloss.Color {
	h := 0
	for _, ch := range string(base) {
		h = h*31 + int(ch)
	}
	h += offset
	if h < 0 {
		h = -h
	}
	return modelColorPalette[h%len(modelColorPalette)]
}

func renderModelsTable(data costData, w int) string {
	allModels := filterTokenModels(data.models)
	if len(allModels) == 0 {
		return ""
	}

	sorted := make([]modelCostEntry, len(allModels))
	copy(sorted, allModels)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].cost != sorted[j].cost {
			return sorted[i].cost > sorted[j].cost
		}
		return (sorted[i].inputTokens + sorted[i].outputTokens) > (sorted[j].inputTokens + sorted[j].outputTokens)
	})

	var sb strings.Builder

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorTeal)
	sb.WriteString("  " + sectionStyle.Render("MODELS & EFFICIENCY") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	nameW := 32
	provW := 16
	colW := 10
	effW := 12

	if nameW+provW+colW*3+effW+10 > w {
		nameW = 24
		provW = 12
	}

	headerStyle := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(headerStyle.Render("Model"), nameW) + " " +
		padRight(headerStyle.Render("Provider"), provW) + " " +
		padLeft(headerStyle.Render("Input"), colW) + " " +
		padLeft(headerStyle.Render("Output"), colW) + " " +
		padLeft(headerStyle.Render("Cost"), colW) + " " +
		padLeft(headerStyle.Render("$/1K tok"), effW) + "\n")

	for _, m := range sorted {
		nameStyle := lipgloss.NewStyle().Foreground(m.color)
		provStyle := lipgloss.NewStyle().Foreground(m.color)

		inputStr := dimStyle.Render("—")
		if m.inputTokens > 0 {
			inputStr = lipgloss.NewStyle().Foreground(colorSapphire).Render(formatTokens(m.inputTokens))
		}

		outputStr := dimStyle.Render("—")
		if m.outputTokens > 0 {
			outputStr = lipgloss.NewStyle().Foreground(colorPeach).Render(formatTokens(m.outputTokens))
		}

		costStr := dimStyle.Render("—")
		if m.cost > 0 {
			costStr = lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatUSD(m.cost))
		}

		effStr := dimStyle.Render("—")
		totalTok := m.inputTokens + m.outputTokens
		if m.cost > 0 && totalTok > 0 {
			costPer1K := m.cost / totalTok * 1000
			effStr = lipgloss.NewStyle().Foreground(colorYellow).Render(fmt.Sprintf("$%.4f", costPer1K))
		}

		sb.WriteString("  " + padRight(nameStyle.Render(truncStr(m.name, nameW)), nameW) + " " +
			padRight(provStyle.Render(truncStr(m.provider, provW)), provW) + " " +
			padLeft(inputStr, colW) + " " +
			padLeft(outputStr, colW) + " " +
			padLeft(costStr, colW) + " " +
			padLeft(effStr, effW) + "\n")
	}

	return sb.String()
}

func renderBottomSection(data costData, w int) string {
	hasRateLimits := len(data.usageGauges) > 0
	hasActivity := len(data.tokenActivity) > 0 || data.totalInput > 0 || data.totalOutput > 0
	hasTimeSeries := len(data.timeSeries) > 0

	if !hasRateLimits && !hasActivity && !hasTimeSeries {
		return ""
	}

	if hasRateLimits && !hasActivity && !hasTimeSeries {
		return renderRateLimitsSection(data, w)
	}
	if !hasRateLimits && (hasActivity || hasTimeSeries) {
		return renderAnalyticsActivitySection(data, w)
	}

	halfW := w / 2
	leftStr := renderRateLimitsSection(data, halfW-1)
	rightStr := renderAnalyticsActivitySection(data, halfW-1)

	leftLines := strings.Split(leftStr, "\n")
	rightLines := strings.Split(rightStr, "\n")

	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	for len(leftLines) < maxLines {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, "")
	}

	var sb strings.Builder
	sep := dimStyle.Render("│")
	for i := 0; i < maxLines; i++ {
		left := leftLines[i]
		right := rightLines[i]
		leftPad := halfW - 1 - lipgloss.Width(left)
		if leftPad < 0 {
			leftPad = 0
		}
		sb.WriteString(left + strings.Repeat(" ", leftPad) + sep + right + "\n")
	}

	return sb.String()
}

func renderRateLimitsSection(data costData, w int) string {
	if len(data.usageGauges) == 0 {
		return ""
	}

	collapsed := collapseUsageGauges(data.usageGauges, data.snapshots)

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorYellow)
	sb.WriteString("  " + sectionStyle.Render("RATE LIMITS") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	provW := 16
	nameW := 18
	barW := clampInt(w-provW-nameW-30, 8, 20)

	for _, g := range collapsed {
		provStyle := lipgloss.NewStyle().Foreground(g.color).Bold(true)

		pct := clampFloat(g.pctUsed, 0, 100)
		filled := int(pct / 100 * float64(barW))
		empty := barW - filled

		barColor := colorGreen
		if pct >= 90 {
			barColor = colorRed
		} else if pct >= 70 {
			barColor = colorYellow
		}

		bar := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("█", filled))
		track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", empty))

		name := g.name
		if g.count > 1 {
			name = fmt.Sprintf("%d quotas", g.count)
		}
		name = truncStr(name, nameW)

		pctStr := lipgloss.NewStyle().Foreground(barColor).Bold(true).Render(fmt.Sprintf("%3.0f%%", pct))

		resetStr := ""
		if g.resetIn != "" {
			resetStr = dimStyle.Render(g.resetIn)
		}

		sb.WriteString("  " + padRight(provStyle.Render(truncStr(g.provider, provW)), provW) + " " +
			padRight(dimStyle.Render(name), nameW) + " " +
			bar + track + " " +
			pctStr + " " +
			dimStyle.Render(g.window) + " " +
			resetStr + "\n")
	}

	return sb.String()
}

func collapseUsageGauges(gauges []usageGaugeEntry, snapshots map[string]core.QuotaSnapshot) []collapsedGaugeGroup {
	type groupKey struct {
		provider string
		pctRound int
		window   string
	}

	groups := make(map[groupKey]*collapsedGaugeGroup)
	var order []groupKey

	for _, g := range gauges {
		pctRound := int(math.Round(g.pctUsed))
		key := groupKey{provider: g.provider, pctRound: pctRound, window: g.window}

		if existing, ok := groups[key]; ok {
			existing.count++
		} else {
			resetStr := ""
			if snap, ok := snapshots[g.provider]; ok {
				resetStr = findBestResetTime(snap.Resets, g.window)
			}

			cg := &collapsedGaugeGroup{
				provider: g.provider,
				name:     g.name,
				count:    1,
				pctUsed:  g.pctUsed,
				window:   g.window,
				color:    g.color,
				resetIn:  resetStr,
			}
			groups[key] = cg
			order = append(order, key)
		}
	}

	result := make([]collapsedGaugeGroup, 0, len(order))
	for _, k := range order {
		result = append(result, *groups[k])
	}
	return result
}

func findBestResetTime(resets map[string]time.Time, window string) string {
	if len(resets) == 0 {
		return ""
	}

	now := time.Now()
	var best time.Duration
	found := false

	for _, t := range resets {
		if t.After(now) {
			d := t.Sub(now)
			if !found || d < best {
				best = d
				found = true
			}
		}
	}

	if !found {
		return ""
	}

	return formatResetDuration(best)
}

func formatResetDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	if hours < 24 {
		mins := int(d.Minutes()) - hours*60
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	days := hours / 24
	remainH := hours - days*24
	return fmt.Sprintf("%dd %dh", days, remainH)
}

func renderAnalyticsActivitySection(data costData, w int) string {
	var sb strings.Builder

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	sb.WriteString("  " + sectionStyle.Render("ACTIVITY & TOKENS") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	if len(data.tokenActivity) > 0 {
		renderCompactTokenActivity(&sb, data.tokenActivity, w)
	}

	if data.totalInput > 0 || data.totalOutput > 0 {
		if len(data.tokenActivity) > 0 {
			sb.WriteString("\n")
		}
		renderCompactTokenBreakdown(&sb, data, w)
	}

	if len(data.timeSeries) > 0 {
		if data.totalInput > 0 || data.totalOutput > 0 || len(data.tokenActivity) > 0 {
			sb.WriteString("\n")
		}
		renderSparklineSummary(&sb, data.timeSeries, w)
	}

	return sb.String()
}

func renderCompactTokenActivity(sb *strings.Builder, entries []tokenActivityEntry, w int) {
	type provGroup struct {
		provider string
		color    lipgloss.Color
		entries  []tokenActivityEntry
	}
	groups := make(map[string]*provGroup)
	var order []string
	for _, e := range entries {
		g, ok := groups[e.provider]
		if !ok {
			g = &provGroup{provider: e.provider, color: e.color}
			groups[e.provider] = g
			order = append(order, e.provider)
		}
		g.entries = append(g.entries, e)
	}
	sort.Strings(order)

	for _, provName := range order {
		g := groups[provName]
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(g.color).Bold(true).Render(g.provider) + "\n")
		for _, e := range g.entries {
			name := truncStr(e.name, 18)
			var parts []string
			if e.input > 0 {
				parts = append(parts, lipgloss.NewStyle().Foreground(colorSapphire).Render("in:"+formatTokens(e.input)))
			}
			if e.output > 0 {
				parts = append(parts, lipgloss.NewStyle().Foreground(colorPeach).Render("out:"+formatTokens(e.output)))
			}
			if e.cached > 0 {
				parts = append(parts, lipgloss.NewStyle().Foreground(colorTeal).Render("cache:"+formatTokens(e.cached)))
			}
			if e.total > 0 && e.input == 0 && e.output == 0 {
				parts = append(parts, lipgloss.NewStyle().Foreground(colorRosewater).Bold(true).Render(formatTokens(e.total)))
			}
			sb.WriteString(fmt.Sprintf("    %-18s %s  %s\n",
				dimStyle.Render(name), strings.Join(parts, " "), dimStyle.Render(e.window)))
		}
	}
}

func renderCompactTokenBreakdown(sb *strings.Builder, data costData, w int) {
	barW := clampInt(w-34, 8, 30)
	maxVal := math.Max(data.totalInput, data.totalOutput)
	if maxVal == 0 {
		maxVal = 1
	}

	inLen := int(data.totalInput / maxVal * float64(barW))
	if inLen < 1 && data.totalInput > 0 {
		inLen = 1
	}
	inBar := lipgloss.NewStyle().Foreground(colorSapphire).Render(strings.Repeat("█", inLen))
	inTrack := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", barW-inLen))
	sb.WriteString(fmt.Sprintf("  %s %s%s  %s\n",
		lipgloss.NewStyle().Foreground(colorSapphire).Width(8).Render("Input"),
		inBar, inTrack,
		dimStyle.Render(formatTokens(data.totalInput)+" tok")))

	outLen := int(data.totalOutput / maxVal * float64(barW))
	if outLen < 1 && data.totalOutput > 0 {
		outLen = 1
	}
	outBar := lipgloss.NewStyle().Foreground(colorPeach).Render(strings.Repeat("█", outLen))
	outTrack := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", barW-outLen))
	sb.WriteString(fmt.Sprintf("  %s %s%s  %s\n",
		lipgloss.NewStyle().Foreground(colorPeach).Width(8).Render("Output"),
		outBar, outTrack,
		dimStyle.Render(formatTokens(data.totalOutput)+" tok")))
}

func renderSparklineSummary(sb *strings.Builder, groups []timeSeriesGroup, w int) {
	sparkW := clampInt(w-30, 10, 40)

	for _, g := range groups {
		rendered := false
		for _, key := range []string{"messages", "sessions", "tokens_total", "total_lines"} {
			pts, ok := g.series[key]
			if !ok || len(pts) < 2 {
				continue
			}

			values := make([]float64, len(pts))
			for i, p := range pts {
				values[i] = p.Value
			}

			label := g.providerName + " " + strings.ReplaceAll(key, "_", " ")
			spark := RenderSparkline(values, sparkW, g.color)
			latest := formatChartValue(values[len(values)-1])

			sb.WriteString(fmt.Sprintf("  %-20s %s %s\n",
				dimStyle.Render(truncStr(label, 20)),
				spark,
				lipgloss.NewStyle().Foreground(g.color).Render(latest)))
			rendered = true
		}
		_ = rendered
	}
}

func padLeft(s string, w int) string {
	vw := lipgloss.Width(s)
	if vw >= w {
		return s
	}
	return strings.Repeat(" ", w-vw) + s
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

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func sortedMetricKeys(m map[string]core.Metric) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
