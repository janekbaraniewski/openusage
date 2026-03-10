package openrouter

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

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
