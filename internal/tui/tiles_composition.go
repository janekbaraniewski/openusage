package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

type modelMixEntry struct {
	name       string
	cost       float64
	input      float64
	output     float64
	requests   float64
	requests1d float64
	series     []core.TimePoint
}

type providerMixEntry struct {
	name     string
	cost     float64
	input    float64
	output   float64
	requests float64
}

type clientMixEntry struct {
	name       string
	total      float64
	input      float64
	output     float64
	cached     float64
	reasoning  float64
	requests   float64
	sessions   float64
	seriesKind string
	series     []core.TimePoint
}

type projectMixEntry struct {
	name       string
	requests   float64
	requests1d float64
	series     []core.TimePoint
}

type sourceMixEntry struct {
	name       string
	requests   float64
	requests1d float64
	series     []core.TimePoint
}

type toolMixEntry struct {
	name  string
	count float64
}

func buildProviderModelCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allModels, usedKeys := collectProviderModelMix(snap)
	if len(allModels) == 0 {
		return nil, nil
	}
	models, hiddenCount := limitModelMix(allModels, expanded, 5)
	modelColors := buildModelColorMap(allModels, snap.AccountID)

	totalCost := float64(0)
	totalTokens := float64(0)
	totalRequests := float64(0)
	for _, m := range allModels {
		totalCost += m.cost
		totalTokens += m.input + m.output
		totalRequests += m.requests
	}

	mode, total := selectBurnMode(totalTokens, totalCost, totalRequests)
	if total <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	headingName := "Model Burn"
	var headerSuffix string
	switch mode {
	case "requests":
		headingName = "Model Activity"
		headerSuffix = shortCompact(total) + " req"
	case "cost":
		headerSuffix = fmt.Sprintf("$%.2f", total)
	default:
		headerSuffix = shortCompact(total) + " tok"
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(headingName) +
			"  " + dimStyle.Render(headerSuffix),
		"  " + renderModelMixBar(allModels, total, barW, mode, modelColors),
	}

	for idx, model := range models {
		value := modelMixValue(model, mode)
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		label := prettifyModelName(model.name)
		colorDot := lipgloss.NewStyle().Foreground(colorForModel(modelColors, model.name)).Render("■")
		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)
		valueStr := fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(model.requests))
		switch mode {
		case "tokens":
			valueStr = fmt.Sprintf("%2.0f%% %s tok",
				pct,
				shortCompact(model.input+model.output),
			)
			if model.cost > 0 {
				valueStr += fmt.Sprintf(" · %s", formatUSD(model.cost))
			}
		case "cost":
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s",
				pct,
				shortCompact(model.input+model.output),
				formatUSD(model.cost),
			)
		case "requests":
			if model.requests1d > 0 {
				valueStr += fmt.Sprintf(" · today %s", shortCompact(model.requests1d))
			}
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	trendEntries := limitModelTrendEntries(models, expanded)
	if len(trendEntries) > 0 {
		lines = append(lines, dimStyle.Render("  Trend (daily by model)"))

		labelW := 12
		if innerW < 55 {
			labelW = 10
		}
		sparkW := innerW - labelW - 5
		if sparkW < 10 {
			sparkW = 10
		}
		if sparkW > 28 {
			sparkW = 28
		}

		for _, model := range trendEntries {
			values := make([]float64, 0, len(model.series))
			for _, point := range model.series {
				values = append(values, point.Value)
			}
			if len(values) < 2 {
				continue
			}
			label := truncateToWidth(prettifyModelName(model.name), labelW)
			spark := RenderSparkline(values, sparkW, colorForModel(modelColors, model.name))
			lines = append(lines, fmt.Sprintf("  %s %s",
				lipgloss.NewStyle().Foreground(colorSubtext).Width(labelW).Render(label),
				spark,
			))
		}
	}

	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more models (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func limitModelMix(models []modelMixEntry, expanded bool, maxVisible int) ([]modelMixEntry, int) {
	if expanded || maxVisible <= 0 || len(models) <= maxVisible {
		return models, 0
	}
	return models[:maxVisible], len(models) - maxVisible
}

func limitModelTrendEntries(models []modelMixEntry, expanded bool) []modelMixEntry {
	maxVisible := 2
	if expanded {
		maxVisible = 4
	}

	trend := make([]modelMixEntry, 0, maxVisible)
	for _, model := range models {
		if len(model.series) < 2 {
			continue
		}
		trend = append(trend, model)
		if len(trend) >= maxVisible {
			break
		}
	}
	return trend
}

func buildModelColorMap(models []modelMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(models))
	if len(models) == 0 {
		return colors
	}

	base := stablePaletteOffset("model", providerID)
	for i, model := range models {
		colors[model.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForModel(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("model:"+name, "model")
}

func modelMixValue(model modelMixEntry, mode string) float64 {
	switch mode {
	case "tokens":
		return model.input + model.output
	case "cost":
		return model.cost
	default:
		return model.requests
	}
}

func selectBurnMode(totalTokens, totalCost, totalRequests float64) (mode string, total float64) {
	switch {
	case totalCost > 0:
		return "cost", totalCost
	case totalTokens > 0:
		return "tokens", totalTokens
	default:
		return "requests", totalRequests
	}
}

func collectProviderModelMix(snap core.UsageSnapshot) ([]modelMixEntry, map[string]bool) {
	type agg struct {
		cost       float64
		input      float64
		output     float64
		requests   float64
		requests1d float64
		series     []core.TimePoint
	}
	byModel := make(map[string]*agg)
	usedKeys := make(map[string]bool)

	ensure := func(name string) *agg {
		if _, ok := byModel[name]; !ok {
			byModel[name] = &agg{}
		}
		return byModel[name]
	}

	recordCost := func(name string, v float64, key string) {
		ensure(name).cost += v
		usedKeys[key] = true
	}
	recordInput := func(name string, v float64, key string) {
		ensure(name).input += v
		usedKeys[key] = true
	}
	recordOutput := func(name string, v float64, key string) {
		ensure(name).output += v
		usedKeys[key] = true
	}
	recordRequests := func(name string, v float64, key string) {
		ensure(name).requests += v
		usedKeys[key] = true
	}
	recordRequests1d := func(name string, v float64, key string) {
		ensure(name).requests1d += v
		usedKeys[key] = true
	}

	for key, met := range snap.Metrics {
		if met.Used == nil {
			continue
		}
		switch {
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost_usd"):
			recordCost(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost_usd"), *met.Used, key)
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost"):
			recordCost(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost"), *met.Used, key)
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_input_tokens"):
			recordInput(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_input_tokens"), *met.Used, key)
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_output_tokens"):
			recordOutput(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_output_tokens"), *met.Used, key)
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_requests_today"):
			recordRequests1d(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_requests_today"), *met.Used, key)
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_requests"):
			recordRequests(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_requests"), *met.Used, key)
		case strings.HasPrefix(key, "input_tokens_"):
			recordInput(strings.TrimPrefix(key, "input_tokens_"), *met.Used, key)
		case strings.HasPrefix(key, "output_tokens_"):
			recordOutput(strings.TrimPrefix(key, "output_tokens_"), *met.Used, key)
		}
	}

	for key, points := range snap.DailySeries {
		const prefix = "usage_model_"
		if !strings.HasPrefix(key, prefix) || len(points) == 0 {
			continue
		}
		name := strings.TrimPrefix(key, prefix)
		if name == "" {
			continue
		}
		m := ensure(name)
		m.series = points
		if m.requests <= 0 {
			m.requests = sumSeriesValues(points)
		}
	}

	models := make([]modelMixEntry, 0, len(byModel))
	for name, v := range byModel {
		if v.cost <= 0 && v.input <= 0 && v.output <= 0 && v.requests <= 0 && len(v.series) == 0 {
			continue
		}
		models = append(models, modelMixEntry{
			name:       name,
			cost:       v.cost,
			input:      v.input,
			output:     v.output,
			requests:   v.requests,
			requests1d: v.requests1d,
			series:     v.series,
		})
	}

	sort.Slice(models, func(i, j int) bool {
		ti := models[i].input + models[i].output
		tj := models[j].input + models[j].output
		if ti != tj {
			return ti > tj
		}
		if models[i].cost != models[j].cost {
			return models[i].cost > models[j].cost
		}
		if models[i].requests != models[j].requests {
			return models[i].requests > models[j].requests
		}
		return models[i].name < models[j].name
	})
	return models, usedKeys
}

func buildProviderVendorCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allProviders, usedKeys := collectProviderVendorMix(snap)
	if len(allProviders) == 0 {
		return nil, nil
	}
	providers, hiddenCount := limitProviderMix(allProviders, expanded, 4)
	providerColors := buildProviderColorMap(allProviders, snap.AccountID)

	totalCost := float64(0)
	totalTokens := float64(0)
	totalRequests := float64(0)
	for _, p := range allProviders {
		totalCost += p.cost
		totalTokens += p.input + p.output
		totalRequests += p.requests
	}

	mode, total := selectBurnMode(totalTokens, totalCost, totalRequests)
	if total <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	heading := "Provider Burn (tokens)"
	if mode == "cost" {
		heading = "Provider Burn (credits)"
	} else if mode == "requests" {
		heading = "Provider Activity (requests)"
	}

	providerClients := make([]clientMixEntry, 0, len(allProviders))
	for _, p := range allProviders {
		value := p.requests
		if mode == "cost" {
			value = p.cost
		} else if mode == "tokens" {
			value = p.input + p.output
		}
		if value <= 0 {
			continue
		}
		providerClients = append(providerClients, clientMixEntry{name: p.name, total: value})
	}
	if len(providerClients) == 0 {
		return nil, nil
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(heading),
		"  " + renderClientMixBar(providerClients, total, barW, providerColors, "tokens"),
	}

	for idx, provider := range providers {
		value := provider.requests
		if mode == "cost" {
			value = provider.cost
		} else if mode == "tokens" {
			value = provider.input + provider.output
		}
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		label := prettifyModelName(provider.name)
		colorDot := lipgloss.NewStyle().Foreground(providerColors[provider.name]).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)

		valueStr := fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(provider.requests))
		if mode == "tokens" {
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s req",
				pct,
				shortCompact(provider.input+provider.output),
				shortCompact(provider.requests),
			)
			if provider.cost > 0 {
				valueStr += fmt.Sprintf(" · %s", formatUSD(provider.cost))
			}
		} else if mode == "cost" {
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s req · %s",
				pct,
				shortCompact(provider.input+provider.output),
				shortCompact(provider.requests),
				formatUSD(provider.cost),
			)
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}
	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more providers (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func collectProviderVendorMix(snap core.UsageSnapshot) ([]providerMixEntry, map[string]bool) {
	type agg struct {
		cost     float64
		input    float64
		output   float64
		requests float64
	}
	type providerFieldState struct {
		cost     bool
		input    bool
		output   bool
		requests bool
	}
	byProvider := make(map[string]*agg)
	usedKeys := make(map[string]bool)
	fieldState := make(map[string]*providerFieldState)

	ensure := func(name string) *agg {
		if _, ok := byProvider[name]; !ok {
			byProvider[name] = &agg{}
		}
		return byProvider[name]
	}
	ensureFieldState := func(name string) *providerFieldState {
		if _, ok := fieldState[name]; !ok {
			fieldState[name] = &providerFieldState{}
		}
		return fieldState[name]
	}

	recordCost := func(name string, v float64, key string) {
		ensure(name).cost += v
		ensureFieldState(name).cost = true
		usedKeys[key] = true
	}
	recordInput := func(name string, v float64, key string) {
		ensure(name).input += v
		ensureFieldState(name).input = true
		usedKeys[key] = true
	}
	recordOutput := func(name string, v float64, key string) {
		ensure(name).output += v
		ensureFieldState(name).output = true
		usedKeys[key] = true
	}
	recordRequests := func(name string, v float64, key string) {
		ensure(name).requests += v
		ensureFieldState(name).requests = true
		usedKeys[key] = true
	}

	// Pass 1: primary metrics (including non-BYOK cost) so BYOK fallback logic is order-independent.
	for key, met := range snap.Metrics {
		if met.Used == nil || !strings.HasPrefix(key, "provider_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_cost_usd"):
			recordCost(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_cost_usd"), *met.Used, key)
		case strings.HasSuffix(key, "_cost") && !strings.HasSuffix(key, "_byok_cost"):
			recordCost(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_cost"), *met.Used, key)
		case strings.HasSuffix(key, "_input_tokens"):
			recordInput(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_input_tokens"), *met.Used, key)
		case strings.HasSuffix(key, "_output_tokens"):
			recordOutput(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_output_tokens"), *met.Used, key)
		case strings.HasSuffix(key, "_requests"):
			recordRequests(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_requests"), *met.Used, key)
		}
	}
	// Pass 2: BYOK cost only when primary provider cost is absent.
	for key, met := range snap.Metrics {
		if met.Used == nil || !strings.HasPrefix(key, "provider_") || !strings.HasSuffix(key, "_byok_cost") {
			continue
		}
		base := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_byok_cost")
		if base == "" || ensureFieldState(base).cost {
			continue
		}
		recordCost(base, *met.Used, key)
	}

	meta := snapshotMetaEntries(snap)
	// Pass 3: raw fallback for primary cost fields (excluding BYOK), tokens, requests.
	for key, raw := range meta {
		if usedKeys[key] || !strings.HasPrefix(key, "provider_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_cost") && !strings.HasSuffix(key, "_byok_cost"):
			if v, ok := parseTileNumeric(raw); ok {
				baseKey := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_cost")
				if baseKey == "" || ensureFieldState(baseKey).cost {
					continue
				}
				recordCost(baseKey, v, key)
			}
		case strings.HasSuffix(key, "_input_tokens"), strings.HasSuffix(key, "_prompt_tokens"):
			if v, ok := parseTileNumeric(raw); ok {
				baseKey := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_input_tokens")
				baseKey = strings.TrimSuffix(baseKey, "_prompt_tokens")
				if baseKey == "" || ensureFieldState(baseKey).input {
					continue
				}
				recordInput(baseKey, v, key)
			}
		case strings.HasSuffix(key, "_output_tokens"), strings.HasSuffix(key, "_completion_tokens"):
			if v, ok := parseTileNumeric(raw); ok {
				baseKey := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_output_tokens")
				baseKey = strings.TrimSuffix(baseKey, "_completion_tokens")
				if baseKey == "" || ensureFieldState(baseKey).output {
					continue
				}
				recordOutput(baseKey, v, key)
			}
		case strings.HasSuffix(key, "_requests"):
			if v, ok := parseTileNumeric(raw); ok {
				baseKey := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_requests")
				if baseKey == "" || ensureFieldState(baseKey).requests {
					continue
				}
				recordRequests(baseKey, v, key)
			}
		}
	}
	// Pass 4: raw fallback for BYOK cost only when no primary cost exists.
	for key, raw := range meta {
		if usedKeys[key] || !strings.HasPrefix(key, "provider_") || !strings.HasSuffix(key, "_byok_cost") {
			continue
		}
		if v, ok := parseTileNumeric(raw); ok {
			baseKey := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_byok_cost")
			if baseKey == "" || ensureFieldState(baseKey).cost {
				continue
			}
			recordCost(baseKey, v, key)
		}
	}

	providers := make([]providerMixEntry, 0, len(byProvider))
	for name, v := range byProvider {
		if v.cost <= 0 && v.input <= 0 && v.output <= 0 && v.requests <= 0 {
			continue
		}
		providers = append(providers, providerMixEntry{
			name:     name,
			cost:     v.cost,
			input:    v.input,
			output:   v.output,
			requests: v.requests,
		})
	}

	sort.Slice(providers, func(i, j int) bool {
		ti := providers[i].input + providers[i].output
		tj := providers[j].input + providers[j].output
		if ti != tj {
			return ti > tj
		}
		if providers[i].cost != providers[j].cost {
			return providers[i].cost > providers[j].cost
		}
		return providers[i].requests > providers[j].requests
	})
	return providers, usedKeys
}

func buildUpstreamProviderCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allProviders, usedKeys := collectUpstreamProviderMix(snap)
	if len(allProviders) == 0 {
		return nil, nil
	}
	providers, hiddenCount := limitProviderMix(allProviders, expanded, 4)
	providerColors := buildProviderColorMap(allProviders, snap.AccountID)

	totalCost := float64(0)
	totalTokens := float64(0)
	totalRequests := float64(0)
	for _, p := range allProviders {
		totalCost += p.cost
		totalTokens += p.input + p.output
		totalRequests += p.requests
	}

	mode, total := selectBurnMode(totalTokens, totalCost, totalRequests)
	if total <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	heading := "Hosting Providers (tokens)"
	if mode == "cost" {
		heading = "Hosting Providers (credits)"
	} else if mode == "requests" {
		heading = "Hosting Providers (requests)"
	}

	providerClients := make([]clientMixEntry, 0, len(allProviders))
	for _, p := range allProviders {
		value := p.requests
		if mode == "cost" {
			value = p.cost
		} else if mode == "tokens" {
			value = p.input + p.output
		}
		if value <= 0 {
			continue
		}
		providerClients = append(providerClients, clientMixEntry{name: p.name, total: value})
	}
	if len(providerClients) == 0 {
		return nil, nil
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(heading),
		"  " + renderClientMixBar(providerClients, total, barW, providerColors, "tokens"),
	}

	for idx, provider := range providers {
		value := provider.requests
		if mode == "cost" {
			value = provider.cost
		} else if mode == "tokens" {
			value = provider.input + provider.output
		}
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		label := prettifyModelName(provider.name)
		colorDot := lipgloss.NewStyle().Foreground(providerColors[provider.name]).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)

		valueStr := fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(provider.requests))
		if mode == "tokens" {
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s req",
				pct,
				shortCompact(provider.input+provider.output),
				shortCompact(provider.requests),
			)
			if provider.cost > 0 {
				valueStr += fmt.Sprintf(" · %s", formatUSD(provider.cost))
			}
		} else if mode == "cost" {
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s req · %s",
				pct,
				shortCompact(provider.input+provider.output),
				shortCompact(provider.requests),
				formatUSD(provider.cost),
			)
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}
	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more providers (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func collectUpstreamProviderMix(snap core.UsageSnapshot) ([]providerMixEntry, map[string]bool) {
	type agg struct {
		cost     float64
		input    float64
		output   float64
		requests float64
	}
	byProvider := make(map[string]*agg)
	usedKeys := make(map[string]bool)

	ensure := func(name string) *agg {
		if _, ok := byProvider[name]; !ok {
			byProvider[name] = &agg{}
		}
		return byProvider[name]
	}

	for key, met := range snap.Metrics {
		if met.Used == nil || !strings.HasPrefix(key, "upstream_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_cost_usd"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "upstream_"), "_cost_usd")
			ensure(name).cost += *met.Used
			usedKeys[key] = true
		case strings.HasSuffix(key, "_input_tokens"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "upstream_"), "_input_tokens")
			ensure(name).input += *met.Used
			usedKeys[key] = true
		case strings.HasSuffix(key, "_output_tokens"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "upstream_"), "_output_tokens")
			ensure(name).output += *met.Used
			usedKeys[key] = true
		case strings.HasSuffix(key, "_requests"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "upstream_"), "_requests")
			ensure(name).requests += *met.Used
			usedKeys[key] = true
		}
	}

	if len(byProvider) == 0 {
		return nil, nil
	}

	result := make([]providerMixEntry, 0, len(byProvider))
	for name, a := range byProvider {
		result = append(result, providerMixEntry{
			name:     name,
			cost:     a.cost,
			input:    a.input,
			output:   a.output,
			requests: a.requests,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		ti := result[i].input + result[i].output
		tj := result[j].input + result[j].output
		if ti != tj {
			return ti > tj
		}
		return result[i].requests > result[j].requests
	})

	return result, usedKeys
}

func limitProviderMix(providers []providerMixEntry, expanded bool, maxVisible int) ([]providerMixEntry, int) {
	if expanded || maxVisible <= 0 || len(providers) <= maxVisible {
		return providers, 0
	}
	return providers[:maxVisible], len(providers) - maxVisible
}

func buildProviderColorMap(providers []providerMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(providers))
	if len(providers) == 0 {
		return colors
	}

	base := stablePaletteOffset("provider", providerID)
	for i, provider := range providers {
		colors[provider.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func buildProviderDailyTrendLines(snap core.UsageSnapshot, innerW int) []string {
	type trendDef struct {
		label string
		keys  []string
		color lipgloss.Color
		unit  string
	}
	defs := []trendDef{
		{label: "Cost", keys: []string{"analytics_cost", "cost"}, color: colorTeal, unit: "USD"},
		{label: "Req", keys: []string{"analytics_requests", "requests"}, color: colorYellow, unit: "requests"},
		{label: "Tokens", keys: []string{"analytics_tokens"}, color: colorSapphire, unit: "tokens"},
	}

	lines := []string{}
	labelW := 8
	if innerW < 55 {
		labelW = 6
	}
	sparkW := innerW - labelW - 14
	if sparkW < 10 {
		sparkW = 10
	}
	if sparkW > 30 {
		sparkW = 30
	}

	for _, def := range defs {
		var points []core.TimePoint
		for _, key := range def.keys {
			if got, ok := snap.DailySeries[key]; ok && len(got) > 1 {
				points = got
				break
			}
		}
		if len(points) < 2 {
			continue
		}
		values := tailSeriesValues(points, 14)
		if len(values) < 2 {
			continue
		}

		last := values[len(values)-1]
		lastLabel := shortCompact(last)
		if def.unit == "USD" {
			lastLabel = formatUSD(last)
		}

		if len(lines) == 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Daily Usage"))
		}

		label := lipgloss.NewStyle().Foreground(colorSubtext).Width(labelW).Render(def.label)
		spark := RenderSparkline(values, sparkW, def.color)
		lines = append(lines, fmt.Sprintf("  %s %s %s", label, spark, dimStyle.Render(lastLabel)))
	}

	if len(lines) == 0 {
		return nil
	}
	return lines
}

func tailSeriesValues(points []core.TimePoint, max int) []float64 {
	if len(points) == 0 {
		return nil
	}
	if max > 0 && len(points) > max {
		points = points[len(points)-max:]
	}
	values := make([]float64, 0, len(points))
	for _, p := range points {
		values = append(values, p.Value)
	}
	return values
}

func parseSourceMetricKey(key string) (name, field string, ok bool) {
	const prefix = "source_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	for _, suffix := range []string{
		"_requests_today",
		"_requests",
	} {
		if strings.HasSuffix(rest, suffix) {
			return strings.TrimSuffix(rest, suffix), strings.TrimPrefix(suffix, "_"), true
		}
	}
	return "", "", false
}

func sourceAsClientBucket(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	if s == "" || s == "unknown" {
		return "other"
	}

	switch s {
	case "composer", "tab", "human", "vscode", "ide", "editor", "cursor":
		return "ide"
	case "cloud", "cloud_agent", "cloud_agents", "web", "web_agent", "background_agent":
		return "cloud_agents"
	case "cli", "terminal", "agent", "agents", "cli_agents":
		return "cli_agents"
	case "desktop", "desktop_app":
		return "desktop_app"
	}

	if strings.Contains(s, "cloud") || strings.Contains(s, "web") {
		return "cloud_agents"
	}
	if strings.Contains(s, "cli") || strings.Contains(s, "terminal") || strings.Contains(s, "agent") {
		return "cli_agents"
	}
	if strings.Contains(s, "compose") || strings.Contains(s, "tab") || strings.Contains(s, "ide") || strings.Contains(s, "editor") {
		return "ide"
	}
	return s
}

func canonicalClientBucket(name string) string {
	bucket := sourceAsClientBucket(name)
	switch bucket {
	case "codex", "openusage":
		return "cli_agents"
	}
	return bucket
}

// collectInterfaceAsClients builds clientMixEntry items from interface_ metrics
// so the interface breakdown (composer, cli, human, tab) can be shown directly
// in the client composition section instead of a separate panel.
func collectInterfaceAsClients(snap core.UsageSnapshot) ([]clientMixEntry, map[string]bool) {
	byName := make(map[string]*clientMixEntry)
	ensure := func(name string) *clientMixEntry {
		if _, ok := byName[name]; !ok {
			byName[name] = &clientMixEntry{name: name}
		}
		return byName[name]
	}
	usedKeys := make(map[string]bool)
	usageSeriesByName := make(map[string]map[string]float64)

	for key, met := range snap.Metrics {
		if met.Used == nil {
			continue
		}
		if !strings.HasPrefix(key, "interface_") {
			continue
		}
		name := canonicalClientBucket(strings.TrimPrefix(key, "interface_"))
		if name == "" {
			continue
		}
		entry := ensure(name)
		entry.requests += *met.Used
		usedKeys[key] = true
	}

	for key, points := range snap.DailySeries {
		if len(points) == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(key, "usage_client_"):
			name := canonicalClientBucket(strings.TrimPrefix(key, "usage_client_"))
			if name == "" {
				continue
			}
			mergeSeriesByDay(usageSeriesByName, name, points)
		case strings.HasPrefix(key, "usage_source_"):
			source := strings.TrimPrefix(key, "usage_source_")
			if source == "" {
				continue
			}
			name := canonicalClientBucket(source)
			mergeSeriesByDay(usageSeriesByName, name, points)
		}
	}

	for name, pointsByDay := range usageSeriesByName {
		entry := ensure(name)
		entry.series = sortedSeriesFromByDay(pointsByDay)
		entry.seriesKind = "requests"
		if entry.requests <= 0 {
			entry.requests = sumSeriesValues(entry.series)
		}
	}

	if len(byName) == 0 {
		return nil, nil
	}

	clients := make([]clientMixEntry, 0, len(byName))
	for _, entry := range byName {
		if entry.requests <= 0 && len(entry.series) == 0 {
			continue
		}
		clients = append(clients, *entry)
	}
	sort.Slice(clients, func(i, j int) bool {
		if clients[i].requests != clients[j].requests {
			return clients[i].requests > clients[j].requests
		}
		return clients[i].name < clients[j].name
	})
	return clients, usedKeys
}

func buildProviderClientCompositionLinesWithWidget(snap core.UsageSnapshot, innerW int, expanded bool, widget core.DashboardWidget) ([]string, map[string]bool) {
	allClients, usedKeys := collectProviderClientMix(snap)

	if widget.ClientCompositionIncludeInterfaces {
		ifaceClients, ifaceKeys := collectInterfaceAsClients(snap)
		if len(ifaceClients) > 0 {
			allClients = ifaceClients
			for k, v := range ifaceKeys {
				usedKeys[k] = v
			}
		}
	}

	if len(allClients) == 0 {
		return nil, nil
	}

	clients, hiddenCount := limitClientMix(allClients, expanded, 4)
	clientColors := buildClientColorMap(allClients, snap.AccountID)

	mode, total := selectClientMixMode(allClients)
	if total <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	headingName := widget.ClientCompositionHeading
	if headingName == "" {
		headingName = "Client Burn"
		if mode == "requests" || mode == "sessions" {
			headingName = "Client Activity"
		}
	}
	var clientHeaderSuffix string
	switch mode {
	case "requests":
		clientHeaderSuffix = shortCompact(total) + " req"
	case "sessions":
		clientHeaderSuffix = shortCompact(total) + " sess"
	default:
		clientHeaderSuffix = shortCompact(total) + " tok"
	}
	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(headingName) +
			"  " + dimStyle.Render(clientHeaderSuffix),
		"  " + renderClientMixBar(allClients, total, barW, clientColors, mode),
	}

	for idx, client := range clients {
		value := clientDisplayValue(client, mode)
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		label := prettifyClientName(client.name)
		clientColor := colorForClient(clientColors, client.name)
		colorDot := lipgloss.NewStyle().Foreground(clientColor).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)

		valueStr := fmt.Sprintf("%2.0f%% %s tok", pct, shortCompact(value))
		switch mode {
		case "requests":
			valueStr = fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(value))
			if client.sessions > 0 {
				valueStr += fmt.Sprintf(" · %s sess", shortCompact(client.sessions))
			}
		case "sessions":
			valueStr = fmt.Sprintf("%2.0f%% %s sess", pct, shortCompact(value))
		default:
			if client.requests > 0 {
				valueStr += fmt.Sprintf(" · %s req", shortCompact(client.requests))
			} else if client.sessions > 0 {
				valueStr += fmt.Sprintf(" · %s sess", shortCompact(client.sessions))
			}
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	trendEntries := limitClientTrendEntries(clients, expanded)
	if len(trendEntries) > 0 {
		lines = append(lines, dimStyle.Render("  Trend (daily by client)"))

		labelW := 12
		if innerW < 55 {
			labelW = 10
		}
		sparkW := innerW - labelW - 5
		if sparkW < 10 {
			sparkW = 10
		}
		if sparkW > 28 {
			sparkW = 28
		}

		for _, client := range trendEntries {
			values := make([]float64, 0, len(client.series))
			for _, point := range client.series {
				values = append(values, point.Value)
			}
			if len(values) < 2 {
				continue
			}
			label := truncateToWidth(prettifyClientName(client.name), labelW)
			spark := RenderSparkline(values, sparkW, colorForClient(clientColors, client.name))
			lines = append(lines, fmt.Sprintf("  %s %s",
				lipgloss.NewStyle().Foreground(colorSubtext).Width(labelW).Render(label),
				spark,
			))
		}
	}

	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more clients (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func buildProviderProjectBreakdownLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allProjects, usedKeys := collectProviderProjectMix(snap)
	if len(allProjects) == 0 {
		return nil, nil
	}

	projects, hiddenCount := limitProjectMix(allProjects, expanded, 6)
	projectColors := buildProjectColorMap(allProjects, snap.AccountID)

	totalRequests := float64(0)
	for _, project := range allProjects {
		totalRequests += project.requests
	}
	if totalRequests <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	barEntries := make([]toolMixEntry, 0, len(allProjects))
	for _, project := range allProjects {
		barEntries = append(barEntries, toolMixEntry{name: project.name, count: project.requests})
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Project Breakdown") +
			"  " + dimStyle.Render(shortCompact(totalRequests)+" req"),
		"  " + renderToolMixBar(barEntries, totalRequests, barW, projectColors),
	}

	for idx, project := range projects {
		if project.requests <= 0 {
			continue
		}
		pct := project.requests / totalRequests * 100
		label := project.name
		projectColor := colorForProject(projectColors, project.name)
		colorDot := lipgloss.NewStyle().Foreground(projectColor).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)
		valueStr := fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(project.requests))
		if project.requests1d > 0 {
			valueStr += fmt.Sprintf(" · today %s", shortCompact(project.requests1d))
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more projects (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func collectProviderProjectMix(snap core.UsageSnapshot) ([]projectMixEntry, map[string]bool) {
	byProject := make(map[string]*projectMixEntry)
	usedKeys := make(map[string]bool)

	ensure := func(name string) *projectMixEntry {
		if _, ok := byProject[name]; !ok {
			byProject[name] = &projectMixEntry{name: name}
		}
		return byProject[name]
	}

	seriesByProject := make(map[string]map[string]float64)

	for key, met := range snap.Metrics {
		if met.Used == nil {
			continue
		}
		name, field, ok := parseProjectMetricKey(key)
		if !ok {
			continue
		}
		project := ensure(name)
		switch field {
		case "requests":
			project.requests = *met.Used
		case "requests_today":
			project.requests1d = *met.Used
		}
		usedKeys[key] = true
	}

	for key, points := range snap.DailySeries {
		if !strings.HasPrefix(key, "usage_project_") {
			continue
		}
		name := strings.TrimPrefix(key, "usage_project_")
		if strings.TrimSpace(name) == "" || len(points) == 0 {
			continue
		}
		mergeSeriesByDay(seriesByProject, name, points)
	}

	for name, pointsByDay := range seriesByProject {
		project := ensure(name)
		project.series = sortedSeriesFromByDay(pointsByDay)
		if project.requests <= 0 {
			project.requests = sumSeriesValues(project.series)
		}
	}

	projects := make([]projectMixEntry, 0, len(byProject))
	for _, project := range byProject {
		if project.requests <= 0 && len(project.series) == 0 {
			continue
		}
		projects = append(projects, *project)
	}

	sort.Slice(projects, func(i, j int) bool {
		if projects[i].requests == projects[j].requests {
			return projects[i].name < projects[j].name
		}
		return projects[i].requests > projects[j].requests
	})

	return projects, usedKeys
}

func parseProjectMetricKey(key string) (name, field string, ok bool) {
	const prefix = "project_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	if strings.HasSuffix(rest, "_requests_today") {
		return strings.TrimSuffix(rest, "_requests_today"), "requests_today", true
	}
	if strings.HasSuffix(rest, "_requests") {
		return strings.TrimSuffix(rest, "_requests"), "requests", true
	}
	return "", "", false
}

func limitProjectMix(projects []projectMixEntry, expanded bool, maxVisible int) ([]projectMixEntry, int) {
	if expanded || maxVisible <= 0 || len(projects) <= maxVisible {
		return projects, 0
	}
	return projects[:maxVisible], len(projects) - maxVisible
}

func buildProjectColorMap(projects []projectMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(projects))
	if len(projects) == 0 {
		return colors
	}

	base := stablePaletteOffset("project", providerID)
	for i, project := range projects {
		colors[project.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForProject(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("project:"+name, "project")
}

func collectProviderClientMix(snap core.UsageSnapshot) ([]clientMixEntry, map[string]bool) {
	byClient := make(map[string]*clientMixEntry)
	usedKeys := make(map[string]bool)

	ensure := func(name string) *clientMixEntry {
		if _, ok := byClient[name]; !ok {
			byClient[name] = &clientMixEntry{name: name}
		}
		return byClient[name]
	}
	tokenSeriesByClient := make(map[string]map[string]float64)
	usageClientSeriesByClient := make(map[string]map[string]float64)
	usageSourceSeriesByClient := make(map[string]map[string]float64)
	hasAllTimeRequests := make(map[string]bool)
	requestsTodayFallback := make(map[string]float64)
	hasAnyClientMetrics := false

	for key, met := range snap.Metrics {
		if met.Used == nil {
			continue
		}
		if strings.HasPrefix(key, "client_") {
			name, field, ok := parseClientMetricKey(key)
			if !ok {
				continue
			}
			name = canonicalClientBucket(name)
			hasAnyClientMetrics = true
			client := ensure(name)
			switch field {
			case "total_tokens":
				client.total = *met.Used
			case "input_tokens":
				client.input = *met.Used
			case "output_tokens":
				client.output = *met.Used
			case "cached_tokens":
				client.cached = *met.Used
			case "reasoning_tokens":
				client.reasoning = *met.Used
			case "requests":
				client.requests = *met.Used
				hasAllTimeRequests[name] = true
			case "sessions":
				client.sessions = *met.Used
			}
			usedKeys[key] = true
			continue
		}
		if strings.HasPrefix(key, "source_") {
			sourceName, field, ok := parseSourceMetricKey(key)
			if !ok {
				continue
			}
			clientName := canonicalClientBucket(sourceName)
			client := ensure(clientName)
			switch field {
			case "requests":
				client.requests += *met.Used
				hasAllTimeRequests[clientName] = true
			case "requests_today":
				requestsTodayFallback[clientName] += *met.Used
			}
			usedKeys[key] = true
		}
	}
	for clientName, value := range requestsTodayFallback {
		if hasAllTimeRequests[clientName] {
			continue
		}
		client := ensure(clientName)
		if client.requests <= 0 {
			client.requests = value
		}
	}
	hasAnyClientSeries := false
	for key := range snap.DailySeries {
		if strings.HasPrefix(key, "tokens_client_") || strings.HasPrefix(key, "usage_client_") {
			hasAnyClientSeries = true
			break
		}
	}

	for key, points := range snap.DailySeries {
		if len(points) == 0 {
			continue
		}

		switch {
		case strings.HasPrefix(key, "tokens_client_"):
			name := canonicalClientBucket(strings.TrimPrefix(key, "tokens_client_"))
			if name == "" {
				continue
			}
			mergeSeriesByDay(tokenSeriesByClient, name, points)
		case strings.HasPrefix(key, "usage_client_"):
			name := canonicalClientBucket(strings.TrimPrefix(key, "usage_client_"))
			if name == "" {
				continue
			}
			mergeSeriesByDay(usageClientSeriesByClient, name, points)
		case strings.HasPrefix(key, "usage_source_"):
			if hasAnyClientMetrics || hasAnyClientSeries {
				continue
			}
			name := canonicalClientBucket(strings.TrimPrefix(key, "usage_source_"))
			if name == "" {
				continue
			}
			mergeSeriesByDay(usageSourceSeriesByClient, name, points)
		default:
			continue
		}
	}

	for name, pointsByDay := range tokenSeriesByClient {
		client := ensure(name)
		client.series = sortedSeriesFromByDay(pointsByDay)
		client.seriesKind = "tokens"
		if client.total <= 0 {
			client.total = sumSeriesValues(client.series)
		}
	}
	for name, pointsByDay := range usageClientSeriesByClient {
		client := ensure(name)
		if client.seriesKind == "tokens" {
			continue
		}
		client.series = sortedSeriesFromByDay(pointsByDay)
		client.seriesKind = "requests"
		if client.requests <= 0 {
			client.requests = sumSeriesValues(client.series)
		}
	}
	for name, pointsByDay := range usageSourceSeriesByClient {
		client := ensure(name)
		if client.seriesKind != "" {
			continue
		}
		client.series = sortedSeriesFromByDay(pointsByDay)
		client.seriesKind = "requests"
		if client.requests <= 0 {
			client.requests = sumSeriesValues(client.series)
		}
	}

	clients := make([]clientMixEntry, 0, len(byClient))
	for _, client := range byClient {
		if clientMixValue(*client) <= 0 && client.sessions <= 0 && client.requests <= 0 && len(client.series) == 0 {
			continue
		}
		clients = append(clients, *client)
	}

	sort.Slice(clients, func(i, j int) bool {
		vi := clientTokenValue(clients[i])
		vj := clientTokenValue(clients[j])
		if vi == vj {
			if clients[i].requests == clients[j].requests {
				if clients[i].sessions == clients[j].sessions {
					return clients[i].name < clients[j].name
				}
				return clients[i].sessions > clients[j].sessions
			}
			return clients[i].requests > clients[j].requests
		}
		return vi > vj
	})

	return clients, usedKeys
}

func parseClientMetricKey(key string) (name, field string, ok bool) {
	const prefix = "client_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	for _, suffix := range []string{
		"_total_tokens", "_input_tokens", "_output_tokens",
		"_cached_tokens", "_reasoning_tokens", "_requests", "_sessions",
	} {
		if strings.HasSuffix(rest, suffix) {
			return strings.TrimSuffix(rest, suffix), strings.TrimPrefix(suffix, "_"), true
		}
	}
	return "", "", false
}

func clientTokenValue(client clientMixEntry) float64 {
	if client.total > 0 {
		return client.total
	}
	if client.input > 0 || client.output > 0 || client.cached > 0 || client.reasoning > 0 {
		return client.input + client.output + client.cached + client.reasoning
	}
	return 0
}

func clientMixValue(client clientMixEntry) float64 {
	if v := clientTokenValue(client); v > 0 {
		return v
	}
	if client.requests > 0 {
		return client.requests
	}
	if len(client.series) > 0 {
		return sumSeriesValues(client.series)
	}
	return 0
}

func clientDisplayValue(client clientMixEntry, mode string) float64 {
	switch mode {
	case "sessions":
		return client.sessions
	case "requests":
		if client.requests > 0 {
			return client.requests
		}
		return sumSeriesValues(client.series)
	default:
		return clientMixValue(client)
	}
}

func selectClientMixMode(clients []clientMixEntry) (mode string, total float64) {
	totalTokens := float64(0)
	totalRequests := float64(0)
	totalSessions := float64(0)
	for _, client := range clients {
		totalTokens += clientTokenValue(client)
		totalRequests += client.requests
		totalSessions += client.sessions
	}
	if totalTokens > 0 {
		return "tokens", totalTokens
	}
	if totalRequests > 0 {
		return "requests", totalRequests
	}
	return "sessions", totalSessions
}

func sumSeriesValues(points []core.TimePoint) float64 {
	total := float64(0)
	for _, p := range points {
		total += p.Value
	}
	return total
}

func mergeSeriesByDay(seriesByClient map[string]map[string]float64, client string, points []core.TimePoint) {
	if client == "" || len(points) == 0 {
		return
	}
	if seriesByClient[client] == nil {
		seriesByClient[client] = make(map[string]float64)
	}
	for _, point := range points {
		if point.Date == "" {
			continue
		}
		seriesByClient[client][point.Date] += point.Value
	}
}

func sortedSeriesFromByDay(pointsByDay map[string]float64) []core.TimePoint {
	if len(pointsByDay) == 0 {
		return nil
	}
	days := lo.Keys(pointsByDay)
	sort.Strings(days)

	points := make([]core.TimePoint, 0, len(days))
	for _, day := range days {
		points = append(points, core.TimePoint{
			Date:  day,
			Value: pointsByDay[day],
		})
	}
	return points
}

func limitClientMix(clients []clientMixEntry, expanded bool, maxVisible int) ([]clientMixEntry, int) {
	if expanded || maxVisible <= 0 || len(clients) <= maxVisible {
		return clients, 0
	}
	return clients[:maxVisible], len(clients) - maxVisible
}

func limitClientTrendEntries(clients []clientMixEntry, expanded bool) []clientMixEntry {
	maxVisible := 2
	if expanded {
		maxVisible = 4
	}

	trend := make([]clientMixEntry, 0, maxVisible)
	for _, client := range clients {
		if len(client.series) < 2 {
			continue
		}
		trend = append(trend, client)
		if len(trend) >= maxVisible {
			break
		}
	}
	return trend
}

func prettifyClientName(name string) string {
	switch name {
	case "cli":
		return "CLI Agents"
	case "ide":
		return "IDE"
	case "exec":
		return "Exec"
	case "desktop_app":
		return "Desktop App"
	case "other":
		return "Other"
	case "composer":
		return "Composer"
	case "human":
		return "Human"
	case "tab":
		return "Tab Completion"
	}

	parts := strings.Split(name, "_")
	for i := range parts {
		switch parts[i] {
		case "cli":
			parts[i] = "CLI"
		case "ide":
			parts[i] = "IDE"
		case "api":
			parts[i] = "API"
		default:
			parts[i] = titleCase(parts[i])
		}
	}
	return strings.Join(parts, " ")
}

func prettifyMCPServerName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "unknown"
	}

	// Strip known prefixes from claude.ai marketplace and plugin system.
	s = strings.TrimPrefix(s, "claude_ai_")
	s = strings.TrimPrefix(s, "plugin_")

	// Strip trailing _mcp suffix (redundant — everything here is MCP).
	s = strings.TrimSuffix(s, "_mcp")

	// Deduplicate: "slack_slack" → "slack".
	parts := strings.Split(s, "_")
	if len(parts) >= 2 && parts[0] == parts[len(parts)-1] {
		parts = parts[:len(parts)-1]
	}
	s = strings.Join(parts, "_")

	if s == "" {
		return raw
	}

	// Title case with separators preserved.
	return prettifyMCPName(s)
}

// prettifyMCPFunctionName cleans up raw MCP function names for display.
func prettifyMCPFunctionName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return raw
	}
	return prettifyMCPName(s)
}

// prettifyMCPName converts snake_case/kebab-case to Title Case.
func prettifyMCPName(s string) string {
	// Replace underscores and hyphens with spaces, then title-case each word.
	s = strings.NewReplacer("_", " ", "-", " ").Replace(s)
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func buildClientColorMap(clients []clientMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(clients))
	if len(clients) == 0 {
		return colors
	}

	base := stablePaletteOffset("client", providerID)
	for i, client := range clients {
		colors[client.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForClient(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("client:"+name, "client")
}

func stablePaletteOffset(prefix, value string) int {
	key := prefix + ":" + value
	hash := 0
	for _, ch := range key {
		hash = hash*31 + int(ch)
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}

func distributedPaletteColor(base, position int) lipgloss.Color {
	if len(modelColorPalette) == 0 {
		return colorSubtext
	}
	idx := distributedPaletteIndex(base, position, len(modelColorPalette))
	return modelColorPalette[idx]
}

func distributedPaletteIndex(base, position, size int) int {
	if size <= 0 {
		return 0
	}
	base %= size
	if base < 0 {
		base += size
	}
	step := distributedPaletteStep(size)
	idx := (base + position*step) % size
	if idx < 0 {
		idx += size
	}
	return idx
}

func distributedPaletteStep(size int) int {
	if size <= 1 {
		return 1
	}
	step := size/2 + 1
	for gcdInt(step, size) != 1 {
		step++
	}
	return step
}

func gcdInt(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func renderClientMixBar(top []clientMixEntry, total float64, barW int, colors map[string]lipgloss.Color, mode string) string {
	if len(top) == 0 || total <= 0 {
		return ""
	}

	type seg struct {
		val   float64
		color lipgloss.Color
	}

	segs := make([]seg, 0, len(top)+1)
	sumTop := float64(0)
	for _, client := range top {
		value := clientDisplayValue(client, mode)
		if value <= 0 {
			continue
		}
		sumTop += value
		segs = append(segs, seg{
			val:   value,
			color: colorForClient(colors, client.name),
		})
	}
	if sumTop < total {
		segs = append(segs, seg{
			val:   total - sumTop,
			color: colorSurface1,
		})
	}
	if len(segs) == 0 {
		return ""
	}

	var sb strings.Builder
	remainingW := barW
	remainingTotal := total
	for i, s := range segs {
		if remainingW <= 0 {
			break
		}
		segW := remainingW
		if i < len(segs)-1 {
			segW = int(math.Round(s.val / remainingTotal * float64(remainingW)))
			if segW < 1 && s.val > 0 {
				segW = 1
			}
			if segW > remainingW {
				segW = remainingW
			}
		}
		if segW <= 0 {
			continue
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(s.color).Render(strings.Repeat("█", segW)))
		remainingW -= segW
		remainingTotal -= s.val
		if remainingTotal <= 0 {
			remainingTotal = 1
		}
	}
	if remainingW > 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", remainingW)))
	}
	return sb.String()
}

func renderModelMixBar(models []modelMixEntry, total float64, barW int, mode string, colors map[string]lipgloss.Color) string {
	if len(models) == 0 || total <= 0 {
		return ""
	}

	type seg struct {
		val   float64
		color lipgloss.Color
	}
	segs := make([]seg, 0, len(models)+1)
	sumTop := float64(0)
	for _, m := range models {
		v := modelMixValue(m, mode)
		if v <= 0 {
			continue
		}
		sumTop += v
		segs = append(segs, seg{
			val:   v,
			color: colorForModel(colors, m.name),
		})
	}
	if sumTop < total {
		segs = append(segs, seg{
			val:   total - sumTop,
			color: colorSurface1,
		})
	}
	if len(segs) == 0 {
		return ""
	}

	var sb strings.Builder
	remainingW := barW
	remainingTotal := total
	for i, s := range segs {
		if remainingW <= 0 {
			break
		}
		segW := remainingW
		if i < len(segs)-1 {
			segW = int(math.Round(s.val / remainingTotal * float64(remainingW)))
			if segW < 1 && s.val > 0 {
				segW = 1
			}
			if segW > remainingW {
				segW = remainingW
			}
		}
		if segW <= 0 {
			continue
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(s.color).Render(strings.Repeat("█", segW)))
		remainingW -= segW
		remainingTotal -= s.val
		if remainingTotal <= 0 {
			remainingTotal = 1
		}
	}
	if remainingW > 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", remainingW)))
	}
	return sb.String()
}

func renderToolMixBar(top []toolMixEntry, total float64, barW int, colors map[string]lipgloss.Color) string {
	if len(top) == 0 || total <= 0 {
		return ""
	}

	type seg struct {
		val   float64
		color lipgloss.Color
	}

	segs := make([]seg, 0, len(top)+1)
	sumTop := float64(0)
	for _, tool := range top {
		if tool.count <= 0 {
			continue
		}
		sumTop += tool.count
		segs = append(segs, seg{
			val:   tool.count,
			color: colorForTool(colors, tool.name),
		})
	}
	if sumTop < total {
		segs = append(segs, seg{
			val:   total - sumTop,
			color: colorSurface1,
		})
	}
	if len(segs) == 0 {
		return ""
	}

	var sb strings.Builder
	remainingW := barW
	remainingTotal := total
	for i, s := range segs {
		if remainingW <= 0 {
			break
		}
		segW := remainingW
		if i < len(segs)-1 {
			segW = int(math.Round(s.val / remainingTotal * float64(remainingW)))
			if segW < 1 && s.val > 0 {
				segW = 1
			}
			if segW > remainingW {
				segW = remainingW
			}
		}
		if segW <= 0 {
			continue
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(s.color).Render(strings.Repeat("█", segW)))
		remainingW -= segW
		remainingTotal -= s.val
		if remainingTotal <= 0 {
			remainingTotal = 1
		}
	}
	if remainingW > 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", remainingW)))
	}
	return sb.String()
}

func buildProviderToolCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool, widget core.DashboardWidget) ([]string, map[string]bool) {
	allTools, usedKeys := collectProviderToolMix(snap)
	if len(allTools) == 0 {
		return nil, nil
	}

	tools, hiddenCount := limitToolMix(allTools, expanded, 4)
	toolColors := buildToolColorMap(allTools, snap.AccountID)

	totalCalls := float64(0)
	for _, tool := range allTools {
		totalCalls += tool.count
	}
	if totalCalls <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	toolHeadingName := "Tool Usage"
	if widget.ToolCompositionHeading != "" {
		toolHeadingName = widget.ToolCompositionHeading
	}
	toolHeaderSuffix := shortCompact(totalCalls) + " calls"

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(toolHeadingName) +
			"  " + dimStyle.Render(toolHeaderSuffix),
		"  " + renderToolMixBar(allTools, totalCalls, barW, toolColors),
	}

	for idx, tool := range tools {
		if tool.count <= 0 {
			continue
		}
		pct := tool.count / totalCalls * 100
		label := tool.name
		toolColor := colorForTool(toolColors, tool.name)
		colorDot := lipgloss.NewStyle().Foreground(toolColor).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)

		valueStr := fmt.Sprintf("%2.0f%% %s calls", pct, shortCompact(tool.count))
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more tools (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func collectProviderToolMix(snap core.UsageSnapshot) ([]toolMixEntry, map[string]bool) {
	byTool := make(map[string]float64)
	usedKeys := make(map[string]bool)

	for key, met := range snap.Metrics {
		if met.Used == nil || strings.HasSuffix(key, "_today") {
			continue
		}
		if !strings.HasPrefix(key, "interface_") {
			continue
		}
		name := strings.TrimPrefix(key, "interface_")
		if name == "" {
			continue
		}
		byTool[name] += *met.Used
		usedKeys[key] = true
	}

	tools := make([]toolMixEntry, 0, len(byTool))
	for name, count := range byTool {
		if count <= 0 {
			continue
		}
		tools = append(tools, toolMixEntry{name: name, count: count})
	}

	sortToolMixEntries(tools)

	return tools, usedKeys
}

func sortToolMixEntries(tools []toolMixEntry) {
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].count == tools[j].count {
			return tools[i].name < tools[j].name
		}
		return tools[i].count > tools[j].count
	})
}

