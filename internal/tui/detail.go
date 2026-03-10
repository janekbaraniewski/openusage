package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type DetailTab int

const (
	TabAll  DetailTab = 0 // show everything
	TabDyn1 DetailTab = 1 // first dynamic group
)

func DetailTabs(snap core.UsageSnapshot) []string {
	tabs := []string{"All"}
	if len(snap.Metrics) > 0 {
		groups := groupMetrics(snap.Metrics, dashboardWidget(snap.ProviderID), detailWidget(snap.ProviderID))
		for _, g := range groups {
			tabs = append(tabs, g.title)
		}
	}
	if hasAnalyticsModelData(snap) {
		tabs = append(tabs, "Models")
	}
	if hasLanguageMetrics(snap) {
		tabs = append(tabs, "Languages")
	}
	if hasMCPMetrics(snap) {
		tabs = append(tabs, "MCP Usage")
	}
	if hasChartableSeries(snap.DailySeries) {
		tabs = append(tabs, "Trends")
	}
	if len(snap.Resets) > 0 {
		tabs = append(tabs, "Timers")
	}
	if len(snap.Attributes) > 0 || len(snap.Diagnostics) > 0 || len(snap.Raw) > 0 {
		tabs = append(tabs, "Info")
	}
	return tabs
}

func RenderDetailContent(snap core.UsageSnapshot, w int, warnThresh, critThresh float64, activeTab int) string {
	var sb strings.Builder
	widget := dashboardWidget(snap.ProviderID)
	details := detailWidget(snap.ProviderID)

	renderDetailHeader(&sb, snap, w)
	sb.WriteString("\n")

	tabs := DetailTabs(snap)
	if activeTab >= len(tabs) {
		activeTab = 0
	}

	renderTabBar(&sb, tabs, activeTab, w)

	if len(snap.Metrics) == 0 && activeTab == 0 {
		if snap.Message != "" {
			sb.WriteString("\n")
			sb.WriteString(dimStyle.Render("  " + snap.Message))
			sb.WriteString("\n")
		}
		return sb.String()
	}

	tabName := tabs[activeTab]
	showAll := tabName == "All"
	showTimers := tabName == "Timers" || showAll
	showInfo := tabName == "Info" || showAll

	costSummary := core.ExtractAnalyticsCostSummary(snap)
	burnRate := costSummary.BurnRateUSD

	if len(snap.Metrics) > 0 {
		groups := groupMetrics(snap.Metrics, widget, details)
		for _, group := range groups {
			if showAll || group.title == tabName {
				renderMetricGroup(&sb, snap, group, widget, details, w, warnThresh, critThresh, snap.DailySeries, burnRate)
			}
		}
	}

	showModels := tabName == "Models" || showAll
	if showModels && hasAnalyticsModelData(snap) {
		sb.WriteString("\n")
		renderDetailSectionHeader(&sb, "Models", w)
		renderModelsSection(&sb, snap, widget, w)
	}

	// Languages section — dispatched directly (needs full snapshot metrics).
	showLanguages := tabName == "Languages" || showAll
	if showLanguages && hasLanguageMetrics(snap) {
		sb.WriteString("\n")
		renderDetailSectionHeader(&sb, "Languages", w)
		renderLanguagesSection(&sb, snap, w)
	}

	// MCP Usage section — dispatched directly (needs full snapshot metrics).
	showMCP := tabName == "MCP Usage" || showAll
	if showMCP && hasMCPMetrics(snap) {
		sb.WriteString("\n")
		renderDetailSectionHeader(&sb, "MCP Usage", w)
		renderMCPSection(&sb, snap, w)
	}

	// Trends section — dispatched directly (needs full snapshot DailySeries).
	showTrends := tabName == "Trends" || showAll
	if showTrends && hasChartableSeries(snap.DailySeries) {
		sb.WriteString("\n")
		renderDetailSectionHeader(&sb, "Trends", w)
		renderTrendsSection(&sb, snap, widget, w)
	}

	if showTimers && len(snap.Resets) > 0 {
		sb.WriteString("\n")
		renderTimersSection(&sb, snap.Resets, widget, w)
	}

	hasInfoData := len(snap.Attributes) > 0 || len(snap.Diagnostics) > 0 || len(snap.Raw) > 0
	if showInfo && hasInfoData {
		sb.WriteString("\n")
		renderInfoSection(&sb, snap, widget, w)
	}

	age := time.Since(snap.Timestamp)
	if age > 60*time.Second {
		sb.WriteString("\n")
		warnBox := lipgloss.NewStyle().
			Foreground(colorYellow).
			Background(colorSurface0).
			Padding(0, 1).
			Bold(true).
			Render(fmt.Sprintf("⚠ Data is %s old — press r to refresh", formatDuration(age)))
		sb.WriteString("  " + warnBox + "\n")
	}

	return sb.String()
}

func renderTabBar(sb *strings.Builder, tabs []string, active int, w int) {
	if len(tabs) <= 1 {
		return // no point showing tabs when there's only "All"
	}

	var parts []string
	for i, t := range tabs {
		if i == active {
			parts = append(parts, tabActiveStyle.Render(t))
		} else {
			parts = append(parts, tabInactiveStyle.Render(t))
		}
	}

	tabLine := "  " + strings.Join(parts, "")
	sb.WriteString(tabLine + "\n")

	sepLen := w - 2
	if sepLen < 4 {
		sepLen = 4
	}
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface2).Render(strings.Repeat("─", sepLen)) + "\n")
}

