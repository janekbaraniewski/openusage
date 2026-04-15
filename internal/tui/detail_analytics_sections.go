package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func hasLanguageMetrics(snap core.UsageSnapshot) bool {
	return core.HasLanguageUsage(snap)
}

func renderLanguagesSection(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	langs, _ := core.ExtractLanguageUsage(snap)
	if len(langs) == 0 {
		return
	}

	total := float64(0)
	for _, language := range langs {
		total += language.Requests
	}
	if total <= 0 {
		return
	}

	maxShow := 10
	if len(langs) > maxShow {
		langs = langs[:maxShow]
	}

	items := make([]chartItem, 0, len(langs))
	for _, language := range langs {
		pct := language.Requests / total * 100
		items = append(items, chartItem{
			Label:     language.Name,
			Value:     language.Requests,
			Color:     stableModelColor("lang:"+language.Name, "languages"),
			ValueText: fmt.Sprintf("%4.1f%%  %s", pct, dimStyle.Render(formatNumber(language.Requests)+" req")),
		})
	}

	labelW := 18
	if w < 55 {
		labelW = 14
	}
	barW := w - labelW - 20
	if barW < 8 {
		barW = 8
	}
	if barW > 30 {
		barW = 30
	}

	sb.WriteString(RenderHBarChart(items, barW, labelW) + "\n")

	if len(langs) > maxShow {
		sb.WriteString("  " + dimStyle.Render(fmt.Sprintf("+ %d more languages", len(langs)-maxShow)) + "\n")
	}
}

func hasMCPMetrics(snap core.UsageSnapshot) bool {
	return core.HasMCPUsage(snap)
}

func renderMCPSection(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	rawServers, _ := core.ExtractMCPUsage(snap)
	servers := make([]struct {
		name  string
		calls float64
		funcs []struct {
			name  string
			calls float64
		}
	}, 0, len(rawServers))
	for _, rawServer := range rawServers {
		server := struct {
			name  string
			calls float64
			funcs []struct {
				name  string
				calls float64
			}
		}{
			name:  prettifyMCPServerName(rawServer.RawName),
			calls: rawServer.Calls,
		}
		for _, rawFunc := range rawServer.Functions {
			server.funcs = append(server.funcs, struct {
				name  string
				calls float64
			}{
				name:  prettifyMCPFunctionName(rawFunc.RawName),
				calls: rawFunc.Calls,
			})
		}
		servers = append(servers, server)
	}
	if len(servers) == 0 {
		return
	}

	totalCalls := float64(0)
	for _, server := range servers {
		totalCalls += server.calls
	}
	if totalCalls <= 0 {
		return
	}

	barW := w - 4
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	allEntries := make([]toolMixEntry, 0, len(servers))
	for _, server := range servers {
		allEntries = append(allEntries, toolMixEntry{name: server.name, count: server.calls})
	}
	toolColors := buildToolColorMap(allEntries, snap.AccountID)

	sb.WriteString(fmt.Sprintf("  %s\n", renderToolMixBar(allEntries, totalCalls, barW, toolColors)))

	for i, server := range servers {
		toolColor := colorForTool(toolColors, server.name)
		colorDot := lipgloss.NewStyle().Foreground(toolColor).Render("■")
		serverLabel := fmt.Sprintf("%s %d %s", colorDot, i+1, server.name)
		valueStr := fmt.Sprintf("%2.0f%% %s calls", server.calls/totalCalls*100, shortCompact(server.calls))
		sb.WriteString(renderDotLeaderRow(serverLabel, valueStr, w-2))
		sb.WriteString("\n")

		maxFuncs := 8
		if len(server.funcs) < maxFuncs {
			maxFuncs = len(server.funcs)
		}
		for j := 0; j < maxFuncs; j++ {
			fn := server.funcs[j]
			sb.WriteString(renderDotLeaderRow("    "+fn.name, fmt.Sprintf("%s calls", shortCompact(fn.calls)), w-2))
			sb.WriteString("\n")
		}
		if len(server.funcs) > 8 {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("    + %d more functions", len(server.funcs)-8)))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("  " + dimStyle.Render(fmt.Sprintf("%d servers · %.0f calls", len(servers), totalCalls)) + "\n")
}
