package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type providerDisplayInfo struct {
	tagEmoji     string
	tagLabel     string
	summary      string
	detail       string
	gaugePercent float64
	reason       string
}

func computeDisplayInfo(snap core.UsageSnapshot, widget core.DashboardWidget) providerDisplayInfo {
	return normalizeProviderDisplayInfoType(computeDisplayInfoRaw(snap, widget))
}

func normalizeProviderDisplayInfoType(info providerDisplayInfo) providerDisplayInfo {
	switch info.tagLabel {
	case "Credits":
		info.tagEmoji = "💰"
	case "Usage":
		info.tagEmoji = "⚡"
	case "Error", "Auth", "N/A", "":
	default:
		info.tagLabel = "Usage"
		info.tagEmoji = "⚡"
	}
	return info
}

func computeDisplayInfoRaw(snap core.UsageSnapshot, widget core.DashboardWidget) providerDisplayInfo {
	info := providerDisplayInfo{gaugePercent: -1}
	costSummary := core.ExtractAnalyticsCostSummary(snap)

	switch snap.Status {
	case core.StatusError:
		info.tagEmoji = "⚠"
		info.tagLabel = "Error"
		info.reason = "status_error"
		msg := snap.Message
		if len(msg) > 50 {
			msg = msg[:47] + "..."
		}
		if msg == "" {
			msg = "Error"
		}
		info.summary = msg
		core.Tracef("[display] %s: branch=status_error", snap.ProviderID)
		return info
	case core.StatusAuth:
		info.tagEmoji = "🔑"
		info.tagLabel = "Auth"
		info.reason = "status_auth"
		info.summary = "Authentication required"
		core.Tracef("[display] %s: branch=status_auth", snap.ProviderID)
		return info
	case core.StatusUnsupported:
		info.tagEmoji = "◇"
		info.tagLabel = "N/A"
		info.reason = "status_unsupported"
		info.summary = "Not supported"
		core.Tracef("[display] %s: branch=status_unsupported", snap.ProviderID)
		return info
	}

	core.Tracef("[display] %s: checking metrics (%d total), has usage_five_hour=%v, has today_api_cost=%v, has spend_limit=%v",
		snap.ProviderID, len(snap.Metrics),
		snap.Metrics["usage_five_hour"].Used != nil,
		snap.Metrics["today_api_cost"].Used != nil,
		snap.Metrics["spend_limit"].Limit != nil)

	if m, ok := snap.Metrics["spend_limit"]; ok && m.Limit != nil && m.Used != nil {
		remaining := *m.Limit - *m.Used
		if m.Remaining != nil {
			remaining = *m.Remaining
		}
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.reason = "spend_limit"
		info.summary = fmt.Sprintf("$%.0f / $%.0f spent", *m.Used, *m.Limit)
		info.detail = fmt.Sprintf("$%.0f remaining", remaining)
		if indiv, ok2 := snap.Metrics["individual_spend"]; ok2 && indiv.Used != nil {
			otherSpend := *m.Used - *indiv.Used
			if otherSpend < 0 {
				otherSpend = 0
			}
			info.detail = fmt.Sprintf("you $%.0f · team $%.0f · $%.0f remaining", *indiv.Used, otherSpend, remaining)
		}
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = 100 - pct
		}
		core.Tracef("[display] %s: branch=spend_limit used=%.2f limit=%.2f gauge=%.1f", snap.ProviderID, *m.Used, *m.Limit, info.gaugePercent)
		return info
	}

	if m, ok := snap.Metrics["plan_spend"]; ok && m.Used != nil && m.Limit != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("$%.0f / $%.0f plan", *m.Used, *m.Limit)
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = 100 - pct
		}
		if pu, ok2 := snap.Metrics["plan_percent_used"]; ok2 && pu.Used != nil {
			info.detail = fmt.Sprintf("%.0f%% plan used", *pu.Used)
		}
		return info
	}

	if m, ok := snap.Metrics["plan_total_spend_usd"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		if lm, ok2 := snap.Metrics["plan_limit_usd"]; ok2 && lm.Limit != nil {
			info.summary = fmt.Sprintf("$%.2f / $%.0f plan", *m.Used, *lm.Limit)
		} else {
			info.summary = fmt.Sprintf("$%.2f spent", *m.Used)
		}
		return info
	}

	if widget.DisplayStyle == core.DashboardDisplayStyleDetailedCredits {
		return computeDetailedCreditsDisplayInfo(snap, info)
	}

	if m, ok := snap.Metrics["credits"]; ok {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		if m.Remaining != nil && m.Limit != nil {
			info.summary = fmt.Sprintf("$%.2f / $%.2f credits", *m.Remaining, *m.Limit)
			if pct := m.Percent(); pct >= 0 {
				info.gaugePercent = 100 - pct
			}
		} else if m.Used != nil {
			info.summary = fmt.Sprintf("$%.4f used", *m.Used)
		} else {
			info.summary = "Credits available"
		}
		return info
	}
	if m, ok := snap.Metrics["credit_balance"]; ok && m.Remaining != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		if m.Limit != nil {
			info.summary = fmt.Sprintf("$%.2f / $%.2f", *m.Remaining, *m.Limit)
			if pct := m.Percent(); pct >= 0 {
				info.gaugePercent = 100 - pct
			}
		} else {
			info.summary = fmt.Sprintf("$%.2f balance", *m.Remaining)
		}
		return info
	}
	if m, ok := snap.Metrics["total_balance"]; ok && m.Remaining != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("%.2f %s available", *m.Remaining, m.Unit)
		return info
	}

	quotaKey := ""
	for _, key := range []string{"quota_pro", "quota", "quota_flash"} {
		if _, ok := snap.Metrics[key]; ok {
			quotaKey = key
			break
		}
	}
	if quotaKey != "" {
		m := snap.Metrics[quotaKey]
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		if pct := core.MetricUsedPercent(quotaKey, m); pct >= 0 {
			info.gaugePercent = pct
			info.summary = fmt.Sprintf("%.0f%% usage used", pct)
		}
		if m.Remaining != nil {
			info.detail = fmt.Sprintf("%.0f%% usage left", *m.Remaining)
		}
		return info
	}

	if m, ok := snap.Metrics["context_window"]; ok && m.Used != nil && m.Limit != nil {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = pct
			info.summary = fmt.Sprintf("%.0f%% usage used", pct)
		}
		info.detail = fmt.Sprintf("%s / %s tokens", shortCompact(*m.Used), shortCompact(*m.Limit))
		return info
	}

	rateLimits := core.ExtractRateLimitDisplayMetrics(snap.Metrics)
	if len(rateLimits) > 0 {
		worstRatePct := float64(100)
		rateParts := make([]string, 0, len(rateLimits))
		for _, rate := range rateLimits {
			if rate.UsedPercent < worstRatePct {
				worstRatePct = rate.UsedPercent
			}
			if rate.UsesRemainingPercent {
				label := metricLabel(widget, rate.LabelKey)
				rateParts = append(rateParts, fmt.Sprintf("%s %.0f%%", label, 100-rate.RemainingPercent))
				continue
			}
			rateParts = append(rateParts, fmt.Sprintf("%s %.0f%%", strings.ToUpper(rate.LabelKey), 100-rate.UsedPercent))
		}
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		info.gaugePercent = 100 - worstRatePct
		info.summary = fmt.Sprintf("%.0f%% used", 100-worstRatePct)
		if len(rateParts) > 0 {
			sort.Strings(rateParts)
			info.detail = strings.Join(rateParts, " · ")
		}
		return info
	}

	if fh, ok := snap.Metrics["usage_five_hour"]; ok && fh.Used != nil {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		info.reason = "usage_five_hour"
		info.gaugePercent = *fh.Used
		parts := []string{fmt.Sprintf("5h %.0f%%", *fh.Used)}
		if sd, ok2 := snap.Metrics["usage_seven_day"]; ok2 && sd.Used != nil {
			parts = append(parts, fmt.Sprintf("7d %.0f%%", *sd.Used))
			if *sd.Used > info.gaugePercent {
				info.gaugePercent = *sd.Used
			}
		}
		info.summary = strings.Join(parts, " · ")

		var detailParts []string
		if dc, ok2 := snap.Metrics["today_api_cost"]; ok2 && dc.Used != nil {
			tag := metricWindowTag(dc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f %s", *dc.Used, tag))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f", *dc.Used))
			}
		}
		if costSummary.BurnRateUSD > 0 {
			detailParts = append(detailParts, fmt.Sprintf("$%.2f/h", costSummary.BurnRateUSD))
		}
		info.detail = strings.Join(detailParts, " · ")
		core.Tracef("[display] %s: branch=usage_five_hour used=%.1f gauge=%.1f -> tag=Usage", snap.ProviderID, *fh.Used, info.gaugePercent)
		return info
	}

	if _, hasBillingBlock := snap.Resets["billing_block"]; hasBillingBlock {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		info.reason = "billing_block_fallback"

		var parts []string
		if dc, ok2 := snap.Metrics["today_api_cost"]; ok2 && dc.Used != nil {
			tag := metricWindowTag(dc)
			if tag != "" {
				parts = append(parts, fmt.Sprintf("~$%.2f %s", *dc.Used, tag))
			} else {
				parts = append(parts, fmt.Sprintf("~$%.2f", *dc.Used))
			}
		}
		if costSummary.BurnRateUSD > 0 {
			parts = append(parts, fmt.Sprintf("$%.2f/h", costSummary.BurnRateUSD))
		}
		info.summary = strings.Join(parts, " · ")

		var detailParts []string
		if bc, ok2 := snap.Metrics["5h_block_cost"]; ok2 && bc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("~$%.2f 5h block", *bc.Used))
		}
		if wc, ok2 := snap.Metrics["7d_api_cost"]; ok2 && wc.Used != nil {
			tag := metricWindowTag(wc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f/%s", *wc.Used, tag))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f", *wc.Used))
			}
		}
		if msgs, ok2 := snap.Metrics["messages_today"]; ok2 && msgs.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f msgs", *msgs.Used))
		}
		if sess, ok2 := snap.Metrics["sessions_today"]; ok2 && sess.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f sessions", *sess.Used))
		}
		info.detail = strings.Join(detailParts, " · ")
		core.Tracef("[display] %s: branch=billing_block_fallback -> tag=Usage", snap.ProviderID)
		return info
	}

	if m, ok := snap.Metrics["today_api_cost"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.reason = "today_api_cost"
		core.Tracef("[display] %s: branch=today_api_cost used=%.2f -> tag=Credits", snap.ProviderID, *m.Used)
		tag := metricWindowTag(m)
		costLabel := fmt.Sprintf("~$%.2f", *m.Used)
		if tag != "" {
			costLabel = fmt.Sprintf("~$%.2f %s", *m.Used, tag)
		}
		parts := []string{costLabel}
		if costSummary.BurnRateUSD > 0 {
			parts = append(parts, fmt.Sprintf("$%.2f/h", costSummary.BurnRateUSD))
		}
		info.summary = strings.Join(parts, " · ")

		var detailParts []string
		if bc, ok2 := snap.Metrics["5h_block_cost"]; ok2 && bc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("~$%.2f 5h block", *bc.Used))
		}
		if wc, ok2 := snap.Metrics["7d_api_cost"]; ok2 && wc.Used != nil {
			wcTag := metricWindowTag(wc)
			if wcTag != "" {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f/%s", *wc.Used, wcTag))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f", *wc.Used))
			}
		}
		if msgs, ok2 := snap.Metrics["messages_today"]; ok2 && msgs.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f msgs", *msgs.Used))
		}
		if sess, ok2 := snap.Metrics["sessions_today"]; ok2 && sess.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f sessions", *sess.Used))
		}
		info.detail = strings.Join(detailParts, " · ")
		return info
	}

	if m, ok := snap.Metrics["5h_block_cost"]; ok && m.Used != nil {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		info.summary = fmt.Sprintf("~$%.2f / 5h block", *m.Used)
		if costSummary.BurnRateUSD > 0 {
			info.detail = fmt.Sprintf("$%.2f/h burn rate", costSummary.BurnRateUSD)
		}
		return info
	}

	hasUsage := false
	worstUsagePct := float64(100)
	var usageKey string
	for _, key := range sortedMetricKeys(snap.Metrics) {
		m := snap.Metrics[key]
		pct := m.Percent()
		if pct >= 0 {
			hasUsage = true
			if pct < worstUsagePct {
				worstUsagePct = pct
				usageKey = key
			}
		}
	}
	if hasUsage {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		info.gaugePercent = 100 - worstUsagePct
		info.summary = fmt.Sprintf("%.0f%% used", 100-worstUsagePct)
		if snap.ProviderID == "gemini_cli" {
			if m, ok := snap.Metrics["total_conversations"]; ok && m.Used != nil {
				info.detail = fmt.Sprintf("%.0f conversations", *m.Used)
				return info
			}
			if m, ok := snap.Metrics["messages_today"]; ok && m.Used != nil {
				info.detail = fmt.Sprintf("%.0f msgs today", *m.Used)
				return info
			}
			return info
		}
		if usageKey != "" {
			qm := snap.Metrics[usageKey]
			parts := []string{metricLabel(widget, usageKey)}
			if qm.Window != "" && qm.Window != "all_time" && qm.Window != "current_period" {
				parts = append(parts, qm.Window)
			}
			info.detail = strings.Join(parts, " · ")
		}
		return info
	}

	if m, ok := snap.Metrics["total_cost_usd"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("$%.2f total", *m.Used)
		return info
	}
	if m, ok := snap.Metrics["all_time_api_cost"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("~$%.2f total (API est.)", *m.Used)
		return info
	}

	if m, ok := snap.Metrics["messages_today"]; ok && m.Used != nil {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		info.summary = fmt.Sprintf("%.0f msgs today", *m.Used)
		var detailParts []string
		if tc, ok2 := snap.Metrics["tool_calls_today"]; ok2 && tc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f tools", *tc.Used))
		}
		if sc, ok2 := snap.Metrics["sessions_today"]; ok2 && sc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f sessions", *sc.Used))
		}
		info.detail = strings.Join(detailParts, " · ")
		return info
	}

	for _, key := range core.FallbackDisplayMetricKeys(snap.Metrics) {
		m := snap.Metrics[key]
		if m.Used != nil {
			info.tagEmoji = "⚡"
			info.tagLabel = "Usage"
			info.summary = fmt.Sprintf("%s: %s %s", metricLabel(widget, key), formatNumber(*m.Used), m.Unit)
			return info
		}
	}

	if snap.Message != "" {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		msg := snap.Message
		if len(msg) > 50 {
			msg = msg[:47] + "..."
		}
		info.summary = msg
		return info
	}

	info.tagEmoji = "⚡"
	info.tagLabel = "Usage"
	if snap.Status == core.StatusUnknown {
		info.summary = "Syncing telemetry..."
	} else {
		info.summary = string(snap.Status)
	}
	return info
}

