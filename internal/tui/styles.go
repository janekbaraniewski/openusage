package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var BrandGradient []lipgloss.Color

func RenderGradientText(text string, frame int) string {
	if len(BrandGradient) == 0 {
		return text
	}
	var b strings.Builder
	shift := frame / 2
	for i, ch := range text {
		c := BrandGradient[(i+shift)%len(BrandGradient)]
		b.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(ch)))
	}
	return b.String()
}

func PulseChar(bright, dim string, frame int) string {
	if (frame/4)%2 == 0 {
		return bright
	}
	return dim
}

func ASCIIBanner(frame int) string {
	lines := []string{
		` █▀█ █▀█ █▀▀ █▄░█   █░█ █▀ ▄▀█ █▀▀ █▀▀`,
		` █▄█ █▀▀ ██▄ █░▀█   █▄█ ▄█ █▀█ █▄█ ██▄`,
	}
	if len(BrandGradient) == 0 {
		return strings.Join(lines, "\n")
	}
	var result []string
	shift := frame / 3
	for _, line := range lines {
		var b strings.Builder
		for i, ch := range line {
			if ch == ' ' {
				b.WriteRune(' ')
			} else {
				c := BrandGradient[(i/2+shift)%len(BrandGradient)]
				b.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(ch)))
			}
		}
		result = append(result, b.String())
	}
	return strings.Join(result, "\n")
}

var (
	colorBase     lipgloss.Color
	colorMantle   lipgloss.Color
	colorSurface0 lipgloss.Color
	colorSurface1 lipgloss.Color
	colorSurface2 lipgloss.Color
	colorText     lipgloss.Color
	colorSubtext  lipgloss.Color
	colorDim      lipgloss.Color
	colorOverlay  lipgloss.Color

	colorAccent    lipgloss.Color
	colorBlue      lipgloss.Color
	colorSapphire  lipgloss.Color
	colorGreen     lipgloss.Color
	colorYellow    lipgloss.Color
	colorRed       lipgloss.Color
	colorPeach     lipgloss.Color
	colorTeal      lipgloss.Color
	colorFlamingo  lipgloss.Color
	colorRosewater lipgloss.Color
	colorLavender  lipgloss.Color
	colorSky       lipgloss.Color
	colorMaroon    lipgloss.Color
	colorMauve     lipgloss.Color

	colorOK       lipgloss.Color
	colorWarn     lipgloss.Color
	colorCrit     lipgloss.Color
	colorAuth     lipgloss.Color
	colorUnknown  lipgloss.Color
	colorBorder   lipgloss.Color
	colorSelected lipgloss.Color
)

