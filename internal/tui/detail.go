package tui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/agentusage/internal/core"
)

// ─── Detail Content ─────────────────────────────────────────────────────────

// DetailTab identifies which tab is active in the detail panel.
type DetailTab int

const (
	TabAll  DetailTab = 0 // show everything
	TabDyn1 DetailTab = 1 // first dynamic group
	// ... subsequent groups assigned dynamically
)

// DetailTabs computes the available tabs for a given snapshot.
// Tab 0 is always "All". Then one tab per metric group. Then "Info" if there are timers/raw.
func DetailTabs(snap core.QuotaSnapshot) []string {
	tabs := []string{"All"}
	if len(snap.Metrics) > 0 {
		groups := groupMetrics(snap.Metrics)
		for _, g := range groups {
			tabs = append(tabs, g.title)
		}
	}
	if len(snap.Resets) > 0 || len(snap.Raw) > 0 {
		tabs = append(tabs, "Info")
	}
	return tabs
}

// RenderDetailContent renders the full scrollable detail for a snapshot.
// activeTab selects which content tab to show (0=All).
// The returned string may exceed the visible height; the model handles scrolling.
func RenderDetailContent(snap core.QuotaSnapshot, w int, warnThresh, critThresh float64, activeTab int) string {
	var sb strings.Builder

	renderDetailHeader(&sb, snap, w)
	sb.WriteString("\n")

	// Compute tabs
	tabs := DetailTabs(snap)
	if activeTab >= len(tabs) {
		activeTab = 0
	}

	// Render tab bar
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
	showInfo := tabName == "Info" || showAll

	// Group metrics by category and render selected groups.
	if len(snap.Metrics) > 0 {
		groups := groupMetrics(snap.Metrics)
		for _, group := range groups {
			if showAll || group.title == tabName {
				renderMetricGroup(&sb, group, w, warnThresh, critThresh)
			}
		}
	}

	// ── Reset timers ──
	if showInfo && len(snap.Resets) > 0 {
		labelW := 22
		if w < 55 {
			labelW = 18
		}
		sb.WriteString("\n")
		renderDetailSectionHeader(&sb, "Timers", w)
		// Sort timer keys for consistent ordering
		timerKeys := make([]string, 0, len(snap.Resets))
		for k := range snap.Resets {
			timerKeys = append(timerKeys, k)
		}
		sort.Strings(timerKeys)
		for _, k := range timerKeys {
			t := snap.Resets[k]
			label := prettifyKey(k)
			remaining := time.Until(t)
			dateStr := t.Format("Jan 02 15:04")
			if remaining > 0 {
				sb.WriteString(fmt.Sprintf("  %s  %s (in %s)\n",
					labelStyle.Width(labelW).Render(label),
					valueStyle.Render(dateStr),
					tealStyle.Render(formatDuration(remaining)),
				))
			} else {
				sb.WriteString(fmt.Sprintf("  %s  %s (expired)\n",
					labelStyle.Width(labelW).Render(label),
					dimStyle.Render(dateStr),
				))
			}
		}
	}

	// ── Raw data (collapsible) ──
	if showInfo && len(snap.Raw) > 0 {
		sb.WriteString("\n")
		count := len(snap.Raw)
		renderDetailSectionHeader(&sb, fmt.Sprintf("› Details (%d entries)", count), w)
		renderRawData(&sb, snap.Raw, w)
	}

	// ── Staleness warning (prominent) ──
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

// renderTabBar draws the tab strip at the top of the detail panel.
// Uses a consistent underline style matching the section headers.
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

// ─── Detail Header Card ────────────────────────────────────────────────────
//
// The header card is the visual centrepiece of the provider detail page.
// It renders inside a rounded border colored by status:
//
//	╭─────────────────────────────────────────────────────╮
//	│  claude-code                               ■ OK ■   │
//	│  ⚡ Rate  ·  claude_code                             │
//	│  ✉ jan@example.com  ·  team  ·  opus                │
//	│                                                     │
//	│  Claude Code CLI usage (stats-cache + account + …)  │
//	│  ⏱ 01:42:18                                         │
//	╰─────────────────────────────────────────────────────╯

func renderDetailHeader(sb *strings.Builder, snap core.QuotaSnapshot, w int) {
	di := computeDisplayInfo(snap)

	innerW := w - 6 // card border + padding eats ~6 chars
	if innerW < 20 {
		innerW = 20
	}

	var cardLines []string

	// ── Line 1: Provider name + Status pill (right-aligned) ──
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

	// ── Line 2: Category tag + provider ID ──
	var line2Parts []string
	if di.tagEmoji != "" && di.tagLabel != "" {
		line2Parts = append(line2Parts, CategoryTag(di.tagEmoji, di.tagLabel))
	}
	line2Parts = append(line2Parts, dimStyle.Render(snap.ProviderID))
	line2 := strings.Join(line2Parts, " "+dimStyle.Render("·")+" ")
	cardLines = append(cardLines, line2)

	// ── Line 3+: Metadata tags rows (rich context about the provider) ──
	var metaTags []string

	// Identity tags
	if email, ok := snap.Raw["account_email"]; ok && email != "" {
		metaTags = append(metaTags, MetaTagHighlight("✉", email))
	}

	// Plan & subscription tags
	if planName, ok := snap.Raw["plan_name"]; ok && planName != "" {
		metaTags = append(metaTags, MetaTag("◆", planName))
	}
	if planType, ok := snap.Raw["plan_type"]; ok && planType != "" {
		metaTags = append(metaTags, MetaTag("◇", planType))
	}
	if membership, ok := snap.Raw["membership_type"]; ok && membership != "" {
		metaTags = append(metaTags, MetaTag("👤", membership))
	}
	if team, ok := snap.Raw["team_membership"]; ok && team != "" {
		metaTags = append(metaTags, MetaTag("🏢", team))
	}
	if org, ok := snap.Raw["organization_name"]; ok && org != "" {
		metaTags = append(metaTags, MetaTag("🏢", org))
	}

	// Model & tool tags
	if model, ok := snap.Raw["active_model"]; ok && model != "" {
		metaTags = append(metaTags, MetaTag("⬡", model))
	}
	if cliVer, ok := snap.Raw["cli_version"]; ok && cliVer != "" {
		metaTags = append(metaTags, MetaTag("⌘", "v"+cliVer))
	}

	// Financial tags
	if planPrice, ok := snap.Raw["plan_price"]; ok && planPrice != "" {
		metaTags = append(metaTags, MetaTag("$", planPrice))
	}
	if credits, ok := snap.Raw["credits"]; ok && credits != "" {
		metaTags = append(metaTags, MetaTag("💳", credits))
	}

	// OAuth / auth status
	if oauth, ok := snap.Raw["oauth_status"]; ok && oauth != "" {
		metaTags = append(metaTags, MetaTag("🔒", oauth))
	}
	if sub, ok := snap.Raw["subscription_status"]; ok && sub != "" {
		metaTags = append(metaTags, MetaTag("✓", sub))
	}

	if len(metaTags) > 0 {
		// Wrap tags into rows that fit innerW
		tagRows := wrapTags(metaTags, innerW)
		for _, row := range tagRows {
			cardLines = append(cardLines, row)
		}
	}

	// ── Blank line before message ──
	cardLines = append(cardLines, "")

	// ── Message line: the primary status description ──
	if snap.Message != "" {
		msg := snap.Message
		if lipgloss.Width(msg) > innerW {
			msg = msg[:innerW-3] + "..."
		}
		cardLines = append(cardLines, lipgloss.NewStyle().Foreground(colorText).Italic(true).Render(msg))
	}

	// ── Hero metric: if there's a gauge %, show a big bar ──
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
		// No gauge, no message — show the summary as a hero value instead
		cardLines = append(cardLines, heroValueStyle.Render(di.summary))
		if di.detail != "" {
			cardLines = append(cardLines, heroLabelStyle.Render(di.detail))
		}
	}

	// ── Timestamp ──
	timeStr := snap.Timestamp.Format("15:04:05")
	age := time.Since(snap.Timestamp)
	if age > 60*time.Second {
		timeStr = fmt.Sprintf("%s (%s ago)", snap.Timestamp.Format("15:04:05"), formatDuration(age))
	}
	cardLines = append(cardLines, dimStyle.Render("⏱ "+timeStr))

	// ── Render the card ──
	cardContent := strings.Join(cardLines, "\n")
	borderColor := StatusBorderColor(snap.Status)
	card := detailHeaderCardStyle.
		Width(innerW + 2). // +2 for padding
		BorderForeground(borderColor).
		Render(cardContent)

	sb.WriteString(card)
	sb.WriteString("\n")
}

