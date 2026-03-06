package tui

import (
	"sort"
	"strconv"
	"strings"

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

var sortByLabels = []string{"Cost \u2193", "Name \u2191", "Tokens \u2193"}

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
