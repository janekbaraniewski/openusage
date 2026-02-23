package tui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
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
	snapshots     map[string]core.UsageSnapshot
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
	providers    []modelProviderSplit
	confidence   float64
	window       string
}

type modelProviderSplit struct {
	provider     string
	cost         float64
	inputTokens  float64
	outputTokens float64
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

type analyticsSummary struct {
	dailyCost         []core.TimePoint
	dailyTokens       []core.TimePoint
	dailyMessages     []core.TimePoint
	dayOfWeekCost     [7]float64
	dayOfWeekCount    [7]int
	peakCostDate      string
	peakCost          float64
	peakTokenDate     string
	peakTokens        float64
	recentCostAvg     float64
	previousCostAvg   float64
	recentTokensAvg   float64
	previousTokensAvg float64
	costVolatility    float64
	tokenVolatility   float64
	concentrationTop3 float64
	activeDays        int
}

type analyticsInsight struct {
	label    string
	detail   string
	severity lipgloss.Color
}

type analyticsScatterPoint struct {
	label string
	x     float64
	y     float64
	color lipgloss.Color
}

func extractCostData(snapshots map[string]core.UsageSnapshot, filter string) costData {
	var data costData
	data.snapshots = snapshots
	lowerFilter := strings.ToLower(filter)

	keys := lo.Keys(snapshots)
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

	data.models = aggregateCanonicalModels(data.providers)

	return data
}

func extractProviderCost(snap core.UsageSnapshot) float64 {
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

func extractTodayCost(snap core.UsageSnapshot) float64 {
	for _, key := range []string{"today_api_cost", "daily_cost_usd", "today_cost", "usage_daily"} {
		if m, ok := snap.Metrics[key]; ok && m.Used != nil && *m.Used > 0 {
			return *m.Used
		}
	}
	return 0
}

func extract7DayCost(snap core.UsageSnapshot) float64 {
	for _, key := range []string{"7d_api_cost", "usage_weekly"} {
		if m, ok := snap.Metrics[key]; ok && m.Used != nil && *m.Used > 0 {
			return *m.Used
		}
	}
	return 0
}

func extractAllModels(snap core.UsageSnapshot, provColor lipgloss.Color) []modelCostEntry {
	if len(snap.ModelUsage) > 0 {
		return extractAllModelsFromRecords(snap)
	}

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

func extractAllModelsFromRecords(snap core.UsageSnapshot) []modelCostEntry {
	type md struct {
		cost       float64
		input      float64
		output     float64
		confidence float64
		window     string
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

	for _, rec := range snap.ModelUsage {
		name := modelRecordDisplayName(rec)
		if name == "" {
			continue
		}
		md := ensure(name)
		if rec.CostUSD != nil && *rec.CostUSD > 0 {
			md.cost += *rec.CostUSD
		}
		if rec.InputTokens != nil {
			md.input += *rec.InputTokens
		}
		if rec.OutputTokens != nil {
			md.output += *rec.OutputTokens
		}
		if rec.TotalTokens != nil && rec.InputTokens == nil && rec.OutputTokens == nil {
			md.input += *rec.TotalTokens
		}
		if rec.Confidence > md.confidence {
			md.confidence = rec.Confidence
		}
		if md.window == "" {
			md.window = rec.Window
		}
	}

	result := make([]modelCostEntry, 0, len(order))
	for _, name := range order {
		md := models[name]
		if md.cost <= 0 && md.input <= 0 && md.output <= 0 {
			continue
		}
		result = append(result, modelCostEntry{
			name:         prettifyModelName(name),
			provider:     snap.AccountID,
			cost:         md.cost,
			inputTokens:  md.input,
			outputTokens: md.output,
			color:        stableModelColor(name, snap.AccountID),
			confidence:   md.confidence,
			window:       md.window,
		})
	}
	return result
}

func modelRecordDisplayName(rec core.ModelUsageRecord) string {
	if rec.Dimensions != nil {
		if groupID := strings.TrimSpace(rec.Dimensions["canonical_group_id"]); groupID != "" {
			return groupID
		}
	}
	if strings.TrimSpace(rec.RawModelID) != "" {
		return rec.RawModelID
	}
	if strings.TrimSpace(rec.CanonicalLineageID) != "" {
		return rec.CanonicalLineageID
	}
	return "unknown"
}

func aggregateCanonicalModels(providers []providerCostEntry) []modelCostEntry {
	type splitAgg struct {
		cost   float64
		input  float64
		output float64
	}
	type modelAgg struct {
		cost       float64
		input      float64
		output     float64
		confidence float64
		window     string
		splits     map[string]*splitAgg
	}

	byModel := make(map[string]*modelAgg)
	order := make([]string, 0, len(providers))

	ensureModel := func(name string) *modelAgg {
		if agg, ok := byModel[name]; ok {
			return agg
		}
		agg := &modelAgg{splits: make(map[string]*splitAgg)}
		byModel[name] = agg
		order = append(order, name)
		return agg
	}
	ensureSplit := func(m *modelAgg, provider string) *splitAgg {
		if s, ok := m.splits[provider]; ok {
			return s
		}
		s := &splitAgg{}
		m.splits[provider] = s
		return s
	}

	for _, provider := range providers {
		for _, model := range provider.models {
			name := strings.TrimSpace(model.name)
			if name == "" {
				continue
			}
			agg := ensureModel(name)
			agg.cost += model.cost
			agg.input += model.inputTokens
			agg.output += model.outputTokens
			if model.confidence > agg.confidence {
				agg.confidence = model.confidence
			}
			if agg.window == "" {
				agg.window = model.window
			}
			split := ensureSplit(agg, provider.name)
			split.cost += model.cost
			split.input += model.inputTokens
			split.output += model.outputTokens
		}
	}

	result := make([]modelCostEntry, 0, len(byModel))
	for _, name := range order {
		agg := byModel[name]
		if agg.cost <= 0 && agg.input <= 0 && agg.output <= 0 {
			continue
		}

		splits := make([]modelProviderSplit, 0, len(agg.splits))
		for provider, split := range agg.splits {
			splits = append(splits, modelProviderSplit{
				provider:     provider,
				cost:         split.cost,
				inputTokens:  split.input,
				outputTokens: split.output,
			})
		}
		sort.Slice(splits, func(i, j int) bool {
			left := splits[i].cost
			right := splits[j].cost
			if left == 0 && right == 0 {
				left = splits[i].inputTokens + splits[i].outputTokens
				right = splits[j].inputTokens + splits[j].outputTokens
			}
			if left == right {
				return splits[i].provider < splits[j].provider
			}
			return left > right
		})

		topProvider := ""
		if len(splits) > 0 {
			topProvider = splits[0].provider
		}

		result = append(result, modelCostEntry{
			name:         name,
			provider:     topProvider,
			cost:         agg.cost,
			inputTokens:  agg.input,
			outputTokens: agg.output,
			color:        stableModelColor(name, "all"),
			providers:    splits,
			confidence:   agg.confidence,
			window:       agg.window,
		})
	}

	return result
}

func extractBudgets(snap core.UsageSnapshot, color lipgloss.Color) []budgetEntry {
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

func extractUsageGauges(snap core.UsageSnapshot, color lipgloss.Color) []usageGaugeEntry {
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
			name:     gaugeLabel(dashboardWidget(snap.ProviderID), key, m.Window),
			pctUsed:  pctUsed,
			window:   window,
			color:    color,
		})
	}
	return result
}

func extractTokenActivity(snap core.UsageSnapshot, color lipgloss.Color) []tokenActivityEntry {
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

	// OpenRouter-specific metrics
	if m, ok := snap.Metrics["today_reasoning_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		result = append(result, tokenActivityEntry{
			provider: snap.AccountID, name: "Reasoning (today)",
			output: *m.Used, total: *m.Used, window: "today", color: color,
		})
	}
	if m, ok := snap.Metrics["today_cached_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		result = append(result, tokenActivityEntry{
			provider: snap.AccountID, name: "Cached (today)",
			cached: *m.Used, total: *m.Used, window: "today", color: color,
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
	data := extractCostData(m.visibleSnapshots(), m.analyticsFilter)
	sortProviders(data.providers, m.analyticsSortBy)
	sortModels(data.models, m.analyticsSortBy)
	summary := computeAnalyticsSummary(data)

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

	content := renderAnalyticsSinglePage(data, summary, w)

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

func renderAnalyticsSinglePage(data costData, summary analyticsSummary, w int) string {
	var sb strings.Builder

	if kpis := renderAnalyticsKPIHeader(data, summary, w); kpis != "" {
		sb.WriteString(kpis)
		sb.WriteString("\n")
	}

	if totalCost := renderTotalCostTrend(data, summary, w, 9); totalCost != "" {
		sb.WriteString(totalCost)
		sb.WriteString("\n")
	}

	if stacked := renderProviderCostStackedChart(data, w, 8); stacked != "" {
		sb.WriteString(stacked)
		sb.WriteString("\n")
	}

	if tokenDist := renderDailyTokenDistributionChart(data, w, 12); tokenDist != "" {
		sb.WriteString(tokenDist)
		sb.WriteString("\n")
	}

	if bottom := renderAnalyticsBottomGrid(data, w); bottom != "" {
		sb.WriteString(bottom)
	}

	return strings.TrimRight(sb.String(), "\n")
}

func renderAnalyticsBottomGrid(data costData, w int) string {
	if w < 80 {
		var sb strings.Builder
		if heat := renderProviderModelDailyUsage(data, w, 12); heat != "" {
			sb.WriteString(heat)
			sb.WriteString("\n")
		}
		if table := renderTopModelsSummary(data.models, w, 10); table != "" {
			sb.WriteString(table)
			sb.WriteString("\n")
		}
		if costs := renderCostTable(data, w); costs != "" {
			sb.WriteString(costs)
		}
		return strings.TrimRight(sb.String(), "\n")
	}

	gap := 2
	colW := (w - 4 - gap) / 2
	if colW < 36 {
		colW = 36
	}
	left := strings.TrimRight(renderProviderModelDailyUsage(data, colW, 12), "\n")
	right := strings.TrimRight(renderTopModelsCompact(data.models, colW, 10), "\n")

	row1 := ""
	switch {
	case left != "" && right != "":
		row1 = lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
	case left != "":
		row1 = left
	case right != "":
		row1 = right
	}

	row2 := strings.TrimRight(renderCostTableCompact(data, w, 8), "\n")

	if row1 == "" {
		return row2
	}
	if row2 == "" {
		return row1
	}
	return row1 + "\n\n" + row2
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
	provW := 24
	colW := 10
	effW := 12

	if nameW+provW+colW*3+effW+10 > w {
		nameW = 22
		provW = 18
	}

	headerStyle := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(headerStyle.Render("Model"), nameW) + " " +
		padRight(headerStyle.Render("Provider split"), provW) + " " +
		padLeft(headerStyle.Render("Input"), colW) + " " +
		padLeft(headerStyle.Render("Output"), colW) + " " +
		padLeft(headerStyle.Render("Cost"), colW) + " " +
		padLeft(headerStyle.Render("$/1K tok"), effW) + "\n")

	for _, m := range sorted {
		nameStyle := lipgloss.NewStyle().Foreground(m.color)
		splitStyle := lipgloss.NewStyle().Foreground(m.color)

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
		splitStr := formatModelProviderSplit(m)

		sb.WriteString("  " + padRight(nameStyle.Render(truncStr(m.name, nameW)), nameW) + " " +
			padRight(splitStyle.Render(truncStr(splitStr, provW)), provW) + " " +
			padLeft(inputStr, colW) + " " +
			padLeft(outputStr, colW) + " " +
			padLeft(costStr, colW) + " " +
			padLeft(effStr, effW) + "\n")
	}

	return sb.String()
}

func renderTopModelsSummary(models []modelCostEntry, w int, limit int) string {
	all := filterTokenModels(models)
	if len(all) == 0 {
		return ""
	}
	sort.Slice(all, func(i, j int) bool {
		li := all[i].inputTokens + all[i].outputTokens
		lj := all[j].inputTokens + all[j].outputTokens
		if li == lj {
			return all[i].cost > all[j].cost
		}
		return li > lj
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorTeal)
	sb.WriteString("  " + sectionStyle.Render("TOP MODELS (Daily volume & efficiency)") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	nameW := clampInt(w/3, 20, 34)
	provW := clampInt(w/5, 14, 22)
	tokW := 12
	costW := 10
	effW := 10

	head := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(head.Render("Model"), nameW) + " " +
		padRight(head.Render("Provider"), provW) + " " +
		padLeft(head.Render("Tokens"), tokW) + " " +
		padLeft(head.Render("Cost"), costW) + " " +
		padLeft(head.Render("$/1K"), effW) + "\n")

	for _, m := range all {
		tokens := m.inputTokens + m.outputTokens
		if tokens <= 0 {
			continue
		}
		eff := "—"
		if m.cost > 0 {
			eff = fmt.Sprintf("$%.4f", m.cost/tokens*1000)
		}
		sb.WriteString("  " +
			padRight(lipgloss.NewStyle().Foreground(m.color).Render(truncStr(m.name, nameW)), nameW) + " " +
			padRight(dimStyle.Render(truncStr(primaryProvider(m), provW)), provW) + " " +
			padLeft(lipgloss.NewStyle().Foreground(colorSapphire).Render(formatTokens(tokens)), tokW) + " " +
			padLeft(lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(m.cost)), costW) + " " +
			padLeft(lipgloss.NewStyle().Foreground(colorYellow).Render(eff), effW) + "\n")
	}
	return sb.String()
}

func renderTopModelsCompact(models []modelCostEntry, w int, limit int) string {
	all := filterTokenModels(models)
	if len(all) == 0 {
		return ""
	}
	sort.Slice(all, func(i, j int) bool {
		li := all[i].inputTokens + all[i].outputTokens
		lj := all[j].inputTokens + all[j].outputTokens
		if li == lj {
			return all[i].cost > all[j].cost
		}
		return li > lj
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorTeal)
	sb.WriteString("  " + sectionStyle.Render("TOP MODELS (compact)") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	nameW := clampInt(w/2, 16, 26)
	provW := clampInt(w/4, 10, 16)
	tokW := 9
	effW := 9

	head := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(head.Render("Model"), nameW) + " " +
		padRight(head.Render("Provider"), provW) + " " +
		padLeft(head.Render("Tokens"), tokW) + " " +
		padLeft(head.Render("$/1K"), effW) + "\n")

	for _, m := range all {
		tokens := m.inputTokens + m.outputTokens
		if tokens <= 0 {
			continue
		}
		eff := "—"
		if m.cost > 0 {
			eff = fmt.Sprintf("$%.4f", m.cost/tokens*1000)
		}
		sb.WriteString("  " +
			padRight(lipgloss.NewStyle().Foreground(m.color).Render(truncStr(m.name, nameW)), nameW) + " " +
			padRight(dimStyle.Render(truncStr(primaryProvider(m), provW)), provW) + " " +
			padLeft(lipgloss.NewStyle().Foreground(colorSapphire).Render(formatTokens(tokens)), tokW) + " " +
			padLeft(lipgloss.NewStyle().Foreground(colorYellow).Render(eff), effW) + "\n")
	}
	return sb.String()
}

func primaryProvider(m modelCostEntry) string {
	if len(m.providers) > 0 {
		return m.providers[0].provider
	}
	if m.provider != "" {
		return m.provider
	}
	return "—"
}

func renderCostTableCompact(data costData, w int, limit int) string {
	if len(data.providers) == 0 {
		return ""
	}
	providers := make([]providerCostEntry, len(data.providers))
	copy(providers, data.providers)
	sort.Slice(providers, func(i, j int) bool {
		li := providers[i].weekCost
		lj := providers[j].weekCost
		if li == 0 && providers[i].todayCost > 0 {
			li = providers[i].todayCost
		}
		if lj == 0 && providers[j].todayCost > 0 {
			lj = providers[j].todayCost
		}
		if li == lj {
			return providers[i].name < providers[j].name
		}
		return li > lj
	})
	if limit > 0 && len(providers) > limit {
		providers = providers[:limit]
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorRosewater)
	sb.WriteString("  " + sectionStyle.Render("COST & SPEND (compact)") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	provW := clampInt(w/3, 14, 24)
	colW := clampInt((w-provW-8)/3, 8, 12)
	head := dimStyle.Copy().Bold(true)
	sb.WriteString("  " + padRight(head.Render("Provider"), provW) + " " +
		padLeft(head.Render("Today"), colW) + " " +
		padLeft(head.Render("7d"), colW) + " " +
		padLeft(head.Render("All"), colW) + "\n")

	for _, p := range providers {
		provColor := p.color
		if p.status == core.StatusLimited || p.status == core.StatusError || p.status == core.StatusAuth {
			provColor = colorRed
		}
		name := lipgloss.NewStyle().Foreground(provColor).Bold(true).Render(truncStr(p.name, provW))
		todayStr := dimStyle.Render("—")
		if p.todayCost > 0 {
			todayStr = lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(p.todayCost))
		}
		weekStr := dimStyle.Render("—")
		if p.weekCost > 0 {
			weekStr = lipgloss.NewStyle().Foreground(colorTeal).Render(formatUSD(p.weekCost))
		}
		allStr := dimStyle.Render("—")
		if p.cost > 0 {
			allStr = lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatUSD(p.cost))
		}
		sb.WriteString("  " + padRight(name, provW) + " " +
			padLeft(todayStr, colW) + " " +
			padLeft(weekStr, colW) + " " +
			padLeft(allStr, colW) + "\n")
	}
	return sb.String()
}

func formatModelProviderSplit(entry modelCostEntry) string {
	if len(entry.providers) == 0 {
		if entry.provider == "" {
			return "—"
		}
		return entry.provider
	}
	if len(entry.providers) == 1 {
		return entry.providers[0].provider
	}

	total := entry.cost
	if total <= 0 {
		total = entry.inputTokens + entry.outputTokens
	}
	if total <= 0 {
		total = 1
	}

	parts := make([]string, 0, 3)
	limit := 2
	if len(entry.providers) < limit {
		limit = len(entry.providers)
	}
	for i := 0; i < limit; i++ {
		s := entry.providers[i]
		base := s.cost
		if base <= 0 {
			base = s.inputTokens + s.outputTokens
		}
		pct := base / total * 100
		parts = append(parts, fmt.Sprintf("%s %.0f%%", s.provider, pct))
	}
	if len(entry.providers) > limit {
		parts = append(parts, fmt.Sprintf("+%d", len(entry.providers)-limit))
	}
	return strings.Join(parts, ", ")
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
	sb.WriteString("  " + sectionStyle.Render("USAGE WINDOWS") + "\n")
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
			name = fmt.Sprintf("%d usage windows", g.count)
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

func collapseUsageGauges(gauges []usageGaugeEntry, snapshots map[string]core.UsageSnapshot) []collapsedGaugeGroup {
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

func renderAnalyticsKPIHeader(data costData, summary analyticsSummary, w int) string {
	if w < 40 {
		return ""
	}
	kpis := []string{
		renderKPIBlock("Total Cost", formatUSD(data.totalCost), fmt.Sprintf("%d providers", data.providerCount), colorTeal),
		renderKPIBlock("Total Tokens", formatTokens(data.totalInput+data.totalOutput), fmt.Sprintf("%d active days", summary.activeDays), colorSapphire),
		renderKPIBlock("Cost Trend", renderTrendPercent(summary.recentCostAvg, summary.previousCostAvg), "last 7d vs prior 7d", colorYellow),
		renderKPIBlock("Top-3 Concentration", fmt.Sprintf("%.0f%%", summary.concentrationTop3*100), "provider spend share", colorPeach),
	}
	return "  " + strings.Join(kpis, "  ")
}

func renderKPIBlock(title, value, subtitle string, accent lipgloss.Color) string {
	titleStr := analyticsCardTitleStyle.Render(title)
	valueStr := analyticsCardValueStyle.Copy().Foreground(accent).Render(value)
	subtitleStr := analyticsCardSubtitleStyle.Render(subtitle)
	return titleStr + " " + valueStr + " " + subtitleStr
}

func renderTrendPercent(current, previous float64) string {
	if current <= 0 && previous <= 0 {
		return "—"
	}
	if previous <= 0 {
		return "+∞"
	}
	delta := (current - previous) / previous * 100
	sign := "+"
	if delta < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.1f%%", sign, delta)
}

func renderInsightCards(insights []analyticsInsight, w int, limit int) string {
	if len(insights) == 0 {
		return ""
	}
	if limit > 0 && len(insights) > limit {
		insights = insights[:limit]
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	sb.WriteString("  " + sectionStyle.Render("INSIGHTS") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	for _, in := range insights {
		icon := lipgloss.NewStyle().Foreground(in.severity).Render("●")
		label := lipgloss.NewStyle().Foreground(in.severity).Bold(true).Render(in.label)
		sb.WriteString("  " + icon + " " + label + "  " + dimStyle.Render(in.detail) + "\n")
	}
	return sb.String()
}

func buildAnalyticsInsights(data costData, summary analyticsSummary) []analyticsInsight {
	var out []analyticsInsight

	if summary.concentrationTop3 >= 0.80 {
		out = append(out, analyticsInsight{
			label:    "High concentration risk",
			detail:   fmt.Sprintf("Top 3 providers account for %.0f%% of spend.", summary.concentrationTop3*100),
			severity: colorRed,
		})
	} else if summary.concentrationTop3 >= 0.60 {
		out = append(out, analyticsInsight{
			label:    "Moderate concentration",
			detail:   fmt.Sprintf("Top 3 providers account for %.0f%% of spend.", summary.concentrationTop3*100),
			severity: colorYellow,
		})
	}

	if summary.previousCostAvg > 0 {
		costDelta := (summary.recentCostAvg - summary.previousCostAvg) / summary.previousCostAvg
		if costDelta >= 0.25 {
			out = append(out, analyticsInsight{
				label:    "Spend acceleration",
				detail:   fmt.Sprintf("Recent daily cost is %.0f%% above the prior week baseline.", costDelta*100),
				severity: colorRed,
			})
		} else if costDelta <= -0.20 {
			out = append(out, analyticsInsight{
				label:    "Spend cooling",
				detail:   fmt.Sprintf("Recent daily cost is %.0f%% below the prior week baseline.", -costDelta*100),
				severity: colorGreen,
			})
		}
	}

	if summary.costVolatility > 0.55 {
		out = append(out, analyticsInsight{
			label:    "Volatile cost pattern",
			detail:   fmt.Sprintf("Daily cost volatility is high (CV %.2f).", summary.costVolatility),
			severity: colorYellow,
		})
	}

	if summary.peakCost > 0 && summary.recentCostAvg > 0 && summary.peakCost > summary.recentCostAvg*2.2 {
		out = append(out, analyticsInsight{
			label:    "Cost spike detected",
			detail:   fmt.Sprintf("Peak day %s reached %s.", formatDateLabel(summary.peakCostDate), formatUSD(summary.peakCost)),
			severity: colorRed,
		})
	}

	if summary.peakTokens > 0 && summary.recentTokensAvg > 0 && summary.peakTokens > summary.recentTokensAvg*1.8 {
		out = append(out, analyticsInsight{
			label:    "Token burst detected",
			detail:   fmt.Sprintf("Peak token day %s hit %s.", formatDateLabel(summary.peakTokenDate), formatTokens(summary.peakTokens)),
			severity: colorPeach,
		})
	}

	if len(data.usageGauges) > 0 {
		critical := 0
		warn := 0
		for _, g := range data.usageGauges {
			if g.pctUsed >= 90 {
				critical++
			} else if g.pctUsed >= 75 {
				warn++
			}
		}
		if critical > 0 {
			out = append(out, analyticsInsight{
				label:    "Usage windows near cap",
				detail:   fmt.Sprintf("%d windows are above 90%% utilization.", critical),
				severity: colorRed,
			})
		} else if warn > 0 {
			out = append(out, analyticsInsight{
				label:    "Usage windows warming up",
				detail:   fmt.Sprintf("%d windows are above 75%% utilization.", warn),
				severity: colorYellow,
			})
		}
	}

	if len(out) == 0 {
		out = append(out, analyticsInsight{
			label:    "Stable usage profile",
			detail:   "No major anomalies detected across spend, token flow, or limits.",
			severity: colorGreen,
		})
	}
	return out
}

func renderProviderCostStackedChart(data costData, w, h int) string {
	series, observedCount, estimatedCount := buildProviderDailyCostSeries(data)
	if len(series) == 0 {
		return ""
	}

	chart := RenderTimeChart(TimeChartSpec{
		Title:      "DAILY COST BY PROVIDER",
		Mode:       TimeChartStacked,
		Series:     series,
		Height:     h,
		MaxSeries:  8,
		WindowDays: 30,
		YFmt:       formatCostAxis,
	}, w)
	if estimatedCount > 0 {
		chart += "  " + dimStyle.Render(fmt.Sprintf("Observed daily cost: %d provider(s). Estimated from activity shape: %d provider(s).", observedCount, estimatedCount)) + "\n"
	}
	return chart
}

func renderTotalCostTrend(data costData, summary analyticsSummary, w, h int) string {
	providerSeries, _, _ := buildProviderDailyCostSeries(data)
	daily := aggregateSeriesByDate(providerSeries)
	if !hasNonZeroData(daily) {
		daily = summary.dailyCost
	}
	if !hasNonZeroData(daily) {
		return ""
	}
	series := []BrailleSeries{
		{Label: "daily cost", Color: colorTeal, Points: daily},
	}
	return RenderTimeChart(TimeChartSpec{
		Title:      "TOTAL COST OVER TIME",
		Mode:       TimeChartBars,
		Series:     series,
		Height:     h,
		WindowDays: 30,
		YFmt:       formatCostAxis,
	}, w)
}

func renderTotalTokenTrend(summary analyticsSummary, w, h int) string {
	daily := summary.dailyTokens
	if !hasNonZeroData(daily) {
		return ""
	}
	series := []BrailleSeries{
		{Label: "daily tokens", Color: colorSapphire, Points: daily},
	}
	ma := movingAveragePoints(daily, 7)
	if len(ma) > 0 {
		series = append(series, BrailleSeries{Label: "7d avg", Color: colorGreen, Points: ma})
	}
	return RenderTimeChart(TimeChartSpec{
		Title:      "TOTAL TOKENS OVER TIME",
		Mode:       TimeChartLine,
		Series:     series,
		Height:     h,
		WindowDays: 30,
		YFmt:       formatChartValue,
	}, w)
}

func renderModelOverTimeByProvider(data costData, w, h int) string {
	series := buildProviderActivitySeries(data, 12)
	if len(series) == 0 {
		return ""
	}
	return RenderTimeChart(TimeChartSpec{
		Title:      "DAILY ACTIVITY BY PROVIDER",
		Mode:       TimeChartStacked,
		Series:     series,
		Height:     h,
		MaxSeries:  8,
		WindowDays: 30,
		YFmt:       formatChartValue,
	}, w)
}

func renderProviderModelDailyUsage(data costData, w, maxRows int) string {
	spec, ok := buildProviderModelHeatmapSpec(data, maxRows, 18)
	if !ok {
		return ""
	}
	return RenderHeatmap(spec, w)
}

func buildProviderDailyCostSeries(data costData) ([]BrailleSeries, int, int) {
	groupByProvider := make(map[string]timeSeriesGroup, len(data.timeSeries))
	for _, g := range data.timeSeries {
		groupByProvider[g.providerName] = g
	}

	var out []BrailleSeries
	observedCount := 0
	estimatedCount := 0
	for _, p := range data.providers {
		if p.cost <= 0 && p.todayCost <= 0 && p.weekCost <= 0 {
			continue
		}
		var g *timeSeriesGroup
		if gg, ok := groupByProvider[p.name]; ok {
			g = &gg
		}
		pts, observed, estimated := deriveProviderDailyCostPoints(p, g)
		if !hasNonZeroData(pts) {
			continue
		}
		pts = clipSeriesPointsByRecentDates(pts, 30)
		if observed {
			observedCount++
		} else if estimated {
			estimatedCount++
		}
		out = append(out, BrailleSeries{
			Label:  truncStr(p.name, 20),
			Color:  p.color,
			Points: pts,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		li := seriesTotal(out[i].Points)
		lj := seriesTotal(out[j].Points)
		if li == lj {
			return out[i].Label < out[j].Label
		}
		return li > lj
	})
	if len(out) == 0 {
		for _, g := range data.timeSeries {
			pts, ok := g.series["cost"]
			if !ok || !hasNonZeroData(pts) {
				continue
			}
			observedCount++
			out = append(out, BrailleSeries{
				Label:  truncStr(g.providerName, 20),
				Color:  g.color,
				Points: clipSeriesPointsByRecentDates(pts, 30),
			})
		}
	}
	return out, observedCount, estimatedCount
}

func deriveProviderDailyCostPoints(p providerCostEntry, group *timeSeriesGroup) ([]core.TimePoint, bool, bool) {
	if group != nil {
		for _, key := range []string{"cost", "analytics_cost", "daily_cost"} {
			if pts, ok := group.series[key]; ok && hasNonZeroData(pts) {
				return pts, true, false
			}
		}
	}
	now := time.Now()
	nowDate := now.Format("2006-01-02")

	if p.todayCost > 0 {
		return []core.TimePoint{{Date: nowDate, Value: p.todayCost}}, true, false
	}

	if group != nil && p.weekCost > 0 {
		if activity := clipSeriesPointsByRecentDates(selectBestProviderCostWeightSeries(group.series), 7); hasNonZeroData(activity) {
			if scaled := scaleSeriesToTotal(activity, p.weekCost); hasNonZeroData(scaled) {
				return scaled, false, true
			}
		}
	}

	return nil, false, false
}

func scaleSeriesToTotal(activity []core.TimePoint, total float64) []core.TimePoint {
	if len(activity) == 0 || total <= 0 {
		return nil
	}
	sum := seriesTotal(activity)
	if sum <= 0 {
		return nil
	}
	out := make([]core.TimePoint, 0, len(activity))
	for _, a := range activity {
		out = append(out, core.TimePoint{
			Date:  a.Date,
			Value: total * (a.Value / sum),
		})
	}
	return out
}

func aggregateSeriesByDate(series []BrailleSeries) []core.TimePoint {
	if len(series) == 0 {
		return nil
	}
	byDate := make(map[string]float64)
	for _, s := range series {
		for _, p := range s.Points {
			if p.Value > 0 {
				byDate[p.Date] += p.Value
			}
		}
	}
	if len(byDate) == 0 {
		return nil
	}
	dates := lo.Keys(byDate)
	sort.Strings(dates)
	out := make([]core.TimePoint, 0, len(dates))
	for _, d := range dates {
		out = append(out, core.TimePoint{Date: d, Value: byDate[d]})
	}
	return out
}

func buildProviderActivitySeries(data costData, limit int) []BrailleSeries {
	type candidate struct {
		series BrailleSeries
		volume float64
	}
	var cands []candidate
	for _, g := range data.timeSeries {
		pts := selectBestProviderActivitySeries(g.series)
		if !hasNonZeroData(pts) {
			continue
		}
		pts = clipSeriesPointsByRecentDates(pts, 30)
		if !hasChartQuality(pts, 2) {
			continue
		}
		label := truncStr(g.providerName, 24)
		cands = append(cands, candidate{
			series: BrailleSeries{Label: label, Color: g.color, Points: pts},
			volume: seriesTotal(pts),
		})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].volume == cands[j].volume {
			return cands[i].series.Label < cands[j].series.Label
		}
		return cands[i].volume > cands[j].volume
	})
	if limit > 0 && len(cands) > limit {
		cands = cands[:limit]
	}
	out := make([]BrailleSeries, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.series)
	}
	return out
}

func renderProviderActivity7DChart(data costData, w int) string {
	type entry struct {
		name  string
		color lipgloss.Color
		value float64
	}
	var entries []entry
	for _, g := range data.timeSeries {
		pts := clipSeriesPointsByRecentDates(selectBestProviderActivitySeries(g.series), 7)
		v := seriesTotal(pts)
		if v <= 0 {
			continue
		}
		entries = append(entries, entry{name: g.providerName, color: g.color, value: v})
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].value == entries[j].value {
			return entries[i].name < entries[j].name
		}
		return entries[i].value > entries[j].value
	})
	if len(entries) > 8 {
		entries = entries[:8]
	}

	items := make([]chartItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, chartItem{
			Label:    truncStr(e.name, 22),
			Value:    e.value,
			Color:    e.color,
			SubLabel: "7d",
		})
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	sb.WriteString("  " + sectionStyle.Render("PROVIDER ACTIVITY (7D)") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")
	sb.WriteString(RenderHBarChart(items, clampInt(w-42, 14, 36), clampInt(w/4, 14, 24)))
	sb.WriteString("\n")
	return sb.String()
}

func renderDailyTokenDistributionChart(data costData, w int, limit int) string {
	series := buildProviderModelTokenDistributionSeries(data, limit)
	if len(series) == 0 {
		return ""
	}
	return RenderTimeChart(TimeChartSpec{
		Title:      "DAILY TOKEN DISTRIBUTION (Model · Provider)",
		Mode:       TimeChartStacked,
		Series:     series,
		Height:     9,
		MaxSeries:  limit,
		WindowDays: 30,
		YFmt:       formatChartValue,
	}, w)
}

func buildProviderModelTokenDistributionSeries(data costData, limit int) []BrailleSeries {
	type candidate struct {
		series BrailleSeries
		volume float64
	}
	var cands []candidate

	for _, g := range data.timeSeries {
		keys := lo.Keys(g.series)
		sort.Strings(keys)
		tokenKeys := make([]string, 0, len(keys))
		usageKeys := make([]string, 0, len(keys))
		for _, key := range keys {
			if strings.HasPrefix(key, "tokens_") {
				tokenKeys = append(tokenKeys, key)
			} else if strings.HasPrefix(key, "usage_model_") {
				usageKeys = append(usageKeys, key)
			}
		}
		modelKeys := tokenKeys
		if len(modelKeys) == 0 {
			modelKeys = usageKeys
		}
		for _, key := range modelKeys {
			pts := clipSeriesPointsByRecentDates(g.series[key], 30)
			if !hasNonZeroData(pts) {
				continue
			}

			model := key
			model = strings.TrimPrefix(model, "tokens_")
			model = strings.TrimPrefix(model, "usage_model_")
			label := truncStr(prettifyModelName(model)+" · "+g.providerName, 34)

			cands = append(cands, candidate{
				series: BrailleSeries{
					Label:  label,
					Color:  stableModelColor(model, g.providerID),
					Points: pts,
				},
				volume: seriesTotal(pts),
			})
		}
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].volume == cands[j].volume {
			return cands[i].series.Label < cands[j].series.Label
		}
		return cands[i].volume > cands[j].volume
	})
	if limit > 0 && len(cands) > limit {
		cands = cands[:limit]
	}

	out := make([]BrailleSeries, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.series)
	}
	return out
}

