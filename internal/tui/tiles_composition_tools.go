package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func prettifyMCPServerName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "unknown"
	}
	s = strings.TrimPrefix(s, "claude_ai_")
	s = strings.TrimPrefix(s, "plugin_")
	s = strings.TrimSuffix(s, "_mcp")
	parts := strings.Split(s, "_")
	if len(parts) >= 2 && parts[0] == parts[len(parts)-1] {
		parts = parts[:len(parts)-1]
	}
	s = strings.Join(parts, "_")
	if s == "" {
		return raw
	}
	return prettifyMCPName(s)
}

func prettifyMCPFunctionName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return raw
	}
	return prettifyMCPName(s)
}

func prettifyMCPName(s string) string {
	s = strings.NewReplacer("_", " ", "-", " ").Replace(s)
	words := strings.Fields(s)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

func buildProviderToolCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool, widget core.DashboardWidget) ([]string, map[string]bool) {
	allTools, usedKeys := collectProviderToolMix(snap)
	if len(allTools) == 0 {
		return nil, nil
	}
	tools, hiddenCount := limitToolMix(allTools, expanded, 4)
	toolColors := buildToolColorMap(allTools, snap.AccountID)
	totalCalls := float64(0)
	for _, tool := range allTools {
		totalCalls += tool.count
	}
	if totalCalls <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	headingName := "Tool Usage"
	if widget.ToolCompositionHeading != "" {
		headingName = widget.ToolCompositionHeading
	}
	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(headingName) + "  " + dimStyle.Render(shortCompact(totalCalls)+" calls"),
		"  " + renderToolMixBar(allTools, totalCalls, barW, toolColors),
	}
	for idx, tool := range tools {
		if tool.count <= 0 {
			continue
		}
		pct := tool.count / totalCalls * 100
		label := tool.name
		colorDot := lipgloss.NewStyle().Foreground(colorForTool(toolColors, tool.name)).Render("■")
		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		lines = append(lines, renderDotLeaderRow(fmt.Sprintf("%s %d %s", colorDot, idx+1, label), fmt.Sprintf("%2.0f%% %s calls", pct, shortCompact(tool.count)), innerW))
	}
	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more tools (Ctrl+O)", hiddenCount)))
	}
	return lines, usedKeys
}

func collectProviderToolMix(snap core.UsageSnapshot) ([]toolMixEntry, map[string]bool) {
	entries, usedKeys := core.ExtractInterfaceClientBreakdown(snap)
	tools := make([]toolMixEntry, 0, len(entries))
	for _, entry := range entries {
		tools = append(tools, toolMixEntry{name: entry.Name, count: entry.Requests})
	}
	return tools, usedKeys
}

func sortToolMixEntries(tools []toolMixEntry) {
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].count == tools[j].count {
			return tools[i].name < tools[j].name
		}
		return tools[i].count > tools[j].count
	})
}

func limitToolMix(tools []toolMixEntry, expanded bool, maxVisible int) ([]toolMixEntry, int) {
	if expanded || maxVisible <= 0 || len(tools) <= maxVisible {
		return tools, 0
	}
	return tools[:maxVisible], len(tools) - maxVisible
}

func buildToolColorMap(tools []toolMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(tools))
	if len(tools) == 0 {
		return colors
	}
	base := stablePaletteOffset("tool", providerID)
	for i, tool := range tools {
		colors[tool.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForTool(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("tool:"+name, "tool")
}

func buildProviderLanguageCompositionLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allLangs, usedKeys := collectProviderLanguageMix(snap)
	if len(allLangs) == 0 {
		return nil, usedKeys
	}
	langs, hiddenCount := limitToolMix(allLangs, expanded, 6)
	langColors := buildLangColorMap(allLangs, snap.AccountID)
	totalReqs := float64(0)
	for _, lang := range allLangs {
		totalReqs += lang.count
	}
	if totalReqs <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Language") + "  " + dimStyle.Render(shortCompact(totalReqs)+" req"),
		"  " + renderToolMixBar(allLangs, totalReqs, barW, langColors),
	}
	for idx, lang := range langs {
		if lang.count <= 0 {
			continue
		}
		pct := lang.count / totalReqs * 100
		label := lang.name
		colorDot := lipgloss.NewStyle().Foreground(colorForTool(langColors, lang.name)).Render("■")
		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		lines = append(lines, renderDotLeaderRow(fmt.Sprintf("%s %d %s", colorDot, idx+1, label), fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(lang.count)), innerW))
	}
	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more languages (Ctrl+O)", hiddenCount)))
	}
	return lines, usedKeys
}

