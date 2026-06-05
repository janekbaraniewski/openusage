// Package tmux is the provider-agnostic formatter and tmux-specific helpers
// that drive the `openusage tmux` subcommand. It must not import internal/tui
// (heavy bubbletea + lipgloss dependency for a once-per-status-interval
// binary); theme data is passed in as plain hex strings instead.
package tmux

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ColorMode selects how color escapes are emitted. truecolor uses 24-bit
// `#[fg=#RRGGBB]`, 256 uses the xterm palette via HexTo256, ansi falls back to
// the 8 base colors, none strips every color directive.
type ColorMode string

const (
	ColorModeTruecolor ColorMode = "truecolor"
	ColorMode256       ColorMode = "256"
	ColorModeANSI      ColorMode = "ansi"
	ColorModeNone      ColorMode = "none"
)

// ParseColorMode maps a user string to a ColorMode. Empty/unknown defaults to
// truecolor (matching the design doc default).
func ParseColorMode(s string) ColorMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "truecolor", "":
		return ColorModeTruecolor
	case "256":
		return ColorMode256
	case "ansi":
		return ColorModeANSI
	case "none":
		return ColorModeNone
	default:
		return ColorModeTruecolor
	}
}

// ThemeColors carries the subset of theme palette fields that the tmux
// formatter exposes as `$name` references inside `#[...]` blocks. Strings are
// hex values (e.g. "#FF6600"); empty strings are skipped during resolution.
//
// Keeping this independent of internal/tui avoids pulling lipgloss/bubbletea
// into a binary that runs once per tmux status-interval tick.
type ThemeColors struct {
	Base     string
	Mantle   string
	Surface0 string
	Surface1 string
	Surface2 string
	Overlay  string
	Text     string
	Subtext  string
	Dim      string
	Accent   string
	Blue     string
	Sapphire string
	Green    string
	Yellow   string
	Red      string
	Peach    string
	Teal     string
	Lavender string
	Sky      string
	Maroon   string
	Mauve    string
}

// ThemeRefs returns the resolution table for `$name` references inside
// `#[...]` blocks. Empty palette entries are dropped so missing colors fall
// through to "" rather than emitting an empty hex.
func ThemeRefs(t ThemeColors, mode ColorMode) map[string]string {
	pairs := map[string]string{
		"base":     t.Base,
		"mantle":   t.Mantle,
		"surface0": t.Surface0,
		"surface1": t.Surface1,
		"surface2": t.Surface2,
		"overlay":  t.Overlay,
		"text":     t.Text,
		"subtext":  t.Subtext,
		"dim":      t.Dim,
		"accent":   t.Accent,
		"blue":     t.Blue,
		"sapphire": t.Sapphire,
		"green":    t.Green,
		"yellow":   t.Yellow,
		"red":      t.Red,
		"peach":    t.Peach,
		"teal":     t.Teal,
		"lavender": t.Lavender,
		"sky":      t.Sky,
		"maroon":   t.Maroon,
		"mauve":    t.Mauve,
	}
	out := make(map[string]string, len(pairs))
	for k, hex := range pairs {
		if strings.TrimSpace(hex) == "" {
			continue
		}
		out[k] = colorForMode(hex, mode)
	}
	return out
}

// colorForMode converts a hex (or pass-through) color reference for the given
// emission mode. For truecolor the hex is returned unchanged; for 256 the
// nearest xterm palette index is returned (formatted "colour###"); for ansi
// the nearest base-8 ANSI color name; for none the empty string.
func colorForMode(value string, mode ColorMode) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	switch mode {
	case ColorModeNone:
		return ""
	case ColorModeTruecolor:
		return v
	case ColorMode256:
		if !strings.HasPrefix(v, "#") {
			return v
		}
		return fmt.Sprintf("colour%d", HexTo256(v))
	case ColorModeANSI:
		if !strings.HasPrefix(v, "#") {
			return v
		}
		return hexToANSIName(v)
	}
	return v
}

