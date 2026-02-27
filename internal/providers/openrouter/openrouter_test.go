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

func todayISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func TestFetch_ParsesCredits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"data": {
					"label": "test-key",
					"usage": 5.25,
					"limit": 100.00,
					"is_free_tier": false,
					"rate_limit": {
						"requests": 200,
						"interval": "10s"
					}
				}
			}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"data": {
					"total_credits": 100.0,
					"total_usage": 5.25,
					"remaining_balance": 94.75
				}
			}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": []}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OPENROUTER_KEY", "test-key")
	defer os.Unsetenv("TEST_OPENROUTER_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-openrouter",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OPENROUTER_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}

	credits, ok := snap.Metrics["credits"]
	if !ok {
		t.Fatal("missing credits metric")
	}
	if credits.Limit == nil || *credits.Limit != 100.00 {
		t.Errorf("credits limit = %v, want 100", credits.Limit)
	}
	if credits.Used == nil || *credits.Used != 5.25 {
		t.Errorf("credits used = %v, want 5.25", credits.Used)
	}
	if credits.Remaining == nil || *credits.Remaining != 94.75 {
		t.Errorf("credits remaining = %v, want 94.75", credits.Remaining)
	}

	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 200 {
		t.Errorf("rpm limit = %v, want 200", rpm.Limit)
	}

	balance, ok := snap.Metrics["credit_balance"]
	if !ok {
		t.Fatal("missing credit_balance metric")
	}
	if balance.Remaining == nil || *balance.Remaining != 94.75 {
		t.Errorf("credit_balance remaining = %v, want 94.75", balance.Remaining)
	}
}

func TestFetch_TokenAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer direct-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"direct","usage":1.0,"limit":50.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p := New()
	acct := core.AccountConfig{
		ID:      "test-token",
		Token:   "direct-token",
		BaseURL: server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}
}

func TestFetch_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_BAD", "bad-key")
	defer os.Unsetenv("TEST_OR_KEY_BAD")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-unauth",
		APIKeyEnv: "TEST_OR_KEY_BAD",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want auth", snap.Status)
	}
}

