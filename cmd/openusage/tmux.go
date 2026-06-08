package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/export"
	"github.com/janekbaraniewski/openusage/internal/tmux"
	"github.com/janekbaraniewski/openusage/internal/tui"
	"github.com/janekbaraniewski/openusage/internal/version"
)

// tmuxFlags carries the shared render-time flags. Subcommands instantiate
// their own flag set; this struct only powers the default render command.
type tmuxFlags struct {
	preset     string
	format     string
	segment    string
	provider   string
	strategy   string
	colorMode  string
	glyphs     string
	theme      string
	source     string
	maxRuntime time.Duration
	raw        bool
	noColor    bool
	noTrueCol  bool
	jsonOut    bool
	noCache    bool
}

// newTmuxCommand returns the full `openusage tmux` command tree. The default
// run renders the status line; subcommands cover install/uninstall, the
// preset and variable catalogs, doctor diagnostics, the ANSI preview helper,
// and the watch alerter.
func newTmuxCommand() *cobra.Command {
	f := &tmuxFlags{
		colorMode:  "truecolor",
		source:     string(export.SourceAuto),
		maxRuntime: 800 * time.Millisecond,
	}

	cmd := &cobra.Command{
		Use:   "tmux",
		Short: "Render a one-line tmux status segment for AI tool usage",
		Long: `Render a one-line tmux status segment with usage data for the active AI tool.

By default the command picks the most recently-used provider (recency then
priority order) and renders the "compact" preset. Pass --preset, --format, or
--segment to customize. Pass --json for structured output.

Run "openusage tmux install" to wire it into your tmux.conf.`,
		Example: strings.Join([]string{
			"  openusage tmux",
			"  openusage tmux --preset claude-focused",
			"  openusage tmux --format '{tool} {today_cost:money}'",
			"  openusage tmux --segment cost",
			"  openusage tmux --json",
		}, "\n"),
		RunE: func(c *cobra.Command, _ []string) error {
			return runTmuxRender(c, f)
		},
	}

	fl := cmd.Flags()
	fl.StringVar(&f.preset, "preset", "", "named preset (see `openusage tmux presets`)")
	fl.StringVar(&f.format, "format", "", "custom template; overrides preset")
	fl.StringVar(&f.segment, "segment", "", "render a single named segment")
	fl.StringVar(&f.provider, "provider", "", "pin a provider id (skips auto-detection)")
	fl.StringVar(&f.strategy, "strategy", "", "active-tool detection strategy list (default: recency,priority)")
	fl.StringVar(&f.colorMode, "color-mode", f.colorMode, "truecolor|256|ansi|none")
	fl.StringVar(&f.glyphs, "glyphs", "", "ascii|unicode|nerdfont (default per preset)")
	fl.StringVar(&f.theme, "theme", "", "override the configured theme")
	fl.StringVar(&f.source, "source", f.source, "snapshot source: auto, direct, or daemon")
	fl.DurationVar(&f.maxRuntime, "max-runtime", f.maxRuntime, "self-kill budget so tmux never blocks")
	fl.BoolVar(&f.raw, "raw", false, "force tmux-format output even when stdout is a TTY")
	fl.BoolVar(&f.noColor, "no-color", false, "strip all #[...] tokens (equivalent to --color-mode=none)")
	fl.BoolVar(&f.noTrueCol, "no-truecolor", false, "downgrade truecolor output to 256")
	fl.BoolVar(&f.jsonOut, "json", false, "emit structured JSON output")
	fl.BoolVar(&f.noCache, "no-cache", false, "bypass the active-tool detection cache")

	cmd.MarkFlagsMutuallyExclusive("preset", "format", "segment", "json")

	cmd.AddCommand(newTmuxInstallCommand())
	cmd.AddCommand(newTmuxUninstallCommand())
	cmd.AddCommand(newTmuxPresetsCommand())
	cmd.AddCommand(newTmuxVariablesCommand())
	cmd.AddCommand(newTmuxDoctorCommand())
	cmd.AddCommand(newTmuxPreviewCommand(f))
	cmd.AddCommand(newTmuxWatchCommand())
	cmd.AddCommand(newTmuxFontCommand())

	return cmd
}

