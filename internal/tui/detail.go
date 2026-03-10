package tui

import (
	"fmt"
	"math"
	"strconv"
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

func wrapTags(tags []string, maxWidth int) []string {
	if len(tags) == 0 {
		return nil
	}
	var rows []string
	currentRow := ""
	currentW := 0
	sep := " "
	sepW := 1

	for _, tag := range tags {
		tagW := lipgloss.Width(tag)
		if currentW > 0 && currentW+sepW+tagW > maxWidth {
			rows = append(rows, currentRow)
			currentRow = tag
			currentW = tagW
		} else {
			if currentW > 0 {
				currentRow += sep
				currentW += sepW
			}
			currentRow += tag
			currentW += tagW
		}
	}
	if currentRow != "" {
		rows = append(rows, currentRow)
	}
	return rows
}

func titleCase(s string) string {
	if len(s) <= 1 {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

// renderInfoSection renders Attributes, Diagnostics, and Raw as separate sub-sections.
func renderInfoSection(sb *strings.Builder, snap core.UsageSnapshot, widget core.DashboardWidget, w int) {
	labelW := sectionLabelWidth(w)
	maxValW := w - labelW - 6
	if maxValW < 20 {
		maxValW = 20
	}
	if maxValW > 45 {
		maxValW = 45
	}

	if len(snap.Attributes) > 0 {
		renderDetailSectionHeader(sb, "Attributes", w)
		renderKeyValuePairs(sb, snap.Attributes, labelW, maxValW, valueStyle)
	}

	if len(snap.Diagnostics) > 0 {
		if len(snap.Attributes) > 0 {
			sb.WriteString("\n")
		}
		renderDetailSectionHeader(sb, "Diagnostics", w)
		warnValueStyle := lipgloss.NewStyle().Foreground(colorYellow)
		renderKeyValuePairs(sb, snap.Diagnostics, labelW, maxValW, warnValueStyle)
	}

	if len(snap.Raw) > 0 {
		if len(snap.Attributes) > 0 || len(snap.Diagnostics) > 0 {
			sb.WriteString("\n")
		}
		renderDetailSectionHeader(sb, "Raw Data", w)
		renderRawData(sb, snap.Raw, widget, w)
	}
}

// renderKeyValuePairs renders a sorted key-value map with consistent formatting.
func renderKeyValuePairs(sb *strings.Builder, data map[string]string, labelW, maxValW int, vs lipgloss.Style) {
	keys := core.SortedStringKeys(data)

	for _, k := range keys {
		v := smartFormatValue(data[k])
		if len(v) > maxValW {
			v = v[:maxValW-3] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			labelStyle.Width(labelW).Render(prettifyKey(k)),
			vs.Render(v),
		))
	}
}

func renderRawData(sb *strings.Builder, raw map[string]string, widget core.DashboardWidget, w int) {
	labelW := sectionLabelWidth(w)

	maxValW := w - labelW - 6
	if maxValW < 20 {
		maxValW = 20
	}
	if maxValW > 45 {
		maxValW = 45
	}

	rendered := make(map[string]bool)

	for _, g := range widget.RawGroups {
		hasAny := false
		for _, key := range g.Keys {
			if v, ok := raw[key]; ok && v != "" {
				hasAny = true
				_ = v
				break
			}
		}
		if !hasAny {
			continue
		}
		for _, key := range g.Keys {
			v, ok := raw[key]
			if !ok || v == "" {
				continue
			}
			rendered[key] = true
			fv := smartFormatValue(v)
			if len(fv) > maxValW {
				fv = fv[:maxValW-3] + "..."
			}
			sb.WriteString(fmt.Sprintf("  %s  %s\n",
				labelStyle.Width(labelW).Render(prettifyKey(key)),
				valueStyle.Render(fv),
			))
		}
	}

	keys := core.SortedStringKeys(raw)

	for _, k := range keys {
		if rendered[k] || strings.HasSuffix(k, "_error") {
			continue
		}
		v := smartFormatValue(raw[k])
		if len(v) > maxValW {
			v = v[:maxValW-3] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			labelStyle.Width(labelW).Render(prettifyKey(k)),
			dimStyle.Render(v),
		))
	}
}

func smartFormatValue(v string) string {
	trimmed := strings.TrimSpace(v)

	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 1e12 && n < 2e13 {
		t := time.Unix(n/1000, 0)
		return t.Format("Jan 02, 2006 15:04")
	}

	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 1e9 && n < 2e10 {
		t := time.Unix(n, 0)
		return t.Format("Jan 02, 2006 15:04")
	}

	return v
}

func renderDetailSectionHeader(sb *strings.Builder, title string, w int) {
	icon := sectionIcon(title)
	sc := sectionColor(title)

	iconStyled := lipgloss.NewStyle().Foreground(sc).Render(icon)
	titleStyled := lipgloss.NewStyle().Bold(true).Foreground(sc).Render(" " + title + " ")
	left := "  " + iconStyled + titleStyled

	lineLen := w - lipgloss.Width(left) - 2
	if lineLen < 4 {
		lineLen = 4
	}
	line := lipgloss.NewStyle().Foreground(sc).Render(strings.Repeat("─", lineLen))
	sb.WriteString(left + line + "\n")
}

func sectionIcon(title string) string {
	switch title {
	case "Usage":
		return "⚡"
	case "Spending":
		return "💰"
	case "Tokens":
		return "📊"
	case "Activity":
		return "📈"
	case "Timers":
		return "⏰"
	case "Models":
		return "🤖"
	case "Languages":
		return "🗂"
	case "Trends":
		return "📈"
	case "MCP Usage":
		return "🔌"
	case "Attributes":
		return "📋"
	case "Diagnostics":
		return "⚠"
	case "Raw Data":
		return "🔧"
	default:
		return "›"
	}
}

func sectionColor(title string) lipgloss.Color {
	switch title {
	case "Usage":
		return colorYellow
	case "Spending":
		return colorTeal
	case "Tokens":
		return colorSapphire
	case "Activity":
		return colorGreen
	case "Timers":
		return colorMaroon
	case "Models":
		return colorLavender
	case "Languages":
		return colorPeach
	case "Trends":
		return colorSapphire
	case "MCP Usage":
		return colorSky
	case "Attributes":
		return colorBlue
	case "Diagnostics":
		return colorYellow
	case "Raw Data":
		return colorDim
	default:
		return colorBlue
	}
}

func formatUsageDetail(m core.Metric) string {
	var parts []string

	if m.Remaining != nil {
		parts = append(parts, fmt.Sprintf("%.0f%% remaining", *m.Remaining))
	} else if m.Used != nil && m.Limit != nil {
		rem := *m.Limit - *m.Used
		parts = append(parts, fmt.Sprintf("%.0f%% remaining", rem))
	}

	if m.Window != "" && m.Window != "all_time" && m.Window != "current_period" {
		parts = append(parts, "["+m.Window+"]")
	}

	return strings.Join(parts, " ")
}

func formatMetricDetail(m core.Metric) string {
	var parts []string
	switch {
	case m.Used != nil && m.Limit != nil:
		parts = append(parts, fmt.Sprintf("%s / %s %s",
			formatNumber(*m.Used), formatNumber(*m.Limit), m.Unit))
	case m.Remaining != nil && m.Limit != nil:
		parts = append(parts, fmt.Sprintf("%s / %s %s remaining",
			formatNumber(*m.Remaining), formatNumber(*m.Limit), m.Unit))
	case m.Used != nil:
		parts = append(parts, fmt.Sprintf("%s %s", formatNumber(*m.Used), m.Unit))
	case m.Remaining != nil:
		parts = append(parts, fmt.Sprintf("%s %s remaining", formatNumber(*m.Remaining), m.Unit))
	}

	if m.Window != "" && m.Window != "all_time" && m.Window != "current_period" {
		parts = append(parts, "["+m.Window+"]")
	}

	return strings.Join(parts, " ")
}

func formatNumber(n float64) string {
	if n == 0 {
		return "0"
	}
	abs := math.Abs(n)
	switch {
	case abs >= 1_000_000:
		return fmt.Sprintf("%.1fM", n/1_000_000)
	case abs >= 10_000:
		return fmt.Sprintf("%.1fK", n/1_000)
	case abs >= 1_000:
		return fmt.Sprintf("%.0f", n)
	case abs == math.Floor(abs):
		return fmt.Sprintf("%.0f", n)
	default:
		return fmt.Sprintf("%.2f", n)
	}
}

func formatTokens(n float64) string {
	if n == 0 {
		return "-"
	}
	return formatNumber(n)
}

func formatUSD(n float64) string {
	if n == 0 {
		return "-"
	}
	if n >= 1000 {
		return fmt.Sprintf("$%.0f", n)
	}
	return fmt.Sprintf("$%.2f", n)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

func prettifyKey(key string) string {
	return core.PrettifyMetricKey(key)
}

func prettifyModelName(name string) string {
	result := strings.ReplaceAll(name, "_", "-")

	switch strings.ToLower(result) {
	case "unattributed":
		return "unmapped spend (missing historical mapping)"
	case "default":
		return "default (auto)"
	case "composer-1":
		return "composer-1 (agent)"
	case "github-bugbot":
		return "github-bugbot (auto)"
	}
	return result
}
