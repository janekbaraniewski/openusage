package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

func (m Model) buildTileGaugeLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int) []string {
	if snap.ProviderID == "antigravity" {
		return m.buildAntigravityTileGaugeLines(snap, innerW)
	}
	if snap.ProviderID == "opencode" {
		if ocLines := m.buildOpenCodeTileGaugeLines(snap, innerW); len(ocLines) > 0 {
			return ocLines
		}
	}
	if snap.ProviderID == "command_code" {
		if ccLines := m.buildCommandCodeTileGaugeLines(snap, innerW); len(ccLines) > 0 {
			return ccLines
		}
	}
	if snap.ProviderID == "cursor" {
		if curLines := m.buildCursorTileGaugeLines(snap, innerW); len(curLines) > 0 {
			return curLines
		}
	}

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

	keys := core.SortedStringKeys(snap.Metrics)
	keys = prioritizeMetricKeys(keys, widget.GaugePriority)

	// When GaugePriority is set, treat it as an allowlist — only those
	// metrics are eligible for gauge rendering.
	var gaugeAllowSet map[string]bool
	if len(widget.GaugePriority) > 0 {
		gaugeAllowSet = lo.SliceToMap(widget.GaugePriority, func(k string) (string, bool) {
			return k, true
		})
	}

	now := m.viewNow()
	annotationIndent := strings.Repeat(" ", maxLabelW+1)

	var lines []string
	renderedGauges := 0
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

		var gauge string
		if m.isUsageModeUsed() {
			gauge = RenderUsageGauge(usedPct, gaugeW, m.warnThreshold, m.critThreshold)
			if sgCfg, ok := widget.StackedGaugeKeys[key]; ok && len(sgCfg.SegmentMetricKeys) > 0 {
				segments := buildStackedSegments(snap, sgCfg, met)
				if len(segments) > 0 {
					gauge = RenderStackedUsageGauge(segments, usedPct, gaugeW)
				}
			}
		} else {
			remainingPct := 100 - usedPct
			if met.Remaining != nil && (met.Limit == nil || *met.Limit <= 0) {
				remainingPct = *met.Remaining
			} else if met.Limit != nil && met.Remaining != nil && *met.Limit > 0 {
				remainingPct = *met.Remaining / *met.Limit * 100
			}
			if remainingPct < 0 {
				remainingPct = 0
			}
			if remainingPct > 100 {
				remainingPct = 100
			}
			gauge = RenderGauge(remainingPct, gaugeW, m.warnThreshold, m.critThreshold)
		}

		labelR := lipgloss.NewStyle().Foreground(colorSubtext).Width(maxLabelW).Render(label)
		lines = append(lines, labelR+" "+gauge)

		// Append a dim projection annotation when the metric has a
		// recognized window + a reset timestamp. Pace mirrors the detail
		// view computation (current% / elapsed minutes / 100).
		if annot := tileGaugeProjectionAnnotation(snap, key, met, usedPct, now); annot != "" {
			lines = append(lines, annotationIndent+dimStyle.Render(annot))
		}

		renderedGauges++
		if maxLines > 0 && renderedGauges >= maxLines {
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

func gaugeLabelWithMode(widget core.DashboardWidget, key string, isUsed bool, window ...string) string {
	if !isUsed && key == "plan_percent_used" {
		return "Plan Remaining"
	}
	return gaugeLabel(widget, key, window...)
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

// tileGaugeProjectionAnnotation returns a compact annotation string suitable
// for the dashboard tile gauge (no leading indent or styling applied). It
// returns "" when neither a reset countdown nor a meaningful pace projection
// is available, so the caller can skip the line entirely.
//
// Returned forms (always dim-styled by caller):
//   - "resets 1h 23m · 100% in 42m"     (pace lands inside the window)
//   - "resets 3h 42m · ~85% by reset"   (pace would overshoot the window)
//   - "resets 1h 23m"                   (no pace yet, e.g. usedPct == 0 or no elapsed time)
//   - "100% in 42m"                     (pace known but no reset timestamp)
//   - ""                                 (nothing meaningful to show)
func tileGaugeProjectionAnnotation(snap core.UsageSnapshot, key string, met core.Metric, usedPct float64, now time.Time) string {
	if key == "codex_credit_percent_used" {
		return tileCodexCreditProjectionAnnotation(snap, usedPct, now)
	}

	windowDur, ok := gaugeWindowDuration(met.Window)
	if !ok {
		return ""
	}
	// Providers store the reset timestamp under either the bare metric key
	// (claude_code: snap.Resets["usage_five_hour"]) or a "_reset"-suffixed
	// key (copilot, opencode: snap.Resets["rolling_usage_reset"]) — both are
	// established conventions in this codebase, so check both.
	resetAt, hasReset := snap.Resets[key]
	if !hasReset {
		resetAt, hasReset = snap.Resets[key+"_reset"]
	}
	if !hasReset || resetAt.IsZero() {
		return ""
	}
	resetIn := resetAt.Sub(now)

	var resetPart string
	if resetIn > 0 {
		resetPart = "resets " + formatDurationShort(resetIn)
	}

	var projPart string
	if usedPct > 0 && usedPct < 100 {
		elapsed := windowDur - resetIn
		if elapsed > 0 {
			elapsedMin := elapsed.Minutes()
			if elapsedMin > 0 {
				paceFraction := (usedPct / 100) / elapsedMin
				if !math.IsNaN(paceFraction) && !math.IsInf(paceFraction, 0) && paceFraction > 0 {
					pctPerMinute := paceFraction * 100
					if pctPerMinute > 0 {
						remainingPct := 100 - usedPct
						minutesTo100 := remainingPct / pctPerMinute
						d := time.Duration(minutesTo100 * float64(time.Minute))
						if d > 0 {
							// If we would not reach 100% before reset,
							// surface the projected % at reset instead.
							if resetIn > 0 && d > resetIn {
								projectedPct := usedPct + pctPerMinute*resetIn.Minutes()
								n := int(math.Round(projectedPct))
								if n < 0 {
									n = 0
								}
								if n >= 100 {
									n = 99
								}
								projPart = fmt.Sprintf("~%d%% by reset", n)
							} else {
								projPart = "100% in " + formatDurationShort(d)
							}
						}
					}
				}
			}
		}
	}

	return joinAnnotationParts(resetPart, projPart)
}

func tileCodexCreditProjectionAnnotation(snap core.UsageSnapshot, usedPct float64, now time.Time) string {
	resetAt, hasReset := snap.Resets["codex_credit_limit"]
	if !hasReset {
		return ""
	}

	resetIn := resetAt.Sub(now)
	resetPart := ""
	if resetIn > 0 {
		resetPart = "resets " + formatDurationShort(resetIn)
	}

	rateMetric, hasRate := snap.Metrics["codex_credit_burn_rate"]
	creditMetric, hasCredits := snap.Metrics["codex_credit_limit"]
	if !hasRate || !hasCredits || rateMetric.Used == nil || creditMetric.Limit == nil || *rateMetric.Used <= 0 || *creditMetric.Limit <= 0 || usedPct >= 100 {
		return resetPart
	}

	// Convert the credit burn rate into percentage points per hour using the
	// authoritative current-period credit limit.
	pctPerHour := *rateMetric.Used / *creditMetric.Limit * 100
	if pctPerHour <= 0 {
		return resetPart
	}
	remainingPct := 100 - usedPct
	hoursTo100 := remainingPct / pctPerHour
	if hoursTo100 <= 0 {
		return resetPart
	}

	var projection string
	if resetIn > 0 && time.Duration(hoursTo100*float64(time.Hour)) > resetIn {
		projectedPct := usedPct + pctPerHour*resetIn.Hours()
		projected := int(math.Round(projectedPct))
		if projected < 0 {
			projected = 0
		}
		if projected >= 100 {
			projected = 99
		}
		projection = fmt.Sprintf("~%d%% by reset", projected)
	} else {
		projection = "100% in " + formatDurationShort(time.Duration(hoursTo100*float64(time.Hour)))
	}

	return joinAnnotationParts(resetPart, projection)
}

func (m Model) buildAntigravityTileGaugeLines(snap core.UsageSnapshot, innerW int) []string {
	var lines []string

	barW := innerW - 14
	if barW < 12 {
		barW = 12
	}
	if barW > 50 {
		barW = 50
	}

	now := m.viewNow()
	isUsed := m.isUsageModeUsed()

	if planTitle := antigravityPlanTitle(snap); planTitle != "" {
		bullet := lipgloss.NewStyle().Bold(true).Foreground(colorMauve).Render("◈ ")
		title := lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(planTitle)
		lines = append(lines, bullet+title)
		lines = append(lines, "")
	}

	renderBlock := func(groupTitle string, modelsDesc string, weeklyKeys []string, fiveHourKeys []string) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		bullet := lipgloss.NewStyle().Bold(true).Foreground(colorMauve).Render("◈ ")
		title := lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(groupTitle)
		lines = append(lines, bullet+title)
		lines = append(lines, "")

		renderItem := func(label string, candidateKeys []string, defaultRemaining float64) {
			var met core.Metric
			found := false
			matchedKey := ""
			for _, k := range candidateKeys {
				if m, ok := snap.Metrics[k]; ok && m.Remaining != nil {
					met = m
					found = true
					matchedKey = k
					break
				}
			}

			remaining := defaultRemaining
			if found && met.Remaining != nil {
				remaining = *met.Remaining
			}

			var resetAt time.Time
			if matchedKey != "" {
				if r, hasReset := snap.Resets[matchedKey]; hasReset {
					resetAt = r
				} else if r, hasReset := snap.Resets[matchedKey+"_reset"]; hasReset {
					resetAt = r
				}
			}
			if resetAt.IsZero() {
				for _, k := range candidateKeys {
					if r, hasReset := snap.Resets[k]; hasReset && !r.IsZero() {
						resetAt = r
						break
					} else if r, hasReset := snap.Resets[k+"_reset"]; hasReset && !r.IsZero() {
						resetAt = r
						break
					}
				}
			}
			if !resetAt.IsZero() && resetAt.Before(now) {
				if strings.Contains(label, "Weekly") {
					for resetAt.Before(now) {
						resetAt = resetAt.Add(7 * 24 * time.Hour)
					}
				} else if strings.Contains(label, "Five Hour") {
					for resetAt.Before(now) {
						resetAt = resetAt.Add(5 * time.Hour)
					}
				}
			}

			var gaugeBar string
			if isUsed {
				gaugeBar = RenderUsageGauge(100-remaining, barW, m.warnThreshold, m.critThreshold)
			} else {
				gaugeBar = RenderGauge(remaining, barW, m.warnThreshold, m.critThreshold)
			}
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorSubtext).Render(label))
			lines = append(lines, "    "+gaugeBar)
			lines = append(lines, "    "+RenderQuotaStatusAndTimerLineWithMode(remaining, resetAt, now, isUsed))
			lines = append(lines, "")
		}

		weeklyRemaining := 100.0
		for _, k := range weeklyKeys {
			if m, ok := snap.Metrics[k]; ok && m.Remaining != nil {
				weeklyRemaining = *m.Remaining
				break
			}
		}

		fiveHourDefault := 100.0
		if weeklyRemaining <= 0 {
			fiveHourDefault = 0.0
		}

		if isUsed {
			renderItem("Weekly Limit Used", weeklyKeys, weeklyRemaining)
			renderItem("Five Hour Limit Used", fiveHourKeys, fiveHourDefault)
		} else {
			renderItem("Weekly Limit Remaining", weeklyKeys, weeklyRemaining)
			renderItem("Five Hour Limit Remaining", fiveHourKeys, fiveHourDefault)
		}
	}

	// 1. GEMINI MODELS
	renderBlock(
		"GEMINI MODELS",
		"Models within this group: Gemini 2.5 Flash, Gemini 2.5 Pro, Gemini 3.7 Flash",
		[]string{"quota_gemini_weekly", "quota_gemini_7d", "quota_gemini", "quota_gemini_flash", "quota_gemini_pro"},
		[]string{"quota_gemini_5h", "quota_gemini", "quota_gemini_flash", "quota_gemini_pro"},
	)

	// 2. CLAUDE AND GPT MODELS
	renderBlock(
		"CLAUDE AND GPT MODELS",
		"Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
		[]string{"quota_claude_weekly", "quota_3p_weekly", "quota_3p_7d", "quota_opus_sonnet_weekly", "quota_claude", "quota_3p", "quota_opus_sonnet"},
		[]string{"quota_claude_5h", "quota_3p_5h", "quota_opus_sonnet_5h", "quota_claude", "quota_3p", "quota_opus_sonnet"},
	)

	return lines
}

