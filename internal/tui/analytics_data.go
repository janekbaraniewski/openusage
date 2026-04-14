package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	analyticsSortCostDesc   = 0
	analyticsSortNameAsc    = 1
	analyticsSortTokensDesc = 2
	analyticsSortCount      = 3
)

var sortByLabels = []string{"Cost \u2193", "Name \u2191", "Tokens \u2193"}

type costData struct {
	timeWindow    core.TimeWindow
	totalCost     float64
	totalInput    float64
	totalOutput   float64
	providerCount int
	activeCount   int
	referenceTime time.Time
	providers     []providerCostEntry
	models        []modelCostEntry
	budgets       []budgetEntry
	usageGauges   []usageGaugeEntry
	tokenActivity []tokenActivityEntry
	clients       []clientAnalyticsEntry
	projects      []projectAnalyticsEntry
	mcpServers    []mcpAnalyticsEntry
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

type clientAnalyticsEntry struct {
	name       string
	total      float64
	requests   float64
	sessions   float64
	seriesKind string
	series     []core.TimePoint
	color      lipgloss.Color
}

type projectAnalyticsEntry struct {
	name     string
	requests float64
	series   []core.TimePoint
	color    lipgloss.Color
}

type mcpAnalyticsEntry struct {
	name   string
	calls  float64
	series []core.TimePoint
	color  lipgloss.Color
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

func extractCostData(snapshots map[string]core.UsageSnapshot, filter string, timeWindow core.TimeWindow) costData {
	var data costData
	data.timeWindow = timeWindow
	data.snapshots = snapshots
	lowerFilter := strings.ToLower(filter)
	clientAgg := make(map[string]clientAnalyticsEntry)
	projectAgg := make(map[string]projectAnalyticsEntry)
	mcpAgg := make(map[string]mcpAnalyticsEntry)

	keys := core.SortedStringKeys(snapshots)

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
		if snap.Timestamp.After(data.referenceTime) {
			data.referenceTime = snap.Timestamp
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
		mergeClientAnalytics(clientAgg, extractClientAnalytics(snap, provColor))
		mergeProjectAnalytics(projectAgg, extractProjectAnalytics(snap, provColor))
		mergeMCPAnalytics(mcpAgg, extractMCPAnalytics(snap, provColor))

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
	if data.referenceTime.IsZero() {
		data.referenceTime = time.Now()
	}
	data.clients = collectClientAnalytics(clientAgg)
	data.projects = collectProjectAnalytics(projectAgg)
	data.mcpServers = collectMCPAnalytics(mcpAgg)
	sortClientAnalytics(data.clients)
	sortProjectAnalytics(data.projects)
	sortMCPAnalytics(data.mcpServers)

	return data
}

func extractProviderCost(snap core.UsageSnapshot) float64 {
	return core.ExtractAnalyticsCostSummary(snap).TotalCostUSD
}

func extractTodayCost(snap core.UsageSnapshot) float64 {
	return core.ExtractAnalyticsCostSummary(snap).TodayCostUSD
}

func extract7DayCost(snap core.UsageSnapshot) float64 {
	return core.ExtractAnalyticsCostSummary(snap).WeekCostUSD
}

func extractAllModels(snap core.UsageSnapshot, provColor lipgloss.Color) []modelCostEntry {
	records := core.ExtractAnalyticsModelUsage(snap)
	result := make([]modelCostEntry, 0, len(records))
	for _, record := range records {
		result = append(result, modelCostEntry{
			name:         prettifyModelName(record.Name),
			provider:     snap.AccountID,
			cost:         record.CostUSD,
			inputTokens:  record.InputTokens,
			outputTokens: record.OutputTokens,
			color:        stableModelColor(record.Name, snap.AccountID),
			confidence:   record.Confidence,
			window:       record.Window,
		})
	}
	return result
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

func extractClientAnalytics(snap core.UsageSnapshot, color lipgloss.Color) []clientAnalyticsEntry {
	clients, _ := core.ExtractClientBreakdown(snap)
	result := make([]clientAnalyticsEntry, 0, len(clients))
	for _, client := range clients {
		total := client.Total
		if total <= 0 {
			total = client.Input + client.Output + client.Cached + client.Reasoning
		}
		result = append(result, clientAnalyticsEntry{
			name:       prettifyClientName(client.Name),
			total:      total,
			requests:   client.Requests,
			sessions:   client.Sessions,
			seriesKind: client.SeriesKind,
			series:     client.Series,
			color:      color,
		})
	}
	return result
}

func extractProjectAnalytics(snap core.UsageSnapshot, color lipgloss.Color) []projectAnalyticsEntry {
	projects, _ := core.ExtractProjectUsage(snap)
	result := make([]projectAnalyticsEntry, 0, len(projects))
	for _, project := range projects {
		result = append(result, projectAnalyticsEntry{
			name:     prettifyProjectName(project.Name),
			requests: project.Requests,
			series:   project.Series,
			color:    color,
		})
	}
	return result
}

func prettifyProjectName(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return "Unscoped"
	}
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	return titleCase(s)
}

func extractMCPAnalytics(snap core.UsageSnapshot, color lipgloss.Color) []mcpAnalyticsEntry {
	servers, _ := core.ExtractMCPBreakdown(snap)
	result := make([]mcpAnalyticsEntry, 0, len(servers))
	for _, server := range servers {
		result = append(result, mcpAnalyticsEntry{
			name:   prettifyMCPServerName(server.RawName),
			calls:  server.Calls,
			series: server.Series,
			color:  color,
		})
	}
	return result
}

func mergeClientAnalytics(dst map[string]clientAnalyticsEntry, entries []clientAnalyticsEntry) {
	for _, entry := range entries {
		merged := dst[entry.name]
		merged.name = entry.name
		merged.total += entry.total
		merged.requests += entry.requests
		merged.sessions += entry.sessions
		if merged.seriesKind == "" {
			merged.seriesKind = entry.seriesKind
		}
		merged.series = mergeAnalyticsSeries(merged.series, entry.series)
		if merged.color == "" {
			merged.color = colorForClient(nil, entry.name)
		}
		dst[entry.name] = merged
	}
}

func mergeProjectAnalytics(dst map[string]projectAnalyticsEntry, entries []projectAnalyticsEntry) {
	for _, entry := range entries {
		merged := dst[entry.name]
		merged.name = entry.name
		merged.requests += entry.requests
		merged.series = mergeAnalyticsSeries(merged.series, entry.series)
		if merged.color == "" {
			merged.color = colorForProject(nil, entry.name)
		}
		dst[entry.name] = merged
	}
}

func mergeMCPAnalytics(dst map[string]mcpAnalyticsEntry, entries []mcpAnalyticsEntry) {
	for _, entry := range entries {
		merged := dst[entry.name]
		merged.name = entry.name
		merged.calls += entry.calls
		merged.series = mergeAnalyticsSeries(merged.series, entry.series)
		if merged.color == "" {
			merged.color = colorForTool(nil, entry.name)
		}
		dst[entry.name] = merged
	}
}

func collectClientAnalytics(entries map[string]clientAnalyticsEntry) []clientAnalyticsEntry {
	out := make([]clientAnalyticsEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	return out
}

func collectProjectAnalytics(entries map[string]projectAnalyticsEntry) []projectAnalyticsEntry {
	out := make([]projectAnalyticsEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	return out
}

func collectMCPAnalytics(entries map[string]mcpAnalyticsEntry) []mcpAnalyticsEntry {
	out := make([]mcpAnalyticsEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	return out
}

func mergeAnalyticsSeries(left, right []core.TimePoint) []core.TimePoint {
	if len(left) == 0 {
		return append([]core.TimePoint(nil), right...)
	}
	if len(right) == 0 {
		return append([]core.TimePoint(nil), left...)
	}
	byDate := make(map[string]float64, len(left)+len(right))
	for _, point := range left {
		byDate[point.Date] += point.Value
	}
	for _, point := range right {
		byDate[point.Date] += point.Value
	}
	return core.SortedTimePoints(byDate)
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

func sortClientAnalytics(clients []clientAnalyticsEntry) {
	sort.Slice(clients, func(i, j int) bool {
		left := clients[i].total
		if left <= 0 {
			left = clients[i].requests
		}
		right := clients[j].total
		if right <= 0 {
			right = clients[j].requests
		}
		if left == right {
			return clients[i].name < clients[j].name
		}
		return left > right
	})
}

func sortProjectAnalytics(projects []projectAnalyticsEntry) {
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].requests == projects[j].requests {
			return projects[i].name < projects[j].name
		}
		return projects[i].requests > projects[j].requests
	})
}

func sortMCPAnalytics(servers []mcpAnalyticsEntry) {
	sort.Slice(servers, func(i, j int) bool {
		if servers[i].calls == servers[j].calls {
			return servers[i].name < servers[j].name
		}
		return servers[i].calls > servers[j].calls
	})
}
