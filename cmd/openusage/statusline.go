package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/export"
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/report"
)

// statuslineInput is the JSON Claude Code pipes to a statusLine command on
// stdin. Only the fields we use are declared; unknown fields are ignored.
type statuslineInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Model          struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
		ProjectDir string `json:"project_dir"`
	} `json:"workspace"`
	Cost struct {
		TotalCostUSD float64 `json:"total_cost_usd"`
	} `json:"cost"`
}

type statuslineOptions struct {
	offline       bool
	mode          string
	color         bool
	contextMedium float64
	contextHigh   float64
	segments      []string // enabled segment keys; empty means all
}

// statuslineSegmentDefs are the toggleable pieces of the status line, in render
// order. Keep this list and the assembly in assembleStatusline in sync.
var statuslineSegmentDefs = []struct{ key, label string }{
	{"model", "Model name"},
	{"session", "Session cost"},
	{"today", "Today's cost"},
	{"block", "5h block cost + time left"},
	{"burn", "Burn rate"},
	{"window5h", "5h usage window %"},
	{"context", "Context window %"},
}

func allStatuslineSegmentKeys() []string {
	keys := make([]string, len(statuslineSegmentDefs))
	for i, s := range statuslineSegmentDefs {
		keys[i] = s.key
	}
	return keys
}

// segmentEnabled reports whether a segment renders. An empty selection means
// "all segments" so an unconfigured statusline shows everything.
func (o statuslineOptions) segmentEnabled(key string) bool {
	if len(o.segments) == 0 {
		return true
	}
	for _, s := range o.segments {
		if s == key {
			return true
		}
	}
	return false
}

// settingsSnippet is shown in --help so users can wire the statusline into
// Claude Code.
const settingsSnippet = `To wire it in automatically (creates a .bak backup, preserves other keys):

  openusage statusline install

Or add this to ~/.claude/settings.json by hand:

  {
    "statusLine": {
      "type": "command",
      "command": "openusage statusline",
      "padding": 0
    }
  }`

func defaultStatuslineOptions() statuslineOptions {
	return statuslineOptions{
		offline:       true,
		mode:          string(claude_code.CostModeCalculate),
		color:         true,
		contextMedium: 50,
		contextHigh:   80,
	}
}

func newStatuslineCommand() *cobra.Command {
	opts := defaultStatuslineOptions()

	cmd := &cobra.Command{
		Use:   "statusline",
		Short: "Emit a one-line Claude Code status bar (session/today/block cost, burn rate, context)",
		Long: `Render a single status line for the Claude Code status bar.

Claude Code pipes the active session JSON to this command on stdin; the output
is one line summarizing the current model, session/today/active-block cost, the
burn rate, and context-window usage. Costs are API-equivalent estimates derived
from the local conversation logs, not subscription charges.

It runs offline by default (embedded pricing) so it responds instantly; pass
--offline=false to fetch live pricing.

` + settingsSnippet,
		Example: strings.Join([]string{
			`  echo '{"session_id":"abc","model":{"display_name":"Opus 4.8"}}' | openusage statusline`,
			"  openusage statusline install",
			"  openusage statusline --offline=false",
		}, "\n"),
		RunE: func(_ *cobra.Command, _ []string) error {
			return runStatusline(opts, os.Stdin, os.Stdout)
		},
	}

	fl := cmd.Flags()
	fl.BoolVar(&opts.offline, "offline", opts.offline, "use embedded pricing and skip network lookups")
	fl.StringVar(&opts.mode, "mode", opts.mode, "cost mode: calculate, display, or auto")
	fl.BoolVar(&opts.color, "color", opts.color, "colorize the output with ANSI escapes")
	fl.Float64Var(&opts.contextMedium, "context-medium", opts.contextMedium, "context %% threshold for the yellow warning color")
	fl.Float64Var(&opts.contextHigh, "context-high", opts.contextHigh, "context %% threshold for the red warning color")
	fl.StringSliceVar(&opts.segments, "segments", nil, "comma-separated segments to show: "+strings.Join(allStatuslineSegmentKeys(), ",")+" (default all)")

	cmd.AddCommand(newStatuslineInstallCommand(), newStatuslineUninstallCommand())
	return cmd
}

