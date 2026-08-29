package tmux

import "github.com/janekbaraniewski/openusage/internal/active"

// DetectOptions, DetectResult and Detect are retained as aliases so existing
// tmux callers keep compiling. The implementation now lives in internal/active
// so other status-bar integrations can share it.
type DetectOptions = active.DetectOptions

type DetectResult = active.DetectResult

type LocalSourceProvider = active.LocalSourceProvider

// DefaultPriorityOrder mirrors active.DefaultPriorityOrder for callers that
// previously customized the tmux detector's fallback order.
var DefaultPriorityOrder = active.DefaultPriorityOrder

// Detect delegates to active.Detect.
func Detect(opts DetectOptions) DetectResult {
	if len(opts.PriorityOrder) == 0 {
		opts.PriorityOrder = DefaultPriorityOrder
	}
	return active.Detect(opts)
}
