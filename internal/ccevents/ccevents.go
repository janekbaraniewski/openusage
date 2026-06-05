// Package ccevents bridges the Claude Code conversation log aggregator with
// the unified report.Event stream. It is shared by the headless statusline,
// the report subcommands, and the tmux integration so each call site does not
// re-duplicate the UsageStat-to-Event mapping.
package ccevents

import (
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/report"
)

// Conversations parses the local Claude Code conversation logs and returns
// them as a unified report.Event slice. offline=true forces the embedded
// pricing table, skipping any network lookup.
func Conversations(mode claude_code.CostMode, offline bool) ([]report.Event, error) {
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
