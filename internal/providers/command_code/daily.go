package command_code

import (
	"context"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	// dailySeriesDays bounds how far back the daily series reaches. The
	// summary endpoint only covers the current billing period, so the series
	// starts at the later of this span and the period start.
	dailySeriesDays = 30
	dayLayout       = "2006-01-02"
	// dailyWalkInterval is the minimum gap between walks over past days.
	dailyWalkInterval = 10 * time.Minute
)

// dayUsage is one local day's usage.
type dayUsage struct {
	cost, requests, tokens float64
}

// dayCache holds finished days per account. A finished day's usage never
// changes, so it is fetched once and reused on every later poll.
var dayCache = struct {
	sync.Mutex
	byAccount map[string]map[string]dayUsage
	walkedAt  map[string]time.Time
}{byAccount: map[string]map[string]dayUsage{}, walkedAt: map[string]time.Time{}}

// walkAllowed rate-limits the walk over past days: it costs one request per
// day, and a walk discarded because usage was in flight would otherwise
// repeat on every poll.
func walkAllowed(accountID string) bool {
	dayCache.Lock()
	defer dayCache.Unlock()
	if last, ok := dayCache.walkedAt[accountID]; ok && time.Since(last) < dailyWalkInterval {
		return false
	}
	dayCache.walkedAt[accountID] = time.Now()
	return true
}

func cachedDays(accountID string) map[string]dayUsage {
	dayCache.Lock()
	defer dayCache.Unlock()
	out := make(map[string]dayUsage, len(dayCache.byAccount[accountID]))
	for day, usage := range dayCache.byAccount[accountID] {
		out[day] = usage
	}
	return out
}

func storeDays(accountID string, days map[string]dayUsage) {
	dayCache.Lock()
	defer dayCache.Unlock()
	if dayCache.byAccount[accountID] == nil {
		dayCache.byAccount[accountID] = map[string]dayUsage{}
	}
	for day, usage := range days {
		dayCache.byAccount[accountID][day] = usage
	}
}

func localMidnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func sinceParam(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// applyDailySeries builds daily cost, request and token series.
//
// The summary endpoint reports usage from a `since` instant to now, so one
// day's usage is the difference between the summaries taken at its two
// midnights. Walking back stops at the first day whose summary already equals
// the whole billing period: nothing earlier is visible to this endpoint.
func (p *Provider) applyDailySeries(ctx context.Context, baseURL, apiKey, orgID string, snap *core.UsageSnapshot, period, today summaryResponse) {
	todayStart := localMidnight(time.Now())
	days := cachedDays(snap.AccountID)
	fresh := map[string]dayUsage{}

	later := today
	yesterday := todayStart.AddDate(0, 0, -1).Format(dayLayout)
	_, haveYesterday := days[yesterday]
	walk := !haveYesterday && walkAllowed(snap.AccountID)
	for back := 1; walk && back < dailySeriesDays; back++ {
		dayStart := todayStart.AddDate(0, 0, -back)
		day := dayStart.Format(dayLayout)
		if _, ok := days[day]; ok {
			// Everything older was fetched when this day was.
			break
		}
		summary, _, err := p.fetchSummary(ctx, baseURL, apiKey, orgID, sinceParam(dayStart))
		if err != nil {
			snap.SetDiagnostic("daily_summary_error", err.Error())
			fresh = nil
			break
		}
		fresh[day] = dayUsage{
			cost:     summary.TotalCost - later.TotalCost,
			requests: summary.TotalCount - later.TotalCount,
			tokens:   summary.TotalTokens - later.TotalTokens,
		}
		if summary.TotalCount >= period.TotalCount {
			break
		}
		later = summary
	}

	if len(fresh) > 0 {
		// The summaries were taken moments apart. If usage landed in between,
		// the differences are skewed, so keep them only when today's summary
		// is unchanged at the end of the walk.
		check, _, err := p.fetchSummary(ctx, baseURL, apiKey, orgID, sinceParam(todayStart))
		if err == nil && check.TotalCount == today.TotalCount {
			storeDays(snap.AccountID, fresh)
			for day, usage := range fresh {
				days[day] = usage
			}
		}
	}

	cutoff := todayStart.AddDate(0, 0, -(dailySeriesDays - 1)).Format(dayLayout)
	cost := map[string]float64{}
	requests := map[string]float64{}
	tokens := map[string]float64{}
	for day, usage := range days {
		if day < cutoff {
			continue
		}
		cost[day], requests[day], tokens[day] = usage.cost, usage.requests, usage.tokens
	}
	todayKey := todayStart.Format(dayLayout)
	cost[todayKey], requests[todayKey], tokens[todayKey] = today.TotalCost, today.TotalCount, today.TotalTokens

	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	snap.DailySeries["cost"] = core.SortedTimePoints(cost)
	snap.DailySeries["requests"] = core.SortedTimePoints(requests)
	snap.DailySeries["tokens"] = core.SortedTimePoints(tokens)
}
