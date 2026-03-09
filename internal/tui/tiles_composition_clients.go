package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func collectInterfaceAsClients(snap core.UsageSnapshot) ([]clientMixEntry, map[string]bool) {
	entries, usedKeys := core.ExtractInterfaceClientBreakdown(snap)
	clients := make([]clientMixEntry, 0, len(entries))
	for _, entry := range entries {
		clients = append(clients, clientMixEntry{
			name:       entry.Name,
			requests:   entry.Requests,
			seriesKind: entry.SeriesKind,
			series:     entry.Series,
		})
	}
	return clients, usedKeys
}

func buildProviderClientCompositionLinesWithWidget(snap core.UsageSnapshot, innerW int, expanded bool, widget core.DashboardWidget) ([]string, map[string]bool) {
	allClients, usedKeys := collectProviderClientMix(snap)
	if widget.ClientCompositionIncludeInterfaces {
		ifaceClients, ifaceKeys := collectInterfaceAsClients(snap)
		if len(ifaceClients) > 0 {
			allClients = ifaceClients
			for key, value := range ifaceKeys {
				usedKeys[key] = value
			}
		}
	}
	if len(allClients) == 0 {
		return nil, nil
	}

	clients, hiddenCount := limitClientMix(allClients, expanded, 4)
	clientColors := buildClientColorMap(allClients, snap.AccountID)
	mode, total := selectClientMixMode(allClients)
	if total <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	headingName := widget.ClientCompositionHeading
	if headingName == "" {
		headingName = "Client Burn"
		if mode == "requests" || mode == "sessions" {
			headingName = "Client Activity"
		}
	}
	headerSuffix := shortCompact(total) + " tok"
	if mode == "requests" {
		headerSuffix = shortCompact(total) + " req"
	} else if mode == "sessions" {
		headerSuffix = shortCompact(total) + " sess"
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render(headingName) + "  " + dimStyle.Render(headerSuffix),
		"  " + renderClientMixBar(allClients, total, barW, clientColors, mode),
	}

	for idx, client := range clients {
		value := clientDisplayValue(client, mode)
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		label := prettifyClientName(client.name)
		colorDot := lipgloss.NewStyle().Foreground(colorForClient(clientColors, client.name)).Render("■")
		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)
		valueStr := fmt.Sprintf("%2.0f%% %s tok", pct, shortCompact(value))
		switch mode {
		case "requests":
			valueStr = fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(value))
			if client.sessions > 0 {
				valueStr += fmt.Sprintf(" · %s sess", shortCompact(client.sessions))
			}
		case "sessions":
			valueStr = fmt.Sprintf("%2.0f%% %s sess", pct, shortCompact(value))
		default:
			if client.requests > 0 {
				valueStr += fmt.Sprintf(" · %s req", shortCompact(client.requests))
			} else if client.sessions > 0 {
				valueStr += fmt.Sprintf(" · %s sess", shortCompact(client.sessions))
			}
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}

	trendEntries := limitClientTrendEntries(clients, expanded)
	if len(trendEntries) > 0 {
		lines = append(lines, dimStyle.Render("  Trend (daily by client)"))
		labelW := 12
		if innerW < 55 {
			labelW = 10
		}
		sparkW := innerW - labelW - 5
		if sparkW < 10 {
			sparkW = 10
		}
		if sparkW > 28 {
			sparkW = 28
		}

		for _, client := range trendEntries {
			values := make([]float64, 0, len(client.series))
			for _, point := range client.series {
				values = append(values, point.Value)
			}
			if len(values) < 2 {
				continue
			}
			label := truncateToWidth(prettifyClientName(client.name), labelW)
			spark := RenderSparkline(values, sparkW, colorForClient(clientColors, client.name))
			lines = append(lines, fmt.Sprintf("  %s %s",
				lipgloss.NewStyle().Foreground(colorSubtext).Width(labelW).Render(label),
				spark,
			))
		}
	}
	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more clients (Ctrl+O)", hiddenCount)))
	}
	return lines, usedKeys
}