func TestFetch_PerModelBreakdown(t *testing.T) {
	now := todayISO()

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
				{
					"id":"gen-1",
					"model":"anthropic/claude-3.5-sonnet",
					"total_cost":0.003,
					"tokens_prompt":1000,
					"tokens_completion":500,
					"created_at":"%s",
					"provider_name":"Anthropic",
					"latency":2500,
					"streamed":true,
					"origin":"api"
				},
				{
					"id":"gen-2",
					"model":"anthropic/claude-3.5-sonnet",
					"total_cost":0.005,
					"tokens_prompt":2000,
					"tokens_completion":800,
					"created_at":"%s",
					"provider_name":"Anthropic",
					"latency":3000,
					"cache_discount":0.001,
					"streamed":true,
					"origin":"api"
				},
				{
					"id":"gen-3",
					"model":"openai/gpt-4o",
					"total_cost":0.010,
					"tokens_prompt":3000,
					"tokens_completion":1000,
					"created_at":"%s",
					"provider_name":"OpenAI",
					"latency":1500,
					"streamed":false,
					"origin":"api"
				}
			]}`, now, now, now)
			w.Write([]byte(data))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_MODELS", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_MODELS")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-models",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_MODELS",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK; message=%s", snap.Status, snap.Message)
	}

	todayReqs, ok := snap.Metrics["today_requests"]
	if !ok {
		t.Fatal("missing today_requests metric")
	}
	if todayReqs.Used == nil || *todayReqs.Used != 3 {
		t.Errorf("today_requests = %v, want 3", todayReqs.Used)
	}

	todayInputTokens, ok := snap.Metrics["today_input_tokens"]
	if !ok {
		t.Fatal("missing today_input_tokens metric")
	}
	if todayInputTokens.Used == nil || *todayInputTokens.Used != 6000 {
		t.Errorf("today_input_tokens = %v, want 6000", todayInputTokens.Used)
	}

	todayOutputTokens, ok := snap.Metrics["today_output_tokens"]
	if !ok {
		t.Fatal("missing today_output_tokens metric")
	}
	if todayOutputTokens.Used == nil || *todayOutputTokens.Used != 2300 {
		t.Errorf("today_output_tokens = %v, want 2300", todayOutputTokens.Used)
	}

	todayCost, ok := snap.Metrics["today_cost"]
	if !ok {
		t.Fatal("missing today_cost metric")
	}
	expectedCost := 0.018 // 0.003 + 0.005 + 0.010
	if todayCost.Used == nil || (*todayCost.Used-expectedCost) > 0.0001 {
		t.Errorf("today_cost = %v, want ~%v", todayCost.Used, expectedCost)
	}

	todayLatency, ok := snap.Metrics["today_avg_latency"]
	if !ok {
		t.Fatal("missing today_avg_latency metric")
	}
	expectedAvgLatency := float64(2500+3000+1500) / 3.0 / 1000.0 // seconds
	if todayLatency.Used == nil || (*todayLatency.Used-expectedAvgLatency) > 0.01 {
		t.Errorf("today_avg_latency = %v, want ~%v", todayLatency.Used, expectedAvgLatency)
	}

	claudeInput, ok := snap.Metrics["model_anthropic_claude-3.5-sonnet_input_tokens"]
	if !ok {
		t.Fatal("missing model_anthropic_claude-3.5-sonnet_input_tokens metric")
	}
	if claudeInput.Used == nil || *claudeInput.Used != 3000 {
		t.Errorf("claude input tokens = %v, want 3000", claudeInput.Used)
	}

	claudeOutput, ok := snap.Metrics["model_anthropic_claude-3.5-sonnet_output_tokens"]
	if !ok {
		t.Fatal("missing model_anthropic_claude-3.5-sonnet_output_tokens metric")
	}
	if claudeOutput.Used == nil || *claudeOutput.Used != 1300 {
		t.Errorf("claude output tokens = %v, want 1300", claudeOutput.Used)
	}

	claudeCost, ok := snap.Metrics["model_anthropic_claude-3.5-sonnet_cost_usd"]
	if !ok {
		t.Fatal("missing model_anthropic_claude-3.5-sonnet_cost_usd metric")
	}
	expectedClaudeCost := 0.008
	if claudeCost.Used == nil || (*claudeCost.Used-expectedClaudeCost) > 0.0001 {
		t.Errorf("claude cost = %v, want ~%v", claudeCost.Used, expectedClaudeCost)
	}

	gptInput, ok := snap.Metrics["model_openai_gpt-4o_input_tokens"]
	if !ok {
		t.Fatal("missing model_openai_gpt-4o_input_tokens metric")
	}
	if gptInput.Used == nil || *gptInput.Used != 3000 {
		t.Errorf("gpt-4o input tokens = %v, want 3000", gptInput.Used)
	}

	gptCost, ok := snap.Metrics["model_openai_gpt-4o_cost_usd"]
	if !ok {
		t.Fatal("missing model_openai_gpt-4o_cost_usd metric")
	}
	if gptCost.Used == nil || *gptCost.Used != 0.010 {
		t.Errorf("gpt-4o cost = %v, want 0.010", gptCost.Used)
	}

	if got := snap.Raw["model_anthropic_claude-3.5-sonnet_requests"]; got != "2" {
		t.Errorf("claude requests in raw = %q, want 2", got)
	}
	if got := snap.Raw["model_anthropic_claude-3.5-sonnet_providers"]; got != "Anthropic" {
		t.Errorf("claude providers in raw = %q, want 'Anthropic'", got)
	}

	if got := snap.Raw["provider_anthropic_requests"]; got != "2" {
		t.Errorf("provider anthropic requests = %q, want 2", got)
	}
	if got := snap.Raw["provider_openai_requests"]; got != "1" {
		t.Errorf("provider openai requests = %q, want 1", got)
	}

	clientAnthropic, ok := snap.Metrics["client_anthropic_total_tokens"]
	if !ok || clientAnthropic.Used == nil {
		t.Fatal("missing client_anthropic_total_tokens metric")
	}
	if *clientAnthropic.Used != 4300 {
		t.Errorf("client_anthropic_total_tokens = %v, want 4300", *clientAnthropic.Used)
	}
	clientOpenAIReq, ok := snap.Metrics["client_openai_requests"]
	if !ok || clientOpenAIReq.Used == nil {
		t.Fatal("missing client_openai_requests metric")
	}
	if *clientOpenAIReq.Used != 1 {
		t.Errorf("client_openai_requests = %v, want 1", *clientOpenAIReq.Used)
	}
	langGeneral, ok := snap.Metrics["lang_general"]
	if !ok || langGeneral.Used == nil {
		t.Fatal("missing lang_general metric")
	}
	if *langGeneral.Used != 3 {
		t.Errorf("lang_general = %v, want 3", *langGeneral.Used)
	}
	if got := snap.Raw["language_usage_source"]; got != "inferred_from_model_ids" {
		t.Errorf("language_usage_source = %q, want inferred_from_model_ids", got)
	}
	if got := snap.Raw["client_usage"]; !strings.Contains(got, "anthropic") {
		t.Errorf("client_usage = %q, expected anthropic share", got)
	}
	if got := snap.Raw["model_usage"]; !strings.Contains(got, "anthropic claude-3.5-sonnet") {
		t.Errorf("model_usage = %q, expected model summary", got)
	}
	if series, ok := snap.DailySeries["tokens_client_anthropic"]; !ok || len(series) == 0 {
		t.Errorf("missing tokens_client_anthropic daily series")
	}
	if series, ok := snap.DailySeries["usage_client_openai"]; !ok || len(series) == 0 {
		t.Errorf("missing usage_client_openai daily series")
	}
}

func TestFetch_RateLimitHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/key":
			w.Header().Set("x-ratelimit-limit-requests", "200")
			w.Header().Set("x-ratelimit-remaining-requests", "150")
			w.Header().Set("x-ratelimit-reset-requests", "30s")
			w.Header().Set("x-ratelimit-limit-tokens", "40000")
			w.Header().Set("x-ratelimit-remaining-tokens", "35000")
			w.Header().Set("x-ratelimit-reset-tokens", "30s")

			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"rl-test","usage":1.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_RL", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_RL")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-ratelimit",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_RL",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	rpmHeaders, ok := snap.Metrics["rpm_headers"]
	if !ok {
		t.Fatal("missing rpm_headers metric")
	}
	if rpmHeaders.Limit == nil || *rpmHeaders.Limit != 200 {
		t.Errorf("rpm_headers limit = %v, want 200", rpmHeaders.Limit)
	}
	if rpmHeaders.Remaining == nil || *rpmHeaders.Remaining != 150 {
		t.Errorf("rpm_headers remaining = %v, want 150", rpmHeaders.Remaining)
	}

	tpmHeaders, ok := snap.Metrics["tpm_headers"]
	if !ok {
		t.Fatal("missing tpm_headers metric")
	}
	if tpmHeaders.Limit == nil || *tpmHeaders.Limit != 40000 {
		t.Errorf("tpm_headers limit = %v, want 40000", tpmHeaders.Limit)
	}
	if tpmHeaders.Remaining == nil || *tpmHeaders.Remaining != 35000 {
		t.Errorf("tpm_headers remaining = %v, want 35000", tpmHeaders.Remaining)
	}

	if _, ok := snap.Resets["rpm_headers_reset"]; !ok {
		t.Error("missing rpm_headers_reset in Resets")
	}
	if _, ok := snap.Resets["tpm_headers_reset"]; !ok {
		t.Error("missing tpm_headers_reset in Resets")
	}
}

func TestFetch_Pagination(t *testing.T) {
	page := 0
	now := todayISO()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":1.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/generation":
			page++
			if page == 1 {
				data := fmt.Sprintf(`{"data":[
					{"id":"gen-1","model":"openai/gpt-4o","total_cost":0.01,"tokens_prompt":100,"tokens_completion":50,"created_at":"%s","provider_name":"OpenAI"},
					{"id":"gen-2","model":"openai/gpt-4o","total_cost":0.01,"tokens_prompt":100,"tokens_completion":50,"created_at":"%s","provider_name":"OpenAI"},
					{"id":"gen-3","model":"openai/gpt-4o","total_cost":0.01,"tokens_prompt":100,"tokens_completion":50,"created_at":"%s","provider_name":"OpenAI"}
				]}`, now, now, now)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(data))
			} else {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"data":[]}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_PAGE", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_PAGE")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-pagination",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_PAGE",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Raw["generations_fetched"] != "3" {
		t.Errorf("generations_fetched = %q, want 3", snap.Raw["generations_fetched"])
	}

	reqs, ok := snap.Metrics["today_requests"]
	if !ok {
		t.Fatal("missing today_requests")
	}
	if reqs.Used == nil || *reqs.Used != 3 {
		t.Errorf("today_requests = %v, want 3", reqs.Used)
	}
}

func TestSanitizeModelName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"anthropic/claude-3.5-sonnet", "anthropic_claude-3.5-sonnet"},
		{"openai/gpt-4o", "openai_gpt-4o"},
		{"simple-model", "simple-model"},
		{"google/gemini-2.5-pro", "google_gemini-2.5-pro"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeProviderName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Anthropic", "Anthropic"},
		{"OpenAI", "OpenAI"},
		{"Google AI Studio", "Google_AI_Studio"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFetch_FreeTier(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"free-key","usage":0.0,"limit":null,"is_free_tier":true,"rate_limit":{"requests":20,"interval":"10s"}}}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_FREE", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_FREE")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-free",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_FREE",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}

	if snap.Raw["tier"] != "free" {
		t.Errorf("tier = %q, want free", snap.Raw["tier"])
	}

	credits, ok := snap.Metrics["credits"]
	if !ok {
		t.Fatal("missing credits metric")
	}
	if credits.Limit != nil {
		t.Errorf("credits limit = %v, want nil (unlimited)", credits.Limit)
	}

	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 20 {
		t.Errorf("rpm limit = %v, want 20", rpm.Limit)
	}

	if !strings.Contains(snap.Message, "$0.0000") {
		t.Errorf("message = %q, want to contain $0.0000", snap.Message)
	}
}

func TestFetch_AnalyticsEndpoint(t *testing.T) {
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
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[
				{"date":"2026-02-18","model":"anthropic/claude-3.5-sonnet","total_cost":1.50,"total_tokens":50000,"requests":20},
				{"date":"2026-02-19","model":"anthropic/claude-3.5-sonnet","total_cost":2.00,"total_tokens":70000,"requests":30},
				{"date":"2026-02-19","model":"openai/gpt-4o","total_cost":0.50,"total_tokens":10000,"requests":5},
				{"date":"2026-02-20","model":"anthropic/claude-3.5-sonnet","total_cost":0.75,"total_tokens":25000,"requests":10}
			]}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			data := fmt.Sprintf(`{"data":[
				{"id":"gen-1","model":"anthropic/claude-3.5-sonnet","total_cost":0.01,"tokens_prompt":500,"tokens_completion":200,"created_at":"%s","provider_name":"Anthropic"}
			]}`, now)
			w.Write([]byte(data))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_ANALYTICS", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_ANALYTICS")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-analytics",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_ANALYTICS",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK; message=%s", snap.Status, snap.Message)
	}

	if snap.DailySeries == nil {
		t.Fatal("DailySeries is nil")
	}

	analyticsCost, ok := snap.DailySeries["analytics_cost"]
	if !ok {
		t.Fatal("missing analytics_cost in DailySeries")
	}
	if len(analyticsCost) != 3 {
		t.Fatalf("analytics_cost has %d entries, want 3", len(analyticsCost))
	}
	// Verify sorted by date
	if analyticsCost[0].Date != "2026-02-18" {
		t.Errorf("analytics_cost[0].Date = %q, want 2026-02-18", analyticsCost[0].Date)
	}
	// 2026-02-19 has two entries summed: 2.00 + 0.50 = 2.50
	if math.Abs(analyticsCost[1].Value-2.50) > 0.001 {
		t.Errorf("analytics_cost[1].Value = %v, want 2.50", analyticsCost[1].Value)
	}

	analyticsTokens, ok := snap.DailySeries["analytics_tokens"]
	if !ok {
		t.Fatal("missing analytics_tokens in DailySeries")
	}
	if len(analyticsTokens) != 3 {
		t.Fatalf("analytics_tokens has %d entries, want 3", len(analyticsTokens))
	}
	// 2026-02-19: 70000 + 10000 = 80000
	if math.Abs(analyticsTokens[1].Value-80000) > 0.1 {
		t.Errorf("analytics_tokens[1].Value = %v, want 80000", analyticsTokens[1].Value)
	}

	// Verify no analytics_error in Raw
	if _, hasErr := snap.Raw["analytics_error"]; hasErr {
		t.Errorf("unexpected analytics_error: %s", snap.Raw["analytics_error"])
	}
}

func TestFetch_AnalyticsTotalTokensOnly_TracksModelAndNormalizesName(t *testing.T) {
	now := todayISO()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":1.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":1.0,"remaining_balance":99.0}}`))
		case "/analytics/user-activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[
				{"date":"2026-02-20","model":"Qwen/Qwen3-Coder-Flash","total_cost":0.0,"total_tokens":4000,"requests":1},
				{"date":"2026-02-21","model":"qwen/qwen3-coder-flash","total_cost":0.0,"total_tokens":8000,"requests":1}
			]}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{"id":"gen-1","model":"openai/gpt-4o","total_cost":0.001,"tokens_prompt":10,"tokens_completion":5,"created_at":"%s","provider_name":"OpenAI"}
			]}`, now)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_ANALYTICS_TOTAL_ONLY", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_ANALYTICS_TOTAL_ONLY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-analytics-total-only",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_ANALYTICS_TOTAL_ONLY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK; message=%s", snap.Status, snap.Message)
	}

	tok, ok := snap.Metrics["model_qwen_qwen3-coder-flash_total_tokens"]
	if !ok {
		t.Fatal("missing normalized qwen total tokens metric")
	}
	if tok.Used == nil || *tok.Used != 12000 {
		t.Fatalf("model_qwen_qwen3-coder-flash_total_tokens = %v, want 12000", tok.Used)
	}

	reqs, ok := snap.Metrics["model_qwen_qwen3-coder-flash_requests"]
	if !ok {
		t.Fatal("missing normalized qwen requests metric")
	}
	if reqs.Used == nil || *reqs.Used != 2 {
		t.Fatalf("model_qwen_qwen3-coder-flash_requests = %v, want 2", reqs.Used)
	}

	if _, ok := snap.Metrics["model_Qwen_Qwen3-Coder-Flash_total_tokens"]; ok {
		t.Fatal("unexpected unnormalized model metric key present")
	}

	foundQwenRecord := false
	for _, rec := range snap.ModelUsage {
		if rec.RawModelID != "qwen/qwen3-coder-flash" {
			continue
		}
		foundQwenRecord = true
		if rec.TotalTokens == nil || *rec.TotalTokens != 12000 {
			t.Fatalf("qwen model_usage total_tokens = %v, want 12000", rec.TotalTokens)
		}
		if rec.Requests == nil || *rec.Requests != 2 {
			t.Fatalf("qwen model_usage requests = %v, want 2", rec.Requests)
		}
	}
	if !foundQwenRecord {
		t.Fatal("expected normalized qwen model_usage record")
	}

	if m, ok := snap.Metrics["lang_code"]; !ok || m.Used == nil || *m.Used != 2 {
		t.Fatalf("lang_code = %v, want 2", m.Used)
	}
	if m, ok := snap.Metrics["lang_general"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("lang_general = %v, want 1", m.Used)
	}
}

func TestFetch_GenerationPerModel_FallsBackTo30dWhenAnalyticsUnavailable(t *testing.T) {
	now := time.Now().UTC()
	today := now.Format(time.RFC3339)
	tenDaysAgo := now.AddDate(0, 0, -10).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":1.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":1.0,"remaining_balance":99.0}}`))
		case "/activity", "/analytics/user-activity":
			w.WriteHeader(http.StatusNotFound)
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{"id":"gen-1","model":"qwen/qwen3-coder-flash","total_cost":0.20,"tokens_prompt":1000,"tokens_completion":2000,"created_at":"%s","provider_name":"Novita"},
				{"id":"gen-2","model":"QWEN/QWEN3-CODER-FLASH","total_cost":0.30,"tokens_prompt":3000,"tokens_completion":4000,"created_at":"%s","provider_name":"Novita"}
			]}`, today, tenDaysAgo)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_GEN_30D", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_GEN_30D")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-gen-30d",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_GEN_30D",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	inp, ok := snap.Metrics["model_qwen_qwen3-coder-flash_input_tokens"]
	if !ok || inp.Used == nil {
		t.Fatalf("missing model_qwen_qwen3-coder-flash_input_tokens metric: %+v", inp)
	}
	if *inp.Used != 4000 {
		t.Fatalf("input tokens = %v, want 4000", *inp.Used)
	}
	if inp.Window != "30d" {
		t.Fatalf("input window = %q, want 30d", inp.Window)
	}

	out, ok := snap.Metrics["model_qwen_qwen3-coder-flash_output_tokens"]
	if !ok || out.Used == nil {
		t.Fatalf("missing model_qwen_qwen3-coder-flash_output_tokens metric: %+v", out)
	}
	if *out.Used != 6000 {
		t.Fatalf("output tokens = %v, want 6000", *out.Used)
	}

	reqs, ok := snap.Metrics["model_qwen_qwen3-coder-flash_requests"]
	if !ok || reqs.Used == nil {
		t.Fatalf("missing model_qwen_qwen3-coder-flash_requests metric: %+v", reqs)
	}
	if *reqs.Used != 2 {
		t.Fatalf("requests = %v, want 2", *reqs.Used)
	}
}

func TestFetch_AnalyticsRows_GenerationModelMixIsAuthoritative(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	today := time.Now().UTC().Format("2006-01-02")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":1.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":1.0,"remaining_balance":99.0}}`))
		case "/activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[
				{"date":"` + today + `","model":"qwen/qwen3-coder-flash","total_cost":0.0,"total_tokens":9000,"requests":3}
			]}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{"id":"gen-1","model":"qwen/qwen3-coder-flash","total_cost":0.2,"tokens_prompt":5000,"tokens_completion":5000,"created_at":"%s","provider_name":"Novita"}
			]}`, now)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_NO_DOUBLE", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_NO_DOUBLE")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-no-double",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_NO_DOUBLE",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	tok, ok := snap.Metrics["model_qwen_qwen3-coder-flash_total_tokens"]
	if !ok || tok.Used == nil {
		t.Fatalf("missing model total tokens metric: %+v", tok)
	}
	if *tok.Used != 10000 {
		t.Fatalf("total_tokens = %v, want 10000 (generation live)", *tok.Used)
	}

	inp, ok := snap.Metrics["model_qwen_qwen3-coder-flash_input_tokens"]
	if !ok || inp.Used == nil || *inp.Used != 5000 {
		t.Fatalf("model input tokens = %+v, want 5000 from generation", inp)
	}
	if got := snap.Raw["model_mix_source"]; got != "generation_live" {
		t.Fatalf("model_mix_source = %q, want generation_live", got)
	}
}

func TestFetch_AnalyticsCachedAt_GenerationLiveModelMix(t *testing.T) {
	now := time.Now().UTC()
	cachedAt := now.Add(-1 * time.Hour).Truncate(time.Second)
	afterCache := now.Add(-20 * time.Minute).Truncate(time.Second)
	beforeCache := now.Add(-2 * time.Hour).Truncate(time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":5.01,"limit":10.0,"usage_monthly":5.01,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":5.01,"remaining_balance":4.99}}`))
		case "/api/internal/v1/transaction-analytics":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":{"data":[
				{"date":"%s","model":"qwen/qwen3-coder-flash","total_cost":1.00,"total_tokens":1000,"requests":1}
			],"cachedAt":"%s"}}`, now.Format("2006-01-02"), cachedAt.Format(time.RFC3339))))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{"id":"gen-before","model":"qwen/qwen3-coder-flash","total_cost":0.50,"tokens_prompt":100,"tokens_completion":50,"created_at":"%s","provider_name":"Novita"},
				{"id":"gen-after","model":"qwen/qwen3-coder-flash","total_cost":0.25,"tokens_prompt":80,"tokens_completion":20,"created_at":"%s","provider_name":"Novita"}
			]}`, beforeCache.Format(time.RFC3339), afterCache.Format(time.RFC3339))))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_CACHE_DELTA", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_CACHE_DELTA")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-cache-delta",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_CACHE_DELTA",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	cost, ok := snap.Metrics["model_qwen_qwen3-coder-flash_cost_usd"]
	if !ok || cost.Used == nil {
		t.Fatalf("missing model cost metric: %+v", cost)
	}
	if math.Abs(*cost.Used-0.75) > 0.0001 {
		t.Fatalf("model cost = %v, want 0.75 (generation live)", *cost.Used)
	}

	reqs, ok := snap.Metrics["model_qwen_qwen3-coder-flash_requests"]
	if !ok || reqs.Used == nil {
		t.Fatalf("missing model requests metric: %+v", reqs)
	}
	if math.Abs(*reqs.Used-2.0) > 0.0001 {
		t.Fatalf("model requests = %v, want 2", *reqs.Used)
	}

	if got := snap.Raw["model_mix_source"]; got != "generation_live" {
		t.Fatalf("model_mix_source = %q, want generation_live", got)
	}
}

