package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func buildProviderVendorCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	return buildProviderVendorCompositionLinesWithHide(snap, innerW, expanded, false)
}

// buildProviderVendorCompositionLinesWithHide is the hide-costs-aware variant.
// When hideCosts is true and burn mode resolves to "cost", we fall back to
// tokens/requests so the section never renders dollar segments.
func buildProviderVendorCompositionLinesWithHide(snap core.UsageSnapshot, innerW int, expanded bool, hideCosts bool) ([]string, map[string]bool) {
	allProviders, usedKeys := collectProviderVendorMix(snap)
	if len(allProviders) == 0 {
		return nil, nil
	}
	providers, hiddenCount := limitProviderMix(allProviders, expanded, 4)
	providerColors := buildProviderColorMap(allProviders, snap.AccountID)

	totalCost := float64(0)
	totalTokens := float64(0)
	totalRequests := float64(0)
	for _, provider := range allProviders {
		totalCost += provider.cost
		totalTokens += provider.input + provider.output
		totalRequests += provider.requests
	}

	mode, total := selectBurnMode(totalTokens, totalCost, totalRequests)
	if hideCosts && mode == "cost" {
		switch {
		case totalTokens > 0:
			mode, total = "tokens", totalTokens
		case totalRequests > 0:
			mode, total = "requests", totalRequests
		default:
			return nil, nil
		}
	}
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
	if hideCosts {
		// Rebrand away from "Burn" which connotes spend. Section is now token
		// or request flow only.
		switch mode {
		case "requests":
			heading = "Provider Activity (requests)"
		default:
			heading = "Provider Usage (tokens)"
		}
	}

	providerClients := make([]clientMixEntry, 0, len(allProviders))
	for _, provider := range allProviders {
		value := provider.requests
		if mode == "cost" {
			value = provider.cost
		} else if mode == "tokens" {
			value = provider.input + provider.output
		}
		if value > 0 {
			providerClients = append(providerClients, clientMixEntry{name: provider.name, total: value})
		}
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
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s req", pct, shortCompact(provider.input+provider.output), shortCompact(provider.requests))
			if provider.cost > 0 && !hideCosts {
				valueStr += fmt.Sprintf(" · %s", formatUSD(provider.cost))
			}
		} else if mode == "cost" {
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s req · %s", pct, shortCompact(provider.input+provider.output), shortCompact(provider.requests), formatUSD(provider.cost))
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
	return buildUpstreamProviderCompositionLinesWithHide(snap, innerW, expanded, false)
}

// buildUpstreamProviderCompositionLinesWithHide is the hide-costs-aware variant.
func buildUpstreamProviderCompositionLinesWithHide(snap core.UsageSnapshot, innerW int, expanded bool, hideCosts bool) ([]string, map[string]bool) {
	allProviders, usedKeys := collectUpstreamProviderMix(snap)
	if len(allProviders) == 0 {
		return nil, nil
	}
	providers, hiddenCount := limitProviderMix(allProviders, expanded, 4)
	providerColors := buildProviderColorMap(allProviders, snap.AccountID)

	totalCost := float64(0)
	totalTokens := float64(0)
	totalRequests := float64(0)
	for _, provider := range allProviders {
		totalCost += provider.cost
		totalTokens += provider.input + provider.output
		totalRequests += provider.requests
	}

	mode, total := selectBurnMode(totalTokens, totalCost, totalRequests)
	if hideCosts && mode == "cost" {
		switch {
		case totalTokens > 0:
			mode, total = "tokens", totalTokens
		case totalRequests > 0:
			mode, total = "requests", totalRequests
		default:
			return nil, nil
		}
	}
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
	for _, provider := range allProviders {
		value := provider.requests
		if mode == "cost" {
			value = provider.cost
		} else if mode == "tokens" {
			value = provider.input + provider.output
		}
		if value > 0 {
			providerClients = append(providerClients, clientMixEntry{name: provider.name, total: value})
		}
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
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s req", pct, shortCompact(provider.input+provider.output), shortCompact(provider.requests))
			if provider.cost > 0 && !hideCosts {
				valueStr += fmt.Sprintf(" · %s", formatUSD(provider.cost))
			}
		} else if mode == "cost" {
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s req · %s", pct, shortCompact(provider.input+provider.output), shortCompact(provider.requests), formatUSD(provider.cost))
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
	return buildProviderDailyTrendLinesWithHide(snap, innerW, false)
}

func buildProviderDailyTrendLinesWithHide(snap core.UsageSnapshot, innerW int, hideCosts bool) []string {
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
	if hideCosts {
		// Strip the Cost row outright — the rendered "last" label is a $ value
		// and the sparkline's only context is monetary.
		defs = defs[1:]
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
	for _, point := range points {
		values = append(values, point.Value)
	}
	return values
}
