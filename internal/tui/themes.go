package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// OPENUSAGE_THEME_DIR can point to one or more additional theme directories
// (path-list separated, e.g. ":" on unix, ";" on Windows).
const themeDirEnvVar = "OPENUSAGE_THEME_DIR"

// Theme represents the full visual token set used by the TUI.
//
// External themes can be defined as JSON files with matching snake_case fields,
// for example: {"name":"My Theme","base":"#111111",...}.
type Theme struct {
	Name string `json:"name"`
	Icon string `json:"icon"`

	Base     lipgloss.Color `json:"base"`
	Mantle   lipgloss.Color `json:"mantle"`
	Surface0 lipgloss.Color `json:"surface0"`
	Surface1 lipgloss.Color `json:"surface1"`
	Surface2 lipgloss.Color `json:"surface2"`
	Overlay  lipgloss.Color `json:"overlay"`

	Text    lipgloss.Color `json:"text"`
	Subtext lipgloss.Color `json:"subtext"`
	Dim     lipgloss.Color `json:"dim"`

	Accent    lipgloss.Color `json:"accent"`
	Blue      lipgloss.Color `json:"blue"`
	Sapphire  lipgloss.Color `json:"sapphire"`
	Green     lipgloss.Color `json:"green"`
	Yellow    lipgloss.Color `json:"yellow"`
	Red       lipgloss.Color `json:"red"`
	Peach     lipgloss.Color `json:"peach"`
	Teal      lipgloss.Color `json:"teal"`
	Flamingo  lipgloss.Color `json:"flamingo"`
	Rosewater lipgloss.Color `json:"rosewater"`
	Lavender  lipgloss.Color `json:"lavender"`
	Sky       lipgloss.Color `json:"sky"`
	Maroon    lipgloss.Color `json:"maroon"`
}

var (
	themeMu        sync.RWMutex
	themes         []Theme
	activeThemeIdx int
)

func init() {
	themes = builtinThemes()
	activeThemeIdx = defaultThemeIndex(themes)
	if len(themes) > 0 {
		applyTheme(themes[activeThemeIdx])
	}
}