func newStatuslineInstallCommand() *cobra.Command {
	opts := defaultStatuslineOptions()
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Wire the statusline into ~/.claude/settings.json (interactive on a TTY)",
		Long: `Set up the Claude Code statusline.

On an interactive terminal this opens a one-screen, live-preview configurator:
toggle which segments show, flip color and pricing, then apply. Passing any
flag (or a non-TTY) keeps it scriptable and bakes the choices into the installed
command. A .bak backup of settings.json is created.`,
		Example: strings.Join([]string{
			"  openusage statusline install",
			"  openusage statusline install --segments today,context",
			"  openusage statusline install --color=false --offline=false",
		}, "\n"),
		RunE: func(c *cobra.Command, _ []string) error {
			customized := c.Flags().Changed("segments") || c.Flags().Changed("mode") ||
				c.Flags().Changed("color") || c.Flags().Changed("offline") ||
				c.Flags().Changed("context-medium") || c.Flags().Changed("context-high")
			if !customized && isStdinTerminal() && isStdoutTerminal() {
				return installStatuslineInteractive(os.Stdout)
			}
			return installStatusline(os.Stdout, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringSliceVar(&opts.segments, "segments", nil, "comma-separated segments to install: "+strings.Join(allStatuslineSegmentKeys(), ",")+" (default all)")
	fl.StringVar(&opts.mode, "mode", opts.mode, "cost mode: calculate, display, or auto")
	fl.BoolVar(&opts.color, "color", opts.color, "colorize the output with ANSI escapes")
	fl.BoolVar(&opts.offline, "offline", opts.offline, "use embedded pricing and skip network lookups")
	fl.Float64Var(&opts.contextMedium, "context-medium", opts.contextMedium, "context %% threshold for the yellow warning color")
	fl.Float64Var(&opts.contextHigh, "context-high", opts.contextHigh, "context %% threshold for the red warning color")
	return cmd
}

func newStatuslineUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the OpenUsage statusline from ~/.claude/settings.json",
		RunE: func(_ *cobra.Command, _ []string) error {
			return uninstallStatusline(os.Stdout)
		},
	}
}

