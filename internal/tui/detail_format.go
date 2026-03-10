package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func wrapTags(tags []string, maxWidth int) []string {
	if len(tags) == 0 {
		return nil
	}
	var rows []string
	currentRow := ""
	currentW := 0
	for _, tag := range tags {
		tagW := lipgloss.Width(tag)
		if currentW > 0 && currentW+1+tagW > maxWidth {
			rows = append(rows, currentRow)
			currentRow = tag
			currentW = tagW
			continue
		}
		if currentW > 0 {
			currentRow += " "
			currentW++
		}
		currentRow += tag
		currentW += tagW
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

func renderDetailSectionHeader(sb *strings.Builder, title string, w int) {
	color := sectionColor(title)
	left := "  " +
		lipgloss.NewStyle().Foreground(color).Render(sectionIcon(title)) +
		lipgloss.NewStyle().Bold(true).Foreground(color).Render(" "+title+" ")
	lineLen := w - lipgloss.Width(left) - 2
	if lineLen < 4 {
		lineLen = 4
	}
	sb.WriteString(left + lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("─", lineLen)) + "\n")
}

func sectionIcon(title string) string {
	switch title {
	case "Usage":
		return "⚡"
	case "Spending":
		return "💰"
	case "Tokens":
		return "📊"
	case "Activity", "Trends":
		return "📈"
	case "Timers":
		return "⏰"
	case "Models":
		return "🤖"
	case "Languages":
		return "🗂"
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
	case "Tokens", "Trends":
		return colorSapphire
	case "Activity":
		return colorGreen
	case "Timers":
		return colorMaroon
	case "Models":
		return colorLavender
	case "Languages":
		return colorPeach
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
		parts = append(parts, fmt.Sprintf("%.0f%% remaining", *m.Limit-*m.Used))
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
		parts = append(parts, fmt.Sprintf("%s / %s %s", formatNumber(*m.Used), formatNumber(*m.Limit), m.Unit))
	case m.Remaining != nil && m.Limit != nil:
		parts = append(parts, fmt.Sprintf("%s / %s %s remaining", formatNumber(*m.Remaining), formatNumber(*m.Limit), m.Unit))
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
	default:
		return result
	}
}
