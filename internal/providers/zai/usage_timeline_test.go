package zai

import (
	"encoding/json"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestProjectUsageTimelineSumsBucketsByDay(t *testing.T) {
	data := json.RawMessage(`{
		"x_time": ["2026-10-05 00:00", "2026-10-05 01:00", "2026-10-06 00:00"],
		"modelCallCount": [2, 3, null],
		"tokensUsage": [100, 200, 50]
	}`)
	snap := core.NewUsageSnapshot("zai", "zai")

	projectUsageTimeline(data, &snap)

	requests := snap.DailySeries["requests"]
	if len(requests) != 1 || requests[0].Date != "2026-10-05" || requests[0].Value != 5 {
		t.Errorf("requests = %+v", requests)
	}
	tokens := snap.DailySeries["tokens"]
	if len(tokens) != 2 || tokens[0].Value != 300 || tokens[1].Value != 50 {
		t.Errorf("tokens = %+v", tokens)
	}
}

func TestProjectUsageTimelineKeepsExistingTrend(t *testing.T) {
	snap := core.NewUsageSnapshot("zai", "zai")
	existing := []core.TimePoint{{Date: "2026-10-04", Value: 1}, {Date: "2026-10-05", Value: 2}}
	snap.DailySeries = map[string][]core.TimePoint{"requests": existing}

	projectUsageTimeline(json.RawMessage(`{"x_time":["2026-10-05 00:00"],"modelCallCount":[9],"tokensUsage":[9]}`), &snap)

	if got := snap.DailySeries["requests"]; len(got) != 2 || got[1].Value != 2 {
		t.Errorf("existing requests series was replaced: %+v", got)
	}
}