func selectBestProviderCostWeightSeries(series map[string][]core.TimePoint) []core.TimePoint {
	for _, key := range []string{
		"tokens_total",
		"messages",
		"sessions",
		"tool_calls",
		"requests",
		"tab_accepted",
		"composer_accepted",
	} {
		if pts, ok := series[key]; ok && hasNonZeroData(pts) {
			return pts
		}
	}
	keys := lo.Keys(series)
	sort.Strings(keys)
	for _, key := range keys {
		if strings.HasPrefix(key, "tokens_") || strings.HasPrefix(key, "usage_model_") || strings.HasPrefix(key, "usage_client_") {
			pts := series[key]
			if hasNonZeroData(pts) {
				return pts
			}
		}
	}
	return nil
}

func selectBestProviderActivitySeries(series map[string][]core.TimePoint) []core.TimePoint {
	for _, key := range []string{
		"tokens_total",
		"analytics_tokens",
		"requests",
		"analytics_requests",
		"messages",
		"sessions",
	} {
		if pts, ok := series[key]; ok && hasNonZeroData(pts) {
			return pts
		}
	}

	sumByDate := make(map[string]float64)
	keys := lo.Keys(series)
	sort.Strings(keys)
	for _, key := range keys {
		if strings.HasPrefix(key, "tokens_") || strings.HasPrefix(key, "usage_model_") || strings.HasPrefix(key, "usage_client_") {
			for _, p := range series[key] {
				if p.Value > 0 {
					sumByDate[p.Date] += p.Value
				}
			}
		}
	}
	if len(sumByDate) == 0 {
		return nil
	}
	dates := lo.Keys(sumByDate)
	sort.Strings(dates)
	out := make([]core.TimePoint, 0, len(dates))
	for _, d := range dates {
		out = append(out, core.TimePoint{Date: d, Value: sumByDate[d]})
	}
	return out
}

