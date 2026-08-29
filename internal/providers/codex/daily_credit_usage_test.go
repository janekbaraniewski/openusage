package codex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetchDailyCreditUsageBuildsCompleteAccountSeries(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"test-token","account_id":"acct-123"}}`), 0600); err != nil {
		t.Fatal(err)
	}

	resetAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	observedAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	periodStart, ok := inferCreditPeriodStart(resetAt, observedAt)
	if !ok {
		t.Fatal("expected an inferred period start")
	}
	startDay := startOfCodexDay(periodStart, time.Local)
	today := startOfCodexDay(observedAt, time.Local)
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/backend-api/wham/usage/daily-workspace-user-credit-usage" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct-123" {
			t.Fatalf("ChatGPT-Account-Id = %q, want acct-123", got)
		}
		if got := r.URL.Query().Get("start_date"); got != formatCodexDay(startDay) {
			t.Fatalf("start_date = %q, want %q", got, formatCodexDay(startDay))
		}
		if got := r.URL.Query().Get("end_date"); got != formatCodexDay(today) {
			t.Fatalf("end_date = %q, want %q", got, formatCodexDay(today))
		}
		if got := r.URL.Query().Get("breakdown"); got != "product" {
			t.Fatalf("breakdown = %q, want product", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, fmt.Sprintf(`{
			"data": [
				{"date": %q, "values": {"codex": 10}},
				{"date": %q, "values": {"codex": 999}}
			],
			"data_freshness_ts": "2026-08-03T12:00:00Z"
		}`, formatCodexDay(startDay), formatCodexDay(today)))
	}))
	defer server.Close()

	limit := 100.0
	used := 35.0
	account := core.AccountConfig{
		ID: "test",
		RuntimeHints: map[string]string{
			"config_dir":       tmpDir,
			"auth_file":        authPath,
			"chatgpt_base_url": server.URL + "/backend-api",
		},
	}
	snap := core.NewUsageSnapshot("codex", "test")
	snap.Timestamp = observedAt
	snap.Metrics["codex_credit_limit"] = core.Metric{Limit: &limit, Used: &used, Unit: "credits"}
	snap.Resets["codex_credit_limit"] = resetAt

	p := New()
	p.HTTPClient = server.Client()
	err := p.fetchDailyCreditUsage(context.Background(), account, tmpDir, &snap)
	if err != nil {
		t.Fatalf("fetchDailyCreditUsage() error: %v", err)
	}

	points := snap.DailySeries[codexCreditUsageDailySeriesKey]
	if len(points) != 3 {
		t.Fatalf("daily points = %d, want 3: %+v", len(points), points)
	}
	if points[0].Date != formatCodexDay(startDay) || points[0].Value != 10 {
		t.Fatalf("first daily point = %+v, want %s/10", points[0], formatCodexDay(startDay))
	}
	if points[1].Value != 0 {
		t.Fatalf("missing day value = %v, want 0", points[1].Value)
	}
	// The live cumulative total wins for today: 35 - 10 historical credits.
	if points[2].Date != formatCodexDay(today) || points[2].Value != 25 {
		t.Fatalf("today daily point = %+v, want %s/25", points[2], formatCodexDay(today))
	}
	if snap.Raw["credit_daily_usage_source"] != "account" || snap.Raw["credit_daily_usage_complete"] != "true" {
		t.Fatalf("unexpected daily usage metadata: %+v", snap.Raw)
	}
	if snap.Raw["credit_daily_usage_data_freshness"] != "2026-08-03T12:00:00Z" {
		t.Fatalf("unexpected freshness timestamp: %q", snap.Raw["credit_daily_usage_data_freshness"])
	}

	// Historical daily data is cached for the current day, but today's value
	// must continue to follow the live cumulative quota.
	usedAgain := 40.0
	snapAgain := core.NewUsageSnapshot("codex", "test")
	snapAgain.Timestamp = observedAt
	snapAgain.Metrics["codex_credit_limit"] = core.Metric{Limit: &limit, Used: &usedAgain, Unit: "credits"}
	snapAgain.Resets["codex_credit_limit"] = resetAt
	if err := p.fetchDailyCreditUsage(context.Background(), account, tmpDir, &snapAgain); err != nil {
		t.Fatalf("cached fetchDailyCreditUsage() error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("daily endpoint requests = %d, want one cached request", requests)
	}
	pointsAgain := snapAgain.DailySeries[codexCreditUsageDailySeriesKey]
	if pointsAgain[len(pointsAgain)-1].Value != 30 {
		t.Fatalf("cached today's value = %v, want 30", pointsAgain[len(pointsAgain)-1].Value)
	}
	if snapAgain.Raw["credit_daily_usage_cache"] != "hit" {
		t.Fatalf("expected cache-hit metadata, got %+v", snapAgain.Raw)
	}
}

func TestApplyCreditForecastUsesAccountDailyAverageAndProjectsReserve(t *testing.T) {
	resetAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	observedAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	periodStart, ok := inferCreditPeriodStart(resetAt, observedAt)
	if !ok {
		t.Fatal("expected an inferred period start")
	}
	startDay := startOfCodexDay(periodStart, time.Local)

	limit := 100.0
	used := 35.0
	snap := core.NewUsageSnapshot("codex", "test")
	snap.Timestamp = observedAt
	snap.Metrics["codex_credit_limit"] = core.Metric{Limit: &limit, Used: &used, Unit: "credits"}
	snap.Resets["codex_credit_limit"] = resetAt
	snap.DailySeries = map[string][]core.TimePoint{
		codexCreditUsageDailySeriesKey: {
			{Date: formatCodexDay(startDay), Value: 10},
			{Date: formatCodexDay(startDay.AddDate(0, 0, 1)), Value: 0},
			// This stale today value must be replaced with used - history = 25.
			{Date: formatCodexDay(startDay.AddDate(0, 0, 2)), Value: 999},
		},
	}

	New().applyCreditForecast(&snap, "test")

	dailyAverage := snap.Metrics["codex_credit_daily_average"]
	if dailyAverage.Used == nil || *dailyAverage.Used < 11.666 || *dailyAverage.Used > 11.667 {
		t.Fatalf("daily average = %v, want 35/3", dailyAverage.Used)
	}
	projected := snap.Metrics["codex_credit_projected_credits_at_reset"]
	if projected.Used == nil || *projected.Used < 361.66 || *projected.Used > 361.67 {
		t.Fatalf("projected credits = %v, want about 361.67", projected.Used)
	}
	reserve := snap.Metrics["codex_credit_projected_reserve_at_reset"]
	if reserve.Used == nil || *reserve.Used > -261.66 || *reserve.Used < -261.67 {
		t.Fatalf("projected reserve = %v, want about -261.67", reserve.Used)
	}
	rate := snap.Metrics["codex_credit_burn_rate"]
	if rate.Used == nil || *rate.Used < 0.486 || *rate.Used > 0.487 {
		t.Fatalf("daily burn rate = %v, want about 0.486 credits/hour", rate.Used)
	}
	if snap.Raw["credit_forecast_source"] != "account_daily_history" {
		t.Fatalf("forecast source = %q, want account_daily_history", snap.Raw["credit_forecast_source"])
	}
}

func TestDailyCreditUsageCacheTTL(t *testing.T) {
	startDay := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	today := time.Date(2026, 8, 3, 0, 0, 0, 0, time.Local)

	tests := []struct {
		name      string
		startDay  time.Time
		today     time.Time
		freshness string
		want      time.Duration
	}{
		{
			name:      "history finalised through today's start",
			startDay:  startDay,
			today:     today,
			freshness: today.Format(time.RFC3339),
			want:      dailyCreditUsageSettledTTL,
		},
		{
			name:      "history finalised past today's start",
			startDay:  startDay,
			today:     today,
			freshness: today.Add(6 * time.Hour).Format(time.RFC3339),
			want:      dailyCreditUsageSettledTTL,
		},
		{
			name:      "yesterday still accumulating",
			startDay:  startDay,
			today:     today,
			freshness: today.Add(-time.Hour).Format(time.RFC3339),
			want:      dailyCreditUsagePendingTTL,
		},
		{
			name:     "no freshness stamp",
			startDay: startDay,
			today:    today,
			want:     dailyCreditUsageUnknownFreshnessTTL,
		},
		{
			name:      "unparseable freshness stamp",
			startDay:  startDay,
			today:     today,
			freshness: "not-a-timestamp",
			want:      dailyCreditUsageUnknownFreshnessTTL,
		},
		{
			name:      "period started today so there is no history to settle",
			startDay:  today,
			today:     today,
			freshness: today.Add(-time.Hour).Format(time.RFC3339),
			want:      dailyCreditUsageSettledTTL,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := dailyCreditUsageCache{freshness: tc.freshness}
			if got := dailyCreditUsageCacheTTL(entry, tc.startDay, tc.today); got != tc.want {
				t.Fatalf("dailyCreditUsageCacheTTL() = %s, want %s", got, tc.want)
			}
		})
	}
}

// dailyCreditUsageFixture serves a lagging first response (the middle day has
// not been finalised) followed by a settled one, mirroring how the endpoint
// backfills recent days.
type dailyCreditUsageFixture struct {
	server   *httptest.Server
	requests int
	account  core.AccountConfig
	newSnap  func() core.UsageSnapshot
	startDay time.Time
	midDay   time.Time
	today    time.Time
}

func newDailyCreditUsageFixture(t *testing.T, firstFreshness, secondFreshness string) *dailyCreditUsageFixture {
	t.Helper()
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"test-token","account_id":"acct-123"}}`), 0600); err != nil {
		t.Fatal(err)
	}

	resetAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	observedAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	periodStart, ok := inferCreditPeriodStart(resetAt, observedAt)
	if !ok {
		t.Fatal("expected an inferred period start")
	}

	fixture := &dailyCreditUsageFixture{
		startDay: startOfCodexDay(periodStart, time.Local),
		today:    startOfCodexDay(observedAt, time.Local),
	}
	fixture.midDay = fixture.startDay.AddDate(0, 0, 1)

	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.requests++
		w.Header().Set("Content-Type", "application/json")
		if fixture.requests == 1 {
			fmt.Fprintf(w, `{"data":[{"date":%q,"values":{"codex":10}}],"data_freshness_ts":%q}`,
				formatCodexDay(fixture.startDay), firstFreshness)
			return
		}
		fmt.Fprintf(w, `{"data":[{"date":%q,"values":{"codex":10}},{"date":%q,"values":{"codex":8}}],"data_freshness_ts":%q}`,
			formatCodexDay(fixture.startDay), formatCodexDay(fixture.midDay), secondFreshness)
	}))
	t.Cleanup(fixture.server.Close)

	fixture.account = core.AccountConfig{
		ID: "test",
		RuntimeHints: map[string]string{
			"config_dir":       tmpDir,
			"auth_file":        authPath,
			"chatgpt_base_url": fixture.server.URL + "/backend-api",
		},
	}
	limit := 100.0
	used := 35.0
	fixture.newSnap = func() core.UsageSnapshot {
		snap := core.NewUsageSnapshot("codex", "test")
		snap.Timestamp = observedAt
		snap.Metrics["codex_credit_limit"] = core.Metric{Limit: &limit, Used: &used, Unit: "credits"}
		snap.Resets["codex_credit_limit"] = resetAt
		return snap
	}
	return fixture
}