func limitToolMix(tools []toolMixEntry, expanded bool, maxVisible int) ([]toolMixEntry, int) {
	if expanded || maxVisible <= 0 || len(tools) <= maxVisible {
		return tools, 0
	}
	return tools[:maxVisible], len(tools) - maxVisible
}

func buildToolColorMap(tools []toolMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(tools))
	if len(tools) == 0 {
		return colors
	}

	base := stablePaletteOffset("tool", providerID)
	for i, tool := range tools {
		colors[tool.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForTool(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("tool:"+name, "tool")
}

func buildProviderLanguageCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allLangs, usedKeys := collectProviderLanguageMix(snap)
	if len(allLangs) == 0 {
		// Show "no data" placeholder when telemetry is active but no language data.
		if snap.Attributes != nil && snap.Attributes["telemetry_view"] == "canonical" {
			return []string{
				lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Language"),
				dimStyle.Render("  No language data for this time range"),
			}, usedKeys
		}
		return nil, nil
	}

	langs, hiddenCount := limitToolMix(allLangs, expanded, 6)
	langColors := buildLangColorMap(allLangs, snap.AccountID)

	totalReqs := float64(0)
	for _, lang := range allLangs {
		totalReqs += lang.count
	}
	if totalReqs <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	langHeaderSuffix := shortCompact(totalReqs) + " req"
	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Language") +
			"  " + dimStyle.Render(langHeaderSuffix),
		"  " + renderToolMixBar(allLangs, totalReqs, barW, langColors),
	}

	for idx, lang := range langs {
		if lang.count <= 0 {
			continue
		}
		pct := lang.count / totalReqs * 100
		label := lang.name
		langColor := colorForTool(langColors, lang.name)
		colorDot := lipgloss.NewStyle().Foreground(langColor).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)

		valueStr := fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(lang.count))
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more languages (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func collectProviderLanguageMix(snap core.UsageSnapshot) ([]toolMixEntry, map[string]bool) {
	byLang := make(map[string]float64)
	usedKeys := make(map[string]bool)

	for key, met := range snap.Metrics {
		if met.Used == nil || !strings.HasPrefix(key, "lang_") {
			continue
		}
		name := strings.TrimPrefix(key, "lang_")
		if name == "" {
			continue
		}
		byLang[name] = *met.Used
		usedKeys[key] = true
	}

	langs := make([]toolMixEntry, 0, len(byLang))
	for name, count := range byLang {
		if count <= 0 {
			continue
		}
		langs = append(langs, toolMixEntry{name: name, count: count})
	}

	sort.Slice(langs, func(i, j int) bool {
		if langs[i].count == langs[j].count {
			return langs[i].name < langs[j].name
		}
		return langs[i].count > langs[j].count
	})

	return langs, usedKeys
}

func buildLangColorMap(langs []toolMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(langs))
	if len(langs) == 0 {
		return colors
	}
	base := stablePaletteOffset("lang", providerID)
	for i, lang := range langs {
		colors[lang.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func buildProviderCodeStatsLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int) ([]string, map[string]bool) {
	cs := widget.CodeStatsMetrics
	usedKeys := make(map[string]bool)
	getVal := func(key string) float64 {
		if key == "" {
			return 0
		}
		if m, ok := snap.Metrics[key]; ok && m.Used != nil {
			usedKeys[key] = true
			return *m.Used
		}
		return 0
	}

	added := getVal(cs.LinesAdded)
	removed := getVal(cs.LinesRemoved)
	files := getVal(cs.FilesChanged)
	commits := getVal(cs.Commits)
	aiPct := getVal(cs.AIPercent)
	prompts := getVal(cs.Prompts)

	if added <= 0 && removed <= 0 && commits <= 0 && files <= 0 {
		if snap.Attributes != nil && snap.Attributes["telemetry_view"] == "canonical" {
			return []string{
				lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Code Statistics"),
				dimStyle.Render("  No code stats for this time range"),
			}, usedKeys
		}
		return nil, nil
	}

	var codeStatParts []string
	if files > 0 {
		codeStatParts = append(codeStatParts, shortCompact(files)+" files")
	}
	if added > 0 || removed > 0 {
		codeStatParts = append(codeStatParts, shortCompact(added+removed)+" lines")
	}
	codeStatSuffix := strings.Join(codeStatParts, " · ")
	codeStatHeading := lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Code Statistics")
	if codeStatSuffix != "" {
		codeStatHeading += "  " + dimStyle.Render(codeStatSuffix)
	}
	lines := []string{
		codeStatHeading,
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	if added > 0 || removed > 0 {
		total := added + removed
		addedColor := colorGreen
		removedColor := colorRed
		addedW := int(math.Round(added / total * float64(barW)))
		if addedW < 1 && added > 0 {
			addedW = 1
		}
		removedW := barW - addedW
		bar := lipgloss.NewStyle().Foreground(addedColor).Render(strings.Repeat("█", addedW)) +
			lipgloss.NewStyle().Foreground(removedColor).Render(strings.Repeat("█", removedW))
		lines = append(lines, "  "+bar)

		addedDot := lipgloss.NewStyle().Foreground(addedColor).Render("■")
		removedDot := lipgloss.NewStyle().Foreground(removedColor).Render("■")
		addedLabel := fmt.Sprintf("%s +%s added", addedDot, shortCompact(added))
		removedLabel := fmt.Sprintf("%s -%s removed", removedDot, shortCompact(removed))
		lines = append(lines, renderDotLeaderRow(addedLabel, removedLabel, innerW))
	}

	if files > 0 {
		lines = append(lines, renderDotLeaderRow("Files Changed", shortCompact(files)+" files", innerW))
	}

	if commits > 0 {
		commitLabel := shortCompact(commits) + " commits"
		if aiPct > 0 {
			commitLabel += fmt.Sprintf(" · %.0f%% AI", aiPct)
		}
		lines = append(lines, renderDotLeaderRow("Commits", commitLabel, innerW))
	}

	if aiPct > 0 {
		aiBarW := barW
		aiFilledW := int(math.Round(aiPct / 100 * float64(aiBarW)))
		if aiFilledW < 1 && aiPct > 0 {
			aiFilledW = 1
		}
		aiEmptyW := aiBarW - aiFilledW
		if aiEmptyW < 0 {
			aiEmptyW = 0
		}
		aiColor := colorBlue
		aiBar := lipgloss.NewStyle().Foreground(aiColor).Render(strings.Repeat("█", aiFilledW)) +
			lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", aiEmptyW))
		lines = append(lines, "  "+aiBar)
	}

	if prompts > 0 {
		lines = append(lines, renderDotLeaderRow("Prompts", shortCompact(prompts)+" total", innerW))
	}

	return lines, usedKeys
}

// actualToolUsage status/aggregate keys that should not appear as individual tool entries.
var actualToolAggregateKeys = map[string]bool{
	"tool_calls_total":  true,
	"tool_completed":    true,
	"tool_errored":      true,
	"tool_cancelled":    true,
	"tool_success_rate": true,
}

func buildActualToolUsageLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	byTool := make(map[string]float64)
	usedKeys := make(map[string]bool)

	for key, met := range snap.Metrics {
		if met.Used == nil {
			continue
		}
		if !strings.HasPrefix(key, "tool_") {
			continue
		}
		if actualToolAggregateKeys[key] {
			usedKeys[key] = true
			continue
		}
		// Skip time-bucketed variants (e.g. tool_bash_today) — these are
		// supplementary metrics that would appear as duplicate entries.
		if strings.HasSuffix(key, "_today") || strings.HasSuffix(key, "_1d") || strings.HasSuffix(key, "_7d") || strings.HasSuffix(key, "_30d") {
			usedKeys[key] = true
			continue
		}
		name := strings.TrimPrefix(key, "tool_")
		if name == "" {
			continue
		}
		// Skip MCP tools — they have their own dedicated section.
		if isMCPToolMetricName(name) {
			usedKeys[key] = true
			continue
		}
		byTool[name] += *met.Used
		usedKeys[key] = true
	}

	if len(byTool) == 0 {
		if snap.Attributes != nil && snap.Attributes["telemetry_view"] == "canonical" {
			return []string{
				lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Tool Usage"),
				dimStyle.Render("  No tool data for this time range"),
			}, usedKeys
		}
		return nil, nil
	}

	allTools := make([]toolMixEntry, 0, len(byTool))
	var totalCalls float64
	for name, count := range byTool {
		if count <= 0 {
			continue
		}
		allTools = append(allTools, toolMixEntry{name: name, count: count})
		totalCalls += count
	}
	if totalCalls <= 0 {
		return nil, nil
	}

	sortToolMixEntries(allTools)

	displayLimit := 6
	if expanded {
		displayLimit = len(allTools)
	}
	visibleTools := allTools
	hiddenCount := 0
	if len(allTools) > displayLimit {
		visibleTools = allTools[:displayLimit]
		hiddenCount = len(allTools) - displayLimit
	}

	toolColors := buildToolColorMap(allTools, snap.AccountID)

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	// Header with total call count and success rate.
	headerSuffix := shortCompact(totalCalls) + " calls"
	if m, ok := snap.Metrics["tool_success_rate"]; ok && m.Used != nil {
		headerSuffix += fmt.Sprintf(" · %.0f%% ok", *m.Used)
	}
	heading := lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Tool Usage") +
		"  " + dimStyle.Render(headerSuffix)

	lines := []string{
		heading,
		"  " + renderToolMixBar(allTools, totalCalls, barW, toolColors),
	}

	for idx, tool := range visibleTools {
		if tool.count <= 0 {
			continue
		}
		pct := tool.count / totalCalls * 100
		label := tool.name
		toolColor := colorForTool(toolColors, tool.name)
		colorDot := lipgloss.NewStyle().Foreground(toolColor).Render("■")

		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)
		valueStr := fmt.Sprintf("%2.0f%% %s calls", pct, shortCompact(tool.count))
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more tools (Ctrl+O)", hiddenCount)))
	}

	return lines, usedKeys
}

func isMCPToolMetricName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}
	if strings.HasPrefix(normalized, "mcp_") {
		return true
	}
	if strings.Contains(normalized, "_mcp_server_") || strings.Contains(normalized, "-mcp-server-") {
		return true
	}
	return strings.HasSuffix(normalized, "_mcp")
}

func buildMCPUsageLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	usedKeys := make(map[string]bool)

	type funcEntry struct {
		name  string
		calls float64
	}
	type serverEntry struct {
		rawName string
		name    string
		calls   float64
		funcs   []funcEntry
	}

	// First pass: collect server totals.
	serverMap := make(map[string]*serverEntry)
	for key, m := range snap.Metrics {
		if !strings.HasPrefix(key, "mcp_") || m.Used == nil {
			continue
		}
		usedKeys[key] = true

		if key == "mcp_calls_total" || key == "mcp_calls_total_today" || key == "mcp_servers_active" {
			continue
		}
		if strings.HasSuffix(key, "_today") {
			continue
		}

		rest := strings.TrimPrefix(key, "mcp_")
		if !strings.HasSuffix(rest, "_total") {
			continue
		}
		rawServerName := strings.TrimSuffix(rest, "_total")
		if rawServerName == "" {
			continue
		}
		serverMap[rawServerName] = &serverEntry{
			rawName: rawServerName,
			name:    prettifyMCPServerName(rawServerName),
			calls:   *m.Used,
		}
	}

	// Second pass: collect functions for each known server.
	for key, m := range snap.Metrics {
		if !strings.HasPrefix(key, "mcp_") || m.Used == nil {
			continue
		}
		if key == "mcp_calls_total" || key == "mcp_calls_total_today" || key == "mcp_servers_active" {
			continue
		}
		if strings.HasSuffix(key, "_today") || strings.HasSuffix(key, "_total") {
			continue
		}
		rest := strings.TrimPrefix(key, "mcp_")
		for rawServerName, srv := range serverMap {
			prefix := rawServerName + "_"
			if strings.HasPrefix(rest, prefix) {
				funcName := strings.TrimPrefix(rest, prefix)
				if funcName != "" {
					srv.funcs = append(srv.funcs, funcEntry{
						name:  prettifyMCPFunctionName(funcName),
						calls: *m.Used,
					})
				}
				break
			}
		}
	}

	// Sort servers and their functions.
	var servers []*serverEntry
	var totalCalls float64
	for _, srv := range serverMap {
		sort.Slice(srv.funcs, func(i, j int) bool {
			if srv.funcs[i].calls != srv.funcs[j].calls {
				return srv.funcs[i].calls > srv.funcs[j].calls
			}
			return srv.funcs[i].name < srv.funcs[j].name
		})
		servers = append(servers, srv)
		totalCalls += srv.calls
	}
	sort.Slice(servers, func(i, j int) bool {
		if servers[i].calls != servers[j].calls {
			return servers[i].calls > servers[j].calls
		}
		return servers[i].name < servers[j].name
	})

	if len(servers) == 0 || totalCalls <= 0 {
		return nil, usedKeys
	}

	// Header.
	headerSuffix := shortCompact(totalCalls) + " calls · " + fmt.Sprintf("%d servers", len(servers))
	heading := lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("MCP Usage") +
		"  " + dimStyle.Render(headerSuffix)

	// Build entries for the bar using prettified names.
	var allEntries []toolMixEntry
	for _, srv := range servers {
		allEntries = append(allEntries, toolMixEntry{name: srv.name, count: srv.calls})
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	toolColors := buildToolColorMap(allEntries, snap.AccountID)

	lines := []string{
		heading,
		"  " + renderToolMixBar(allEntries, totalCalls, barW, toolColors),
	}

	// Show up to 6 servers with nested function breakdown.
	displayLimit := 6
	if expanded {
		displayLimit = len(servers)
	}
	visible := servers
	if len(visible) > displayLimit {
		visible = visible[:displayLimit]
	}

	for idx, srv := range visible {
		pct := srv.calls / totalCalls * 100
		toolColor := colorForTool(toolColors, srv.name)
		colorDot := lipgloss.NewStyle().Foreground(toolColor).Render("■")
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, srv.name)
		valueStr := fmt.Sprintf("%2.0f%% %s calls", pct, shortCompact(srv.calls))
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))

		// Show top 3 functions per server, indented.
		maxFuncs := 3
		if expanded {
			maxFuncs = len(srv.funcs)
		}
		if len(srv.funcs) < maxFuncs {
			maxFuncs = len(srv.funcs)
		}
		for j := 0; j < maxFuncs; j++ {
			fn := srv.funcs[j]
			fnLabel := "    " + fn.name
			fnValue := fmt.Sprintf("%s calls", shortCompact(fn.calls))
			lines = append(lines, renderDotLeaderRow(fnLabel, fnValue, innerW))
		}
		if !expanded && len(srv.funcs) > 3 {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("    + %d more (Ctrl+O)", len(srv.funcs)-3)))
		}
	}

	if !expanded && len(servers) > displayLimit {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more servers (Ctrl+O)", len(servers)-displayLimit)))
	}

	return lines, usedKeys
}