func buildProviderModelSeries(data costData, limit int) []BrailleSeries {
	type candidate struct {
		series BrailleSeries
		volume float64
	}
	var cands []candidate
	for _, g := range data.timeSeries {
		keys := lo.Keys(g.series)
		sort.Strings(keys)
		for _, key := range keys {
			pts := g.series[key]
			if !strings.HasPrefix(key, "tokens_") || !hasNonZeroData(pts) {
				continue
			}
			pts = clipSeriesPointsByRecentDates(pts, 30)
			if !hasChartQuality(pts, 3) {
				continue
			}
			model := prettifyModelName(strings.TrimPrefix(key, "tokens_"))
			label := truncStr(g.providerName+" · "+model, 30)
			cands = append(cands, candidate{
				series: BrailleSeries{
					Label:  label,
					Color:  stableModelColor(key, g.providerID),
					Points: pts,
				},
				volume: seriesTotal(pts),
			})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].volume == cands[j].volume {
			return cands[i].series.Label < cands[j].series.Label
		}
		return cands[i].volume > cands[j].volume
	})
	if limit > 0 && len(cands) > limit {
		cands = cands[:limit]
	}
	out := make([]BrailleSeries, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.series)
	}
	if len(out) == 0 {
		return buildProviderModelSeriesFallback(data, limit)
	}
	return out
}

