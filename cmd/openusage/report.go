package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/export"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/janekbaraniewski/openusage/internal/report"
)

// telemetryCollectTimeout bounds a single provider's local-log collection.
// Generous because a one-shot CLI report may parse a large history.
const telemetryCollectTimeout = 30 * time.Second

// reportFlags holds the shared flag set behind the daily/weekly/monthly/
// session/blocks subcommands.
type reportFlags struct {
	json      bool
	since     string
	until     string
	breakdown bool
	provider  string
	project   string
	mode      string
	offline   bool
	source    string
	weekStart string
	topModels int
}

// newReportCommands returns the headless usage-report subcommands. They expose
// OpenUsage's parsing and pricing as scriptable tables/JSON for use in CI and
// other automation.
func newReportCommands() []*cobra.Command {
	specs := []struct {
		use   string
		kind  report.Kind
		short string
	}{
		{"daily", report.KindDaily, "Show usage and cost aggregated by day"},
		{"weekly", report.KindWeekly, "Show usage and cost aggregated by week"},
		{"monthly", report.KindMonthly, "Show usage and cost aggregated by month"},
		{"session", report.KindSession, "Show usage and cost grouped by Claude Code session"},
		{"blocks", report.KindBlocks, "Show usage by 5-hour billing block with burn rate and projection"},
	}

	cmds := make([]*cobra.Command, 0, len(specs))
	for _, spec := range specs {
		spec := spec
		f := &reportFlags{}
		cmd := &cobra.Command{
			Use:   spec.use,
			Short: spec.short,
			Long: reportLongHelp(spec.kind) + `

daily, weekly and monthly aggregate every configured provider (Claude Code
from its conversation logs at full fidelity; other providers from their daily
cost series). session and blocks read Claude Code conversation logs, which are
the only source with the per-message timestamps those views need.

Costs are API-equivalent estimates derived from token counts, not subscription
charges. Use --mode display to trust the cost recorded in the logs instead.`,
			Example: reportExamples(spec.use),
			RunE: func(_ *cobra.Command, _ []string) error {
				return runReport(spec.kind, f)
			},
		}
		bindReportFlags(cmd, f, spec.kind)
		cmds = append(cmds, cmd)
	}
	return cmds
}

func bindReportFlags(cmd *cobra.Command, f *reportFlags, kind report.Kind) {
	fl := cmd.Flags()
	fl.BoolVar(&f.json, "json", false, "emit JSON instead of a table")
	fl.StringVar(&f.since, "since", "", "only include usage on/after this date (YYYY-MM-DD)")
	fl.StringVar(&f.until, "until", "", "only include usage on/before this date (YYYY-MM-DD)")
	fl.BoolVarP(&f.breakdown, "breakdown", "b", false, "add a per-model breakdown under each row")
	fl.StringVar(&f.provider, "provider", "", "limit to a single provider id (e.g. claude_code)")
	fl.StringVar(&f.project, "project", "", "limit to a single project/workspace label")
	fl.StringVar(&f.mode, "mode", string(claude_code.CostModeCalculate),
		"cost mode: calculate (recompute from tokens), display (trust logged cost), or auto")
	fl.BoolVar(&f.offline, "offline", false, "skip network pricing lookups; use embedded rates")
	fl.IntVar(&f.topModels, "top-models", 0, "cap the number of models shown per breakdown row (0 = all)")
	if kind == report.KindDaily || kind == report.KindWeekly || kind == report.KindMonthly {
		fl.StringVar(&f.source, "source", string(export.SourceAuto),
			"snapshot source for non-Claude providers: auto, direct, or daemon")
	}
	if kind == report.KindWeekly {
		fl.StringVar(&f.weekStart, "week-start", "monday", "week boundary: monday or sunday")
	}
}

func runReport(kind report.Kind, f *reportFlags) error {
	opts := report.Options{
		Kind:            kind,
		Breakdown:       f.breakdown,
		Provider:        strings.TrimSpace(f.provider),
		Project:         strings.TrimSpace(f.project),
		WeekStartMonday: !strings.EqualFold(strings.TrimSpace(f.weekStart), "sunday"),
		TopModels:       f.topModels,
		Now:             time.Now(),
	}

	var err error
	if opts.Since, err = parseReportDate(f.since, false); err != nil {
		return fmt.Errorf("invalid --since: %w", err)
	}
	if opts.Until, err = parseReportDate(f.until, true); err != nil {
		return fmt.Errorf("invalid --until: %w", err)
	}

	sp := startSpinner(fmt.Sprintf("collecting %s usage…", kind))
	events, note, err := gatherReportEvents(kind, f)
	sp.stop()
	if err != nil {
		return err
	}

	rep := report.Build(events, opts)
	if note != "" {
		if rep.Note != "" {
			rep.Note = note + "; " + rep.Note
		} else {
			rep.Note = note
		}
	}

	if f.json {
		return rep.WriteJSON(os.Stdout)
	}
	return rep.WriteTable(os.Stdout)
}