// newTmuxFontCommand manages the bundled provider-icon font: installing it into
// the user font directory, checking install/version state, and removing it.
// When installed, `openusage tmux` auto-upgrades the default unicode glyphs to
// the real provider icons.
func newTmuxFontCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "font",
		Short: "Install or check the bundled provider-icon font",
		Long: `Install the OpenUsage provider-icon font so the status bar can render real
provider logos instead of emoji.

The font ships glyphs at Private Use Area codepoints; your terminal falls back
to it for those codepoints once it is installed system-wide. After installing,
restart your terminal and tmux. Providers without a bundled glyph fall back to
the unicode emoji, and providers fall back further to ASCII labels with
--glyphs ascii.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install the icon font into your user font directory",
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := tmux.InstallFont()
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "installed %s v%s at %s\n", tmux.IconFontFamily(), tmux.IconFontVersion(), path)
			fmt.Fprintln(os.Stdout, "Restart your terminal and tmux so they pick up the new font.")
			fmt.Fprintln(os.Stdout, "It is used automatically by the default preset; force it with `--glyphs customfont`.")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Remove the installed icon font",
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := tmux.UninstallFont()
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "removed icon font at %s\n", path)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "setup",
		Short: "Auto-configure your terminal(s) to render the provider icons",
		Long: `Configure detected terminals to use the bundled icon font for the icon
codepoints, the preferred way (per-range fallback — your main font is left
untouched). Works for kitty, Ghostty, and WezTerm. iTerm2 / Terminal.app have
no per-range fallback; for those, use ` + "`openusage tmux font patch`" + `.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			// The fallback only works if the font is installed, so ensure it.
			if !tmux.FontInstalled() {
				if _, err := tmux.InstallFont(); err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "installed %s\n", tmux.IconFontFamily())
			}
			results := tmux.SetupTerminalFallback()
			if len(results) == 0 {
				fmt.Fprintln(os.Stdout, "No supported terminals detected. Supported: kitty, Ghostty, WezTerm (per-range fallback); iTerm2/Terminal.app via `font patch`.")
				return nil
			}
			for _, r := range results {
				switch r.Action {
				case "configured":
					fmt.Fprintf(os.Stdout, "✓ %s: %s\n  %s\n", r.Terminal, r.Path, r.Message)
				case "manual":
					fmt.Fprintf(os.Stdout, "• %s (manual step):\n  %s\n", r.Terminal, r.Message)
				case "patch":
					fmt.Fprintf(os.Stdout, "• %s: %s\n", r.Terminal, r.Message)
				}
			}
			fmt.Fprintln(os.Stdout, "\nRestart the affected terminals to load the font.")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show whether the icon font is installed and up to date",
		RunE: func(_ *cobra.Command, _ []string) error {
			st := tmux.FontStatus()
			fmt.Fprintf(os.Stdout, "family:    %s\n", st.Family)
			fmt.Fprintf(os.Stdout, "version:   %s\n", st.Version)
			fmt.Fprintf(os.Stdout, "path:      %s\n", st.Path)
			fmt.Fprintf(os.Stdout, "glyphs:    %d providers\n", len(tmux.CustomFontProviders()))
			switch {
			case !st.Installed:
				fmt.Fprintln(os.Stdout, "installed: no   (run: openusage tmux font install)")
			case st.UpToDate:
				fmt.Fprintln(os.Stdout, "installed: yes, up to date")
			default:
				fmt.Fprintln(os.Stdout, "installed: yes, but OUTDATED (run: openusage tmux font install to update)")
				fmt.Fprintf(os.Stdout, "  embedded sha256:  %s\n", st.EmbeddedSHA)
				fmt.Fprintf(os.Stdout, "  installed sha256: %s\n", st.InstalledSHA)
			}
			return nil
		},
	})
	return cmd
}

