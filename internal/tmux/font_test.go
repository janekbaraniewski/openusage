package tmux

import (
	"testing"
)

func TestParseGlyphTierCustomFont(t *testing.T) {
	for _, s := range []string{"customfont", "custom", "openusage", "CustomFont"} {
		if got := ParseGlyphTier(s); got != GlyphTierCustomFont {
			t.Errorf("ParseGlyphTier(%q) = %q, want customfont", s, got)
		}
	}
}

func TestProviderIconCustomFont(t *testing.T) {
	// A provider with a bundled glyph resolves to its PUA rune (E900 for
	// claude_code per assets/icons.json).
	if got := ProviderIcon("claude_code", GlyphTierCustomFont); got != string(rune(0xE900)) {
		t.Errorf("claude_code customfont = %q (%U), want U+E900", got, []rune(got))
	}

	// A provider WITHOUT a bundled glyph falls back to the unicode emoji, never
	// blank.
	got := ProviderIcon("aider", GlyphTierCustomFont)
	if got == "" {
		t.Fatal("aider customfont should fall back to a non-empty unicode glyph")
	}
	if got != ProviderIcon("aider", GlyphTierUnicode) {
		t.Errorf("aider customfont = %q, want unicode fallback %q", got, ProviderIcon("aider", GlyphTierUnicode))
	}

	// An unknown provider falls back to the unicode "*" sentinel.
	if got := ProviderIcon("nope-not-real", GlyphTierCustomFont); got != providerIcons[GlyphTierUnicode]["*"] {
		t.Errorf("unknown customfont = %q, want unicode fallback %q", got, providerIcons[GlyphTierUnicode]["*"])
	}
}

func TestCustomFontMapMatchesManifest(t *testing.T) {
	provs := CustomFontProviders()
	if len(provs) == 0 {
		t.Fatal("expected at least one bundled provider glyph")
	}
	// Every bundled provider must resolve to a single PUA rune in the customfont tier.
	for _, p := range provs {
		g := ProviderIcon(p, GlyphTierCustomFont)
		r := []rune(g)
		if len(r) != 1 || r[0] < 0xE000 || r[0] > 0xF8FF {
			t.Errorf("provider %q customfont glyph %q is not a single PUA rune", p, g)
		}
	}
}

func TestEmbeddedFontPresent(t *testing.T) {
	if len(EmbeddedIconFont()) == 0 {
		t.Fatal("embedded icon font is empty — was scripts/gen-icon-font.py run?")
	}
	if EmbeddedFontSHA256() == "" {
		t.Fatal("embedded font sha256 should not be empty")
	}
}

func TestFontStatusNotInstalled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := FontStatus()
	if st.Installed {
		t.Fatal("font should not be installed in a fresh HOME")
	}
	if st.UpToDate {
		t.Fatal("an uninstalled font cannot be up to date")
	}
	if st.Family == "" || st.Version == "" {
		t.Errorf("status should report family/version even when not installed: %+v", st)
	}
}
