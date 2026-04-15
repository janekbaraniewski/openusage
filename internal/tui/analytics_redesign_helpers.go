package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func analyticsWindowDays(window core.TimeWindow) int {
	if window == core.TimeWindowAll {
		return 0
	}
	return window.Days()
}

func analyticsComparisonWindowDays(window core.TimeWindow) int {
	switch window {
	case core.TimeWindow1d:
		return 1
	case core.TimeWindow3d:
		return 3
	case core.TimeWindow7d:
		return 7
	case core.TimeWindow30d:
		return 30
	default:
		return 14
	}
}

func analyticsComparisonLabel(window core.TimeWindow) string {
	days := analyticsComparisonWindowDays(window)
	if days <= 1 {
		return "today vs prior day"
	}
	return fmt.Sprintf("last %dd vs prior %dd", days, days)
}

func analyticsWindowSubtitle(data costData) string {
	if data.timeWindow == core.TimeWindowAll {
		return "all retained telemetry"
	}
	return data.timeWindow.Label()
}

func analyticsTokenMixSubtitle(data costData) string {
	if data.totalInput <= 0 && data.totalOutput <= 0 {
		return "no token mix"
	}
	return fmt.Sprintf("in %s · out %s", shortCompact(data.totalInput), shortCompact(data.totalOutput))
}

func analyticsShareText(value, total float64) string {
	if value <= 0 || total <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", value/total*100)
}

func analyticsShareLabel(value, total float64) string {
	share := analyticsShareText(value, total)
	if share == "—" {
		return "no share"
	}
	return share + " of window"
}

func analyticsPerActiveDay(total float64, activeDays int) float64 {
	if total <= 0 {
		return 0
	}
	if activeDays <= 0 {
		return total
	}
	return total / float64(activeDays)
}

func analyticsModelEfficiencyLabel(model modelCostEntry) string {
	totalTokens := model.inputTokens + model.outputTokens
	if model.cost <= 0 || totalTokens <= 0 {
		return "no efficiency signal"
	}
	return fmt.Sprintf("$%.3f / 1K tok", model.cost/totalTokens*1000)
}

func analyticsSparkline(points []core.TimePoint, width int, color lipgloss.Color) string {
	if len(points) < 2 {
		return ""
	}
	values := make([]float64, 0, len(points))
	for _, point := range points {
		values = append(values, point.Value)
	}
	return RenderSparkline(values, width, color)
}

func analyticsCropSeries(points []core.TimePoint, window core.TimeWindow, referenceTime time.Time) []core.TimePoint {
	if analyticsWindowDays(window) <= 0 {
		return append([]core.TimePoint(nil), points...)
	}
	return clipAndPadPointsByRecentDays(points, analyticsWindowDays(window), referenceTime)
}

func analyticsTopProvider(data costData) (string, float64) {
	for _, provider := range data.providers {
		if score := providerAnalyticsRankValue(provider); score > 0 {
			return provider.name, score
		}
	}
	return "—", 0
}

func analyticsTopClient(data costData) (string, float64) {
	for _, client := range data.clients {
		if client.total > 0 {
			return client.name, client.total
		}
		if client.requests > 0 {
			return client.name, client.requests
		}
	}
	return "—", 0
}

func analyticsTopProject(data costData) (string, float64) {
	for _, project := range data.projects {
		if project.requests > 0 {
			return project.name, project.requests
		}
	}
	return "—", 0
}

func analyticsTopMCP(data costData) (string, float64) {
	for _, server := range data.mcpServers {
		if server.calls > 0 {
			return server.name, server.calls
		}
	}
	return "—", 0
}

func analyticsHotspotValueLabel(value float64, unit string) string {
	if value <= 0 {
		return "no data"
	}
	return shortCompact(value) + " " + unit
}

func providerAnalyticsRankValue(provider providerCostEntry) float64 {
	if provider.cost > 0 {
		return provider.cost
	}
	total := 0.0
	for _, model := range provider.models {
		total += model.inputTokens + model.outputTokens
	}
	return total
}

func analyticsProviderRankLabel(provider providerCostEntry, totalCost float64) (string, string) {
	if provider.cost > 0 {
		return formatUSD(provider.cost), analyticsShareLabel(provider.cost, totalCost)
	}
	totalTokens := providerAnalyticsRankValue(provider)
	if totalTokens > 0 {
		return shortCompact(totalTokens) + " tok", "activity only · no direct spend signal"
	}
	return "", ""
}

func filterNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func analyticsColumnWidth(totalWidth, cols, gap int) int {
	if cols <= 1 {
		return max(28, totalWidth)
	}
	return max(28, (totalWidth-(cols-1)*gap)/cols)
}

func analyticsJoinColumns(blocks ...string) string {
	return analyticsJoinColumnsWithGap(2, blocks...)
}

func analyticsJoinColumnsWithGap(gap int, blocks ...string) string {
	blocks = filterNonEmptyStrings(blocks)
	if len(blocks) == 0 {
		return ""
	}
	if len(blocks) == 1 {
		return blocks[0]
	}
	gapStr := strings.Repeat(" ", gap)
	return lipgloss.JoinHorizontal(lipgloss.Top, intersperse(blocks, gapStr)...)
}

func analyticsPadLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	cut := ansi.Cut(line, 0, width)
	if pad := width - lipgloss.Width(cut); pad > 0 {
		cut += strings.Repeat(" ", pad)
	}
	return cut
}
