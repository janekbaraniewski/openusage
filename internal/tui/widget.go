package tui

import (
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

// no gaps to maximize space

// 2 border + 2 padding