func builtinThemes() []Theme {
	return []Theme{
		{
			Name: "Gruvbox", Icon: "🌻",
			Base: "#282828", Mantle: "#1D2021",
			Surface0: "#3C3836", Surface1: "#504945", Surface2: "#665C54", Overlay: "#504945",
			Text: "#EBDBB2", Subtext: "#D5C4A1", Dim: "#665C54",
			Accent: "#D3869B", Blue: "#83A598", Sapphire: "#83A598",
			Green: "#B8BB26", Yellow: "#FABD2F", Red: "#FB4934",
			Peach: "#FE8019", Teal: "#8EC07C", Flamingo: "#D3869B",
			Rosewater: "#EBDBB2", Lavender: "#D3869B", Sky: "#83A598", Maroon: "#CC241D",
		},
		{
			Name: "Catppuccin Mocha", Icon: "🐱",
			Base: "#1E1E2E", Mantle: "#181825",
			Surface0: "#313244", Surface1: "#45475A", Surface2: "#585B70", Overlay: "#45475A",
			Text: "#CDD6F4", Subtext: "#A6ADC8", Dim: "#585B70",
			Accent: "#CBA6F7", Blue: "#89B4FA", Sapphire: "#74C7EC",
			Green: "#A6E3A1", Yellow: "#F9E2AF", Red: "#F38BA8",
			Peach: "#FAB387", Teal: "#94E2D5", Flamingo: "#F2CDCD",
			Rosewater: "#F5E0DC", Lavender: "#B4BEFE", Sky: "#89DCEB", Maroon: "#EBA0AC",
		},
		{
			Name: "Dracula", Icon: "🧛",
			Base: "#282A36", Mantle: "#21222C",
			Surface0: "#44475A", Surface1: "#6272A4", Surface2: "#7E8AB0", Overlay: "#44475A",
			Text: "#F8F8F2", Subtext: "#BFBFBF", Dim: "#6272A4",
			Accent: "#BD93F9", Blue: "#8BE9FD", Sapphire: "#8BE9FD",
			Green: "#50FA7B", Yellow: "#F1FA8C", Red: "#FF5555",
			Peach: "#FFB86C", Teal: "#8BE9FD", Flamingo: "#FF79C6",
			Rosewater: "#FF79C6", Lavender: "#BD93F9", Sky: "#8BE9FD", Maroon: "#FF6E6E",
		},
		{
			Name: "Nord", Icon: "❄",
			Base: "#2E3440", Mantle: "#242933",
			Surface0: "#3B4252", Surface1: "#434C5E", Surface2: "#4C566A", Overlay: "#434C5E",
			Text: "#ECEFF4", Subtext: "#D8DEE9", Dim: "#4C566A",
			Accent: "#B48EAD", Blue: "#81A1C1", Sapphire: "#88C0D0",
			Green: "#A3BE8C", Yellow: "#EBCB8B", Red: "#BF616A",
			Peach: "#D08770", Teal: "#8FBCBB", Flamingo: "#B48EAD",
			Rosewater: "#D8DEE9", Lavender: "#B48EAD", Sky: "#88C0D0", Maroon: "#BF616A",
		},
		{
			Name: "Tokyo Night", Icon: "🌃",
			Base: "#1A1B26", Mantle: "#16161E",
			Surface0: "#24283B", Surface1: "#414868", Surface2: "#565F89", Overlay: "#414868",
			Text: "#C0CAF5", Subtext: "#A9B1D6", Dim: "#565F89",
			Accent: "#BB9AF7", Blue: "#7AA2F7", Sapphire: "#7DCFFF",
			Green: "#9ECE6A", Yellow: "#E0AF68", Red: "#F7768E",
			Peach: "#FF9E64", Teal: "#73DACA", Flamingo: "#FF007C",
			Rosewater: "#C0CAF5", Lavender: "#BB9AF7", Sky: "#7DCFFF", Maroon: "#DB4B4B",
		},
		{
			Name: "Synthwave '84", Icon: "🌆",
			Base: "#262335", Mantle: "#1E1A2B",
			Surface0: "#34294F", Surface1: "#443873", Surface2: "#544693", Overlay: "#443873",
			Text: "#F0E6FF", Subtext: "#C2B5D9", Dim: "#544693",
			Accent: "#FF7EDB", Blue: "#36F9F6", Sapphire: "#72F1B8",
			Green: "#72F1B8", Yellow: "#FEDE5D", Red: "#FE4450",
			Peach: "#FF8B39", Teal: "#36F9F6", Flamingo: "#FF7EDB",
			Rosewater: "#F97E72", Lavender: "#CF8DFB", Sky: "#36F9F6", Maroon: "#FE4450",
		},
		{
			Name: "One Dark", Icon: "🧪",
			Base: "#282C34", Mantle: "#21252B",
			Surface0: "#2C313C", Surface1: "#3E4451", Surface2: "#4B5263", Overlay: "#3E4451",
			Text: "#ABB2BF", Subtext: "#98A2B3", Dim: "#5C6370",
			Accent: "#C678DD", Blue: "#61AFEF", Sapphire: "#56B6C2",
			Green: "#98C379", Yellow: "#E5C07B", Red: "#E06C75",
			Peach: "#D19A66", Teal: "#56B6C2", Flamingo: "#BE5046",
			Rosewater: "#E5C07B", Lavender: "#C678DD", Sky: "#61AFEF", Maroon: "#BE5046",
		},
		{
			Name: "Solarized Dark", Icon: "🌅",
			Base: "#002B36", Mantle: "#073642",
			Surface0: "#073642", Surface1: "#0E3A45", Surface2: "#144754", Overlay: "#0E3A45",
			Text: "#93A1A1", Subtext: "#839496", Dim: "#586E75",
			Accent: "#D33682", Blue: "#268BD2", Sapphire: "#2AA198",
			Green: "#859900", Yellow: "#B58900", Red: "#DC322F",
			Peach: "#CB4B16", Teal: "#2AA198", Flamingo: "#D33682",
			Rosewater: "#EEE8D5", Lavender: "#6C71C4", Sky: "#268BD2", Maroon: "#DC322F",
		},
		{
			Name: "Monokai", Icon: "🦎",
			Base: "#272822", Mantle: "#1E1F1C",
			Surface0: "#3E3D32", Surface1: "#575642", Surface2: "#75715E", Overlay: "#575642",
			Text: "#F8F8F2", Subtext: "#CFCFC2", Dim: "#75715E",
			Accent: "#AE81FF", Blue: "#66D9EF", Sapphire: "#78DCE8",
			Green: "#A6E22E", Yellow: "#E6DB74", Red: "#F92672",
			Peach: "#FD971F", Teal: "#66D9EF", Flamingo: "#F92672",
			Rosewater: "#F8F8F2", Lavender: "#AE81FF", Sky: "#78DCE8", Maroon: "#D14A68",
		},
		{
			Name: "Everforest", Icon: "🌲",
			Base: "#2D353B", Mantle: "#232A2E",
			Surface0: "#343F44", Surface1: "#3D484D", Surface2: "#475258", Overlay: "#3D484D",
			Text: "#D3C6AA", Subtext: "#A7C080", Dim: "#859289",
			Accent: "#D699B6", Blue: "#7FBBB3", Sapphire: "#83C092",
			Green: "#A7C080", Yellow: "#DBBC7F", Red: "#E67E80",
			Peach: "#E69875", Teal: "#83C092", Flamingo: "#D699B6",
			Rosewater: "#D3C6AA", Lavender: "#D699B6", Sky: "#7FBBB3", Maroon: "#E67E80",
		},
		{
			Name: "Kanagawa", Icon: "⛩",
			Base: "#1F1F28", Mantle: "#16161D",
			Surface0: "#2A2A37", Surface1: "#363646", Surface2: "#54546D", Overlay: "#363646",
			Text: "#DCD7BA", Subtext: "#C8C093", Dim: "#727169",
			Accent: "#957FB8", Blue: "#7E9CD8", Sapphire: "#7FB4CA",
			Green: "#76946A", Yellow: "#C0A36E", Red: "#C34043",
			Peach: "#FFA066", Teal: "#6A9589", Flamingo: "#D27E99",
			Rosewater: "#DCD7BA", Lavender: "#957FB8", Sky: "#7FB4CA", Maroon: "#E46876",
		},
		{
			Name: "Rose Pine", Icon: "🌹",
			Base: "#191724", Mantle: "#16141F",
			Surface0: "#1F1D2E", Surface1: "#26233A", Surface2: "#403D52", Overlay: "#26233A",
			Text: "#E0DEF4", Subtext: "#908CAA", Dim: "#6E6A86",
			Accent: "#C4A7E7", Blue: "#9CCFD8", Sapphire: "#31748F",
			Green: "#9CCFD8", Yellow: "#F6C177", Red: "#EB6F92",
			Peach: "#EA9A97", Teal: "#9CCFD8", Flamingo: "#EBBCBA",
			Rosewater: "#E0DEF4", Lavender: "#C4A7E7", Sky: "#9CCFD8", Maroon: "#B4637A",
		},
		{
			Name: "Ayu Dark", Icon: "🌙",
			Base: "#0B0E14", Mantle: "#090B10",
			Surface0: "#11151C", Surface1: "#1B2330", Surface2: "#2A3547", Overlay: "#1B2330",
			Text: "#BFBDB6", Subtext: "#A6A49D", Dim: "#626A73",
			Accent: "#D2A6FF", Blue: "#59C2FF", Sapphire: "#95E6CB",
			Green: "#AAD94C", Yellow: "#FFB454", Red: "#F07178",
			Peach: "#FF8F40", Teal: "#95E6CB", Flamingo: "#F29668",
			Rosewater: "#E6E1CF", Lavender: "#D2A6FF", Sky: "#73D0FF", Maroon: "#E06C75",
		},
		{
			Name: "Nightfox", Icon: "🦊",
			Base: "#192330", Mantle: "#131A24",
			Surface0: "#29394F", Surface1: "#394B70", Surface2: "#4E5F82", Overlay: "#394B70",
			Text: "#CDCECF", Subtext: "#9DA9BC", Dim: "#738091",
			Accent: "#9D79D6", Blue: "#719CD6", Sapphire: "#63CDCF",
			Green: "#81B29A", Yellow: "#DBC074", Red: "#C94F6D",
			Peach: "#F4A261", Teal: "#63CDCF", Flamingo: "#9D79D6",
			Rosewater: "#CDCECF", Lavender: "#9D79D6", Sky: "#63CDCF", Maroon: "#C94F6D",
		},
		{
			Name: "Grayscale", Icon: "⬛",
			Base: "#000000", Mantle: "#0A0A0A",
			Surface0: "#181818", Surface1: "#2A2A2A", Surface2: "#3E3E3E", Overlay: "#2A2A2A",
			Text: "#F5F5F5", Subtext: "#D6D6D6", Dim: "#A8A8A8",
			Accent: "#FFFFFF", Blue: "#E8E8E8", Sapphire: "#DDDDDD",
			Green: "#D0D0D0", Yellow: "#BEBEBE", Red: "#AAAAAA",
			Peach: "#ECECEC", Teal: "#CCCCCC", Flamingo: "#B4B4B4",
			Rosewater: "#F0F0F0", Lavender: "#D9D9D9", Sky: "#CDCDCD", Maroon: "#989898",
		},
		// Source (OpenCode): https://raw.githubusercontent.com/anomalyco/opencode/dev/packages/opencode/src/cli/cmd/tui/context/theme/opencode.json
		{
			Name: "OpenCode Official", Icon: "◧",
			Base: "#0A0A0A", Mantle: "#141414",
			Surface0: "#1E1E1E", Surface1: "#323232", Surface2: "#3C3C3C", Overlay: "#484848",
			Text: "#EEEEEE", Subtext: "#808080", Dim: "#606060",
			Accent: "#9D7CD8", Blue: "#5C9CF5", Sapphire: "#56B6C2",
			Green: "#7FD88F", Yellow: "#E5C07B", Red: "#E06C75",
			Peach: "#F5A742", Teal: "#56B6C2", Flamingo: "#FAB283",
			Rosewater: "#FFC09F", Lavender: "#9D7CD8", Sky: "#5C9CF5", Maroon: "#C53B53",
		},
		// Source (Claude Code): https://unpkg.com/@anthropic-ai/claude-code@2.1.63/cli.js (theme object Jy5 in vm initializer)
		{
			Name: "Claude Code Dark", Icon: "◨",
			Base: "#000000", Mantle: "#111111",
			Surface0: "#373737", Surface1: "#505050", Surface2: "#888888", Overlay: "#999999",
			Text: "#FFFFFF", Subtext: "#C1C1C1", Dim: "#999999",
			Accent: "#B1B9F9", Blue: "#93A5FF", Sapphire: "#48968C",
			Green: "#4EBA65", Yellow: "#FFC107", Red: "#FF6B80",
			Peach: "#D77757", Teal: "#00CCCC", Flamingo: "#FD5DB1",
			Rosewater: "#EB9F7F", Lavender: "#AF87FF", Sky: "#B1B9F9", Maroon: "#7A2936",
		},
	}
}

