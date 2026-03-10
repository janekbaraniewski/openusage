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