// runTmuxRender is the default `openusage tmux` entry point. It applies the
// max-runtime budget so a slow daemon can never freeze tmux: on timeout we
// emit a `?` placeholder and exit 0 so the status bar keeps ticking.
func runTmuxRender(c *cobra.Command, f *tmuxFlags) error {
	cfg, _ := config.Load()
	opts := resolveTmuxOptions(c, f, cfg)

	// Smart TTY hint: when not inside tmux and stdout is a terminal, and no
	// override flags were passed, point users at the install command.
	if !opts.raw && !opts.jsonOut && os.Getenv("TMUX") == "" && isStdoutTerminal() &&
		!c.Flags().Changed("preset") && !c.Flags().Changed("format") && !c.Flags().Changed("segment") {
		fmt.Fprintln(os.Stdout, "not running inside tmux. Try `openusage tmux install` to add it to your status bar.")
		return nil
	}

	ctx, cancel := context.WithTimeout(c.Context(), opts.maxRuntime)
	defer cancel()

	rendered, bctx, detected, err := buildAndRender(ctx, opts)
	if err != nil {
		// Never block tmux. Prefer the last good status (so a transient daemon
		// hiccup does not blank the bar); fall back to "?" only if there is no
		// recent cache.
		if last, ok := readLastStatus(); ok {
			fmt.Fprintln(os.Stdout, last)
			return nil
		}
		fmt.Fprintf(os.Stderr, "tmux: %v\n", err)
		fmt.Fprintln(os.Stdout, "?")
		return nil
	}

	if opts.jsonOut {
		payload := tmux.BuildJSON(bctx, rendered, detected)
		return tmux.WriteJSON(os.Stdout, payload)
	}

	// Anti-flicker: when the snapshot read was degraded (e.g. the daemon read
	// timed out and there was no time to fall back), reuse the last good
	// status instead of flashing a blank/zeroed segment. Only successful
	// renders update the cache.
	if bctx.Degraded {
		if last, ok := readLastStatus(); ok {
			fmt.Fprintln(os.Stdout, last)
			return nil
		}
	} else if strings.TrimSpace(rendered) != "" {
		writeLastStatus(rendered)
	}

	fmt.Fprintln(os.Stdout, rendered)
	return nil
}

// lastStatusTTL caps how stale a cached status may be before it is treated as
// missing. Long enough to ride out daemon restarts, short enough that a truly
// idle machine eventually shows live (possibly empty) state.
const lastStatusTTL = 10 * time.Minute

// lastStatusPath is the cache file for the most recent successful render.
func lastStatusPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "openusage", "tmux-laststatus")
}