// wrapTags arranges tag strings into rows that fit within maxWidth.
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

// ─── Metric Grouping ───────────────────────────────────────────────────────

type metricGroup struct {
	title   string
	entries []metricEntry
	order   int
}

type metricEntry struct {
	key    string
	label  string
	metric core.Metric
}

func groupMetrics(metrics map[string]core.Metric) []metricGroup {
	groups := make(map[string]*metricGroup)

	for key, m := range metrics {
		groupName, label, order := classifyMetric(key, m)
		g, ok := groups[groupName]
		if !ok {
			g = &metricGroup{title: groupName, order: order}
			groups[groupName] = g
		}
		g.entries = append(g.entries, metricEntry{key: key, label: label, metric: m})
	}

	result := make([]metricGroup, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g.entries, func(i, j int) bool {
			return g.entries[i].key < g.entries[j].key
		})
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].order != result[j].order {
			return result[i].order < result[j].order
		}
		return result[i].title < result[j].title
	})

	return result
}

// classifyMetric assigns each metric key + value to one of 4 UNIVERSAL sections.
// This ensures every provider detail page follows the SAME normalized layout:
//
//	Usage → Spending → Tokens → Activity → [Timers] → [Details]
//
// The 4 sections:
//   - Usage:    gauges, rate limits, quota bars, budget progress
//   - Spending: dollar amounts, costs, model cost tables
//   - Tokens:   token counts, per-model token tables
//   - Activity: counters, stats, everything else
func classifyMetric(key string, m core.Metric) (group, label string, order int) {
	lk := strings.ToLower(key)

	switch {
	// ════════════════════════════════════════════════════════════════
	// ── USAGE: gauges, rate limits, quotas, budget progress ────────
	// ════════════════════════════════════════════════════════════════

	// Standard rate limit keys (from response headers)
	case key == "rpm" || key == "tpm" || key == "rpd" || key == "tpd":
		return "Usage", strings.ToUpper(key), 1
	case strings.HasPrefix(key, "rate_limit_"):
		return "Usage", prettifyKey(strings.TrimPrefix(key, "rate_limit_")), 1
	case key == "rpm_headers" || key == "tpm_headers":
		return "Usage", prettifyKey(key), 1
	case key == "gh_api_rpm" || key == "copilot_chat":
		return "Usage", prettifyKey(key), 1

	// Plan percentage gauge
	case key == "plan_percent_used":
		return "Usage", "Plan Used", 1

	// Spend limit (always has a gauge)
	case key == "spend_limit":
		return "Usage", "Spend Limit", 1

	// Plan spend (used/limit → gauge)
	case key == "plan_spend":
		return "Usage", "Plan Spend", 1

	// Monthly spend/budget with limit (Mistral-style gauge)
	case key == "monthly_spend" && m.Limit != nil:
		return "Usage", "Monthly Spend", 1
	case key == "monthly_budget" && m.Limit != nil:
		return "Usage", "Monthly Budget", 1

	// Credits/balance WITH a limit → gauge
	case (key == "credits" || key == "credit_balance") && m.Limit != nil:
		return "Usage", prettifyKey(key), 1

	// Context window (used vs limit → gauge)
	case key == "context_window":
		return "Usage", "Context Window", 1

	// Gemini-style quota (Remaining+Limit, not % or USD)
	case m.Remaining != nil && m.Limit != nil && m.Unit != "%" && m.Unit != "USD":
		return "Usage", prettifyQuotaKey(key), 1

	// ════════════════════════════════════════════════════════════════
	// ── SPENDING: dollar amounts, costs, model cost tables ─────────
	// ════════════════════════════════════════════════════════════════

	// Per-model costs → compact table (exclude token-suffix keys)
	case strings.HasPrefix(key, "model_") &&
		!strings.HasSuffix(key, "_input_tokens") &&
		!strings.HasSuffix(key, "_output_tokens"):
		return "Spending", strings.TrimPrefix(key, "model_"), 2

	// Plan sub-values (informational, no gauge)
	case key == "plan_included" || key == "plan_bonus" ||
		key == "plan_total_spend_usd" || key == "plan_limit_usd":
		return "Spending", prettifyKey(strings.TrimPrefix(key, "plan_")), 2

	// Individual spend
	case key == "individual_spend":
		return "Spending", "Individual Spend", 2

	// Cost / burn-rate metrics
	case strings.Contains(lk, "cost") || strings.Contains(lk, "burn_rate"):
		return "Spending", prettifyKey(key), 2

	// Credits/balance WITHOUT limit → just a value
	case key == "credits" || key == "credit_balance":
		return "Spending", prettifyKey(key), 2

	// Monthly spend/budget without limit
	case key == "monthly_spend" || key == "monthly_budget":
		return "Spending", prettifyKey(key), 2

	// Balance metrics (DeepSeek CNY balances etc.)
	case strings.HasSuffix(key, "_balance"):
		return "Spending", prettifyKey(key), 2

	// ════════════════════════════════════════════════════════════════
	// ── TOKENS: token counts, per-model token tables ───────────────
	// ════════════════════════════════════════════════════════════════

	// Per-model tokens prefix style (Claude: input_tokens_*, output_tokens_*)
	case strings.HasPrefix(key, "input_tokens_") || strings.HasPrefix(key, "output_tokens_"):
		return "Tokens", key, 3

	// Per-model tokens suffix style (OpenRouter: model_*_input_tokens)
	case strings.HasPrefix(key, "model_") &&
		(strings.HasSuffix(key, "_input_tokens") || strings.HasSuffix(key, "_output_tokens")):
		return "Tokens", key, 3

	// Session tokens (Codex)
	case strings.HasPrefix(key, "session_"):
		return "Tokens", prettifyKey(strings.TrimPrefix(key, "session_")), 3

	// Any other token metric
	case strings.Contains(lk, "token"):
		return "Tokens", prettifyKey(key), 3

	// ════════════════════════════════════════════════════════════════
	// ── ACTIVITY: counters, stats, everything else ──────────────────
	// ════════════════════════════════════════════════════════════════

	// Code stats (Cursor tab/composer)
	case strings.HasPrefix(key, "tab_") || strings.HasPrefix(key, "composer_"):
		return "Activity", prettifyKey(key), 4

	// Counters
	case strings.Contains(lk, "message") || strings.Contains(lk, "session") ||
		strings.Contains(lk, "conversation") || strings.Contains(lk, "tool_call") ||
		strings.Contains(lk, "request"):
		return "Activity", prettifyKey(key), 4

	// Catch-all → Activity
	default:
		return "Activity", prettifyKey(key), 4
	}
}

