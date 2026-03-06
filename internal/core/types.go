package core

import (
	"maps"
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

// Percent returns the remaining percentage (0–100) or -1 if unknown.
// For used percentage, use MetricUsedPercent which is context-aware.
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
		ProviderID:  providerID,
		AccountID:   accountID,
		Timestamp:   time.Now(),
		Metrics:     make(map[string]Metric),
		Resets:      make(map[string]time.Time),
		Attributes:  make(map[string]string),
		Diagnostics: make(map[string]string),
		Raw:         make(map[string]string),
	}
}

func NewAuthSnapshot(providerID, accountID, message string) UsageSnapshot {
	snap := NewUsageSnapshot(providerID, accountID)
	snap.Status = StatusAuth
	snap.Message = message
	return snap
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

// DeepClone returns a deep copy of the snapshot with all map and pointer
// fields fully independent from the original.
func (s UsageSnapshot) DeepClone() UsageSnapshot {
	clone := s
	clone.Metrics = deepCloneMetrics(s.Metrics)
	clone.Resets = maps.Clone(s.Resets)
	clone.Attributes = maps.Clone(s.Attributes)
	clone.Diagnostics = maps.Clone(s.Diagnostics)
	clone.Raw = maps.Clone(s.Raw)

	if s.ModelUsage != nil {
		clone.ModelUsage = make([]ModelUsageRecord, len(s.ModelUsage))
		for i, rec := range s.ModelUsage {
			clone.ModelUsage[i] = rec
			clone.ModelUsage[i].Dimensions = maps.Clone(rec.Dimensions)
			clone.ModelUsage[i].InputTokens = cloneFloat64Ptr(rec.InputTokens)
			clone.ModelUsage[i].OutputTokens = cloneFloat64Ptr(rec.OutputTokens)
			clone.ModelUsage[i].CachedTokens = cloneFloat64Ptr(rec.CachedTokens)
			clone.ModelUsage[i].ReasoningTokens = cloneFloat64Ptr(rec.ReasoningTokens)
			clone.ModelUsage[i].TotalTokens = cloneFloat64Ptr(rec.TotalTokens)
			clone.ModelUsage[i].CostUSD = cloneFloat64Ptr(rec.CostUSD)
			clone.ModelUsage[i].Requests = cloneFloat64Ptr(rec.Requests)
		}
	}

	if s.DailySeries != nil {
		clone.DailySeries = make(map[string][]TimePoint, len(s.DailySeries))
		for k, pts := range s.DailySeries {
			cp := make([]TimePoint, len(pts))
			copy(cp, pts)
			clone.DailySeries[k] = cp
		}
	}

	return clone
}

// DeepCloneSnapshots returns a deep copy of a snapshot map where each
// snapshot is independently deep-cloned.
func DeepCloneSnapshots(m map[string]UsageSnapshot) map[string]UsageSnapshot {
	if m == nil {
		return nil
	}
	out := make(map[string]UsageSnapshot, len(m))
	for k, v := range m {
		out[k] = v.DeepClone()
	}
	return out
}

func deepCloneMetrics(m map[string]Metric) map[string]Metric {
	if m == nil {
		return nil
	}
	out := make(map[string]Metric, len(m))
	for k, v := range m {
		out[k] = Metric{
			Limit:     cloneFloat64Ptr(v.Limit),
			Remaining: cloneFloat64Ptr(v.Remaining),
			Used:      cloneFloat64Ptr(v.Used),
			Unit:      v.Unit,
			Window:    v.Window,
		}
	}
	return out
}

func cloneFloat64Ptr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
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
