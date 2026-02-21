package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type Theme struct {
	Name string
	Icon string // emoji identifier

	Base, Mantle                          lipgloss.Color
	Surface0, Surface1, Surface2, Overlay lipgloss.Color

	Text, Subtext, Dim lipgloss.Color

	Accent, Blue, Sapphire           lipgloss.Color
	Green, Yellow, Red               lipgloss.Color
	Peach, Teal, Flamingo            lipgloss.Color
	Rosewater, Lavender, Sky, Maroon lipgloss.Color
}

var catppuccinMocha = Theme{
	Name: "Catppuccin Mocha", Icon: "🐱",
	Base: "#1E1E2E", Mantle: "#181825",
	Surface0: "#313244", Surface1: "#45475A", Surface2: "#585B70", Overlay: "#45475A",
	Text: "#CDD6F4", Subtext: "#A6ADC8", Dim: "#585B70",
	Accent: "#CBA6F7", Blue: "#89B4FA", Sapphire: "#74C7EC",
	Green: "#A6E3A1", Yellow: "#F9E2AF", Red: "#F38BA8",
	Peach: "#FAB387", Teal: "#94E2D5", Flamingo: "#F2CDCD",
	Rosewater: "#F5E0DC", Lavender: "#B4BEFE", Sky: "#89DCEB", Maroon: "#EBA0AC",
}

var dracula = Theme{
	Name: "Dracula", Icon: "🧛",
	Base: "#282A36", Mantle: "#21222C",
	Surface0: "#44475A", Surface1: "#6272A4", Surface2: "#7E8AB0", Overlay: "#44475A",
	Text: "#F8F8F2", Subtext: "#BFBFBF", Dim: "#6272A4",
	Accent: "#BD93F9", Blue: "#8BE9FD", Sapphire: "#8BE9FD",
	Green: "#50FA7B", Yellow: "#F1FA8C", Red: "#FF5555",
	Peach: "#FFB86C", Teal: "#8BE9FD", Flamingo: "#FF79C6",
	Rosewater: "#FF79C6", Lavender: "#BD93F9", Sky: "#8BE9FD", Maroon: "#FF6E6E",
}

var nord = Theme{
	Name: "Nord", Icon: "❄",
	Base: "#2E3440", Mantle: "#242933",
	Surface0: "#3B4252", Surface1: "#434C5E", Surface2: "#4C566A", Overlay: "#434C5E",
	Text: "#ECEFF4", Subtext: "#D8DEE9", Dim: "#4C566A",
	Accent: "#B48EAD", Blue: "#81A1C1", Sapphire: "#88C0D0",
	Green: "#A3BE8C", Yellow: "#EBCB8B", Red: "#BF616A",
	Peach: "#D08770", Teal: "#8FBCBB", Flamingo: "#B48EAD",
	Rosewater: "#D8DEE9", Lavender: "#B48EAD", Sky: "#88C0D0", Maroon: "#BF616A",
}

var tokyoNight = Theme{
	Name: "Tokyo Night", Icon: "🌃",
	Base: "#1A1B26", Mantle: "#16161E",
	Surface0: "#24283B", Surface1: "#414868", Surface2: "#565F89", Overlay: "#414868",
	Text: "#C0CAF5", Subtext: "#A9B1D6", Dim: "#565F89",
	Accent: "#BB9AF7", Blue: "#7AA2F7", Sapphire: "#7DCFFF",
	Green: "#9ECE6A", Yellow: "#E0AF68", Red: "#F7768E",
	Peach: "#FF9E64", Teal: "#73DACA", Flamingo: "#FF007C",
	Rosewater: "#C0CAF5", Lavender: "#BB9AF7", Sky: "#7DCFFF", Maroon: "#DB4B4B",
}

var gruvbox = Theme{
	Name: "Gruvbox", Icon: "🌻",
	Base: "#282828", Mantle: "#1D2021",
	Surface0: "#3C3836", Surface1: "#504945", Surface2: "#665C54", Overlay: "#504945",
	Text: "#EBDBB2", Subtext: "#D5C4A1", Dim: "#665C54",
	Accent: "#D3869B", Blue: "#83A598", Sapphire: "#83A598",
	Green: "#B8BB26", Yellow: "#FABD2F", Red: "#FB4934",
	Peach: "#FE8019", Teal: "#8EC07C", Flamingo: "#D3869B",
	Rosewater: "#EBDBB2", Lavender: "#D3869B", Sky: "#83A598", Maroon: "#CC241D",
}