func buildProviderModelSeriesFallback(data costData, limit int) []BrailleSeries {
	type candidate struct {
		series BrailleSeries
		volume float64
	}
	var cands []candidate
	for _, g := range data.timeSeries {
		keys := lo.Keys(g.series)
		sort.Strings(keys)
		for _, key := range keys {
			pts := g.series[key]
			if !strings.HasPrefix(key, "tokens_") || !hasNonZeroData(pts) {
				continue
			}
			model := prettifyModelName(strings.TrimPrefix(key, "tokens_"))
			cands = append(cands, candidate{
				series: BrailleSeries{
					Label:  truncStr(g.providerName+" · "+model, 30),
					Color:  stableModelColor(key, g.providerID),
					Points: clipSeriesPointsByRecentDates(pts, 30),
				},
				volume: seriesTotal(pts),
			})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].volume > cands[j].volume })
	if limit > 0 && len(cands) > limit {
		cands = cands[:limit]
	}
	out := make([]BrailleSeries, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.series)
	}
	return out
}

func buildProviderModelHeatmapSpec(data costData, maxRows int, lastDays int) (HeatmapSpec, bool) {
	type row struct {
		label string
		color lipgloss.Color
		vals  map[string]float64
		total float64
	}
	var rows []row
	dateSet := make(map[string]bool)

	for _, g := range data.timeSeries {
		keys := lo.Keys(g.series)
		sort.Strings(keys)
		for _, key := range keys {
			pts := g.series[key]
			if !strings.HasPrefix(key, "tokens_") {
				continue
			}
			total := seriesTotal(pts)
			if total <= 0 {
				continue
			}
			vals := make(map[string]float64, len(pts))
			for _, p := range pts {
				if p.Value > 0 {
					vals[p.Date] = p.Value
					dateSet[p.Date] = true
				}
			}
			model := prettifyModelName(strings.TrimPrefix(key, "tokens_"))
			rows = append(rows, row{
				label: truncStr(g.providerName+" · "+model, 42),
				color: stableModelColor(key, g.providerID),
				vals:  vals,
				total: total,
			})
		}
	}

	if len(rows) == 0 || len(dateSet) == 0 {
		return HeatmapSpec{}, false
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].total > rows[j].total })
	if maxRows > 0 && len(rows) > maxRows {
		rows = rows[:maxRows]
	}

	dates := lo.Keys(dateSet)
	sort.Strings(dates)
	dates = clipDatesToRecent(dates, lastDays)

	labels := make([]string, len(rows))
	rowColors := make([]lipgloss.Color, len(rows))
	values := make([][]float64, len(rows))
	for i, r := range rows {
		labels[i] = r.label
		rowColors[i] = r.color
		line := make([]float64, len(dates))
		for j, d := range dates {
			line[j] = r.vals[d]
		}
		values[i] = line
	}
	return HeatmapSpec{
		Title:     "DAILY USAGE HEATMAP (Provider · Model)",
		Rows:      labels,
		Cols:      dates,
		Values:    values,
		RowColors: rowColors,
		MaxCols:   0,
		RowScale:  true,
	}, true
}

