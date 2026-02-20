package parsers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type RateLimitGroup struct {
	Limit     *float64
	Remaining *float64
	ResetTime *time.Time
}

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

func ParseResetTime(val string) *time.Time {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}

	if ts, err := strconv.ParseFloat(val, 64); err == nil && ts > 1_000_000_000 {
		t := time.Unix(int64(ts), 0)
		return &t
	}

	if t, err := time.Parse(time.RFC3339, val); err == nil {
		return &t
	}

	if d, err := time.ParseDuration(val); err == nil {
		t := time.Now().Add(d)
		return &t
	}

	return nil
}

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
