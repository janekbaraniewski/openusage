package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func (m *Model) cachedTileBodyLines(
	snap core.UsageSnapshot,
	widget core.DashboardWidget,
	di providerDisplayInfo,
	innerW int,
	modelMixExpanded bool,
) []string {
	key := tileBodyCacheKey(snap, widget, m.timeWindow, innerW, modelMixExpanded, m.hideSectionsWithNoData)
	if lines, ok := m.tileBodyCache[key]; ok {
		return lines
	}

	lines := m.buildTileBodyLines(snap, widget, di, innerW, modelMixExpanded)
	if m.tileBodyCache == nil {
		m.tileBodyCache = make(map[string][]string)
	}
	m.tileBodyCache[key] = lines
	return lines
}

func tileBodyCacheKey(
	snap core.UsageSnapshot,
	widget core.DashboardWidget,
	window core.TimeWindow,
	innerW int,
	modelMixExpanded bool,
	hideEmpty bool,
) string {
	return strings.Join([]string{
		snap.ProviderID,
		snap.AccountID,
		string(snap.Status),
		strconv.FormatInt(snap.Timestamp.Unix(), 10),
		strconv.Itoa(len(snap.Metrics)),
		strconv.Itoa(len(snap.Raw)),
		strconv.Itoa(len(snap.DailySeries)),
		strconv.Itoa(len(snap.Resets)),
		string(window),
		strconv.Itoa(innerW),
		strconv.FormatBool(modelMixExpanded),
		strconv.FormatBool(hideEmpty),
		tileWidgetCacheKey(widget),
	}, "|")
}

func tileWidgetCacheKey(widget core.DashboardWidget) string {
	parts := make([]string, 0, len(widget.EffectiveStandardSectionOrder())+10)
	for _, section := range widget.EffectiveStandardSectionOrder() {
		parts = append(parts, string(section))
	}
	parts = append(parts,
		fmt.Sprintf("client:%t", widget.ShowClientComposition),
		fmt.Sprintf("tool:%t", widget.ShowToolComposition),
		fmt.Sprintf("actual:%t", widget.ShowActualToolUsage),
		fmt.Sprintf("mcp:%t", widget.ShowMCPUsage),
		fmt.Sprintf("lang:%t", widget.ShowLanguageComposition),
		fmt.Sprintf("code:%t", widget.ShowCodeStatsComposition),
		fmt.Sprintf("fold_iface:%t", widget.ClientCompositionIncludeInterfaces),
		fmt.Sprintf("hide_zero:%t", widget.SuppressZeroNonUsageMetrics),
		"client_heading:"+widget.ClientCompositionHeading,
		"tool_heading:"+widget.ToolCompositionHeading,
	)
	return strings.Join(parts, ",")
}

