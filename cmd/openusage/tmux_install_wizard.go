package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/tmux"
)

// runTmuxInstallWizard is the interactive front-end of `openusage tmux install`.
// It collects position, preset, and icon preference in one small form, then
// applies everything — writes the tmux.conf snippet, installs the icon font,
// and configures the terminal — so the user ends up with a working setup from a
// single command instead of a string of subcommands.
func runTmuxInstallWizard(version string) error {
	position := "right"
	preset := tmux.DefaultPreset
	icons := "emoji"
	if tmux.FontInstalled() {
		icons = "real"
	}

	presetOpts := make([]huh.Option[string], 0)
	for _, p := range tmux.Presets() {
		label := p.Name
		if p.Sample != "" {
			label = fmt.Sprintf("%-16s %s", p.Name, p.Sample)
		}
		presetOpts = append(presetOpts, huh.NewOption(label, p.Name))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Status bar position").
				Description("Where the usage segment sits in your tmux status line.").
				Options(
					huh.NewOption("Right — inner edge of status-right (recommended)", "right"),
					huh.NewOption("Left", "left"),
					huh.NewOption("Both", "both"),
				).
				Value(&position),
			huh.NewSelect[string]().
				Title("Preset").
				Description("The look of the segment. compact is the default.").
				Options(presetOpts...).
				Value(&preset),
			huh.NewSelect[string]().
				Title("Provider icons").
				Description("Emoji works everywhere with no setup. Real icons install a font and configure your terminal.").
				Options(
					huh.NewOption("Emoji — works everywhere, zero setup", "emoji"),
					huh.NewOption("Real provider logos — install font + configure my terminal", "real"),
				).
				Value(&icons),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	// Apply: write the tmux.conf snippet.
	opts := tmux.InstallOptions{Write: true, Position: position, Preset: preset, Version: version}
	path, err := tmux.Install(os.Stdout, opts)
	if err != nil {
		return err
	}
	if path != "" {
		_ = config.SaveIntegrationState("tmux", config.IntegrationState{
			Installed:   true,
			Version:     version,
			InstalledAt: time.Now().UTC().Format(time.RFC3339),
		})
	}

	if icons == "real" {
		applyRealIcons()
	}

	fmt.Fprintf(os.Stdout, "\nDone. Reload tmux:  tmux source-file %s\n", path)
	if icons == "real" {
		fmt.Fprintln(os.Stdout, "Restart your terminal so it picks up the icon font.")
	}
	return nil
}

// applyRealIcons installs the icon font and wires up the terminal: per-range
// fallback for the terminals that support it, and an augmented-font patch for
// iTerm2/Terminal.app (best effort).
func applyRealIcons() {
	if !tmux.FontInstalled() {
		if _, err := tmux.InstallFont(); err != nil {
			fmt.Fprintf(os.Stderr, "tmux: icon font not installed: %v\n", err)
		} else {
			fmt.Fprintf(os.Stdout, "installed %s\n", tmux.IconFontFamily())
		}
	}
	for _, r := range tmux.SetupTerminalFallback() {
		switch r.Action {
		case "configured":
			fmt.Fprintf(os.Stdout, "✓ %s configured (%s)\n", r.Terminal, r.Path)
		case "manual":
			fmt.Fprintf(os.Stdout, "• %s: %s\n", r.Terminal, r.Message)
		case "patch":
			// iTerm2 / Terminal.app: no per-range fallback. Try to augment the
			// terminal font automatically; fall back to instructions.
			if fam, ok := tryPatchTerminalFont(); ok {
				fmt.Fprintf(os.Stdout, "✓ %s: augmented font installed — select \"%s\" in your terminal font settings\n", r.Terminal, fam)
			} else {
				fmt.Fprintf(os.Stdout, "• %s: %s\n", r.Terminal, r.Message)
			}
		}
	}
}

// tryPatchTerminalFont attempts to build and install an augmented copy of the
// terminal's current font (original untouched). Returns the new family name on
// success. Best effort: needs a source checkout (the patch script + SVGs),
// Python 3 with fonttools, and fontconfig to resolve the font file.
func tryPatchTerminalFont() (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	script := locatePatchScript()
	py := findFontPython()
	base := detectITermFontFile()
	if script == "" || py == "" || base == "" {
		return "", false
	}
	dir := tmux.FontInstallDir()
	if dir == "" {
		return "", false
	}
	stem := strings.TrimSuffix(filepath.Base(base), filepath.Ext(base))
	out := filepath.Join(dir, stem+"-OpenUsage"+filepath.Ext(base))
	cmd := exec.Command(py, script, "--base", base, "--out", out, "--name-suffix", " +OpenUsage")
	if err := cmd.Run(); err != nil {
		return "", false
	}
	_ = exec.Command("fc-cache", "-f", dir).Run()
	return resolveFamilyName(out), true
}

// locatePatchScript finds scripts/patch-terminal-font.py relative to the working
// directory (source checkout). Returns "" when not found.
func locatePatchScript() string {
	for _, p := range []string{
		filepath.Join("scripts", "patch-terminal-font.py"),
		filepath.Join("..", "scripts", "patch-terminal-font.py"),
	} {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	return ""
}

// findFontPython returns a python interpreter that has fonttools, or "".
func findFontPython() string {
	candidates := []string{
		filepath.Join(".venv-font", "bin", "python"),
		"python3",
	}
	for _, c := range candidates {
		path := c
		if !strings.Contains(c, string(os.PathSeparator)) {
			p, err := exec.LookPath(c)
			if err != nil {
				continue
			}
			path = p
		} else if _, err := os.Stat(c); err != nil {
			continue
		}
		if exec.Command(path, "-c", "import fontTools").Run() == nil {
			return path
		}
	}
	return ""
}

var itermNormalFontRe = regexp.MustCompile(`"Normal Font"\s*=\s*"([^"]+)"`)

// detectITermFontFile reads iTerm2's configured Normal Font and resolves it to a
// file path via fontconfig. Returns "" when iTerm2/fontconfig are unavailable.
func detectITermFontFile() string {
	out, err := exec.Command("defaults", "read", "com.googlecode.iterm2", "New Bookmarks").Output()
	if err != nil {
		return ""
	}
	m := itermNormalFontRe.FindStringSubmatch(string(out))
	if m == nil {
		return ""
	}
	ps := strings.Fields(m[1]) // "<postscript-name> <size>"
	if len(ps) == 0 {
		return ""
	}
	f, err := exec.Command("fc-match", "-f", "%{file}", "postscriptname="+ps[0]).Output()
	if err != nil {
		return ""
	}
	file := strings.TrimSpace(string(f))
	if file == "" {
		return ""
	}
	return file
}

// resolveFamilyName returns the family (name ID 1) of a font file via
// fontconfig, falling back to the file stem.
func resolveFamilyName(path string) string {
	out, err := exec.Command("fc-query", "-f", "%{family}", path).Output()
	if err == nil {
		if s := strings.TrimSpace(strings.Split(string(out), ",")[0]); s != "" {
			return s
		}
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}
