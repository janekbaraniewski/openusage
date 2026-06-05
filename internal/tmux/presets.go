package tmux

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Preset is one named, pre-baked status template. Each preset bundles a
// display-ready format string with the glyph tier it was designed for, plus a
// canned sample for the `openusage tmux presets` listing.
type Preset struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Format      string `json:"format"`
	Glyphs      string `json:"glyphs,omitempty"`
	Sample      string `json:"sample,omitempty"`
}

//go:embed presets/*.json
var presetsFS embed.FS

// Presets returns the full built-in preset catalog, sorted by name. The
// catalog is loaded once at first call and cached for subsequent calls.
func Presets() []Preset {
	loadPresetsOnce()
	out := make([]Preset, 0, len(presetsCache))
	for _, p := range presetsCache {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SamplePreset returns the named preset or an error if it does not exist.
func SamplePreset(name string) (Preset, error) {
	loadPresetsOnce()
	if p, ok := presetsCache[strings.ToLower(strings.TrimSpace(name))]; ok {
		return p, nil
	}
	return Preset{}, fmt.Errorf("tmux: unknown preset %q", name)
}

// DefaultPreset is the preset used when neither --preset nor --format is
// supplied. It is intentionally short so it fits on narrow status bars.
const DefaultPreset = "compact"

var presetsCache map[string]Preset

func loadPresetsOnce() {
	if presetsCache != nil {
		return
	}
	presetsCache = map[string]Preset{}
	entries, err := presetsFS.ReadDir("presets")
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, readErr := presetsFS.ReadFile("presets/" + e.Name())
		if readErr != nil {
			continue
		}
		var p Preset
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		if p.Name == "" {
			p.Name = strings.TrimSuffix(e.Name(), ".json")
		}
		presetsCache[strings.ToLower(p.Name)] = p
	}
}