func clipDatesToRecent(dates []string, days int) []string {
	if len(dates) == 0 || days <= 0 {
		return dates
	}
	maxDate, err := time.Parse("2006-01-02", dates[len(dates)-1])
	if err != nil {
		if len(dates) > days {
			return dates[len(dates)-days:]
		}
		return dates
	}
	cutoff := maxDate.AddDate(0, 0, -(days - 1))
	out := make([]string, 0, len(dates))
	for _, d := range dates {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		if t.Before(cutoff) || t.After(maxDate) {
			continue
		}
		out = append(out, d)
	}
	return out
}

func movingAveragePoints(points []core.TimePoint, window int) []core.TimePoint {
	if len(points) == 0 || window <= 1 {
		return nil
	}
	out := make([]core.TimePoint, 0, len(points))
	for i := range points {
		start := i - window + 1
		if start < 0 {
			start = 0
		}
		sum := 0.0
		n := 0
		for j := start; j <= i; j++ {
			sum += points[j].Value
			n++
		}
		if n == 0 {
			continue
		}
		out = append(out, core.TimePoint{Date: points[i].Date, Value: sum / float64(n)})
	}
	return out
}

func seriesTotal(points []core.TimePoint) float64 {
	total := 0.0
	for _, p := range points {
		total += p.Value
	}
	return total
}

