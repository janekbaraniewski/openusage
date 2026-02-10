package core

import "time"

// Status represents the overall health/status of a provider's quota.
type Status string

const (
	StatusOK          Status = "OK"
	StatusNearLimit   Status = "NEAR_LIMIT"
	StatusLimited     Status = "LIMITED"
	StatusAuth        Status = "AUTH_REQUIRED"
	StatusUnsupported Status = "UNSUPPORTED"
	StatusError       Status = "ERROR"
	StatusUnknown     Status = "UNKNOWN"
)

// Metric holds a single quota dimension (e.g., requests-per-minute, tokens-per-minute).
type Metric struct {
	Limit     *float64 `json:"limit,omitempty"`
	Remaining *float64 `json:"remaining,omitempty"`
	Used      *float64 `json:"used,omitempty"`
	Unit      string   `json:"unit"`   // "requests", "tokens", "USD", "credits"
	Window    string   `json:"window"` // "1m", "1d", "month", "rolling-5h", etc.
}

// Percent returns the remaining percentage (0-100). Returns -1 if not computable.
func (m Metric) Percent() float64 {
	if m.Limit != nil && m.Remaining != nil && *m.Limit > 0 {
		return (*m.Remaining / *m.Limit) * 100
	}
	if m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		return ((*m.Limit - *m.Used) / *m.Limit) * 100
	}
	return -1
}

// QuotaSnapshot is the canonical result returned by every provider adapter.
type QuotaSnapshot struct {
	ProviderID string               `json:"provider_id"`
	AccountID  string               `json:"account_id"`
	Timestamp  time.Time            `json:"timestamp"`
	Status     Status               `json:"status"`
	Metrics    map[string]Metric    `json:"metrics"`           // keys like "rpm", "tpm", "rpd"
	Resets     map[string]time.Time `json:"resets,omitempty"`  // e.g. "rpm_reset"
	Raw        map[string]string    `json:"raw,omitempty"`     // redacted header dump / CLI lines
	Message    string               `json:"message,omitempty"` // human-readable summary
}

// WorstStatus returns the "worst" status across all metrics.
func (s QuotaSnapshot) WorstPercent() float64 {
	worst := float64(100)
	found := false
	for _, m := range s.Metrics {
		p := m.Percent()
		if p >= 0 {
			found = true
			if p < worst {
				worst = p
			}
		}
	}
	if !found {
		return -1
	}
	return worst
}