func collectProviderLanguageMix(snap core.UsageSnapshot) ([]toolMixEntry, map[string]bool) {
	languageUsage, usedKeys := core.ExtractLanguageUsage(snap)
	if len(languageUsage) == 0 {
		return nil, usedKeys
	}
	langs := make([]toolMixEntry, 0, len(languageUsage))
	for _, language := range languageUsage {
		langs = append(langs, toolMixEntry{name: language.Name, count: language.Requests})
	}
	return langs, usedKeys
}

func buildLangColorMap(langs []toolMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(langs))
	if len(langs) == 0 {
		return colors
	}
	base := stablePaletteOffset("lang", providerID)
	for i, lang := range langs {
		colors[lang.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func buildProviderCodeStatsLines(snap core.UsageSnapshot, widget core.DashboardWidget, innerW int) ([]string, map[string]bool) {
	cs := widget.CodeStatsMetrics
	usedKeys := make(map[string]bool)
	getVal := func(key string) float64 {
		if key == "" {
			return 0
		}
		if metric, ok := snap.Metrics[key]; ok && metric.Used != nil {
			usedKeys[key] = true
			return *metric.Used
		}
		return 0
	}

	added := getVal(cs.LinesAdded)
	removed := getVal(cs.LinesRemoved)
	files := getVal(cs.FilesChanged)
	commits := getVal(cs.Commits)
	aiPct := getVal(cs.AIPercent)
	prompts := getVal(cs.Prompts)

	if added <= 0 && removed <= 0 && commits <= 0 && files <= 0 {
		return nil, usedKeys
	}

	parts := []string{}
	if files > 0 {
		parts = append(parts, shortCompact(files)+" files")
	}
	if added > 0 || removed > 0 {
		parts = append(parts, shortCompact(added+removed)+" lines")
	}
	heading := lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Code Statistics")
	if len(parts) > 0 {
		heading += "  " + dimStyle.Render(strings.Join(parts, " · "))
	}
	lines := []string{heading}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	if added > 0 || removed > 0 {
		total := added + removed
		addedW := int(math.Round(added / total * float64(barW)))
		if addedW < 1 && added > 0 {
			addedW = 1
		}
		removedW := barW - addedW
		bar := lipgloss.NewStyle().Foreground(colorGreen).Render(strings.Repeat("█", addedW)) +
			lipgloss.NewStyle().Foreground(colorRed).Render(strings.Repeat("█", removedW))
		lines = append(lines, "  "+bar)
		lines = append(lines, renderDotLeaderRow(
			fmt.Sprintf("%s +%s added", lipgloss.NewStyle().Foreground(colorGreen).Render("■"), shortCompact(added)),
			fmt.Sprintf("%s -%s removed", lipgloss.NewStyle().Foreground(colorRed).Render("■"), shortCompact(removed)),
			innerW,
		))
	}
	if files > 0 {
		lines = append(lines, renderDotLeaderRow("Files Changed", shortCompact(files)+" files", innerW))
	}
	if commits > 0 {
		label := shortCompact(commits) + " commits"
		if aiPct > 0 {
			label += fmt.Sprintf(" · %.0f%% AI", aiPct)
		}
		lines = append(lines, renderDotLeaderRow("Commits", label, innerW))
	}
	if aiPct > 0 {
		filled := int(math.Round(aiPct / 100 * float64(barW)))
		if filled < 1 && aiPct > 0 {
			filled = 1
		}
		empty := barW - filled
		if empty < 0 {
			empty = 0
		}
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorBlue).Render(strings.Repeat("█", filled))+
			lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("░", empty)))
	}
	if prompts > 0 {
		lines = append(lines, renderDotLeaderRow("Prompts", shortCompact(prompts)+" total", innerW))
	}
	return lines, usedKeys
}

func buildActualToolUsageLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	rawTools, usedKeys := core.ExtractActualToolUsage(snap)
	if len(rawTools) == 0 {
		return nil, usedKeys
	}
	allTools := make([]toolMixEntry, 0, len(rawTools))
	totalCalls := float64(0)
	for _, rawTool := range rawTools {
		allTools = append(allTools, toolMixEntry{name: rawTool.RawName, count: rawTool.Calls})
		totalCalls += rawTool.Calls
	}
	if totalCalls <= 0 {
		return nil, nil
	}
	sortToolMixEntries(allTools)
	displayLimit := 6
	if expanded {
		displayLimit = len(allTools)
	}
	visibleTools := allTools
	hiddenCount := 0
	if len(allTools) > displayLimit {
		visibleTools = allTools[:displayLimit]
		hiddenCount = len(allTools) - displayLimit
	}
	toolColors := buildToolColorMap(allTools, snap.AccountID)
	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}
	headerSuffix := shortCompact(totalCalls) + " calls"
	if metric, ok := snap.Metrics["tool_success_rate"]; ok && metric.Used != nil {
		headerSuffix += fmt.Sprintf(" · %.0f%% ok", *metric.Used)
	}
	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Tool Usage") + "  " + dimStyle.Render(headerSuffix),
		"  " + renderToolMixBar(allTools, totalCalls, barW, toolColors),
	}
	for idx, tool := range visibleTools {
		if tool.count <= 0 {
			continue
		}
		pct := tool.count / totalCalls * 100
		label := tool.name
		colorDot := lipgloss.NewStyle().Foreground(colorForTool(toolColors, tool.name)).Render("■")
		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		lines = append(lines, renderDotLeaderRow(fmt.Sprintf("%s %d %s", colorDot, idx+1, label), fmt.Sprintf("%2.0f%% %s calls", pct, shortCompact(tool.count)), innerW))
	}
	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more tools (Ctrl+O)", hiddenCount)))
	}
	return lines, usedKeys
}

func buildMCPUsageLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	type funcEntry struct {
		name  string
		calls float64
	}
	type serverEntry struct {
		name  string
		calls float64
		funcs []funcEntry
	}

	rawServers, usedKeys := core.ExtractMCPUsage(snap)
	servers := make([]serverEntry, 0, len(rawServers))
	totalCalls := float64(0)
	for _, rawServer := range rawServers {
		server := serverEntry{name: prettifyMCPServerName(rawServer.RawName), calls: rawServer.Calls}
		for _, rawFunc := range rawServer.Functions {
			server.funcs = append(server.funcs, funcEntry{name: prettifyMCPFunctionName(rawFunc.RawName), calls: rawFunc.Calls})
		}
		servers = append(servers, server)
		totalCalls += server.calls
	}
	if len(servers) == 0 || totalCalls <= 0 {
		return nil, usedKeys
	}

	headerSuffix := shortCompact(totalCalls) + " calls · " + fmt.Sprintf("%d servers", len(servers))
	allEntries := make([]toolMixEntry, 0, len(servers))
	for _, server := range servers {
		allEntries = append(allEntries, toolMixEntry{name: server.name, count: server.calls})
	}
	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}
	toolColors := buildToolColorMap(allEntries, snap.AccountID)
	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("MCP Usage") + "  " + dimStyle.Render(headerSuffix),
		"  " + renderToolMixBar(allEntries, totalCalls, barW, toolColors),
	}

	displayLimit := 6
	if expanded {
		displayLimit = len(servers)
	}
	visible := servers
	if len(visible) > displayLimit {
		visible = visible[:displayLimit]
	}

	for idx, server := range visible {
		pct := server.calls / totalCalls * 100
		colorDot := lipgloss.NewStyle().Foreground(colorForTool(toolColors, server.name)).Render("■")
		lines = append(lines, renderDotLeaderRow(fmt.Sprintf("%s %d %s", colorDot, idx+1, server.name), fmt.Sprintf("%2.0f%% %s calls", pct, shortCompact(server.calls)), innerW))
		maxFuncs := 3
		if expanded {
			maxFuncs = len(server.funcs)
		}
		if len(server.funcs) < maxFuncs {
			maxFuncs = len(server.funcs)
		}
		for j := 0; j < maxFuncs; j++ {
			fn := server.funcs[j]
			lines = append(lines, renderDotLeaderRow("    "+fn.name, fmt.Sprintf("%s calls", shortCompact(fn.calls)), innerW))
		}
		if !expanded && len(server.funcs) > 3 {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("    + %d more (Ctrl+O)", len(server.funcs)-3)))
		}
	}
	if !expanded && len(servers) > displayLimit {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more servers (Ctrl+O)", len(servers)-displayLimit)))
	}
	return lines, usedKeys
}
