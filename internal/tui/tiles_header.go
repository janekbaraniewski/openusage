package tui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func buildTileHeaderMetaLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, animFrame int) []string {
	var pills []string
	pills = append(pills, buildTileCyclePills(snap)...)
	pills = append(pills, buildTileResetPills(snap, widget, animFrame)...)
	return wrapTilePills(pills, innerW)
}

func buildTileCyclePills(snap core.UsageSnapshot) []string {
	var pills []string
	if pill := buildTileCyclePill("Billing", snapshotMeta(snap, "billing_cycle_start"), snapshotMeta(snap, "billing_cycle_end"), snap.Timestamp); pill != "" {
		pills = append(pills, pill)
	}
	if pill := buildTileCyclePill("Usage 5h", snapshotMeta(snap, "block_start"), snapshotMeta(snap, "block_end"), snap.Timestamp); pill != "" {
		pills = append(pills, pill)
	}
	return pills
}

func buildTileCyclePill(label, startRaw, endRaw string, referenceTime time.Time) string {
	start, hasStart := parseTileTimestamp(startRaw)
	end, hasEnd := parseTileTimestamp(endRaw)
	if !hasStart && !hasEnd {
		return ""
	}

	var span string
	switch {
	case hasStart && hasEnd:
		span = fmt.Sprintf("%s→%s", formatTileTimestamp(start, referenceTime), formatTileTimestamp(end, referenceTime))
	case hasEnd:
		span = "ends " + formatTileTimestamp(end, referenceTime)
	default:
		span = "since " + formatTileTimestamp(start, referenceTime)
	}

	return lipgloss.NewStyle().Foreground(colorLavender).Bold(true).Render("◷ "+label) +
		" " + lipgloss.NewStyle().Foreground(colorSubtext).Render(span)
}

func parseTileTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}

	if unixVal, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if unixVal > 1e12 {
			return time.Unix(unixVal/1000, (unixVal%1000)*1e6), true
		}
		return time.Unix(unixVal, 0), true
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02",
		"Jan 02, 2006 15:04 MST",
		"Jan 02, 2006 15:04",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}

	return time.Time{}, false
}

func formatTileTimestamp(t, referenceTime time.Time) string {
	now := referenceTime
	if now.IsZero() {
		now = time.Now()
	}
	isDateOnly := t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0
	if isDateOnly {
		if t.Year() == now.Year() {
			return t.Format("Jan 02")
		}
		return t.Format("2006-01-02")
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 02 15:04")
	}
	return t.Format("2006-01-02 15:04")
}