// prettifyQuotaKey formats Gemini-style quota keys like "Gemini-2.5-flash_REQUESTS"
// into "Gemini-2.5-flash REQUESTS" by splitting model name from token type.
func prettifyQuotaKey(key string) string {
	lastUnderscore := strings.LastIndex(key, "_")
	if lastUnderscore > 0 && lastUnderscore < len(key)-1 {
		suffix := key[lastUnderscore+1:]
		prefix := key[:lastUnderscore]
		// If suffix is all-uppercase (token type like REQUESTS), split cleanly
		if suffix == strings.ToUpper(suffix) && len(suffix) > 1 {
			return prefix + " " + suffix
		}
	}
	return prettifyKey(key)
}

// ─── Metric Group Rendering ─────────────────────────────────────────────────
//
// Each of the 4 universal sections gets a dedicated renderer that handles
// its sub-types. This ensures EVERY provider page looks identical in structure.

func renderMetricGroup(sb *strings.Builder, group metricGroup, w int, warnThresh, critThresh float64) {
	sb.WriteString("\n")
	renderDetailSectionHeader(sb, group.title, w)

	switch group.title {
	case "Usage":
		renderUsageSection(sb, group.entries, w, warnThresh, critThresh)
	case "Spending":
		renderSpendingSection(sb, group.entries, w)
	case "Tokens":
		renderTokensSection(sb, group.entries, w)
	case "Activity":
		renderActivitySection(sb, group.entries, w)
	}
}

