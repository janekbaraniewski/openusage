package tui

import (
	"embed"
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

//go:embed bundled_themes/*.json
var bundledThemesFS embed.FS

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

// themeMu protects the theme catalog (themes slice) and the active theme index
// (activeThemeIdx). These are the only variables guarded by this mutex.
//
// The color and style globals in styles.go (colorBase, colorAccent, headerStyle,
// etc.) are written by applyTheme and read by all rendering functions. These
// globals are intentionally NOT protected by themeMu because Bubble Tea's
// concurrency model provides safety:
//
//   - applyTheme is called from init() (single-threaded startup), from
//     LoadThemes/SetThemeByName (called before tea.Program.Run), and from
//     CycleTheme/SetThemeByName (called from Update key handlers).
//   - All rendering (View, renderHeader, etc.) runs on the same Bubble Tea
//     goroutine as Update, so there is no concurrent read/write on the globals.
//
// Callers outside the Bubble Tea goroutine (e.g., background tasks) MUST NOT
// call CycleTheme, SetThemeByName, or read color globals directly. Use
// AvailableThemes/ActiveTheme/ActiveThemeIndex for safe catalog access.
//
// Locking protocol:
//   - Write lock (themeMu.Lock): LoadThemes, CycleTheme, SetThemeByName
//   - Read lock (themeMu.RLock): AvailableThemes, ActiveTheme, ActiveThemeIndex
//   - No lock: applyTheme (always called while write lock is held, or at init)
var (
	themeMu        sync.RWMutex
	themes         []Theme
	activeThemeIdx int
)

func init() {
	themes = loadDefaultThemes()
	activeThemeIdx = defaultThemeIndex(themes)
	if len(themes) > 0 {
		applyTheme(themes[activeThemeIdx])
	}
}

// defaultTheme is the single hardcoded fallback theme — a custom deep-space
// palette with vibrant accent colors designed for high contrast and readability.
func defaultTheme() Theme {
	return Theme{
		Name: "Deep Space", Icon: "✦",
		Base: "#0C0E16", Mantle: "#080A11",
		Surface0: "#161928", Surface1: "#1E2235", Surface2: "#2A2F47", Overlay: "#1E2235",
		Text: "#E4E6F0", Subtext: "#B0B4C8", Dim: "#5C6180",
		Accent: "#7EB8F7", Blue: "#5DA4E8", Sapphire: "#4EC5C1",
		Green: "#59D4A0", Yellow: "#F0C75E", Red: "#F06A7A",
		Peach: "#F09860", Teal: "#4EC5C1", Flamingo: "#E878B0",
		Rosewater: "#F0D0C0", Lavender: "#A899F0", Sky: "#70C8E8", Maroon: "#C44B5C",
	}
}

// loadDefaultThemes returns the default theme plus all bundled JSON themes.
func loadDefaultThemes() []Theme {
	all := []Theme{defaultTheme()}

	entries, err := bundledThemesFS.ReadDir("bundled_themes")
	if err != nil {
		return all
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		data, readErr := bundledThemesFS.ReadFile("bundled_themes/" + entry.Name())
		if readErr != nil {
			continue
		}
		var t Theme
		if unmarshalErr := json.Unmarshal(data, &t); unmarshalErr != nil {
			continue
		}
		t = normalizeTheme(t)
		if validateErr := t.validate(); validateErr != nil {
			continue
		}
		all = append(all, t)
	}
	return all
}

func defaultThemeIndex(all []Theme) int {
	for i, t := range all {
		if strings.EqualFold(strings.TrimSpace(t.Name), "Deep Space") {
			return i
		}
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

// setActiveThemeByNameLocked sets the active theme by name. The caller MUST
// hold themeMu for writing (the "Locked" suffix indicates the lock is already
// held). This function writes to activeThemeIdx and calls applyTheme.
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

// LoadThemes reloads the theme catalog from the default theme, bundled themes,
// plus external theme files.
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

	nextThemes := loadDefaultThemes()
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