func wrapTilePills(pills []string, innerW int) []string {
	if len(pills) == 0 {
		return nil
	}

	sep := dimStyle.Render(" · ")
	sepW := lipgloss.Width(sep)

	var lines []string
	var line string
	lineW := 0

	for _, pill := range pills {
		pillW := lipgloss.Width(pill)
		if lineW == 0 {
			line = pill
			lineW = pillW
			continue
		}
		if lineW+sepW+pillW <= innerW {
			line += sep + pill
			lineW += sepW + pillW
			continue
		}
		lines = append(lines, line)
		line = pill
		lineW = pillW
	}

	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

type resetEntry struct {
	key   string
	label string
	dur   time.Duration
	at    time.Time
}

var resetLabelMap = map[string]string{
	"billing_block":        "Usage 5h",
	"billing_cycle_end":    "Billing",
	"quota_reset":          "Usage",
	"usage_five_hour":      "Usage 5h",
	"usage_one_day":        "Usage 1d",
	"usage_seven_day":      "Usage 7d",
	"limit_reset":          "Limit",
	"key_expires":          "Key Exp",
	"rate_limit_primary":   "Primary",
	"rate_limit_secondary": "Secondary",
	"rpm":                  "RPM",
	"tpm":                  "TPM",
	"rpd":                  "RPD",
	"tpd":                  "TPD",
	"rpm_headers":          "Req",
	"tpm_headers":          "Tok",
	"gh_core_rpm":          "Core",
	"gh_search_rpm":        "Search",
	"gh_graphql_rpm":       "GraphQL",
}

func collectActiveResetEntries(snap core.UsageSnapshot, widget core.DashboardWidget) []resetEntry {
	if len(snap.Resets) == 0 {
		return nil
	}

	var entries []resetEntry
	for key, t := range snap.Resets {
		dur := time.Until(t)
		if dur < 0 {
			continue
		}
		entries = append(entries, resetEntry{
			key:   key,
			label: resetLabelForKey(snap, widget, key),
			dur:   dur,
			at:    t,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		pi := resetSortPriority(entries[i].key)
		pj := resetSortPriority(entries[j].key)
		if pi != pj {
			return pi < pj
		}
		if !entries[i].at.Equal(entries[j].at) {
			return entries[i].at.Before(entries[j].at)
		}
		return entries[i].label < entries[j].label
	})

	// Deduplicate entries with the same label, keeping the first (highest priority).
	seen := make(map[string]bool, len(entries))
	deduped := entries[:0]
	for _, e := range entries {
		if seen[e.label] {
			continue
		}
		seen[e.label] = true
		deduped = append(deduped, e)
	}
	return deduped
}

func resetSortPriority(key string) int {
	k := strings.TrimSpace(strings.TrimSuffix(key, "_reset"))
	order := map[string]int{
		"rate_limit_primary":               10,
		"rate_limit_secondary":             11,
		"rate_limit_code_review_primary":   12,
		"rate_limit_code_review_secondary": 13,
		"gh_core_rpm":                      20,
		"gh_search_rpm":                    21,
		"gh_graphql_rpm":                   22,
		"usage_five_hour":                  30,
		"usage_one_day":                    31,
		"usage_seven_day":                  32,
		"billing_block":                    40,
		"billing_cycle_end":                41,
		"quota_reset":                      42,
		"limit_reset":                      43,
		"key_expires":                      44,
		"rpm":                              50,
		"tpm":                              51,
		"rpd":                              52,
		"tpd":                              53,
		"rpm_headers":                      54,
		"tpm_headers":                      55,
	}
	if p, ok := order[k]; ok {
		return p
	}
	return 999
}

func resetLabelForKey(snap core.UsageSnapshot, widget core.DashboardWidget, key string) string {
	if widget.ResetStyle == core.DashboardResetStyleCompactModelResets {
		if label := compactModelResetLabel(strings.TrimSuffix(key, "_reset")); label != "" {
			return label
		}
	}
	if label := resetLabelMap[key]; label != "" {
		return label
	}
	trimmed := strings.TrimSuffix(key, "_reset")
	if label := resetLabelMap[trimmed]; label != "" {
		return label
	}
	if met, ok := snap.Metrics[trimmed]; ok && met.Window != "" {
		return metricLabel(widget, trimmed)
	}
	if met, ok := snap.Metrics[key]; ok && met.Window != "" {
		return metricLabel(widget, key)
	}
	return metricLabel(widget, trimmed)
}

func compactModelResetLabel(key string) string {
	model := key
	token := ""
	if idx := strings.LastIndex(key, "_"); idx > 0 {
		model = key[:idx]
		token = key[idx+1:]
	}

	model = strings.ToLower(model)
	model = strings.ReplaceAll(model, "_", "-")

	model = truncateToWidth(model, 18)
	if token == "" {
		return model
	}

	tokenMap := map[string]string{
		"requests": "req",
		"tokens":   "tok",
		"quota":    "quota",
	}
	if short, ok := tokenMap[token]; ok {
		token = short
	}
	return model + " " + token
}

func formatHeaderDuration(d time.Duration) string {
	if d <= 0 {
		return "<1m"
	}
	if d < time.Hour {
		mins := int(math.Ceil(d.Minutes()))
		if mins < 1 {
			mins = 1
		}
		return fmt.Sprintf("%dm", mins)
	}
	if d < 24*time.Hour {
		totalMins := int(math.Ceil(d.Minutes()))
		h := totalMins / 60
		m := totalMins % 60
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	totalHours := int(math.Ceil(d.Hours()))
	return fmt.Sprintf("%dd%02dh", totalHours/24, totalHours%24)
}

func buildCompactModelResetPills(entries []resetEntry) []string {
	if len(entries) == 0 {
		return nil
	}

	type group struct {
		at     time.Time
		labels []string
		minDur time.Duration
	}
	groups := make(map[int64]*group)
	for _, e := range entries {
		bucket := e.at.Unix() / 60
		g, ok := groups[bucket]
		if !ok {
			g = &group{at: e.at, minDur: e.dur}
			groups[bucket] = g
		}
		if e.dur < g.minDur {
			g.minDur = e.dur
		}
		g.labels = append(g.labels, e.label)
	}

	ordered := make([]*group, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g.labels)
		ordered = append(ordered, g)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].at.Before(ordered[j].at) })

	var pills []string
	for _, g := range ordered {
		durColor := colorTeal
		if g.minDur < 10*time.Minute {
			durColor = colorPeach
		} else if g.minDur < 30*time.Minute {
			durColor = colorYellow
		}

		label := "Model quotas"
		if len(g.labels) <= 2 {
			label = strings.Join(g.labels, ", ")
		} else {
			label = fmt.Sprintf("Model quotas (%d models)", len(g.labels))
		}

		pill := lipgloss.NewStyle().Foreground(colorSubtext).Render("◷ "+label+" ") +
			lipgloss.NewStyle().Foreground(durColor).Bold(true).Render(formatHeaderDuration(g.minDur))
		pills = append(pills, pill)
	}
	return pills
}

