package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

// todayISO returns today's date in RFC3339 for test generation entries.
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

	// Check credit balance from /credits endpoint
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

	// Check today's aggregate stats
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

	// Check average latency
	todayLatency, ok := snap.Metrics["today_avg_latency"]
	if !ok {
		t.Fatal("missing today_avg_latency metric")
	}
	expectedAvgLatency := float64(2500+3000+1500) / 3.0 / 1000.0 // seconds
	if todayLatency.Used == nil || (*todayLatency.Used-expectedAvgLatency) > 0.01 {
		t.Errorf("today_avg_latency = %v, want ~%v", todayLatency.Used, expectedAvgLatency)
	}

	// Check per-model breakdown for Claude
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

	// Check per-model breakdown for GPT-4o
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

	// Check Raw per-model data
	if got := snap.Raw["model_anthropic_claude-3.5-sonnet_requests"]; got != "2" {
		t.Errorf("claude requests in raw = %q, want 2", got)
	}
	if got := snap.Raw["model_anthropic_claude-3.5-sonnet_providers"]; got != "Anthropic" {
		t.Errorf("claude providers in raw = %q, want 'Anthropic'", got)
	}

	// Check Raw per-provider data
	if got := snap.Raw["provider_anthropic_requests"]; got != "2" {
		t.Errorf("provider anthropic requests = %q, want 2", got)
	}
	if got := snap.Raw["provider_openai_requests"]; got != "1" {
		t.Errorf("provider openai requests = %q, want 1", got)
	}
}

func TestFetch_RateLimitHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/key":
			// Set both request and token rate limit headers
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

	// Check request rate limit headers
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

	// Check token rate limit headers
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

	// Check reset times exist
	if _, ok := snap.Resets["rpm_reset"]; !ok {
		t.Error("missing rpm_reset in Resets")
	}
	if _, ok := snap.Resets["tpm_reset"]; !ok {
		t.Error("missing tpm_reset in Resets")
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
			// First page: return full page, second page: return partial
			if page == 1 {
				// Return 3 items (since our test uses small numbers)
				data := fmt.Sprintf(`{"data":[
					{"id":"gen-1","model":"openai/gpt-4o","total_cost":0.01,"tokens_prompt":100,"tokens_completion":50,"created_at":"%s","provider_name":"OpenAI"},
					{"id":"gen-2","model":"openai/gpt-4o","total_cost":0.01,"tokens_prompt":100,"tokens_completion":50,"created_at":"%s","provider_name":"OpenAI"},
					{"id":"gen-3","model":"openai/gpt-4o","total_cost":0.01,"tokens_prompt":100,"tokens_completion":50,"created_at":"%s","provider_name":"OpenAI"}
				]}`, now, now, now)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(data))
			} else {
				// Empty page — end of data
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
			got := sanitizeModelName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeModelName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeProviderName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Anthropic", "anthropic"},
		{"OpenAI", "openai"},
		{"Google AI Studio", "google_ai_studio"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeProviderName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeProviderName(%q) = %q, want %q", tt.input, got, tt.want)
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

	// Credits should not have a limit
	credits, ok := snap.Metrics["credits"]
	if !ok {
		t.Fatal("missing credits metric")
	}
	if credits.Limit != nil {
		t.Errorf("credits limit = %v, want nil (unlimited)", credits.Limit)
	}

	// Free tier should have lower RPM
	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 20 {
		t.Errorf("rpm limit = %v, want 20", rpm.Limit)
	}

	// Message should show $0.0000 used
	if !strings.Contains(snap.Message, "$0.0000") {
		t.Errorf("message = %q, want to contain $0.0000", snap.Message)
	}
}
