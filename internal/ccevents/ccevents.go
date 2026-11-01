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

// RecentLookback bounds how far back Recent reads. It covers "today" (local
// midnight is always within 24h) and the active 5h billing block plus the
// chain of blocks before it in all but round-the-clock usage.
const RecentLookback = 24 * time.Hour

// Recent is Conversations restricted to log files modified within
// RecentLookback of now, plus any file whose path contains sessionID (the
// active session's transcript and subagent logs). It is meant for the
// per-render callers (statusline, tmux) that only show today, the active
// block, the current session and its context window, and which previously
// re-parsed the entire history on every render.
func Recent(mode claude_code.CostMode, offline bool, now time.Time, sessionID string) ([]report.Event, error) {
	if now.IsZero() {
		now = time.Now()
	}
	opts := claude_code.AggregateOptions{
		Mode:          mode,
		Offline:       offline,
		ModifiedSince: now.Add(-RecentLookback),
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
