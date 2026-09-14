package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// Both tile and detail use the same account-scoped quota windows. Never fall
// back to cache-hit or context percentages when subscription quotas are absent.
func buildCodexQuotaLines(snap core.UsageSnapshot, width int, warn, crit float64) []string {
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
	var lines []string
	for _, prefix := range core.SortedStringKeys(groups) {
		lines = append(lines, lipgloss.NewStyle().Width(width).Render(groups[prefix]))
		seen := map[string]bool{}
		for _, slot := range []string{"primary", "secondary"} {
			key := prefix + slot
			met, ok := snap.Metrics[key]
			if !ok || met.Used == nil || met.Remaining == nil {
				continue
			}
			seen[met.Window] = true
			label := met.Window + " remaining"
			gaugeWidth := width - len(label) - 1 - 8
			if gaugeWidth < 6 {
				gaugeWidth = 6
			}
			if gaugeWidth > 50 {
				gaugeWidth = 50
			}
			lines = append(lines, label+" "+RenderGauge(*met.Remaining, gaugeWidth, warn, crit))
			reset := "reset unknown"
			if at, ok := snap.Resets[key]; ok {
				reset = "reset " + at.Local().Format("02 Jan 15:04 MST")
			}
			lines = append(lines, dimStyle.Render(reset))
		}
		for _, window := range []string{"5h", "7d"} {
			if !seen[window] {
				lines = append(lines, dimStyle.Render(fmt.Sprintf("%s: window unavailable", window)))
			}
		}
	}
	return lines
}