func defaultThemeIndex(all []Theme) int {
	for i, t := range all {
		if strings.EqualFold(strings.TrimSpace(t.Name), "Gruvbox") {
			return i
		}
	}
	if len(all) == 0 {
		return 0
	}
	return 0
}

func trimColor(c lipgloss.Color) lipgloss.Color {
	return lipgloss.Color(strings.TrimSpace(string(c)))
}

func normalizeTheme(in Theme) Theme {
	in.Name = strings.TrimSpace(in.Name)
	in.Icon = strings.TrimSpace(in.Icon)
	if in.Icon == "" {
		in.Icon = "🎨"
	}

	in.Base = trimColor(in.Base)
	in.Mantle = trimColor(in.Mantle)
	in.Surface0 = trimColor(in.Surface0)
	in.Surface1 = trimColor(in.Surface1)
	in.Surface2 = trimColor(in.Surface2)
	in.Overlay = trimColor(in.Overlay)
	in.Text = trimColor(in.Text)
	in.Subtext = trimColor(in.Subtext)
	in.Dim = trimColor(in.Dim)
	in.Accent = trimColor(in.Accent)
	in.Blue = trimColor(in.Blue)
	in.Sapphire = trimColor(in.Sapphire)
	in.Green = trimColor(in.Green)
	in.Yellow = trimColor(in.Yellow)
	in.Red = trimColor(in.Red)
	in.Peach = trimColor(in.Peach)
	in.Teal = trimColor(in.Teal)
	in.Flamingo = trimColor(in.Flamingo)
	in.Rosewater = trimColor(in.Rosewater)
	in.Lavender = trimColor(in.Lavender)
	in.Sky = trimColor(in.Sky)
	in.Maroon = trimColor(in.Maroon)

	return in
}