func TestFetchDailyCreditUsageRefreshesPendingHistory(t *testing.T) {
	// The first response has not finalised yesterday; the second has.
	fx := newDailyCreditUsageFixture(t,
		time.Date(2026, 8, 2, 23, 0, 0, 0, time.Local).Format(time.RFC3339),
		time.Date(2026, 8, 3, 6, 0, 0, 0, time.Local).Format(time.RFC3339),
	)

	p := New()
	p.HTTPClient = fx.server.Client()
	clock := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	p.setCreditDailyClock(func() time.Time { return clock })

	first := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &first); err != nil {
		t.Fatalf("first fetchDailyCreditUsage() error: %v", err)
	}
	assertDailyValues(t, "first fetch", first, map[string]float64{
		formatCodexDay(fx.startDay): 10,
		formatCodexDay(fx.midDay):   0,
		formatCodexDay(fx.today):    25,
	})

	// Inside the pending window: reuse the lagging response.
	clock = clock.Add(dailyCreditUsagePendingTTL - time.Minute)
	cached := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &cached); err != nil {
		t.Fatalf("cached fetchDailyCreditUsage() error: %v", err)
	}
	if fx.requests != 1 {
		t.Fatalf("requests inside the pending window = %d, want 1", fx.requests)
	}
	if cached.Raw["credit_daily_usage_cache"] != "hit" {
		t.Fatalf("expected a cache hit inside the pending window, got %+v", cached.Raw)
	}

	// Past it: refetch, and the finalised middle day must win instead of
	// staying latched at the lagging zero.
	clock = clock.Add(2 * time.Minute)
	refreshed := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &refreshed); err != nil {
		t.Fatalf("refreshed fetchDailyCreditUsage() error: %v", err)
	}
	if fx.requests != 2 {
		t.Fatalf("requests past the pending window = %d, want 2", fx.requests)
	}
	assertDailyValues(t, "refreshed fetch", refreshed, map[string]float64{
		formatCodexDay(fx.startDay): 10,
		formatCodexDay(fx.midDay):   8,
		formatCodexDay(fx.today):    17,
	})
}

