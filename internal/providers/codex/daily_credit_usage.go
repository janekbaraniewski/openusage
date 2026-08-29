package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const codexCreditUsageDailySeriesKey = "codex_credit_usage"

const codexCreditUsageDailyPath = "/wham/usage/daily-workspace-user-credit-usage"

// How long a cached daily-credit response stays usable. The endpoint
// finalises days lazily and reports how far it has settled via
// data_freshness_ts, so the window depends on whether the response's
// historical days are final:
//
//   - settled: every day before today is final. Only today can still move,
//     and today is re-derived from the live cumulative quota on every poll,
//     so the cached history needs refreshing only as a hedge against
//     server-side backfill.
//   - pending: the endpoint has not finalised yesterday yet. Holding this
//     response latches a lagging historical total and skews every forecast
//     derived from it, so retry soon.
//   - unknown: the response carried no usable data_freshness_ts. Split the
//     difference rather than assuming either way.
const (
	dailyCreditUsageSettledTTL          = time.Hour
	dailyCreditUsagePendingTTL          = 5 * time.Minute
	dailyCreditUsageUnknownFreshnessTTL = 15 * time.Minute
)

// How long a *failed* attempt suppresses further requests. Failures are
// cached as well as successes: without this, a rejected token would be
// retried on every poll, which at the 30s default is ~2900 authenticated
// requests a day to an endpoint that has already said no.
const (
	dailyCreditUsageAuthBackoff  = 30 * time.Minute
	dailyCreditUsageErrorBackoff = 2 * time.Minute
)

// errDailyCreditAuth marks a rejected token, which is not worth retrying at
// the same cadence as a transient upstream blip.
var errDailyCreditAuth = errors.New("codex: daily credit auth rejected")

type dailyCreditUsagePayload struct {
	Data               []dailyCreditUsageDay `json:"data"`
	DataFreshnessStamp string                `json:"data_freshness_ts,omitempty"`
}

type dailyCreditUsageDay struct {
	Date   string         `json:"date"`
	Values map[string]any `json:"values"`
}

type dailyCreditUsageCache struct {
	periodStartDay string
	today          string
	totals         map[string]float64
	// rows counts the in-period days the endpoint actually reported, which
	// distinguishes real history from a 200 carrying an empty data array.
	rows      int
	fetchedAt time.Time
	freshness string
}

type dailyCreditUsageFailure struct {
	at      time.Time
	err     error
	backoff time.Duration
}

// fetchDailyCreditUsage adds a complete current-period account-credit series to
// the snapshot. The endpoint is an optional enrichment: callers should record
// an error and keep the normal cumulative-quota path when it is unavailable.
func (p *Provider) fetchDailyCreditUsage(
	ctx context.Context,
	acct core.AccountConfig,
	configDir string,
	snap *core.UsageSnapshot,
) error {
	if p == nil || snap == nil {
		return nil
	}

	metric, ok := snap.Metrics["codex_credit_limit"]
	if !ok || metric.Used == nil {
		return nil
	}
	resetAt, ok := snap.Resets["codex_credit_limit"]
	if !ok || !resetAt.After(snap.Timestamp) {
		return nil
	}
	periodStart, ok := inferCreditPeriodStart(resetAt, snap.Timestamp)
	if !ok {
		return nil
	}

	authPath := filepath.Join(configDir, "auth.json")
	if override := acct.Hint("auth_file", ""); override != "" {
		authPath = override
	}
	authData, err := os.ReadFile(authPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("codex: reading daily credit auth: %w", err)
	}
	var auth authFile
	if err := json.Unmarshal(authData, &auth); err != nil {
		return fmt.Errorf("codex: parsing daily credit auth: %w", err)
	}
	if strings.TrimSpace(auth.Tokens.AccessToken) == "" {
		return nil
	}

	location := time.Local
	startDay := startOfCodexDay(periodStart, location)
	today := startOfCodexDay(snap.Timestamp, location)
	if today.Before(startDay) {
		return nil
	}

	baseURL := resolveChatGPTBaseURL(acct, configDir)
	cacheKey := dailyCreditUsageCacheKey(acct, auth, authPath, baseURL)
	dailyTotals, rowCount, fetchedAt, dataFreshness, cacheHit := p.loadDailyCreditUsageCache(cacheKey, startDay, today)
	if !cacheHit {
		if err := p.dailyCreditUsageBackoff(cacheKey); err != nil {
			return err
		}
		totals, rows, freshness, err := p.requestDailyCreditUsage(ctx, acct, auth, baseURL, snap, startDay, today, location)
		if err != nil {
			p.storeDailyCreditUsageFailure(cacheKey, err)
			return err
		}
		p.clearDailyCreditUsageFailure(cacheKey)
		dailyTotals, dataFreshness = totals, freshness
		fetchedAt = p.creditDailyClock()()
		rowCount = rows
		p.storeDailyCreditUsageCache(cacheKey, dailyCreditUsageCache{
			periodStartDay: formatCodexDay(startDay),
			today:          formatCodexDay(today),
			totals:         dailyTotals,
			rows:           rowCount,
			fetchedAt:      fetchedAt,
			freshness:      dataFreshness,
		})
	}

	// A 200 carrying no usable rows is a valid response, not authoritative
	// history. Claiming otherwise would badge the weakest possible estimate
	// with the strongest possible source, so fall back to the elapsed-time
	// forecast instead. The response still stays cached: it was not a failure.
	if rowCount == 0 {
		snap.EnsureMaps()
		snap.Diagnostics["credit_daily_usage"] = "daily credit endpoint returned no usage rows for the current period"
		return nil
	}

	historicalCredits := 0.0
	for day, credits := range dailyTotals {
		if day < formatCodexDay(today) {
			historicalCredits += credits
		}
	}
	// The daily endpoint can lag behind the live quota response. Today's
	// account-authoritative value is therefore the live cumulative total minus
	// the server-reported historical days, never the daily endpoint's today row.
	usedCredits := *metric.Used
	if math.IsNaN(usedCredits) || math.IsInf(usedCredits, 0) || usedCredits < 0 {
		usedCredits = 0
	}
	dailyTotals[formatCodexDay(today)] = maxFloat(usedCredits - historicalCredits)

	snap.EnsureMaps()
	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	snap.DailySeries[codexCreditUsageDailySeriesKey] = core.SortedTimePoints(dailyTotals)
	snap.Raw["credit_daily_usage_source"] = "account"
	snap.Raw["credit_daily_usage_complete"] = "true"
	snap.Raw["credit_daily_usage_period_start"] = periodStart.UTC().Format(time.RFC3339)
	snap.Raw["credit_daily_usage_fetched_at"] = fetchedAt.UTC().Format(time.RFC3339)
	if dataFreshness != "" {
		snap.Raw["credit_daily_usage_data_freshness"] = dataFreshness
	}
	if cacheHit {
		snap.Raw["credit_daily_usage_cache"] = "hit"
	}
	return nil
}

