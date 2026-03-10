package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

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
		renderKeyValuePairs(sb, snap.Diagnostics, labelW, maxValW, lipgloss.NewStyle().Foreground(colorYellow))
	}
	if len(snap.Raw) > 0 {
		if len(snap.Attributes) > 0 || len(snap.Diagnostics) > 0 {
			sb.WriteString("\n")
		}
		renderDetailSectionHeader(sb, "Raw Data", w)
		renderRawData(sb, snap.Raw, widget, w)
	}
}

func renderKeyValuePairs(sb *strings.Builder, data map[string]string, labelW, maxValW int, valueStyle lipgloss.Style) {
	for _, key := range core.SortedStringKeys(data) {
		value := smartFormatValue(data[key])
		if len(value) > maxValW {
			value = value[:maxValW-3] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			labelStyle.Width(labelW).Render(prettifyKey(key)),
			valueStyle.Render(value),
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
	for _, group := range widget.RawGroups {
		hasAny := false
		for _, key := range group.Keys {
			if value := strings.TrimSpace(raw[key]); value != "" {
				hasAny = true
				break
			}
		}
		if !hasAny {
			continue
		}
		for _, key := range group.Keys {
			value := strings.TrimSpace(raw[key])
			if value == "" {
				continue
			}
			rendered[key] = true
			value = smartFormatValue(value)
			if len(value) > maxValW {
				value = value[:maxValW-3] + "..."
			}
			sb.WriteString(fmt.Sprintf("  %s  %s\n",
				labelStyle.Width(labelW).Render(prettifyKey(key)),
				valueStyle.Render(value),
			))
		}
	}

	for _, key := range core.SortedStringKeys(raw) {
		if rendered[key] || strings.HasSuffix(key, "_error") {
			continue
		}
		value := smartFormatValue(raw[key])
		if len(value) > maxValW {
			value = value[:maxValW-3] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			labelStyle.Width(labelW).Render(prettifyKey(key)),
			dimStyle.Render(value),
		))
	}
}

func smartFormatValue(v string) string {
	trimmed := strings.TrimSpace(v)
	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 1e12 && n < 2e13 {
		return time.Unix(n/1000, 0).Format("Jan 02, 2006 15:04")
	}
	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 1e9 && n < 2e10 {
		return time.Unix(n, 0).Format("Jan 02, 2006 15:04")
	}
	return v
}