// ── Usage Section ────────────────────────────────────────────────────────────
// Renders gauges for rate limits, plan usage, spend limits, and quotas.
// Regular metrics get individual gauge lines; quota-style entries (many
// per-model buckets) get a compact table.

func renderUsageSection(sb *strings.Builder, entries []metricEntry, w int, warnThresh, critThresh float64) {
	labelW := sectionLabelWidth(w)

	var quotaEntries []metricEntry
	var gaugeEntries []metricEntry

	for _, e := range entries {
		m := e.metric
		// Quota-style: Remaining+Limit, non-% non-USD (Gemini per-model buckets)
		if m.Remaining != nil && m.Limit != nil && m.Unit != "%" && m.Unit != "USD" {
			quotaEntries = append(quotaEntries, e)
		} else {
			gaugeEntries = append(gaugeEntries, e)
		}
	}

	// Render standard gauge entries
	for _, entry := range gaugeEntries {
		renderGaugeEntry(sb, entry, labelW, w, warnThresh, critThresh)
	}

	// Render quota entries as a compact table (better for many models)
	if len(quotaEntries) > 0 {
		if len(gaugeEntries) > 0 {
			sb.WriteString("\n")
		}
		renderQuotaTable(sb, quotaEntries, w, warnThresh, critThresh)
	}
}

