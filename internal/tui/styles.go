package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/agentusage/internal/core"
)

// ─── Color Palette (Catppuccin Mocha + modern refinements) ──────────────────

var (
	// Base tones
	colorBase     = lipgloss.Color("#1E1E2E") // background
	colorMantle   = lipgloss.Color("#181825") // deeper bg
	colorSurface0 = lipgloss.Color("#313244") // card bg
	colorSurface1 = lipgloss.Color("#45475A") // lighter surface
	colorSurface2 = lipgloss.Color("#525566") // even lighter surface
	colorText     = lipgloss.Color("#CDD6F4") // primary text
	colorSubtext  = lipgloss.Color("#A6ADC8") // secondary text
	colorDim      = lipgloss.Color("#585B70") // muted, borders
	colorOverlay  = lipgloss.Color("#45475A") // selected bg

	// Accents
	colorAccent    = lipgloss.Color("#CBA6F7") // mauve – primary accent
	colorBlue      = lipgloss.Color("#89B4FA") // section headers
	colorSapphire  = lipgloss.Color("#74C7EC") // links, secondary accent
	colorGreen     = lipgloss.Color("#A6E3A1") // OK / healthy
	colorYellow    = lipgloss.Color("#F9E2AF") // warning
	colorRed       = lipgloss.Color("#F38BA8") // error / critical
	colorPeach     = lipgloss.Color("#FAB387") // auth issues
	colorTeal      = lipgloss.Color("#94E2D5") // secondary highlight
	colorFlamingo  = lipgloss.Color("#F2CDCD") // subtle highlight
	colorRosewater = lipgloss.Color("#F5E0DC") // hover
	colorLavender  = lipgloss.Color("#B4BEFE") // titles
	colorSky       = lipgloss.Color("#89DCEB") // info
	colorMaroon    = lipgloss.Color("#EBA0AC") // alt-red

	// Semantic aliases
	colorOK       = colorGreen
	colorWarn     = colorYellow
	colorCrit     = colorRed
	colorAuth     = colorPeach
	colorUnknown  = colorDim
	colorBorder   = colorDim
	colorSelected = colorAccent
)