func (m Model) buildOpenCodeTileGaugeLines(snap core.UsageSnapshot, innerW int) []string {
	var lines []string

	barW := innerW - 14
	if barW < 12 {
		barW = 12
	}
	if barW > 50 {
		barW = 50
	}

	now := m.viewNow()
	isUsed := m.isUsageModeUsed()

	hasGoMetrics := false
	for _, k := range []string{"rolling_usage", "weekly_usage", "monthly_usage_pct"} {
		if _, ok := snap.Metrics[k]; ok {
			hasGoMetrics = true
			break
		}
	}

	if !hasGoMetrics {
		return nil
	}

	bullet := lipgloss.NewStyle().Bold(true).Foreground(colorBlue).Render("◈ ")
	title := lipgloss.NewStyle().Bold(true).Foreground(colorText).Render("OPENCODE GO SUBSCRIPTION")
	lines = append(lines, bullet+title)
	lines = append(lines, "")

	renderItem := func(label string, metricKey string) {
		met, ok := snap.Metrics[metricKey]
		if !ok {
			return
		}

		remaining := 0.0
		if met.Remaining != nil {
			remaining = *met.Remaining
		} else if met.Used != nil {
			remaining = 100 - *met.Used
		}
		if remaining < 0 {
			remaining = 0
		}
		if remaining > 100 {
			remaining = 100
		}

		var resetAt time.Time
		if r, hasReset := snap.Resets[metricKey]; hasReset {
			resetAt = r
		} else if r, hasReset := snap.Resets[metricKey+"_reset"]; hasReset {
			resetAt = r
		} else if r, hasReset := snap.Resets[strings.TrimSuffix(metricKey, "_pct")]; hasReset {
			resetAt = r
		} else if r, hasReset := snap.Resets[strings.TrimSuffix(metricKey, "_pct")+"_reset"]; hasReset {
			resetAt = r
		}

		var gaugeBar string
		if isUsed {
			gaugeBar = RenderUsageGauge(100-remaining, barW, m.warnThreshold, m.critThreshold)
		} else {
			gaugeBar = RenderGauge(remaining, barW, m.warnThreshold, m.critThreshold)
		}
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorSubtext).Render(label))
		lines = append(lines, "    "+gaugeBar)
		lines = append(lines, "    "+RenderQuotaStatusAndTimerLineWithMode(remaining, resetAt, now, isUsed))
		lines = append(lines, "")
	}

	if isUsed {
		renderItem("Five Hour Limit Used", "rolling_usage")
		renderItem("Weekly Limit Used", "weekly_usage")
		renderItem("Monthly Limit Used", "monthly_usage_pct")
	} else {
		renderItem("Five Hour Limit Remaining", "rolling_usage")
		renderItem("Weekly Limit Remaining", "weekly_usage")
		renderItem("Monthly Limit Remaining", "monthly_usage_pct")
	}

	return lines
}