func TestFetch_AnalyticsMaxDate_GenerationLiveModelMix(t *testing.T) {
	now := time.Now().UTC()
	staleDay := now.AddDate(0, 0, -2).Format("2006-01-02")
	newerTs := now.Add(-30 * time.Minute).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":5.74,"limit":10.0,"usage_monthly":5.74,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":5.74,"remaining_balance":4.26}}`))
		case "/activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{"date":"%s","model":"qwen/qwen3-coder-flash","total_cost":1.00,"total_tokens":1000,"requests":1}
			]}`, staleDay)))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{"id":"gen-new","model":"qwen/qwen3-coder-flash","total_cost":0.40,"tokens_prompt":120,"tokens_completion":80,"created_at":"%s","provider_name":"Novita"}
			]}`, newerTs)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_MAXDATE_DELTA", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_MAXDATE_DELTA")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-maxdate-delta",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_MAXDATE_DELTA",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	cost, ok := snap.Metrics["model_qwen_qwen3-coder-flash_cost_usd"]
	if !ok || cost.Used == nil {
		t.Fatalf("missing model cost metric: %+v", cost)
	}
	if math.Abs(*cost.Used-0.40) > 0.0001 {
		t.Fatalf("model cost = %v, want 0.40 (generation live)", *cost.Used)
	}

	if got := snap.Raw["model_mix_source"]; got != "generation_live" {
		t.Fatalf("model_mix_source = %q, want generation_live", got)
	}
}

func TestFetch_StaleAnalytics_GenerationLiveAndStaleMarker(t *testing.T) {
	now := time.Now().UTC()
	staleCachedAt := now.Add(-2 * time.Hour).Truncate(time.Second)
	generationTs := now.Add(-5 * time.Minute).Truncate(time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":5.74,"limit":10.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":5.74,"remaining_balance":4.26}}`))
		case "/api/internal/v1/transaction-analytics":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":{"data":[
				{"date":"%s","model":"old/model","total_cost":3.0,"total_tokens":3000000,"requests":10}
			],"cachedAt":"%s"}}`, now.AddDate(0, 0, -2).Format("2006-01-02"), staleCachedAt.Format(time.RFC3339))))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{"id":"gen-1","model":"fresh/model","total_cost":0.40,"tokens_prompt":120,"tokens_completion":80,"created_at":"%s","provider_name":"Novita"}
			]}`, generationTs.Format(time.RFC3339))))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_STALE_MIX", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_STALE_MIX")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-stale-mix",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_STALE_MIX",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := snap.Raw["activity_rows_stale"]; got != "true" {
		t.Fatalf("activity_rows_stale = %q, want true", got)
	}

	if got := snap.Raw["model_mix_source"]; got != "generation_live" {
		t.Fatalf("model_mix_source = %q, want generation_live", got)
	}

	if tok, ok := snap.Metrics["model_old_model_total_tokens"]; !ok || tok.Used == nil || *tok.Used != 3000000 {
		t.Fatalf("old model total tokens metric missing/invalid: %+v", tok)
	}
	if cost, ok := snap.Metrics["model_fresh_model_cost_usd"]; !ok || cost.Used == nil || *cost.Used != 0.4 {
		t.Fatalf("fresh model delta cost metric missing/invalid: %+v", cost)
	}
}

