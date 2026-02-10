package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ═══════════════════════════════════════════════════════════════════════════
// PANEL GRID — Grafana-style tile layout engine for the TUI
// ═══════════════════════════════════════════════════════════════════════════
//
// A Panel is a single bordered tile containing pre-rendered content.
// Panels are arranged in rows by the grid layout engine.

// Panel represents a single dashboard tile.
type Panel struct {
	Title   string         // displayed in the top border
	Icon    string         // emoji icon before title
	Content string         // pre-rendered body text
	Width   int            // requested width in columns (0 = auto-fill)
	Height  int            // requested height in rows (0 = auto from content)
	Span    int            // how many grid columns this panel occupies (1 or 2)
	Color   lipgloss.Color // accent color for the border
}

// ─── Panel Rendering ────────────────────────────────────────────────────────

// renderPanel draws a single bordered panel tile.
func renderPanel(p Panel, w, h int) string {
	if w < 8 {
		w = 8
	}
	innerW := w - 4 // 2 border + 2 padding

	// ── Title bar ──
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

	// Top border: ┌─ Title ─────────┐
	topLeft := bStyle.Render("┌─ ")
	topRight := " "
	remaining := w - lipgloss.Width(topLeft) - titleW - 2
	if remaining < 1 {
		remaining = 1
	}
	topRight = bStyle.Render(" " + strings.Repeat("─", remaining) + "┐")
	topLine := topLeft + titleRendered + topRight

	// ── Body lines ──
	contentLines := strings.Split(p.Content, "\n")
	// Truncate lines to fit
	for i, line := range contentLines {
		if lipgloss.Width(line) > innerW {
			// Rough truncation — cut to innerW characters
			runes := []rune(line)
			if len(runes) > innerW {
				contentLines[i] = string(runes[:innerW-1]) + "…"
			}
		}
	}

	// Pad or trim to target height
	bodyH := h - 2 // minus top and bottom borders
	if bodyH < 1 {
		bodyH = 1
	}
	for len(contentLines) < bodyH {
		contentLines = append(contentLines, "")
	}
	if len(contentLines) > bodyH {
		contentLines = contentLines[:bodyH]
	}

	// Render body with side borders
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

	// Bottom border: └──────────────────┘
	bottomLine := bStyle.Render("└" + strings.Repeat("─", w-2) + "┘")

	return topLine + "\n" + strings.Join(bodyLines, "\n") + "\n" + bottomLine
}

// ─── Grid Layout ────────────────────────────────────────────────────────────

// PanelRow is a row of panels to be laid out horizontally.
type PanelRow struct {
	Panels []Panel
}

// renderPanelRow renders a row of panels side by side, sharing the total width.
func renderPanelRow(row PanelRow, totalW, rowH int) string {
	n := len(row.Panels)
	if n == 0 {
		return ""
	}

	// Calculate total span
	totalSpan := 0
	for _, p := range row.Panels {
		span := p.Span
		if span < 1 {
			span = 1
		}
		totalSpan += span
	}

	gap := 1 // gap between panels
	availW := totalW - gap*(n-1)
	if availW < n*8 {
		availW = n * 8
	}

	var rendered []string
	for _, p := range row.Panels {
		span := p.Span
		if span < 1 {
			span = 1
		}
		pw := (availW * span) / totalSpan
		if pw < 8 {
			pw = 8
		}
		h := rowH
		if p.Height > 0 && p.Height < h {
			h = p.Height
		}
		rendered = append(rendered, renderPanel(p, pw, h))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, intersperseStr(rendered, strings.Repeat(" ", gap))...)
}

// renderPanelGrid renders a full dashboard grid from multiple rows.
// Each row gets its own height based on the tallest panel in that row.
func renderPanelGrid(rows []PanelRow, totalW int) string {
	var result []string
	for _, row := range rows {
		if len(row.Panels) == 0 {
			continue
		}
		// Determine row height from content
		maxH := 0
		for _, p := range row.Panels {
			h := strings.Count(p.Content, "\n") + 1 + 2 // +2 for borders
			if p.Height > 0 && p.Height > h {
				h = p.Height
			}
			if h > maxH {
				maxH = h
			}
		}
		if maxH < 4 {
			maxH = 4
		}
		if maxH > 30 {
			maxH = 30
		}
		result = append(result, renderPanelRow(row, totalW, maxH))
	}
	return strings.Join(result, "\n")
}

// intersperseStr inserts sep between each element.
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

// ─── Content Builder Helpers ────────────────────────────────────────────────

// panelContent builds pre-rendered content from lines.
func panelContent(lines ...string) string {
	return strings.Join(lines, "\n")
}

// padLine pads a line to target width.
func padLine(line string, w int) string {
	lineW := lipgloss.Width(line)
	if lineW >= w {
		return line
	}
	return line + strings.Repeat(" ", w-lineW)
}
