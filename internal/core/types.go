package core

import (
	"time"

	"github.com/samber/lo"
)

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

type Metric struct {
	Limit     *float64 `json:"limit,omitempty"`
	Remaining *float64 `json:"remaining,omitempty"`
	Used      *float64 `json:"used,omitempty"`
	Unit      string   `json:"unit"`   // "requests", "tokens", "USD", "credits"
	Window    string   `json:"window"` // "1m", "1d", "month", "rolling-5h", etc.
}

func (m Metric) Percent() float64 {
	if m.Limit != nil && m.Remaining != nil && *m.Limit > 0 {
		return (*m.Remaining / *m.Limit) * 100
	}
	if m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		return ((*m.Limit - *m.Used) / *m.Limit) * 100
	}
	return -1
}

type TimePoint struct {
	Date  string  `json:"date"`  // "2025-01-15"
	Value float64 `json:"value"` // metric value at that date
}

type UsageSnapshot struct {
	ProviderID  string                 `json:"provider_id"`
	AccountID   string                 `json:"account_id"`
	Timestamp   time.Time              `json:"timestamp"`
	Status      Status                 `json:"status"`
	Metrics     map[string]Metric      `json:"metrics"`                // keys like "rpm", "tpm", "rpd"
	Resets      map[string]time.Time   `json:"resets,omitempty"`       // e.g. "rpm_reset"
	Attributes  map[string]string      `json:"attributes,omitempty"`   // normalized provider/account metadata
	Diagnostics map[string]string      `json:"diagnostics,omitempty"`  // non-fatal errors, warnings, probe/debug notes
	Raw         map[string]string      `json:"raw,omitempty"`          // provider metadata/debug bag (not for primary quota analytics)
	ModelUsage  []ModelUsageRecord     `json:"model_usage,omitempty"`  // per-model usage rows with canonical IDs
	DailySeries map[string][]TimePoint `json:"daily_series,omitempty"` // time-indexed data (e.g. "messages", "cost", "tokens_<model>")
	Message     string                 `json:"message,omitempty"`      // human-readable summary
}

func NewUsageSnapshot(providerID, accountID string) UsageSnapshot {
	return UsageSnapshot{
		ProviderID: providerID,
		AccountID:  accountID,
		Timestamp:  time.Now(),
		Metrics:    make(map[string]Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
	}
}

func NewAuthSnapshot(providerID, accountID, message string) UsageSnapshot {
	return UsageSnapshot{
		ProviderID: providerID,
		AccountID:  accountID,
		Timestamp:  time.Now(),
		Status:     StatusAuth,
		Message:    message,
	}
}

func MergeAccounts(manual, autoDetected []AccountConfig) []AccountConfig {
	return lo.UniqBy(append(manual, autoDetected...), func(acct AccountConfig) string {
		return acct.ID
	})
}

func (s *UsageSnapshot) EnsureMaps() {
	if s.Metrics == nil {
		s.Metrics = make(map[string]Metric)
	}
	if s.Resets == nil {
		s.Resets = make(map[string]time.Time)
	}
	if s.Attributes == nil {
		s.Attributes = make(map[string]string)
	}
	if s.Diagnostics == nil {
		s.Diagnostics = make(map[string]string)
	}
	if s.Raw == nil {
		s.Raw = make(map[string]string)
	}
}

func (s *UsageSnapshot) SetAttribute(key, value string) {
	if key == "" || value == "" {
		return
	}
	s.EnsureMaps()
	s.Attributes[key] = value
}

func (s *UsageSnapshot) SetDiagnostic(key, value string) {
	if key == "" || value == "" {
		return
	}
	s.EnsureMaps()
	s.Diagnostics[key] = value
}

func (s UsageSnapshot) MetaValue(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	if v, ok := s.Attributes[key]; ok && v != "" {
		return v, true
	}
	if v, ok := s.Diagnostics[key]; ok && v != "" {
		return v, true
	}
	if v, ok := s.Raw[key]; ok && v != "" {
		return v, true
	}
	return "", false
}

func (s UsageSnapshot) WorstPercent() float64 {
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
