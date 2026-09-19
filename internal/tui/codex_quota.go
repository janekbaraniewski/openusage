package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// Both tile and detail use the same account-scoped quota windows. The gauges
// render used-percent like every other provider (RenderUsageGauge), keeping
// the multi-pool grouping and explicit "window unavailable" notes. Never fall
// back to cache-hit or context percentages when subscription quotas are
// absent.
func buildCodexQuotaLines(snap core.UsageSnapshot, width int, warn, crit float64, now time.Time) []string {
	groups := map[string]string{}
	for key := range snap.Metrics {
		if !strings.HasPrefix(key, "rate_limit_") {
			continue
		}
		if strings.HasSuffix(key, "_primary") || strings.HasSuffix(key, "_secondary") {
			prefix := key[:strings.LastIndex(key, "_")+1]
			name, _ := snap.MetaValue(key + "_bucket")
			groups[prefix] = core.FirstNonEmpty(name, "codex")
		}
	}
	if len(groups) == 0 {
		return []string{dimStyle.Render("Quotas unavailable")}
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
		for _, window := range []string{"5h", "7d"} {
			if !seen[window] {
				lines = append(lines, dimStyle.Render(fmt.Sprintf("%s: window unavailable", window)))
			}
		}
	}
	if len(lines) == 0 {
		return []string{dimStyle.Render("Quotas unavailable")}
	}
	return lines
}