func TestFetch_FreshAnalytics_GenerationLiveAndFreshMarker(t *testing.T) {
	now := time.Now().UTC()
	freshCachedAt := now.Add(-2 * time.Minute).Truncate(time.Second)
	generationTs := now.Add(-1 * time.Minute).Truncate(time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"test","usage":5.74,"limit":10.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":5.74,"remaining_balance":4.26}}`))
		case "/api/internal/v1/transaction-analytics":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":{"data":[
				{"date":"%s","model":"qwen/qwen3-coder-flash","total_cost":1.0,"total_tokens":1000,"requests":1}
			],"cachedAt":"%s"}}`, now.Format("2006-01-02"), freshCachedAt.Format(time.RFC3339))))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{"id":"gen-1","model":"qwen/qwen3-coder-flash","total_cost":0.10,"tokens_prompt":10,"tokens_completion":5,"created_at":"%s","provider_name":"Novita"}
			]}`, generationTs.Format(time.RFC3339))))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_FRESH_MIX", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_FRESH_MIX")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-fresh-mix",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_FRESH_MIX",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	source := snap.Raw["model_mix_source"]
	if source != "generation_live" {
		t.Fatalf("model_mix_source = %q, want generation_live", source)
	}
	if got := snap.Raw["activity_rows_stale"]; got != "false" {
		t.Fatalf("activity_rows_stale = %q, want false", got)
	}
}

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