// readLastStatus returns the last good rendered status if it exists and is
// within lastStatusTTL.
func readLastStatus() (string, bool) {
	path := lastStatusPath()
	if path == "" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > lastStatusTTL {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	s := strings.TrimRight(string(data), "\n")
	if strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

// writeLastStatus records a successful render for reuse on the next degraded
// read. Failures are silent: the cache is a best-effort flicker guard.
func writeLastStatus(rendered string) {
	path := lastStatusPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(rendered), 0o600)
}

// tmuxOptions is the post-resolution form of tmuxFlags; one struct that the
// renderer, the JSON marshaller, and the smart-TTY check all read from.
type tmuxOptions struct {
	preset     string
	format     string
	segment    string
	provider   string
	strategy   string
	colorMode  tmux.ColorMode
	glyphs     tmux.GlyphTier
	theme      tmux.ThemeColors
	source     export.Source
	maxRuntime time.Duration
	raw        bool
	jsonOut    bool
	noCache    bool
	cfg        config.TmuxConfig
}

// resolveTmuxOptions folds CLI flags, settings.json, preset defaults, and
// hardcoded fallbacks into a single options struct. Cobra's Flags().Changed
// is the source of truth for "user passed it" so a default value that
// happens to match a config field does not override the config.
func resolveTmuxOptions(c *cobra.Command, f *tmuxFlags, cfg config.Config) tmuxOptions {
	tcfg := cfg.Tmux

	opts := tmuxOptions{
		preset:     orString(f.preset, tcfg.Preset, tmux.DefaultPreset),
		format:     orString(f.format, tcfg.Format, ""),
		segment:    f.segment,
		provider:   orString(f.provider, tcfg.Provider, ""),
		strategy:   orString(f.strategy, tcfg.ActiveStrategy, ""),
		maxRuntime: f.maxRuntime,
		raw:        f.raw,
		jsonOut:    f.jsonOut,
		noCache:    f.noCache,
		cfg:        tcfg,
	}

	// Color mode resolution: --no-color > --no-truecolor > --color-mode > settings > default.
	switch {
	case f.noColor:
		opts.colorMode = tmux.ColorModeNone
	case f.noTrueCol:
		opts.colorMode = tmux.ColorMode256
	case c.Flags().Changed("color-mode"):
		opts.colorMode = tmux.ParseColorMode(f.colorMode)
	case strings.TrimSpace(tcfg.ColorMode) != "":
		opts.colorMode = tmux.ParseColorMode(tcfg.ColorMode)
	default:
		opts.colorMode = tmux.ParseColorMode(f.colorMode)
	}

	// Glyph tier: flag > settings > preset default, then an auto-upgrade to the
	// bundled icon font when it is installed. The auto-upgrade only kicks in
	// when the user did not choose a tier explicitly and the resolved tier is
	// the plain "unicode" default — so an explicit --glyphs, a configured tier,
	// or an ascii/nerdfont preset are all respected as-is.
	explicitGlyphs := strings.TrimSpace(f.glyphs) != "" || strings.TrimSpace(tcfg.Glyphs) != ""
	glyphRaw := f.glyphs
	if glyphRaw == "" {
		glyphRaw = tcfg.Glyphs
	}
	if glyphRaw == "" {
		if p, err := tmux.SamplePreset(opts.preset); err == nil {
			glyphRaw = p.Glyphs
		}
	}
	opts.glyphs = tmux.ParseGlyphTier(glyphRaw)
	if !explicitGlyphs && opts.glyphs == tmux.GlyphTierUnicode && tmux.FontInstalled() {
		opts.glyphs = tmux.GlyphTierCustomFont
	}

	// Source: flag > settings > auto.
	if c.Flags().Changed("source") {
		opts.source = export.Source(strings.ToLower(strings.TrimSpace(f.source)))
	} else if v := strings.TrimSpace(tcfg.Source); v != "" {
		opts.source = export.Source(strings.ToLower(v))
	} else {
		opts.source = export.Source(strings.ToLower(strings.TrimSpace(f.source)))
	}

	// Theme: flag > settings.tmux.theme > settings.theme.
	themeName := f.theme
	if themeName == "" {
		themeName = tcfg.Theme
	}
	if themeName == "" {
		themeName = cfg.Theme
	}
	if themeName != "" {
		tui.SetThemeByName(themeName)
	}
	opts.theme = themeColorsFromTUI(tui.ActiveTheme())

	return opts
}

// themeColorsFromTUI maps the bubbletea-based tui.Theme palette into the
// plain-string ThemeColors used by the tmux formatter. Keeping the mapping
// here (rather than inside internal/tmux) preserves the design's hard rule
// that internal/tmux must not import internal/tui.
func themeColorsFromTUI(t tui.Theme) tmux.ThemeColors {
	return tmux.ThemeColors{
		Base:     string(t.Base),
		Mantle:   string(t.Mantle),
		Surface0: string(t.Surface0),
		Surface1: string(t.Surface1),
		Surface2: string(t.Surface2),
		Overlay:  string(t.Overlay),
		Text:     string(t.Text),
		Subtext:  string(t.Subtext),
		Dim:      string(t.Dim),
		Accent:   string(t.Accent),
		Blue:     string(t.Blue),
		Sapphire: string(t.Sapphire),
		Green:    string(t.Green),
		Yellow:   string(t.Yellow),
		Red:      string(t.Red),
		Peach:    string(t.Peach),
		Teal:     string(t.Teal),
		Lavender: string(t.Lavender),
		Sky:      string(t.Sky),
		Maroon:   string(t.Maroon),
		Mauve:    string(t.Mauve),
	}
}

// buildAndRender resolves the active provider, builds the formatter context,
// and renders the chosen template. Returns the rendered string, the
// detection result (so JSON output can echo it), and any non-fatal error.
func buildAndRender(ctx context.Context, opts tmuxOptions) (string, tmux.Context, *tmux.DetectResult, error) {
	res := tmux.Detect(tmux.DetectOptions{
		Strategy:      opts.strategy,
		PriorityOrder: opts.cfg.PriorityOrder,
		RecencyWindow: parseDurationOr(opts.cfg.RecencyWindow, 0),
		Pinned:        opts.provider,
		NoCache:       opts.noCache,
	})

	bctx, err := tmux.BuildContext(ctx, tmux.BuildOptions{
		Source:               opts.source,
		Provider:             opts.provider,
		Candidates:           candidatesFrom(res),
		Theme:                opts.theme,
		ColorMode:            opts.colorMode,
		Glyphs:               opts.glyphs,
		Variables:            opts.cfg.Variables,
		Segments:             opts.cfg.Segments,
		ColorRules:           configColorRules(opts.cfg.ColorRules),
		OfflineClaudePricing: true,
	})
	if err != nil {
		return "", bctx, &res, err
	}

	template, err := resolveTemplate(opts)
	if err != nil {
		return "", bctx, &res, err
	}

	rendered, err := tmux.Render(template, bctx)
	if err != nil {
		return "", bctx, &res, err
	}
	return rendered, bctx, &res, nil
}

// candidatesFrom flattens a detection result into the recency-ordered
// candidate list BuildContext walks when no provider is pinned.
func candidatesFrom(res tmux.DetectResult) []string {
	if len(res.Ordered) > 0 {
		return res.Ordered
	}
	if res.Primary != "" {
		return []string{res.Primary}
	}
	return nil
}

// resolveTemplate decides which template the renderer should evaluate. It is
// extracted from buildAndRender so the precedence rules (segment > format >
// preset) are easy to read and the cobra wiring can call it from a future
// dry-run command if needed.
func resolveTemplate(opts tmuxOptions) (string, error) {
	if seg := strings.TrimSpace(opts.segment); seg != "" {
		return "{" + seg + "}", nil
	}
	if fmtStr := strings.TrimSpace(opts.format); fmtStr != "" {
		return fmtStr, nil
	}
	preset, err := tmux.SamplePreset(opts.preset)
	if err != nil {
		return "", err
	}
	return preset.Format, nil
}

// configColorRules converts the config-side ColorRule map into the
// formatter-internal type. Both shapes are intentionally identical; the
// duplication is justified by the design's no-tui-import rule (config can
// import core but not tmux, and tmux must not import config).
func configColorRules(in map[string]config.ColorRule) map[string]tmux.ColorRule {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]tmux.ColorRule, len(in))
	for k, v := range in {
		out[k] = tmux.ColorRule{
			LowAt:       v.LowAt,
			MediumAt:    v.MediumAt,
			HighAt:      v.HighAt,
			LowColor:    v.LowColor,
			MediumColor: v.MediumColor,
			HighColor:   v.HighColor,
		}
	}
	return out
}

