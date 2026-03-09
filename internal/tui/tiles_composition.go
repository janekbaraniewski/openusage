package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
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
	entries, usedKeys := core.ExtractModelBreakdown(snap)
	models := make([]modelMixEntry, 0, len(entries))
	for _, entry := range entries {
		models = append(models, modelMixEntry{
			name:       entry.Name,
			cost:       entry.Cost,
			input:      entry.Input,
			output:     entry.Output,
			requests:   entry.Requests,
			requests1d: entry.Requests1d,
			series:     entry.Series,
		})
	}
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
	entries, usedKeys := core.ExtractProviderBreakdown(snap)
	providers := make([]providerMixEntry, 0, len(entries))
	for _, entry := range entries {
		providers = append(providers, providerMixEntry{
			name:     entry.Name,
			cost:     entry.Cost,
			input:    entry.Input,
			output:   entry.Output,
			requests: entry.Requests,
		})
	}
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
	entries, usedKeys := core.ExtractUpstreamProviderBreakdown(snap)
	result := make([]providerMixEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, providerMixEntry{
			name:     entry.Name,
			cost:     entry.Cost,
			input:    entry.Input,
			output:   entry.Output,
			requests: entry.Requests,
		})
	}
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

// collectInterfaceAsClients builds clientMixEntry items from interface_ metrics
// so the interface breakdown (composer, cli, human, tab) can be shown directly
// in the client composition section instead of a separate panel.
func collectInterfaceAsClients(snap core.UsageSnapshot) ([]clientMixEntry, map[string]bool) {
	entries, usedKeys := core.ExtractInterfaceClientBreakdown(snap)
	clients := make([]clientMixEntry, 0, len(entries))
	for _, entry := range entries {
		clients = append(clients, clientMixEntry{
			name:       entry.Name,
			requests:   entry.Requests,
			seriesKind: entry.SeriesKind,
			series:     entry.Series,
		})
	}
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
	projectUsage, usedKeys := core.ExtractProjectUsage(snap)
	if len(projectUsage) == 0 {
		return nil, usedKeys
	}
	projects := make([]projectMixEntry, 0, len(projectUsage))
	for _, project := range projectUsage {
		projects = append(projects, projectMixEntry{
			name:       project.Name,
			requests:   project.Requests,
			requests1d: project.Requests1d,
			series:     project.Series,
		})
	}
	return projects, usedKeys
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
	entries, usedKeys := core.ExtractClientBreakdown(snap)
	clients := make([]clientMixEntry, 0, len(entries))
	for _, entry := range entries {
		clients = append(clients, clientMixEntry{
			name:       entry.Name,
			total:      entry.Total,
			input:      entry.Input,
			output:     entry.Output,
			cached:     entry.Cached,
			reasoning:  entry.Reasoning,
			requests:   entry.Requests,
			sessions:   entry.Sessions,
			seriesKind: entry.SeriesKind,
			series:     entry.Series,
		})
	}
	return clients, usedKeys
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
	entries, usedKeys := core.ExtractInterfaceClientBreakdown(snap)
	tools := make([]toolMixEntry, 0, len(entries))
	for _, entry := range entries {
		tools = append(tools, toolMixEntry{
			name:  entry.Name,
			count: entry.Requests,
		})
	}
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
		return nil, usedKeys
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
	languageUsage, usedKeys := core.ExtractLanguageUsage(snap)
	if len(languageUsage) == 0 {
		return nil, usedKeys
	}
	langs := make([]toolMixEntry, 0, len(languageUsage))
	for _, language := range languageUsage {
		langs = append(langs, toolMixEntry{name: language.Name, count: language.Requests})
	}
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
		return nil, usedKeys
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

func buildActualToolUsageLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	rawTools, usedKeys := core.ExtractActualToolUsage(snap)
	if len(rawTools) == 0 {
		return nil, usedKeys
	}

	allTools := make([]toolMixEntry, 0, len(rawTools))
	var totalCalls float64
	for _, rawTool := range rawTools {
		allTools = append(allTools, toolMixEntry{name: rawTool.RawName, count: rawTool.Calls})
		totalCalls += rawTool.Calls
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

func buildMCPUsageLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	type funcEntry struct {
		name  string
		calls float64
	}
	type serverEntry struct {
		name  string
		calls float64
		funcs []funcEntry
	}

	rawServers, usedKeys := core.ExtractMCPUsage(snap)
	servers := make([]serverEntry, 0, len(rawServers))
	var totalCalls float64
	for _, rawServer := range rawServers {
		server := serverEntry{
			name:  prettifyMCPServerName(rawServer.RawName),
			calls: rawServer.Calls,
		}
		for _, rawFunc := range rawServer.Functions {
			server.funcs = append(server.funcs, funcEntry{
				name:  prettifyMCPFunctionName(rawFunc.RawName),
				calls: rawFunc.Calls,
			})
		}
		servers = append(servers, server)
		totalCalls += server.calls
	}

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