func clipSeriesPointsByRecentDates(points []core.TimePoint, days int) []core.TimePoint {
	if len(points) == 0 || days <= 0 {
		return points
	}
	dates := make([]string, len(points))
	for i := range points {
		dates[i] = points[i].Date
	}
	dates = clipDatesToRecent(dates, days)
	if len(dates) == 0 {
		return points
	}
	allow := make(map[string]bool, len(dates))
	for _, d := range dates {
		allow[d] = true
	}
	out := make([]core.TimePoint, 0, len(points))
	for _, p := range points {
		if allow[p.Date] {
			out = append(out, p)
		}
	}
	return out
}

func hasChartQuality(points []core.TimePoint, minNonZeroDays int) bool {
	if len(points) == 0 {
		return false
	}
	nonZero := 0
	distinctVals := make(map[int64]bool)
	for _, p := range points {
		if p.Value <= 0 {
			continue
		}
		nonZero++
		// coarse bucket to avoid float noise.
		bucket := int64(p.Value * 1000)
		distinctVals[bucket] = true
	}
	if nonZero < minNonZeroDays {
		return false
	}
	return len(distinctVals) >= 2
}

func hasStrongSeries(points []core.TimePoint, minNonZeroDays int) bool {
	if !hasChartQuality(points, minNonZeroDays) {
		return false
	}
	minV := 0.0
	maxV := 0.0
	init := false
	for _, p := range points {
		if p.Value <= 0 {
			continue
		}
		if !init {
			minV = p.Value
			maxV = p.Value
			init = true
			continue
		}
		if p.Value < minV {
			minV = p.Value
		}
		if p.Value > maxV {
			maxV = p.Value
		}
	}
	if !init || minV == 0 {
		return false
	}
	return (maxV / minV) >= 1.2
}

func hasSeriesVariance(points []core.TimePoint) bool {
	if len(points) < 2 {
		return false
	}
	minV := points[0].Value
	maxV := points[0].Value
	for _, p := range points[1:] {
		if p.Value < minV {
			minV = p.Value
		}
		if p.Value > maxV {
			maxV = p.Value
		}
	}
	return maxV > minV
}

