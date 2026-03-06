package core

// TokenUsage holds the canonical token and cost counters shared across
// telemetry events, hook payloads, and ingest requests. Centralizing
// these fields eliminates the triple-duplication that previously existed
// between shared.TelemetryEvent, telemetry.IngestRequest, and
// telemetry.CanonicalEvent.
type TokenUsage struct {
	InputTokens      *int64   `json:"input_tokens,omitempty"`
	OutputTokens     *int64   `json:"output_tokens,omitempty"`
	ReasoningTokens  *int64   `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  *int64   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int64   `json:"cache_write_tokens,omitempty"`
	TotalTokens      *int64   `json:"total_tokens,omitempty"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
	Requests         *int64   `json:"requests,omitempty"`
}

// SumTotalTokens computes TotalTokens from parts if it is nil.
func (u *TokenUsage) SumTotalTokens() {
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

// HasTokenData reports whether the usage contains any non-zero token or cost data.
func (u TokenUsage) HasTokenData() bool {
	for _, part := range []*int64{u.InputTokens, u.OutputTokens, u.ReasoningTokens, u.CacheReadTokens, u.CacheWriteTokens, u.TotalTokens} {
		if part != nil && *part > 0 {
			return true
		}
	}
	return u.CostUSD != nil && *u.CostUSD > 0
}

// Int64Ptr returns a pointer to the given int64 value.
func Int64Ptr(v int64) *int64 {
	return &v
}