var (
	headerStyle        lipgloss.Style
	headerBrandStyle   lipgloss.Style
	sectionHeaderStyle lipgloss.Style
	helpStyle          lipgloss.Style
	helpKeyStyle       lipgloss.Style
	labelStyle         lipgloss.Style
	valueStyle         lipgloss.Style
	dimStyle           lipgloss.Style
	tealStyle          lipgloss.Style
	gaugeTrackStyle    lipgloss.Style

	cardNormalStyle   lipgloss.Style
	cardSelectedStyle lipgloss.Style

	badgeOKStyle   lipgloss.Style
	badgeWarnStyle lipgloss.Style
	badgeCritStyle lipgloss.Style
	badgeAuthStyle lipgloss.Style

	detailTitleStyle      lipgloss.Style
	detailHeroNameStyle   lipgloss.Style
	metricValueStyle      lipgloss.Style
	detailHeaderCardStyle lipgloss.Style

	statusPillOKStyle   lipgloss.Style
	statusPillWarnStyle lipgloss.Style
	statusPillCritStyle lipgloss.Style
	statusPillAuthStyle lipgloss.Style
	statusPillDimStyle  lipgloss.Style

	metaTagStyle          lipgloss.Style
	metaTagHighlightStyle lipgloss.Style
	categoryTagStyle      lipgloss.Style

	heroValueStyle lipgloss.Style
	heroLabelStyle lipgloss.Style

	tabActiveStyle    lipgloss.Style
	tabInactiveStyle  lipgloss.Style
	tabUnderlineStyle lipgloss.Style
	sectionSepStyle   lipgloss.Style

	screenTabActiveStyle   lipgloss.Style
	screenTabInactiveStyle lipgloss.Style

	analyticsCardTitleStyle    lipgloss.Style
	analyticsCardValueStyle    lipgloss.Style
	analyticsCardSubtitleStyle lipgloss.Style
	analyticsSortLabelStyle    lipgloss.Style

	analyticsSubTabActiveStyle   lipgloss.Style
	analyticsSubTabInactiveStyle lipgloss.Style

	chartTitleStyle       lipgloss.Style
	chartAxisStyle        lipgloss.Style
	chartLegendTitleStyle lipgloss.Style

	tileBorderStyle         lipgloss.Style
	tileSelectedBorderStyle lipgloss.Style
	tileNameStyle           lipgloss.Style
	tileNameSelectedStyle   lipgloss.Style
	tileSummaryStyle        lipgloss.Style
	tileTimestampStyle      lipgloss.Style
	tileHeroStyle           lipgloss.Style
	tileDotLeaderStyle      lipgloss.Style

	// Cached simple foreground styles for hot render paths.
	// Use these instead of lipgloss.NewStyle().Foreground(color) in loops.
	accentBoldStyle    lipgloss.Style
	subtextBoldStyle   lipgloss.Style
	subtextItalicStyle lipgloss.Style
	sapphireStyle      lipgloss.Style
	redStyle           lipgloss.Style
	greenStyle         lipgloss.Style
	yellowStyle        lipgloss.Style
	textBoldStyle      lipgloss.Style
	surface1Style      lipgloss.Style
	tealBoldStyle      lipgloss.Style
)