// Emit builds a tmux `#[...]` directive (or its ANSI escape equivalent for the
// outside-tmux preview helper). fg and bg may be hex values, palette indices,
// theme refs already-resolved, or empty. mode=none returns an empty string.
//
// The text argument is returned wrapped with the prefix only; callers append a
// `#[default]` reset themselves when needed (matching tmux's own conventions).
func Emit(text, fg, bg string, mode ColorMode) string {
	if mode == ColorModeNone {
		return text
	}
	directives := make([]string, 0, 2)
	if fg = strings.TrimSpace(fg); fg != "" {
		directives = append(directives, "fg="+fg)
	}
	if bg = strings.TrimSpace(bg); bg != "" {
		directives = append(directives, "bg="+bg)
	}
	if len(directives) == 0 {
		return text
	}
	return "#[" + strings.Join(directives, ",") + "]" + text + "#[default]"
}

// HexTo256 returns the nearest xterm 256-color palette index for a hex string
// like "#FF6600" or "FF6600". An invalid hex returns 7 (light grey) so output
// stays renderable instead of breaking the status bar.
func HexTo256(hex string) int {
	r, g, b, ok := parseHexRGB(hex)
	if !ok {
		return 7
	}
	// The xterm 256-color palette covers:
	//   0-15   system colors (we ignore for nearest-match precision)
	//   16-231 6x6x6 RGB cube
	//   232-255 grayscale ramp
	bestIdx := 16
	bestDist := math.MaxFloat64
	for i := 16; i < 256; i++ {
		pr, pg, pb := xterm256RGB(i)
		dr := float64(r) - float64(pr)
		dg := float64(g) - float64(pg)
		db := float64(b) - float64(pb)
		dist := dr*dr + dg*dg + db*db
		if dist < bestDist {
			bestDist = dist
			bestIdx = i
		}
	}
	return bestIdx
}

// xterm256RGB returns the RGB triplet for a given 256-color palette index in
// the 16..255 range.
func xterm256RGB(i int) (int, int, int) {
	if i >= 232 {
		v := 8 + (i-232)*10
		return v, v, v
	}
	idx := i - 16
	r := idx / 36
	g := (idx % 36) / 6
	b := idx % 6
	scale := func(v int) int {
		if v == 0 {
			return 0
		}
		return 55 + v*40
	}
	return scale(r), scale(g), scale(b)
}

var hexRE = regexp.MustCompile(`^#?([0-9a-fA-F]{6})$`)

func parseHexRGB(hex string) (int, int, int, bool) {
	m := hexRE.FindStringSubmatch(strings.TrimSpace(hex))
	if m == nil {
		return 0, 0, 0, false
	}
	val, err := strconv.ParseUint(m[1], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(val>>16) & 0xff, int(val>>8) & 0xff, int(val) & 0xff, true
}

// hexToANSIName returns the nearest base-8 ANSI color name for a hex value.
// Used only by ColorModeANSI; truecolor/256 paths bypass it.
func hexToANSIName(hex string) string {
	r, g, b, ok := parseHexRGB(hex)
	if !ok {
		return "white"
	}
	// Score each base-8 color and pick the closest. The bright palette is
	// intentionally ignored — bright colors render inconsistently across
	// terminals; the eight standard colors are universally supported.
	type ansi struct {
		name    string
		r, g, b int
	}
	table := []ansi{
		{"black", 0, 0, 0},
		{"red", 170, 0, 0},
		{"green", 0, 170, 0},
		{"yellow", 170, 170, 0},
		{"blue", 0, 0, 170},
		{"magenta", 170, 0, 170},
		{"cyan", 0, 170, 170},
		{"white", 170, 170, 170},
	}
	bestName := "white"
	bestDist := math.MaxFloat64
	for _, c := range table {
		dr := float64(r - c.r)
		dg := float64(g - c.g)
		db := float64(b - c.b)
		dist := dr*dr + dg*dg + db*db
		if dist < bestDist {
			bestDist = dist
			bestName = c.name
		}
	}
	return bestName
}
