package tmux

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/lo"
)

// Sentinel markers bracket the openusage-managed snippet inside tmux.conf so
// install/uninstall can find and rewrite it in place without disturbing
// adjacent user config. The markers also let the doctor command verify that
// the snippet is still installed.
const (
	sentinelStart = "# >>> openusage tmux >>> (managed; do not edit between sentinels)"
	sentinelEnd   = "# <<< openusage tmux <<<"
)

// InstallOptions configures the tmux.conf snippet emitted by Install. Each
// field maps to a CLI flag; zero/empty values fall back to sensible defaults.
type InstallOptions struct {
	// Position is "left", "right", or "both". Empty means "right".
	Position string
	// Preset is the named preset embedded in the snippet's status line
	// command. Empty means DefaultPreset.
	Preset string
	// Providers pins one segment per provider id (e.g. show Claude Code and
	// Cursor side by side, each with its own metrics). Empty means a single
	// segment with auto-detection (the active tool).
	Providers []string
	// Interval is the tmux status-interval written to the snippet. Zero
	// means 5.
	Interval int
	// RightLength sets status-right-length. Zero means 200.
	RightLength int
	// LeftLength sets status-left-length. Zero means 80.
	LeftLength int
	// BindPopup, when non-empty, is a key letter (e.g. "u") that gets
	// bound to a display-popup running `openusage`. Requires tmux 3.2+.
	BindPopup string
	// BindRefresh, when non-empty, is a key letter that triggers a
	// status-bar refresh on demand.
	BindRefresh string
	// Write controls whether Install applies the snippet to tmux.conf
	// (true) or only prints it to the writer (false).
	Write bool
	// Binary overrides the openusage binary path used in the snippet's
	// status-line command. Empty means `openusage`.
	Binary string
	// ConfPath overrides the auto-detected tmux.conf location. Tests use
	// it to point at a temp file.
	ConfPath string
	// Version is recorded in the IntegrationState entry after a successful
	// write. Callers pass the binary's version string.
	Version string
	// Now is injected by tests; zero means time.Now().
	Now time.Time
}

// DetectTmuxConf returns the tmux.conf path to use, following the documented
// preference order:
//
//  1. `$XDG_CONFIG_HOME/tmux/tmux.conf` if XDG_CONFIG_HOME is set
//  2. `~/.config/tmux/tmux.conf`
//  3. `~/.tmux.conf`
//
// When none exists the XDG path is returned so callers can create it.
// Multiple existing candidates emit a warning to warnOut (when non-nil) and
// the most-preferred one wins.
func DetectTmuxConf(warnOut io.Writer) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("tmux: resolving home directory: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("tmux: no home directory")
	}

	candidates := []string{}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "tmux", "tmux.conf"))
	}
	candidates = append(candidates,
		filepath.Join(home, ".config", "tmux", "tmux.conf"),
		filepath.Join(home, ".tmux.conf"),
	)

	existing := []string{}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			existing = append(existing, c)
		}
	}

	if len(existing) == 0 {
		// No conf yet: prefer the XDG-style path.
		return candidates[0], nil
	}
	if len(existing) > 1 && warnOut != nil {
		fmt.Fprintf(warnOut, "tmux: multiple tmux configs found (%s); using %s\n",
			strings.Join(existing, ", "), existing[0])
	}
	return existing[0], nil
}