func (m *Model) buildTileBodyLines(
	snap core.UsageSnapshot,
	widget core.DashboardWidget,
	di providerDisplayInfo,
	innerW int,
	modelMixExpanded bool,
) []string {
	truncate := func(s string) string {
		if lipglossWidth := len([]rune(s)); lipglossWidth > innerW {
			return s[:innerW-1] + "…"
		}
		return s
	}

	type section struct {
		lines []string
	}
	sectionsByID := make(map[core.DashboardStandardSection]section)
	withSectionPadding := func(lines []string) []string {
		if len(lines) == 0 {
			return nil
		}
		s := []string{""}
		s = append(s, lines...)
		return s
	}
	addUsedKeys := func(dst map[string]bool, src map[string]bool) map[string]bool {
		if len(src) == 0 {
			return dst
		}
		if dst == nil {
			dst = make(map[string]bool, len(src))
		}
		for k := range src {
			dst[k] = true
		}
		return dst
	}
	appendOtherGroup := func(dst []string, lines []string) []string {
		if len(lines) == 0 {
			return dst
		}
		if len(dst) > 0 {
			dst = append(dst, "")
		}
		dst = append(dst, lines...)
		return dst
	}

	topUsageLines := m.buildTileGaugeLines(snap, widget, innerW)
	if di.summary != "" {
		topUsageLines = append(topUsageLines, tileHeroStyle.Render(truncate(di.summary)))
	}
	if di.detail != "" {
		topUsageLines = append(topUsageLines, tileSummaryStyle.Render(truncate(di.detail)))
	}
	if wl := windowActivityLine(snap, m.timeWindow); wl != "" {
		topUsageLines = append(topUsageLines, dimStyle.Render(truncate(wl)))
	}
	if len(topUsageLines) > 0 {
		sectionsByID[core.DashboardSectionTopUsageProgress] = section{withSectionPadding(topUsageLines)}
	}

	compactMetricLines, compactMetricKeys := buildTileCompactMetricSummaryLines(snap, widget, innerW)

	modelBurnLines, modelBurnKeys := buildProviderModelCompositionLines(snap, innerW, modelMixExpanded)
	if len(modelBurnLines) > 0 {
		sectionsByID[core.DashboardSectionModelBurn] = section{withSectionPadding(modelBurnLines)}
	}
	compactMetricKeys = addUsedKeys(compactMetricKeys, modelBurnKeys)

	if widget.ShowClientComposition {
		clientBurnLines, clientBurnKeys := buildProviderClientCompositionLinesWithWidget(snap, innerW, modelMixExpanded, widget)
		if len(clientBurnLines) > 0 {
			sectionsByID[core.DashboardSectionClientBurn] = section{withSectionPadding(clientBurnLines)}
		}
		compactMetricKeys = addUsedKeys(compactMetricKeys, clientBurnKeys)
	}

	projectBreakdownLines, projectBreakdownKeys := buildProviderProjectBreakdownLines(snap, innerW, modelMixExpanded)
	if len(projectBreakdownLines) > 0 {
		sectionsByID[core.DashboardSectionProjectBreakdown] = section{withSectionPadding(projectBreakdownLines)}
	}
	compactMetricKeys = addUsedKeys(compactMetricKeys, projectBreakdownKeys)

	var toolBurnLines []string
	if widget.ShowToolComposition {
		var toolBurnKeys map[string]bool
		toolBurnLines, toolBurnKeys = buildProviderToolCompositionLines(snap, innerW, modelMixExpanded, widget)
		compactMetricKeys = addUsedKeys(compactMetricKeys, toolBurnKeys)
	}

	actualToolLines, actualToolKeys := buildActualToolUsageLines(snap, innerW, modelMixExpanded)
	compactMetricKeys = addUsedKeys(compactMetricKeys, actualToolKeys)
	if len(actualToolLines) > 0 {
		sectionsByID[core.DashboardSectionToolUsage] = section{withSectionPadding(actualToolLines)}
	} else if len(toolBurnLines) > 0 {
		sectionsByID[core.DashboardSectionToolUsage] = section{withSectionPadding(toolBurnLines)}
	}

	if widget.ShowMCPUsage {
		mcpUsageLines, mcpUsageKeys := buildMCPUsageLines(snap, innerW, modelMixExpanded)
		if len(mcpUsageLines) > 0 {
			sectionsByID[core.DashboardSectionMCPUsage] = section{withSectionPadding(mcpUsageLines)}
		}
		compactMetricKeys = addUsedKeys(compactMetricKeys, mcpUsageKeys)
	}

	if widget.ShowLanguageComposition {
		langBurnLines, langBurnKeys := buildProviderLanguageCompositionLines(snap, innerW, modelMixExpanded)
		if len(langBurnLines) > 0 {
			sectionsByID[core.DashboardSectionLanguageBurn] = section{withSectionPadding(langBurnLines)}
		}
		compactMetricKeys = addUsedKeys(compactMetricKeys, langBurnKeys)
	}

	if widget.ShowCodeStatsComposition {
		codeStatsLines, codeStatsKeys := buildProviderCodeStatsLines(snap, widget, innerW)
		if len(codeStatsLines) > 0 {
			sectionsByID[core.DashboardSectionCodeStats] = section{withSectionPadding(codeStatsLines)}
		}
		compactMetricKeys = addUsedKeys(compactMetricKeys, codeStatsKeys)
	}

	dailyUsageLines := buildProviderDailyTrendLines(snap, innerW)
	if len(dailyUsageLines) > 0 {
		sectionsByID[core.DashboardSectionDailyUsage] = section{withSectionPadding(dailyUsageLines)}
	}

	upstreamProviderLines, upstreamProviderKeys := buildUpstreamProviderCompositionLines(snap, innerW, modelMixExpanded)
	if len(upstreamProviderLines) > 0 {
		sectionsByID[core.DashboardSectionUpstreamProviders] = section{withSectionPadding(upstreamProviderLines)}
	}
	compactMetricKeys = addUsedKeys(compactMetricKeys, upstreamProviderKeys)

	providerBurnLines, providerBurnKeys := buildProviderVendorCompositionLines(snap, innerW, modelMixExpanded)
	if len(providerBurnLines) > 0 {
		sectionsByID[core.DashboardSectionProviderBurn] = section{withSectionPadding(providerBurnLines)}
	}
	compactMetricKeys = addUsedKeys(compactMetricKeys, providerBurnKeys)

	var otherLines []string
	otherLines = appendOtherGroup(otherLines, compactMetricLines)

	geminiQuotaLines, geminiQuotaKeys := buildGeminiOtherQuotaLines(snap, innerW)
	otherLines = appendOtherGroup(otherLines, geminiQuotaLines)
	compactMetricKeys = addUsedKeys(compactMetricKeys, geminiQuotaKeys)

	metricLines := m.buildTileMetricLines(snap, widget, innerW, compactMetricKeys)
	otherLines = appendOtherGroup(otherLines, metricLines)

	if snap.Message != "" && snap.Status != core.StatusError {
		msg := snap.Message
		if len(msg) > innerW-3 {
			msg = msg[:innerW-6] + "..."
		}
		otherLines = appendOtherGroup(otherLines, []string{
			lipglossNewItalic(msg),
		})
	}

	metaLines := buildTileMetaLines(snap, innerW)
	otherLines = appendOtherGroup(otherLines, metaLines)
	if len(otherLines) > 0 {
		sectionsByID[core.DashboardSectionOtherData] = section{withSectionPadding(otherLines)}
	}

	var fullBody []string
	for _, sectionID := range widget.EffectiveStandardSectionOrder() {
		if sectionID == core.DashboardSectionHeader {
			continue
		}
		sec, ok := sectionsByID[sectionID]
		if ok && len(sec.lines) > 0 {
			fullBody = append(fullBody, sec.lines...)
			continue
		}
		if m.hideSectionsWithNoData {
			continue
		}
		emptyLines := buildEmptyTileSectionLines(sectionID, widget)
		if len(emptyLines) == 0 {
			continue
		}
		fullBody = append(fullBody, withSectionPadding(emptyLines)...)
	}

	return fullBody
}

func lipglossNewItalic(msg string) string {
	return lipgloss.NewStyle().Foreground(colorSubtext).Italic(true).Render(msg)
}