// --- install / uninstall ----------------------------------------------------

func newTmuxInstallCommand() *cobra.Command {
	opts := tmux.InstallOptions{
		Position:    "right",
		Preset:      tmux.DefaultPreset,
		Interval:    5,
		RightLength: 200,
		LeftLength:  80,
	}
	var withFont, noFont bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Set up the tmux status segment (interactive wizard, or flag-driven)",
		RunE: func(c *cobra.Command, _ []string) error {
			opts.Version = version.Version
			// Bare `tmux install` on an interactive terminal launches the
			// one-stop wizard (position, preset, icons, terminal setup). Passing
			// any flag, or a non-TTY, keeps the scriptable flag-driven behavior.
			if c.Flags().NFlag() == 0 && isStdinTerminal() && isStdoutTerminal() {
				return runTmuxInstallWizard(version.Version)
			}
			if opts.Write {
				path, err := tmux.Install(os.Stdout, opts)
				if err != nil {
					return err
				}
				if path != "" {
					_ = config.SaveIntegrationState("tmux", config.IntegrationState{
						Installed:   true,
						Version:     version.Version,
						InstalledAt: time.Now().UTC().Format(time.RFC3339),
					})
				}
				offerFontInstall(withFont, noFont)
				return nil
			}
			_, err := tmux.Install(os.Stdout, opts)
			return err
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&opts.Write, "write", false, "apply to tmux.conf (creates a .bak backup)")
	fl.StringVar(&opts.Position, "position", opts.Position, "left|right|both")
	fl.StringVar(&opts.Preset, "preset", opts.Preset, "embedded preset name")
	fl.IntVar(&opts.Interval, "interval", opts.Interval, "tmux status-interval")
	fl.IntVar(&opts.RightLength, "right-length", opts.RightLength, "tmux status-right-length")
	fl.IntVar(&opts.LeftLength, "left-length", opts.LeftLength, "tmux status-left-length")
	fl.StringVar(&opts.BindPopup, "bind-popup", "", "bind a key to display-popup -E openusage (tmux 3.2+)")
	fl.StringVar(&opts.BindRefresh, "bind-refresh", "", "bind a key to refresh the status bar on demand")
	fl.StringVar(&opts.Binary, "binary", "", "override the openusage binary path in the snippet")
	fl.BoolVar(&withFont, "with-font", false, "install the bundled provider-icon font without prompting")
	fl.BoolVar(&noFont, "no-font", false, "skip the provider-icon font prompt entirely")
	return cmd
}

