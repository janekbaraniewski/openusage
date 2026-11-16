package tui

import (
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// Both tile and detail use the same account-scoped quota windows. The gauges
// render used-percent like every other provider (RenderUsageGauge), keeping
// the multi-pool grouping and explicit "window unavailable" notes. Never fall
// back to cache-hit or context percentages. Monthly subscription credits are
// a separately labeled fallback when rate-limit windows are absent.
func buildCodexQuotaLines(snap core.UsageSnapshot, width int, warn, crit float64, now time.Time) []string {
	groups := map[string]string{}
	for key, metric := range snap.Metrics {
		if !strings.HasPrefix(key, "rate_limit_") || !(metricUsedPercent(key, metric) >= 0) {
			continue
		}
		if strings.HasSuffix(key, "_primary") || strings.HasSuffix(key, "_secondary") {
			prefix := key[:strings.LastIndex(key, "_")+1]
			name, _ := snap.MetaValue(key + "_bucket")
			groups[prefix] = core.FirstNonEmpty(name, "codex")
		}
	}
	const maxLabelW = 14
	gaugeW := width - maxLabelW - 10 // label + gauge + " XX.X%" + spaces
	if gaugeW < 6 {
		gaugeW = 6
	}
	annotationIndent := strings.Repeat(" ", maxLabelW+1)

	var lines []string
	multi := len(groups) > 1
	for _, prefix := range core.SortedStringKeys(groups) {
		if multi {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorSubtext).Render(groups[prefix]))
		}
		seen := map[string]bool{}
		for _, slot := range []string{"primary", "secondary"} {
			key := prefix + slot
			met, ok := snap.Metrics[key]
			if !ok {
				continue
			}
			usedPct := metricUsedPercent(key, met)
			if usedPct < 0 {
				continue
			}
			seen[met.Window] = true

			label := "Usage " + met.Window
			if strings.TrimSpace(met.Window) == "" {
				label = "Usage " + slot
			}
			labelR := lipgloss.NewStyle().Foreground(colorSubtext).Width(maxLabelW).Render(label)
			lines = append(lines, labelR+" "+RenderUsageGauge(usedPct, gaugeW, warn, crit))

			if annot := tileGaugeProjectionAnnotation(snap, key, met, usedPct, now); annot != "" {
				lines = append(lines, annotationIndent+dimStyle.Render(annot))
			}
		}
		if prefix == "rate_limit_" && seen["7d"] && !seen["5h"] {
			lines = append(lines, dimStyle.Render("5h: window unavailable"))
		}
	}
	if len(lines) == 0 {
		if usedPct := codexCreditUsedPercent(snap); usedPct >= 0 {
			const creditLabel = "Monthly credits"
			creditGaugeW := max(6, width-len(creditLabel)-10)
			label := lipgloss.NewStyle().Foreground(colorSubtext).Render(creditLabel)
			lines = append(lines, label+" "+RenderUsageGauge(usedPct, creditGaugeW, warn, crit))
			if annot := tileCodexCreditProjectionAnnotation(snap, usedPct, now); annot != "" {
				lines = append(lines, strings.Repeat(" ", len(creditLabel)+1)+dimStyle.Render(annot))
			}
			return lines
		}
		return []string{dimStyle.Render("Quotas unavailable")}
	}
	return lines
}

// Only the authoritative monthly credit metrics qualify for this fallback;
// dollar costs, token counts and activity ratios are not credit allowances.
func codexCreditUsedPercent(snap core.UsageSnapshot) float64 {
	for _, key := range []string{"codex_credit_percent_used", "codex_credit_limit"} {
		met, ok := snap.Metrics[key]
		if !ok || (key == "codex_credit_percent_used" && met.Unit != "%") || (key == "codex_credit_limit" && (met.Unit != "credits" || met.Limit == nil)) {
			continue
		}
		limit := 100.0
		if met.Limit != nil {
			limit = *met.Limit
		}
		if !(limit > 0) || math.IsInf(limit, 0) || (key == "codex_credit_percent_used" && limit != 100) {
			continue
		}
		valid := true
		for _, value := range []*float64{met.Used, met.Remaining} {
			if value != nil && (math.IsNaN(*value) || *value < 0 || *value > limit) {
				valid = false
			}
		}
		if valid {
			if usedPct := metricUsedPercent(key, met); usedPct >= 0 && usedPct <= 100 {
				return usedPct
			}
		}
	}
	return -1
}