// BuildSnippet returns the sentinel-bracketed tmux.conf snippet for opts. It
// is pure (no I/O), used by Install (which appends/replaces in place) and by
// the print mode (which only writes to stdout).
func BuildSnippet(opts InstallOptions) string {
	opts = withInstallDefaults(opts)
	binary := strings.TrimSpace(opts.Binary)
	if binary == "" {
		binary = "openusage"
	}

	var b strings.Builder
	b.WriteString(sentinelStart)
	b.WriteString("\n")
	fmt.Fprintf(&b, "set -g status-interval %d\n", opts.Interval)
	fmt.Fprintf(&b, "set -g status-right-length %d\n", opts.RightLength)
	fmt.Fprintf(&b, "set -g status-left-length %d\n", opts.LeftLength)

	switch opts.Position {
	case "left":
		// Append to status-left so the segment(s) sit at the inner (right)
		// edge of the left side, next to the window list.
		inners := segmentInners(binary, opts.Preset, opts.Providers)
		cmd := strings.Join(lo.Map(inners, func(i string, _ int) string { return "#" + i }), " │ ")
		fmt.Fprintf(&b, "set -ga status-left %q\n", cmd)
	case "both":
		fmt.Fprintf(&b, "set -ga status-left %q\n", fmt.Sprintf("#(%s tmux --preset compact --segment tool)", binary))
		b.WriteString(prependStatusRight(binary, opts.Preset, opts.Providers))
	default: // right
		b.WriteString(prependStatusRight(binary, opts.Preset, opts.Providers))
	}

	if key := strings.TrimSpace(opts.BindPopup); key != "" {
		fmt.Fprintf(&b, "bind-key %s display-popup -E -w 90%% -h 90%% -T \" openusage \" %s\n", key, binary)
	}
	if key := strings.TrimSpace(opts.BindRefresh); key != "" {
		fmt.Fprintf(&b, "bind-key %s run-shell '%s tmux preview' \\; refresh-client -S\n", key, binary)
	}

	b.WriteString(sentinelEnd)
	b.WriteString("\n")
	return b.String()
}

// prependStatusRight returns a run-shell line that inserts the openusage
// segment at the inner (left) edge of status-right at config-load time,
// instead of appending it to the far-right edge. This places the usage
// info next to the center of the bar, ahead of the user's existing
// right-side segments (clock, battery, etc.).
//
// The line is idempotent: a guard skips the insert when the segment is
// already present, so repeated `tmux source-file` calls do not stack copies.
//
// We deliberately avoid writing a literal "#(" into the conf. tmux expands
// #(...) inside run-shell arguments at parse time, which would execute
// openusage immediately and freeze its output into the option. Instead the
// shell rebuilds the leading "#" at runtime via printf (so tmux never sees a
// command substitution to expand) and `tmux set` (no -F) stores the segment
// unexpanded, preserving both our segment and the user's existing #(...)
// segments for live rendering.
// segmentInners returns the inner `(openusage tmux …)` command(s) — WITHOUT the
// leading `#` — for the configured providers. With no providers it is a single
// auto-detecting segment; with providers it is one pinned segment each, so
// multiple tools show side by side, each with its own metrics.
func segmentInners(binary, preset string, providers []string) []string {
	if len(providers) == 0 {
		return []string{fmt.Sprintf("(%s tmux --preset %s)", binary, preset)}
	}
	return lo.Map(providers, func(p string, _ int) string {
		return fmt.Sprintf("(%s tmux --provider %s --preset %s)", binary, p, preset)
	})
}

func prependStatusRight(binary, preset string, providers []string) string {
	// Quote each inner command as a shell word for the for-loop. A " │ "
	// separator divides the openusage segment(s) from the user's existing
	// right-side segments (clock, battery, …) and from each other; it is a
	// plain box-drawing bar (no "#[" styling) so it inherits surrounding colors.
	items := strings.Join(lo.Map(segmentInners(binary, preset, providers), func(i string, _ int) string {
		return fmt.Sprintf("%q", i)
	}), " ")
	// We deliberately keep a literal "#(" out of the conf: tmux expands #(...)
	// inside run-shell arguments at parse time, which would run openusage
	// immediately and freeze its output. The shell rebuilds the leading "#" per
	// segment at runtime via printf, joins them with the separator, and
	// `tmux set` (no -F) stores everything unexpanded for live rendering. The
	// case guard makes re-sourcing idempotent.
	return fmt.Sprintf(
		`run-shell -b 'seg=""; for c in %s; do seg="${seg:+$seg │ }$(printf "#%%s" "$c")"; done; cur="$(tmux show -gqv status-right)"; case "$cur" in *"$seg"*) exit 0 ;; *) tmux set -g status-right "$seg │ $cur" ;; esac'`+"\n",
		items,
	)
}