func TestFetch_ActivityEndpointNewSchema(t *testing.T) {
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	sixDaysAgo := now.AddDate(0, 0, -6).Format("2006-01-02")
	fifteenDaysAgo := now.AddDate(0, 0, -15).Format("2006-01-02")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"activity-key","usage":5.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":5.0}}`))
		case "/activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{"date":"%s","model":"anthropic/claude-3.5-sonnet","endpoint_id":"ep-claude","provider_name":"Anthropic","usage":1.2,"byok_usage_inference":0.4,"prompt_tokens":1000,"completion_tokens":500,"reasoning_tokens":150,"requests":3},
				{"date":"%s","model":"openai/gpt-4o","endpoint_id":"ep-gpt4o","provider_name":"OpenAI","usage":0.8,"byok_usage_inference":0.2,"prompt_tokens":600,"completion_tokens":300,"reasoning_tokens":0,"requests":2},
				{"date":"%s","model":"google/gemini-2.5-pro","endpoint_id":"ep-gemini","provider_name":"Google","usage":2.5,"byok_usage_inference":0.5,"prompt_tokens":1200,"completion_tokens":400,"reasoning_tokens":50,"requests":4}
			]}`, today, sixDaysAgo, fifteenDaysAgo)))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_ACTIVITY_NEW", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_ACTIVITY_NEW")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-activity-new",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_ACTIVITY_NEW",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := snap.Raw["activity_endpoint"]; got != "/activity" {
		t.Fatalf("activity_endpoint = %q, want /activity", got)
	}
	if got := snap.Raw["activity_rows"]; got != "3" {
		t.Fatalf("activity_rows = %q, want 3", got)
	}
	if got := snap.Raw["activity_endpoints"]; got != "3" {
		t.Fatalf("activity_endpoints = %q, want 3", got)
	}

	byokToday := snap.Metrics["today_byok_cost"]
	if byokToday.Used == nil || math.Abs(*byokToday.Used-0.4) > 0.0001 {
		t.Fatalf("today_byok_cost = %v, want 0.4", byokToday.Used)
	}
	byok7d := snap.Metrics["7d_byok_cost"]
	if byok7d.Used == nil || math.Abs(*byok7d.Used-0.6) > 0.0001 {
		t.Fatalf("7d_byok_cost = %v, want 0.6", byok7d.Used)
	}
	byok30d := snap.Metrics["30d_byok_cost"]
	if byok30d.Used == nil || math.Abs(*byok30d.Used-1.1) > 0.0001 {
		t.Fatalf("30d_byok_cost = %v, want 1.1", byok30d.Used)
	}

	if got := seriesValueByDate(snap.DailySeries["analytics_requests"], today); math.Abs(got-3) > 0.001 {
		t.Fatalf("analytics_requests[%s] = %v, want 3", today, got)
	}
	if got := seriesValueByDate(snap.DailySeries["analytics_tokens"], today); math.Abs(got-1650) > 0.001 {
		t.Fatalf("analytics_tokens[%s] = %v, want 1650", today, got)
	}
	if analytics30dCost := snap.Metrics["analytics_30d_cost"]; analytics30dCost.Used == nil || math.Abs(*analytics30dCost.Used-4.5) > 0.001 {
		t.Fatalf("analytics_30d_cost = %v, want 4.5", analytics30dCost.Used)
	}
	if analytics30dReq := snap.Metrics["analytics_30d_requests"]; analytics30dReq.Used == nil || math.Abs(*analytics30dReq.Used-9) > 0.001 {
		t.Fatalf("analytics_30d_requests = %v, want 9", analytics30dReq.Used)
	}
	if analytics7dCost := snap.Metrics["analytics_7d_cost"]; analytics7dCost.Used == nil || math.Abs(*analytics7dCost.Used-2.0) > 0.001 {
		t.Fatalf("analytics_7d_cost = %v, want 2.0", analytics7dCost.Used)
	}
	if endpointCost := snap.Metrics["endpoint_ep-gemini_cost_usd"]; endpointCost.Used == nil || math.Abs(*endpointCost.Used-2.5) > 0.001 {
		t.Fatalf("endpoint_ep-gemini_cost_usd = %v, want 2.5", endpointCost.Used)
	}
	if providerCost := snap.Metrics["provider_google_cost_usd"]; providerCost.Used == nil || math.Abs(*providerCost.Used-2.5) > 0.001 {
		t.Fatalf("provider_google_cost_usd = %v, want 2.5", providerCost.Used)
	}

	mCost := snap.Metrics["model_anthropic_claude-3.5-sonnet_cost_usd"]
	if mCost.Used == nil || math.Abs(*mCost.Used-1.2) > 0.0001 {
		t.Fatalf("model cost = %v, want 1.2", mCost.Used)
	}
	mIn := snap.Metrics["model_anthropic_claude-3.5-sonnet_input_tokens"]
	if mIn.Used == nil || math.Abs(*mIn.Used-1000) > 0.001 {
		t.Fatalf("model input tokens = %v, want 1000", mIn.Used)
	}
	mOut := snap.Metrics["model_anthropic_claude-3.5-sonnet_output_tokens"]
	if mOut.Used == nil || math.Abs(*mOut.Used-500) > 0.001 {
		t.Fatalf("model output tokens = %v, want 500", mOut.Used)
	}
	mReasoning := snap.Metrics["model_anthropic_claude-3.5-sonnet_reasoning_tokens"]
	if mReasoning.Used == nil || math.Abs(*mReasoning.Used-150) > 0.001 {
		t.Fatalf("model reasoning tokens = %v, want 150", mReasoning.Used)
	}
	if got := snap.Raw["model_anthropic_claude-3.5-sonnet_requests"]; got != "3" {
		t.Fatalf("model requests raw = %q, want 3", got)
	}
}

func TestFetch_ActivityDateTimeFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"activity-key","usage":1.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":200,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":1.0}}`))
		case "/activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[
				{"date":"2026-02-20 00:00:00","model":"moonshotai/kimi-k2.5","provider_name":"baseten/fp4","usage":0.10,"byok_usage_inference":0.01,"prompt_tokens":1000,"completion_tokens":100,"reasoning_tokens":20,"requests":2},
				{"date":"2026-02-20 12:34:56","model":"moonshotai/kimi-k2.5","provider_name":"baseten/fp4","usage":0.20,"byok_usage_inference":0.02,"prompt_tokens":2000,"completion_tokens":200,"reasoning_tokens":30,"requests":3}
			]}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_ACTIVITY_DT", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_ACTIVITY_DT")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-activity-dt",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_ACTIVITY_DT",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := seriesValueByDate(snap.DailySeries["analytics_cost"], "2026-02-20"); math.Abs(got-0.30) > 0.0001 {
		t.Fatalf("analytics_cost[2026-02-20] = %v, want 0.30", got)
	}
	if got := seriesValueByDate(snap.DailySeries["analytics_tokens"], "2026-02-20"); math.Abs(got-3350) > 0.0001 {
		t.Fatalf("analytics_tokens[2026-02-20] = %v, want 3350", got)
	}
	if got := seriesValueByDate(snap.DailySeries["analytics_requests"], "2026-02-20"); math.Abs(got-5) > 0.0001 {
		t.Fatalf("analytics_requests[2026-02-20] = %v, want 5", got)
	}
	if got := seriesValueByDate(snap.DailySeries["analytics_reasoning_tokens"], "2026-02-20"); math.Abs(got-50) > 0.0001 {
		t.Fatalf("analytics_reasoning_tokens[2026-02-20] = %v, want 50", got)
	}

	mCost := snap.Metrics["model_moonshotai_kimi-k2.5_cost_usd"]
	if mCost.Used == nil || math.Abs(*mCost.Used-0.30) > 0.0001 {
		t.Fatalf("model cost = %v, want 0.30", mCost.Used)
	}
	if got := snap.Raw["provider_baseten_fp4_requests"]; got != "5" {
		t.Fatalf("provider requests raw = %q, want 5", got)
	}
	if providerCost := snap.Metrics["provider_baseten_fp4_cost_usd"]; providerCost.Used == nil || math.Abs(*providerCost.Used-0.30) > 0.0001 {
		t.Fatalf("provider cost metric = %v, want 0.30", providerCost.Used)
	}
	if analyticsTokens := snap.Metrics["analytics_30d_tokens"]; analyticsTokens.Used == nil || math.Abs(*analyticsTokens.Used-3350) > 0.1 {
		t.Fatalf("analytics_30d_tokens = %v, want 3350", analyticsTokens.Used)
	}
}