func computeAnalyticsSummary(data costData) analyticsSummary {
	var s analyticsSummary
	costByDate := make(map[string]float64)
	tokensByDate := make(map[string]float64)
	messagesByDate := make(map[string]float64)

	for _, g := range data.timeSeries {
		if pts, ok := g.series["cost"]; ok {
			for _, p := range pts {
				costByDate[p.Date] += p.Value
			}
		}

		hasTotalTokens := false
		if pts, ok := g.series["tokens_total"]; ok {
			hasTotalTokens = true
			for _, p := range pts {
				tokensByDate[p.Date] += p.Value
			}
		}
		if !hasTotalTokens {
			for key, pts := range g.series {
				if !strings.HasPrefix(key, "tokens_") {
					continue
				}
				for _, p := range pts {
					tokensByDate[p.Date] += p.Value
				}
			}
		}

		if pts, ok := g.series["messages"]; ok {
			for _, p := range pts {
				messagesByDate[p.Date] += p.Value
			}
		}
	}

	s.dailyCost = mapToSortedPoints(costByDate)
	s.dailyTokens = mapToSortedPoints(tokensByDate)
	s.dailyMessages = mapToSortedPoints(messagesByDate)
	s.activeDays = countNonZeroDays(s.dailyCost, s.dailyTokens, s.dailyMessages)

	s.peakCostDate, s.peakCost = maxPoint(s.dailyCost)
	s.peakTokenDate, s.peakTokens = maxPoint(s.dailyTokens)

	s.recentCostAvg, s.previousCostAvg = splitWindowAverages(s.dailyCost, 7)
	s.recentTokensAvg, s.previousTokensAvg = splitWindowAverages(s.dailyTokens, 7)
	s.costVolatility = coefficientOfVariation(s.dailyCost)
	s.tokenVolatility = coefficientOfVariation(s.dailyTokens)
	s.concentrationTop3 = providerConcentration(data.providers, 3)

	for _, p := range s.dailyCost {
		t, err := time.Parse("2006-01-02", p.Date)
		if err != nil {
			continue
		}
		wd := int(t.Weekday())
		s.dayOfWeekCost[wd] += p.Value
		s.dayOfWeekCount[wd]++
	}
	return s
}

func mapToSortedPoints(m map[string]float64) []core.TimePoint {
	keys := lo.Keys(m)
	sort.Strings(keys)

	out := make([]core.TimePoint, 0, len(keys))
	for _, k := range keys {
		out = append(out, core.TimePoint{Date: k, Value: m[k]})
	}
	return out
}

func maxPoint(points []core.TimePoint) (string, float64) {
	bestDate := ""
	best := 0.0
	for _, p := range points {
		if p.Value > best {
			bestDate = p.Date
			best = p.Value
		}
	}
	return bestDate, best
}

func splitWindowAverages(points []core.TimePoint, window int) (float64, float64) {
	if len(points) == 0 || window <= 0 {
		return 0, 0
	}
	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.Value
	}

	recentStart := len(values) - window
	if recentStart < 0 {
		recentStart = 0
	}
	recent := avg(values[recentStart:])

	prevStart := recentStart - window
	if prevStart < 0 {
		prevStart = 0
	}
	prevEnd := recentStart
	if prevEnd < prevStart {
		prevEnd = prevStart
	}
	prev := avg(values[prevStart:prevEnd])

	return recent, prev
}

func avg(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

func stddev(v []float64, mean float64) float64 {
	if len(v) < 2 {
		return 0
	}
	sum := 0.0
	for _, x := range v {
		d := x - mean
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(v)))
}

func coefficientOfVariation(points []core.TimePoint) float64 {
	if len(points) < 2 {
		return 0
	}
	values := make([]float64, 0, len(points))
	for _, p := range points {
		if p.Value > 0 {
			values = append(values, p.Value)
		}
	}
	if len(values) < 2 {
		return 0
	}
	m := avg(values)
	if m <= 0 {
		return 0
	}
	return stddev(values, m) / m
}

func providerConcentration(providers []providerCostEntry, topN int) float64 {
	if len(providers) == 0 || topN <= 0 {
		return 0
	}
	vals := make([]float64, 0, len(providers))
	total := 0.0
	for _, p := range providers {
		if p.cost <= 0 {
			continue
		}
		vals = append(vals, p.cost)
		total += p.cost
	}
	if total <= 0 || len(vals) == 0 {
		return 0
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] > vals[j] })
	if len(vals) < topN {
		topN = len(vals)
	}
	top := 0.0
	for i := 0; i < topN; i++ {
		top += vals[i]
	}
	return top / total
}

func countNonZeroDays(series ...[]core.TimePoint) int {
	days := make(map[string]bool)
	for _, pts := range series {
		for _, p := range pts {
			if p.Value > 0 {
				days[p.Date] = true
			}
		}
	}
	return len(days)
}

func renderWeekdayPattern(summary analyticsSummary, w int) string {
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	maxV := 0.0
	avgByDay := make([]float64, 7)
	for i := 0; i < 7; i++ {
		if summary.dayOfWeekCount[i] > 0 {
			avgByDay[i] = summary.dayOfWeekCost[i] / float64(summary.dayOfWeekCount[i])
		}
		if avgByDay[i] > maxV {
			maxV = avgByDay[i]
		}
	}
	if maxV <= 0 {
		return ""
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorFlamingo)
	sb.WriteString("  " + sectionStyle.Render("WEEKDAY COST PATTERN") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	barW := clampInt(w-24, 12, 36)
	for i := 0; i < 7; i++ {
		v := avgByDay[i]
		l := int(v / maxV * float64(barW))
		if l < 1 && v > 0 {
			l = 1
		}
		bar := lipgloss.NewStyle().Foreground(colorFlamingo).Render(strings.Repeat("█", l))
		track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", barW-l))
		sb.WriteString(fmt.Sprintf("  %s %s%s %s\n",
			dimStyle.Render(names[i]), bar, track, dimStyle.Render(formatCostAxis(v))))
	}
	return sb.String()
}

func renderTrendSummary(data costData, summary analyticsSummary, w int) string {
	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorYellow)
	sb.WriteString("  " + sectionStyle.Render("TREND SUMMARY") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")
	sb.WriteString("  " + dimStyle.Render("Cost 7d vs prev: ") + lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(renderTrendPercent(summary.recentCostAvg, summary.previousCostAvg)) + "\n")
	sb.WriteString("  " + dimStyle.Render("Tokens 7d vs prev: ") + lipgloss.NewStyle().Foreground(colorSapphire).Bold(true).Render(renderTrendPercent(summary.recentTokensAvg, summary.previousTokensAvg)) + "\n")
	sb.WriteString("  " + dimStyle.Render("Cost volatility (CV): ") + lipgloss.NewStyle().Foreground(colorPeach).Bold(true).Render(fmt.Sprintf("%.2f", summary.costVolatility)) + "\n")
	if summary.peakCost > 0 {
		sb.WriteString("  " + dimStyle.Render("Peak cost day: ") + lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatDateLabel(summary.peakCostDate)+" "+formatUSD(summary.peakCost)) + "\n")
	}
	return sb.String()
}

