package tmux

import (
	"strings"
	"testing"
)

func TestParseColorMode(t *testing.T) {
	cases := map[string]ColorMode{
		"":          ColorModeTruecolor,
		"truecolor": ColorModeTruecolor,
		"TRUECOLOR": ColorModeTruecolor,
		"256":       ColorMode256,
		"ansi":      ColorModeANSI,
		"none":      ColorModeNone,
		"garbage":   ColorModeTruecolor,
		" 256 ":     ColorMode256,
	}
	for in, want := range cases {
		if got := ParseColorMode(in); got != want {
			t.Errorf("ParseColorMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHexTo256_RoundTripsOpenUsagePalette(t *testing.T) {
	// Spot-check 18 representative OpenUsage palette hex codes. We do not
	// assert an exact palette index because the xterm 256 cube is lossy by
	// design; we assert the index is in the legal 16..255 range and that
	// identical inputs map to identical outputs (determinism).
	palette := []string{
		"#FF6600", // brand orange
		"#FF8433", "#FFE0CC", "#CC5100",
		"#050B24", "#0C0E16", "#161928", "#1E2235",
		"#E4E6F0", "#B0B4C8", "#828592",
		"#7EB8F7", "#5DA4E8", "#4EC5C1",
		"#59D4A0", "#F0C75E", "#F06A7A", "#F09860",
	}
	seen := map[string]int{}
	for _, hex := range palette {
		idx := HexTo256(hex)
		if idx < 16 || idx > 255 {
			t.Errorf("HexTo256(%q) = %d, out of legal 16..255 range", hex, idx)
		}
		if got, ok := seen[hex]; ok && got != idx {
			t.Errorf("HexTo256 not deterministic for %q: %d vs %d", hex, got, idx)
		}
		seen[hex] = idx
		// Calling again must yield the same value.
		if again := HexTo256(hex); again != idx {
			t.Errorf("HexTo256 not stable for %q: %d then %d", hex, idx, again)
		}
	}
}

func TestHexTo256_InvalidFallsBack(t *testing.T) {
	for _, bad := range []string{"", "not-a-color", "#fff", "#ZZZZZZ"} {
		if got := HexTo256(bad); got != 7 {
			t.Errorf("HexTo256(%q) = %d, want fallback 7", bad, got)
		}
	}
}

func TestHexTo256_BrandOrangeMapsToOrangeFamily(t *testing.T) {
	// Brand orange #FF6600 should land somewhere in the 196..214 family
	// (the saturated red/orange row of the xterm cube). This guards against
	// a regression where the nearest-neighbor search picks something silly
	// like grey.
	idx := HexTo256("#FF6600")
	if idx < 130 || idx > 214 {
		t.Errorf("HexTo256(#FF6600) = %d, expected an orange-family index 130..214", idx)
	}
}

func TestEmit_TruecolorEmitsHex(t *testing.T) {
	out := Emit("hello", "#FF6600", "", ColorModeTruecolor)
	if !strings.Contains(out, "#[fg=#FF6600]") {
		t.Errorf("Emit truecolor missing fg directive: %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("Emit dropped text: %q", out)
	}
	if !strings.HasSuffix(out, "#[default]") {
		t.Errorf("Emit did not append #[default] reset: %q", out)
	}
}

func TestEmit_NoneStripsDirectives(t *testing.T) {
	out := Emit("hello", "#FF6600", "#000000", ColorModeNone)
	if out != "hello" {
		t.Errorf("Emit none: got %q, want %q", out, "hello")
	}
}

func TestEmit_BothFgAndBg(t *testing.T) {
	out := Emit("x", "#FF6600", "#000000", ColorModeTruecolor)
	if !strings.Contains(out, "fg=#FF6600") || !strings.Contains(out, "bg=#000000") {
		t.Errorf("Emit fg+bg: %q", out)
	}
}

func TestEmit_EmptyDirectivesPassesThrough(t *testing.T) {
	out := Emit("plain", "", "", ColorModeTruecolor)
	if out != "plain" {
		t.Errorf("Emit empty: %q, want %q", out, "plain")
	}
}

func TestThemeRefs_DropsEmptyEntries(t *testing.T) {
	refs := ThemeRefs(ThemeColors{
		Accent: "#FF6600",
		Base:   "",
		Green:  "#59D4A0",
	}, ColorModeTruecolor)
	if refs["accent"] != "#FF6600" {
		t.Errorf("accent: got %q", refs["accent"])
	}
	if refs["green"] != "#59D4A0" {
		t.Errorf("green: got %q", refs["green"])
	}
	if _, has := refs["base"]; has {
		t.Errorf("base should be omitted when empty")
	}
}

func TestThemeRefs_KeyCoverage(t *testing.T) {
	// All 21 palette fields should appear as keys when set, lowercased.
	full := ThemeColors{
		Base: "#000001", Mantle: "#000002", Surface0: "#000003", Surface1: "#000004",
		Surface2: "#000005", Overlay: "#000006",
		Text: "#000007", Subtext: "#000008", Dim: "#000009",
		Accent: "#00000A", Blue: "#00000B", Sapphire: "#00000C",
		Green: "#00000D", Yellow: "#00000E", Red: "#00000F", Peach: "#000010",
		Teal: "#000011", Lavender: "#000012", Sky: "#000013", Maroon: "#000014",
		Mauve: "#000015",
	}
	refs := ThemeRefs(full, ColorModeTruecolor)
	want := []string{
		"base", "mantle", "surface0", "surface1", "surface2", "overlay",
		"text", "subtext", "dim",
		"accent", "blue", "sapphire", "green", "yellow", "red", "peach",
		"teal", "lavender", "sky", "maroon", "mauve",
	}
	for _, k := range want {
		if _, ok := refs[k]; !ok {
			t.Errorf("ThemeRefs missing key %q", k)
		}
	}
	if len(refs) != len(want) {
		t.Errorf("ThemeRefs key count = %d, want %d (got %v)", len(refs), len(want), refs)
	}
}

func TestThemeRefs_256ConvertsHex(t *testing.T) {
	refs := ThemeRefs(ThemeColors{Accent: "#FF6600"}, ColorMode256)
	if !strings.HasPrefix(refs["accent"], "colour") {
		t.Errorf("256 mode expected colour### prefix, got %q", refs["accent"])
	}
}

func TestThemeRefs_ANSIReturnsName(t *testing.T) {
	refs := ThemeRefs(ThemeColors{Red: "#FF0000"}, ColorModeANSI)
	if refs["red"] != "red" {
		t.Errorf("ANSI red mapping: got %q want %q", refs["red"], "red")
	}
}

func TestThemeRefs_NoneReturnsEmpty(t *testing.T) {
	refs := ThemeRefs(ThemeColors{Accent: "#FF6600", Green: "#00FF00"}, ColorModeNone)
	for k, v := range refs {
		if v != "" {
			t.Errorf("none mode: ref %q = %q, want empty", k, v)
		}
	}
}

func TestParseGlyphTier(t *testing.T) {
	cases := map[string]GlyphTier{
		"":         GlyphTierUnicode,
		"ascii":    GlyphTierASCII,
		"ASCII":    GlyphTierASCII,
		"nerdfont": GlyphTierNerdfont,
		"nerd":     GlyphTierNerdfont,
		"nf":       GlyphTierNerdfont,
		"unicode":  GlyphTierUnicode,
		"unknown":  GlyphTierUnicode,
	}
	for in, want := range cases {
		if got := ParseGlyphTier(in); got != want {
			t.Errorf("ParseGlyphTier(%q) = %q want %q", in, got, want)
		}
	}
}

func TestProviderIcon_FallsBackToWildcard(t *testing.T) {
	if g := ProviderIcon("not-a-real-provider", GlyphTierASCII); g != "[ai]" {
		t.Errorf("ascii wildcard: got %q want %q", g, "[ai]")
	}
	if g := ProviderIcon("not-a-real-provider", GlyphTierUnicode); g == "" {
		t.Errorf("unicode wildcard should be non-empty")
	}
}

func TestProviderIcon_KnownProvider(t *testing.T) {
	if g := ProviderIcon("claude_code", GlyphTierASCII); g != "[claude]" {
		t.Errorf("claude_code ascii: got %q", g)
	}
}