func TestResolveGenerationHostingProvider_PrefersUpstreamResponses(t *testing.T) {
	ok200 := 200
	fail503 := 503

	tests := []struct {
		name string
		gen  generationEntry
		want string
	}{
		{
			name: "prefers successful provider response",
			gen: generationEntry{
				Model:        "moonshotai/kimi-k2.5",
				ProviderName: "Openusage",
				ProviderResponses: []generationProviderResponse{
					{ProviderName: "Openusage", Status: &fail503},
					{ProviderName: "Novita", Status: &ok200},
				},
			},
			want: "Novita",
		},
		{
			name: "falls back to provider_name when responses missing",
			gen: generationEntry{
				Model:        "openai/gpt-4o",
				ProviderName: "OpenAI",
			},
			want: "OpenAI",
		},
		{
			name: "falls back to model vendor prefix",
			gen: generationEntry{
				Model: "z-ai/glm-5",
			},
			want: "z-ai",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveGenerationHostingProvider(tc.gen); got != tc.want {
				t.Fatalf("resolveGenerationHostingProvider() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFetch_GenerationUsesUpstreamProviderResponsesForProviderBreakdown(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"gen-provider","usage":0.3,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":0.3}}`))
		case "/activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{
					"id":"gen-1",
					"model":"moonshotai/kimi-k2.5",
					"total_cost":0.2,
					"tokens_prompt":1200,
					"tokens_completion":800,
					"created_at":"%s",
					"provider_name":"Openusage",
					"provider_responses":[
						{"provider_name":"Openusage","status":503},
						{"provider_name":"Novita","status":200}
					]
				},
				{
					"id":"gen-2",
					"model":"z-ai/glm-5",
					"total_cost":0.1,
					"tokens_prompt":100,
					"tokens_completion":50,
					"created_at":"%s"
				}
			]}`, now, now)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_GEN_PROVIDER_RESPONSES", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_GEN_PROVIDER_RESPONSES")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-gen-provider-responses",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_GEN_PROVIDER_RESPONSES",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := snap.Raw["provider_novita_requests"]; got != "1" {
		t.Fatalf("provider_novita_requests = %q, want 1", got)
	}
	if got := snap.Raw["provider_z-ai_requests"]; got != "1" {
		t.Fatalf("provider_z-ai_requests = %q, want 1", got)
	}
	if _, ok := snap.Metrics["provider_openusage_requests"]; ok {
		t.Fatal("provider_openusage_requests should not be emitted when upstream provider_responses are present")
	}
	if got := snap.Raw["model_moonshotai_kimi-k2.5_providers"]; got != "Novita" {
		t.Fatalf("model_moonshotai_kimi-k2.5_providers = %q, want Novita", got)
	}
}

func TestResolveGenerationHostingProvider_TreatsOpenusageAsNonHostProvider(t *testing.T) {
	gen := generationEntry{
		Model:        "moonshotai-kimi-k2.5",
		ProviderName: "Openusage",
	}
	if got := resolveGenerationHostingProvider(gen); got != "moonshotai" {
		t.Fatalf("resolveGenerationHostingProvider() = %q, want moonshotai", got)
	}
}

func TestResolveGenerationHostingProvider_UsesAlternativeEntryFields(t *testing.T) {
	gen := generationEntry{
		Model:                "moonshotai-kimi-k2.5",
		ProviderName:         "Openusage",
		UpstreamProvider:     "Novita",
		UpstreamProviderName: "",
	}
	if got := resolveGenerationHostingProvider(gen); got != "Novita" {
		t.Fatalf("resolveGenerationHostingProvider() = %q, want Novita", got)
	}
}

