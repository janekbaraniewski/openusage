package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Terminal font setup.
//
// The *preferred* way to render provider icons is per-range font fallback: tell
// the terminal "for codepoints U+E900..E912, use OpenUsage Icons", leaving the
// user's main font untouched. kitty, Ghostty, and WezTerm support this. iTerm2,
// Terminal.app, and Alacritty do NOT — for those the only option is augmenting
// the user's font (see scripts/patch-terminal-font.py / `tmux font patch`).
//
// This file auto-configures the fallback-capable terminals (idempotent, via a
// sentinel-bracketed block) and reports clear next steps for the rest.

const (
	termSentinelStart = "# >>> openusage icons >>> (managed; do not edit between sentinels)"
	termSentinelEnd   = "# <<< openusage icons <<<"
)

// TermSetupResult is the outcome of configuring one terminal.
type TermSetupResult struct {
	Terminal string // e.g. "kitty"
	Action   string // "configured", "manual", "patch", "absent"
	Path     string // config file written, when Action == "configured"
	Message  string // human-facing detail / instructions
}

// SetupTerminalFallback configures every detected fallback-capable terminal to
// use the bundled icon font for the icon codepoint range, and returns a result
// per terminal (including manual-step terminals). It does not install the font;
// callers should ensure FontInstalled() first.
func SetupTerminalFallback() []TermSetupResult {
	var out []TermSetupResult
	if r, ok := setupKitty(); ok {
		out = append(out, r)
	}
	if r, ok := setupGhostty(); ok {
		out = append(out, r)
	}
	if r, ok := weztermGuidance(); ok {
		out = append(out, r)
	}
	out = append(out, iterm2Guidance()...)
	return out
}

// --- kitty ------------------------------------------------------------------

func kittyConfigPath() string {
	if d := os.Getenv("KITTY_CONFIG_DIRECTORY"); d != "" {
		return filepath.Join(d, "kitty.conf")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "kitty", "kitty.conf")
}

func setupKitty() (TermSetupResult, bool) {
	path := kittyConfigPath()
	if path == "" {
		return TermSetupResult{}, false
	}
	// Only act if kitty appears to be in use (its config dir exists).
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return TermSetupResult{}, false
	}
	lo, hi := IconCodepointRange()
	block := fmt.Sprintf("%s\nsymbol_map U+%04X-U+%04X %s\n%s\n",
		termSentinelStart, lo, hi, IconFontFamily(), termSentinelEnd)
	if err := writeManagedBlock(path, block); err != nil {
		return TermSetupResult{Terminal: "kitty", Action: "manual", Message: err.Error()}, true
	}
	return TermSetupResult{
		Terminal: "kitty", Action: "configured", Path: path,
		Message: "added symbol_map for the icon range; reload kitty config or restart kitty",
	}, true
}

// --- ghostty ----------------------------------------------------------------

func ghosttyConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	// Ghostty reads ~/.config/ghostty/config on all platforms (also an
	// Application Support path on macOS, but the XDG-style one works for both).
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ghostty", "config")
	}
	return filepath.Join(home, ".config", "ghostty", "config")
}

func setupGhostty() (TermSetupResult, bool) {
	path := ghosttyConfigPath()
	if path == "" {
		return TermSetupResult{}, false
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return TermSetupResult{}, false
	}
	// Ghostty falls back to additional font-family entries for missing glyphs.
	block := fmt.Sprintf("%s\nfont-family = %q\n%s\n",
		termSentinelStart, IconFontFamily(), termSentinelEnd)
	if err := writeManagedBlock(path, block); err != nil {
		return TermSetupResult{Terminal: "ghostty", Action: "manual", Message: err.Error()}, true
	}
	return TermSetupResult{
		Terminal: "ghostty", Action: "configured", Path: path,
		Message: "added OpenUsage Icons as a fallback font-family; restart Ghostty",
	}, true
}

// --- wezterm (guidance; Lua config is not safe to auto-edit) -----------------

func weztermConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	for _, p := range []string{
		filepath.Join(home, ".wezterm.lua"),
		filepath.Join(home, ".config", "wezterm", "wezterm.lua"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func weztermGuidance() (TermSetupResult, bool) {
	path := weztermConfigPath()
	if path == "" {
		return TermSetupResult{}, false
	}
	msg := "WezTerm config is Lua; add OpenUsage Icons to your font fallback by hand:\n" +
		"    config.font = wezterm.font_with_fallback {\n" +
		"      '<your current font>',\n" +
		"      'OpenUsage Icons',\n" +
		"    }"
	return TermSetupResult{Terminal: "wezterm", Action: "manual", Path: path, Message: msg}, true
}

// --- iterm2 / terminal.app / alacritty (no per-range fallback) ---------------

func iterm2Guidance() []TermSetupResult {
	if runtime.GOOS != "darwin" {
		return nil
	}
	var out []TermSetupResult
	if _, err := os.Stat("/Applications/iTerm.app"); err == nil {
		out = append(out, TermSetupResult{
			Terminal: "iTerm2", Action: "patch",
			Message: "iTerm2 has no per-range fallback. Run `openusage tmux font patch` to install an augmented copy of your terminal font (your original is untouched), then select it in iTerm2.",
		})
	}
	return out
}

// --- managed-block writer ---------------------------------------------------

// writeManagedBlock appends or replaces the openusage sentinel block in a
// config file, creating the file and parent dir if needed, with a .bak of any
// prior non-empty content. The block edit itself uses the shared helper in
// managed.go (same semantics as the tmux.conf install).
func writeManagedBlock(path, block string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("tmux: reading %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("tmux: creating %s: %w", filepath.Dir(path), err)
	}
	if len(existing) > 0 {
		_ = os.WriteFile(path+".bak", existing, 0o600)
	}
	updated := replaceOrAppendBlock(existing, termSentinelStart, termSentinelEnd, block)
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("tmux: writing %s: %w", path, err)
	}
	return nil
}
