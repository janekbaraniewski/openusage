package openrouter

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetch_PeriodCosts(t *testing.T) {
	now := time.Now().UTC()
	today := now.Format(time.RFC3339)
	threeDaysAgo := now.AddDate(0, 0, -3).Format(time.RFC3339)
	tenDaysAgo := now.AddDate(0, 0, -10).Format(time.RFC3339)
	twentyDaysAgo := now.AddDate(0, 0, -20).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":10.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":10.0,"remaining_balance":90.0}}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			data := fmt.Sprintf(`{"data":[
				{"id":"gen-1","model":"anthropic/claude-3.5-sonnet","total_cost":0.50,"tokens_prompt":1000,"tokens_completion":500,"created_at":"%s","provider_name":"Anthropic"},
				{"id":"gen-2","model":"openai/gpt-4o","total_cost":0.30,"tokens_prompt":800,"tokens_completion":400,"created_at":"%s","provider_name":"OpenAI"},
				{"id":"gen-3","model":"anthropic/claude-3.5-sonnet","total_cost":1.00,"tokens_prompt":2000,"tokens_completion":1000,"created_at":"%s","provider_name":"Anthropic"},
				{"id":"gen-4","model":"openai/gpt-4o","total_cost":0.20,"tokens_prompt":500,"tokens_completion":200,"created_at":"%s","provider_name":"OpenAI"}
			]}`, today, threeDaysAgo, tenDaysAgo, twentyDaysAgo)
			w.Write([]byte(data))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_PERIOD", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_PERIOD")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-period",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_PERIOD",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}

	// 7d cost: today (0.50) + 3 days ago (0.30) = 0.80
	cost7d, ok := snap.Metrics["7d_api_cost"]
	if !ok {
		t.Fatal("missing 7d_api_cost metric")
	}
	if cost7d.Used == nil || math.Abs(*cost7d.Used-0.80) > 0.001 {
		t.Errorf("7d_api_cost = %v, want 0.80", cost7d.Used)
	}

	// 30d cost: all four = 0.50 + 0.30 + 1.00 + 0.20 = 2.00
	cost30d, ok := snap.Metrics["30d_api_cost"]
	if !ok {
		t.Fatal("missing 30d_api_cost metric")
	}
	if cost30d.Used == nil || math.Abs(*cost30d.Used-2.00) > 0.001 {
		t.Errorf("30d_api_cost = %v, want 2.00", cost30d.Used)
	}

	// DailySeries["cost"] should have entries for each unique date
	costSeries, ok := snap.DailySeries["cost"]
	if !ok {
		t.Fatal("missing cost in DailySeries")
	}
	if len(costSeries) < 3 {
		t.Errorf("cost DailySeries has %d entries, want at least 3 distinct days", len(costSeries))
	}

	// DailySeries["requests"] should exist
	reqSeries, ok := snap.DailySeries["requests"]
	if !ok {
		t.Fatal("missing requests in DailySeries")
	}
	// Total requests across all days should sum to 4
	var totalReqs float64
	for _, pt := range reqSeries {
		totalReqs += pt.Value
	}
	if math.Abs(totalReqs-4) > 0.001 {
		t.Errorf("total requests in DailySeries = %v, want 4", totalReqs)
	}

	// Per-model token series should exist for the top models
	if _, ok := snap.DailySeries["tokens_anthropic_claude-3.5-sonnet"]; !ok {
		t.Error("missing tokens_anthropic_claude-3.5-sonnet in DailySeries")
	}
	if _, ok := snap.DailySeries["tokens_openai_gpt-4o"]; !ok {
		t.Error("missing tokens_openai_gpt-4o in DailySeries")
	}
}