func renderModelMixChart(models []modelCostEntry, w int) string {
	if len(models) == 0 {
		return ""
	}
	top := make([]modelCostEntry, 0, len(models))
	for _, m := range models {
		if m.cost > 0 || (m.inputTokens+m.outputTokens) > 0 {
			top = append(top, m)
		}
	}
	if len(top) == 0 {
		return ""
	}
	sort.Slice(top, func(i, j int) bool {
		li := top[i].cost
		lj := top[j].cost
		if li == 0 && lj == 0 {
			li = top[i].inputTokens + top[i].outputTokens
			lj = top[j].inputTokens + top[j].outputTokens
		}
		return li > lj
	})
	if len(top) > 8 {
		top = top[:8]
	}

	items := make([]chartItem, 0, len(top))
	for _, m := range top {
		v := m.cost
		sub := formatTokens(m.inputTokens + m.outputTokens)
		if v <= 0 {
			v = m.inputTokens + m.outputTokens
			sub = formatUSD(m.cost)
		}
		items = append(items, chartItem{
			Label:    m.name,
			Value:    v,
			Color:    m.color,
			SubLabel: sub,
		})
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	sb.WriteString("  " + sectionStyle.Render("MODEL MIX (Top contributors)") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")
	sb.WriteString(RenderHBarChart(items, clampInt(w-42, 12, 28), clampInt(w/4, 18, 30)))
	sb.WriteString("\n")
	return sb.String()
}

func renderConcentrationSection(summary analyticsSummary, w int) string {
	if summary.concentrationTop3 <= 0 {
		return ""
	}
	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	sb.WriteString("  " + sectionStyle.Render("SPEND CONCENTRATION") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")
	pct := summary.concentrationTop3 * 100
	sb.WriteString("  " + dimStyle.Render("Top-3 provider share: ") + lipgloss.NewStyle().Foreground(colorLavender).Bold(true).Render(fmt.Sprintf("%.1f%%", pct)) + "\n")
	sb.WriteString("  " + RenderInlineGauge(pct, clampInt(w-28, 10, 40)) + "\n")
	return sb.String()
}

func renderEfficiencyScatter(models []modelCostEntry, w int) string {
	var pts []analyticsScatterPoint
	for _, m := range models {
		tokens := m.inputTokens + m.outputTokens
		if tokens <= 0 || m.cost <= 0 {
			continue
		}
		pts = append(pts, analyticsScatterPoint{
			label: truncStr(m.name, 18),
			x:     tokens,
			y:     m.cost / tokens * 1000,
			color: m.color,
		})
	}
	if len(pts) == 0 {
		return ""
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].x > pts[j].x })
	if len(pts) > 18 {
		pts = pts[:18]
	}

	chartW := clampInt(w-20, 26, 100)
	chartH := 12
	maxX := 0.0
	maxY := 0.0
	for _, p := range pts {
		if p.x > maxX {
			maxX = p.x
		}
		if p.y > maxY {
			maxY = p.y
		}
	}
	if maxX <= 0 {
		maxX = 1
	}
	if maxY <= 0 {
		maxY = 1
	}

	canvas := make([][]string, chartH)
	for y := range canvas {
		canvas[y] = make([]string, chartW)
		for x := range canvas[y] {
			canvas[y][x] = " "
		}
	}
	for i, p := range pts {
		x := int(p.x / maxX * float64(chartW-1))
		y := chartH - 1 - int(p.y/maxY*float64(chartH-1))
		if y < 0 {
			y = 0
		}
		if y >= chartH {
			y = chartH - 1
		}
		marker := "●"
		if i%2 == 1 {
			marker = "◆"
		}
		canvas[y][x] = lipgloss.NewStyle().Foreground(p.color).Render(marker)
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	sb.WriteString("  " + sectionStyle.Render("EFFICIENCY SCATTER (x=tokens, y=$/1K)") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")
	for y := 0; y < chartH; y++ {
		label := formatUSD(maxY * float64(chartH-1-y) / float64(chartH-1))
		sb.WriteString(fmt.Sprintf("  %6s ┤", dimStyle.Render(label)))
		sb.WriteString(strings.Join(canvas[y], ""))
		sb.WriteString("\n")
	}
	sb.WriteString("         └" + strings.Repeat("─", chartW) + "\n")
	sb.WriteString("          " + dimStyle.Render("0 tok") + strings.Repeat(" ", clampInt(chartW-14, 0, chartW)) + dimStyle.Render(formatTokens(maxX)+" tok") + "\n")

	topEff := bestEfficiencyModels(pts, 3)
	if len(topEff) > 0 {
		sb.WriteString("  " + dimStyle.Render("Best $/1K: "))
		for i, p := range topEff {
			if i > 0 {
				sb.WriteString(dimStyle.Render(" · "))
			}
			sb.WriteString(lipgloss.NewStyle().Foreground(p.color).Render(p.label))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func bestEfficiencyModels(pts []analyticsScatterPoint, n int) []analyticsScatterPoint {
	tmp := make([]analyticsScatterPoint, len(pts))
	copy(tmp, pts)
	sort.Slice(tmp, func(i, j int) bool { return tmp[i].y < tmp[j].y })
	if len(tmp) < n {
		n = len(tmp)
	}
	return tmp[:n]
}

func renderInputOutputRatio(models []modelCostEntry, w int) string {
	type ratioEntry struct {
		name  string
		in    float64
		out   float64
		ratio float64
		color lipgloss.Color
	}
	var rows []ratioEntry
	for _, m := range models {
		if m.inputTokens <= 0 || m.outputTokens <= 0 {
			continue
		}
		rows = append(rows, ratioEntry{
			name:  m.name,
			in:    m.inputTokens,
			out:   m.outputTokens,
			ratio: m.outputTokens / m.inputTokens,
			color: m.color,
		})
	}
	if len(rows) == 0 {
		return ""
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ratio > rows[j].ratio })
	if len(rows) > 8 {
		rows = rows[:8]
	}

	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	sb.WriteString("  " + sectionStyle.Render("OUTPUT/INPUT RATIO (Top models)") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	barW := clampInt(w-46, 8, 20)
	maxR := rows[0].ratio
	if maxR <= 0 {
		maxR = 1
	}
	for _, r := range rows {
		l := int(r.ratio / maxR * float64(barW))
		if l < 1 {
			l = 1
		}
		bar := lipgloss.NewStyle().Foreground(r.color).Render(strings.Repeat("█", l))
		track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", barW-l))
		sb.WriteString(fmt.Sprintf("  %-24s %s%s %s\n",
			dimStyle.Render(truncStr(r.name, 24)),
			bar, track,
			lipgloss.NewStyle().Foreground(r.color).Render(fmt.Sprintf("%.2fx", r.ratio))))
	}
	return sb.String()
}

func renderAnomalyTimeline(summary analyticsSummary, w int) string {
	if len(summary.dailyCost) < 3 {
		return ""
	}
	var sb strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorRed)
	sb.WriteString("  " + sectionStyle.Render("ANOMALY TIMELINE") + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4)) + "\n")

	costVals := make([]float64, len(summary.dailyCost))
	for i, p := range summary.dailyCost {
		costVals[i] = p.Value
	}
	mean := avg(costVals)
	sd := stddev(costVals, mean)
	if sd == 0 {
		sd = 1
	}

	found := 0
	for _, p := range summary.dailyCost {
		z := (p.Value - mean) / sd
		if z < 1.5 {
			continue
		}
		severity := colorYellow
		tag := "spike"
		if z >= 2.5 {
			severity = colorRed
			tag = "major spike"
		}
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(severity).Render("▲") + " " +
			dimStyle.Render(formatDateLabel(p.Date)) + " " +
			lipgloss.NewStyle().Foreground(severity).Bold(true).Render(formatUSD(p.Value)) + " " +
			dimStyle.Render(fmt.Sprintf("(%s, z=%.1f)", tag, z)) + "\n")
		found++
		if found >= 8 {
			break
		}
	}
	if found == 0 {
		sb.WriteString("  " + dimStyle.Render("No cost anomalies above z-score 1.5 in the visible series.") + "\n")
	}
	return sb.String()
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
	keys := lo.Keys(m)
	sort.Strings(keys)
	return keys
}
