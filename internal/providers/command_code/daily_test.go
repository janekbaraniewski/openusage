package command_code

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func resetDayCache(t *testing.T) {
	t.Helper()
	reset := func() {
		dayCache.Lock()
		dayCache.byAccount = map[string]map[string]dayUsage{}
		dayCache.walkedAt = map[string]time.Time{}
		dayCache.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

// summaryServer answers the summary route from a table of requests per local
// day: a `since` query returns everything on or after that instant.
func summaryServer(t *testing.T, perDay map[string]float64, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		var since time.Time
		if raw := r.URL.Query().Get("since"); raw != "" {
			since, _ = time.Parse(time.RFC3339, raw)
		}
		total := 0.0
		for day, count := range perDay {
			start, _ := time.ParseInLocation(dayLayout, day, time.Local)
			if !start.Before(since) {
				total += count
			}
		}
		fmt.Fprintf(w, `{"totalCount":%v,"totalCost":%v,"totalTokens":%v}`, total, total/10, total*100)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestApplyDailySeriesDifferencesSummaries(t *testing.T) {
	resetDayCache(t)
	today := localMidnight(time.Now())
	day := func(back int) string { return today.AddDate(0, 0, -back).Format(dayLayout) }
	perDay := map[string]float64{day(0): 4, day(1): 10, day(2): 0, day(3): 6}
	calls := 0
	srv := summaryServer(t, perDay, &calls)

	p := New()
	fetch := func() core.UsageSnapshot {
		snap := core.NewUsageSnapshot("command_code", "command_code")
		period, _, _ := p.fetchSummary(context.Background(), srv.URL, "key", "", "")
		todaySummary, _, _ := p.fetchSummary(context.Background(), srv.URL, "key", "", sinceParam(today))
		p.applyDailySeries(context.Background(), srv.URL, "key", "", &snap, period, todaySummary)
		return snap
	}

	snap := fetch()
	want := map[string]float64{day(3): 6, day(2): 0, day(1): 10, day(0): 4}
	got := snap.DailySeries["requests"]
	if len(got) != len(want) {
		t.Fatalf("requests series = %+v, want %d days", got, len(want))
	}
	for _, point := range got {
		if want[point.Date] != point.Value {
			t.Errorf("requests[%s] = %v, want %v", point.Date, point.Value, want[point.Date])
		}
	}
	if cost := snap.DailySeries["cost"]; cost[len(cost)-1].Value != 0.4 {
		t.Errorf("today cost = %v, want 0.4", cost[len(cost)-1].Value)
	}

	// Finished days come from the cache on the next poll.
	calls = 0
	fetch()
	if calls != 2 {
		t.Errorf("second poll made %d summary calls, want 2 (period and today)", calls)
	}
}
