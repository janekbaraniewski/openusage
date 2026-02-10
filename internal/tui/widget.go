package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Panel struct {
	Title   string         // displayed in the top border
	Icon    string         // emoji icon before title
	Content string         // pre-rendered body text
	Span    int            // how many grid columns this panel occupies (1 or 2)
	Color   lipgloss.Color // accent color for the border
}

type PanelRow struct {
	Panels []Panel
	Weight int // relative height weight (0 → 1)
}

func RenderFixedGrid(rows []PanelRow, totalW, totalH int) string {
	if len(rows) == 0 || totalW < 10 || totalH < 3 {
		return ""
	}

	var activeRows []PanelRow
	for _, r := range rows {
		if len(r.Panels) > 0 {
			activeRows = append(activeRows, r)
		}
	}
	if len(activeRows) == 0 {
		return ""
	}

	totalWeight := 0
	for i := range activeRows {
		if activeRows[i].Weight <= 0 {
			activeRows[i].Weight = 1
		}
		totalWeight += activeRows[i].Weight
	}

	rowGap := 0 // no gaps to maximize space
	availH := totalH - rowGap*(len(activeRows)-1)
	if availH < len(activeRows)*3 {
		availH = len(activeRows) * 3
	}

	rowHeights := make([]int, len(activeRows))
	assigned := 0
	for i, r := range activeRows {
		if i == len(activeRows)-1 {
			rowHeights[i] = availH - assigned
		} else {
			rowHeights[i] = (availH * r.Weight) / totalWeight
		}
		if rowHeights[i] < 3 {
			rowHeights[i] = 3
		}
		assigned += rowHeights[i]
	}

	var rendered []string
	for i, row := range activeRows {
		rStr := renderFixedRow(row, totalW, rowHeights[i])
		rendered = append(rendered, rStr)
		_ = i
	}

	result := strings.Join(rendered, "\n")

	lines := strings.Split(result, "\n")
	if len(lines) > totalH {
		lines = lines[:totalH]
	}
	for len(lines) < totalH {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func renderFixedRow(row PanelRow, totalW, h int) string {
	n := len(row.Panels)
	if n == 0 {
		return ""
	}

	totalSpan := 0
	for _, p := range row.Panels {
		s := p.Span
		if s < 1 {
			s = 1
		}
		totalSpan += s
	}

	gap := 1
	availW := totalW - gap*(n-1)
	if availW < n*8 {
		availW = n * 8
	}

	var parts []string
	for _, p := range row.Panels {
		s := p.Span
		if s < 1 {
			s = 1
		}
		pw := (availW * s) / totalSpan
		if pw < 8 {
			pw = 8
		}
		parts = append(parts, renderFixedPanel(p, pw, h))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, intersperseStr(parts, strings.Repeat(" ", gap))...)
}

func renderFixedPanel(p Panel, w, h int) string {
	if w < 6 {
		w = 6
	}
	if h < 3 {
		h = 3
	}
	innerW := w - 4 // 2 border + 2 padding

	titleStr := ""
	if p.Icon != "" {
		titleStr = p.Icon + " "
	}
	titleStr += p.Title

	borderColor := p.Color
	if borderColor == "" {
		borderColor = colorSurface1
	}
	bStyle := lipgloss.NewStyle().Foreground(borderColor)
	titleRendered := lipgloss.NewStyle().Bold(true).Foreground(borderColor).Render(titleStr)
	titleW := lipgloss.Width(titleRendered)

	topLeft := bStyle.Render("┌─ ")
	remaining := w - lipgloss.Width(topLeft) - titleW - 2
	if remaining < 1 {
		remaining = 1
	}
	topRight := bStyle.Render(" " + strings.Repeat("─", remaining) + "┐")
	topLine := topLeft + titleRendered + topRight

	contentLines := strings.Split(p.Content, "\n")
	for i, line := range contentLines {
		if lipgloss.Width(line) > innerW {
			runes := []rune(line)
			if len(runes) > innerW {
				contentLines[i] = string(runes[:innerW-1]) + "…"
			}
		}
	}

	bodyH := h - 2
	if bodyH < 1 {
		bodyH = 1
	}
	for len(contentLines) < bodyH {
		contentLines = append(contentLines, "")
	}
	if len(contentLines) > bodyH {
		contentLines = contentLines[:bodyH]
	}

	var bodyLines []string
	for _, line := range contentLines {
		lineW := lipgloss.Width(line)
		pad := innerW - lineW
		if pad < 0 {
			pad = 0
		}
		bodyLines = append(bodyLines,
			bStyle.Render("│")+" "+line+strings.Repeat(" ", pad)+" "+bStyle.Render("│"))
	}

	bottomLine := bStyle.Render("└" + strings.Repeat("─", w-2) + "┘")

	return topLine + "\n" + strings.Join(bodyLines, "\n") + "\n" + bottomLine
}

func RenderSubTabBar(labels []string, active int, w int) string {
	var parts []string
	for i, label := range labels {
		tabLabel := fmt.Sprintf(" %d:%s ", i+1, label)
		if i == active {
			parts = append(parts, analyticsSubTabActiveStyle.Render(tabLabel))
		} else {
			parts = append(parts, analyticsSubTabInactiveStyle.Render(tabLabel))
		}
	}

	bar := strings.Join(parts, "")
	barW := lipgloss.Width(bar)
	if barW < w {
		bar += strings.Repeat(" ", w-barW)
	}
	return bar
}

func intersperseStr(items []string, sep string) []string {
	if len(items) <= 1 {
		return items
	}
	result := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			result = append(result, sep)
		}
		result = append(result, item)
	}
	return result
}

func panelContent(lines ...string) string {
	return strings.Join(lines, "\n")
}

func padLine(line string, w int) string {
	lineW := lipgloss.Width(line)
	if lineW >= w {
		return line
	}
	return line + strings.Repeat(" ", w-lineW)
}
