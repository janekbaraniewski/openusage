package zai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func testAccount(baseURL string) core.AccountConfig {
	return core.AccountConfig{
		ID:        "zai-test",
		Provider:  "zai",
		APIKeyEnv: "TEST_ZAI_KEY",
		BaseURL:   baseURL + "/api/coding/paas/v4",
	}
}

func TestFetch_MissingKey_ReturnsAuth(t *testing.T) {
	t.Setenv("TEST_ZAI_KEY_MISSING", "")

	p := New()
	acct := core.AccountConfig{
		ID:        "zai-test",
		Provider:  "zai",
		APIKeyEnv: "TEST_ZAI_KEY_MISSING",
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Fatalf("Status = %v, want %v", snap.Status, core.StatusAuth)
	}
}

func TestFetch_ModelsUnauthorized_ReturnsAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/coding/paas/v4/models" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Fatalf("Status = %v, want %v", snap.Status, core.StatusAuth)
	}
}

func TestFetch_ModelsOK_NoMonitorData_FreeState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.5"}]}`))
		case "/api/monitor/usage/quota/limit", "/api/monitor/usage/model-usage", "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"code":0,"msg":"ok","data":null}`))
		case "/api/paas/v4/user/credit_grants":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want %v", snap.Status, core.StatusOK)
	}
	if !strings.Contains(strings.ToLower(snap.Message), "no active coding package") {
		t.Fatalf("Message = %q, want no active coding package hint", snap.Message)
	}
	if got := snap.Raw["models_count"]; got != "1" {
		t.Fatalf("models_count = %q, want 1", got)
	}
	if got := snap.Raw["subscription_status"]; got != "inactive_or_free" {
		t.Fatalf("subscription_status = %q, want inactive_or_free", got)
	}
}

func TestFetch_QuotaLimit_ParsesMetricsAndNearLimit(t *testing.T) {
	var quotaCalls int32
	reset := time.Now().UTC().Add(30 * time.Minute).UnixMilli()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.5"}]}`))
		case "/api/monitor/usage/quota/limit":
			call := atomic.AddInt32(&quotaCalls, 1)
			if call == 1 && r.Header.Get("Authorization") == "test-zai-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer test-zai-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{
				"success": true,
				"code": 0,
				"msg": "ok",
				"data": {
					"limits": [
						{"type":"TOKENS_LIMIT","percentage":85,"usage":2000000,"currentValue":1700000,"nextResetTime":%d},
						{"type":"TIME_LIMIT","percentage":40,"usage":1000,"currentValue":400}
					]
				}
			}`, reset)))
		case "/api/monitor/usage/model-usage", "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusNearLimit {
		t.Fatalf("Status = %v, want %v", snap.Status, core.StatusNearLimit)
	}
	usage, ok := snap.Metrics["usage_five_hour"]
	if !ok || usage.Used == nil {
		t.Fatalf("missing usage_five_hour metric")
	}
	if *usage.Used != 85 {
		t.Fatalf("usage_five_hour.used = %v, want 85", *usage.Used)
	}
	tokens, ok := snap.Metrics["tokens_five_hour"]
	if !ok || tokens.Limit == nil || tokens.Used == nil || tokens.Remaining == nil {
		t.Fatalf("missing tokens_five_hour metric fields")
	}
	if *tokens.Limit != 2000000 || *tokens.Used != 1700000 || *tokens.Remaining != 300000 {
		t.Fatalf("unexpected tokens_five_hour values: %+v", tokens)
	}
	mcp, ok := snap.Metrics["mcp_monthly_usage"]
	if !ok || mcp.Limit == nil || mcp.Used == nil {
		t.Fatalf("missing mcp_monthly_usage metric")
	}
	if *mcp.Limit != 1000 || *mcp.Used != 400 {
		t.Fatalf("unexpected mcp_monthly_usage values: %+v", mcp)
	}
	if _, ok := snap.Resets["usage_five_hour"]; !ok {
		t.Fatalf("expected usage_five_hour reset")
	}
	if got := snap.Raw["quota_api"]; got != "ok" {
		t.Fatalf("quota_api = %q, want ok", got)
	}
	if atomic.LoadInt32(&quotaCalls) < 2 {
		t.Fatalf("expected auth fallback to bearer to execute, calls=%d", quotaCalls)
	}
}

func TestFetch_QuotaLimit_LimitedByBusinessCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.5"}]}`))
		case "/api/monitor/usage/quota/limit":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":false,"code":1113,"msg":"Insufficient balance or no resource package","data":null}`))
		case "/api/monitor/usage/model-usage", "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Fatalf("Status = %v, want %v", snap.Status, core.StatusLimited)
	}
	if !strings.Contains(strings.ToLower(snap.Message), "insufficient balance") {
		t.Fatalf("Message = %q, want insufficient balance note", snap.Message)
	}
}

