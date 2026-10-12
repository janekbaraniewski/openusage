package zai

import (
	"encoding/json"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// usageTimeline is the columnar form of the model-usage payload: one entry
// per time bucket in x_time, with parallel arrays of request and token
// counts for the whole account.
type usageTimeline struct {
	XTime          []string   `json:"x_time"`
	ModelCallCount []*float64 `json:"modelCallCount"`
	TokensUsage    []*float64 `json:"tokensUsage"`
}

// projectUsageTimeline sums the time buckets into daily request and token
// series. It fills only series the per-sample projection left without a
// trend, so payloads that carry dated samples keep their own figures.
func projectUsageTimeline(data json.RawMessage, snap *core.UsageSnapshot) {
	var timeline usageTimeline
	if err := json.Unmarshal(data, &timeline); err != nil || len(timeline.XTime) == 0 {
		return
	}

	daily := func(values []*float64) []core.TimePoint {
		if len(values) != len(timeline.XTime) {
			return nil
		}
		byDay := make(map[string]float64)
		for i, stamp := range timeline.XTime {
			if len(stamp) < len("2006-01-02") || values[i] == nil {
				continue
			}
			byDay[stamp[:len("2006-01-02")]] += *values[i]
		}
		return core.SortedTimePoints(byDay)
	}

	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	for key, values := range map[string][]*float64{"requests": timeline.ModelCallCount, "tokens": timeline.TokensUsage} {
		if len(snap.DailySeries[key]) > 1 {
			continue
		}
		if points := daily(values); len(points) > 0 {
			snap.DailySeries[key] = points
		}
	}
}