func (t Theme) validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("missing required field: name")
	}
	fields := []struct {
		name  string
		value lipgloss.Color
	}{
		{"base", t.Base}, {"mantle", t.Mantle},
		{"surface0", t.Surface0}, {"surface1", t.Surface1}, {"surface2", t.Surface2}, {"overlay", t.Overlay},
		{"text", t.Text}, {"subtext", t.Subtext}, {"dim", t.Dim},
		{"accent", t.Accent}, {"blue", t.Blue}, {"sapphire", t.Sapphire},
		{"green", t.Green}, {"yellow", t.Yellow}, {"red", t.Red},
		{"peach", t.Peach}, {"teal", t.Teal}, {"flamingo", t.Flamingo},
		{"rosewater", t.Rosewater}, {"lavender", t.Lavender}, {"sky", t.Sky}, {"maroon", t.Maroon},
	}
	missing := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.TrimSpace(string(f.value)) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required color fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

func themeSearchDirs(configDir string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}

	if strings.TrimSpace(configDir) != "" {
		add(filepath.Join(configDir, "themes"))
	}
	if env := strings.TrimSpace(os.Getenv(themeDirEnvVar)); env != "" {
		for _, part := range strings.Split(env, string(os.PathListSeparator)) {
			add(part)
		}
	}
	return out
}