func TestFetch_GenerationProviderDetailEnrichmentForGenericProviderLabel(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"gen-detail","usage":0.1,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":0.1}}`))
		case "/activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		case "/generation":
			if r.URL.Query().Get("id") == "gen-1" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"data":{
					"id":"gen-1",
					"model":"moonshotai/kimi-k2.5",
					"total_cost":0.1,
					"tokens_prompt":1000,
					"tokens_completion":500,
					"provider_name":"Openusage",
					"provider_responses":[
						{"provider_name":"Openusage","status":503},
						{"provider_name":"Novita","status":200}
					]
				}}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{
					"id":"gen-1",
					"model":"moonshotai/kimi-k2.5",
					"total_cost":0.1,
					"tokens_prompt":1000,
					"tokens_completion":500,
					"created_at":"%s",
					"provider_name":"Openusage"
				}
			]}`, now)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_GEN_DETAIL_ENRICH", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_GEN_DETAIL_ENRICH")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-gen-detail-enrich",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_GEN_DETAIL_ENRICH",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := snap.Raw["generation_provider_detail_lookups"]; got != "1" {
		t.Fatalf("generation_provider_detail_lookups = %q, want 1", got)
	}
	if got := snap.Raw["generation_provider_detail_hits"]; got != "1" {
		t.Fatalf("generation_provider_detail_hits = %q, want 1", got)
	}
	if got := snap.Raw["provider_novita_requests"]; got != "1" {
		t.Fatalf("provider_novita_requests = %q, want 1", got)
	}
	if _, ok := snap.Metrics["provider_openusage_requests"]; ok {
		t.Fatal("provider_openusage_requests should not be emitted after detail enrichment")
	}
}

func TestFetch_GenerationExtendedMetrics(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"gen-ext","usage":1.0,"limit":100.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":100.0,"total_usage":1.0}}`))
		case "/activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"data":[
				{
					"id":"gen-1",
					"model":"openai/gpt-4o",
					"total_cost":0.09,
					"is_byok":true,
					"upstream_inference_cost":0.07,
					"tokens_prompt":1000,
					"tokens_completion":500,
					"native_tokens_prompt":900,
					"native_tokens_completion":450,
					"native_tokens_reasoning":120,
					"native_tokens_cached":80,
					"native_tokens_completion_images":5,
					"num_media_prompt":2,
					"num_media_completion":1,
					"num_input_audio_prompt":3,
					"num_search_results":4,
					"streamed":true,
					"latency":2000,
					"generation_time":1500,
					"moderation_latency":120,
					"cancelled":true,
					"finish_reason":"stop",
					"origin":"https://openrouter.ai",
					"router":"openrouter/auto",
					"api_type":"completions",
					"created_at":"%s",
					"provider_name":"OpenAI"
				}
			]}`, now)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_GEN_EXT", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_GEN_EXT")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-generation-ext",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_GEN_EXT",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	check := func(name string, want float64) {
		t.Helper()
		m, ok := snap.Metrics[name]
		if !ok || m.Used == nil {
			t.Fatalf("missing metric %s", name)
		}
		if math.Abs(*m.Used-want) > 0.0001 {
			t.Fatalf("%s = %v, want %v", name, *m.Used, want)
		}
	}

	check("today_reasoning_tokens", 120)
	check("today_cached_tokens", 80)
	check("today_image_tokens", 5)
	check("today_native_input_tokens", 900)
	check("today_native_output_tokens", 450)
	check("today_media_prompts", 2)
	check("today_media_completions", 1)
	check("today_audio_inputs", 3)
	check("today_search_results", 4)
	check("today_cancelled", 1)
	check("today_streamed_requests", 1)
	check("today_streamed_percent", 100)
	check("today_avg_latency", 2)
	check("today_avg_generation_time", 1.5)
	check("today_avg_moderation_latency", 0.12)
	check("today_completions_requests", 1)
	check("today_byok_cost", 0.07)
	check("7d_byok_cost", 0.07)
	check("30d_byok_cost", 0.07)
	check("tool_openai_gpt-4o", 1)
	check("tool_calls_total", 1)
	check("tool_completed", 0)
	check("tool_cancelled", 1)
	check("tool_success_rate", 0)
	check("model_openai_gpt-4o_reasoning_tokens", 120)
	check("model_openai_gpt-4o_cached_tokens", 80)
	check("model_openai_gpt-4o_image_tokens", 5)
	check("model_openai_gpt-4o_native_input_tokens", 900)
	check("model_openai_gpt-4o_native_output_tokens", 450)
	check("model_openai_gpt-4o_avg_latency", 2)

	if got := snap.Raw["today_finish_reasons"]; !strings.Contains(got, "stop=1") {
		t.Fatalf("today_finish_reasons = %q, want stop=1", got)
	}
	if got := snap.Raw["today_origins"]; !strings.Contains(got, "https://openrouter.ai=1") {
		t.Fatalf("today_origins = %q, want https://openrouter.ai=1", got)
	}
	if got := snap.Raw["today_routers"]; !strings.Contains(got, "openrouter/auto=1") {
		t.Fatalf("today_routers = %q, want openrouter/auto=1", got)
	}
	if got := snap.Raw["tool_usage_source"]; got != "inferred_from_model_requests" {
		t.Fatalf("tool_usage_source = %q, want inferred_from_model_requests", got)
	}
	if got := snap.Raw["tool_usage"]; !strings.Contains(got, "openai/gpt-4o: 1 calls") {
		t.Fatalf("tool_usage = %q, want model-based usage summary", got)
	}
}

func TestFetch_ActivityForbidden_ReportsManagementKeyRequirement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"std-key","usage":0.5,"limit":10.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":2.25}}`))
		case "/activity":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"message":"Only management keys can fetch activity for an account","code":403}}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_ACTIVITY_403", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_ACTIVITY_403")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-activity-403",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_ACTIVITY_403",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK", snap.Status)
	}
	if got := snap.Raw["analytics_error"]; !strings.Contains(got, "management keys") {
		t.Fatalf("analytics_error = %q, want management-keys message", got)
	}
	if !strings.Contains(snap.Message, "$2.2500 used / $10.00 credits") {
		t.Fatalf("message = %q, want credits-detail based message", snap.Message)
	}
}

func TestFetch_ActivityForbidden_FallsBackToAnalyticsUserActivity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"std-key","usage":0.5,"limit":10.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":2.25}}`))
		case "/activity":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"message":"Only management keys can fetch activity for an account","code":403}}`))
		case "/analytics/user-activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[
				{"date":"2026-02-21","model":"qwen/qwen3-coder-flash","total_cost":0.918,"total_tokens":3058944,"requests":72}
			]}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_ACTIVITY_FALLBACK", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_ACTIVITY_FALLBACK")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-activity-fallback",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_ACTIVITY_FALLBACK",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK; message=%s", snap.Status, snap.Message)
	}
	if _, ok := snap.Raw["analytics_error"]; ok {
		t.Fatalf("unexpected analytics_error: %q", snap.Raw["analytics_error"])
	}
	if got := snap.Raw["activity_endpoint"]; got != "/analytics/user-activity" {
		t.Fatalf("activity_endpoint = %q, want /analytics/user-activity", got)
	}
	if m, ok := snap.Metrics["model_qwen_qwen3-coder-flash_total_tokens"]; !ok || m.Used == nil || *m.Used != 3058944 {
		t.Fatalf("missing/invalid qwen total tokens metric: %+v", m)
	}
}

func TestFetch_ActivityDateFallback_UsesYesterdayAndNoCacheHeaders(t *testing.T) {
	var seenEmptyDate bool
	var seenFallbackDate string
	var seenCacheControl string
	var seenPragma string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"std-key","usage":0.5,"limit":10.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":2.25}}`))
		case "/activity":
			seenCacheControl = r.Header.Get("Cache-Control")
			seenPragma = r.Header.Get("Pragma")
			date := strings.TrimSpace(r.URL.Query().Get("date"))
			if date == "" {
				seenEmptyDate = true
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":{"message":"Date must be within the last 30 (completed) UTC days","code":400}}`))
				return
			}
			seenFallbackDate = date
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[
				{"date":"2026-02-21 00:00:00","model_permaslug":"qwen/qwen3-coder-flash","usage":0.91764,"requests":72,"prompt_tokens":3052166,"completion_tokens":6778,"reasoning_tokens":0,"cached_tokens":1508864}
			]}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_ACTIVITY_DATE_FALLBACK", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_ACTIVITY_DATE_FALLBACK")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-activity-date-fallback",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_ACTIVITY_DATE_FALLBACK",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK; message=%s", snap.Status, snap.Message)
	}
	if !seenEmptyDate {
		t.Fatal("expected initial /activity call without date")
	}
	if seenFallbackDate == "" {
		t.Fatal("expected fallback /activity call with date query")
	}
	if seenCacheControl != "no-cache, no-store, max-age=0" {
		t.Fatalf("cache-control = %q, want no-cache, no-store, max-age=0", seenCacheControl)
	}
	if seenPragma != "no-cache" {
		t.Fatalf("pragma = %q, want no-cache", seenPragma)
	}
	if got := snap.Raw["activity_endpoint"]; !strings.HasPrefix(got, "/activity?date=") {
		t.Fatalf("activity_endpoint = %q, want /activity?date=...", got)
	}
	if m, ok := snap.Metrics["model_qwen_qwen3-coder-flash_input_tokens"]; !ok || m.Used == nil || *m.Used != 3052166 {
		t.Fatalf("missing/invalid qwen input tokens metric: %+v", m)
	}
}

func TestFetch_TransactionAnalyticsNestedPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"std-key","usage":0.5,"limit":10.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":2.25}}`))
		case "/api/internal/v1/transaction-analytics":
			if r.URL.RawQuery != "window=1mo" {
				t.Fatalf("unexpected query: %q", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"cachedAt":"2026-02-22T00:00:00Z","data":[
				{"date":"2026-02-21 00:00:00","model_permaslug":"qwen/qwen3-coder-flash","usage":0.91764,"requests":72,"prompt_tokens":3052166,"completion_tokens":6778,"reasoning_tokens":0,"cached_tokens":1508864}
			]}}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_TX_ANALYTICS", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_TX_ANALYTICS")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-tx-analytics",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_TX_ANALYTICS",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK; message=%s", snap.Status, snap.Message)
	}
	if got := snap.Raw["activity_endpoint"]; got != "/api/internal/v1/transaction-analytics?window=1mo" {
		t.Fatalf("activity_endpoint = %q, want transaction analytics endpoint", got)
	}
	if got := snap.Raw["activity_cached_at"]; got != "2026-02-22T00:00:00Z" {
		t.Fatalf("activity_cached_at = %q, want 2026-02-22T00:00:00Z", got)
	}
	if m, ok := snap.Metrics["model_qwen_qwen3-coder-flash_input_tokens"]; !ok || m.Used == nil || *m.Used != 3052166 {
		t.Fatalf("missing/invalid qwen input tokens metric: %+v", m)
	}
	if m, ok := snap.Metrics["model_qwen_qwen3-coder-flash_output_tokens"]; !ok || m.Used == nil || *m.Used != 6778 {
		t.Fatalf("missing/invalid qwen output tokens metric: %+v", m)
	}
	if m, ok := snap.Metrics["model_qwen_qwen3-coder-flash_cached_tokens"]; !ok || m.Used == nil || *m.Used != 1508864 {
		t.Fatalf("missing/invalid qwen cached tokens metric: %+v", m)
	}
	if m, ok := snap.Metrics["model_qwen_qwen3-coder-flash_cost_usd"]; !ok || m.Used == nil || math.Abs(*m.Used-0.91764) > 0.000001 {
		t.Fatalf("missing/invalid qwen cost metric: %+v", m)
	}
}

func TestFetch_TransactionAnalyticsNumericCachedAtAndByokRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"std-key","usage":0.5,"limit":10.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":2.25}}`))
		case "/api/internal/v1/transaction-analytics":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"cachedAt":1771717984900,"data":[
				{"date":"2026-02-21 00:00:00","model_permaslug":"qwen/qwen3-coder-flash","usage":0.91764,"requests":72,"byok_requests":3,"prompt_tokens":3052166,"completion_tokens":6778,"reasoning_tokens":0,"cached_tokens":1508864}
			]}}`))
		case "/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_TX_ANALYTICS_NUM", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_TX_ANALYTICS_NUM")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-tx-analytics-num",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_TX_ANALYTICS_NUM",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if got := snap.Raw["activity_cached_at"]; got != "2026-02-21T23:53:04Z" {
		t.Fatalf("activity_cached_at = %q, want 2026-02-21T23:53:04Z", got)
	}
	if m, ok := snap.Metrics["model_qwen_qwen3-coder-flash_byok_requests"]; !ok || m.Used == nil || *m.Used != 3 {
		t.Fatalf("missing/invalid byok requests metric: %+v", m)
	}
}

func TestFetch_TransactionAnalyticsURL_UsesRootWhenBaseURLHasAPIV1(t *testing.T) {
	var seenInternalPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"std-key","usage":0.5,"limit":10.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/api/v1/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":2.25}}`))
		case "/api/internal/v1/transaction-analytics":
			seenInternalPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"cachedAt":1771717984900,"data":[
				{"date":"2026-02-21 00:00:00","model_permaslug":"qwen/qwen3-coder-flash","usage":0.91764,"requests":72,"prompt_tokens":3052166,"completion_tokens":6778,"reasoning_tokens":0,"cached_tokens":1508864}
			]}}`))
		case "/api/v1/generation":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_TX_URL", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_TX_URL")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-tx-url",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_TX_URL",
		BaseURL:   server.URL + "/api/v1",
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK; message=%s", snap.Status, snap.Message)
	}
	if seenInternalPath != "/api/internal/v1/transaction-analytics" {
		t.Fatalf("internal analytics path = %q, want /api/internal/v1/transaction-analytics", seenInternalPath)
	}
	if got := snap.Raw["activity_endpoint"]; got != "/api/internal/v1/transaction-analytics?window=1mo" {
		t.Fatalf("activity_endpoint = %q, want transaction analytics endpoint", got)
	}
}

func TestFetch_GenerationListUnsupported_Graceful(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/key":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"label":"std-key","usage":1.0,"limit":10.0,"is_free_tier":false,"rate_limit":{"requests":100,"interval":"10s"}}}`))
		case "/credits":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"total_credits":10.0,"total_usage":1.0}}`))
		case "/activity":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		case "/generation":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"success":false,"error":{"name":"ZodError","message":"expected string for id"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OR_KEY_GEN_400", "test-key")
	defer os.Unsetenv("TEST_OR_KEY_GEN_400")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-generation-400",
		Provider:  "openrouter",
		APIKeyEnv: "TEST_OR_KEY_GEN_400",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := snap.Raw["generation_note"]; got == "" {
		t.Fatal("missing generation_note for unsupported generation listing")
	}
	if got := snap.Raw["generations_fetched"]; got != "0" {
		t.Fatalf("generations_fetched = %q, want 0", got)
	}
	if _, ok := snap.Raw["generation_error"]; ok {
		t.Fatalf("unexpected generation_error = %q", snap.Raw["generation_error"])
	}
}

func seriesValueByDate(points []core.TimePoint, date string) float64 {
	for _, p := range points {
		if p.Date == date {
			return p.Value
		}
	}
	return 0
}
