// Package sketchybar provides the first-class SketchyBar integration.
//
// The package owns the small managed config block and the scripts it points
// at. It intentionally does not know about a user's plugins directory: the
// installer writes generated scripts under OpenUsage's neutral data path.
package sketchybar

import "time"

const (
	// SentinelStart and SentinelEnd bracket the only section OpenUsage edits
	// in sketchybarrc.
	SentinelStart = "# >>> openusage sketchybar >>> (managed; do not edit between sentinels)"
	SentinelEnd   = "# <<< openusage sketchybar <<<"

	// DefaultPreset is the Catppuccin Macchiato-shaped layout used by the
	// reference configuration.
	DefaultPreset = "catppuccin-macchiato"
)

// Colors is the SketchyBar ARGB palette used by a preset.
type Colors struct {
	Bar     string `json:"bar"`
	Text    string `json:"text"`
	Surface string `json:"surface"`
	Crust   string `json:"crust"`
	Accent  string `json:"accent"`
	Good    string `json:"good"`
	Warn    string `json:"warn"`
	Bad     string `json:"bad"`
	Unknown string `json:"unknown"`
}

// Preset controls the generated item layout. The scripts consume the same
// severity colors through environment assignments in the managed block.
type Preset struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Position     string `json:"position"`
	Item         string `json:"item"`
	Switcher     string `json:"switcher"`
	UpdateFreq   int    `json:"update_freq"`
	Icon         string `json:"icon"`
	SwitcherIcon string `json:"switcher_icon"`
	Colors       Colors `json:"colors"`
}

// InstallOptions controls rendering and installation. Write=false prints a
// complete managed block; Write=true writes the block and embedded scripts.
type InstallOptions struct {
	Write           bool
	Preset          string
	Binary          string
	ConfigPath      string
	DataDir         string
	UsageTrigger    string
	SwitcherTrigger string
	Now             time.Time
}

// DoctorOptions controls the live checks emitted by Doctor.
type DoctorOptions struct {
	ConfigPath string
	DataDir    string
	Binary     string
	Sketchybar string
	// UsageTrigger and SwitcherTrigger are the currently configured
	// gestures. Doctor compares them against the values baked into the
	// installed scripts so an edited config that was never reinstalled is
	// reported rather than silently ignored.
	UsageTrigger    string
	SwitcherTrigger string
}
