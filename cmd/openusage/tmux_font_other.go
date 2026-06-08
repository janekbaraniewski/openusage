//go:build !darwin

package main

import "fmt"

// detectTerminalFontFile auto-detection is macOS/iTerm2-only. On other
// platforms the user passes --base explicitly (and terminals with per-range
// fallback should use `tmux font setup` instead of patching).
func detectTerminalFontFile() (string, error) {
	return "", fmt.Errorf("auto-detecting the terminal font is only supported on macOS; pass --base <font file>")
}