func TestFetchDailyCreditUsageHoldsSettledHistory(t *testing.T) {
	settled := time.Date(2026, 8, 3, 6, 0, 0, 0, time.Local).Format(time.RFC3339)
	fx := newDailyCreditUsageFixture(t, settled, settled)

	p := New()
	p.HTTPClient = fx.server.Client()
	clock := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	p.setCreditDailyClock(func() time.Time { return clock })

	first := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &first); err != nil {
		t.Fatalf("first fetchDailyCreditUsage() error: %v", err)
	}

	// Long past the pending window, but the server already called the history
	// final, so the response is still good.
	clock = clock.Add(dailyCreditUsageSettledTTL - time.Minute)
	cached := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &cached); err != nil {
		t.Fatalf("cached fetchDailyCreditUsage() error: %v", err)
	}
	if fx.requests != 1 {
		t.Fatalf("requests inside the settled window = %d, want 1", fx.requests)
	}
	if cached.Raw["credit_daily_usage_cache"] != "hit" {
		t.Fatalf("expected a cache hit inside the settled window, got %+v", cached.Raw)
	}

	// The settled window is a backfill hedge, not a promise: it still expires.
	clock = clock.Add(2 * time.Minute)
	refreshed := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &refreshed); err != nil {
		t.Fatalf("refreshed fetchDailyCreditUsage() error: %v", err)
	}
	if fx.requests != 2 {
		t.Fatalf("requests past the settled window = %d, want 2", fx.requests)
	}
	assertDailyValues(t, "refreshed fetch", refreshed, map[string]float64{
		formatCodexDay(fx.startDay): 10,
		formatCodexDay(fx.midDay):   8,
		formatCodexDay(fx.today):    17,
	})
}

