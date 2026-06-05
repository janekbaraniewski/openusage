package tmux

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/export"
)

// AlertMode controls what tmux command(s) fire when a threshold trips.
type AlertMode string

const (
	AlertModeMessage AlertMode = "message"
	AlertModeBell    AlertMode = "bell"
	AlertModeBoth    AlertMode = "both"
	AlertModeNone    AlertMode = "none"
)

// ParseAlertMode normalises a user-supplied mode string. Unknown/empty falls
// back to AlertModeMessage so the watcher fails open with the most-useful
// default.
func ParseAlertMode(s string) AlertMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "bell":
		return AlertModeBell
	case "both":
		return AlertModeBoth
	case "none":
		return AlertModeNone
	default:
		return AlertModeMessage
	}
}

// tmuxRunner is the indirection used by Watch so tests can capture the
// commands that would have been dispatched without spawning real tmux.
type tmuxRunner func(args ...string) error

// WatchOptions configures Watch. Most fields are optional; the zero value
// uses sensible defaults but a context is required so the caller can
// cancel.
type WatchOptions struct {
	// Interval is the poll period. Zero means 5s.
	Interval time.Duration
	// Cooldown suppresses repeat alerts within this window. Zero falls
	// back to config.Alerts.CooldownMinutes (or 15 minutes when unset).
	Cooldown time.Duration
	// Alerts carries the threshold configuration. Zero means "use defaults
	// from the loaded config".
	Alerts config.TmuxAlerts
	// Source controls how the snapshot is acquired. Defaults to "auto".
	Source export.Source
	// Mode overrides Alerts.Mode (CLI flag wins over file config).
	Mode AlertMode
	// Now lets tests inject a clock; the live watcher uses time.Now.
	Now func() time.Time
	// Runner is injected by tests; nil means run real tmux.
	Runner tmuxRunner
	// Out receives log lines (one per alert fire and lifecycle events).
	Out io.Writer
	// PIDFile is the path used by --background to coordinate single
	// ownership. Empty means default `~/.cache/openusage/tmux-watch.pid`.
	PIDFile string
}