// withInstallDefaults fills the zero/empty fields of opts with defaults so
// callers can pass partial structs. Returned opts is a copy; the caller's
// struct is not mutated.
func withInstallDefaults(opts InstallOptions) InstallOptions {
	if strings.TrimSpace(opts.Position) == "" {
		opts.Position = "right"
	}
	if strings.TrimSpace(opts.Preset) == "" {
		opts.Preset = DefaultPreset
	}
	if opts.Interval <= 0 {
		opts.Interval = 5
	}
	if opts.RightLength <= 0 {
		opts.RightLength = 200
	}
	if opts.LeftLength <= 0 {
		opts.LeftLength = 80
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return opts
}

// Install either prints the snippet to out (when opts.Write is false) or
// writes it into tmux.conf, with a `.bak` backup of any pre-existing
// non-empty file. If sentinels already exist in the conf the block is
// replaced in place; otherwise the snippet is appended after a blank line.
//
// Returns the path that was written (or would have been) and a non-nil error
// only on real I/O failures.
func Install(out io.Writer, opts InstallOptions) (string, error) {
	opts = withInstallDefaults(opts)
	snippet := BuildSnippet(opts)

	if !opts.Write {
		if _, err := io.WriteString(out, snippet); err != nil {
			return "", fmt.Errorf("tmux: writing snippet: %w", err)
		}
		if _, err := fmt.Fprintf(out, "\nTo apply, paste this into your tmux.conf and run: tmux source-file <path>\n"+
			"Or re-run with --write to install automatically.\n"); err != nil {
			return "", fmt.Errorf("tmux: writing instructions: %w", err)
		}
		return "", nil
	}

	path := strings.TrimSpace(opts.ConfPath)
	if path == "" {
		detected, err := DetectTmuxConf(out)
		if err != nil {
			return "", err
		}
		path = detected
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("tmux: reading %s: %w", path, err)
	}

	if len(existing) > 0 {
		backupPath := path + ".bak"
		if err := os.WriteFile(backupPath, existing, 0o600); err != nil {
			return "", fmt.Errorf("tmux: writing backup: %w", err)
		}
	}

	updated := replaceOrAppendSnippet(existing, snippet)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("tmux: creating conf dir: %w", err)
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return "", fmt.Errorf("tmux: writing %s: %w", path, err)
	}

	fmt.Fprintf(out, "installed tmux snippet at %s\n", path)
	fmt.Fprintf(out, "  reload with: tmux source-file %s\n", path)
	return path, nil
}

// Uninstall removes the sentinel block from tmux.conf, creating a `.bak`
// backup first. When the file has no sentinel block it is left untouched and
// an informational note is written to out.
func Uninstall(out io.Writer, confPath string) error {
	path := strings.TrimSpace(confPath)
	if path == "" {
		detected, err := DetectTmuxConf(out)
		if err != nil {
			return err
		}
		path = detected
	}

	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(out, "no tmux.conf found at %s; nothing to uninstall\n", path)
			return nil
		}
		return fmt.Errorf("tmux: reading %s: %w", path, err)
	}

	if !bytes.Contains(existing, []byte(sentinelStart)) {
		fmt.Fprintf(out, "no openusage block in %s; nothing to uninstall\n", path)
		return nil
	}

	backupPath := path + ".bak"
	if err := os.WriteFile(backupPath, existing, 0o600); err != nil {
		return fmt.Errorf("tmux: writing backup: %w", err)
	}

	cleaned := removeSentinelBlock(existing)
	if err := os.WriteFile(path, cleaned, 0o644); err != nil {
		return fmt.Errorf("tmux: writing %s: %w", path, err)
	}

	fmt.Fprintf(out, "removed openusage block from %s\n", path)
	return nil
}

// replaceOrAppendSnippet replaces/appends the openusage tmux.conf block. Thin
// wrapper over the shared managed-block helper (see managed.go).
func replaceOrAppendSnippet(existing []byte, snippet string) []byte {
	return replaceOrAppendBlock(existing, sentinelStart, sentinelEnd, snippet)
}

// removeSentinelBlock strips the openusage tmux.conf block. Thin wrapper over
// the shared managed-block helper (see managed.go).
func removeSentinelBlock(existing []byte) []byte {
	return removeBlock(existing, sentinelStart, sentinelEnd)
}

// SentinelPresent reports whether path contains the openusage-managed
// snippet. Used by the doctor command to verify install state.
func SentinelPresent(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("tmux: reading %s: %w", path, err)
	}
	return bytes.Contains(data, []byte(sentinelStart)), nil
}