func buildTileResetPills(snap core.UsageSnapshot, widget core.DashboardWidget, animFrame int) []string {
	_ = animFrame
	entries := collectActiveResetEntries(snap, widget)
	if len(entries) == 0 {
		return nil
	}
	if snap.ProviderID == "gemini_cli" {
		entries = filterGeminiPrimaryQuotaReset(entries, snap)
	}

	if widget.ResetStyle == core.DashboardResetStyleCompactModelResets {
		threshold := widget.ResetCompactThreshold
		if threshold <= 0 {
			threshold = 4
		}
		if len(entries) >= threshold {
			return buildCompactModelResetPills(entries)
		}
	}

	pills := make([]string, 0, len(entries))
	for _, e := range entries {
		durColor := colorTeal
		if e.dur < 10*time.Minute {
			durColor = colorPeach
		} else if e.dur < 30*time.Minute {
			durColor = colorYellow
		}
		pill := lipgloss.NewStyle().Foreground(colorSubtext).Render("◷ "+e.label+" ") +
			lipgloss.NewStyle().Foreground(durColor).Bold(true).Render(formatHeaderDuration(e.dur))
		pills = append(pills, pill)
	}
	return pills
}

func buildTileResetLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int, animFrame int) []string {
	return wrapTilePills(buildTileResetPills(snap, widget, animFrame), innerW)
}

type geminiQuotaEntry struct {
	key         string
	label       string
	usedPercent float64
	resetKey    string
	resetAt     time.Time
	hasReset    bool
}

func collectGeminiQuotaEntries(snap core.UsageSnapshot) []geminiQuotaEntry {
	if snap.ProviderID != "gemini_cli" {
		return nil
	}

	entries := make([]geminiQuotaEntry, 0)
	for key, metric := range snap.Metrics {
		if !strings.HasPrefix(key, "quota_model_") {
			continue
		}
		usedPct := metricUsedPercent(key, metric)
		if usedPct < 0 {
			continue
		}

		entry := geminiQuotaEntry{
			key:         key,
			label:       geminiQuotaLabelFromMetricKey(key),
			usedPercent: usedPct,
			resetKey:    key + "_reset",
		}
		if resetAt, ok := snap.Resets[entry.resetKey]; ok && !resetAt.IsZero() {
			entry.hasReset = true
			entry.resetAt = resetAt
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].usedPercent != entries[j].usedPercent {
			return entries[i].usedPercent > entries[j].usedPercent
		}
		return entries[i].label < entries[j].label
	})
	return entries
}

func geminiQuotaLabelFromMetricKey(metricKey string) string {
	base := strings.TrimPrefix(metricKey, "quota_model_")
	if base == "" {
		return metricKey
	}

	modelPart := base
	tokenType := ""
	if idx := strings.LastIndex(base, "_"); idx > 0 {
		modelPart = base[:idx]
		tokenType = base[idx+1:]
	}

	modelLabel := prettifyModelName(strings.ReplaceAll(modelPart, "_", "-"))
	tokenLabel := tokenType
	switch tokenType {
	case "requests":
		tokenLabel = "req"
	case "tokens":
		tokenLabel = "tok"
	}
	if tokenLabel == "" {
		return truncateToWidth(modelLabel, 28)
	}
	return truncateToWidth(modelLabel+" "+tokenLabel, 28)
}

