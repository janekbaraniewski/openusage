package parsers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

// RateLimitGroup holds a parsed set of rate-limit header values.
type RateLimitGroup struct {
	Limit     *float64
	Remaining *float64
	ResetTime *time.Time
}

// ParseFloat attempts to parse a header value as float64.
func ParseFloat(val string) *float64 {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return nil
	}
	return &f
}

// ParseResetTime parses common reset-time header formats:
//   - Unix timestamp (e.g. "1700000000")
//   - ISO 8601 / RFC 3339 (e.g. "2025-01-01T00:00:00Z")
//   - Duration strings (e.g. "30s", "1m30s")
func ParseResetTime(val string) *time.Time {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}

	// Try as unix timestamp.
	if ts, err := strconv.ParseFloat(val, 64); err == nil && ts > 1_000_000_000 {
		t := time.Unix(int64(ts), 0)
		return &t
	}

	// Try as RFC 3339.
	if t, err := time.Parse(time.RFC3339, val); err == nil {
		return &t
	}

	// Try as duration string (e.g. "30s", "1m30s").
	if d, err := time.ParseDuration(val); err == nil {
		t := time.Now().Add(d)
		return &t
	}

	return nil
}

// ParseRateLimitGroup extracts limit, remaining, and reset values from HTTP
// headers using the given header names. Returns nil if no data is found.
func ParseRateLimitGroup(h http.Header, limitHeader, remainingHeader, resetHeader string) *RateLimitGroup {
	limit := ParseFloat(h.Get(limitHeader))
	remaining := ParseFloat(h.Get(remainingHeader))
	if limit == nil && remaining == nil {
		return nil
	}
	return &RateLimitGroup{
		Limit:     limit,
		Remaining: remaining,
		ResetTime: ParseResetTime(h.Get(resetHeader)),
	}
}

// ApplyRateLimitGroup parses rate-limit headers and writes the result into the
// given snapshot under the specified metric key. It is a no-op when the headers
// contain no usable data.
func ApplyRateLimitGroup(h http.Header, snap *core.QuotaSnapshot, key, unit, window, limitH, remainH, resetH string) {
	rlg := ParseRateLimitGroup(h, limitH, remainH, resetH)
	if rlg == nil {
		return
	}
	snap.Metrics[key] = core.Metric{
		Limit:     rlg.Limit,
		Remaining: rlg.Remaining,
		Unit:      unit,
		Window:    window,
	}
	if rlg.ResetTime != nil {
		snap.Resets[key+"_reset"] = *rlg.ResetTime
	}
}

// RedactHeaders returns a copy of headers with sensitive values partially redacted.
func RedactHeaders(headers http.Header, sensitiveKeys ...string) map[string]string {
	sensitive := map[string]bool{
		"authorization": true,
		"x-api-key":     true,
		"cookie":        true,
	}
	for _, k := range sensitiveKeys {
		sensitive[strings.ToLower(k)] = true
	}

	out := make(map[string]string)
	for k, vals := range headers {
		key := strings.ToLower(k)
		val := strings.Join(vals, ", ")
		if sensitive[key] {
			if len(val) > 8 {
				val = val[:4] + "..." + val[len(val)-4:]
			} else {
				val = "****"
			}
		}
		out[k] = val
	}
	return out
}
