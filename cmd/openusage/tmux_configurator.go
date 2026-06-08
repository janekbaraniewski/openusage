package main

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/samber/lo"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/tmux"
)

// tmuxChoices is the result of the interactive configurator.
type tmuxChoices struct {
	position   string   // right | left | both
	mode       string   // dynamic | pinned | several
	pinned     string   // provider id (mode == pinned)
	several    []string // provider ids (mode == several)
	components []string // selected segment component keys
	icons      string   // emoji | real
	cancelled  bool
}

// providersForMode returns the providers that get a pinned segment for the
// chosen mode (none for dynamic).
func (c tmuxChoices) providersForMode() []string {
	switch c.mode {
	case "pinned":
		if c.pinned != "" {
			return []string{c.pinned}
		}
	case "several":
		return c.several
	}
	return nil
}

var (
	cfgPositions = []string{"right", "left", "both"}
	cfgModes     = []string{"dynamic", "pinned", "several"}
	cfgIcons     = []string{"emoji", "real"}
)

// configuratorModel is a single-screen, live-preview status-bar configurator.
// Everything (position, which tools, the segment components, icons) is editable
// on one screen with the rendered preview always on top.
type configuratorModel struct {
	posIdx   int
	modeIdx  int
	iconsIdx int
	pinIdx   int
	provIDs  []string
	several  map[string]bool
	comps    map[string]bool

	cursor    int
	rows      []cfgRow
	width     int
	done      bool
	cancelled bool

	accent  lipgloss.Style
	dim     lipgloss.Style
	sel     lipgloss.Style
	heading lipgloss.Style
}

type cfgRowKind int

const (
	rowCycle cfgRowKind = iota
	rowComponentToggle
	rowProviderToggle
	rowApply
)

type cfgRow struct {
	kind  cfgRowKind
	id    string // setting name, component key, or provider id
	label string
}

func newConfiguratorModel() configuratorModel {
	ids := lo.Map(providers.AllProviders(), func(p core.UsageProvider, _ int) string { return p.ID() })
	sort.Strings(ids)

	m := configuratorModel{
		provIDs: ids,
		several: map[string]bool{"claude_code": true},
		comps:   map[string]bool{"icon": true, "block": true, "today": true},
		accent:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8433")).Bold(true),
		dim:     lipgloss.NewStyle().Foreground(lipgloss.Color("#828592")),
		heading: lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8433")).Bold(true),
		sel:     lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true),
	}
	if i := lo.IndexOf(ids, "claude_code"); i >= 0 {
		m.pinIdx = i
	}
	// Default to real logos when the icon font is already installed, otherwise
	// emoji (zero setup).
	if tmux.FontInstalled() {
		m.iconsIdx = lo.IndexOf(cfgIcons, "real")
	}
	m.rebuildRows()
	return m
}

// rebuildRows recomputes the visible, focusable rows based on the current mode
// (the pinned/provider rows appear conditionally), keeping the cursor in range.
func (m *configuratorModel) rebuildRows() {
	rows := []cfgRow{
		{rowCycle, "position", "Position"},
		{rowCycle, "mode", "Which tool(s)"},
	}
	switch cfgModes[m.modeIdx] {
	case "pinned":
		rows = append(rows, cfgRow{rowCycle, "pinned", "Pinned tool"})
	case "several":
		for _, id := range m.provIDs {
			rows = append(rows, cfgRow{rowProviderToggle, id, id})
		}
	}
	rows = append(rows, cfgRow{rowCycle, "icons", "Provider icons"})
	for _, c := range templateComponents {
		rows = append(rows, cfgRow{rowComponentToggle, c.key, c.label})
	}
	rows = append(rows, cfgRow{rowApply, "apply", "Apply"})
	m.rows = rows
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m configuratorModel) Init() tea.Cmd { return nil }

func (m configuratorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
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
		case "left", "h":
			m.cycle(-1)
		case "right", "l":
			m.cycle(1)
		case " ", "x":
			m.toggle()
		case "enter":
			row := m.rows[m.cursor]
			switch row.kind {
			case rowApply:
				m.done = true
				return m, tea.Quit
			case rowComponentToggle, rowProviderToggle:
				m.toggle()
			default:
				m.cycle(1)
			}
		}
	}
	return m, nil
}

// cycle advances a cyclable setting (position/mode/icons/pinned) by dir.
func (m *configuratorModel) cycle(dir int) {
	row := m.rows[m.cursor]
	if row.kind != rowCycle {
		return
	}
	switch row.id {
	case "position":
		m.posIdx = wrap(m.posIdx+dir, len(cfgPositions))
	case "mode":
		m.modeIdx = wrap(m.modeIdx+dir, len(cfgModes))
		m.rebuildRows()
	case "icons":
		m.iconsIdx = wrap(m.iconsIdx+dir, len(cfgIcons))
	case "pinned":
		m.pinIdx = wrap(m.pinIdx+dir, len(m.provIDs))
	}
}