func TestFetch_BurnRate(t *testing.T) {
	now := time.Now().UTC()
	// All generations within the last 60 minutes
	tenMinAgo := now.Add(-10 * time.Minute).Format(time.RFC3339)
	thirtyMinAgo := now.Add(-30 * time.Minute).Format(time.RFC3339)
	fiftyMinAgo := now.Add(-50 * time.Minute).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":5.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":5.0,"remaining_balance":95.0}}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			data := fmt.Sprintf(`{"data":[
				{"id":"gen-1","model":"anthropic/claude-3.5-sonnet","total_cost":0.10,"tokens_prompt":500,"tokens_completion":200,"created_at":"%s","provider_name":"Anthropic"},
				{"id":"gen-2","model":"anthropic/claude-3.5-sonnet","total_cost":0.20,"tokens_prompt":1000,"tokens_completion":400,"created_at":"%s","provider_name":"Anthropic"},
				{"id":"gen-3","model":"openai/gpt-4o","total_cost":0.30,"tokens_prompt":1500,"tokens_completion":600,"created_at":"%s","provider_name":"OpenAI"}
			]}`, tenMinAgo, thirtyMinAgo, fiftyMinAgo)
			w.Write([]byte(data))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_BURN", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_BURN")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-burn",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_BURN",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}

	// Burn rate: total cost in last 60 min = 0.10 + 0.20 + 0.30 = 0.60 USD/hour
	burnRate, ok := snap.Metrics["burn_rate"]
	if !ok {
		t.Fatal("missing burn_rate metric")
	}
	expectedBurn := 0.60
	if burnRate.Used == nil || math.Abs(*burnRate.Used-expectedBurn) > 0.001 {
		t.Errorf("burn_rate = %v, want %v", burnRate.Used, expectedBurn)
	}
	if burnRate.Unit != "USD/hour" {
		t.Errorf("burn_rate unit = %q, want USD/hour", burnRate.Unit)
	}

	// Daily projected: 0.60 * 24 = 14.40
	dailyProj, ok := snap.Metrics["daily_projected"]
	if !ok {
		t.Fatal("missing daily_projected metric")
	}
	expectedProj := 14.40
	if dailyProj.Used == nil || math.Abs(*dailyProj.Used-expectedProj) > 0.01 {
		t.Errorf("daily_projected = %v, want %v", dailyProj.Used, expectedProj)
	}
}

