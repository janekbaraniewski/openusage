// Package active resolves which AI provider the user is currently working
// with, and narrates that provider's quota position.
//
// Selection prefers firsthand telemetry events. When the daemon is
// unavailable, callers can fall back to local-file mtime detection.
package active

import "time"

// Severity is the traffic-light band a consumer paints with.
type Severity string

const (
	SeverityGood    Severity = "good"
	SeverityWarn    Severity = "warn"
	SeverityBad     Severity = "bad"
	SeverityUnknown Severity = "unknown"
)

// Facts are the structured quota numbers behind a rendered label.
type Facts struct {
	AtCap             bool       `json:"at_cap,omitempty"`
	PctUsed           *float64   `json:"pct_used,omitempty"`
	PctRemaining      *float64   `json:"pct_remaining,omitempty"`
	RunoutAt          *time.Time `json:"runout_at,omitempty"`
	ResetAt           *time.Time `json:"reset_at,omitempty"`
	RunoutBeforeReset bool       `json:"runout_before_reset,omitempty"`
	ForecastSource    string     `json:"forecast_source,omitempty"`
	RequestsToday     *float64   `json:"requests_today,omitempty"`
}

// Candidate is one selectable provider account.
type Candidate struct {
	Key         string     `json:"key"`
	ProviderID  string     `json:"provider_id"`
	AccountID   string     `json:"account_id"`
	Display     string     `json:"display"`
	Severity    Severity   `json:"severity,omitempty"`
	LastEventAt *time.Time `json:"last_event_at,omitempty"`
}

// Selection is the answer to "which provider is active, and how is it doing".
type Selection struct {
	Selected string   `json:"selected"`
	Display  string   `json:"display"`
	Pinned   bool     `json:"pinned"`
	Severity Severity `json:"severity"`
	Label    string   `json:"label"`
	Facts    Facts    `json:"facts,omitzero"`
	// Source is "events", "local" (mtime fallback), or "pinned".
	Source string `json:"source"`
	// Status is "ok", "no_data", or "unavailable".
	Status string `json:"status"`
}

// CandidateList is the selector's current candidate set. It is intentionally
// small: status-bar consumers need stable keys, event evidence, and the same
// compact severity band used by the active-provider response, not full
// provider snapshots.
type CandidateList struct {
	Selected   string      `json:"selected,omitempty"`
	Pinned     string      `json:"pinned,omitempty"`
	Status     string      `json:"status"`
	Candidates []Candidate `json:"candidates"`
}

// DetailResponse contains the selected provider's structured metric rows.
// Consumers may use Display for a compact row, or the numeric fields when
// they need their own presentation.
type DetailResponse struct {
	Selection Selection   `json:"selection"`
	Rows      []DetailRow `json:"rows"`
	Status    string      `json:"status"`
	Message   string      `json:"message,omitempty"`
}

// DetailRow is a safe, presentation-ready view of one snapshot metric. Raw
// provider metadata is deliberately absent; this endpoint is suitable for
// shell integrations and should not become a credentials/debug dump.
type DetailRow struct {
	Name    string `json:"name"`
	Display string `json:"display"`
	// Primary marks the small set of rows a compact consumer (status bar
	// popup) should render. Consumers that want everything ignore it.
	Primary   bool       `json:"primary,omitempty"`
	Limit     *float64   `json:"limit,omitempty"`
	Remaining *float64   `json:"remaining,omitempty"`
	Used      *float64   `json:"used,omitempty"`
	Unit      string     `json:"unit,omitempty"`
	Window    string     `json:"window,omitempty"`
	ResetAt   *time.Time `json:"reset_at,omitempty"`
}