func geminiPrimaryQuotaMetricKey(snap core.UsageSnapshot) string {
	entries := collectGeminiQuotaEntries(snap)
	if len(entries) > 0 {
		return entries[0].key
	}

	bestKey := ""
	bestUsed := -1.0
	for _, key := range []string{"quota", "quota_pro", "quota_flash"} {
		metric, ok := snap.Metrics[key]
		if !ok {
			continue
		}
		usedPct := metricUsedPercent(key, metric)
		if usedPct > bestUsed {
			bestUsed = usedPct
			bestKey = key
		}
	}
	return bestKey
}

func isGeminiQuotaResetKey(key string) bool {
	switch key {
	case "quota_reset", "quota_pro_reset", "quota_flash_reset":
		return true
	}
	return strings.HasPrefix(key, "quota_model_")
}

func filterGeminiPrimaryQuotaReset(entries []resetEntry, snap core.UsageSnapshot) []resetEntry {
	if len(entries) == 0 {
		return nil
	}

	primaryMetricKey := geminiPrimaryQuotaMetricKey(snap)
	primaryResetKey := ""
	if primaryMetricKey != "" {
		primaryResetKey = primaryMetricKey + "_reset"
	}

	var quotaEntries []resetEntry
	filtered := make([]resetEntry, 0, len(entries))
	for _, entry := range entries {
		if isGeminiQuotaResetKey(entry.key) {
			quotaEntries = append(quotaEntries, entry)
			continue
		}
		filtered = append(filtered, entry)
	}
	if len(quotaEntries) == 0 {
		return entries
	}

	chosen := quotaEntries[0]
	found := false
	if primaryResetKey != "" {
		for _, entry := range quotaEntries {
			if entry.key == primaryResetKey {
				chosen = entry
				found = true
				break
			}
		}
	}
	if !found {
		for _, fallbackKey := range []string{"quota_reset", "quota_pro_reset", "quota_flash_reset"} {
			for _, entry := range quotaEntries {
				if entry.key == fallbackKey {
					chosen = entry
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}

	filtered = append(filtered, chosen)
	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].at.Equal(filtered[j].at) {
			return filtered[i].at.Before(filtered[j].at)
		}
		return filtered[i].label < filtered[j].label
	})
	return filtered
}

func buildGeminiOtherQuotaLines(snap core.UsageSnapshot, innerW int) ([]string, map[string]bool) {
	entries := collectGeminiQuotaEntries(snap)
	if len(entries) <= 1 {
		return nil, nil
	}

	primaryKey := geminiPrimaryQuotaMetricKey(snap)
	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Other Usage"),
	}
	usedKeys := make(map[string]bool, len(entries))

	maxLabel := innerW / 2
	if maxLabel < 14 {
		maxLabel = 14
	}
	for _, entry := range entries {
		if entry.key == primaryKey {
			continue
		}

		value := fmt.Sprintf("%.1f%% used", entry.usedPercent)
		if entry.hasReset {
			remaining := time.Until(entry.resetAt)
			if remaining > 0 {
				value += " · " + formatHeaderDuration(remaining)
			}
		}

		lines = append(lines, renderDotLeaderRow(truncateToWidth(entry.label, maxLabel), value, innerW))
		usedKeys[entry.key] = true
	}

	if len(lines) <= 1 {
		return nil, nil
	}
	return lines, usedKeys
}

func buildTileMetaLines(snap core.UsageSnapshot, innerW int) []string {
	meta := snapshotMetaEntries(snap)
	if len(meta) == 0 {
		return nil
	}

	type metaEntry struct {
		label, key string
	}
	order := []metaEntry{
		{"Account", "account_email"},
		{"Key", "key_label"},
		{"Key Name", "key_name"},
		{"Key Type", "key_type"},
		{"Tier", "tier"},
		{"Plan", "plan_name"},
		{"Type", "plan_type"},
		{"Role", "membership_type"},
		{"Team", "team_membership"},
		{"Org", "organization_name"},
		{"Model", "active_model"},
		{"Version", "cli_version"},
		{"Price", "plan_price"},
		{"Status", "subscription_status"},
		{"Reset", "limit_reset"},
		{"Expires", "expires_at"},
	}

	var lines []string
	for _, e := range order {
		val, ok := meta[e.key]
		if !ok || val == "" {
			continue
		}
		maxVal := innerW - len(e.label) - 5
		if maxVal < 5 {
			maxVal = 5
		}
		if len(val) > maxVal {
			val = val[:maxVal-1] + "…"
		}
		lines = append(lines, renderDotLeaderRow(e.label, val, innerW))
	}
	return lines
}