// applyTheme sets all package-level color and style globals from the given
// theme. It MUST only be called while themeMu is held for writing, or during
// init() (before any concurrent access is possible). Rendering functions read
// these globals from the Bubble Tea View goroutine, which is the same goroutine
// that calls Update (where theme switches happen), so no additional
// synchronization is needed. See the themeMu comment in themes.go for the full
// locking protocol.
func applyTheme(t Theme) {
	colorBase = t.Base
	colorMantle = t.Mantle
	colorSurface0 = t.Surface0
	colorSurface1 = t.Surface1
	colorSurface2 = t.Surface2
	colorOverlay = t.Overlay
	colorText = t.Text
	colorSubtext = t.Subtext
	colorDim = t.Dim
	colorAccent = t.Accent
	colorBlue = t.Blue
	colorSapphire = t.Sapphire
	colorGreen = t.Green
	colorYellow = t.Yellow
	colorRed = t.Red
	colorPeach = t.Peach
	colorTeal = t.Teal
	colorFlamingo = t.Flamingo
	colorRosewater = t.Rosewater
	colorLavender = t.Lavender
	colorSky = t.Sky
	colorMaroon = t.Maroon
	colorMauve = t.Mauve

	colorOK = colorGreen
	colorWarn = colorYellow
	colorCrit = colorRed
	colorAuth = colorPeach
	colorUnknown = colorDim
	colorBorder = colorDim
	colorSelected = colorAccent

	BrandGradient = []lipgloss.Color{
		t.Accent, t.Blue, t.Sapphire, t.Teal, t.Green, t.Lavender,
	}

	modelColorPalette = []lipgloss.Color{
		colorPeach, colorTeal, colorSapphire, colorGreen,
		colorYellow, colorLavender, colorSky, colorFlamingo,
		colorMaroon, colorRosewater, colorBlue, colorAccent,
	}

	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	headerBrandStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	sectionHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	helpStyle = lipgloss.NewStyle().Foreground(colorDim)
	helpKeyStyle = lipgloss.NewStyle().Foreground(colorSapphire).Bold(true)
	labelStyle = lipgloss.NewStyle().Foreground(colorSubtext)
	valueStyle = lipgloss.NewStyle().Foreground(colorText)
	dimStyle = lipgloss.NewStyle().Foreground(colorDim)
	tealStyle = lipgloss.NewStyle().Foreground(colorTeal)
	gaugeTrackStyle = lipgloss.NewStyle().Foreground(colorDim)

	cardNormalStyle = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	cardSelectedStyle = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1).Background(colorSurface0)

	badgeOKStyle = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	badgeWarnStyle = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	badgeCritStyle = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	badgeAuthStyle = lipgloss.NewStyle().Foreground(colorPeach).Bold(true)

	detailTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	detailHeroNameStyle = lipgloss.NewStyle().Bold(true).Foreground(colorText)
	metricValueStyle = lipgloss.NewStyle().Foreground(colorRosewater).Bold(true)

	detailHeaderCardStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorSurface1).
		Padding(0, 1)

	statusPillOKStyle = lipgloss.NewStyle().Foreground(colorMantle).Background(colorGreen).Bold(true).Padding(0, 1)
	statusPillWarnStyle = lipgloss.NewStyle().Foreground(colorMantle).Background(colorYellow).Bold(true).Padding(0, 1)
	statusPillCritStyle = lipgloss.NewStyle().Foreground(colorMantle).Background(colorRed).Bold(true).Padding(0, 1)
	statusPillAuthStyle = lipgloss.NewStyle().Foreground(colorMantle).Background(colorPeach).Bold(true).Padding(0, 1)
	statusPillDimStyle = lipgloss.NewStyle().Foreground(colorText).Background(colorSurface1).Padding(0, 1)

	metaTagStyle = lipgloss.NewStyle().Foreground(colorSubtext).Background(colorSurface0).Padding(0, 1)
	metaTagHighlightStyle = lipgloss.NewStyle().Foreground(colorSapphire).Background(colorSurface0).Padding(0, 1)
	categoryTagStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1)

	heroValueStyle = lipgloss.NewStyle().Bold(true).Foreground(colorText)
	heroLabelStyle = lipgloss.NewStyle().Foreground(colorSubtext)

	tabActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(colorLavender).Background(colorSurface0).Padding(0, 1)
	tabInactiveStyle = lipgloss.NewStyle().Foreground(colorDim).Padding(0, 1)
	tabUnderlineStyle = lipgloss.NewStyle().Foreground(colorLavender)
	sectionSepStyle = lipgloss.NewStyle().Foreground(colorSurface1)

	screenTabActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(colorMantle).Background(colorAccent).Padding(0, 1)
	screenTabInactiveStyle = lipgloss.NewStyle().Foreground(colorDim).Padding(0, 1)

	analyticsCardTitleStyle = lipgloss.NewStyle().Foreground(colorDim)
	analyticsCardValueStyle = lipgloss.NewStyle().Bold(true)
	analyticsCardSubtitleStyle = lipgloss.NewStyle().Foreground(colorSubtext)
	analyticsSortLabelStyle = lipgloss.NewStyle().Foreground(colorTeal)

	analyticsSubTabActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(colorMantle).Background(colorBlue).Padding(0, 1)
	analyticsSubTabInactiveStyle = lipgloss.NewStyle().Foreground(colorDim).Background(colorSurface0).Padding(0, 1)

	chartTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	chartAxisStyle = lipgloss.NewStyle().Foreground(colorDim)
	chartLegendTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSubtext)

	tileBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorSurface1).
		Padding(0, tilePadH)

	tileSelectedBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(0, tilePadH)

	tileNameStyle = lipgloss.NewStyle().Bold(true).Foreground(colorText)
	tileNameSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(colorLavender)
	tileSummaryStyle = lipgloss.NewStyle().Foreground(colorSubtext)
	tileTimestampStyle = lipgloss.NewStyle().Foreground(colorDim)
	tileHeroStyle = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	tileDotLeaderStyle = lipgloss.NewStyle().Foreground(colorSurface2)

	accentBoldStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	subtextBoldStyle = lipgloss.NewStyle().Foreground(colorSubtext).Bold(true)
	subtextItalicStyle = lipgloss.NewStyle().Foreground(colorSubtext).Italic(true)
	sapphireStyle = lipgloss.NewStyle().Foreground(colorSapphire)
	redStyle = lipgloss.NewStyle().Foreground(colorRed)
	greenStyle = lipgloss.NewStyle().Foreground(colorGreen)
	yellowStyle = lipgloss.NewStyle().Foreground(colorYellow)
	textBoldStyle = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	surface1Style = lipgloss.NewStyle().Foreground(colorSurface1)
	tealBoldStyle = lipgloss.NewStyle().Foreground(colorTeal).Bold(true)
}

