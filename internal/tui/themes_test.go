package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func snapshotThemeState() ([]Theme, int) {
	themeMu.RLock()
	defer themeMu.RUnlock()

	copyThemes := make([]Theme, len(themes))
	copy(copyThemes, themes)
	return copyThemes, activeThemeIdx
}

func restoreThemeState(saved []Theme, savedIdx int) {
	themeMu.Lock()
	defer themeMu.Unlock()

	themes = make([]Theme, len(saved))
	copy(themes, saved)
	if len(themes) == 0 {
		activeThemeIdx = 0
		return
	}
	if savedIdx < 0 || savedIdx >= len(themes) {
		savedIdx = defaultThemeIndex(themes)
	}
	activeThemeIdx = savedIdx
	applyTheme(themes[activeThemeIdx])
}

func writeThemeFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write theme file %s: %v", path, err)
	}
}

func externalThemeJSON(name, icon, accent string) string {
	return `{
  "name": "` + name + `",
  "icon": "` + icon + `",
  "base": "#111111",
  "mantle": "#161616",
  "surface0": "#232323",
  "surface1": "#303030",
  "surface2": "#424242",
  "overlay": "#303030",
  "text": "#E8E8E8",
  "subtext": "#BDBDBD",
  "dim": "#7F7F7F",
  "accent": "` + accent + `",
  "blue": "#CFCFCF",
  "sapphire": "#BBBBBB",
  "green": "#ABABAB",
  "yellow": "#9A9A9A",
  "red": "#878787",
  "peach": "#DCDCDC",
  "teal": "#B3B3B3",
  "flamingo": "#8F8F8F",
  "rosewater": "#E1E1E1",
  "lavender": "#C4C4C4",
  "sky": "#B4B4B4",
  "maroon": "#757575"
}`
}

func TestBuiltinThemesIncludeCalmPresets(t *testing.T) {
	savedThemes, savedIdx := snapshotThemeState()
	defer restoreThemeState(savedThemes, savedIdx)

	if err := LoadThemes(t.TempDir()); err != nil {
		t.Fatalf("LoadThemes error: %v", err)
	}

	list := AvailableThemes()
	found := make(map[string]bool)
	for _, theme := range list {
		found[theme.Name] = true
	}
	for _, expected := range []string{"Grayscale", "OpenCode Official", "Claude Code Dark"} {
		if !found[expected] {
			t.Fatalf("%s theme not found in available themes", expected)
		}
	}
}

func TestLoadThemesFromConfigDir(t *testing.T) {
	savedThemes, savedIdx := snapshotThemeState()
	defer restoreThemeState(savedThemes, savedIdx)

	cfgDir := t.TempDir()
	themesDir := filepath.Join(cfgDir, "themes")
	writeThemeFile(t, themesDir, "custom-gray.json", externalThemeJSON("Custom Gray", "◼", "#FAFAFA"))

	if err := LoadThemes(cfgDir); err != nil {
		t.Fatalf("LoadThemes error: %v", err)
	}

	if !SetThemeByName("Custom Gray") {
		t.Fatalf("SetThemeByName(Custom Gray) returned false")
	}
	active := ActiveTheme()
	if active.Name != "Custom Gray" {
		t.Fatalf("active theme = %q, want Custom Gray", active.Name)
	}
	if active.Accent != lipgloss.Color("#FAFAFA") {
		t.Fatalf("accent = %q, want #FAFAFA", active.Accent)
	}
}

func TestLoadThemesCanOverrideBuiltinByName(t *testing.T) {
	savedThemes, savedIdx := snapshotThemeState()
	defer restoreThemeState(savedThemes, savedIdx)

	cfgDir := t.TempDir()
	themesDir := filepath.Join(cfgDir, "themes")
	writeThemeFile(t, themesDir, "gruvbox-override.json", externalThemeJSON("Gruvbox", "🌻", "#FFFFFF"))

	if err := LoadThemes(cfgDir); err != nil {
		t.Fatalf("LoadThemes error: %v", err)
	}
	if !SetThemeByName("Gruvbox") {
		t.Fatalf("SetThemeByName(Gruvbox) returned false")
	}
	active := ActiveTheme()
	if active.Accent != lipgloss.Color("#FFFFFF") {
		t.Fatalf("accent = %q, want #FFFFFF", active.Accent)
	}
}

func TestLoadThemesFromEnvPath(t *testing.T) {
	savedThemes, savedIdx := snapshotThemeState()
	defer restoreThemeState(savedThemes, savedIdx)

	extraDir := t.TempDir()
	writeThemeFile(t, extraDir, "env-theme.json", externalThemeJSON("Env Gray", "◻", "#F0F0F0"))
	t.Setenv(themeDirEnvVar, extraDir)

	if err := LoadThemes(t.TempDir()); err != nil {
		t.Fatalf("LoadThemes error: %v", err)
	}
	if !SetThemeByName("Env Gray") {
		t.Fatalf("SetThemeByName(Env Gray) returned false")
	}
}

func TestLoadThemesReportsInvalidThemeFiles(t *testing.T) {
	savedThemes, savedIdx := snapshotThemeState()
	defer restoreThemeState(savedThemes, savedIdx)

	cfgDir := t.TempDir()
	themesDir := filepath.Join(cfgDir, "themes")
	writeThemeFile(t, themesDir, "broken.json", `{"name":"Broken"}`)

	err := LoadThemes(cfgDir)
	if err == nil {
		t.Fatal("expected error for invalid theme file")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "missing required color fields") {
		t.Fatalf("unexpected error: %v", err)
	}

	if !SetThemeByName("Gruvbox") {
		t.Fatal("expected built-in themes to remain available")
	}
}