func assertDailyValues(t *testing.T, label string, snap core.UsageSnapshot, want map[string]float64) {
	t.Helper()
	points := snap.DailySeries[codexCreditUsageDailySeriesKey]
	if len(points) != len(want) {
		t.Fatalf("%s: daily points = %d, want %d: %+v", label, len(points), len(want), points)
	}
	for _, point := range points {
		expected, ok := want[point.Date]
		if !ok {
			t.Fatalf("%s: unexpected daily point %+v", label, point)
		}
		if point.Value != expected {
			t.Fatalf("%s: %s = %v, want %v", label, point.Date, point.Value, expected)
		}
	}
}

func TestFetchDailyCreditUsageBacksOffAfterAuthFailure(t *testing.T) {
	fx := newDailyCreditUsageFixture(t, "", "")
	calls := 0
	fx.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"token expired"}`)
	})

	p := New()
	p.HTTPClient = fx.server.Client()
	clock := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	p.setCreditDailyClock(func() time.Time { return clock })

	// Ten polls at the 30s default interval.
	var lastErr error
	for i := 0; i < 10; i++ {
		snap := fx.newSnap()
		lastErr = p.fetchDailyCreditUsage(context.Background(), fx.account, "", &snap)
		if lastErr == nil {
			t.Fatalf("poll %d: expected the auth failure to keep surfacing", i)
		}
		clock = clock.Add(30 * time.Second)
	}
	if calls != 1 {
		t.Fatalf("upstream requests across 10 polls = %d, want 1", calls)
	}

	// The backoff eventually lapses so a re-authenticated token recovers.
	clock = clock.Add(dailyCreditUsageAuthBackoff)
	snap := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &snap); err == nil {
		t.Fatal("expected the retried auth failure to surface")
	}
	if calls != 2 {
		t.Fatalf("upstream requests after the auth backoff = %d, want 2", calls)
	}
}

func TestFetchDailyCreditUsageBacksOffAfterTransientFailure(t *testing.T) {
	fx := newDailyCreditUsageFixture(t, "", "")
	calls := 0
	fx.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
	})

	p := New()
	p.HTTPClient = fx.server.Client()
	clock := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	p.setCreditDailyClock(func() time.Time { return clock })

	first := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &first); err == nil {
		t.Fatal("expected the upstream failure to surface")
	}
	clock = clock.Add(dailyCreditUsageErrorBackoff - time.Second)
	held := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &held); err == nil {
		t.Fatal("expected the cached failure to keep surfacing")
	}
	if calls != 1 {
		t.Fatalf("requests inside the transient backoff = %d, want 1", calls)
	}
	// A transient failure must retry far sooner than an auth failure.
	clock = clock.Add(2 * time.Second)
	retried := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &retried); err == nil {
		t.Fatal("expected the retried failure to surface")
	}
	if calls != 2 {
		t.Fatalf("requests past the transient backoff = %d, want 2", calls)
	}
	if dailyCreditUsageErrorBackoff >= dailyCreditUsageAuthBackoff {
		t.Fatal("a transient failure must retry sooner than a rejected token")
	}
}

func TestFetchDailyCreditUsageDoesNotClaimAccountHistoryWithoutRows(t *testing.T) {
	fx := newDailyCreditUsageFixture(t, "", "")
	calls := 0
	fx.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[]}`)
	})

	p := New()
	p.HTTPClient = fx.server.Client()
	clock := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	p.setCreditDailyClock(func() time.Time { return clock })

	snap := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &snap); err != nil {
		t.Fatalf("an empty period is not an error: %v", err)
	}
	if points, ok := snap.DailySeries[codexCreditUsageDailySeriesKey]; ok {
		t.Fatalf("expected no daily series from a rowless response, got %+v", points)
	}
	if snap.Raw["credit_daily_usage_source"] == "account" || snap.Raw["credit_daily_usage_complete"] == "true" {
		t.Fatalf("a rowless response must not claim authoritative history: %+v", snap.Raw)
	}
	if snap.Diagnostics["credit_daily_usage"] == "" {
		t.Fatal("expected a diagnostic explaining the missing daily history")
	}

	// The forecast must fall back rather than badging the guess as account data.
	p.applyCreditForecast(&snap, "t")
	if snap.Raw["credit_forecast_source"] == "account_daily_history" {
		t.Fatalf("forecast claimed account history without any: %+v", snap.Raw)
	}

	// A valid-but-empty response is still a valid response: do not re-request it.
	clock = clock.Add(time.Minute)
	again := fx.newSnap()
	if err := p.fetchDailyCreditUsage(context.Background(), fx.account, "", &again); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("rowless responses should still be cached, got %d requests", calls)
	}
}

func TestFetchSendsVersionedUserAgentToLiveEndpoints(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"tok","account_id":"acct"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "version.json"), []byte(`{"latest_version":"0.98.0"}`), 0600); err != nil {
		t.Fatal(err)
	}

	var agents []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents = append(agents, r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "daily-workspace-user-credit-usage") {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		fmt.Fprintf(w, `{"rate_limits":{"individual_limit":{"limit":7500,"used":2500,"resets_at":%d}}}`,
			time.Now().Add(72*time.Hour).Unix())
	}))
	defer server.Close()

	p := New()
	p.HTTPClient = server.Client()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "codex-ua",
		Provider: "codex",
		RuntimeHints: map[string]string{
			"config_dir":       tmpDir,
			"auth_file":        authPath,
			"chatgpt_base_url": server.URL + "/backend-api",
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Raw["cli_version"] != "0.98.0" {
		t.Fatalf("cli_version = %q, want 0.98.0", snap.Raw["cli_version"])
	}
	if len(agents) == 0 {
		t.Fatal("expected the live endpoints to be called")
	}
	for i, agent := range agents {
		if agent != "codex-cli/0.98.0" {
			t.Errorf("request %d User-Agent = %q, want codex-cli/0.98.0", i, agent)
		}
	}
}