func TestFetch_AnalyticsGracefulDegradation(t *testing.T) {
	now := todayISO()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":5.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":5.0,"remaining_balance":95.0}}`))
		case "/analytics/user-activity":
			// Return 404 to simulate analytics not available
			w.WriteHeader(http.StatusNotFound)
		case "/generation":
			w.WriteHeader(http.StatusOK)
			data := fmt.Sprintf(`{"data":[
				{"id":"gen-1","model":"openai/gpt-4o","total_cost":0.05,"tokens_prompt":500,"tokens_completion":200,"created_at":"%s","provider_name":"OpenAI"}
			]}`, now)
			w.Write([]byte(data))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_GRACEFUL", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_GRACEFUL")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-graceful",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_GRACEFUL",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	// Status should still be OK despite analytics failure
	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK; message=%s", snap.Status, snap.Message)
	}

	// Analytics error should be logged
	analyticsErr, ok := snap.Raw["analytics_error"]
	if !ok {
		t.Error("expected analytics_error in Raw")
	}
	if !strings.Contains(analyticsErr, "404") {
		t.Errorf("analytics_error = %q, want to contain '404'", analyticsErr)
	}

	// Generation data should still be processed
	if snap.Raw["generations_fetched"] != "1" {
		t.Errorf("generations_fetched = %q, want 1", snap.Raw["generations_fetched"])
	}

	// Metrics from credits and generations should still work
	if _, ok := snap.Metrics["credits"]; !ok {
		t.Error("missing credits metric")
	}
	if _, ok := snap.Metrics["today_requests"]; !ok {
		t.Error("missing today_requests metric")
	}

	// DailySeries from generations should still be populated
	if _, ok := snap.DailySeries["cost"]; !ok {
		t.Error("missing cost in DailySeries despite analytics failure")
	}
}

func TestFetch_DateBasedCutoff(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-1 * time.Hour).Format(time.RFC3339)
	fiveDaysAgo := now.AddDate(0, 0, -5).Format(time.RFC3339)
	// 35 days ago: beyond the 30-day cutoff
	thirtyFiveDaysAgo := now.AddDate(0, 0, -35).Format(time.RFC3339)

	generationRequests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":5.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":5.0,"remaining_balance":95.0}}`))
		case "/generation":
			generationRequests++
			w.WriteHeader(http.StatusOK)
			if generationRequests == 1 {
				// First page: 2 recent + 1 old (beyond 30 day cutoff)
				data := fmt.Sprintf(`{"data":[
					{"id":"gen-1","model":"openai/gpt-4o","total_cost":0.10,"tokens_prompt":500,"tokens_completion":200,"created_at":"%s","provider_name":"OpenAI"},
					{"id":"gen-2","model":"openai/gpt-4o","total_cost":0.20,"tokens_prompt":1000,"tokens_completion":400,"created_at":"%s","provider_name":"OpenAI"},
					{"id":"gen-3","model":"openai/gpt-4o","total_cost":0.50,"tokens_prompt":2000,"tokens_completion":800,"created_at":"%s","provider_name":"OpenAI"}
				]}`, recent, fiveDaysAgo, thirtyFiveDaysAgo)
				w.Write([]byte(data))
			} else {
				// Should not reach here due to date cutoff
				w.Write([]byte(`{"data":[
					{"id":"gen-old","model":"openai/gpt-4o","total_cost":999.0,"tokens_prompt":99999,"tokens_completion":99999,"created_at":"2025-01-01T00:00:00Z","provider_name":"OpenAI"}
				]}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_CUTOFF", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_CUTOFF")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-cutoff",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_CUTOFF",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}

	// Only 2 generations should be fetched (the old one is beyond cutoff)
	if snap.Raw["generations_fetched"] != "2" {
		t.Errorf("generations_fetched = %q, want 2 (old generation should be excluded)", snap.Raw["generations_fetched"])
	}

	// 30d cost should only include the 2 recent generations: 0.10 + 0.20 = 0.30
	cost30d, ok := snap.Metrics["30d_api_cost"]
	if !ok {
		t.Fatal("missing 30d_api_cost metric")
	}
	if cost30d.Used == nil || math.Abs(*cost30d.Used-0.30) > 0.001 {
		t.Errorf("30d_api_cost = %v, want 0.30 (should not include generation beyond 30 days)", cost30d.Used)
	}

	// Should only have made 1 generation request (stopped due to date cutoff)
	if generationRequests != 1 {
		t.Errorf("generation API requests = %d, want 1 (should stop on date cutoff)", generationRequests)
	}
}

func TestFetch_CurrentKeyRichData(t *testing.T) {
	limitReset := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	expiresAt := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":{
				"label":"mgmt-key",
				"usage":12.5,
				"limit":50.0,
				"limit_remaining":37.5,
				"usage_daily":1.25,
				"usage_weekly":6.5,
				"usage_monthly":12.5,
				"byok_usage":3.0,
				"byok_usage_inference":0.2,
				"byok_usage_daily":0.2,
				"byok_usage_weekly":0.9,
				"byok_usage_monthly":3.0,
				"is_free_tier":false,
				"is_management_key":true,
				"is_provisioning_key":false,
				"include_byok_in_limit":true,
				"limit_reset":"%s",
				"expires_at":"%s",
				"rate_limit":{"requests":240,"interval":"10s","note":"model-dependent"}
			}}`, limitReset, expiresAt)))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":50.0,"total_usage":12.5}}`))
		case "/activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_RICH", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_RICH")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-rich-key",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_RICH",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK", snap.Status)
	}

	checkMetric := func(name string, want float64) {
		t.Helper()
		m, ok := snap.Metrics[name]
		if !ok || m.Used == nil {
			t.Fatalf("missing metric %s", name)
		}
		if math.Abs(*m.Used-want) > 0.0001 {
			t.Fatalf("%s = %v, want %v", name, *m.Used, want)
		}
	}

	checkMetric("usage_daily", 1.25)
	checkMetric("usage_weekly", 6.5)
	checkMetric("usage_monthly", 12.5)
	checkMetric("byok_usage", 3.0)
	checkMetric("byok_daily", 0.2)
	checkMetric("byok_weekly", 0.9)
	checkMetric("byok_monthly", 3.0)
	checkMetric("limit_remaining", 37.5)

	if got := snap.Raw["key_type"]; got != "management" {
		t.Fatalf("key_type = %q, want management", got)
	}
	if got := snap.Raw["rate_limit_note"]; got != "model-dependent" {
		t.Fatalf("rate_limit_note = %q, want model-dependent", got)
	}
	if _, ok := snap.Resets["limit_reset"]; !ok {
		t.Fatal("missing limit_reset in Resets")
	}
	if _, ok := snap.Resets["key_expires"]; !ok {
		t.Fatal("missing key_expires in Resets")
	}
}

func TestFetch_ManagementKeyLoadsKeysMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{
				"label":"sk-or-v1-mgr...abc",
				"usage":1.0,
				"limit":50.0,
				"is_free_tier":false,
				"is_management_key":true,
				"is_provisioning_key":true,
				"rate_limit":{"requests":240,"interval":"10s","note":"deprecated"}
			}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":50.0,"total_usage":1.0}}`))
		case "/keys":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[
				{"hash":"1234567890abcdef","name":"Primary","label":"sk-or-v1-mgr...abc","disabled":false,"limit":50.0,"limit_remaining":49.0,"limit_reset":null,"include_byok_in_limit":false,"usage":1.0,"usage_daily":0.1,"usage_weekly":0.2,"usage_monthly":1.0,"byok_usage":0.0,"byok_usage_daily":0.0,"byok_usage_weekly":0.0,"byok_usage_monthly":0.0,"created_at":"2026-02-20T10:00:00Z","updated_at":"2026-02-20T10:30:00Z","expires_at":null},
				{"hash":"abcdef0123456789","name":"Secondary","label":"sk-or-v1-secondary","disabled":true,"limit":null,"limit_remaining":null,"limit_reset":null,"include_byok_in_limit":false,"usage":0.0,"usage_daily":0.0,"usage_weekly":0.0,"usage_monthly":0.0,"byok_usage":0.0,"byok_usage_daily":0.0,"byok_usage_weekly":0.0,"byok_usage_monthly":0.0,"created_at":"2026-02-19T10:00:00Z","updated_at":null,"expires_at":null}
			]}`))
		case "/activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_KEYS_META", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_KEYS_META")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-keys-meta",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_KEYS_META",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := snap.Raw["keys_total"]; got != "2" {
		t.Fatalf("keys_total = %q, want 2", got)
	}
	if got := snap.Raw["keys_active"]; got != "1" {
		t.Fatalf("keys_active = %q, want 1", got)
	}
	if got := snap.Raw["keys_disabled"]; got != "1" {
		t.Fatalf("keys_disabled = %q, want 1", got)
	}
	if got := snap.Raw["key_name"]; got != "Primary" {
		t.Fatalf("key_name = %q, want Primary", got)
	}
	if got := snap.Raw["key_disabled"]; got != "false" {
		t.Fatalf("key_disabled = %q, want false", got)
	}
	if got := snap.Raw["key_created_at"]; got == "" {
		t.Fatal("expected key_created_at")
	}

	if total := snap.Metrics["keys_total"]; total.Used == nil || *total.Used != 2 {
		t.Fatalf("keys_total metric = %v, want 2", total.Used)
	}
	if active := snap.Metrics["keys_active"]; active.Used == nil || *active.Used != 1 {
		t.Fatalf("keys_active metric = %v, want 1", active.Used)
	}
	if disabled := snap.Metrics["keys_disabled"]; disabled.Used == nil || *disabled.Used != 1 {
		t.Fatalf("keys_disabled metric = %v, want 1", disabled.Used)
	}
}