func TestFetch_ParsesModelAndToolUsage(t *testing.T) {
	// Both day keys come from one instant; two separate time.Now() calls can
	// straddle midnight and collapse yesterday onto today.
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	yesterday := now.Add(-24 * time.Hour).Format("2006-01-02")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.5"},{"id":"glm-4.5-air"}]}`))
		case "/api/monitor/usage/quota/limit":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":45,"usage":1000,"currentValue":450}]}}`))
		case "/api/monitor/usage/model-usage":
			if r.URL.Query().Get("startTime") == "" || r.URL.Query().Get("endTime") == "" {
				t.Fatalf("expected startTime and endTime query params")
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{
				"success": true,
				"data": [
					{"date":"%s","model":"glm-4.5","requests":2,"input_tokens":1000,"output_tokens":500,"total_cost":0.42},
					{"date":"%s","model":"glm-4.5","requests":1,"input_tokens":100,"output_tokens":50,"total_cost":0.05},
					{"date":"%s","model":"glm-4.5-air","requests":3,"input_tokens":300,"output_tokens":150,"total_cost":0.12}
				]
			}`, yesterday, today, today)))
		case "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{
				"success": true,
				"data": [
					{"date":"%s","tool":"search","calls":4},
					{"date":"%s","tool":"editor","calls":2}
				]
			}`, today, today)))
		case "/api/paas/v4/user/credit_grants":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"total_granted":100,"total_used":27.5,"total_available":72.5}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want %v", snap.Status, core.StatusOK)
	}
	if metric, ok := snap.Metrics["today_requests"]; !ok || metric.Used == nil || *metric.Used != 4 {
		t.Fatalf("today_requests metric missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["7d_api_cost"]; !ok || metric.Used == nil || *metric.Used != 0.59 {
		t.Fatalf("7d_api_cost metric missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["tool_calls_today"]; !ok || metric.Used == nil || *metric.Used != 6 {
		t.Fatalf("tool_calls_today metric missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["model_glm-4_5_cost_usd"]; !ok || metric.Used == nil || *metric.Used != 0.47 {
		t.Fatalf("model_glm-4_5_cost_usd metric missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["credit_balance"]; !ok || metric.Remaining == nil || *metric.Remaining != 72.5 {
		t.Fatalf("credit_balance metric missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["available_balance"]; !ok || metric.Used == nil || *metric.Used != 72.5 {
		t.Fatalf("available_balance metric missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["spend_limit"]; !ok || metric.Used == nil || metric.Limit == nil || *metric.Used != 27.5 || *metric.Limit != 100 {
		t.Fatalf("spend_limit metric missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["plan_percent_used"]; !ok || metric.Used == nil || *metric.Used < 27.49 || *metric.Used > 27.51 {
		t.Fatalf("plan_percent_used metric missing or invalid: %+v", metric)
	}
	if len(snap.ModelUsage) != 2 {
		t.Fatalf("ModelUsage records = %d, want 2", len(snap.ModelUsage))
	}
	if len(snap.DailySeries["cost"]) == 0 {
		t.Fatalf("expected daily cost series")
	}
	if got := snap.Raw["model_usage_api"]; got != "ok" {
		t.Fatalf("model_usage_api = %q, want ok", got)
	}
}

func TestFetch_EnrichesUsageDimensionsAndSummaries(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.5"},{"id":"glm-5"}]}`))
		case "/api/monitor/usage/quota/limit":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":20,"usage":1000,"currentValue":200}]}}`))
		case "/api/monitor/usage/model-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{
				"success": true,
				"data": {
					"items": [
						{
							"date": "%s",
							"model": {"name":"glm-4.5"},
							"client": "openusage",
							"source": "openusage",
							"provider": "z-ai",
							"interface": "cli",
							"language": "go",
							"requestCount": 3,
							"inputTokens": 100,
							"outputTokens": 40,
							"reasoningTokens": 10,
							"totalCostUSD": 0.20
						},
						{
							"date": "%s",
							"modelName": "glm-5",
							"clientName": "openusage",
							"source_name": "openusage",
							"provider_name": "z-ai",
							"interface_name": "cli",
							"programming_language": "go",
							"requests": 1,
							"total_tokens": 500,
							"cost_cents": 12
						},
						{
							"date": "%s",
							"language_name": "python",
							"requests": 2
						}
					]
				}
			}`, today, today, today)))
		case "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{
				"success": true,
				"data": {
					"rows": [
						{"date":"%s","tool_name":"read","calls":4},
						{"date":"%s","tool":{"name":"bash"},"request_count":2}
					]
				}
			}`, today, today)))
		case "/api/paas/v4/user/credit_grants":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"total_granted":20,"total_used":1.32,"total_available":18.68}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want %v", snap.Status, core.StatusOK)
	}

	if metric, ok := snap.Metrics["client_openusage_total_tokens"]; !ok || metric.Used == nil || *metric.Used != 650 {
		t.Fatalf("client_openusage_total_tokens missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["client_openusage_requests"]; !ok || metric.Used == nil || *metric.Used != 4 {
		t.Fatalf("client_openusage_requests missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["source_openusage_requests_today"]; !ok || metric.Used == nil || *metric.Used != 4 {
		t.Fatalf("source_openusage_requests_today missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["provider_z-ai_requests"]; !ok || metric.Used == nil || *metric.Used != 4 {
		t.Fatalf("provider_z-ai_requests missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["interface_cli"]; !ok || metric.Used == nil || *metric.Used != 4 {
		t.Fatalf("interface_cli missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["lang_go"]; !ok || metric.Used == nil || *metric.Used != 4 {
		t.Fatalf("lang_go missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["lang_python"]; !ok || metric.Used == nil || *metric.Used != 2 {
		t.Fatalf("lang_python missing or invalid: %+v", metric)
	}

	if metric, ok := snap.Metrics["tool_read"]; !ok || metric.Used == nil || *metric.Used != 4 {
		t.Fatalf("tool_read missing or invalid: %+v", metric)
	}
	if _, ok := snap.Metrics["tool_read_calls"]; ok {
		t.Fatalf("tool_read_calls should not be emitted anymore")
	}
	if metric, ok := snap.Metrics["window_requests"]; !ok || metric.Used == nil || *metric.Used != 4 {
		t.Fatalf("window_requests missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["window_tokens"]; !ok || metric.Used == nil || *metric.Used != 650 {
		t.Fatalf("window_tokens missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["window_cost"]; !ok || metric.Used == nil || *metric.Used != 0.32 {
		t.Fatalf("window_cost missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["available_balance"]; !ok || metric.Used == nil || *metric.Used != 18.68 {
		t.Fatalf("available_balance missing or invalid: %+v", metric)
	}
	for _, key := range []string{
		"api_models_payload_bytes",
		"api_quota_limit_payload_bytes",
		"api_model_usage_payload_bytes",
		"api_tool_usage_payload_bytes",
		"api_credits_payload_bytes",
	} {
		metric, ok := snap.Metrics[key]
		if !ok || metric.Used == nil || *metric.Used <= 0 {
			t.Fatalf("%s missing or invalid: %+v", key, metric)
		}
	}
	if strings.TrimSpace(snap.Raw["api_model_usage_numeric_top"]) == "" {
		t.Fatalf("api_model_usage_numeric_top should be populated")
	}

	for _, key := range []string{"model_usage", "client_usage", "tool_usage", "language_usage", "provider_usage"} {
		if strings.TrimSpace(snap.Raw[key]) == "" {
			t.Fatalf("raw summary %q should be populated", key)
		}
	}
	for _, key := range []string{"activity_days", "activity_models", "activity_clients", "activity_sources", "activity_providers"} {
		if strings.TrimSpace(snap.Raw[key]) == "" {
			t.Fatalf("raw activity key %q should be populated", key)
		}
	}
	if !strings.Contains(snap.Raw["tool_usage"], "read") {
		t.Fatalf("tool_usage = %q, expected read", snap.Raw["tool_usage"])
	}
}

func TestFetch_CreditsFromGrantRowsWithoutTotalAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-5"}]}`))
		case "/api/monitor/usage/quota/limit", "/api/monitor/usage/model-usage", "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":null}`))
		case "/api/paas/v4/user/credit_grants":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"data": {
					"credit_grants": {
						"grants": [
							{"grant_amount": 50, "used_amount": 5, "expires_at": "2099-01-01T00:00:00Z"},
							{"grant_amount": 20, "used_amount": 4}
						]
					}
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if metric, ok := snap.Metrics["available_balance"]; !ok || metric.Used == nil || *metric.Used != 61 {
		t.Fatalf("available_balance missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["spend_limit"]; !ok || metric.Used == nil || metric.Limit == nil || *metric.Used != 9 || *metric.Limit != 70 {
		t.Fatalf("spend_limit missing or invalid: %+v", metric)
	}
	if got := snap.Raw["credit_grants_count"]; got != "2" {
		t.Fatalf("credit_grants_count = %q, want 2", got)
	}
	if got := snap.Raw["credits_api"]; got != "ok" {
		t.Fatalf("credits_api = %q, want ok", got)
	}
}

func TestFetch_ParsesKeyedUsageBreakdowns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.5"},{"id":"glm-5"}]}`))
		case "/api/monitor/usage/quota/limit":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":10,"usage":1000,"currentValue":100}]}}`))
		case "/api/monitor/usage/model-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"success": true,
				"data": {
					"model_usage": {
						"glm-5": {"requests": 3, "total_tokens": 700, "total_cost": 0.11},
						"glm-4.5": {"requests": 2, "input_tokens": 100, "output_tokens": 50, "total_cost": 0.05}
					},
					"language_usage": {
						"go": 3,
						"python": 2
					},
					"provider_usage": {
						"z-ai": {"requests": 5, "total_cost": 0.16}
					}
				}
			}`))
		case "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if metric, ok := snap.Metrics["model_glm-5_requests"]; !ok || metric.Used == nil || *metric.Used != 3 {
		t.Fatalf("model_glm-5_requests missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["model_glm-4_5_requests"]; !ok || metric.Used == nil || *metric.Used != 2 {
		t.Fatalf("model_glm-4_5_requests missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["provider_z-ai_requests"]; !ok || metric.Used == nil || *metric.Used != 5 {
		t.Fatalf("provider_z-ai_requests missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["lang_go"]; !ok || metric.Used == nil || *metric.Used != 3 {
		t.Fatalf("lang_go missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["lang_python"]; !ok || metric.Used == nil || *metric.Used != 2 {
		t.Fatalf("lang_python missing or invalid: %+v", metric)
	}
	if metric, ok := snap.Metrics["active_languages"]; !ok || metric.Used == nil || *metric.Used < 2 {
		t.Fatalf("active_languages missing or invalid: %+v", metric)
	}
	if got := snap.Raw["activity_languages"]; got == "" {
		t.Fatalf("activity_languages should be populated")
	}
}

func TestFetch_PartialMonitorFailures_ReturnsSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.5"}]}`))
		case "/api/monitor/usage/quota/limit":
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/monitor/usage/model-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{`))
		case "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want %v", snap.Status, core.StatusOK)
	}
	if snap.Raw["quota_limit_error"] == "" {
		t.Fatalf("expected quota_limit_error")
	}
	if snap.Raw["model_usage_error"] == "" {
		t.Fatalf("expected model_usage_error")
	}
	if snap.Raw["tool_usage_error"] == "" {
		t.Fatalf("expected tool_usage_error")
	}
}

func requireMetric(t *testing.T, snap core.UsageSnapshot, key string) core.Metric {
	t.Helper()
	metric, ok := snap.Metrics[key]
	if !ok {
		t.Fatalf("missing metric %q (have %v)", key, core.SortedStringKeys(snap.Metrics))
	}
	return metric
}

func TestApplyQuotaData_CreditLimitWindows(t *testing.T) {
	const resetMS = int64(1798761600000)
	payload := json.RawMessage(fmt.Sprintf(`{
		"limits": [
			{"type":"CREDIT_LIMIT","unit":3,"number":5,"usage":200,"currentValue":0,"percentage":0,"remaining":200},
			{"type":"CREDIT_LIMIT","unit":6,"number":1,"usage":1000,"currentValue":250,"percentage":25,"remaining":750,"nextResetTime":%d}
		],
		"level": "pro"
	}`, resetMS))

	snap := core.NewUsageSnapshot("zai", "acct")
	var state providerState
	if !applyQuotaData(payload, &snap, &state) {
		t.Fatalf("applyQuotaData = false, want true")
	}

	fiveHour := requireMetric(t, snap, "usage_five_hour")
	if fiveHour.Unit != "%" || fiveHour.Window != "5h" ||
		fiveHour.Used == nil || fiveHour.Limit == nil || *fiveHour.Used != 0 || *fiveHour.Limit != 100 {
		t.Fatalf("usage_five_hour = %+v", fiveHour)
	}

	sevenDay := requireMetric(t, snap, "usage_seven_day")
	if sevenDay.Unit != "%" || sevenDay.Window != "7d" ||
		sevenDay.Used == nil || sevenDay.Limit == nil || *sevenDay.Used != 25 || *sevenDay.Limit != 100 {
		t.Fatalf("usage_seven_day = %+v", sevenDay)
	}

	credits5h := requireMetric(t, snap, "credits_five_hour")
	if credits5h.Unit != "credits" || credits5h.Window != "5h" ||
		credits5h.Limit == nil || credits5h.Used == nil || credits5h.Remaining == nil ||
		*credits5h.Limit != 200 || *credits5h.Used != 0 || *credits5h.Remaining != 200 {
		t.Fatalf("credits_five_hour = %+v", credits5h)
	}

	credits7d := requireMetric(t, snap, "credits_seven_day")
	if credits7d.Unit != "credits" || credits7d.Window != "7d" ||
		credits7d.Limit == nil || credits7d.Used == nil || credits7d.Remaining == nil ||
		*credits7d.Limit != 1000 || *credits7d.Used != 250 || *credits7d.Remaining != 750 {
		t.Fatalf("credits_seven_day = %+v", credits7d)
	}

	for _, key := range []string{"tokens_five_hour", "tokens_seven_day"} {
		if _, ok := snap.Metrics[key]; ok {
			t.Fatalf("%s should not be emitted for CREDIT_LIMIT", key)
		}
	}

	reset, ok := snap.Resets["usage_seven_day"]
	if !ok {
		t.Fatalf("expected usage_seven_day reset")
	}
	if want := time.Unix(resetMS/1000, 0).UTC(); !reset.Equal(want) {
		t.Fatalf("usage_seven_day reset = %v, want %v", reset, want)
	}
	if _, ok := snap.Resets["usage_five_hour"]; ok {
		t.Fatalf("did not expect a 5h reset when nextResetTime is absent")
	}
	if state.limited || state.nearLimit {
		t.Fatalf("state = %+v, want neither limited nor near-limit", state)
	}
}

func TestApplyQuotaData_LegacyTokenLimitWithoutUnitKeepsFiveHour(t *testing.T) {
	payload := json.RawMessage(`{"limits":[{"type":"TOKENS_LIMIT","percentage":45,"usage":1000,"currentValue":450,"nextResetTime":1798761600000}]}`)

	snap := core.NewUsageSnapshot("zai", "acct")
	var state providerState
	if !applyQuotaData(payload, &snap, &state) {
		t.Fatalf("applyQuotaData = false, want true")
	}

	usage := requireMetric(t, snap, "usage_five_hour")
	if usage.Window != "5h" || usage.Used == nil || *usage.Used != 45 {
		t.Fatalf("usage_five_hour = %+v", usage)
	}
	tokens := requireMetric(t, snap, "tokens_five_hour")
	if tokens.Unit != "tokens" || tokens.Limit == nil || tokens.Used == nil || tokens.Remaining == nil ||
		*tokens.Limit != 1000 || *tokens.Used != 450 || *tokens.Remaining != 550 {
		t.Fatalf("tokens_five_hour = %+v", tokens)
	}
	for _, key := range []string{"usage_seven_day", "tokens_seven_day", "credits_five_hour", "credits_seven_day"} {
		if _, ok := snap.Metrics[key]; ok {
			t.Fatalf("%s should not be emitted for a unit-less legacy TOKENS_LIMIT", key)
		}
	}
	if _, ok := snap.Resets["usage_five_hour"]; !ok {
		t.Fatalf("expected usage_five_hour reset")
	}
}

func TestApplyQuotaData_TokenLimitModernWindows(t *testing.T) {
	payload := json.RawMessage(`{"limits":[
		{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":10,"usage":1000,"currentValue":100},
		{"type":"TOKENS_LIMIT","unit":6,"number":1,"percentage":20,"usage":5000,"currentValue":1000}
	]}`)

	snap := core.NewUsageSnapshot("zai", "acct")
	var state providerState
	if !applyQuotaData(payload, &snap, &state) {
		t.Fatalf("applyQuotaData = false, want true")
	}

	if usage := requireMetric(t, snap, "usage_five_hour"); usage.Window != "5h" || usage.Used == nil || *usage.Used != 10 {
		t.Fatalf("usage_five_hour = %+v", usage)
	}
	if tokens := requireMetric(t, snap, "tokens_five_hour"); tokens.Window != "5h" || tokens.Unit != "tokens" {
		t.Fatalf("tokens_five_hour = %+v", tokens)
	}
	if usage := requireMetric(t, snap, "usage_seven_day"); usage.Window != "7d" || usage.Used == nil || *usage.Used != 20 {
		t.Fatalf("usage_seven_day = %+v", usage)
	}
	if tokens := requireMetric(t, snap, "tokens_seven_day"); tokens.Window != "7d" || tokens.Unit != "tokens" ||
		tokens.Limit == nil || *tokens.Limit != 5000 {
		t.Fatalf("tokens_seven_day = %+v", tokens)
	}
	for _, key := range []string{"credits_five_hour", "credits_seven_day"} {
		if _, ok := snap.Metrics[key]; ok {
			t.Fatalf("%s should not be emitted for TOKENS_LIMIT", key)
		}
	}
}

func TestApplyQuotaData_UnsupportedWindowNotMislabeled(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "credit limit unsupported duration",
			payload: `{"limits":[{"type":"CREDIT_LIMIT","unit":3,"number":1,"percentage":55,"usage":1000,"currentValue":550}]}`,
		},
		{
			name:    "token limit missing number",
			payload: `{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":55,"usage":1000,"currentValue":550}]}`,
		},
		{
			name:    "credit limit missing unit",
			payload: `{"limits":[{"type":"CREDIT_LIMIT","number":5,"percentage":55,"usage":1000,"currentValue":550}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := core.NewUsageSnapshot("zai", "acct")
			var state providerState
			if !applyQuotaData(json.RawMessage(tt.payload), &snap, &state) {
				t.Fatalf("applyQuotaData = false, want true: a recognized limit type still signals quota data")
			}
			if len(snap.Metrics) != 0 {
				t.Fatalf("metrics = %v, want none for an unsupported window", snap.Metrics)
			}
			if state.limited || state.nearLimit {
				t.Fatalf("state = %+v, want unsupported window ignored", state)
			}
		})
	}
}

func TestApplyQuotaData_PercentageIsIntegerPercent(t *testing.T) {
	t.Run("percentage of one is one percent", func(t *testing.T) {
		snap := core.NewUsageSnapshot("zai", "acct")
		var state providerState
		payload := json.RawMessage(`{"limits":[{"type":"CREDIT_LIMIT","unit":6,"number":1,"percentage":1,"usage":1000,"currentValue":10}]}`)
		if !applyQuotaData(payload, &snap, &state) {
			t.Fatalf("applyQuotaData = false, want true")
		}
		weekly := requireMetric(t, snap, "usage_seven_day")
		if weekly.Used == nil || *weekly.Used != 1 {
			t.Fatalf("usage_seven_day.Used = %v, want 1", weekly.Used)
		}
		if state.limited || state.nearLimit {
			t.Fatalf("state = %+v, want 1%% to stay far from the limit", state)
		}
	})

	t.Run("missing percentage falls back to currentValue over usage", func(t *testing.T) {
		snap := core.NewUsageSnapshot("zai", "acct")
		var state providerState
		payload := json.RawMessage(`{"limits":[{"type":"CREDIT_LIMIT","unit":3,"number":5,"usage":1000,"currentValue":250}]}`)
		if !applyQuotaData(payload, &snap, &state) {
			t.Fatalf("applyQuotaData = false, want true")
		}
		fiveHour := requireMetric(t, snap, "usage_five_hour")
		if fiveHour.Used == nil || *fiveHour.Used != 25 {
			t.Fatalf("usage_five_hour.Used = %v, want 25", fiveHour.Used)
		}
	})
}

func TestApplyQuotaData_WeeklyPercentageDrivesNearLimit(t *testing.T) {
	snap := core.NewUsageSnapshot("zai", "acct")
	var state providerState
	payload := json.RawMessage(`{"limits":[{"type":"CREDIT_LIMIT","unit":6,"number":1,"percentage":85,"usage":1000,"currentValue":850}]}`)
	if !applyQuotaData(payload, &snap, &state) {
		t.Fatalf("applyQuotaData = false, want true")
	}
	if !state.nearLimit || state.limited {
		t.Fatalf("state = %+v, want near-limit from the weekly window", state)
	}
}

func TestFetch_QuotaLimit_CreditLimitPlan(t *testing.T) {
	const resetMS = int64(1798761600000)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-5"}]}`))
		case "/api/monitor/usage/quota/limit":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"success":true,"code":0,"msg":"ok","data":{"limits":[
				{"type":"CREDIT_LIMIT","unit":3,"number":5,"usage":200,"currentValue":0,"percentage":0,"remaining":200},
				{"type":"CREDIT_LIMIT","unit":6,"number":1,"usage":1000,"currentValue":250,"percentage":25,"remaining":750,"nextResetTime":%d}
			],"level":"pro"}}`, resetMS)))
		case "/api/monitor/usage/model-usage", "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v (message %q), want %v", snap.Status, snap.Message, core.StatusOK)
	}
	if got := snap.Raw["quota_api"]; got != "ok" {
		t.Fatalf("quota_api = %q, want ok", got)
	}
	if got := snap.Raw["subscription_status"]; got != "active" {
		t.Fatalf("subscription_status = %q, want active", got)
	}

	if weekly := requireMetric(t, snap, "usage_seven_day"); weekly.Used == nil || *weekly.Used != 25 {
		t.Fatalf("usage_seven_day = %+v, want 25%%", weekly)
	}
	if credits := requireMetric(t, snap, "credits_seven_day"); credits.Unit != "credits" || credits.Limit == nil || *credits.Limit != 1000 {
		t.Fatalf("credits_seven_day = %+v", credits)
	}
	if _, ok := snap.Resets["usage_seven_day"]; !ok {
		t.Fatalf("expected usage_seven_day reset")
	}
	if !strings.Contains(snap.Message, "7d usage 25%") {
		t.Fatalf("Message = %q, want the most-consumed weekly window summary", snap.Message)
	}
}

func TestDashboardWidget_ExposesWeeklyQuotaMetrics(t *testing.T) {
	widget := dashboardWidget()

	for _, key := range []string{"usage_seven_day", "tokens_seven_day", "credits_five_hour", "credits_seven_day"} {
		if !slices.Contains(widget.GaugePriority, key) {
			t.Fatalf("GaugePriority missing %q: %v", key, widget.GaugePriority)
		}
		if _, ok := widget.MetricLabelOverrides[key]; !ok {
			t.Fatalf("MetricLabelOverrides missing %q", key)
		}
		if _, ok := widget.CompactMetricLabelOverrides[key]; !ok {
			t.Fatalf("CompactMetricLabelOverrides missing %q", key)
		}
	}

	for _, key := range []string{"credits_seven_day", "tokens_seven_day"} {
		surfaced := false
		for _, row := range widget.CompactRows {
			for _, metricKey := range row.Keys {
				if metricKey == key {
					surfaced = true
				}
			}
		}
		if !surfaced {
			t.Fatalf("compact rows must surface %q", key)
		}
	}
}

func TestDashboardWidget_UsageGaugesPrecedeBalances(t *testing.T) {
	widget := dashboardWidget()
	if widget.GaugeMaxLines != 2 {
		t.Fatalf("GaugeMaxLines = %d, want 2", widget.GaugeMaxLines)
	}

	// A credits plan with a non-zero balance: both balance gauges are
	// gauge-eligible, so if they precede the quota windows they consume both
	// lines and hide the weekly usage gauge. The first two gauge-eligible
	// entries in GaugePriority must therefore be the 5h and 7d windows.
	used := func(v float64) *float64 { return core.Float64Ptr(v) }
	metrics := map[string]core.Metric{
		"spend_limit":     {Used: used(27.5), Limit: used(100), Remaining: used(72.5), Unit: "USD"},
		"plan_spend":      {Used: used(27.5), Limit: used(100), Remaining: used(72.5), Unit: "USD"},
		"credit_balance":  {Used: used(27.5), Limit: used(100), Remaining: used(72.5), Unit: "USD"},
		"usage_five_hour": {Used: used(0), Limit: used(100), Unit: "%", Window: "5h"},
		"usage_seven_day": {Used: used(25), Limit: used(100), Unit: "%", Window: "7d"},
		"credits_five_hour": {
			Used: used(0), Limit: used(200), Remaining: used(200), Unit: "credits", Window: "5h",
		},
		"credits_seven_day": {
			Used: used(250), Limit: used(1000), Remaining: used(750), Unit: "credits", Window: "7d",
		},
	}

	var gauges []string
	for _, key := range widget.GaugePriority {
		met, ok := metrics[key]
		if !ok {
			continue
		}
		if core.MetricUsedPercent(key, met) < 0 {
			continue
		}
		gauges = append(gauges, key)
		if len(gauges) == widget.GaugeMaxLines {
			break
		}
	}

	if len(gauges) != 2 || gauges[0] != "usage_five_hour" || gauges[1] != "usage_seven_day" {
		t.Fatalf("first %d gauges = %v, want [usage_five_hour usage_seven_day]", widget.GaugeMaxLines, gauges)
	}
}

func TestApplyQuotaData_MixedLegacyAndWindowedRowsExplicitWins(t *testing.T) {
	legacy := `{"type":"TOKENS_LIMIT","percentage":45,"usage":1000,"currentValue":450,"nextResetTime":1500000000000}`
	windowed := `{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":12,"usage":2000,"currentValue":240,"nextResetTime":1798761600000}`

	for _, tc := range []struct {
		name    string
		payload string
	}{
		{name: "legacy after windowed", payload: `{"limits":[` + windowed + `,` + legacy + `]}`},
		{name: "legacy before windowed", payload: `{"limits":[` + legacy + `,` + windowed + `]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := core.NewUsageSnapshot("zai", "acct")
			var state providerState
			if !applyQuotaData(json.RawMessage(tc.payload), &snap, &state) {
				t.Fatalf("applyQuotaData = false, want true")
			}

			usage := requireMetric(t, snap, "usage_five_hour")
			if usage.Window != "5h" || usage.Used == nil || *usage.Used != 12 {
				t.Fatalf("usage_five_hour = %+v, want the windowed row's 12%%", usage)
			}
			tokens := requireMetric(t, snap, "tokens_five_hour")
			if tokens.Limit == nil || *tokens.Limit != 2000 || tokens.Used == nil || *tokens.Used != 240 {
				t.Fatalf("tokens_five_hour = %+v, want the windowed row's token split", tokens)
			}
			reset, ok := snap.Resets["usage_five_hour"]
			if !ok {
				t.Fatalf("expected usage_five_hour reset")
			}
			if want := time.Unix(1798761600000/1000, 0).UTC(); !reset.Equal(want) {
				t.Fatalf("usage_five_hour reset = %v, want %v (windowed row)", reset, want)
			}
		})
	}
}

// zaiModelUsageRollupFixture mirrors the model-usage payload shape observed in
// production, with synthetic values: a per-model time series (modelDataList), a
// summary list repeated at two levels, and whole-window rollups (totalUsage).
// The window holds 7 calls and 1200 tokens, and the per-model totals (800 +
// 400) add up to the token rollup. Token totals must never be read as request
// counts, and the repeated summaries must not be summed more than once.
const zaiModelUsageRollupFixture = `{
	"success": true,
	"data": {
		"x_time": [1767225600, 1767312000],
		"modelCallCount": [4, 3],
		"tokensUsage": [700, 500],
		"totalUsage": {
			"totalModelCallCount": 7,
			"totalTokensUsage": 1200,
			"modelSummaryList": [
				{"modelName": "glm-4.6", "totalTokens": 800},
				{"modelName": "glm-4.5-air", "totalTokens": 400}
			]
		},
		"modelDataList": [
			{"modelName": "glm-4.6", "tokensUsage": [600, 200], "totalTokens": 800},
			{"modelName": "glm-4.5-air", "tokensUsage": [100, 300], "totalTokens": 400}
		],
		"modelSummaryList": [
			{"modelName": "glm-4.6", "totalTokens": 800},
			{"modelName": "glm-4.5-air", "totalTokens": 400}
		]
	}
}`

func TestExtractUsageSamples_ModelRollupsAreNotRequestsOrDuplicated(t *testing.T) {
	var envelope monitorEnvelope
	if err := json.Unmarshal([]byte(zaiModelUsageRollupFixture), &envelope); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	samples := extractUsageSamples(envelope.Data, "model")

	named := map[string]usageSample{}
	rollups := 0
	var rollupRequests, rollupTokens float64
	for _, sample := range samples {
		if sample.Aggregate {
			rollups++
			rollupRequests += sample.Requests
			rollupTokens += sample.Total
			continue
		}
		named[sample.Name] = sample
	}

	if len(named) != 2 {
		t.Fatalf("named model samples = %v, want exactly the two modelDataList entries", core.SortedStringKeys(named))
	}
	if _, ok := named["totalTokensUsage"]; ok {
		t.Fatalf("a token rollup key leaked into the per-model rows: %v", core.SortedStringKeys(named))
	}
	for name, sample := range named {
		if sample.Requests != 0 {
			t.Fatalf("model %q requests = %v, want 0 (no per-model call counts in this payload)", name, sample.Requests)
		}
		if sample.Total != 800 && sample.Total != 400 {
			t.Fatalf("model %q total = %v, want the per-model total once", name, sample.Total)
		}
	}

	if rollups != 2 {
		t.Fatalf("aggregate rollups = %d, want 2 (call count + token total)", rollups)
	}
	if rollupRequests != 7 {
		t.Fatalf("rollup requests = %v, want 7 from totalModelCallCount", rollupRequests)
	}
	if rollupTokens != 1200 {
		t.Fatalf("rollup tokens = %v, want 1200 from totalTokensUsage", rollupTokens)
	}
}

func TestFetch_ModelUsageRollupShape_ReportsRollupTotals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.6"},{"id":"glm-4.5-air"}]}`))
		case "/api/monitor/usage/quota/limit":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":6,"number":1,"percentage":25,"usage":1000,"currentValue":250}]}}`))
		case "/api/monitor/usage/model-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(zaiModelUsageRollupFixture))
		case "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v (message %q), want %v", snap.Status, snap.Message, core.StatusOK)
	}

	if req := requireMetric(t, snap, "window_requests"); req.Used == nil || *req.Used != 7 {
		t.Fatalf("window_requests = %+v, want 7 calls (not the 1200-token total)", req)
	}
	if req := requireMetric(t, snap, "7d_requests"); req.Used == nil || *req.Used != 7 {
		t.Fatalf("7d_requests = %+v, want 7", req)
	}
	if tok := requireMetric(t, snap, "window_tokens"); tok.Used == nil || *tok.Used != 1200 {
		t.Fatalf("window_tokens = %+v, want 1200 counted once", tok)
	}
	if tok := requireMetric(t, snap, "7d_tokens"); tok.Used == nil || *tok.Used != 1200 {
		t.Fatalf("7d_tokens = %+v, want 1200 counted once", tok)
	}

	if metric := requireMetric(t, snap, "model_glm-4_6_total_tokens"); metric.Used == nil || *metric.Used != 800 {
		t.Fatalf("model_glm-4_6_total_tokens = %+v", metric)
	}
	if metric := requireMetric(t, snap, "model_glm-4_5-air_total_tokens"); metric.Used == nil || *metric.Used != 400 {
		t.Fatalf("model_glm-4_5-air_total_tokens = %+v", metric)
	}

	for key := range snap.Metrics {
		if strings.Contains(key, "totalTokensUsage") || strings.Contains(key, "totalModelCallCount") ||
			strings.Contains(key, "modelSummaryList") || strings.Contains(key, "modelDataList") {
			t.Fatalf("rollup key leaked into a metric: %s", key)
		}
	}

	if got := snap.Raw["activity_models"]; got != "2" {
		t.Fatalf("activity_models = %q, want 2 (rollups are not models)", got)
	}
	if len(snap.ModelUsage) != 2 {
		t.Fatalf("ModelUsage records = %d, want 2", len(snap.ModelUsage))
	}
}

// zaiToolUsageRollupFixture mirrors the tool-usage payload shape observed in
// production, with synthetic values: per-tool time series arrays, whole-window
// rollups under totalUsage (one per tool plus a totalSearchMcpCount group
// total), the richer toolDataList, and a toolSummaryList repeated at the top
// level and inside totalUsage. The window holds 9 tool calls (6 network
// searches + 3 web reads). Counts must not be read out of the total* rollups as
// pseudo tools, and the repeated summary must not be summed on top of
// toolDataList.
const zaiToolUsageRollupFixture = `{
	"success": true,
	"data": {
		"x_time": [1767225600, 1767312000],
		"networkSearchCount": [4, 2],
		"webReadMcpCount": [2, 1],
		"zreadMcpCount": [0, 0],
		"totalUsage": {
			"totalNetworkSearchCount": 6,
			"totalWebReadMcpCount": 3,
			"totalZreadMcpCount": 0,
			"totalSearchMcpCount": 9,
			"toolDetails": [],
			"toolSummaryList": [
				{"toolName": "network_search", "totalCalls": 6},
				{"toolName": "web_read", "totalCalls": 3}
			]
		},
		"toolDataList": [
			{"toolName": "network_search", "countsUsage": [4, 2], "totalCalls": 6},
			{"toolName": "web_read", "countsUsage": [2, 1], "totalCalls": 3}
		],
		"toolSummaryList": [
			{"toolName": "network_search", "totalCalls": 6},
			{"toolName": "web_read", "totalCalls": 3}
		]
	}
}`

func TestExtractUsageSamples_ToolRollupsAreNotToolsOrDuplicated(t *testing.T) {
	var envelope monitorEnvelope
	if err := json.Unmarshal([]byte(zaiToolUsageRollupFixture), &envelope); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	samples := extractUsageSamples(envelope.Data, "tool")

	named := map[string]float64{}
	rollupCount := 0
	rollupCalls := 0.0
	for _, sample := range samples {
		if sample.Aggregate {
			if sample.Name != "" {
				t.Fatalf("rollup sample inherited a pseudo tool name %q", sample.Name)
			}
			rollupCount++
			rollupCalls += sample.Requests
			continue
		}
		named[sample.Name] += sample.Requests
	}

	if len(named) != 2 {
		t.Fatalf("named tool samples = %v, want exactly the two toolDataList entries", core.SortedStringKeys(named))
	}
	if named["network_search"] != 6 || named["web_read"] != 3 {
		t.Fatalf("named tool calls = %v, want network_search 6 and web_read 3 (toolDataList read once)", named)
	}
	for _, pseudo := range []string{"totalNetworkSearchCount", "totalWebReadMcpCount", "totalSearchMcpCount", "toolSummaryList", "toolDataList", "toolDetails"} {
		if _, ok := named[pseudo]; ok {
			t.Fatalf("payload key %q leaked into the per-tool rows: %v", pseudo, core.SortedStringKeys(named))
		}
	}
	if rollupCount != 3 || rollupCalls != 18 {
		t.Fatalf("tool rollups = %d samples / %v calls, want 3 / 18 (6+3+9, zero zread dropped)", rollupCount, rollupCalls)
	}
}

func TestFetch_ToolUsageRollupShape_CountsEachToolOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.6"}]}`))
		case "/api/monitor/usage/quota/limit":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":6,"number":1,"percentage":25,"usage":1000,"currentValue":250}]}}`))
		case "/api/monitor/usage/model-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":[]}`))
		case "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(zaiToolUsageRollupFixture))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := snap.Raw["tool_usage_api"]; got != "ok" {
		t.Fatalf("tool_usage_api = %q, want ok", got)
	}

	if metric := requireMetric(t, snap, "7d_tool_calls"); metric.Used == nil || *metric.Used != 9 {
		t.Fatalf("7d_tool_calls = %+v, want 9 (6+3 read once, not the duplicated summaries or the overlapping rollups)", metric)
	}
	if metric := requireMetric(t, snap, "tool_network_search"); metric.Used == nil || *metric.Used != 6 {
		t.Fatalf("tool_network_search = %+v, want 6", metric)
	}
	if metric := requireMetric(t, snap, "tool_web_read"); metric.Used == nil || *metric.Used != 3 {
		t.Fatalf("tool_web_read = %+v, want 3", metric)
	}
	for key := range snap.Metrics {
		if strings.Contains(key, "totalNetworkSearchCount") || strings.Contains(key, "totalSearchMcpCount") ||
			strings.Contains(key, "totalWebReadMcpCount") || strings.Contains(key, "toolSummaryList") ||
			strings.Contains(key, "toolDataList") || strings.Contains(key, "unknown") {
			t.Fatalf("payload key leaked into a metric: %s", key)
		}
	}
	if summary := snap.Raw["tool_usage"]; !strings.Contains(summary, "network search: 6 calls") {
		t.Fatalf("tool_usage = %q, want the per-tool counts once", summary)
	}
}

func TestFetch_ToolUsageRollupsWithoutBreakdown_ReportsWindowTotal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.6"}]}`))
		case "/api/monitor/usage/quota/limit", "/api/monitor/usage/model-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":[]}`))
		case "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":{
				"totalUsage": {"totalNetworkSearchCount": 6, "totalWebReadMcpCount": 3, "totalSearchMcpCount": 9},
				"toolDataList": [],
				"toolSummaryList": []
			}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	// The overlapping rollups are not summed (6+3+9 would double the total);
	// the largest is the best available window figure when the per-tool
	// breakdown is empty.
	if metric := requireMetric(t, snap, "7d_tool_calls"); metric.Used == nil || *metric.Used != 9 {
		t.Fatalf("7d_tool_calls = %+v, want 9 from the group rollup", metric)
	}
	for key := range snap.Metrics {
		if strings.HasPrefix(key, "tool_") && key != "tool_calls_today" {
			t.Fatalf("rollup-only payload produced a pseudo tool metric: %s", key)
		}
	}
}

func TestFetch_ToolUsageRollupsRescueUnreadableBreakdown(t *testing.T) {
	// If the per-tool list is present but carries no counts the extractor can
	// read, the window total still comes from the payload's rollups instead of
	// being reported as nothing.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/coding/paas/v4/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-4.6"}]}`))
		case "/api/monitor/usage/quota/limit", "/api/monitor/usage/model-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":[]}`))
		case "/api/monitor/usage/tool-usage":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":{
				"totalUsage": {"totalNetworkSearchCount": 6, "totalWebReadMcpCount": 3, "totalSearchMcpCount": 9},
				"toolDataList": [
					{"toolName": "network_search", "callsUsage": [4, 2]},
					{"toolName": "web_read", "callsUsage": [2, 1]}
				],
				"toolSummaryList": []
			}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("TEST_ZAI_KEY", "test-zai-key")

	p := New()
	snap, err := p.Fetch(context.Background(), testAccount(server.URL))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if metric := requireMetric(t, snap, "7d_tool_calls"); metric.Used == nil || *metric.Used != 9 {
		t.Fatalf("7d_tool_calls = %+v, want the 9-call rollup when the breakdown has no readable counts", metric)
	}
}

func TestExtractUsageSamples_ZeroTotalsPayloadHasNoRows(t *testing.T) {
	// A tool-usage payload whose window had no tool calls (zero rollups, empty
	// breakdown lists) must not fabricate tool rows.
	const payload = `{
		"x_time": [1767225600, 1767312000],
		"networkSearchCount": [0, 0],
		"webReadMcpCount": [0, 0],
		"zreadMcpCount": [0, 0],
		"totalUsage": {
			"totalNetworkSearchCount": 0,
			"totalWebReadMcpCount": 0,
			"totalZreadMcpCount": 0,
			"totalSearchMcpCount": 0,
			"toolDetails": [],
			"toolSummaryList": []
		},
		"toolDataList": [],
		"toolSummaryList": []
	}`

	if samples := extractUsageSamples(json.RawMessage(payload), "tool"); len(samples) != 0 {
		t.Fatalf("samples = %d, want none (all-zero payload)", len(samples))
	}
}

func TestExtractUsageSamples_DuplicatedSummaryLists(t *testing.T) {
	for _, tc := range []struct {
		name       string
		payload    string
		kind       string
		wantNamed  int
		wantTotals map[string]float64
	}{
		{
			name: "model summary repeated without modelDataList",
			kind: "model",
			payload: `{"data":{
				"totalUsage":{"modelSummaryList":[{"modelName":"glm-5.3","totalTokens":300},{"modelName":"glm-5.3-flash","totalTokens":100}]},
				"modelSummaryList":[{"modelName":"glm-5.3","totalTokens":300},{"modelName":"glm-5.3-flash","totalTokens":100}]
			}}`,
			wantNamed:  2,
			wantTotals: map[string]float64{"glm-5.3": 300, "glm-5.3-flash": 100},
		},
		{
			name: "tool summary repeated without toolDataList",
			kind: "tool",
			payload: `{"data":{
				"totalUsage":{"toolSummaryList":[{"toolName":"network_search","totalCalls":6},{"toolName":"web_read","totalCalls":3}]},
				"toolSummaryList":[{"toolName":"network_search","totalCalls":6},{"toolName":"web_read","totalCalls":3}]
			}}`,
			wantNamed:  2,
			wantTotals: map[string]float64{"network_search": 6, "web_read": 3},
		},
		{
			name: "different summary copies are both kept",
			kind: "model",
			payload: `{"data":{
				"totalUsage":{"modelSummaryList":[{"modelName":"glm-5.3","totalTokens":300}]},
				"modelSummaryList":[{"modelName":"glm-5.3-flash","totalTokens":100}]
			}}`,
			wantNamed:  2,
			wantTotals: map[string]float64{"glm-5.3": 300, "glm-5.3-flash": 100},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples := extractUsageSamples(json.RawMessage(tc.payload), tc.kind)

			got := map[string]float64{}
			for _, sample := range samples {
				if sample.Aggregate {
					continue
				}
				value := sample.Requests
				if tc.kind == "model" {
					value = sample.Total
				}
				got[sample.Name] += value
			}
			if len(got) != tc.wantNamed {
				t.Fatalf("named samples = %v, want %d", got, tc.wantNamed)
			}
			for name, want := range tc.wantTotals {
				if got[name] != want {
					t.Fatalf("sample %q = %v, want %v (all samples: %v)", name, got[name], want, got)
				}
			}
		})
	}
}

func TestResolveAPIBases(t *testing.T) {
	tests := []struct {
		name        string
		acct        core.AccountConfig
		wantCoding  string
		wantMonitor string
		wantRegion  string
	}{
		{
			name:        "default global",
			acct:        core.AccountConfig{},
			wantCoding:  defaultGlobalCodingBaseURL,
			wantMonitor: defaultGlobalMonitorBaseURL,
			wantRegion:  "global",
		},
		{
			name: "plan china",
			acct: core.AccountConfig{
				RuntimeHints: map[string]string{"plan_type": "glm_coding_plan_china"},
			},
			wantCoding:  defaultChinaCodingBaseURL,
			wantMonitor: defaultChinaMonitorBaseURL,
			wantRegion:  "china",
		},
		{
			name: "custom root base",
			acct: core.AccountConfig{
				BaseURL: "https://example.com",
			},
			wantCoding:  "https://example.com/api/coding/paas/v4",
			wantMonitor: "https://example.com",
			wantRegion:  "global",
		},
		{
			name: "custom coding base path",
			acct: core.AccountConfig{
				BaseURL: "https://example.com/api/coding/paas/v4",
			},
			wantCoding:  "https://example.com/api/coding/paas/v4",
			wantMonitor: "https://example.com",
			wantRegion:  "global",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCoding, gotMonitor, gotRegion := resolveAPIBases(tt.acct)
			if gotCoding != tt.wantCoding {
				t.Fatalf("coding = %q, want %q", gotCoding, tt.wantCoding)
			}
			if gotMonitor != tt.wantMonitor {
				t.Fatalf("monitor = %q, want %q", gotMonitor, tt.wantMonitor)
			}
			if gotRegion != tt.wantRegion {
				t.Fatalf("region = %q, want %q", gotRegion, tt.wantRegion)
			}
		})
	}
}

func TestMain(m *testing.M) {
	_ = os.Unsetenv("TEST_ZAI_KEY")
	os.Exit(m.Run())
}
