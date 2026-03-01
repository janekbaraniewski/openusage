package zai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")

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
				ExtraData: map[string]string{"plan_type": "glm_coding_plan_china"},
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