var modelColorPalette []lipgloss.Color

func ProviderColor(providerID string) lipgloss.Color {
	switch dashboardWidget(providerID).ColorRole {
	case core.DashboardColorRoleGreen:
		return colorGreen
	case core.DashboardColorRolePeach:
		return colorPeach
	case core.DashboardColorRoleLavender:
		return colorLavender
	case core.DashboardColorRoleBlue:
		return colorBlue
	case core.DashboardColorRoleTeal:
		return colorTeal
	case core.DashboardColorRoleYellow:
		return colorYellow
	case core.DashboardColorRoleSky:
		return colorSky
	case core.DashboardColorRoleSapphire:
		return colorSapphire
	case core.DashboardColorRoleMaroon:
		return colorMaroon
	case core.DashboardColorRoleFlamingo:
		return colorFlamingo
	case core.DashboardColorRoleRosewater:
		return colorRosewater
	case core.DashboardColorRoleMauve:
		return colorMauve
	}
	h := 0
	for _, ch := range providerID {
		h = h*31 + int(ch)
	}
	if h < 0 {
		h = -h
	}
	return modelColorPalette[h%len(modelColorPalette)]
}

func stableModelColor(modelName, providerID string) lipgloss.Color {
	key := providerID + ":" + modelName
	h := 0
	for _, ch := range key {
		h = h*31 + int(ch)
	}
	if h < 0 {
		h = -h
	}
	return modelColorPalette[h%len(modelColorPalette)]
}

