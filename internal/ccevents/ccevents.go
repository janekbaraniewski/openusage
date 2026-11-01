// Package ccevents bridges the Claude Code conversation log aggregator with
// the unified report.Event stream. It is shared by the headless statusline,
// the report subcommands, and the tmux integration so each call site does not
// re-duplicate the UsageStat-to-Event mapping.
package ccevents

import (
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/report"
)

// Conversations parses the local Claude Code conversation logs and returns
// them as a unified report.Event slice. offline=true forces the embedded
// pricing table, skipping any network lookup.
func Conversations(mode claude_code.CostMode, offline bool) ([]report.Event, error) {
	return aggregate(claude_code.AggregateOptions{Mode: mode, Offline: offline})
}

// BlockLookback is how far back the active 5h billing block needs history.
// A block's start depends on the events before it (a new block opens 5h after
// the previous block's start or after a 5h idle gap), so two block lengths
// recover the previous block's phase in all but round-the-clock usage.
const BlockLookback = 10 * time.Hour

// BlockSince is the earliest log mtime needed to compute the active block.
func BlockSince(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	return now.Add(-BlockLookback)
}

// TodaySince is the earliest log mtime needed for "today" totals and the
// active block: whichever of local midnight and BlockSince is earlier. Every
// event since midnight lives in a file modified since midnight, so today's
// totals are exact.
func TodaySince(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if block := BlockSince(now); block.Before(midnight) {
		return block
	}
	return midnight
}

// Recent is Conversations restricted to log files modified at or after
// since, plus any file whose path contains sessionID (the active session's
// transcript and subagent logs). It is meant for the per-render callers
// (statusline, tmux), which previously re-parsed the entire history on every
// render. Pick since with TodaySince or BlockSince to match what is shown.
func Recent(mode claude_code.CostMode, offline bool, since time.Time, sessionID string) ([]report.Event, error) {
	opts := claude_code.AggregateOptions{
		Mode:          mode,
		Offline:       offline,
		ModifiedSince: since,
	}
	if sid := strings.TrimSpace(sessionID); sid != "" {
		opts.KeepPath = func(p string) bool { return strings.Contains(p, sid) }
	}
	return aggregate(opts)
}

func aggregate(opts claude_code.AggregateOptions) ([]report.Event, error) {
	stats, err := claude_code.AggregateConversations(opts)
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
