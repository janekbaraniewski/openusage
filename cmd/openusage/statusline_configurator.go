package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
)

// statuslineChoices is the result of the interactive statusline configurator.
type statuslineChoices struct {
	segments  []string // enabled segment keys, in render order
	color     bool
	offline   bool // true = embedded pricing, false = live pricing
	cancelled bool
}

// options builds the statuslineOptions the install path persists.
func (c statuslineChoices) options() statuslineOptions {
	return statuslineOptions{
		offline:       c.offline,
		mode:          string(claude_code.CostModeCalculate),
		color:         c.color,
		contextMedium: 50,
		contextHigh:   80,
		segments:      c.segments,
	}
}

// sampleStatuslineValues is representative data for the live preview.
func sampleStatuslineValues() statuslineValues {
	return statuslineValues{
		model:        "Opus 4.8",
		sessionCost:  12.40,
		todayCost:    6.79,
		blockCost:    3.40,
		blockLeft:    2*time.Hour + 41*time.Minute,
		burn:         1.20,
		haveBlock:    true,
		fiveHourPct:  15,
		haveFiveHour: true,
		contextTok:   96000,
		ctxPct:       48,
	}
}

// slConfigModel is a single-screen, live-preview configurator for the Claude
// Code statusline: toggle which segments show and a couple of options, with the
// rendered line always on top.
type slConfigModel struct {
	segs    map[string]bool
	color   bool
	offline bool

	cursor    int
	rows      []slRow
	done      bool
	cancelled bool

	accent  lipgloss.Style
	dim     lipgloss.Style
	sel     lipgloss.Style
	heading lipgloss.Style
}

type slRowKind int

const (
	slRowSegment slRowKind = iota
	slRowCycle
	slRowApply
)

type slRow struct {
	kind  slRowKind
	id    string
	label string
}

func newStatuslineConfigModel() slConfigModel {
	segs := map[string]bool{}
	for _, s := range statuslineSegmentDefs {
		segs[s.key] = true
	}
	m := slConfigModel{
		segs:    segs,
		color:   true,
		offline: true,
		accent:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8433")).Bold(true),
		dim:     lipgloss.NewStyle().Foreground(lipgloss.Color("#828592")),
		heading: lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8433")).Bold(true),
		sel:     lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true),
	}
	rows := make([]slRow, 0, len(statuslineSegmentDefs)+3)
	for _, s := range statuslineSegmentDefs {
		rows = append(rows, slRow{slRowSegment, s.key, s.label})
	}
	rows = append(rows,
		slRow{slRowCycle, "color", "Color"},
		slRow{slRowCycle, "pricing", "Pricing"},
		slRow{slRowApply, "apply", "Apply"},
	)
	m.rows = rows
	return m
}

func (m slConfigModel) Init() tea.Cmd { return nil }

func (m slConfigModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "left", "h", "right", "l":
			m.cycle()
		case " ", "x":
			m.toggle()
		case "enter":
			switch m.rows[m.cursor].kind {
			case slRowApply:
				m.done = true
				return m, tea.Quit
			case slRowCycle:
				m.cycle()
			default:
				m.toggle()
			}
		}
	}
	return m, nil
}

func (m *slConfigModel) toggle() {
	row := m.rows[m.cursor]
	if row.kind == slRowSegment {
		m.segs[row.id] = !m.segs[row.id]
	}
}

func (m *slConfigModel) cycle() {
	row := m.rows[m.cursor]
	if row.kind != slRowCycle {
		return
	}
	switch row.id {
	case "color":
		m.color = !m.color
	case "pricing":
		m.offline = !m.offline
	}
}

// choices snapshots the current selection.
func (m slConfigModel) choices() statuslineChoices {
	var segs []string
	for _, s := range statuslineSegmentDefs {
		if m.segs[s.key] {
			segs = append(segs, s.key)
		}
	}
	return statuslineChoices{
		segments:  segs,
		color:     m.color,
		offline:   m.offline,
		cancelled: m.cancelled,
	}
}

func (m slConfigModel) preview() string {
	opts := m.choices().options()
	out := assembleStatusline(sampleStatuslineValues(), opts)
	if strings.TrimSpace(out) == "" {
		return m.dim.Render("(select at least one segment)")
	}
	return out
}

func (m slConfigModel) cycleValue(id string) string {
	switch id {
	case "color":
		if m.color {
			return "on"
		}
		return "off"
	case "pricing":
		if m.offline {
			return "embedded (instant)"
		}
		return "live (network)"
	}
	return ""
}

func (m slConfigModel) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.heading.Render("Configure your Claude Code statusline"))
	b.WriteString("\n\n")
	b.WriteString(m.dim.Render("preview "))
	b.WriteString(m.preview())
	b.WriteString("\n\n")

	for i, row := range m.rows {
		cursor := "  "
		if i == m.cursor {
			cursor = m.accent.Render("› ")
		}
		var line string
		switch row.kind {
		case slRowSegment:
			line = checkbox(m.segs[row.id]) + " " + row.label
		case slRowCycle:
			line = fmt.Sprintf("%-14s %s", row.label, m.accent.Render("‹ "+m.cycleValue(row.id)+" ›"))
		case slRowApply:
			line = m.accent.Render("[ Apply ]")
		}
		if i == m.cursor && row.kind != slRowApply {
			line = m.sel.Render(line)
		}
		b.WriteString(cursor + line + "\n")
	}

	b.WriteString("\n")
	b.WriteString(m.dim.Render("↑/↓ move · ←/→ change · space toggle · enter apply · q cancel"))
	return b.String()
}

// runStatuslineConfigurator runs the interactive configurator and returns the
// chosen settings (or cancelled).
func runStatuslineConfigurator() (statuslineChoices, error) {
	m, err := tea.NewProgram(newStatuslineConfigModel()).Run()
	if err != nil {
		return statuslineChoices{}, err
	}
	return m.(slConfigModel).choices(), nil
}