// ─── Reusable Styles ────────────────────────────────────────────────────────

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorLavender)

	headerBrandStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent)

	sectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBlue)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorSapphire).
			Bold(true)

	labelStyle = lipgloss.NewStyle().
			Foreground(colorSubtext)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorText)

	dimStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	tealStyle = lipgloss.NewStyle().
			Foreground(colorTeal)

	gaugeTrackStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	// Card styles for list items
	cardNormalStyle = lipgloss.NewStyle().
			PaddingLeft(1).
			PaddingRight(1)

	cardSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				PaddingRight(1).
				Background(colorSurface0)

	// Badge-like status pill
	badgeOKStyle = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	badgeWarnStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true)

	badgeCritStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	badgeAuthStyle = lipgloss.NewStyle().
			Foreground(colorPeach).
			Bold(true)

	// Detail header — provider name
	detailTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorLavender)

	// Detail header — big name at top of card
	detailHeroNameStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorText)

	// Metric value highlight
	metricValueStyle = lipgloss.NewStyle().
				Foreground(colorRosewater).
				Bold(true)

	// ─── Detail Header Card Styles ───────────────────────────────────

	// The bordered header card wrapping the provider identity
	detailHeaderCardStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorSurface1).
				Padding(0, 1)

	// Status pill: colored background badge
	statusPillOKStyle = lipgloss.NewStyle().
				Foreground(colorMantle).
				Background(colorGreen).
				Bold(true).
				Padding(0, 1)

	statusPillWarnStyle = lipgloss.NewStyle().
				Foreground(colorMantle).
				Background(colorYellow).
				Bold(true).
				Padding(0, 1)

	statusPillCritStyle = lipgloss.NewStyle().
				Foreground(colorMantle).
				Background(colorRed).
				Bold(true).
				Padding(0, 1)

	statusPillAuthStyle = lipgloss.NewStyle().
				Foreground(colorMantle).
				Background(colorPeach).
				Bold(true).
				Padding(0, 1)

	statusPillDimStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorSurface1).
				Padding(0, 1)

	// Metadata tag pills (inline chips for email, plan, etc.)
	metaTagStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			Background(colorSurface0).
			Padding(0, 1)

	metaTagHighlightStyle = lipgloss.NewStyle().
				Foreground(colorSapphire).
				Background(colorSurface0).
				Padding(0, 1)

	// Category tag style (the colored pill like "⚡ Rate", "🔥 Cost")
	categoryTagStyle = lipgloss.NewStyle().
				Bold(true).
				Padding(0, 1)

	// Hero metric value (the big number)
	heroValueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorText)

	heroLabelStyle = lipgloss.NewStyle().
			Foreground(colorSubtext)

	// Active tab in detail panel
	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorLavender).
			Background(colorSurface0).
			Padding(0, 1)

	// Inactive tab in detail panel
	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Padding(0, 1)

	// Tab underline indicator
	tabUnderlineStyle = lipgloss.NewStyle().
				Foreground(colorLavender)

	// Section header separator line
	sectionSepStyle = lipgloss.NewStyle().
			Foreground(colorSurface1)

	// ─── Screen Tab Styles (tmux-like) ──────────────────────────────

	screenTabActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorMantle).
				Background(colorAccent).
				Padding(0, 1)

	screenTabInactiveStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Padding(0, 1)

	// ─── Analytics Styles ───────────────────────────────────────────

	analyticsCardTitleStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	analyticsCardValueStyle = lipgloss.NewStyle().
				Bold(true)

	analyticsCardSubtitleStyle = lipgloss.NewStyle().
					Foreground(colorSubtext)

	analyticsSortLabelStyle = lipgloss.NewStyle().
				Foreground(colorTeal)

	// ─── Chart Styles ───────────────────────────────────────────────

	chartTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue)

	chartAxisStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	chartLegendTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorSubtext)
)

// ─── Provider & Model Color Palettes ────────────────────────────────────────

// providerColorMap assigns a unique accent color to each known provider.
var providerColorMap = map[string]lipgloss.Color{
	"openai":      colorGreen,
	"anthropic":   colorPeach,
	"cursor":      colorLavender,
	"gemini_api":  colorBlue,
	"gemini_cli":  colorBlue,
	"claude_code": colorTeal,
	"groq":        colorYellow,
	"deepseek":    colorSky,
	"copilot":     colorSapphire,
	"xai":         colorMaroon,
	"mistral":     colorFlamingo,
	"openrouter":  colorRosewater,
	"codex":       colorGreen,
}

// modelColorPalette cycles through colors for model-level charts.
var modelColorPalette = []lipgloss.Color{
	colorPeach, colorTeal, colorSapphire, colorGreen,
	colorYellow, colorLavender, colorSky, colorFlamingo,
	colorMaroon, colorRosewater, colorBlue, colorAccent,
}

// ProviderColor returns the accent color for a provider by ID.
func ProviderColor(providerID string) lipgloss.Color {
	if c, ok := providerColorMap[providerID]; ok {
		return c
	}
	// Fallback: hash the name to pick a color from the model palette
	h := 0
	for _, ch := range providerID {
		h = h*31 + int(ch)
	}
	if h < 0 {
		h = -h
	}
	return modelColorPalette[h%len(modelColorPalette)]
}

// ModelColor returns a color for a model by its index.
func ModelColor(idx int) lipgloss.Color {
	if idx < 0 {
		idx = 0
	}
	return modelColorPalette[idx%len(modelColorPalette)]
}