func computeDetailedCreditsDisplayInfo(snap core.UsageSnapshot, info providerDisplayInfo) providerDisplayInfo {
	costSummary := core.ExtractAnalyticsCostSummary(snap)

	if m, ok := snap.Metrics["credit_balance"]; ok && m.Limit != nil && m.Remaining != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		spent := *m.Limit - *m.Remaining
		if m.Used != nil {
			spent = *m.Used
		}
		info.summary = fmt.Sprintf("$%.2f / $%.2f spent", spent, *m.Limit)
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = 100 - pct
		}

		detailParts := []string{fmt.Sprintf("$%.2f remaining", *m.Remaining)}
		if dc, ok2 := snap.Metrics["today_cost"]; ok2 && dc.Used != nil {
			tag := metricWindowTag(dc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("%s $%.2f", tag, *dc.Used))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("$%.2f", *dc.Used))
			}
		} else if dc, ok2 := snap.Metrics["usage_daily"]; ok2 && dc.Used != nil {
			tag := metricWindowTag(dc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("%s $%.2f", tag, *dc.Used))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("$%.2f", *dc.Used))
			}
		}
		if wc, ok2 := snap.Metrics["7d_api_cost"]; ok2 && wc.Used != nil {
			tag := metricWindowTag(wc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("%s $%.2f", tag, *wc.Used))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("$%.2f", *wc.Used))
			}
		} else if wc, ok2 := snap.Metrics["usage_weekly"]; ok2 && wc.Used != nil {
			tag := metricWindowTag(wc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("%s $%.2f", tag, *wc.Used))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("$%.2f", *wc.Used))
			}
		}
		if models := snapshotMeta(snap, "activity_models"); models != "" {
			detailParts = append(detailParts, fmt.Sprintf("%s models", models))
		}
		info.detail = strings.Join(detailParts, " · ")
		return info
	}

	if m, ok := snap.Metrics["credits"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("$%.4f used", *m.Used)

		var detailParts []string
		if daily, ok := snap.Metrics["usage_daily"]; ok && daily.Used != nil {
			tag := metricWindowTag(daily)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("%s $%.2f", tag, *daily.Used))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("$%.2f", *daily.Used))
			}
		}
		if byok, ok := snap.Metrics["byok_daily"]; ok && byok.Used != nil && *byok.Used > 0 {
			detailParts = append(detailParts, fmt.Sprintf("BYOK $%.2f", *byok.Used))
		}
		if costSummary.BurnRateUSD > 0 {
			detailParts = append(detailParts, fmt.Sprintf("$%.2f/h", costSummary.BurnRateUSD))
		}
		if models := snapshotMeta(snap, "activity_models"); models != "" {
			detailParts = append(detailParts, fmt.Sprintf("%s models", models))
		}
		info.detail = strings.Join(detailParts, " · ")
		return info
	}

	info.tagEmoji = "💰"
	info.tagLabel = "Credits"
	info.summary = "Connected"
	return info
}

func windowActivityLine(snap core.UsageSnapshot, tw core.TimeWindow) string {
	var parts []string
	if m, ok := snap.Metrics["window_requests"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, fmt.Sprintf("%.0f reqs", *m.Used))
	}
	if m, ok := snap.Metrics["window_cost"]; ok && m.Used != nil && *m.Used > 0.001 {
		parts = append(parts, fmt.Sprintf("$%.2f", *m.Used))
	}
	if m, ok := snap.Metrics["window_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, shortCompact(*m.Used)+" tok")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ") + " in " + tw.Label()
}

func metricWindowTag(met core.Metric) string {
	return strings.TrimSpace(met.Window)
}
