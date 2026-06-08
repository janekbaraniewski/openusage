package tmux

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
)

// GlyphTier selects the icon set used by `:icon` and preset glyph references.
// The ladder (best to safest) is:
//
//	customfont — OpenUsage's bundled provider-icon font (real brand glyphs at
//	             Private Use Area codepoints). Requires the font to be installed
//	             so the terminal can fall back to it for those codepoints
//	             (`openusage tmux font install`).
//	nerdfont   — assumes a Nerd Font is installed.
//	unicode    — emoji/symbols, the default; works in most terminals.
//	ascii      — always-safe bracketed labels, e.g. [claude].
//
// Providers without a glyph in a given tier fall back to the next-safest tier
// (customfont → unicode) or to the tier's "*" entry.
type GlyphTier string

const (
	GlyphTierASCII      GlyphTier = "ascii"
	GlyphTierUnicode    GlyphTier = "unicode"
	GlyphTierNerdfont   GlyphTier = "nerdfont"
	GlyphTierCustomFont GlyphTier = "customfont"
)

// ParseGlyphTier resolves a tier string. Empty/unknown defaults to unicode.
func ParseGlyphTier(s string) GlyphTier {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ascii":
		return GlyphTierASCII
	case "nerdfont", "nerd", "nf":
		return GlyphTierNerdfont
	case "customfont", "custom", "openusage":
		return GlyphTierCustomFont
	case "unicode", "":
		return GlyphTierUnicode
	default:
		return GlyphTierUnicode
	}
}

//go:embed assets/icons.json
var iconManifestJSON []byte

// iconManifest is the parsed form of assets/icons.json — the single source of
// truth shared with scripts/gen-icon-font.py. Keeping the provider→codepoint
// mapping in one file guarantees the renderer and the generated font agree.
type iconManifest struct {
	Family  string `json:"family"`
	Version string `json:"version"`
	Glyphs  []struct {
		Provider  string `json:"provider"`
		SVG       string `json:"svg"`
		Codepoint string `json:"codepoint"`
	} `json:"glyphs"`
}

var (
	customFontOnce  sync.Once
	customFontMap   map[string]string
	iconFamilyName  = "OpenUsage Icons"
	iconFontVersion = "0"
)

func loadCustomFontMap() {
	customFontMap = map[string]string{}
	var m iconManifest
	if err := json.Unmarshal(iconManifestJSON, &m); err != nil {
		return
	}
	if strings.TrimSpace(m.Family) != "" {
		iconFamilyName = m.Family
	}
	if strings.TrimSpace(m.Version) != "" {
		iconFontVersion = m.Version
	}
	for _, g := range m.Glyphs {
		cp, err := strconv.ParseInt(strings.TrimSpace(g.Codepoint), 16, 32)
		if err != nil {
			continue
		}
		customFontMap[strings.ToLower(g.Provider)] = string(rune(cp))
	}
}

// customFontIcon returns the bundled-font glyph (a PUA rune) for a provider, or
// "" if the provider has no bundled glyph.
func customFontIcon(provider string) string {
	customFontOnce.Do(loadCustomFontMap)
	return customFontMap[provider]
}

// IconFontFamily returns the family name of the bundled icon font (from the
// manifest). Used by the font install/detect helpers.
func IconFontFamily() string {
	customFontOnce.Do(loadCustomFontMap)
	return iconFamilyName
}

// IconFontVersion returns the manifest version of the bundled icon font.
func IconFontVersion() string {
	customFontOnce.Do(loadCustomFontMap)
	return iconFontVersion
}

// CustomFontProviders returns the provider IDs that have a bundled glyph.
// Exposed for tests and the `tmux font status` command.
func CustomFontProviders() []string {
	customFontOnce.Do(loadCustomFontMap)
	out := make([]string, 0, len(customFontMap))
	for k := range customFontMap {
		out = append(out, k)
	}
	return out
}

// providerIcons maps provider IDs to their per-tier glyph. The fallback in
// each tier is keyed by "*" and is used when a provider has no entry.
var providerIcons = map[GlyphTier]map[string]string{
	GlyphTierASCII: {
		"*":             "[ai]",
		"claude_code":   "[claude]",
		"anthropic":     "[claude]",
		"codex":         "[codex]",
		"openai":        "[openai]",
		"cursor":        "[cursor]",
		"copilot":       "[copilot]",
		"gemini_cli":    "[gemini]",
		"gemini_api":    "[gemini]",
		"openrouter":    "[or]",
		"aider":         "[aider]",
		"ollama":        "[ollama]",
		"opencode":      "[oc]",
		"groq":          "[groq]",
		"mistral":       "[mistral]",
		"deepseek":      "[deepseek]",
		"xai":           "[xai]",
		"perplexity":    "[pplx]",
		"alibaba_cloud": "[qwen]",
		"zai":           "[zai]",
		"moonshot":      "[kimi]",
	},
	GlyphTierUnicode: {
		"*":             "✨", // sparkles
		"claude_code":   "\U0001F916",
		"anthropic":     "\U0001F916",
		"codex":         "⚡",
		"openai":        "⚡",
		"cursor":        "▸",
		"copilot":       "\U0001F9E0",
		"gemini_cli":    "✨",
		"gemini_api":    "✨",
		"openrouter":    "\U0001F500",
		"aider":         "\U0001F527",
		"ollama":        "\U0001F999",
		"opencode":      "●",
		"groq":          "⚡",
		"mistral":       "\U0001F32C",
		"deepseek":      "\U0001F50D",
		"xai":           "✖",
		"perplexity":    "❔",
		"alibaba_cloud": "云",
		"zai":           "✦",
		"moonshot":      "\U0001F319", // crescent moon
	},
	GlyphTierNerdfont: {
		"*":             "", // nf-fa-rocket
		"claude_code":   "", // nf-dev-claude-ish (placeholder)
		"anthropic":     "",
		"codex":         "",
		"openai":        "",
		"cursor":        "",
		"copilot":       "",
		"gemini_cli":    "",
		"gemini_api":    "",
		"openrouter":    "",
		"aider":         "",
		"ollama":        "",
		"opencode":      "",
		"groq":          "",
		"mistral":       "",
		"deepseek":      "",
		"xai":           "",
		"perplexity":    "",
		"alibaba_cloud": "",
	},
}

// ProviderIcon returns the glyph for a provider in the given tier. The custom
// font is a partial overlay: providers with a bundled glyph use it, and any
// provider without one falls back to the unicode tier so the segment is never
// blank. Unknown providers fall back to the tier's "*" entry; unknown tiers
// fall back to unicode.
func ProviderIcon(provider string, tier GlyphTier) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	if tier == GlyphTierCustomFont {
		if g := customFontIcon(p); g != "" {
			return g
		}
		// No bundled glyph for this provider: degrade to the unicode emoji
		// rather than emit nothing.
		tier = GlyphTierUnicode
	}
	tbl, ok := providerIcons[tier]
	if !ok {
		tbl = providerIcons[GlyphTierUnicode]
	}
	if g, ok := tbl[p]; ok && g != "" {
		return g
	}
	return tbl["*"]
}

// barGlyphs returns the (filled, empty) cell glyphs used by the `:bar`
// modifier for a given tier. ASCII uses `#` and `.` so output remains safe in
// terminals without unicode; unicode uses heavy-shade blocks; nerdfont uses
// powerline-style separators.
func barGlyphs(tier GlyphTier) (string, string) {
	switch tier {
	case GlyphTierASCII:
		return "#", "."
	case GlyphTierNerdfont:
		return "█", "░"
	default:
		return "▓", "░"
	}
}