// ── Spending Section ─────────────────────────────────────────────────────────
// Renders dollar values, with model costs in a compact table.

func renderSpendingSection(sb *strings.Builder, entries []metricEntry, w int) {
	labelW := sectionLabelWidth(w)

	var modelCosts []metricEntry
	var otherCosts []metricEntry

	for _, e := range entries {
		if isModelCostKey(e.key) {
			modelCosts = append(modelCosts, e)
		} else {
			otherCosts = append(otherCosts, e)
		}
	}

	// Render plain cost values
	for _, e := range otherCosts {
		val := formatMetricValue(e.metric)
		vs := metricValueStyle // dollar amounts get accent color
		if !strings.Contains(val, "$") && !strings.Contains(val, "USD") {
			vs = valueStyle
		}
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(labelW).Render(e.label), vs.Render(val)))
	}

	// Render model costs as a compact table
	if len(modelCosts) > 0 {
		if len(otherCosts) > 0 {
			sb.WriteString("\n")
		}
		renderModelCostsTable(sb, modelCosts, w)
	}
}

// ── Tokens Section ───────────────────────────────────────────────────────────
// Renders per-model token tables and plain token values.

func renderTokensSection(sb *strings.Builder, entries []metricEntry, w int) {
	labelW := sectionLabelWidth(w)

	var perModelTokens []metricEntry
	var otherTokens []metricEntry

	for _, e := range entries {
		if isPerModelTokenKey(e.key) {
			perModelTokens = append(perModelTokens, e)
		} else {
			otherTokens = append(otherTokens, e)
		}
	}

	// Render plain token values first
	for _, e := range otherTokens {
		val := formatMetricValue(e.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(labelW).Render(e.label), valueStyle.Render(val)))
	}

	// Render per-model tokens as a compact table
	if len(perModelTokens) > 0 {
		if len(otherTokens) > 0 {
			sb.WriteString("\n")
		}
		renderTokenUsageTable(sb, perModelTokens, w)
	}
}

// ── Activity Section ─────────────────────────────────────────────────────────
// Simple label → value pairs for counters and stats.

func renderActivitySection(sb *strings.Builder, entries []metricEntry, w int) {
	labelW := sectionLabelWidth(w)

	for _, e := range entries {
		val := formatMetricValue(e.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(labelW).Render(e.label), valueStyle.Render(val)))
	}
}

// ── Shared helpers ───────────────────────────────────────────────────────────

// sectionLabelWidth returns a consistent label width for the given terminal width.
func sectionLabelWidth(w int) int {
	switch {
	case w < 45:
		return 14
	case w < 55:
		return 18
	default:
		return 22
	}
}

// renderGaugeEntry renders a single metric as a gauge line + detail line.
func renderGaugeEntry(sb *strings.Builder, entry metricEntry, labelW, w int, warnThresh, critThresh float64) {
	m := entry.metric
	labelRendered := labelStyle.Width(labelW).Render(entry.label)
	gaugeW := min(24, w-labelW-10)
	if gaugeW < 8 {
		gaugeW = 8
	}

	// Percentage-unit metrics → usage gauge (fills up, green→red)
	if m.Unit == "%" && m.Used != nil {
		gauge := RenderUsageGauge(*m.Used, gaugeW, warnThresh, critThresh)
		sb.WriteString(fmt.Sprintf("  %s %s\n", labelRendered, gauge))
		if detail := formatUsageDetail(m); detail != "" {
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				strings.Repeat(" ", labelW+2), dimStyle.Render(detail)))
		}
		return
	}

	// Remaining gauge (green when full, red when empty)
	if pct := m.Percent(); pct >= 0 {
		gauge := RenderGauge(pct, gaugeW, warnThresh, critThresh)
		sb.WriteString(fmt.Sprintf("  %s %s\n", labelRendered, gauge))
		if detail := formatMetricDetail(m); detail != "" {
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				strings.Repeat(" ", labelW+2), dimStyle.Render(detail)))
		}
		return
	}

	// No gauge — label + value
	val := formatMetricValue(m)
	vs := valueStyle
	if strings.Contains(val, "$") || strings.Contains(val, "USD") {
		vs = metricValueStyle
	}
	sb.WriteString(fmt.Sprintf("  %s %s\n", labelRendered, vs.Render(val)))
}