// offerFontInstall nudges the user to install the bundled provider-icon font
// after a tmux install. We push it: the prompt defaults to Yes. force (from
// --with-font) installs without asking; skip (from --no-font) does nothing.
// On a non-interactive stdin we don't block on a prompt — we print a one-line
// pointer instead.
func offerFontInstall(force, skip bool) {
	if skip {
		return
	}
	st := tmux.FontStatus()
	if st.Installed && st.UpToDate {
		return // already good, nothing to nudge
	}

	install := force
	if !force {
		if !isStdinTerminal() {
			fmt.Fprintln(os.Stdout, "Tip: install the provider-icon font for real provider logos in your status bar:")
			fmt.Fprintln(os.Stdout, "       openusage tmux font install")
			return
		}
		verb := "Install"
		if st.Installed && !st.UpToDate {
			verb = "Update"
		}
		fmt.Fprintln(os.Stdout, "")
		fmt.Fprintln(os.Stdout, "OpenUsage ships an icon font so your status bar shows real provider logos")
		fmt.Fprintln(os.Stdout, "(Claude, Cursor, Codex, …) instead of emoji.")
		install = promptYesNo(fmt.Sprintf("%s the provider-icon font now? [Y/n] ", verb), true)
	}
	if !install {
		fmt.Fprintln(os.Stdout, "Skipped. Install it anytime with: openusage tmux font install")
		return
	}
	path, err := tmux.InstallFont()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tmux: icon font not installed: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stdout, "installed %s at %s\n", tmux.IconFontFamily(), path)
	fmt.Fprintln(os.Stdout, "Restart your terminal and tmux to see the icons.")
}

// promptYesNo asks a yes/no question on stdin, returning def on an empty reply.
func promptYesNo(prompt string, def bool) bool {
	fmt.Fprint(os.Stdout, prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return def
	case "y", "yes":
		return true
	default:
		return false
	}
}

// isStdinTerminal reports whether stdin is a real interactive terminal, so we
// only prompt when a human can actually answer. term.IsTerminal does a real
// TTY ioctl, so it correctly returns false for pipes and /dev/null (an
// os.ModeCharDevice check would wrongly treat /dev/null as a terminal).
func isStdinTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func newTmuxUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the openusage block from tmux.conf",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := tmux.Uninstall(os.Stdout, ""); err != nil {
				return err
			}
			_ = config.SaveIntegrationState("tmux", config.IntegrationState{Installed: false})
			return nil
		},
	}
}

// --- presets / variables ----------------------------------------------------