// requestDailyCreditUsage performs the one upstream call, returning the
// zero-filled per-day totals and how many in-period rows the endpoint
// actually reported.
func (p *Provider) requestDailyCreditUsage(
	ctx context.Context,
	acct core.AccountConfig,
	auth authFile,
	baseURL string,
	snap *core.UsageSnapshot,
	startDay, today time.Time,
	location *time.Location,
) (map[string]float64, int, string, error) {
	components, err := url.Parse(dailyCreditUsageURLForBase(baseURL))
	if err != nil {
		return nil, 0, "", fmt.Errorf("codex: building daily credit URL: %w", err)
	}
	components.RawQuery = url.Values{
		"start_date": []string{formatCodexDay(startDay)},
		"end_date":   []string{formatCodexDay(today)},
		"breakdown":  []string{"product"},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, components.String(), nil)
	if err != nil {
		return nil, 0, "", fmt.Errorf("codex: creating daily credit request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+auth.Tokens.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	if accountID := core.FirstNonEmpty(auth.Tokens.AccountID, auth.AccountID, acct.Hint("account_id", "")); accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}
	if cliVersion := snap.Raw["cli_version"]; cliVersion != "" {
		req.Header.Set("User-Agent", "codex-cli/"+cliVersion)
	} else {
		req.Header.Set("User-Agent", "codex-cli")
	}

	resp, err := p.Client().Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("codex: daily credit request failed: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, 0, "", fmt.Errorf("codex: reading daily credit response: %w", readErr)
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, 0, "", fmt.Errorf("%w: HTTP %d: %s", errDailyCreditAuth, resp.StatusCode, truncateForError(string(body), maxHTTPErrorBodySize))
	default:
		return nil, 0, "", fmt.Errorf("codex: daily credit HTTP %d: %s", resp.StatusCode, truncateForError(string(body), maxHTTPErrorBodySize))
	}

	var payload dailyCreditUsagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, 0, "", fmt.Errorf("codex: parsing daily credit response: %w", err)
	}

	totals := make(map[string]float64)
	for day := startDay; !day.After(today); day = day.AddDate(0, 0, 1) {
		totals[formatCodexDay(day)] = 0
	}

	rows := 0
	for _, point := range payload.Data {
		pointDay, err := parseCodexDay(point.Date, location)
		if err != nil {
			return nil, 0, "", fmt.Errorf("codex: parsing daily credit date %q: %w", point.Date, err)
		}
		if pointDay.Before(startDay) || pointDay.After(today) {
			continue
		}
		credits, ok := parseFlexibleNumber(point.Values["codex"])
		if !ok || math.IsNaN(credits) || math.IsInf(credits, 0) || credits < 0 {
			credits = 0
		}
		totals[formatCodexDay(pointDay)] += credits
		rows++
	}
	return totals, rows, payload.DataFreshnessStamp, nil
}

// dailyCreditUsageCacheTTL reports how long entry stays usable.
//
// Only the historical days of a cached response can go stale: today's total is
// always re-derived from the live cumulative quota. Because the endpoint's
// freshness stamp advances monotonically, "is every historical day final?"
// reduces to a single comparison — has the stamp reached the start of today?
func dailyCreditUsageCacheTTL(entry dailyCreditUsageCache, startDay, today time.Time) time.Duration {
	if !today.After(startDay) {
		// The period started today, so there is no history to settle.
		return dailyCreditUsageSettledTTL
	}
	freshness, err := time.Parse(time.RFC3339, strings.TrimSpace(entry.freshness))
	if err != nil {
		return dailyCreditUsageUnknownFreshnessTTL
	}
	if freshness.Before(today) {
		return dailyCreditUsagePendingTTL
	}
	return dailyCreditUsageSettledTTL
}

// creditDailyClock returns the freshness clock, tolerating a zero-value
// Provider built by a caller that bypassed New.
func (p *Provider) creditDailyClock() func() time.Time {
	if p == nil || p.creditDailyNow == nil {
		return time.Now
	}
	return p.creditDailyNow
}

// setCreditDailyClock overrides the freshness clock. Test-only.
func (p *Provider) setCreditDailyClock(fn func() time.Time) {
	if p != nil && fn != nil {
		p.creditDailyNow = fn
	}
}

func dailyCreditUsageCacheKey(acct core.AccountConfig, auth authFile, authPath, baseURL string) string {
	accountKey := core.FirstNonEmpty(acct.ID, auth.Tokens.AccountID, auth.AccountID, acct.Hint("account_id", ""))
	return strings.Join([]string{accountKey, authPath, baseURL}, "\x00")
}

func (p *Provider) loadDailyCreditUsageCache(key string, startDay, today time.Time) (map[string]float64, int, time.Time, string, bool) {
	if p == nil {
		return nil, 0, time.Time{}, "", false
	}
	p.creditDailyMu.Lock()
	defer p.creditDailyMu.Unlock()
	entry, ok := p.creditDaily[key]
	if !ok || entry.periodStartDay != formatCodexDay(startDay) || entry.today != formatCodexDay(today) {
		return nil, 0, time.Time{}, "", false
	}
	if entry.fetchedAt.IsZero() {
		return nil, 0, time.Time{}, "", false
	}
	if p.creditDailyClock()().Sub(entry.fetchedAt) >= dailyCreditUsageCacheTTL(entry, startDay, today) {
		return nil, 0, time.Time{}, "", false
	}
	totals := make(map[string]float64, len(entry.totals))
	for day, credits := range entry.totals {
		totals[day] = credits
	}
	return totals, entry.rows, entry.fetchedAt, entry.freshness, true
}

// dailyCreditUsageBackoff returns the cached failure while it is still
// suppressing requests, so the caller keeps surfacing the real reason without
// re-hitting an endpoint that has already rejected it.
func (p *Provider) dailyCreditUsageBackoff(key string) error {
	if p == nil {
		return nil
	}
	p.creditDailyMu.Lock()
	defer p.creditDailyMu.Unlock()
	failure, ok := p.creditDailyFail[key]
	if !ok || failure.at.IsZero() {
		return nil
	}
	if p.creditDailyClock()().Sub(failure.at) >= failure.backoff {
		return nil
	}
	return failure.err
}

func (p *Provider) storeDailyCreditUsageFailure(key string, err error) {
	if p == nil || err == nil {
		return
	}
	backoff := dailyCreditUsageErrorBackoff
	if errors.Is(err, errDailyCreditAuth) {
		backoff = dailyCreditUsageAuthBackoff
	}
	p.creditDailyMu.Lock()
	defer p.creditDailyMu.Unlock()
	if p.creditDailyFail == nil {
		p.creditDailyFail = make(map[string]dailyCreditUsageFailure)
	}
	p.creditDailyFail[key] = dailyCreditUsageFailure{at: p.creditDailyClock()(), err: err, backoff: backoff}
}

func (p *Provider) clearDailyCreditUsageFailure(key string) {
	if p == nil {
		return
	}
	p.creditDailyMu.Lock()
	defer p.creditDailyMu.Unlock()
	delete(p.creditDailyFail, key)
}

func (p *Provider) storeDailyCreditUsageCache(key string, entry dailyCreditUsageCache) {
	if p == nil {
		return
	}
	totals := make(map[string]float64, len(entry.totals))
	for day, credits := range entry.totals {
		totals[day] = credits
	}
	entry.totals = totals
	p.creditDailyMu.Lock()
	defer p.creditDailyMu.Unlock()
	if p.creditDaily == nil {
		p.creditDaily = make(map[string]dailyCreditUsageCache)
	}
	p.creditDaily[key] = entry
}

func dailyCreditUsageURLForBase(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.Contains(baseURL, "/backend-api") {
		return baseURL + codexCreditUsageDailyPath
	}
	return baseURL + "/api/codex/usage" + codexCreditUsageDailyPath[len("/wham/usage"):]
}

func startOfCodexDay(value time.Time, location *time.Location) time.Time {
	local := value.In(location)
	year, month, day := local.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

func formatCodexDay(value time.Time) string {
	return value.Format("2006-01-02")
}

func parseCodexDay(value string, location *time.Location) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(value), location)
}

func maxFloat(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