func buildProviderProjectBreakdownLines(snap core.UsageSnapshot, innerW int, expanded bool) ([]string, map[string]bool) {
	allProjects, usedKeys := collectProviderProjectMix(snap)
	if len(allProjects) == 0 {
		return nil, nil
	}

	projects, hiddenCount := limitProjectMix(allProjects, expanded, 6)
	projectColors := buildProjectColorMap(allProjects, snap.AccountID)
	totalRequests := float64(0)
	for _, project := range allProjects {
		totalRequests += project.requests
	}
	if totalRequests <= 0 {
		return nil, nil
	}

	barW := innerW - 2
	if barW < 12 {
		barW = 12
	}
	if barW > 40 {
		barW = 40
	}

	barEntries := make([]toolMixEntry, 0, len(allProjects))
	for _, project := range allProjects {
		barEntries = append(barEntries, toolMixEntry{name: project.name, count: project.requests})
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Project Breakdown") + "  " + dimStyle.Render(shortCompact(totalRequests)+" req"),
		"  " + renderToolMixBar(barEntries, totalRequests, barW, projectColors),
	}

	for idx, project := range projects {
		if project.requests <= 0 {
			continue
		}
		pct := project.requests / totalRequests * 100
		label := project.name
		colorDot := lipgloss.NewStyle().Foreground(colorForProject(projectColors, project.name)).Render("■")
		maxLabelLen := tableLabelMaxLen(innerW)
		if len(label) > maxLabelLen {
			label = label[:maxLabelLen-1] + "…"
		}
		displayLabel := fmt.Sprintf("%s %d %s", colorDot, idx+1, label)
		valueStr := fmt.Sprintf("%2.0f%% %s req", pct, shortCompact(project.requests))
		if project.requests1d > 0 {
			valueStr += fmt.Sprintf(" · today %s", shortCompact(project.requests1d))
		}
		lines = append(lines, renderDotLeaderRow(displayLabel, valueStr, innerW))
	}
	if hiddenCount > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("+ %d more projects (Ctrl+O)", hiddenCount)))
	}
	return lines, usedKeys
}

func collectProviderProjectMix(snap core.UsageSnapshot) ([]projectMixEntry, map[string]bool) {
	projectUsage, usedKeys := core.ExtractProjectUsage(snap)
	if len(projectUsage) == 0 {
		return nil, usedKeys
	}
	projects := make([]projectMixEntry, 0, len(projectUsage))
	for _, project := range projectUsage {
		projects = append(projects, projectMixEntry{
			name:       project.Name,
			requests:   project.Requests,
			requests1d: project.Requests1d,
			series:     project.Series,
		})
	}
	return projects, usedKeys
}

func limitProjectMix(projects []projectMixEntry, expanded bool, maxVisible int) ([]projectMixEntry, int) {
	if expanded || maxVisible <= 0 || len(projects) <= maxVisible {
		return projects, 0
	}
	return projects[:maxVisible], len(projects) - maxVisible
}