// toggle flips a component or provider checkbox.
func (m *configuratorModel) toggle() {
	row := m.rows[m.cursor]
	switch row.kind {
	case rowComponentToggle:
		m.comps[row.id] = !m.comps[row.id]
	case rowProviderToggle:
		m.several[row.id] = !m.several[row.id]
	}
}

func wrap(i, n int) int {
	if n == 0 {
		return 0
	}
	return ((i % n) + n) % n
}

// choices snapshots the model's current selections.
func (m configuratorModel) choices() tmuxChoices {
	comps := lo.FilterMap(templateComponents, func(c templateComponent, _ int) (string, bool) {
		return c.key, m.comps[c.key]
	})
	several := lo.Filter(m.provIDs, func(id string, _ int) bool { return m.several[id] })
	return tmuxChoices{
		position:   cfgPositions[m.posIdx],
		mode:       cfgModes[m.modeIdx],
		pinned:     m.provIDs[m.pinIdx],
		several:    several,
		components: comps,
		icons:      cfgIcons[m.iconsIdx],
		cancelled:  m.cancelled,
	}
}

// preview renders the live status-bar preview for the current selections. For
// "several" it renders one sample segment per selected provider, joined by the
// separator, matching what the installed snippet produces.
func (m configuratorModel) preview() string {
	tmpl := assembleTemplate(m.choices().components)
	if strings.TrimSpace(tmpl) == "" {
		return m.dim.Render("(select at least one component)")
	}
	var provs []string
	switch cfgModes[m.modeIdx] {
	case "pinned":
		provs = []string{m.provIDs[m.pinIdx]}
	case "several":
		provs = lo.Filter(m.provIDs, func(id string, _ int) bool { return m.several[id] })
	default:
		provs = []string{"claude_code"}
	}
	if len(provs) == 0 {
		return m.dim.Render("(select at least one tool)")
	}
	// Render with the glyph tier matching the icons choice, so the preview shows
	// real provider logos when "real logos (font)" is selected (requires the icon
	// font to be installed and configured in this terminal) and emoji otherwise.
	glyphs := tmux.GlyphTierUnicode
	if cfgIcons[m.iconsIdx] == "real" {
		glyphs = tmux.GlyphTierCustomFont
	}
	segs := lo.FilterMap(provs, func(p string, _ int) (string, bool) {
		ctx := sampleTemplateContextFor(p)
		ctx.Glyphs = glyphs
		out, err := tmux.Render(tmpl, ctx)
		if err != nil {
			return "", false
		}
		return tmux.Preview(out), true
	})
	return strings.Join(segs, m.dim.Render(" │ "))
}

func (m configuratorModel) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder

	b.WriteString(m.heading.Render("Configure your tmux status segment"))
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
		case rowCycle:
			line = fmt.Sprintf("%-16s %s", row.label, m.accent.Render("‹ "+m.cycleValue(row.id)+" ›"))
		case rowComponentToggle:
			line = checkbox(m.comps[row.id]) + " " + row.label
		case rowProviderToggle:
			line = checkbox(m.several[row.id]) + " " + row.label
		case rowApply:
			line = m.accent.Render("[ Apply ]")
		}
		if i == m.cursor && row.kind != rowApply {
			line = m.sel.Render(line)
		}
		b.WriteString(cursor + line + "\n")
	}

	b.WriteString("\n")
	b.WriteString(m.dim.Render("↑/↓ move · ←/→ change · space toggle · enter apply · q cancel"))
	return b.String()
}

func (m configuratorModel) cycleValue(id string) string {
	switch id {
	case "position":
		return cfgPositions[m.posIdx]
	case "mode":
		switch cfgModes[m.modeIdx] {
		case "dynamic":
			return "active tool (auto)"
		case "pinned":
			return "pin one tool"
		case "several":
			return "several, side by side"
		}
	case "icons":
		switch cfgIcons[m.iconsIdx] {
		case "emoji":
			return "emoji (no setup)"
		case "real":
			return "real logos (font)"
		}
	case "pinned":
		return m.provIDs[m.pinIdx]
	}
	return ""
}

func checkbox(on bool) string {
	if on {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#59D4A0")).Render("[x]")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#828592")).Render("[ ]")
}

// runTmuxConfigurator runs the interactive configurator and returns the chosen
// settings (or cancelled).
func runTmuxConfigurator() (tmuxChoices, error) {
	m, err := tea.NewProgram(newConfiguratorModel()).Run()
	if err != nil {
		return tmuxChoices{}, err
	}
	cm := m.(configuratorModel)
	return cm.choices(), nil
}
