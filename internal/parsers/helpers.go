package parsers

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

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