func (m Model) buildCommandCodeTileGaugeLines(snap core.UsageSnapshot, innerW int) []string {
	var lines []string

	barW := innerW - 14
	if barW < 12 {
		barW = 12
	}
	if barW > 50 {
		barW = 50
	}

	now := m.viewNow()
	isUsed := m.isUsageModeUsed()

	planName := "Command Code"
	if pn := snap.Attributes["plan_name"]; pn != "" {
		planName = fmt.Sprintf("Command Code (%s)", pn)
	} else if planID := snap.Attributes["plan_id"]; planID != "" {
		cleaned := strings.TrimPrefix(planID, "individual-")
		cleaned = strings.TrimPrefix(cleaned, "teams-")
		planName = fmt.Sprintf("Command Code (%s)", strings.ToUpper(cleaned))
	}

	bullet := lipgloss.NewStyle().Bold(true).Foreground(colorTeal).Render("◈ ")
	title := lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(strings.ToUpper(planName))
	lines = append(lines, bullet+title)

	var subtitles []string
	if monthlyCap := snap.Attributes["monthly_cap"]; monthlyCap != "" {
		subtitles = append(subtitles, fmt.Sprintf("Plan: %s/mo", monthlyCap))
	}
	if weeklyCap := snap.Attributes["weekly_cap"]; weeklyCap != "" {
		subtitles = append(subtitles, fmt.Sprintf("Sliding Cap: %s/wk", weeklyCap))
	}
	if len(subtitles) > 0 {
		lines = append(lines, "  "+dimStyle.Render(strings.Join(subtitles, " · ")))
	}
	lines = append(lines, "")

	renderItem := func(label string, metricKey string, capAttr string, usedAttr string) {
		met, ok := snap.Metrics[metricKey]
		if !ok {
			return
		}

		remaining := 0.0
		if met.Remaining != nil {
			remaining = *met.Remaining
		} else if met.Used != nil {
			remaining = 100 - *met.Used
		}
		if remaining < 0 {
			remaining = 0
		}
		if remaining > 100 {
			remaining = 100
		}

		var resetAt time.Time
		if r, hasReset := snap.Resets[metricKey]; hasReset {
			resetAt = r
		} else if r, hasReset := snap.Resets[metricKey+"_reset"]; hasReset {
			resetAt = r
		} else if metricKey == "monthly_subscription" {
			if r, hasReset := snap.Resets["billing_cycle_end"]; hasReset {
				resetAt = r
			} else if r, hasReset := snap.Resets["billing_period"]; hasReset {
				resetAt = r
			}
		}

		capInfo := ""
		if capVal := snap.Attributes[capAttr]; capVal != "" {
			if usedVal := snap.Attributes[usedAttr]; usedVal != "" {
				capInfo = fmt.Sprintf(" (%s / %s used)", usedVal, capVal)
			} else {
				capInfo = fmt.Sprintf(" (%s cap)", capVal)
			}
		}

		var gaugeBar string
		if isUsed {
			gaugeBar = RenderUsageGauge(100-remaining, barW, m.warnThreshold, m.critThreshold)
		} else {
			gaugeBar = RenderGauge(remaining, barW, m.warnThreshold, m.critThreshold)
		}
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorSubtext).Render(label+capInfo))
		lines = append(lines, "    "+gaugeBar)
		lines = append(lines, "    "+RenderQuotaStatusAndTimerLineWithMode(remaining, resetAt, now, isUsed))
		lines = append(lines, "")
	}

	if _, ok := snap.Metrics["monthly_subscription"]; ok {
		if isUsed {
			renderItem("Monthly Subscription Used", "monthly_subscription", "monthly_cap", "monthly_used")
		} else {
			renderItem("Monthly Subscription Remaining", "monthly_subscription", "monthly_cap", "monthly_used")
		}
	}

	if isUsed {
		renderItem("Weekly Limit Used", "weekly_usage", "weekly_cap", "weekly_used")
		renderItem("Five Hour Limit Used", "five_hour_usage", "five_hour_cap", "five_hour_used")
	} else {
		renderItem("Weekly Limit Remaining", "weekly_usage", "weekly_cap", "weekly_used")
		renderItem("Five Hour Limit Remaining", "five_hour_usage", "five_hour_cap", "five_hour_used")
	}

	if bal, ok := snap.Metrics["balance"]; ok && bal.Remaining != nil {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorSubtext).Render("Credit Balance"))
		lines = append(lines, "    "+lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(fmt.Sprintf("$%.2f monthly balance", *bal.Remaining)))
		lines = append(lines, "")
	}

	return lines
}

