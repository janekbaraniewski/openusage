package tmux

import "strings"

// GlyphTier selects the icon set used by `:icon` and preset glyph references.
// ascii is the always-safe fallback; unicode is the default for most presets;
// nerdfont assumes a Nerd Font is installed.
type GlyphTier string

const (
	GlyphTierASCII    GlyphTier = "ascii"
	GlyphTierUnicode  GlyphTier = "unicode"
	GlyphTierNerdfont GlyphTier = "nerdfont"
)

// ParseGlyphTier resolves a tier string. Empty/unknown defaults to unicode.
func ParseGlyphTier(s string) GlyphTier {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ascii":
		return GlyphTierASCII
	case "nerdfont", "nerd", "nf":
		return GlyphTierNerdfont
	case "unicode", "":
		return GlyphTierUnicode
	default:
		return GlyphTierUnicode
	}
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

// ProviderIcon returns the glyph for a provider in the given tier. Unknown
// providers fall back to the tier's "*" entry. Unknown tiers fall back to
// unicode.
func ProviderIcon(provider string, tier GlyphTier) string {
	p := strings.ToLower(strings.TrimSpace(provider))
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
