package sketchybar

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed presets/*.json
var presetsFS embed.FS

var (
	presetsOnce  sync.Once
	presetsCache map[string]Preset
)

// Presets returns the embedded preset catalog sorted by name.
func Presets() []Preset {
	loadPresets()
	out := make([]Preset, 0, len(presetsCache))
	for _, preset := range presetsCache {
		out = append(out, preset)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SamplePreset returns a named preset, accepting an empty name as the
// configured default.
func SamplePreset(name string) (Preset, error) {
	loadPresets()
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = DefaultPreset
	}
	if preset, ok := presetsCache[name]; ok {
		return preset, nil
	}
	return Preset{}, fmt.Errorf("sketchybar: unknown preset %q", name)
}

func loadPresets() {
	presetsOnce.Do(func() {
		presetsCache = make(map[string]Preset)
		entries, err := presetsFS.ReadDir("presets")
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			data, err := presetsFS.ReadFile("presets/" + entry.Name())
			if err != nil {
				continue
			}
			var preset Preset
			if err := json.Unmarshal(data, &preset); err != nil || strings.TrimSpace(preset.Name) == "" {
				continue
			}
			presetsCache[strings.ToLower(preset.Name)] = preset
		}
	})
}
