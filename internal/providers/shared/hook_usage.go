package shared

import "github.com/janekbaraniewski/openusage/internal/core"

// HookUsage holds token and cost data extracted from webhook/hook payloads.
// Used by claude_code, codex, and other telemetry providers.
// It embeds core.TokenUsage to avoid duplicating the canonical token fields.
type HookUsage struct {
	core.TokenUsage
}

// HasHookUsage returns true if the usage contains any non-zero token or cost data.
func HasHookUsage(u HookUsage) bool {
	return u.TokenUsage.HasTokenData()
}