// Watch runs the burn-rate / block-expiry alert loop. It blocks until ctx is
// cancelled or the runner returns a fatal error. Per the design doc the loop
// must exit cleanly when `$TMUX` is unset so users cannot spawn a watcher
// that has no terminal to alert into.
func Watch(ctx context.Context, opts WatchOptions) error {
	if !insideTmux() {
		return fmt.Errorf("tmux: $TMUX not set; run inside a tmux session")
	}
	if opts.Out == nil {
		opts.Out = os.Stderr
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Runner == nil {
		opts.Runner = realTmuxRunner
	}
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Second
	}
	if opts.Cooldown <= 0 {
		mins := opts.Alerts.CooldownMinutes
		if mins <= 0 {
			mins = 15
		}
		opts.Cooldown = time.Duration(mins) * time.Minute
	}
	mode := opts.Mode
	if mode == "" {
		mode = ParseAlertMode(opts.Alerts.Mode)
	}

	state := alertState{}
	fmt.Fprintf(opts.Out, "tmux watch: polling every %s (mode=%s, cooldown=%s)\n",
		opts.Interval, mode, opts.Cooldown)

	tick := time.NewTicker(opts.Interval)
	defer tick.Stop()

	for {
		evaluate(ctx, opts, mode, &state)
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// alertState tracks last-fire times keyed by alert kind so the cooldown can
// suppress duplicate notifications without persisting state to disk.
type alertState struct {
	lastBurnFire  time.Time
	lastBlockFire time.Time
}

// evaluate takes one poll snapshot and fires alerts when thresholds are
// crossed. Returns silently on transient errors so the watcher keeps running
// over flaky daemons.
func evaluate(ctx context.Context, opts WatchOptions, mode AlertMode, state *alertState) {
	now := opts.Now()
	src := opts.Source
	if src == "" {
		src = export.SourceAuto
	}
	bctx, err := BuildContext(ctx, BuildOptions{Source: src, Now: now, OfflineClaudePricing: true})
	if err != nil {
		return
	}

	burnLimit := opts.Alerts.BurnRatePerHour
	if burnLimit > 0 && bctx.HaveBlock && bctx.Block.BurnRateUSDPerHour >= burnLimit {
		if now.Sub(state.lastBurnFire) >= opts.Cooldown {
			fire(opts, mode, fmt.Sprintf("burn rate %.2f USD/hr exceeds threshold %.2f",
				bctx.Block.BurnRateUSDPerHour, burnLimit))
			state.lastBurnFire = now
		}
	}

	blockMins := opts.Alerts.BlockMinutesRemaining
	if blockMins > 0 && bctx.HaveBlock {
		remaining := bctx.Block.TimeRemaining
		if remaining > 0 && remaining <= time.Duration(blockMins)*time.Minute {
			if now.Sub(state.lastBlockFire) >= opts.Cooldown {
				fire(opts, mode, fmt.Sprintf("active block ends in %s",
					formatMinutes(remaining)))
				state.lastBlockFire = now
			}
		}
	}
}

// fire dispatches the alert via the configured mode. It always refreshes the
// status bar so users see the new value immediately. Errors from tmux are
// surfaced to the log but never abort the watcher.
func fire(opts WatchOptions, mode AlertMode, msg string) {
	switch mode {
	case AlertModeNone:
		fmt.Fprintf(opts.Out, "tmux watch: alert suppressed (mode=none): %s\n", msg)
		return
	case AlertModeBell:
		if err := opts.Runner("display-message", "-d", "0", msg); err != nil {
			fmt.Fprintf(opts.Out, "tmux watch: display-message error: %v\n", err)
		}
		if err := opts.Runner("set-option", "-g", "visual-bell", "on"); err != nil {
			fmt.Fprintf(opts.Out, "tmux watch: visual-bell error: %v\n", err)
		}
	case AlertModeBoth:
		if err := opts.Runner("display-message", msg); err != nil {
			fmt.Fprintf(opts.Out, "tmux watch: display-message error: %v\n", err)
		}
		if err := opts.Runner("set-option", "-g", "visual-bell", "on"); err != nil {
			fmt.Fprintf(opts.Out, "tmux watch: visual-bell error: %v\n", err)
		}
	default: // message
		if err := opts.Runner("display-message", msg); err != nil {
			fmt.Fprintf(opts.Out, "tmux watch: display-message error: %v\n", err)
		}
	}
	if err := opts.Runner("refresh-client", "-S"); err != nil {
		fmt.Fprintf(opts.Out, "tmux watch: refresh-client error: %v\n", err)
	}
	fmt.Fprintf(opts.Out, "tmux watch: fired %s alert: %s\n", mode, msg)
}

// insideTmux reports whether the caller is running in a tmux session. We
// only fire status updates against an active client; spawning the watcher
// outside tmux silently does nothing useful and risks confusing users.
func insideTmux() bool {
	return strings.TrimSpace(os.Getenv("TMUX")) != ""
}

// realTmuxRunner is the production tmuxRunner: it invokes the tmux binary
// with the given args, returning any non-nil error from the process.
func realTmuxRunner(args ...string) error {
	cmd := exec.Command("tmux", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux: invoking tmux %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func formatMinutes(d time.Duration) string {
	mins := int(d.Minutes())
	if mins <= 0 {
		return "<1m"
	}
	return strconv.Itoa(mins) + "m"
}

// DefaultPIDFile returns the path used by --background to coordinate single
// ownership. Exposed for the cobra wiring and the doctor command.
func DefaultPIDFile() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "openusage", "tmux-watch.pid")
}

// WritePIDFile records the current pid to path, replacing whatever was
// there before. Returns the previous pid (if any) so the caller can kill
// the old process.
func WritePIDFile(path string) (int, error) {
	if path == "" {
		return 0, fmt.Errorf("tmux: empty pid file path")
	}
	var prev int
	if data, err := os.ReadFile(path); err == nil {
		prev, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, fmt.Errorf("tmux: creating pid dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return 0, fmt.Errorf("tmux: writing pid file: %w", err)
	}
	return prev, nil
}