// isModelCostKey returns true if the key represents a per-model cost metric.
func isModelCostKey(key string) bool {
	return strings.HasPrefix(key, "model_") &&
		(strings.HasSuffix(key, "_cost") || strings.HasSuffix(key, "_cost_usd"))
}

// isPerModelTokenKey returns true if the key represents per-model token data.
func isPerModelTokenKey(key string) bool {
	if strings.HasPrefix(key, "input_tokens_") || strings.HasPrefix(key, "output_tokens_") {
		return true
	}
	if strings.HasPrefix(key, "model_") &&
		(strings.HasSuffix(key, "_input_tokens") || strings.HasSuffix(key, "_output_tokens")) {
		return true
	}
	return false
}

// formatMetricValue returns a formatted value string with window annotation.
func formatMetricValue(m core.Metric) string {
	var value string
	switch {
	case m.Used != nil && m.Limit != nil:
		value = fmt.Sprintf("%s / %s %s",
			formatNumber(*m.Used), formatNumber(*m.Limit), m.Unit)
	case m.Remaining != nil && m.Limit != nil:
		value = fmt.Sprintf("%s / %s %s remaining",
			formatNumber(*m.Remaining), formatNumber(*m.Limit), m.Unit)
	case m.Used != nil:
		value = fmt.Sprintf("%s %s", formatNumber(*m.Used), m.Unit)
	case m.Remaining != nil:
		value = fmt.Sprintf("%s %s remaining", formatNumber(*m.Remaining), m.Unit)
	}

	if m.Window != "" && m.Window != "all_time" && m.Window != "current_period" {
		value += " " + dimStyle.Render("["+m.Window+"]")
	}
	return value
}

// ─── Specialized Table Renderers ─────────────────────────────────────────────
//
// Each major metric category gets a compact, purpose-built table renderer
// to ensure visual consistency across ALL providers.

// renderModelCostsTable renders cursor-style per-model cost breakdown as a compact table.
//
//	Model                          Cost
//	Claude-4.5-opus-high-thinking  $171.10
//	Default                         $49.06
//	Claude-4.5-sonnet-thinking      $28.79
func renderModelCostsTable(sb *strings.Builder, entries []metricEntry, w int) {
	type modelCost struct {
		name    string
		cost    float64
		window  string
		hasData bool
	}

	var models []modelCost
	var unmatched []metricEntry

	for _, e := range entries {
		label := e.label
		// Try to extract model name from different suffixes
		var modelName string
		switch {
		case strings.HasSuffix(label, "_cost"):
			modelName = strings.TrimSuffix(label, "_cost")
		case strings.HasSuffix(label, "_cost_usd"):
			modelName = strings.TrimSuffix(label, "_cost_usd")
		default:
			unmatched = append(unmatched, e)
			continue
		}

		cost := float64(0)
		if e.metric.Used != nil {
			cost = *e.metric.Used
		}
		models = append(models, modelCost{
			name:    prettifyModelName(modelName),
			cost:    cost,
			window:  e.metric.Window,
			hasData: true,
		})
	}

	// Sort by cost descending
	sort.Slice(models, func(i, j int) bool {
		return models[i].cost > models[j].cost
	})

	if len(models) > 0 {
		nameW := 28
		if w < 55 {
			nameW = 20
		}

		// Window annotation (same for all, show once)
		windowHint := ""
		if len(models) > 0 && models[0].window != "" &&
			models[0].window != "all_time" && models[0].window != "current_period" {
			windowHint = " " + dimStyle.Render("["+models[0].window+"]")
		}

		// Header
		sb.WriteString(fmt.Sprintf("  %-*s %10s%s\n",
			nameW, dimStyle.Bold(true).Render("Model"),
			dimStyle.Bold(true).Render("Cost"),
			windowHint,
		))

		for _, mc := range models {
			name := mc.name
			if len(name) > nameW {
				name = name[:nameW-1] + "…"
			}
			costStr := formatUSD(mc.cost)
			costStyle := tealStyle
			if mc.cost >= 10 {
				costStyle = metricValueStyle
			}
			sb.WriteString(fmt.Sprintf("  %-*s %10s\n",
				nameW, valueStyle.Render(name),
				costStyle.Render(costStr),
			))
		}
	}

	// Render any unmatched entries as plain metrics
	for _, e := range unmatched {
		val := formatMetricValue(e.metric)
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Width(22).Render(prettifyModelName(e.label)),
			valueStyle.Render(val),
		))
	}
}