func buildProjectColorMap(projects []projectMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(projects))
	if len(projects) == 0 {
		return colors
	}
	base := stablePaletteOffset("project", providerID)
	for i, project := range projects {
		colors[project.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForProject(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("project:"+name, "project")
}

func collectProviderClientMix(snap core.UsageSnapshot) ([]clientMixEntry, map[string]bool) {
	entries, usedKeys := core.ExtractClientBreakdown(snap)
	clients := make([]clientMixEntry, 0, len(entries))
	for _, entry := range entries {
		clients = append(clients, clientMixEntry{
			name:       entry.Name,
			total:      entry.Total,
			input:      entry.Input,
			output:     entry.Output,
			cached:     entry.Cached,
			reasoning:  entry.Reasoning,
			requests:   entry.Requests,
			sessions:   entry.Sessions,
			seriesKind: entry.SeriesKind,
			series:     entry.Series,
		})
	}
	return clients, usedKeys
}

func clientTokenValue(client clientMixEntry) float64 {
	if client.total > 0 {
		return client.total
	}
	if client.input > 0 || client.output > 0 || client.cached > 0 || client.reasoning > 0 {
		return client.input + client.output + client.cached + client.reasoning
	}
	return 0
}

func clientMixValue(client clientMixEntry) float64 {
	if value := clientTokenValue(client); value > 0 {
		return value
	}
	if client.requests > 0 {
		return client.requests
	}
	if len(client.series) > 0 {
		return sumSeriesValues(client.series)
	}
	return 0
}

func clientDisplayValue(client clientMixEntry, mode string) float64 {
	switch mode {
	case "sessions":
		return client.sessions
	case "requests":
		if client.requests > 0 {
			return client.requests
		}
		return sumSeriesValues(client.series)
	default:
		return clientMixValue(client)
	}
}

func selectClientMixMode(clients []clientMixEntry) (string, float64) {
	totalTokens := float64(0)
	totalRequests := float64(0)
	totalSessions := float64(0)
	for _, client := range clients {
		totalTokens += clientTokenValue(client)
		totalRequests += client.requests
		totalSessions += client.sessions
	}
	if totalTokens > 0 {
		return "tokens", totalTokens
	}
	if totalRequests > 0 {
		return "requests", totalRequests
	}
	return "sessions", totalSessions
}

func sumSeriesValues(points []core.TimePoint) float64 {
	total := float64(0)
	for _, point := range points {
		total += point.Value
	}
	return total
}

func mergeSeriesByDay(seriesByClient map[string]map[string]float64, client string, points []core.TimePoint) {
	if client == "" || len(points) == 0 {
		return
	}
	if seriesByClient[client] == nil {
		seriesByClient[client] = make(map[string]float64)
	}
	for _, point := range points {
		if point.Date != "" {
			seriesByClient[client][point.Date] += point.Value
		}
	}
}

func limitClientMix(clients []clientMixEntry, expanded bool, maxVisible int) ([]clientMixEntry, int) {
	if expanded || maxVisible <= 0 || len(clients) <= maxVisible {
		return clients, 0
	}
	return clients[:maxVisible], len(clients) - maxVisible
}

func limitClientTrendEntries(clients []clientMixEntry, expanded bool) []clientMixEntry {
	maxVisible := 2
	if expanded {
		maxVisible = 4
	}
	trend := make([]clientMixEntry, 0, maxVisible)
	for _, client := range clients {
		if len(client.series) < 2 {
			continue
		}
		trend = append(trend, client)
		if len(trend) >= maxVisible {
			break
		}
	}
	return trend
}

func prettifyClientName(name string) string {
	switch name {
	case "cli":
		return "CLI Agents"
	case "ide":
		return "IDE"
	case "exec":
		return "Exec"
	case "desktop_app":
		return "Desktop App"
	case "other":
		return "Other"
	case "composer":
		return "Composer"
	case "human":
		return "Human"
	case "tab":
		return "Tab Completion"
	}
	parts := strings.Split(name, "_")
	for i := range parts {
		switch parts[i] {
		case "cli":
			parts[i] = "CLI"
		case "ide":
			parts[i] = "IDE"
		case "api":
			parts[i] = "API"
		default:
			parts[i] = titleCase(parts[i])
		}
	}
	return strings.Join(parts, " ")
}

func buildClientColorMap(clients []clientMixEntry, providerID string) map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color, len(clients))
	if len(clients) == 0 {
		return colors
	}
	base := stablePaletteOffset("client", providerID)
	for i, client := range clients {
		colors[client.name] = distributedPaletteColor(base, i)
	}
	return colors
}

func colorForClient(colors map[string]lipgloss.Color, name string) lipgloss.Color {
	if color, ok := colors[name]; ok {
		return color
	}
	return stableModelColor("client:"+name, "client")
}