var synthwave = Theme{
	Name: "Synthwave '84", Icon: "🌆",
	Base: "#262335", Mantle: "#1E1A2B",
	Surface0: "#34294F", Surface1: "#443873", Surface2: "#544693", Overlay: "#443873",
	Text: "#F0E6FF", Subtext: "#C2B5D9", Dim: "#544693",
	Accent: "#FF7EDB", Blue: "#36F9F6", Sapphire: "#72F1B8",
	Green: "#72F1B8", Yellow: "#FEDE5D", Red: "#FE4450",
	Peach: "#FF8B39", Teal: "#36F9F6", Flamingo: "#FF7EDB",
	Rosewater: "#F97E72", Lavender: "#CF8DFB", Sky: "#36F9F6", Maroon: "#FE4450",
}

var oneDark = Theme{
	Name: "One Dark", Icon: "🧪",
	Base: "#282C34", Mantle: "#21252B",
	Surface0: "#2C313C", Surface1: "#3E4451", Surface2: "#4B5263", Overlay: "#3E4451",
	Text: "#ABB2BF", Subtext: "#98A2B3", Dim: "#5C6370",
	Accent: "#C678DD", Blue: "#61AFEF", Sapphire: "#56B6C2",
	Green: "#98C379", Yellow: "#E5C07B", Red: "#E06C75",
	Peach: "#D19A66", Teal: "#56B6C2", Flamingo: "#BE5046",
	Rosewater: "#E5C07B", Lavender: "#C678DD", Sky: "#61AFEF", Maroon: "#BE5046",
}

var solarizedDark = Theme{
	Name: "Solarized Dark", Icon: "🌅",
	Base: "#002B36", Mantle: "#073642",
	Surface0: "#073642", Surface1: "#0E3A45", Surface2: "#144754", Overlay: "#0E3A45",
	Text: "#93A1A1", Subtext: "#839496", Dim: "#586E75",
	Accent: "#D33682", Blue: "#268BD2", Sapphire: "#2AA198",
	Green: "#859900", Yellow: "#B58900", Red: "#DC322F",
	Peach: "#CB4B16", Teal: "#2AA198", Flamingo: "#D33682",
	Rosewater: "#EEE8D5", Lavender: "#6C71C4", Sky: "#268BD2", Maroon: "#DC322F",
}

var monokai = Theme{
	Name: "Monokai", Icon: "🦎",
	Base: "#272822", Mantle: "#1E1F1C",
	Surface0: "#3E3D32", Surface1: "#575642", Surface2: "#75715E", Overlay: "#575642",
	Text: "#F8F8F2", Subtext: "#CFCFC2", Dim: "#75715E",
	Accent: "#AE81FF", Blue: "#66D9EF", Sapphire: "#78DCE8",
	Green: "#A6E22E", Yellow: "#E6DB74", Red: "#F92672",
	Peach: "#FD971F", Teal: "#66D9EF", Flamingo: "#F92672",
	Rosewater: "#F8F8F2", Lavender: "#AE81FF", Sky: "#78DCE8", Maroon: "#D14A68",
}

var everforest = Theme{
	Name: "Everforest", Icon: "🌲",
	Base: "#2D353B", Mantle: "#232A2E",
	Surface0: "#343F44", Surface1: "#3D484D", Surface2: "#475258", Overlay: "#3D484D",
	Text: "#D3C6AA", Subtext: "#A7C080", Dim: "#859289",
	Accent: "#D699B6", Blue: "#7FBBB3", Sapphire: "#83C092",
	Green: "#A7C080", Yellow: "#DBBC7F", Red: "#E67E80",
	Peach: "#E69875", Teal: "#83C092", Flamingo: "#D699B6",
	Rosewater: "#D3C6AA", Lavender: "#D699B6", Sky: "#7FBBB3", Maroon: "#E67E80",
}