func renderDetailHeader(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	di := computeDisplayInfo(snap, dashboardWidget(snap.ProviderID))

	innerW := w - 6 // card border + padding eats ~6 chars
	if innerW < 20 {
		innerW = 20
	}

	var cardLines []string

	statusPill := StatusPill(snap.Status)
	pillW := lipgloss.Width(statusPill)

	name := snap.AccountID
	maxName := innerW - pillW - 2
	if maxName < 8 {
		maxName = 8
	}
	if len(name) > maxName {
		name = name[:maxName-1] + "…"
	}

	nameRendered := detailHeroNameStyle.Render(name)
	nameW := lipgloss.Width(nameRendered)
	gap1 := innerW - nameW - pillW
	if gap1 < 1 {
		gap1 = 1
	}
	line1 := nameRendered + strings.Repeat(" ", gap1) + statusPill
	cardLines = append(cardLines, line1)

	var line2Parts []string
	if di.tagEmoji != "" && di.tagLabel != "" {
		line2Parts = append(line2Parts, CategoryTag(di.tagEmoji, di.tagLabel))
	}
	line2Parts = append(line2Parts, dimStyle.Render(snap.ProviderID))
	line2 := strings.Join(line2Parts, " "+dimStyle.Render("·")+" ")
	cardLines = append(cardLines, line2)

	var metaTags []string

	if email := snapshotMeta(snap, "account_email"); email != "" {
		metaTags = append(metaTags, MetaTagHighlight("✉", email))
	}

	if planName := snapshotMeta(snap, "plan_name"); planName != "" {
		metaTags = append(metaTags, MetaTag("◆", planName))
	}
	if planType := snapshotMeta(snap, "plan_type"); planType != "" {
		metaTags = append(metaTags, MetaTag("◇", planType))
	}
	if membership := snapshotMeta(snap, "membership_type"); membership != "" {
		metaTags = append(metaTags, MetaTag("👤", membership))
	}
	if team := snapshotMeta(snap, "team_membership"); team != "" {
		metaTags = append(metaTags, MetaTag("🏢", team))
	}
	if org := snapshotMeta(snap, "organization_name"); org != "" {
		metaTags = append(metaTags, MetaTag("🏢", org))
	}

	if model := snapshotMeta(snap, "active_model"); model != "" {
		metaTags = append(metaTags, MetaTag("⬡", model))
	}
	if cliVer := snapshotMeta(snap, "cli_version"); cliVer != "" {
		metaTags = append(metaTags, MetaTag("⌘", "v"+cliVer))
	}

	if planPrice := snapshotMeta(snap, "plan_price"); planPrice != "" {
		metaTags = append(metaTags, MetaTag("$", planPrice))
	}
	if credits := snapshotMeta(snap, "credits"); credits != "" {
		metaTags = append(metaTags, MetaTag("💳", credits))
	}

	if oauth := snapshotMeta(snap, "oauth_status"); oauth != "" {
		metaTags = append(metaTags, MetaTag("🔒", oauth))
	}
	if sub := snapshotMeta(snap, "subscription_status"); sub != "" {
		metaTags = append(metaTags, MetaTag("✓", sub))
	}

	if len(metaTags) > 0 {
		tagRows := wrapTags(metaTags, innerW)
		for _, row := range tagRows {
			cardLines = append(cardLines, row)
		}
	}

	cardLines = append(cardLines, "")

	if snap.Message != "" {
		msg := snap.Message
		if lipgloss.Width(msg) > innerW {
			msg = msg[:innerW-3] + "..."
		}
		cardLines = append(cardLines, lipgloss.NewStyle().Foreground(colorText).Italic(true).Render(msg))
	}

	if di.gaugePercent >= 0 {
		gaugeW := innerW - 10
		if gaugeW < 12 {
			gaugeW = 12
		}
		if gaugeW > 40 {
			gaugeW = 40
		}
		heroGauge := RenderGauge(di.gaugePercent, gaugeW, 0.3, 0.1) // use standard thresholds
		cardLines = append(cardLines, heroGauge)
		if di.summary != "" {
			summaryLine := heroLabelStyle.Render(di.summary)
			if di.detail != "" {
				summaryLine += dimStyle.Render("  ·  ") + heroLabelStyle.Render(di.detail)
			}
			cardLines = append(cardLines, summaryLine)
		}
	} else if di.summary != "" && snap.Message == "" {
		cardLines = append(cardLines, heroValueStyle.Render(di.summary))
		if di.detail != "" {
			cardLines = append(cardLines, heroLabelStyle.Render(di.detail))
		}
	}

	timeStr := snap.Timestamp.Format("15:04:05")
	age := time.Since(snap.Timestamp)
	if age > 60*time.Second {
		timeStr = fmt.Sprintf("%s (%s ago)", snap.Timestamp.Format("15:04:05"), formatDuration(age))
	}
	cardLines = append(cardLines, dimStyle.Render("⏱ "+timeStr))

	cardContent := strings.Join(cardLines, "\n")
	borderColor := StatusBorderColor(snap.Status)
	card := detailHeaderCardStyle.
		Width(innerW + 2). // +2 for padding
		BorderForeground(borderColor).
		Render(cardContent)

	sb.WriteString(card)
	sb.WriteString("\n")
}
