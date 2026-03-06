package shared

// HookUsage holds token and cost data extracted from webhook/hook payloads.
// Used by claude_code, codex, and other telemetry providers.
type HookUsage struct {
	InputTokens      *int64
	OutputTokens     *int64
	ReasoningTokens  *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
	TotalTokens      *int64
	CostUSD          *float64
}

// HasHookUsage returns true if the usage contains any non-zero token or cost data.
func HasHookUsage(u HookUsage) bool {
	for _, tokenPart := range []*int64{u.InputTokens, u.OutputTokens, u.ReasoningTokens, u.CacheReadTokens, u.CacheWriteTokens, u.TotalTokens} {
		if tokenPart != nil && *tokenPart > 0 {
			return true
		}
	}
	return u.CostUSD != nil && *u.CostUSD > 0
}

// SumTotalTokens computes TotalTokens from parts if it's nil.
func (u *HookUsage) SumTotalTokens() {
	if u.TotalTokens != nil {
		return
	}
	var total int64
	hasAny := false
	for _, part := range []*int64{u.InputTokens, u.OutputTokens, u.ReasoningTokens, u.CacheReadTokens, u.CacheWriteTokens} {
		if part != nil {
			total += *part
			hasAny = true
		}
	}
	if hasAny {
		u.TotalTokens = Int64Ptr(total)
	}
}