func tagColor(label string) lipgloss.Color {
	switch label {
	case "Spend":
		return colorPeach
	case "Usage":
		return colorYellow
	case "Plan":
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

// SnapshotStatusBadge returns a styled status badge that specifies the exact limit window
// (e.g. WEEKLY LIMIT, MONTHLY LIMIT, 5H LIMIT) when an account is limited.
func SnapshotStatusBadge(snap core.UsageSnapshot) string {
	if snap.Status != core.StatusLimited {
		return StatusBadge(snap.Status)
	}

	limitType := resolveExhaustedLimitType(snap)
	text := "LIMIT"
	if limitType != "" {
		text = limitType + " LIMIT"
	}
	return badgeCritStyle.Render(text)
}

func resolveExhaustedLimitType(snap core.UsageSnapshot) string {
	model := strings.ToLower(snap.Attributes["model"])
	if model == "" {
		model = strings.ToLower(snap.Attributes["model_id"])
	}

	activePool := ""
	if strings.Contains(model, "gemini") {
		activePool = "gemini"
	} else if strings.Contains(model, "claude") || strings.Contains(model, "sonnet") || strings.Contains(model, "opus") || strings.Contains(model, "3p") || strings.Contains(model, "gpt") {
		activePool = "claude"
	}

	claudeDisabled := snap.Attributes["claude_disabled"] == "true" || snap.Attributes["3p_disabled"] == "true"

	isRelevantKey := func(key string) bool {
		k := strings.ToLower(key)
		if claudeDisabled && (strings.Contains(k, "claude") || strings.Contains(k, "3p") || strings.Contains(k, "opus") || strings.Contains(k, "sonnet")) {
			return false
		}
		if activePool == "gemini" && (strings.Contains(k, "claude") || strings.Contains(k, "3p") || strings.Contains(k, "opus") || strings.Contains(k, "sonnet")) {
			return false
		}
		if activePool == "claude" && strings.Contains(k, "gemini") {
			return false
		}
		return true
	}

	// 1. Check metrics for exhausted windows
	exhausted := make(map[string]bool)
	for key, met := range snap.Metrics {
		if !isRelevantKey(key) {
			continue
		}

		isExhausted := false
		if met.Remaining != nil && *met.Remaining <= 0 {
			isExhausted = true
		} else if met.Used != nil && *met.Used >= 100 && met.Unit == "%" {
			isExhausted = true
		} else if met.Limit != nil && met.Used != nil && *met.Limit > 0 && *met.Used >= *met.Limit {
			isExhausted = true
		}

		if isExhausted {
			k := strings.ToLower(key)
			w := strings.ToLower(met.Window)
			if strings.Contains(k, "month") || strings.Contains(w, "month") || strings.Contains(w, "30d") {
				exhausted["MONTHLY"] = true
			} else if strings.Contains(k, "weekly") || strings.Contains(k, "week") || strings.Contains(k, "7d") || strings.Contains(w, "weekly") || strings.Contains(w, "7d") {
				exhausted["WEEKLY"] = true
			} else if strings.Contains(k, "5h") || strings.Contains(k, "five_hour") || strings.Contains(k, "rolling") || strings.Contains(w, "5h") {
				exhausted["5H"] = true
			} else if strings.Contains(k, "daily") || strings.Contains(k, "day") || strings.Contains(k, "1d") || strings.Contains(k, "24h") || strings.Contains(w, "daily") || strings.Contains(w, "1d") {
				exhausted["DAILY"] = true
			}
		}
	}

	// Safeguard: If exhausted["WEEKLY"] is set, but there is ANY relevant weekly metric
	// with Remaining > 0 (e.g. quota_gemini_weekly has 37.87% remaining), do NOT report WEEKLY limit!
	if exhausted["WEEKLY"] {
		for key, met := range snap.Metrics {
			if !isRelevantKey(key) {
				continue
			}
			k := strings.ToLower(key)
			w := strings.ToLower(met.Window)
			if strings.Contains(k, "weekly") || strings.Contains(k, "week") || strings.Contains(k, "7d") || strings.Contains(w, "weekly") || strings.Contains(w, "7d") {
				if met.Remaining != nil && *met.Remaining > 0 {
					exhausted["WEEKLY"] = false
					break
				}
			}
		}
	}

	// Priority order: MONTHLY > WEEKLY > DAILY > 5H
	if exhausted["MONTHLY"] {
		return "MONTHLY"
	}
	if exhausted["WEEKLY"] {
		return "WEEKLY"
	}
	if exhausted["DAILY"] {
		return "DAILY"
	}
	if exhausted["5H"] {
		return "5H"
	}

	// 2. Check message or attributes
	msg := strings.ToLower(snap.Message + " " + snap.Attributes["limit_type"] + " " + snap.Attributes["rate_limit"])
	if strings.Contains(msg, "monthly") || strings.Contains(msg, "month") {
		return "MONTHLY"
	}
	if strings.Contains(msg, "weekly") || strings.Contains(msg, "week") || strings.Contains(msg, "7d") {
		return "WEEKLY"
	}
	if strings.Contains(msg, "5h") || strings.Contains(msg, "5 hour") || strings.Contains(msg, "five hour") {
		return "5H"
	}
	if strings.Contains(msg, "daily") || strings.Contains(msg, "day") {
		return "DAILY"
	}

	// 3. Check reset keys if any window has an active reset
	hasResetFor := func(target string) bool {
		for rKey, resetTime := range snap.Resets {
			if resetTime.IsZero() {
				continue
			}
			if !isRelevantKey(rKey) {
				continue
			}
			rk := strings.ToLower(rKey)
			matches := false
			switch target {
			case "MONTHLY":
				matches = strings.Contains(rk, "monthly")
			case "WEEKLY":
				matches = strings.Contains(rk, "weekly") || strings.Contains(rk, "7d")
			case "5H":
				matches = strings.Contains(rk, "5h") || strings.Contains(rk, "rolling")
			}
			if matches {
				if m, ok := snap.Metrics[rKey]; ok && m.Remaining != nil && *m.Remaining > 0 {
					continue
				}
				return true
			}
		}
		return false
	}

	if hasResetFor("MONTHLY") {
		return "MONTHLY"
	}
	if hasResetFor("WEEKLY") {
		return "WEEKLY"
	}
	if hasResetFor("5H") {
		return "5H"
	}

	return ""
}

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