// renderTokenUsageTable renders per-model token breakdowns as a compact table.
// Handles both prefix-style keys (Claude: input_tokens_*) and suffix-style
// keys (OpenRouter: model_*_input_tokens).
//
//	Model                   Input       Output
//	Claude Opus 4 5          41.7K       42.0K
//	Claude Opus 4 6         125.8K      136.0K
func renderTokenUsageTable(sb *strings.Builder, entries []metricEntry, w int) {
	type tokenData struct {
		name         string
		inputTokens  float64
		outputTokens float64
	}

	models := make(map[string]*tokenData)
	var modelOrder []string

	for _, e := range entries {
		key := e.key // use the raw metric key for pattern matching
		var modelName string
		var isInput bool

		switch {
		// Prefix style: input_tokens_claude_opus_4_6
		case strings.HasPrefix(key, "input_tokens_"):
			modelName = strings.TrimPrefix(key, "input_tokens_")
			isInput = true
		case strings.HasPrefix(key, "output_tokens_"):
			modelName = strings.TrimPrefix(key, "output_tokens_")
			isInput = false
		// Suffix style: model_anthropic_claude-3.5-sonnet_input_tokens
		case strings.HasSuffix(key, "_input_tokens"):
			modelName = strings.TrimPrefix(
				strings.TrimSuffix(key, "_input_tokens"), "model_")
			isInput = true
		case strings.HasSuffix(key, "_output_tokens"):
			modelName = strings.TrimPrefix(
				strings.TrimSuffix(key, "_output_tokens"), "model_")
			isInput = false
		default:
			continue
		}

		md, ok := models[modelName]
		if !ok {
			md = &tokenData{name: modelName}
			models[modelName] = md
			modelOrder = append(modelOrder, modelName)
		}
		if e.metric.Used != nil {
			if isInput {
				md.inputTokens = *e.metric.Used
			} else {
				md.outputTokens = *e.metric.Used
			}
		}
	}

	if len(modelOrder) == 0 {
		return
	}

	nameW := 26
	colW := 10
	if w < 55 {
		nameW = 18
		colW = 8
	}

	// Header
	sb.WriteString(fmt.Sprintf("  %-*s %*s %*s\n",
		nameW, dimStyle.Bold(true).Render("Model"),
		colW, dimStyle.Bold(true).Render("Input"),
		colW, dimStyle.Bold(true).Render("Output"),
	))

	for _, name := range modelOrder {
		md := models[name]
		displayName := prettifyModelName(md.name)
		if len(displayName) > nameW {
			displayName = displayName[:nameW-1] + "…"
		}
		sb.WriteString(fmt.Sprintf("  %-*s %*s %*s\n",
			nameW, valueStyle.Render(displayName),
			colW, lipgloss.NewStyle().Foreground(colorSubtext).Render(formatTokens(md.inputTokens)),
			colW, lipgloss.NewStyle().Foreground(colorSubtext).Render(formatTokens(md.outputTokens)),
		))
	}
}

// renderQuotaTable renders Gemini-style per-model quota remaining as a compact table.
//
//	Model                        Remaining         Window
//	Gemini-2.0-flash REQUESTS   ━━━━━━━━ 99.8%   [15h10m]
//	Gemini-2.5-pro REQUESTS     ━━━━━━━━ 100.0%  [23h59m]
func renderQuotaTable(sb *strings.Builder, entries []metricEntry, w int, warnThresh, critThresh float64) {
	if len(entries) == 0 {
		return
	}

	// Sort entries: by remaining % ascending (worst first)
	sort.Slice(entries, func(i, j int) bool {
		pi := entries[i].metric.Percent()
		pj := entries[j].metric.Percent()
		if pi < 0 {
			pi = 200
		}
		if pj < 0 {
			pj = 200
		}
		return pi < pj
	})

	nameW := 30
	gaugeW := 10
	if w < 65 {
		nameW = 22
		gaugeW = 8
	}
	if w < 50 {
		nameW = 16
		gaugeW = 6
	}

	for _, entry := range entries {
		m := entry.metric
		name := entry.label
		if len(name) > nameW {
			name = name[:nameW-1] + "…"
		}

		pct := m.Percent()
		gauge := ""
		pctStr := ""
		if pct >= 0 {
			gauge = RenderMiniGauge(pct, gaugeW)
			var color lipgloss.Color
			switch {
			case pct <= critThresh*100:
				color = colorCrit
			case pct <= warnThresh*100:
				color = colorWarn
			default:
				color = colorOK
			}
			pctStr = lipgloss.NewStyle().Foreground(color).Bold(true).Render(fmt.Sprintf("%5.1f%%", pct))
		}

		windowStr := ""
		if m.Window != "" && m.Window != "all_time" && m.Window != "current_period" {
			windowStr = dimStyle.Render(" [" + m.Window + "]")
		}

		sb.WriteString(fmt.Sprintf("  %-*s %s %s%s\n",
			nameW, labelStyle.Render(name),
			gauge, pctStr, windowStr,
		))
	}
}

