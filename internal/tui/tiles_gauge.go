package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

func (m Model) buildTileGaugeLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int) []string {
	maxLabelW := 14
	gaugeW := innerW - maxLabelW - 10 // label + gauge + " XX.X%" + spaces
	if gaugeW < 6 {
		gaugeW = 6
	}
	maxLines := widget.GaugeMaxLines
	if maxLines <= 0 {
		maxLines = 2
	}

	if len(snap.Metrics) == 0 {
		// No metrics yet — show shimmer placeholders if gauges are expected.
		return m.buildGaugeShimmerLines(widget, maxLabelW, gaugeW, maxLines)
	}

	keys := lo.Keys(snap.Metrics)
	sort.Strings(keys)
	keys = prioritizeMetricKeys(keys, widget.GaugePriority)

	// When GaugePriority is set, treat it as an allowlist — only those
	// metrics are eligible for gauge rendering.
	var gaugeAllowSet map[string]bool
	if len(widget.GaugePriority) > 0 {
		gaugeAllowSet = make(map[string]bool, len(widget.GaugePriority))
		for _, k := range widget.GaugePriority {
			gaugeAllowSet[k] = true
		}
	}

	var lines []string
	for _, key := range keys {
		if gaugeAllowSet != nil && !gaugeAllowSet[key] {
			continue
		}
		met := snap.Metrics[key]
		usedPct := metricUsedPercent(key, met)
		if usedPct < 0 {
			continue
		}

		label := gaugeLabel(widget, key, met.Window)
		if len(label) > maxLabelW {
			label = label[:maxLabelW-1] + "…"
		}

		gauge := RenderUsageGauge(usedPct, gaugeW, m.warnThreshold, m.critThreshold)

		// Check for stacked gauge configuration
		if sgCfg, ok := widget.StackedGaugeKeys[key]; ok && len(sgCfg.SegmentMetricKeys) > 0 {
			segments := buildStackedSegments(snap, sgCfg, met)
			if len(segments) > 0 {
				gauge = RenderStackedUsageGauge(segments, usedPct, gaugeW)
			}
		}

		labelR := lipgloss.NewStyle().Foreground(colorSubtext).Width(maxLabelW).Render(label)
		lines = append(lines, labelR+" "+gauge)
		if maxLines > 0 && len(lines) >= maxLines {
			break
		}
	}

	// Gauges expected but not yet renderable (metrics exist but none are
	// gauge-eligible yet, e.g. local data loaded but API billing data hasn't).
	// Only shimmer if at least one gauge-priority metric EXISTS in the snapshot
	// (meaning the data source reports it but it's not yet gauge-eligible).
	// If none of the priority keys exist, the provider simply doesn't supply
	// gauge data (e.g. free-plan accounts) — skip the gauge area entirely.
	if len(lines) == 0 {
		anyPriorityPresent := false
		for _, k := range widget.GaugePriority {
			if _, ok := snap.Metrics[k]; ok {
				anyPriorityPresent = true
				break
			}
		}
		if anyPriorityPresent {
			return m.buildGaugeShimmerLines(widget, maxLabelW, gaugeW, maxLines)
		}
		return nil
	}
	return lines
}

// buildGaugeShimmerLines renders animated placeholder gauge tracks while
// waiting for gauge-eligible metric data.
func (m Model) buildGaugeShimmerLines(widget core.DashboardWidget, maxLabelW, gaugeW, maxLines int) []string {
	if len(widget.GaugePriority) == 0 {
		return nil
	}
	var lines []string
	for i, key := range widget.GaugePriority {
		if i >= maxLines {
			break
		}
		label := gaugeLabel(widget, key)
		if len(label) > maxLabelW {
			label = label[:maxLabelW-1] + "…"
		}
		// Offset each bar's animation slightly so they shimmer in sequence.
		shimmer := RenderShimmerGauge(gaugeW, m.animFrame+i*5)
		labelR := lipgloss.NewStyle().Foreground(colorDim).Width(maxLabelW).Render(label)
		lines = append(lines, labelR+" "+shimmer)
	}
	return lines
}

func buildStackedSegments(snap core.UsageSnapshot, cfg core.StackedGaugeConfig, met core.Metric) []GaugeSegment {
	if met.Limit == nil || *met.Limit <= 0 {
		return nil
	}
	limit := *met.Limit
	var segments []GaugeSegment
	for i, metricKey := range cfg.SegmentMetricKeys {
		segMetric, ok := snap.Metrics[metricKey]
		if !ok || segMetric.Used == nil || *segMetric.Used <= 0 {
			continue
		}
		pct := *segMetric.Used / limit * 100
		color := resolveSegmentColor(cfg, i)
		segments = append(segments, GaugeSegment{Percent: pct, Color: color})
	}
	return segments
}

func resolveSegmentColor(cfg core.StackedGaugeConfig, idx int) lipgloss.Color {
	if idx >= len(cfg.SegmentColors) {
		return colorSubtext
	}
	switch cfg.SegmentColors[idx] {
	case "teal":
		return colorTeal
	case "peach":
		return colorPeach
	case "green":
		return colorGreen
	case "yellow":
		return colorYellow
	case "blue":
		return colorBlue
	case "red":
		return colorRed
	case "lavender":
		return colorLavender
	case "sapphire":
		return colorSapphire
	default:
		return colorSubtext
	}
}

func gaugeLabel(widget core.DashboardWidget, key string, window ...string) string {
	overrides := map[string]string{
		"plan_percent_used":    "Plan Used",
		"plan_spend":           "Credits",
		"plan_total_spend_usd": "Total Credits",
		"spend_limit":          "Credit Limit",
		"individual_spend":     "My Credits",
		"team_budget":          "Team Budget",
	}

	if strings.HasPrefix(key, "rate_limit_") {
		w := ""
		if len(window) > 0 {
			w = window[0]
		}
		if w != "" {
			return "Usage " + w
		}
		return "Usage " + metricLabel(widget, strings.TrimPrefix(key, "rate_limit_"))
	}
	if label, ok := overrides[key]; ok {
		return label
	}
	return metricLabel(widget, key)
}

func metricUsedPercent(key string, met core.Metric) float64 {
	return core.MetricUsedPercent(key, met)
}

func metricHasGauge(key string, met core.Metric) bool {
	return metricUsedPercent(key, met) >= 0
}

func parseTileNumeric(raw string) (float64, bool) {
	s := strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if s == "" {
		return 0, false
	}
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSuffix(s, "%")
	if idx := strings.IndexByte(s, ' '); idx > 0 {
		s = s[:idx]
	}
	if idx := strings.IndexByte(s, '/'); idx > 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