func newTmuxPresetsCommand() *cobra.Command {
	var show string
	cmd := &cobra.Command{
		Use:   "presets",
		Short: "List the built-in status-bar presets",
		RunE: func(_ *cobra.Command, _ []string) error {
			if show != "" {
				p, err := tmux.SamplePreset(show)
				if err != nil {
					return err
				}
				return json.NewEncoder(os.Stdout).Encode(p)
			}
			presets := tmux.Presets()
			fmt.Fprintf(os.Stdout, "%-18s %-9s %s\n", "NAME", "GLYPHS", "SAMPLE")
			for _, p := range presets {
				glyphs := p.Glyphs
				if glyphs == "" {
					glyphs = "unicode"
				}
				fmt.Fprintf(os.Stdout, "%-18s %-9s %s\n", p.Name, glyphs, p.Sample)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&show, "show", "", "dump a single preset as JSON")
	return cmd
}

func newTmuxVariablesCommand() *cobra.Command {
	var provider string
	var markdown bool
	cmd := &cobra.Command{
		Use:   "variables",
		Short: "List the template variables available to --format",
		RunE: func(_ *cobra.Command, _ []string) error {
			vars := collectKnownVariables()
			sort.Strings(vars)
			if markdown {
				fmt.Fprintln(os.Stdout, "| Variable | Kind |")
				fmt.Fprintln(os.Stdout, "| --- | --- |")
				for _, v := range vars {
					fmt.Fprintf(os.Stdout, "| `{%s}` | %s |\n", v, classifyVariable(v))
				}
				return nil
			}
			if provider != "" {
				fmt.Fprintf(os.Stdout, "Variables for provider %q (semantic aliases shown; provider-native metric keys also valid):\n\n", provider)
			}
			for _, v := range vars {
				fmt.Fprintf(os.Stdout, "  {%s}\t%s\n", v, classifyVariable(v))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "(informational) scope to a provider")
	cmd.Flags().BoolVar(&markdown, "markdown", false, "emit as a Markdown table")
	return cmd
}

// collectKnownVariables merges the semantic alias names, built-in segment
// names, and a small set of always-available bare names. The result is what
// `openusage tmux variables` exposes to users.
func collectKnownVariables() []string {
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
		}
	}
	for _, name := range tmux.SegmentNames() {
		add(name)
	}
	for _, name := range tmux.AliasNames() {
		add(name)
	}
	for _, name := range []string{"tool", "provider", "account", "model"} {
		add(name)
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func classifyVariable(name string) string {
	switch name {
	case "tool", "provider", "account", "model":
		return "snapshot attribute"
	}
	for _, seg := range tmux.SegmentNames() {
		if seg == name {
			return "segment"
		}
	}
	return "semantic alias"
}

// --- doctor / preview / watch ----------------------------------------------

func newTmuxDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose tmux, daemon, theme, and snippet state",
		RunE: func(_ *cobra.Command, _ []string) error {
			return tmux.Run(os.Stdout, tmux.DoctorOptions{})
		},
	}
}

func newTmuxPreviewCommand(parent *tmuxFlags) *cobra.Command {
	f := &tmuxFlags{
		colorMode:  parent.colorMode,
		source:     parent.source,
		maxRuntime: parent.maxRuntime,
	}
	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Render the status line with ANSI escapes for terminals outside tmux",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, _ := config.Load()
			f.raw = true
			opts := resolveTmuxOptions(c, f, cfg)
			ctx, cancel := context.WithTimeout(c.Context(), opts.maxRuntime)
			defer cancel()
			rendered, _, _, err := buildAndRender(ctx, opts)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, tmux.Preview(rendered))
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.preset, "preset", "", "named preset")
	fl.StringVar(&f.format, "format", "", "custom template; overrides preset")
	fl.StringVar(&f.segment, "segment", "", "render a single named segment")
	fl.StringVar(&f.provider, "provider", "", "pin a provider id")
	fl.StringVar(&f.strategy, "strategy", "", "active-tool detection strategy list")
	fl.StringVar(&f.colorMode, "color-mode", f.colorMode, "truecolor|256|ansi|none")
	fl.StringVar(&f.glyphs, "glyphs", "", "ascii|unicode|nerdfont")
	fl.StringVar(&f.theme, "theme", "", "override the configured theme")
	fl.StringVar(&f.source, "source", f.source, "snapshot source: auto, direct, or daemon")
	return cmd
}

func newTmuxWatchCommand() *cobra.Command {
	var (
		background bool
		alertMode  string
		interval   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Foreground push-alert loop (display-message on threshold cross)",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, _ := config.Load()
			if background {
				prev, err := tmux.WritePIDFile(tmux.DefaultPIDFile())
				if err != nil {
					return err
				}
				if prev > 0 {
					fmt.Fprintf(os.Stdout, "tmux watch: replacing previous instance pid=%d\n", prev)
				}
			}
			return tmux.Watch(c.Context(), tmux.WatchOptions{
				Interval: interval,
				Alerts:   cfg.Tmux.Alerts,
				Mode:     tmux.ParseAlertMode(alertMode),
				Out:      os.Stderr,
				PIDFile:  tmux.DefaultPIDFile(),
			})
		},
	}
	cmd.Flags().BoolVar(&background, "background", false, "write a pidfile so a second invocation replaces the first")
	cmd.Flags().StringVar(&alertMode, "alert-mode", "", "message|bell|both|none")
	cmd.Flags().DurationVar(&interval, "interval", 0, "poll interval (default 5s)")
	return cmd
}

// --- helpers ----------------------------------------------------------------

func orString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseDurationOr(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

// isStdoutTerminal reports whether stdout is connected to a TTY. Used by the
// smart-hint behavior so users running `openusage tmux` interactively (no
// flags, not inside tmux) get a friendly install pointer instead of a
// tmux-format string. The os.ModeCharDevice check mirrors the convention
// used by spinner.go and statusline.go.
func isStdoutTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