func loadThemesFromDir(dir string) ([]Theme, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read theme dir %s: %w", dir, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	loaded := make([]Theme, 0, len(entries))
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", path, readErr))
			continue
		}

		var t Theme
		if unmarshalErr := json.Unmarshal(data, &t); unmarshalErr != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", path, unmarshalErr))
			continue
		}

		t = normalizeTheme(t)
		if validateErr := t.validate(); validateErr != nil {
			errs = append(errs, fmt.Errorf("validate %s: %w", path, validateErr))
			continue
		}
		loaded = append(loaded, t)
	}

	return loaded, errors.Join(errs...)
}

func mergeThemes(base, extra []Theme) []Theme {
	if len(extra) == 0 {
		return base
	}
	merged := append([]Theme(nil), base...)
	indexByName := make(map[string]int, len(merged))
	for i, t := range merged {
		indexByName[strings.ToLower(strings.TrimSpace(t.Name))] = i
	}
	for _, t := range extra {
		k := strings.ToLower(strings.TrimSpace(t.Name))
		if i, ok := indexByName[k]; ok {
			merged[i] = t
			continue
		}
		indexByName[k] = len(merged)
		merged = append(merged, t)
	}
	return merged
}

func setActiveThemeByNameLocked(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(themes) == 0 {
		return false
	}
	for i, t := range themes {
		if t.Name == name {
			activeThemeIdx = i
			applyTheme(t)
			return true
		}
	}
	needle := strings.ToLower(name)
	for i, t := range themes {
		if strings.ToLower(t.Name) == needle {
			activeThemeIdx = i
			applyTheme(t)
			return true
		}
	}
	return false
}

// LoadThemes reloads the theme catalog from built-ins plus external theme files.
//
// External files are loaded from:
//  1. <configDir>/themes
//  2. each path in OPENUSAGE_THEME_DIR (path-list separated)
//
// Invalid theme files are skipped. The function returns an aggregated error when
// one or more files fail to load, while still keeping valid themes available.
func LoadThemes(configDir string) error {
	themeMu.Lock()
	defer themeMu.Unlock()

	currentName := ""
	if len(themes) > 0 && activeThemeIdx >= 0 && activeThemeIdx < len(themes) {
		currentName = themes[activeThemeIdx].Name
	}

	nextThemes := builtinThemes()
	var errs []error
	for _, dir := range themeSearchDirs(configDir) {
		loaded, err := loadThemesFromDir(dir)
		if err != nil {
			errs = append(errs, err)
		}
		nextThemes = mergeThemes(nextThemes, loaded)
	}

	themes = nextThemes
	if !setActiveThemeByNameLocked(currentName) {
		activeThemeIdx = defaultThemeIndex(themes)
		if len(themes) > 0 {
			applyTheme(themes[activeThemeIdx])
		}
	}

	return errors.Join(errs...)
}

func AvailableThemes() []Theme {
	themeMu.RLock()
	defer themeMu.RUnlock()

	out := make([]Theme, len(themes))
	copy(out, themes)
	return out
}

func ActiveThemeIndex() int {
	themeMu.RLock()
	defer themeMu.RUnlock()
	if len(themes) == 0 {
		return 0
	}
	if activeThemeIdx < 0 || activeThemeIdx >= len(themes) {
		return 0
	}
	return activeThemeIdx
}

func ActiveTheme() Theme {
	themeMu.RLock()
	defer themeMu.RUnlock()
	if len(themes) == 0 {
		return Theme{Name: "Theme", Icon: "🎨"}
	}
	if activeThemeIdx < 0 || activeThemeIdx >= len(themes) {
		return themes[0]
	}
	return themes[activeThemeIdx]
}

func CycleTheme() string {
	themeMu.Lock()
	defer themeMu.Unlock()

	if len(themes) == 0 {
		return ""
	}
	activeThemeIdx = (activeThemeIdx + 1) % len(themes)
	applyTheme(themes[activeThemeIdx])
	return themes[activeThemeIdx].Name
}

func ThemeName() string {
	t := ActiveTheme()
	if t.Name == "" {
		return "🎨 Theme"
	}
	if strings.TrimSpace(t.Icon) == "" {
		return t.Name
	}
	return t.Icon + " " + t.Name
}

func SetThemeByName(name string) bool {
	themeMu.Lock()
	defer themeMu.Unlock()
	return setActiveThemeByNameLocked(name)
}