// tagColor returns the accent color for a provider category tag.
// Each category gets a distinct, semantically meaningful color.
func tagColor(label string) lipgloss.Color {
	switch label {
	case "Spend", "Cost":
		return colorPeach
	case "Rate":
		return colorYellow
	case "Quota", "Plan":
		return colorSapphire
	case "Credits", "Balance":
		return colorTeal
	case "Block":
		return colorSky
	case "Activity":
		return colorGreen
	case "Error":
		return colorRed
	case "Auth":
		return colorPeach
	case "Info":
		return colorLavender
	case "Metrics":
		return colorFlamingo
	default:
		return colorSubtext
	}
}

// ─── Status Helpers ─────────────────────────────────────────────────────────

// StatusColor returns the accent color for a given status.
func StatusColor(s core.Status) lipgloss.Color {
	switch s {
	case core.StatusOK:
		return colorOK
	case core.StatusNearLimit:
		return colorWarn
	case core.StatusLimited:
		return colorCrit
	case core.StatusAuth:
		return colorAuth
	case core.StatusError:
		return colorCrit
	case core.StatusUnsupported, core.StatusUnknown:
		return colorUnknown
	default:
		return colorUnknown
	}
}

// StatusIcon returns a compact icon for a status.
func StatusIcon(s core.Status) string {
	switch s {
	case core.StatusOK:
		return "●"
	case core.StatusNearLimit:
		return "◐"
	case core.StatusLimited:
		return "◌"
	case core.StatusAuth:
		return "◈"
	case core.StatusError:
		return "✗"
	case core.StatusUnsupported:
		return "◇"
	default:
		return "·"
	}
}

// StatusBadge returns a styled badge string for the status.
func StatusBadge(s core.Status) string {
	var style lipgloss.Style
	var text string
	switch s {
	case core.StatusOK:
		style = badgeOKStyle
		text = "OK"
	case core.StatusNearLimit:
		style = badgeWarnStyle
		text = "WARN"
	case core.StatusLimited:
		style = badgeCritStyle
		text = "LIMIT"
	case core.StatusAuth:
		style = badgeAuthStyle
		text = "AUTH"
	case core.StatusError:
		style = badgeCritStyle
		text = "ERR"
	default:
		style = dimStyle
		text = "…"
	}
	return style.Render(text)
}

// StatusPill returns a filled pill-style badge (colored background) for detail headers.
func StatusPill(s core.Status) string {
	switch s {
	case core.StatusOK:
		return statusPillOKStyle.Render(" OK ")
	case core.StatusNearLimit:
		return statusPillWarnStyle.Render(" WARN ")
	case core.StatusLimited:
		return statusPillCritStyle.Render(" LIMIT ")
	case core.StatusAuth:
		return statusPillAuthStyle.Render(" AUTH ")
	case core.StatusError:
		return statusPillCritStyle.Render(" ERR ")
	default:
		return statusPillDimStyle.Render(" … ")
	}
}

// StatusBorderColor returns the border color for the header card based on status.
func StatusBorderColor(s core.Status) lipgloss.Color {
	switch s {
	case core.StatusOK:
		return colorGreen
	case core.StatusNearLimit:
		return colorYellow
	case core.StatusLimited, core.StatusError:
		return colorRed
	case core.StatusAuth:
		return colorPeach
	default:
		return colorSurface1
	}
}

// CategoryTag renders a colored category pill like "⚡ Rate" or "🔥 Cost".
func CategoryTag(emoji, label string) string {
	if emoji == "" || label == "" {
		return ""
	}
	c := tagColor(label)
	return categoryTagStyle.
		Foreground(c).
		Background(colorSurface0).
		Render(emoji + " " + label)
}

// MetaTag renders a small metadata chip with dimmed styling.
func MetaTag(icon, text string) string {
	if text == "" {
		return ""
	}
	return metaTagStyle.Render(icon + " " + text)
}

// MetaTagHighlight renders a metadata chip with accent color.
func MetaTagHighlight(icon, text string) string {
	if text == "" {
		return ""
	}
	return metaTagHighlightStyle.Render(icon + " " + text)
}
