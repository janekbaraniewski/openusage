package tui

import (
	"fmt"
	"math"
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
	for _, model := range allModels {
		totalCost += model.cost
		totalTokens += model.input + model.output
		totalRequests += model.requests
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
			valueStr = fmt.Sprintf("%2.0f%% %s tok", pct, shortCompact(model.input+model.output))
			if model.cost > 0 {
				valueStr += fmt.Sprintf(" · %s", formatUSD(model.cost))
			}
		case "cost":
			valueStr = fmt.Sprintf("%2.0f%% %s tok · %s", pct, shortCompact(model.input+model.output), formatUSD(model.cost))
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

func selectBurnMode(totalTokens, totalCost, totalRequests float64) (string, float64) {
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
	return modelColorPalette[distributedPaletteIndex(base, position, len(modelColorPalette))]
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
		segs = append(segs, seg{val: value, color: colorForClient(colors, client.name)})
	}
	if sumTop < total {
		segs = append(segs, seg{val: total - sumTop, color: colorSurface1})
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
	for _, model := range models {
		value := modelMixValue(model, mode)
		if value <= 0 {
			continue
		}
		sumTop += value
		segs = append(segs, seg{val: value, color: colorForModel(colors, model.name)})
	}
	if sumTop < total {
		segs = append(segs, seg{val: total - sumTop, color: colorSurface1})
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
		segs = append(segs, seg{val: tool.count, color: colorForTool(colors, tool.name)})
	}
	if sumTop < total {
		segs = append(segs, seg{val: total - sumTop, color: colorSurface1})
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