// gatherReportEvents assembles the unified event stream for a report.
//
// Three itemized sources contribute per-turn events (timestamp + tokens + model
// + session), which is what session/blocks need:
//   - Claude Code, parsed at full fidelity (cost modes, dedup).
//   - Every other provider implementing the telemetry source interface
//     (codex, gemini_cli, copilot, cursor, ollama, opencode), via Collect().
//
// The periodic reports (daily/weekly/monthly) additionally fold in any
// remaining provider from its snapshot cost/token series. Providers already
// covered by an itemized source are excluded there to avoid double-counting.
func gatherReportEvents(kind report.Kind, f *reportFlags) ([]report.Event, string, error) {
	mode := claude_code.ParseCostMode(f.mode)
	provider := strings.TrimSpace(f.provider)
	cost := report.PricingCost(f.offline)

	var events []report.Event
	var notes []string
	covered := map[string]bool{} // providers handled by an itemized source

	// 1. Claude Code conversation logs.
	if provider == "" || provider == "claude_code" {
		cc, err := claudeCodeConversationEvents(mode, f.offline)
		if err != nil {
			notes = append(notes, fmt.Sprintf("claude_code logs unavailable: %v", err))
		}
		events = append(events, cc...)
		covered["claude_code"] = true
	}

	// 2. Other telemetry-source providers (per-turn local logs).
	for _, p := range providers.AllProviders() {
		src, ok := p.(shared.TelemetrySource)
		if !ok || p.ID() == "claude_code" {
			continue
		}
		if provider != "" && p.ID() != provider {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), telemetryCollectTimeout)
		evs, err := src.Collect(ctx, src.DefaultCollectOptions())
		cancel()
		covered[p.ID()] = true
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s telemetry unavailable: %v", p.ID(), err))
			continue
		}
		events = append(events, report.FromTelemetry(evs, p.ID(), cost)...)
	}

	// 3. File-based providers exposing itemized usage (no telemetry source).
	for _, p := range providers.AllProviders() {
		if covered[p.ID()] {
			continue
		}
		src, ok := p.(core.ItemizedUsageProvider)
		if !ok {
			continue
		}
		if provider != "" && p.ID() != provider {
			continue
		}
		evs, err := src.ItemizedUsage()
		covered[p.ID()] = true
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s usage unavailable: %v", p.ID(), err))
			continue
		}
		events = append(events, report.FromItemized(evs, cost)...)
	}

	// 4. Snapshot fallback for the remaining providers (periodic reports only).
	periodic := kind == report.KindDaily || kind == report.KindWeekly || kind == report.KindMonthly
	if periodic {
		ctx := context.Background()
		snaps, _, err := export.Collect(ctx, export.Source(strings.ToLower(strings.TrimSpace(f.source))))
		if err != nil {
			notes = append(notes, fmt.Sprintf("provider snapshots unavailable: %v", err))
		} else {
			others := snaps[:0]
			for _, s := range snaps {
				if covered[s.ProviderID] {
					continue
				}
				if provider != "" && s.ProviderID != provider {
					continue
				}
				others = append(others, s)
			}
			events = append(events, report.FromSnapshots(others)...)
		}
	}

	return events, strings.Join(notes, "; "), nil
}

// claudeCodeConversationEvents maps Claude Code's per-turn usage stats into the
// report event stream. Shared by the report subcommands and the statusline.
func claudeCodeConversationEvents(mode claude_code.CostMode, offline bool) ([]report.Event, error) {
	stats, err := claude_code.AggregateConversations(claude_code.AggregateOptions{
		Mode:    mode,
		Offline: offline,
	})
	if err != nil {
		return nil, err
	}
	out := make([]report.Event, 0, len(stats))
	for _, s := range stats {
		out = append(out, report.Event{
			Time:        s.Time,
			Provider:    "claude_code",
			Model:       s.Model,
			Project:     s.Project,
			Session:     s.Session,
			Input:       s.Input,
			Output:      s.Output,
			CacheRead:   s.CacheRead,
			CacheCreate: s.CacheCreate,
			Reasoning:   s.Reasoning,
			Cost:        s.Cost,
		})
	}
	return out, nil
}

// parseReportDate parses a YYYY-MM-DD bound in the local timezone. For the
// upper bound it returns the last instant of the day so the range is inclusive.
func parseReportDate(s string, endOfDay bool) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Nanosecond)
	}
	return t, nil
}

func reportLongHelp(kind report.Kind) string {
	switch kind {
	case report.KindBlocks:
		return "Group usage into Claude Code's 5-hour billing windows. The active block shows a burn rate ($/hour) and a projected end-of-block cost."
	case report.KindSession:
		return "Group usage by Claude Code session (one conversation each)."
	default:
		return fmt.Sprintf("Aggregate token usage and cost by %s.", strings.TrimSuffix(string(kind), "ly"))
	}
}

func reportExamples(use string) string {
	return strings.Join([]string{
		"  openusage " + use,
		"  openusage " + use + " --json",
		"  openusage " + use + " --breakdown",
		"  openusage " + use + " --since 2026-05-01 --until 2026-05-31",
		"  openusage " + use + " --provider claude_code --offline",
	}, "\n")
}
