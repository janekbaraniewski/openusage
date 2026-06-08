//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var itermNormalFontRe = regexp.MustCompile(`"Normal Font"\s*=\s*"([^"]+)"`)

// detectTerminalFontFile resolves the font file backing iTerm2's configured
// font so it can be augmented. Returns a clear error when the font cannot be
// determined or is already an OpenUsage-augmented font.
func detectTerminalFontFile() (string, error) {
	ps := itermNormalFontPSName()
	if ps == "" {
		return "", fmt.Errorf("could not read iTerm2's configured font; pass --base <font file>")
	}
	if strings.Contains(strings.ToLower(ps), "openusage") {
		return "", fmt.Errorf("iTerm2 already uses an augmented font (%s) — nothing to do", ps)
	}
	file := resolveFontFileByPSName(ps)
	if file == "" {
		return "", fmt.Errorf("could not locate the file for iTerm2's font %q; pass --base <font file>", ps)
	}
	return file, nil
}

// itermNormalFontPSName returns the PostScript name of iTerm2's configured
// Normal Font (the value is "<postscript-name> <size>"), or "".
func itermNormalFontPSName() string {
	out, err := exec.Command("defaults", "read", "com.googlecode.iterm2", "New Bookmarks").Output()
	if err != nil {
		return ""
	}
	m := itermNormalFontRe.FindStringSubmatch(string(out))
	if m == nil {
		return ""
	}
	fields := strings.Fields(m[1])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// resolveFontFileByPSName maps a PostScript name to its file path using macOS's
// system_profiler (reliable, unlike fontconfig which may not index PostScript
// names and falls back to a default). Returns "" when not found.
func resolveFontFileByPSName(ps string) string {
	out, err := exec.Command("system_profiler", "-json", "SPFontsDataType").Output()
	if err != nil {
		return ""
	}
	var data struct {
		Fonts []struct {
			Path      string `json:"path"`
			Typefaces []struct {
				Name string `json:"_name"`
			} `json:"typefaces"`
		} `json:"SPFontsDataType"`
	}
	if json.Unmarshal(out, &data) != nil {
		return ""
	}
	for _, f := range data.Fonts {
		for _, tf := range f.Typefaces {
			if tf.Name == ps {
				return f.Path
			}
		}
	}
	return ""
}