// claudeSettingsPath resolves the Claude Code settings.json, honoring the
// CLAUDE_SETTINGS_FILE override used elsewhere in the codebase.
func claudeSettingsPath() string {
	if f := strings.TrimSpace(os.Getenv("CLAUDE_SETTINGS_FILE")); f != "" {
		return f
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// statuslineCommandString returns the command Claude Code should invoke: the
// resolved openusage binary, the statusline subcommand, and any non-default
// options baked in as flags so the customization survives in settings.json.
func statuslineCommandString(opts statuslineOptions) string {
	bin, err := os.Executable()
	if err != nil || strings.TrimSpace(bin) == "" {
		bin = "openusage"
	}
	cmd := bin + " statusline"
	// Only persist a subset; an empty/full selection is the implicit default.
	if n := len(opts.segments); n > 0 && n < len(statuslineSegmentDefs) {
		cmd += " --segments " + strings.Join(opts.segments, ",")
	}
	if m := strings.TrimSpace(opts.mode); m != "" && m != string(claude_code.CostModeCalculate) {
		cmd += " --mode " + m
	}
	if !opts.color {
		cmd += " --color=false"
	}
	if !opts.offline {
		cmd += " --offline=false"
	}
	if opts.contextMedium != 0 && opts.contextMedium != 50 {
		cmd += fmt.Sprintf(" --context-medium %g", opts.contextMedium)
	}
	if opts.contextHigh != 0 && opts.contextHigh != 80 {
		cmd += fmt.Sprintf(" --context-high %g", opts.contextHigh)
	}
	return cmd
}

// installStatusline merges the statusLine block into settings.json, preserving
// every other key and backing up the original file first.
func installStatusline(out io.Writer, opts statuslineOptions) error {
	path := claudeSettingsPath()
	cfg, err := readJSONObject(path)
	if err != nil {
		return err
	}
	command := statuslineCommandString(opts)
	cfg["statusLine"] = map[string]any{
		"type":    "command",
		"command": command,
		"padding": 0,
	}
	if err := writeJSONObjectWithBackup(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "installed statusline into %s\n", path)
	fmt.Fprintf(out, "  command: %s\n", command)
	fmt.Fprintln(out, "Restart Claude Code (or open a new session) to see it.")
	return nil
}

// installStatuslineInteractive runs the one-screen configurator and installs
// the chosen statusline. Cancelling leaves settings.json untouched.
func installStatuslineInteractive(out io.Writer) error {
	ch, err := runStatuslineConfigurator()
	if err != nil {
		return err
	}
	if ch.cancelled {
		fmt.Fprintln(out, "statusline: install cancelled.")
		return nil
	}
	return installStatusline(out, ch.options())
}

// uninstallStatusline removes our statusLine block when it points at openusage.
func uninstallStatusline(out io.Writer) error {
	path := claudeSettingsPath()
	cfg, err := readJSONObject(path)
	if err != nil {
		return err
	}
	if sl, ok := cfg["statusLine"].(map[string]any); ok {
		if cmd, _ := sl["command"].(string); strings.Contains(cmd, "statusline") && strings.Contains(cmd, "openusage") {
			delete(cfg, "statusLine")
			if err := writeJSONObjectWithBackup(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(out, "removed statusline from %s\n", path)
			return nil
		}
		fmt.Fprintf(out, "statusLine in %s is not managed by openusage; left unchanged\n", path)
		return nil
	}
	fmt.Fprintf(out, "no statusLine configured in %s\n", path)
	return nil
}

func readJSONObject(path string) (map[string]any, error) {
	cfg := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

func writeJSONObjectWithBackup(path string, cfg map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		if err := os.WriteFile(path+".bak", existing, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize settings: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func runStatusline(opts statuslineOptions, stdin io.Reader, stdout io.Writer) error {
	in := readStatuslineInput(stdin)

	// The 5h usage-window % is rate-limit data the daemon already collects; read
	// it from the daemon over the local socket (no network) only when the segment
	// is shown, and omit it silently if the daemon isn't running.
	fiveHourPct, haveFiveHour := 0.0, false
	if opts.segmentEnabled("window5h") {
		fiveHourPct, haveFiveHour = fetchFiveHourPctFromDaemon()
	}

	events, err := claudeCodeConversationEvents(claude_code.ParseCostMode(opts.mode), opts.offline)
	if err != nil {
		// Without logs we can still echo the model and Claude Code's own cost.
		fmt.Fprintln(stdout, renderStatusline(in, nil, fiveHourPct, haveFiveHour, time.Now(), opts))
		return nil
	}
	fmt.Fprintln(stdout, renderStatusline(in, events, fiveHourPct, haveFiveHour, time.Now(), opts))
	return nil
}

// fetchFiveHourPctFromDaemon reads the claude_code 5h usage-window utilization
// from the running daemon. It is best-effort: any error (no daemon, timeout,
// missing metric) returns ok=false so the segment is simply omitted. Never makes
// a network call — SourceDaemon only talks to the local socket.
func fetchFiveHourPctFromDaemon() (float64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	snaps, _, err := export.Collect(ctx, export.SourceDaemon)
	if err != nil {
		return 0, false
	}
	for _, s := range snaps {
		if s.ProviderID != "claude_code" {
			continue
		}
		if m, ok := s.Metrics["usage_five_hour"]; ok && m.Used != nil {
			return *m.Used, true
		}
	}
	return 0, false
}

// readStatuslineInput reads and decodes the stdin payload. A terminal (no pipe)
// or malformed JSON yields a zero-value input so the command still renders.
func readStatuslineInput(stdin io.Reader) statuslineInput {
	var in statuslineInput
	if f, ok := stdin.(*os.File); ok {
		if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			return in // interactive terminal: nothing piped in
		}
	}
	data, err := io.ReadAll(stdin)
	if err != nil || len(data) == 0 {
		return in
	}
	_ = json.Unmarshal(data, &in)
	return in
}

// renderStatusline builds the status line. It is pure (no I/O) so it can be
// unit-tested with synthetic events.
func renderStatusline(in statuslineInput, events []report.Event, fiveHourPct float64, haveFiveHour bool, now time.Time, opts statuslineOptions) string {
	model := strings.TrimSpace(in.Model.DisplayName)
	if model == "" {
		model = shortModelID(in.Model.ID)
	}

	var (
		sessionCost  float64
		todayCost    float64
		contextTok   int
		haveSession  bool
		midnight     = core.LocalMidnight()
		lastSessTime time.Time
	)
	for _, e := range events {
		if !e.Time.Before(midnight) {
			todayCost += e.Cost
		}
		if in.SessionID != "" && e.Session == in.SessionID {
			sessionCost += e.Cost
			haveSession = true
			if !e.Time.Before(lastSessTime) {
				lastSessTime = e.Time
				contextTok = e.Input + e.CacheRead + e.CacheCreate
				if model == "" {
					model = shortModelID(e.Model)
				}
			}
		}
	}
	// Fall back to Claude Code's own session cost when we have no matching logs.
	if !haveSession && in.Cost.TotalCostUSD > 0 {
		sessionCost = in.Cost.TotalCostUSD
	}
	if model == "" {
		model = "claude"
	}

	// Active billing block.
	var (
		blockCost float64
		blockLeft time.Duration
		burn      float64
		haveBlock bool
	)
	if len(events) > 0 {
		rep := report.Build(events, report.Options{Kind: report.KindBlocks, Now: now})
		if active, ok := rep.ActiveBlock(); ok {
			blockCost = active.Cost
			blockLeft = active.TimeRemaining
			burn = active.BurnRateUSDPerHour
			haveBlock = true
		}
	}

	ctxWindow := contextWindowFor(in.Model.ID)
	// If the observed context already exceeds the guessed window, the session
	// is on the 1M-token tier; correct the denominator so the percentage stays
	// meaningful offline (where we can't consult model metadata).
	if contextTok > ctxWindow {
		ctxWindow = 1_000_000
	}
	ctxPct := 0.0
	if ctxWindow > 0 && contextTok > 0 {
		ctxPct = float64(contextTok) / float64(ctxWindow) * 100
		if ctxPct > 100 {
			ctxPct = 100
		}
	}

	return assembleStatusline(statuslineValues{
		model:        model,
		sessionCost:  sessionCost,
		todayCost:    todayCost,
		blockCost:    blockCost,
		blockLeft:    blockLeft,
		burn:         burn,
		haveBlock:    haveBlock,
		fiveHourPct:  fiveHourPct,
		haveFiveHour: haveFiveHour,
		contextTok:   contextTok,
		ctxPct:       ctxPct,
	}, opts)
}

// statuslineValues holds the resolved numbers a status line renders, separate
// from assembly so both the live renderer and the configurator preview can use
// the same segment layout.
type statuslineValues struct {
	model        string
	sessionCost  float64
	todayCost    float64
	blockCost    float64
	blockLeft    time.Duration
	burn         float64
	haveBlock    bool
	fiveHourPct  float64
	haveFiveHour bool
	contextTok   int
	ctxPct       float64
}

// assembleStatusline joins the enabled segments in canonical order. The three
// cost figures share one 💰 group joined by " / "; everything else is
// pipe-separated.
func assembleStatusline(v statuslineValues, opts statuslineOptions) string {
	var parts []string
	if opts.segmentEnabled("model") && v.model != "" {
		parts = append(parts, "🤖 "+v.model)
	}
	var costs []string
	if opts.segmentEnabled("session") {
		costs = append(costs, fmt.Sprintf("$%.2f sess", v.sessionCost))
	}
	if opts.segmentEnabled("today") {
		costs = append(costs, fmt.Sprintf("$%.2f today", v.todayCost))
	}
	if opts.segmentEnabled("block") && v.haveBlock {
		costs = append(costs, fmt.Sprintf("$%.2f block (%s left)", v.blockCost, fmtStatusDuration(v.blockLeft)))
	}
	if len(costs) > 0 {
		parts = append(parts, "💰 "+strings.Join(costs, " / "))
	}
	if opts.segmentEnabled("burn") && v.haveBlock && v.burn > 0 {
		parts = append(parts, "🔥 "+colorize(fmt.Sprintf("$%.2f", v.burn), ansiOrange, opts.color)+"/hr")
	}
	if opts.segmentEnabled("window5h") && v.haveFiveHour {
		parts = append(parts, colorize(fmt.Sprintf("🕔 5h %.0f%%", v.fiveHourPct), contextColor(v.fiveHourPct, opts), opts.color))
	}
	if opts.segmentEnabled("context") && v.contextTok > 0 {
		ctxStr := "🧠 " + fmtTokensShort(v.contextTok)
		if v.ctxPct > 0 {
			ctxStr += fmt.Sprintf(" (%.0f%%)", v.ctxPct)
		}
		parts = append(parts, colorize(ctxStr, contextColor(v.ctxPct, opts), opts.color))
	}
	return strings.Join(parts, " | ")
}

// --- helpers ---

const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiOrange = "\033[38;5;208m" // approx brand orange
)

func colorize(s, code string, enabled bool) string {
	if !enabled || code == "" {
		return s
	}
	return code + s + ansiReset
}

func contextColor(pct float64, opts statuslineOptions) string {
	switch {
	case pct >= opts.contextHigh:
		return ansiRed
	case pct >= opts.contextMedium:
		return ansiYellow
	default:
		return ansiGreen
	}
}

func contextWindowFor(modelID string) int {
	id := strings.ToLower(modelID)
	if strings.Contains(id, "1m") {
		return 1_000_000
	}
	// Claude models are 200k by default.
	return 200_000
}

func shortModelID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if i := strings.LastIndex(id, "/"); i >= 0 && i < len(id)-1 {
		id = id[i+1:]
	}
	return id
}

func fmtStatusDuration(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func fmtTokensShort(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