// ─── Raw Data ───────────────────────────────────────────────────────────────

func renderRawData(sb *strings.Builder, raw map[string]string, w int) {
	// Consistent label width matching the rest of the page.
	labelW := 22
	if w < 55 {
		labelW = 18
	}

	// Priority keys shown first with accent styling.
	priority := []string{
		"account_email", "account_name", "plan_name", "plan_type", "plan_price",
		"membership_type", "team_membership", "organization_name",
		"billing_cycle_start", "billing_cycle_end",
		"subscription_status", "cli_version", "credits", "oauth_status",
	}
	for _, key := range priority {
		if v, ok := raw[key]; ok && v != "" {
			sb.WriteString(fmt.Sprintf("  %s  %s\n",
				labelStyle.Width(labelW).Render(prettifyKey(key)),
				valueStyle.Render(smartFormatValue(v)),
			))
		}
	}

	// Then remaining keys sorted alphabetically.
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	isPriority := func(k string) bool {
		for _, pk := range priority {
			if k == pk {
				return true
			}
		}
		return false
	}

	// Skip overly long values (pricing_summary, etc.) and error keys
	isVerbose := func(k string) bool {
		return k == "pricing_summary" || strings.HasSuffix(k, "_error")
	}

	shown := 0
	for _, k := range keys {
		if isPriority(k) || isVerbose(k) {
			continue
		}
		if shown >= 20 {
			remaining := 0
			for _, k2 := range keys {
				if !isPriority(k2) && !isVerbose(k2) {
					remaining++
				}
			}
			remaining -= shown
			if remaining > 0 {
				sb.WriteString(dimStyle.Render(fmt.Sprintf("  … and %d more\n", remaining)))
			}
			break
		}
		v := smartFormatValue(raw[k])
		if len(v) > 55 {
			v = v[:52] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			labelStyle.Width(labelW).Render(prettifyKey(k)),
			dimStyle.Render(v),
		))
		shown++
	}
}

// smartFormatValue attempts to detect and format timestamps and other values.
func smartFormatValue(v string) string {
	trimmed := strings.TrimSpace(v)

	// Try epoch millis (13+ digits)
	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 1e12 && n < 2e13 {
		t := time.Unix(n/1000, 0)
		return t.Format("Jan 02, 2006 15:04")
	}

	// Try epoch secs (10 digits)
	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 1e9 && n < 2e10 {
		t := time.Unix(n, 0)
		return t.Format("Jan 02, 2006 15:04")
	}

	return v
}

// ─── Section Header ─────────────────────────────────────────────────────────

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

// sectionIcon returns a small icon for the 4 universal sections + system sections.
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
	default:
		return "›"
	}
}

// sectionColor returns the accent color for a section header.
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
	default:
		return colorBlue
	}
}

// ─── Formatting Helpers ─────────────────────────────────────────────────────

// formatUsageDetail returns a human-readable detail for "percent used" metrics.
// e.g. "25% remaining [7d]" instead of the confusing "75 / 100 %".
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

// formatMetricDetail returns a human-readable value string for a metric.
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
	parts := strings.Split(key, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	result := strings.Join(parts, " ")
	// Common abbreviation fixes
	for _, pair := range [][2]string{
		{"Usd", "USD"}, {"Rpm", "RPM"}, {"Tpm", "TPM"},
		{"Rpd", "RPD"}, {"Tpd", "TPD"}, {"Api", "API"},
	} {
		result = strings.ReplaceAll(result, pair[0], pair[1])
	}
	return result
}

func prettifyModelName(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}