func cursorMetricPresent(snap core.UsageSnapshot, key string) bool {
	m, ok := snap.Metrics[key]
	return ok && (m.Used != nil || m.Remaining != nil)
}

func cursorPlanResetAt(snap core.UsageSnapshot) time.Time {
	for _, k := range []string{"plan_percent_used", "cursor_plan_usage", "plan_auto_percent_used", "plan_api_percent_used", "quota"} {
		if t, ok := snap.Resets[k]; ok && !t.IsZero() {
			return t
		}
		if t, ok := snap.Resets[k+"_reset"]; ok && !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func (m Model) buildCursorTileGaugeLines(snap core.UsageSnapshot, innerW int) []string {
	return buildCursorPlanUsageLines(snap, innerW, m.isUsageModeUsed(), m.viewNow(), m.warnThreshold, m.critThreshold)
}

func buildCursorPlanUsageLines(snap core.UsageSnapshot, innerW int, isUsed bool, now time.Time, warnThresh, critThresh float64) []string {
	hasPlan := cursorMetricPresent(snap, "plan_percent_used") ||
		cursorMetricPresent(snap, "cursor_plan_usage") ||
		cursorMetricPresent(snap, "plan_auto_percent_used") ||
		cursorMetricPresent(snap, "plan_api_percent_used")
	ondemandDisabled := snap.Attributes["ondemand"] == "disabled"
	ondemandEnabled := snap.Attributes["ondemand"] == "enabled"
	if !hasPlan && !ondemandDisabled && !ondemandEnabled {
		return nil
	}

	barW := innerW - 14
	if barW < 20 {
		barW = 20
	}
	if barW > 50 {
		barW = 50
	}

	var lines []string

	title := "CURSOR PLAN"
	if plan := strings.TrimSpace(snap.Attributes["plan_tier"]); plan != "" {
		title = "CURSOR " + strings.ToUpper(plan) + " PLAN"
	}
	bullet := lipgloss.NewStyle().Bold(true).Foreground(colorLavender).Render("◈ ")
	lines = append(lines, bullet+lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(title))
	lines = append(lines, "")

	resetForKeys := func(keys ...string) time.Time {
		for _, k := range keys {
			if t, ok := snap.Resets[k]; ok && !t.IsZero() {
				return t
			}
			if t, ok := snap.Resets[k+"_reset"]; ok && !t.IsZero() {
				return t
			}
		}
		return cursorPlanResetAt(snap)
	}

	remainingFromMetric := func(met core.Metric) float64 {
		remaining := 0.0
		if met.Remaining != nil {
			remaining = *met.Remaining
		} else if met.Used != nil {
			remaining = 100 - *met.Used
		}
		if remaining < 0 {
			remaining = 0
		}
		if remaining > 100 {
			remaining = 100
		}
		return remaining
	}

	renderBucket := func(label string, keys []string) {
		var met core.Metric
		found := false
		for _, k := range keys {
			if m, ok := snap.Metrics[k]; ok && (m.Used != nil || m.Remaining != nil) {
				met = m
				found = true
				break
			}
		}
		if !found {
			return
		}

		remaining := remainingFromMetric(met)
		resetAt := resetForKeys(keys...)

		var gaugeBar string
		if isUsed {
			gaugeBar = RenderUsageGauge(100-remaining, barW, warnThresh, critThresh)
		} else {
			gaugeBar = RenderGauge(remaining, barW, warnThresh, critThresh)
		}
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorSubtext).Render(label))
		lines = append(lines, "    "+gaugeBar)
		lines = append(lines, "    "+RenderQuotaStatusAndTimerLineWithMode(remaining, resetAt, now, isUsed))
		lines = append(lines, "")
	}

	suffix := "Remaining"
	if isUsed {
		suffix = "Used"
	}
	renderBucket("Included "+suffix, []string{"plan_percent_used", "cursor_plan_usage"})
	renderBucket("Auto "+suffix, []string{"plan_auto_percent_used"})
	renderBucket("API "+suffix, []string{"plan_api_percent_used"})

	if ondemandDisabled || ondemandEnabled {
		if ondemandDisabled {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorSubtext).Render("On-Demand"))
			lines = append(lines, "    "+lipgloss.NewStyle().Foreground(colorSubtext).Render("Disabled"))
			lines = append(lines, "")
		} else if met, ok := snap.Metrics["plan_ondemand_percent_used"]; ok && (met.Used != nil || met.Remaining != nil) {
			renderBucket("On-Demand "+suffix, []string{"plan_ondemand_percent_used"})
		} else {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorSubtext).Render("On-Demand"))
			lines = append(lines, "    "+lipgloss.NewStyle().Foreground(colorSubtext).Render("Enabled"))
			lines = append(lines, "")
		}
	}

	return lines
}
