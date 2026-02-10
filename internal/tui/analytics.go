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
	rateLimits    []rateLimitInfo
	quotas        []quotaInfo
	tokenActivity []tokenActivityEntry
	timeSeries    []timeSeriesGroup
}

type timeSeriesGroup struct {
	providerID   string
	providerName string
	color        lipgloss.Color
	series       map[string][]core.TimePoint
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

type rateLimitInfo struct {
	provider string
	name     string
	pctUsed  float64
	window   string
	color    lipgloss.Color
}

type quotaInfo struct {
	provider     string
	model        string
	pctRemaining float64
	window       string
	color        lipgloss.Color
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

// ═══════════════════════════════════════════════════════════════════════════
// DATA EXTRACTION — handles every provider's metric naming convention
// ═══════════════════════════════════════════════════════════════════════════

func extractCostData(snapshots map[string]core.QuotaSnapshot, filter string) costData {
	var data costData
	lowerFilter := strings.ToLower(filter)

	// Sort snapshot keys for deterministic ordering (prevents flickering)
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
		data.burnRate += extractBurnRate(snap)

		models := extractAllModels(snap, provColor)
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

		data.budgets = append(data.budgets, extractBudgets(snap, provColor, extractBurnRate(snap))...)
		data.rateLimits = append(data.rateLimits, extractRateLimits(snap, provColor)...)
		data.quotas = append(data.quotas, extractQuotas(snap, provColor)...)
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

	// Flatten models
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
		"individual_spend",
		"jsonl_total_cost_usd", "total_cost_usd", "plan_total_spend_usd",
		"daily_cost_usd", "block_cost_usd",
		"credits",
	} {
		if m, ok := snap.Metrics[key]; ok && m.Used != nil && *m.Used > 0 {
			return *m.Used
		}
	}

	cost := estimateCostFromTokens(snap)
	if cost > 0 {
		return cost
	}

	return 0
}

func estimateCostFromTokens(snap core.QuotaSnapshot) float64 {
	type pricing struct{ input, output float64 }
	knownPricing := map[string]pricing{
		"o3":                            {2.0, 8.0},
		"o3-pro":                        {20.0, 80.0},
		"o4-mini":                       {1.10, 4.40},
		"o3-mini":                       {1.10, 4.40},
		"gpt-4.1":                       {2.0, 8.0},
		"gpt-4.1-mini":                  {0.40, 1.60},
		"gpt-4.1-nano":                  {0.10, 0.40},
		"gpt-4o":                        {2.50, 10.0},
		"gpt-4o-mini":                   {0.15, 0.60},
		"gpt-5.2-codex":                 {2.0, 8.0},
		"claude-opus-4-6":               {15.0, 75.0},
		"claude-4.5-opus-high-thinking": {15.0, 75.0},
		"claude-sonnet-4-5":             {3.0, 15.0},
		"claude-4.5-sonnet":             {3.0, 15.0},
		"claude-4.5-sonnet-thinking":    {3.0, 15.0},
	}

	var sessionIn, sessionOut float64
	if m, ok := snap.Metrics["session_input_tokens"]; ok && m.Used != nil {
		sessionIn = *m.Used
	}
	if m, ok := snap.Metrics["session_output_tokens"]; ok && m.Used != nil {
		sessionOut = *m.Used
	}
	if sessionIn == 0 && sessionOut == 0 {
		return 0
	}

	model := ""
	if m, ok := snap.Raw["model"]; ok {
		model = strings.ToLower(m)
	}
	if m, ok := snap.Raw["current_model"]; ok && model == "" {
		model = strings.ToLower(m)
	}

	bestPricing := pricing{2.0, 8.0}
	for name, p := range knownPricing {
		if strings.Contains(model, strings.ToLower(name)) {
			bestPricing = p
			break
		}
	}

	cost := (sessionIn/1_000_000)*bestPricing.input + (sessionOut/1_000_000)*bestPricing.output
	return cost
}

func extractBurnRate(snap core.QuotaSnapshot) float64 {
	if m, ok := snap.Metrics["burn_rate_usd_per_hour"]; ok && m.Used != nil {
		return *m.Used
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

func extractBudgets(snap core.QuotaSnapshot, color lipgloss.Color, burnRate float64) []budgetEntry {
	var result []budgetEntry

	if m, ok := snap.Metrics["spend_limit"]; ok && m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		result = append(result, budgetEntry{
			name: snap.AccountID + " (team)", used: *m.Used, limit: *m.Limit,
			color: color, burnRate: burnRate,
		})
		if ind, ok2 := snap.Metrics["individual_spend"]; ok2 && ind.Used != nil && *ind.Used > 0 {
			result = append(result, budgetEntry{
				name: snap.AccountID + " (you)", used: *ind.Used, limit: *m.Limit,
				color: color, burnRate: burnRate,
			})
		}
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

// ─── Rate Limit Extraction ──────────────────────────────────────────────────

func extractRateLimits(snap core.QuotaSnapshot, color lipgloss.Color) []rateLimitInfo {
	var result []rateLimitInfo

	// Sort metric keys for deterministic order
	mkeys := sortedMetricKeys(snap.Metrics)
	for _, key := range mkeys {
		m := snap.Metrics[key]
		isRate := strings.HasPrefix(key, "rate_limit_") ||
			key == "rpm" || key == "tpm" || key == "rpd" || key == "tpd"
		if !isRate {
			continue
		}

		pctUsed := float64(0)
		if m.Unit == "%" && m.Used != nil {
			pctUsed = *m.Used
		} else if m.Limit != nil && m.Used != nil && *m.Limit > 0 {
			pctUsed = *m.Used / *m.Limit * 100
		} else if m.Limit != nil && m.Remaining != nil && *m.Limit > 0 {
			pctUsed = (*m.Limit - *m.Remaining) / *m.Limit * 100
		} else {
			continue
		}

		name := key
		if strings.HasPrefix(key, "rate_limit_") {
			name = strings.TrimPrefix(key, "rate_limit_")
		}
		name = prettifyKey(name)
		window := m.Window
		if window == "" {
			window = "current"
		}

		result = append(result, rateLimitInfo{
			provider: snap.AccountID,
			name:     name,
			pctUsed:  pctUsed,
			window:   window,
			color:    color,
		})
	}
	return result
}

// ─── Quota Extraction ───────────────────────────────────────────────────────

func extractQuotas(snap core.QuotaSnapshot, color lipgloss.Color) []quotaInfo {
	var result []quotaInfo
	skipKeys := map[string]bool{
		"rpm": true, "tpm": true, "rpd": true, "tpd": true,
		"spend_limit": true, "plan_spend": true, "plan_included": true,
		"plan_bonus": true, "plan_percent_used": true, "individual_spend": true,
		"credits": true, "credit_balance": true, "total_balance": true,
		"daily_cost_usd": true, "total_cost_usd": true, "block_cost_usd": true,
		"jsonl_total_cost_usd": true, "burn_rate_usd_per_hour": true,
		"context_window": true, "total_messages": true, "total_sessions": true,
		"messages_today": true, "tool_calls_today": true, "sessions_today": true,
		"total_conversations": true, "plan_total_spend_usd": true, "plan_limit_usd": true,
	}

	mkeys := sortedMetricKeys(snap.Metrics)
	for _, key := range mkeys {
		m := snap.Metrics[key]
		if skipKeys[key] {
			continue
		}
		if strings.HasPrefix(key, "model_") || strings.HasPrefix(key, "input_tokens_") ||
			strings.HasPrefix(key, "output_tokens_") || strings.HasPrefix(key, "rate_limit_") ||
			strings.HasPrefix(key, "session_") || strings.HasPrefix(key, "tokens_today_") {
			continue
		}

		pctRemaining := float64(-1)
		if m.Remaining != nil && m.Limit != nil && *m.Limit > 0 {
			pctRemaining = *m.Remaining / *m.Limit * 100
		} else if m.Unit == "%" || m.Unit == "quota" {
			if m.Remaining != nil {
				pctRemaining = *m.Remaining
			}
		}
		if pctRemaining < 0 {
			continue
		}

		window := m.Window
		if window == "" {
			window = "current"
		}
		result = append(result, quotaInfo{
			provider:     snap.AccountID,
			model:        prettifyModelName(key),
			pctRemaining: pctRemaining,
			window:       window,
			color:        color,
		})
	}
	return result
}

// ─── Token Activity Extraction ──────────────────────────────────────────────

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
// RENDERING — Grafana-style panel grid dashboard
// ═══════════════════════════════════════════════════════════════════════════

func (m Model) renderAnalyticsContent(w, h int) string {
	data := extractCostData(m.snapshots, m.analyticsFilter)
	sortProviders(data.providers, m.analyticsSortBy)
	sortModels(data.models, m.analyticsSortBy)

	var sb strings.Builder

	// ── Status bar ──
	renderStatusBar(&sb, m.analyticsSortBy, m.analyticsFilter, w)

	// ── Row 1: Summary KPI cards ──
	sb.WriteString(buildSummaryCards(data, w))
	sb.WriteString("\n")

	// ── Row 2: Provider Spend | Cost Distribution (side by side) ──
	if data.totalCost > 0 && len(data.providers) > 0 {
		provItems := toProviderItems(data.providers, data.totalCost)
		row := PanelRow{}

		// Left panel: provider bar chart
		barW := (w/2 - 8) - 28
		if barW < 6 {
			barW = 6
		}
		provChart := RenderHBarChart(provItems, barW, 24)
		row.Panels = append(row.Panels, Panel{
			Title: "Provider Spend", Icon: "💰",
			Content: provChart, Span: 1, Color: colorRosewater,
		})

		// Right panel: distribution + vertical comparison
		var distContent strings.Builder
		if len(provItems) >= 2 {
			distContent.WriteString(RenderDistributionBar(provItems, w/2-8))
			distContent.WriteString("\n\n")
			// Add legend
			for _, item := range provItems {
				dot := lipgloss.NewStyle().Foreground(item.Color).Render("██")
				distContent.WriteString(fmt.Sprintf(" %s %s %s %s\n",
					dot,
					lipgloss.NewStyle().Foreground(item.Color).Width(20).Render(item.Label),
					lipgloss.NewStyle().Foreground(colorRosewater).Bold(true).Render(formatUSD(item.Value)),
					dimStyle.Render(item.SubLabel)))
			}
		} else if len(provItems) == 1 {
			distContent.WriteString(fmt.Sprintf(" %s  %s\n",
				lipgloss.NewStyle().Foreground(provItems[0].Color).Bold(true).Render(provItems[0].Label),
				lipgloss.NewStyle().Foreground(colorRosewater).Bold(true).Render(formatUSD(provItems[0].Value))))
		}
		row.Panels = append(row.Panels, Panel{
			Title: "Cost Distribution", Icon: "🍩",
			Content: distContent.String(), Span: 1, Color: colorPeach,
		})

		sb.WriteString(renderPanelGrid([]PanelRow{row}, w))
		sb.WriteString("\n")
	}

	// ── Row 3: Rate Limits | Quotas (side by side) ──
	if len(data.rateLimits) > 0 || len(data.quotas) > 0 {
		row := PanelRow{}
		if len(data.rateLimits) > 0 {
			var rlContent strings.Builder
			renderRateLimitsContent(&rlContent, data.rateLimits, w/2-6)
			row.Panels = append(row.Panels, Panel{
				Title: "Rate Limits", Icon: "⚡",
				Content: rlContent.String(), Span: 1, Color: colorYellow,
			})
		}
		if len(data.quotas) > 0 {
			var qContent strings.Builder
			renderQuotaContent(&qContent, data.quotas, w/2-6)
			panelW := 1
			if len(data.rateLimits) == 0 {
				panelW = 2
			}
			row.Panels = append(row.Panels, Panel{
				Title: "Quota Usage", Icon: "📊",
				Content: qContent.String(), Span: panelW, Color: colorLavender,
			})
		}
		// If only rate limits, make it full width
		if len(data.quotas) == 0 && len(row.Panels) == 1 {
			row.Panels[0].Span = 2
		}
		sb.WriteString(renderPanelGrid([]PanelRow{row}, w))
		sb.WriteString("\n")
	}

	// ── Row 4: Activity Overview | Token I/O (side by side) ──
	if len(data.tokenActivity) > 0 || data.totalInput > 0 || data.totalOutput > 0 {
		row := PanelRow{}
		if len(data.tokenActivity) > 0 {
			var actContent strings.Builder
			renderTokenActivityContent(&actContent, data.tokenActivity, w/2-6)
			row.Panels = append(row.Panels, Panel{
				Title: "Activity Overview", Icon: "📋",
				Content: actContent.String(), Span: 1, Color: colorGreen,
			})
		}
		if data.totalInput > 0 || data.totalOutput > 0 {
			tokenIO := RenderTokenBreakdown(data.totalInput, data.totalOutput, w/2-8)
			row.Panels = append(row.Panels, Panel{
				Title: "Input vs Output", Icon: "📐",
				Content: tokenIO, Span: 1, Color: colorSapphire,
			})
		}
		if len(row.Panels) == 1 {
			row.Panels[0].Span = 2
		}
		sb.WriteString(renderPanelGrid([]PanelRow{row}, w))
		sb.WriteString("\n")
	}

	// ── Row 5: Model Spend chart (full width) ──
	costModels := filterCostModels(data.models)
	if len(costModels) > 0 {
		costItems := toModelItems(costModels)
		sortChartItems(costItems)

		barW := w - 46
		barW = clampInt(barW, 10, 60)
		chart := RenderHBarChart(costItems, barW, 30)
		sb.WriteString(renderPanelGrid([]PanelRow{{Panels: []Panel{{
			Title: "Model Spend", Icon: "🤖",
			Content: chart, Span: 2, Color: colorTeal,
		}}}}, w))
		sb.WriteString("\n")
	}

	// ── Row 6: All Models table (full width) ──
	allModels := filterTokenModels(data.models)
	if len(allModels) > 0 {
		var tbl strings.Builder
		renderAllModelsContent(&tbl, allModels, w-6)
		sb.WriteString(renderPanelGrid([]PanelRow{{Panels: []Panel{{
			Title: "All Models", Icon: "📋",
			Content: tbl.String(), Span: 2, Color: colorSubtext,
		}}}}, w))
		sb.WriteString("\n")
	}

	// ── Row 7: Model Comparison vertical chart (full width) ──
	if len(costModels) >= 2 {
		costItems := toModelItems(costModels)
		sortChartItems(costItems)
		chartH := clampInt(len(costItems)*2+4, 8, 18)
		vChart := RenderVerticalBarChart(costItems, w-8, chartH, "")
		// Build legend string
		var legend strings.Builder
		for _, item := range costItems {
			dot := lipgloss.NewStyle().Foreground(item.Color).Render("██")
			legend.WriteString(fmt.Sprintf(" %s %s %s %s\n",
				dot,
				lipgloss.NewStyle().Foreground(item.Color).Width(28).Render(item.Label),
				lipgloss.NewStyle().Foreground(colorRosewater).Bold(true).Render(formatUSD(item.Value)),
				dimStyle.Render(item.SubLabel)))
		}
		sb.WriteString(renderPanelGrid([]PanelRow{{Panels: []Panel{{
			Title: "Model Comparison", Icon: "📈",
			Content: vChart + "\n" + legend.String(), Span: 2, Color: colorBlue,
		}}}}, w))
		sb.WriteString("\n")
	}

	// ── Row 8: Time-series charts ──
	if len(data.timeSeries) > 0 {
		renderTimeSeriesPanels(&sb, data.timeSeries, w)
	}

	// ── Row 9: Budget | Burn Rate (side by side) ──
	if len(data.budgets) > 0 || data.burnRate > 0 {
		row := PanelRow{}
		if len(data.budgets) > 0 {
			var budgetContent strings.Builder
			for _, b := range data.budgets {
				barW := (w/2 - 8) - 30
				barW = clampInt(barW, 8, 40)
				budgetContent.WriteString(RenderBudgetGauge(b.name, b.used, b.limit, barW, 18, b.color, b.burnRate))
				budgetContent.WriteString("\n")
			}
			row.Panels = append(row.Panels, Panel{
				Title: "Budget Utilization", Icon: "💳",
				Content: budgetContent.String(), Span: 1, Color: colorMaroon,
			})
		}
		if data.burnRate > 0 {
			row.Panels = append(row.Panels, Panel{
				Title: "Burn Rate", Icon: "🔮",
				Content: buildBurnRateContent(data, w/2-8), Span: 1, Color: colorPeach,
			})
		}
		if len(row.Panels) == 1 {
			row.Panels[0].Span = 2
		}
		sb.WriteString(renderPanelGrid([]PanelRow{row}, w))
		sb.WriteString("\n")
	}

	// ── Row 10: Cost Efficiency | Top Spenders (side by side) ──
	effItems := buildEfficiencyItems(data.models)
	costItems2 := toModelItems(filterCostModels(data.models))
	sortChartItems(costItems2)
	if len(effItems) > 0 || len(costItems2) >= 3 {
		row := PanelRow{}
		if len(effItems) > 0 {
			barW := (w/2 - 8) - 28
			barW = clampInt(barW, 8, 35)
			row.Panels = append(row.Panels, Panel{
				Title: "Cost Efficiency ($/1K tok)", Icon: "⚙️",
				Content: RenderEfficiencyChart(effItems, barW, 22), Span: 1, Color: colorSky,
			})
		}
		if len(costItems2) >= 3 {
			lb := RenderLeaderboard(costItems2, w/2-6, 10, "")
			row.Panels = append(row.Panels, Panel{
				Title: "Top Spenders", Icon: "🏆",
				Content: lb, Span: 1, Color: colorFlamingo,
			})
		}
		if len(row.Panels) == 1 {
			row.Panels[0].Span = 2
		}
		sb.WriteString(renderPanelGrid([]PanelRow{row}, w))
		sb.WriteString("\n")
	}

	// ── Row 11: Per-provider drill-down ──
	for _, prov := range data.providers {
		if len(prov.models) == 0 {
			continue
		}
		var drillContent strings.Builder
		nameW := 28
		colW := 10
		for _, mdl := range prov.models {
			n := mdl.name
			if len(n) > nameW {
				n = n[:nameW-1] + "…"
			}
			costStr := dimStyle.Render("—")
			if mdl.cost > 0 {
				costStr = lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatUSD(mdl.cost))
			}
			drillContent.WriteString(fmt.Sprintf(" %s %*s %*s %*s\n",
				lipgloss.NewStyle().Foreground(mdl.color).Width(nameW).Render(n),
				colW, dimStyle.Render(formatTokens(mdl.inputTokens)),
				colW, dimStyle.Render(formatTokens(mdl.outputTokens)),
				colW, costStr))
		}
		pctStr := ""
		if data.totalCost > 0 && prov.cost > 0 {
			pctStr = fmt.Sprintf(" (%.1f%%)", prov.cost/data.totalCost*100)
		}
		title := fmt.Sprintf("%s  %s%s", prov.name, formatUSD(prov.cost), pctStr)
		sb.WriteString(renderPanelGrid([]PanelRow{{Panels: []Panel{{
			Title: title, Icon: "████",
			Content: drillContent.String(), Span: 2, Color: prov.color,
		}}}}, w))
		sb.WriteString("\n")
	}

	// ── Empty state ──
	if data.totalCost == 0 && len(data.models) == 0 && len(data.budgets) == 0 &&
		len(data.rateLimits) == 0 && len(data.quotas) == 0 && len(data.tokenActivity) == 0 {
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

// ─── Summary KPI Cards ──────────────────────────────────────────────────────

func buildSummaryCards(data costData, w int) string {
	type card struct {
		title, value, sub string
		color             lipgloss.Color
	}

	cards := []card{
		{"Your Spend", formatUSD(data.totalCost),
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
	if len(data.rateLimits) > 0 {
		cards = append(cards, card{
			"Rate Limits", fmt.Sprintf("%d", len(data.rateLimits)), "tracked", colorYellow})
	}
	if len(data.quotas) > 0 {
		cards = append(cards, card{
			"Quotas", fmt.Sprintf("%d", len(data.quotas)), "tracked", colorLavender})
	}

	n := len(cards)
	cardW := (w - 2 - (n-1)*2) / n
	cardW = clampInt(cardW, 16, 24)

	var rendered []string
	for _, c := range cards {
		rendered = append(rendered, RenderSummaryCard(c.title, c.value, c.sub, cardW, c.color))
	}
	return " " + lipgloss.JoinHorizontal(lipgloss.Top, intersperse(rendered, "  ")...)
}

// ─── Panel Content Builders ─────────────────────────────────────────────────

func renderRateLimitsContent(sb *strings.Builder, limits []rateLimitInfo, panelW int) {
	type provGroup struct {
		provider string
		color    lipgloss.Color
		limits   []rateLimitInfo
	}
	groups := make(map[string]*provGroup)
	var order []string
	for _, rl := range limits {
		g, ok := groups[rl.provider]
		if !ok {
			g = &provGroup{provider: rl.provider, color: rl.color}
			groups[rl.provider] = g
			order = append(order, rl.provider)
		}
		g.limits = append(g.limits, rl)
	}
	sort.Strings(order) // ← deterministic provider order

	labelW := 20
	barW := panelW - labelW - 24
	barW = clampInt(barW, 6, 35)

	for _, provName := range order {
		g := groups[provName]
		sb.WriteString(lipgloss.NewStyle().Foreground(g.color).Bold(true).Render(g.provider) + "\n")
		for _, rl := range g.limits {
			pct := clampFloat(rl.pctUsed, 0, 100)
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
			name := truncStr(rl.name, labelW)

			sb.WriteString(fmt.Sprintf("  %-*s %s%s %s %s\n",
				labelW, dimStyle.Render(name), bar, track,
				lipgloss.NewStyle().Foreground(barColor).Bold(true).Render(fmt.Sprintf("%3.0f%%", pct)),
				dimStyle.Render(rl.window)))
		}
	}
}

func renderQuotaContent(sb *strings.Builder, quotas []quotaInfo, panelW int) {
	type provGroup struct {
		provider string
		color    lipgloss.Color
		quotas   []quotaInfo
	}
	groups := make(map[string]*provGroup)
	var order []string
	for _, q := range quotas {
		g, ok := groups[q.provider]
		if !ok {
			g = &provGroup{provider: q.provider, color: q.color}
			groups[q.provider] = g
			order = append(order, q.provider)
		}
		g.quotas = append(g.quotas, q)
	}
	sort.Strings(order) // ← deterministic

	labelW := 26
	barW := panelW - labelW - 24
	barW = clampInt(barW, 6, 35)

	for _, provName := range order {
		g := groups[provName]
		sb.WriteString(lipgloss.NewStyle().Foreground(g.color).Bold(true).Render(g.provider) + "\n")
		for _, q := range g.quotas {
			pctRemaining := clampFloat(q.pctRemaining, 0, 100)
			pctUsed := 100 - pctRemaining
			filled := int(pctUsed / 100 * float64(barW))
			empty := barW - filled

			barColor := colorGreen
			if pctRemaining < 15 {
				barColor = colorRed
			} else if pctRemaining < 40 {
				barColor = colorYellow
			}

			bar := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("█", filled))
			track := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", empty))
			name := truncStr(q.model, labelW)

			sb.WriteString(fmt.Sprintf("  %-*s %s%s %s %s\n",
				labelW, dimStyle.Render(name), bar, track,
				lipgloss.NewStyle().Foreground(barColor).Bold(true).Render(fmt.Sprintf("%3.0f%%", pctRemaining)),
				dimStyle.Render(q.window)))
		}
	}
}

func renderTokenActivityContent(sb *strings.Builder, entries []tokenActivityEntry, panelW int) {
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
	sort.Strings(order) // ← deterministic

	for _, provName := range order {
		g := groups[provName]
		sb.WriteString(lipgloss.NewStyle().Foreground(g.color).Bold(true).Render(g.provider) + "\n")
		for _, e := range g.entries {
			name := truncStr(e.name, 18)
			var parts []string
			if e.input > 0 {
				parts = append(parts,
					lipgloss.NewStyle().Foreground(colorSapphire).Render("↓"+formatTokens(e.input)))
			}
			if e.output > 0 {
				parts = append(parts,
					lipgloss.NewStyle().Foreground(colorPeach).Render("↑"+formatTokens(e.output)))
			}
			if e.cached > 0 {
				parts = append(parts,
					lipgloss.NewStyle().Foreground(colorTeal).Render("⚡"+formatTokens(e.cached)))
			}
			if e.total > 0 && e.input == 0 && e.output == 0 {
				parts = append(parts,
					lipgloss.NewStyle().Foreground(colorRosewater).Bold(true).Render(formatTokens(e.total)))
			}
			sb.WriteString(fmt.Sprintf("  %-18s %s  %s\n",
				dimStyle.Render(name), strings.Join(parts, " "), dimStyle.Render(e.window)))
		}
	}
}

func renderAllModelsContent(sb *strings.Builder, models []modelCostEntry, tableW int) {
	nameW := 28
	provW := 14
	colW := 10

	sb.WriteString(fmt.Sprintf(" %-*s %-*s %*s %*s %*s\n",
		nameW, dimStyle.Bold(true).Render("Model"),
		provW, dimStyle.Bold(true).Render("Provider"),
		colW, dimStyle.Bold(true).Render("Input"),
		colW, dimStyle.Bold(true).Render("Output"),
		colW, dimStyle.Bold(true).Render("Cost")))
	sb.WriteString(" " + lipgloss.NewStyle().Foreground(colorSurface1).Render(
		strings.Repeat("─", nameW+provW+colW*3+4)) + "\n")

	sorted := make([]modelCostEntry, len(models))
	copy(sorted, models)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].cost != sorted[j].cost {
			return sorted[i].cost > sorted[j].cost
		}
		return (sorted[i].inputTokens + sorted[i].outputTokens) > (sorted[j].inputTokens + sorted[j].outputTokens)
	})

	for _, m := range sorted {
		costStr := dimStyle.Render("—")
		if m.cost > 0 {
			costStr = lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(formatUSD(m.cost))
		}
		inputStr := dimStyle.Render("—")
		if m.inputTokens > 0 {
			inputStr = lipgloss.NewStyle().Foreground(colorSapphire).Render(formatTokens(m.inputTokens))
		}
		outputStr := dimStyle.Render("—")
		if m.outputTokens > 0 {
			outputStr = lipgloss.NewStyle().Foreground(colorPeach).Render(formatTokens(m.outputTokens))
		}
		sb.WriteString(fmt.Sprintf(" %s %s %*s %*s %*s\n",
			lipgloss.NewStyle().Foreground(m.color).Width(nameW).Render(truncStr(m.name, nameW)),
			lipgloss.NewStyle().Foreground(m.color).Width(provW).Render(truncStr(m.provider, provW)),
			colW, inputStr, colW, outputStr, colW, costStr))
	}
}

func buildBurnRateContent(data costData, panelW int) string {
	var sb strings.Builder
	daily := data.burnRate * 24
	weekly := daily * 7
	monthly := daily * 30

	lw := 18
	sb.WriteString(fmt.Sprintf(" %-*s %s\n", lw, dimStyle.Render("Current:"),
		lipgloss.NewStyle().Foreground(colorPeach).Bold(true).Render(fmt.Sprintf("$%.2f/hour", data.burnRate))))
	sb.WriteString(fmt.Sprintf(" %-*s %s\n", lw, dimStyle.Render("Daily:"),
		lipgloss.NewStyle().Foreground(colorYellow).Render(formatUSD(daily))))
	sb.WriteString(fmt.Sprintf(" %-*s %s\n", lw, dimStyle.Render("Weekly:"),
		lipgloss.NewStyle().Foreground(colorYellow).Render(formatUSD(weekly))))
	sb.WriteString(fmt.Sprintf(" %-*s %s\n", lw, dimStyle.Render("Monthly:"),
		lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(formatUSD(monthly))))

	// Sparkline
	var sparkData []float64
	for i := 1; i <= 30; i++ {
		sparkData = append(sparkData, data.burnRate*float64(i)*24)
	}
	sparkW := panelW - 4
	if sparkW < 20 {
		sparkW = 20
	}
	sb.WriteString("\n")
	sb.WriteString(RenderAreaSparkline(sparkData, sparkW, colorPeach, "30-day projected"))

	// Budget exhaustion
	for _, b := range data.budgets {
		if b.burnRate > 0 {
			remaining := b.limit - b.used
			if remaining > 0 {
				daysLeft := remaining / b.burnRate / 24
				sb.WriteString(fmt.Sprintf("\n %s: ",
					lipgloss.NewStyle().Foreground(b.color).Bold(true).Render(truncStr(b.name, 16))))
				if daysLeft < 3 {
					sb.WriteString(lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(
						fmt.Sprintf("⚠ %.0fh left!", remaining/b.burnRate)))
				} else if daysLeft < 14 {
					sb.WriteString(lipgloss.NewStyle().Foreground(colorYellow).Render(
						fmt.Sprintf("⚠ ~%.0fd left", daysLeft)))
				} else {
					sb.WriteString(lipgloss.NewStyle().Foreground(colorGreen).Render(
						fmt.Sprintf("✓ ~%.0fd left", daysLeft)))
				}
			}
		}
	}
	return sb.String()
}

// ─── Time-Series Panels ─────────────────────────────────────────────────────

func renderTimeSeriesPanels(sb *strings.Builder, groups []timeSeriesGroup, w int) {
	chartW := w/2 - 8
	if chartW < 30 {
		chartW = 30
	}
	chartH := 10

	// Collect all time-series panels
	var panels []Panel

	// Activity over time (messages/sessions)
	var activityLines []TimeSeriesLine
	for _, g := range groups {
		if pts, ok := g.series["messages"]; ok && len(pts) > 1 {
			activityLines = append(activityLines, TimeSeriesLine{
				Label: g.providerName + " msgs", Color: g.color, Points: toTSPoints(pts),
			})
		} else if pts, ok := g.series["sessions"]; ok && len(pts) > 1 {
			activityLines = append(activityLines, TimeSeriesLine{
				Label: g.providerName + " sess", Color: g.color, Points: toTSPoints(pts),
			})
		}
		if pts, ok := g.series["total_lines"]; ok && len(pts) > 1 {
			activityLines = append(activityLines, TimeSeriesLine{
				Label: g.providerName + " lines", Color: shiftColor(g.color, 30),
				Points: toTSPoints(pts),
			})
		}
	}
	if len(activityLines) > 0 {
		panels = append(panels, Panel{
			Title: "Activity Over Time", Icon: "📈",
			Content: RenderTimeSeriesChart(activityLines, chartW, chartH),
			Span:    1, Color: colorGreen,
		})
	}

	// Token usage over time
	var tokenLines []TimeSeriesLine
	for _, g := range groups {
		if pts, ok := g.series["tokens_total"]; ok && len(pts) > 1 {
			tokenLines = append(tokenLines, TimeSeriesLine{
				Label: g.providerName + " total", Color: g.color,
				Points: toTSPoints(pts),
			})
		}
		type modelSeries struct {
			name   string
			pts    []core.TimePoint
			volume float64
		}
		var modelData []modelSeries
		// Sort series keys for deterministic iteration
		seriesKeys := make([]string, 0, len(g.series))
		for k := range g.series {
			seriesKeys = append(seriesKeys, k)
		}
		sort.Strings(seriesKeys)
		for _, key := range seriesKeys {
			pts := g.series[key]
			if strings.HasPrefix(key, "tokens_") && key != "tokens_total" && len(pts) > 1 {
				total := 0.0
				for _, p := range pts {
					total += p.Value
				}
				modelData = append(modelData, modelSeries{
					name: strings.TrimPrefix(key, "tokens_"), pts: pts, volume: total,
				})
			}
		}
		sort.Slice(modelData, func(i, j int) bool { return modelData[i].volume > modelData[j].volume })
		limit := 3
		if len(modelData) < limit {
			limit = len(modelData)
		}
		for i := 0; i < limit; i++ {
			md := modelData[i]
			tokenLines = append(tokenLines, TimeSeriesLine{
				Label:  prettifyModelName(md.name),
				Color:  stableModelColor(md.name, g.providerID),
				Points: toTSPoints(md.pts),
			})
		}
	}
	if len(tokenLines) > 0 {
		panels = append(panels, Panel{
			Title: "Token Usage Over Time", Icon: "🔢",
			Content: RenderTimeSeriesChart(tokenLines, chartW, chartH),
			Span:    1, Color: colorSapphire,
		})
	}

	// Tool calls over time
	var toolLines []TimeSeriesLine
	for _, g := range groups {
		if pts, ok := g.series["tool_calls"]; ok && len(pts) > 1 {
			toolLines = append(toolLines, TimeSeriesLine{
				Label: g.providerName + " tools", Color: g.color,
				Points: toTSPoints(pts),
			})
		}
	}
	if len(toolLines) > 0 {
		panels = append(panels, Panel{
			Title: "Tool Calls Over Time", Icon: "🔧",
			Content: RenderTimeSeriesChart(toolLines, chartW, chartH),
			Span:    1, Color: colorPeach,
		})
	}

	// Cursor completions over time
	var cursorLines []TimeSeriesLine
	for _, g := range groups {
		if g.providerID != "cursor" {
			continue
		}
		for _, pair := range []struct{ key, label string }{
			{"tab_accepted", "tab accepted"},
			{"composer_accepted", "composer accepted"},
			{"tab_suggested", "tab suggested"},
		} {
			if pts, ok := g.series[pair.key]; ok && len(pts) > 1 {
				cursorLines = append(cursorLines, TimeSeriesLine{
					Label: pair.label, Color: stableModelColor(pair.key, "cursor"),
					Points: toTSPoints(pts),
				})
			}
		}
	}
	if len(cursorLines) > 0 {
		panels = append(panels, Panel{
			Title: "Cursor Completions", Icon: "🖊️",
			Content: RenderTimeSeriesChart(cursorLines, chartW, chartH),
			Span:    1, Color: colorLavender,
		})
	}

	// Lay out panels in rows of 2
	for i := 0; i < len(panels); i += 2 {
		row := PanelRow{}
		row.Panels = append(row.Panels, panels[i])
		if i+1 < len(panels) {
			row.Panels = append(row.Panels, panels[i+1])
		} else {
			// Single panel → full width
			row.Panels[0].Span = 2
		}
		sb.WriteString(renderPanelGrid([]PanelRow{row}, w))
		sb.WriteString("\n")
	}
}

// ─── Conversion helpers ─────────────────────────────────────────────────────

func toTSPoints(pts []core.TimePoint) []TimeSeriesPoint {
	out := make([]TimeSeriesPoint, len(pts))
	for i, p := range pts {
		out[i] = TimeSeriesPoint{Date: p.Date, Value: p.Value}
	}
	return out
}

func shiftColor(base lipgloss.Color, offset int) lipgloss.Color {
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

// ─── Utility ────────────────────────────────────────────────────────────────

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