var kanagawa = Theme{
	Name: "Kanagawa", Icon: "⛩",
	Base: "#1F1F28", Mantle: "#16161D",
	Surface0: "#2A2A37", Surface1: "#363646", Surface2: "#54546D", Overlay: "#363646",
	Text: "#DCD7BA", Subtext: "#C8C093", Dim: "#727169",
	Accent: "#957FB8", Blue: "#7E9CD8", Sapphire: "#7FB4CA",
	Green: "#76946A", Yellow: "#C0A36E", Red: "#C34043",
	Peach: "#FFA066", Teal: "#6A9589", Flamingo: "#D27E99",
	Rosewater: "#DCD7BA", Lavender: "#957FB8", Sky: "#7FB4CA", Maroon: "#E46876",
}

var rosePine = Theme{
	Name: "Rose Pine", Icon: "🌹",
	Base: "#191724", Mantle: "#16141F",
	Surface0: "#1F1D2E", Surface1: "#26233A", Surface2: "#403D52", Overlay: "#26233A",
	Text: "#E0DEF4", Subtext: "#908CAA", Dim: "#6E6A86",
	Accent: "#C4A7E7", Blue: "#9CCFD8", Sapphire: "#31748F",
	Green: "#9CCFD8", Yellow: "#F6C177", Red: "#EB6F92",
	Peach: "#EA9A97", Teal: "#9CCFD8", Flamingo: "#EBBCBA",
	Rosewater: "#E0DEF4", Lavender: "#C4A7E7", Sky: "#9CCFD8", Maroon: "#B4637A",
}

var ayuDark = Theme{
	Name: "Ayu Dark", Icon: "🌙",
	Base: "#0B0E14", Mantle: "#090B10",
	Surface0: "#11151C", Surface1: "#1B2330", Surface2: "#2A3547", Overlay: "#1B2330",
	Text: "#BFBDB6", Subtext: "#A6A49D", Dim: "#626A73",
	Accent: "#D2A6FF", Blue: "#59C2FF", Sapphire: "#95E6CB",
	Green: "#AAD94C", Yellow: "#FFB454", Red: "#F07178",
	Peach: "#FF8F40", Teal: "#95E6CB", Flamingo: "#F29668",
	Rosewater: "#E6E1CF", Lavender: "#D2A6FF", Sky: "#73D0FF", Maroon: "#E06C75",
}

var nightfox = Theme{
	Name: "Nightfox", Icon: "🦊",
	Base: "#192330", Mantle: "#131A24",
	Surface0: "#29394F", Surface1: "#394B70", Surface2: "#4E5F82", Overlay: "#394B70",
	Text: "#CDCECF", Subtext: "#9DA9BC", Dim: "#738091",
	Accent: "#9D79D6", Blue: "#719CD6", Sapphire: "#63CDCF",
	Green: "#81B29A", Yellow: "#DBC074", Red: "#C94F6D",
	Peach: "#F4A261", Teal: "#63CDCF", Flamingo: "#9D79D6",
	Rosewater: "#CDCECF", Lavender: "#9D79D6", Sky: "#63CDCF", Maroon: "#C94F6D",
}

var Themes = []Theme{
	gruvbox,
	catppuccinMocha,
	dracula,
	nord,
	tokyoNight,
	synthwave,
	oneDark,
	solarizedDark,
	monokai,
	everforest,
	kanagawa,
	rosePine,
	ayuDark,
	nightfox,
}

var ActiveThemeIdx int

func CycleTheme() string {
	ActiveThemeIdx = (ActiveThemeIdx + 1) % len(Themes)
	applyTheme(Themes[ActiveThemeIdx])
	return Themes[ActiveThemeIdx].Name
}

func ThemeName() string {
	t := Themes[ActiveThemeIdx]
	return t.Icon + " " + t.Name
}

func SetThemeByName(name string) bool {
	for i, t := range Themes {
		if t.Name == name {
			ActiveThemeIdx = i
			applyTheme(t)
			return true
		}
	}
	return false
}

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
)

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
}

func init() {
	applyTheme(Themes[ActiveThemeIdx])
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

func ModelColor(idx int) lipgloss.Color {
	if idx < 0 {
		idx = 0
	}
	return modelColorPalette[idx%len(modelColorPalette)]
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
		return statusPillDimStyle.Render(fmt.Sprintf(" %s ", string(s)))
	}
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

func MetaTag(icon, text string) string {
	if text == "" {
		return ""
	}
	return metaTagStyle.Render(icon + " " + text)
}

func MetaTagHighlight(icon, text string) string {
	if text == "" {
		return ""
	}
	return metaTagHighlightStyle.Render(icon + " " + text)
}
