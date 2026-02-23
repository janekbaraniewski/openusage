package gemini_cli

import (
	"context"
	"encoding/json"
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

func TestFetch_ReadsLocalData(t *testing.T) {
	tmpDir := t.TempDir()

	creds := oauthCreds{
		AccessToken: "ya29.test",
		Scope:       "openid https://www.googleapis.com/auth/cloud-platform",
		TokenType:   "Bearer",
		ExpiryDate:  4102444800000, // 2100-01-01 in millis
	}
	writeJSON(t, filepath.Join(tmpDir, "oauth_creds.json"), creds)

	accounts := googleAccounts{
		Active: "test@example.com",
		Old:    []string{"old@example.com"},
	}
	writeJSON(t, filepath.Join(tmpDir, "google_accounts.json"), accounts)

	settings := map[string]interface{}{
		"security": map[string]interface{}{
			"auth": map[string]interface{}{
				"selectedType": "oauth-personal",
			},
		},
	}
	writeJSON(t, filepath.Join(tmpDir, "settings.json"), settings)

	os.WriteFile(filepath.Join(tmpDir, "installation_id"), []byte("test-uuid-1234"), 0644)

	convDir := filepath.Join(tmpDir, "antigravity", "conversations")
	os.MkdirAll(convDir, 0755)
	os.WriteFile(filepath.Join(convDir, "session1.pb"), []byte("data"), 0644)
	os.WriteFile(filepath.Join(convDir, "session2.pb"), []byte("data"), 0644)
	os.WriteFile(filepath.Join(convDir, "session3.pb"), []byte("data"), 0644)

	p := New()
	acct := core.AccountConfig{
		ID:        "test-gemini-cli",
		Provider:  "gemini_cli",
		ExtraData: map[string]string{"config_dir": tmpDir},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK; message = %s", snap.Status, snap.Message)
	}

	if snap.Raw["account_email"] != "test@example.com" {
		t.Errorf("account_email = %q, want test@example.com", snap.Raw["account_email"])
	}

	if snap.Raw["oauth_status"] != "valid" {
		t.Errorf("oauth_status = %q, want valid", snap.Raw["oauth_status"])
	}

	if snap.Raw["auth_type"] != "oauth-personal" {
		t.Errorf("auth_type = %q, want oauth-personal", snap.Raw["auth_type"])
	}

	if snap.Raw["installation_id"] != "test-uuid-1234" {
		t.Errorf("installation_id = %q, want test-uuid-1234", snap.Raw["installation_id"])
	}

	conv, ok := snap.Metrics["total_conversations"]
	if !ok {
		t.Fatal("missing total_conversations metric")
	}
	if conv.Used == nil || *conv.Used != 3 {
		t.Errorf("total_conversations = %v, want 3", conv.Used)
	}
}

func TestFetch_ExpiredOAuth(t *testing.T) {
	tmpDir := t.TempDir()

	creds := oauthCreds{
		AccessToken: "ya29.expired",
		ExpiryDate:  1000000000000, // 2001 — long expired
	}
	writeJSON(t, filepath.Join(tmpDir, "oauth_creds.json"), creds)

	p := New()
	acct := core.AccountConfig{
		ID:        "test-expired",
		ExtraData: map[string]string{"config_dir": tmpDir},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want auth; message = %s", snap.Status, snap.Message)
	}

	if snap.Raw["oauth_status"] != "expired" {
		t.Errorf("oauth_status = %q, want expired", snap.Raw["oauth_status"])
	}
}

func TestFetch_NoData(t *testing.T) {
	tmpDir := t.TempDir()

	p := New()
	acct := core.AccountConfig{
		ID:        "test-empty",
		ExtraData: map[string]string{"config_dir": tmpDir},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusError {
		t.Errorf("Status = %v, want error", snap.Status)
	}
}

func TestFetch_UsageAPI(t *testing.T) {
	var tokenCalled, loadCalled, quotaCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalled = true
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"ya29.fresh","expires_in":3600,"token_type":"Bearer"}`)
		case "/v1internal:loadCodeAssist":
			loadCalled = true
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"cloudaicompanionProject":"test-project-123","currentTier":{"id":"free-tier","name":"Free Tier"}}`)
		case "/v1internal:retrieveUserQuota":
			quotaCalled = true
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"buckets":[
				{"modelId":"gemini-2.5-flash","remainingFraction":0.75,"resetTime":"2099-01-01T00:00:00Z","tokenType":"requests"},
				{"modelId":"gemini-2.5-pro","remainingFraction":0.10,"resetTime":"2099-01-01T00:00:00Z","tokenType":"requests"}
			]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()

	creds := oauthCreds{
		AccessToken:  "ya29.expired",
		ExpiryDate:   1000000000000, // 2001 — expired
		RefreshToken: "1//refresh-token-test",
	}
	writeJSON(t, filepath.Join(tmpDir, "oauth_creds.json"), creds)

	accounts := googleAccounts{Active: "test@example.com"}
	writeJSON(t, filepath.Join(tmpDir, "google_accounts.json"), accounts)

	ctx := context.Background()

	accessToken, err := refreshAccessTokenWithEndpoint(ctx, creds.RefreshToken, server.URL+"/token")
	if err != nil {
		t.Fatalf("refreshAccessToken() error: %v", err)
	}
	if accessToken != "ya29.fresh" {
		t.Errorf("accessToken = %q, want ya29.fresh", accessToken)
	}
	if !tokenCalled {
		t.Error("token endpoint was not called")
	}

	resp, err := loadCodeAssistDetailsWithEndpoint(ctx, accessToken, "", server.URL)
	if err != nil {
		t.Fatalf("loadCodeAssist() error: %v", err)
	}
	projectID := resp.CloudAICompanionProject
	if projectID != "test-project-123" {
		t.Errorf("projectID = %q, want test-project-123", projectID)
	}
	if !loadCalled {
		t.Error("loadCodeAssist endpoint was not called")
	}

	quota, method, err := retrieveUserQuotaWithEndpoint(ctx, accessToken, projectID, server.URL)
	if err != nil {
		t.Fatalf("retrieveUserQuota() error: %v", err)
	}
	if method != "retrieveUserQuota" {
		t.Fatalf("method = %q, want retrieveUserQuota", method)
	}
	if len(quota.Buckets) != 2 {
		t.Fatalf("got %d buckets, want 2", len(quota.Buckets))
	}
	if !quotaCalled {
		t.Error("retrieveUserQuota endpoint was not called")
	}

	flash := quota.Buckets[0]
	if flash.ModelID != "gemini-2.5-flash" {
		t.Errorf("bucket[0].ModelID = %q, want gemini-2.5-flash", flash.ModelID)
	}
	if flash.RemainingFraction == nil || *flash.RemainingFraction != 0.75 {
		t.Errorf("bucket[0].RemainingFraction = %v, want 0.75", flash.RemainingFraction)
	}

	pro := quota.Buckets[1]
	if pro.ModelID != "gemini-2.5-pro" {
		t.Errorf("bucket[1].ModelID = %q, want gemini-2.5-pro", pro.ModelID)
	}
	if pro.RemainingFraction == nil || *pro.RemainingFraction != 0.10 {
		t.Errorf("bucket[1].RemainingFraction = %v, want 0.10", pro.RemainingFraction)
	}
}

func TestFetch_UsageAPI_DoesNotFallbackToLegacyMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:retrieveUserQuota":
			http.NotFound(w, r)
		case "/v1internal:retrieveUserUsage":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"buckets":[{"modelId":"gemini-3-pro-preview","remainingFraction":0.02,"resetTime":"2099-01-01T00:00:00Z","tokenType":"REQUESTS"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, _, err := retrieveUserQuotaWithEndpoint(context.Background(), "test-token", "test-project", server.URL); err == nil {
		t.Fatalf("retrieveUserQuotaWithEndpoint() error = nil, want not found error")
	}
}

func TestFetch_SessionUsageBreakdowns(t *testing.T) {
	tmpDir := t.TempDir()

	creds := oauthCreds{
		AccessToken: "ya29.test",
		Scope:       "openid https://www.googleapis.com/auth/cloud-platform",
		TokenType:   "Bearer",
		ExpiryDate:  4102444800000, // 2100-01-01 in millis
	}
	writeJSON(t, filepath.Join(tmpDir, "oauth_creds.json"), creds)
	writeJSON(t, filepath.Join(tmpDir, "google_accounts.json"), googleAccounts{Active: "test@example.com"})
	writeJSON(t, filepath.Join(tmpDir, "settings.json"), map[string]any{
		"security": map[string]any{
			"auth": map[string]any{
				"selectedType": "oauth-personal",
			},
		},
	})

	chatDir := filepath.Join(tmpDir, "tmp", "proj-hash", "chats")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatalf("mkdir chat dir: %v", err)
	}

	chat := map[string]any{
		"sessionId":   "session-1",
		"startTime":   "2026-02-01T10:00:00Z",
		"lastUpdated": "2026-02-01T10:05:00Z",
		"messages": []map[string]any{
			{
				"type":      "user",
				"timestamp": "2026-02-01T10:00:10Z",
				"content":   "hello",
			},
			{
				"type":      "gemini",
				"timestamp": "2026-02-01T10:01:00Z",
				"model":     "gemini-3-flash-preview",
				"tokens": map[string]any{
					"input":    90,
					"output":   10,
					"cached":   0,
					"thoughts": 5,
					"tool":     0,
					"total":    100,
				},
				"toolCalls": []map[string]any{{"name": "run_command"}},
			},
			{
				"type":      "user",
				"timestamp": "2026-02-01T10:02:00Z",
				"content":   "more",
			},
			{
				"type":      "gemini",
				"timestamp": "2026-02-01T10:03:00Z",
				"model":     "gemini-3-flash-preview",
				"tokens": map[string]any{
					"input":    190,
					"output":   25,
					"cached":   20,
					"thoughts": 10,
					"tool":     0,
					"total":    220,
				},
				"toolCalls": []map[string]any{{"name": "web_search"}, {"name": "run_command"}},
			},
		},
	}
	writeJSON(t, filepath.Join(chatDir, "session-2026-02-01T10-00-aaaa1111.json"), chat)

	p := New()
	acct := core.AccountConfig{
		ID:        "test-gemini-cli",
		Provider:  "gemini_cli",
		ExtraData: map[string]string{"config_dir": tmpDir},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK (message: %s)", snap.Status, snap.Message)
	}

	if m, ok := snap.Metrics["model_gemini_3_flash_preview_input_tokens"]; !ok || m.Used == nil || *m.Used != 190 {
		t.Fatalf("model_gemini_3_flash_preview_input_tokens = %v, want 190", m.Used)
	}
	if m, ok := snap.Metrics["model_gemini_3_flash_preview_output_tokens"]; !ok || m.Used == nil || *m.Used != 25 {
		t.Fatalf("model_gemini_3_flash_preview_output_tokens = %v, want 25", m.Used)
	}
	if m, ok := snap.Metrics["client_cli_total_tokens"]; !ok || m.Used == nil || *m.Used != 220 {
		t.Fatalf("client_cli_total_tokens = %v, want 220", m.Used)
	}
	if m, ok := snap.Metrics["client_cli_sessions"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("client_cli_sessions = %v, want 1", m.Used)
	}
	if m, ok := snap.Metrics["messages_today"]; !ok || m.Used == nil || *m.Used != 2 {
		t.Fatalf("messages_today = %v, want 2", m.Used)
	}
	if m, ok := snap.Metrics["tool_calls_today"]; !ok || m.Used == nil || *m.Used != 3 {
		t.Fatalf("tool_calls_today = %v, want 3", m.Used)
	}
	// New tool metric checks
	if m, ok := snap.Metrics["tool_run_command"]; !ok || m.Used == nil || *m.Used != 2 {
		t.Fatalf("tool_run_command = %v, want 2", m.Used)
	}
	if m, ok := snap.Metrics["tool_web_search"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("tool_web_search = %v, want 1", m.Used)
	}

	if m, ok := snap.Metrics["7d_tokens"]; !ok || m.Used == nil || *m.Used != 220 {
		t.Fatalf("7d_tokens = %v, want 220", m.Used)
	}
	if m, ok := snap.Metrics["today_input_tokens"]; !ok || m.Used == nil || *m.Used != 190 {
		t.Fatalf("today_input_tokens = %v, want 190", m.Used)
	}
	if m, ok := snap.Metrics["today_output_tokens"]; !ok || m.Used == nil || *m.Used != 25 {
		t.Fatalf("today_output_tokens = %v, want 25", m.Used)
	}
	if m, ok := snap.Metrics["total_cached_tokens"]; !ok || m.Used == nil || *m.Used != 20 {
		t.Fatalf("total_cached_tokens = %v, want 20", m.Used)
	}
	if m, ok := snap.Metrics["model_gemini_3_flash_preview_requests"]; !ok || m.Used == nil || *m.Used != 2 {
		t.Fatalf("model_gemini_3_flash_preview_requests = %v, want 2", m.Used)
	}
	if m, ok := snap.Metrics["total_conversations"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("total_conversations = %v, want 1", m.Used)
	}
	if !strings.Contains(snap.Raw["model_usage"], "gemini-3-flash-preview") {
		t.Fatalf("model_usage = %q, expected model name", snap.Raw["model_usage"])
	}

	if m, ok := snap.Metrics["context_window"]; !ok || m.Used == nil || *m.Used != 220 {
		t.Fatalf("context_window used = %v, want 220", m.Used)
	}

	modelSeries := snap.DailySeries["tokens_model_gemini_3_flash_preview"]
	if len(modelSeries) != 1 || modelSeries[0].Value != 220 {
		t.Fatalf("tokens_model_gemini_3_flash_preview series = %+v, want one point at 220", modelSeries)
	}
	clientSeries := snap.DailySeries["tokens_client_cli"]
	if len(clientSeries) != 1 || clientSeries[0].Value != 220 {
		t.Fatalf("tokens_client_cli series = %+v, want one point at 220", clientSeries)
	}
}

func TestFetch_QuotaLimitMessageFallback(t *testing.T) {
	tmpDir := t.TempDir()

	writeJSON(t, filepath.Join(tmpDir, "oauth_creds.json"), oauthCreds{
		AccessToken: "ya29.test",
		ExpiryDate:  4102444800000,
		// No refresh token to force local-only mode.
	})
	writeJSON(t, filepath.Join(tmpDir, "google_accounts.json"), googleAccounts{Active: "test@example.com"})

	chatDir := filepath.Join(tmpDir, "tmp", "proj-hash", "chats")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatalf("mkdir chat dir: %v", err)
	}
	writeJSON(t, filepath.Join(chatDir, "session-2026-02-01T10-00-quota.json"), map[string]any{
		"sessionId":   "session-1",
		"startTime":   "2026-02-01T10:00:00Z",
		"lastUpdated": "2026-02-01T10:05:00Z",
		"messages": []map[string]any{
			{"type": "gemini", "timestamp": "2026-02-01T10:01:00Z", "content": "Usage limit reached for all Pro models.\n/stats for usage details"},
		},
	})

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:        "test-gemini-cli",
		Provider:  "gemini_cli",
		ExtraData: map[string]string{"config_dir": tmpDir},
	})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Fatalf("status = %v, want limited", snap.Status)
	}
	q, ok := snap.Metrics["quota"]
	if !ok || q.Used == nil || *q.Used != 100 {
		t.Fatalf("quota metric = %+v, want used=100", q)
	}
}

func TestApplyQuotaBuckets(t *testing.T) {
	snap := core.UsageSnapshot{
		Metrics: make(map[string]core.Metric),
		Resets:  make(map[string]time.Time),
		Raw:     make(map[string]string),
	}

	result := applyQuotaBuckets(&snap, []bucketInfo{
		{ModelID: "gemini-3-pro-preview", TokenType: "REQUESTS", RemainingFraction: float64Ptr(0.02), ResetTime: "2099-01-01T00:00:00Z"},
		{ModelID: "gemini-3-pro-preview_vertex", TokenType: "REQUESTS", RemainingFraction: float64Ptr(0.04), ResetTime: "2099-01-01T00:00:00Z"},
		{ModelID: "gemini-3-flash-preview", TokenType: "REQUESTS", RemainingFraction: float64Ptr(0.95), ResetTime: "2099-01-01T00:00:00Z"},
	})

	if result.modelCount != 2 {
		t.Fatalf("modelCount = %d, want 2", result.modelCount)
	}
	if result.worstFraction != 0.02 {
		t.Fatalf("worstFraction = %.2f, want 0.02", result.worstFraction)
	}

	quota, ok := snap.Metrics["quota"]
	if !ok || quota.Used == nil {
		t.Fatalf("missing quota metric: %+v", quota)
	}
	if *quota.Used != 98 {
		t.Fatalf("quota used = %.1f, want 98", *quota.Used)
	}

	if _, ok := snap.Metrics["quota_pro"]; !ok {
		t.Fatal("missing quota_pro metric")
	}
	if _, ok := snap.Metrics["quota_flash"]; !ok {
		t.Fatal("missing quota_flash metric")
	}
	if _, ok := snap.Metrics["quota_model_gemini_3_pro_preview_requests"]; !ok {
		t.Fatal("missing per-model quota metric")
	}
}

func TestFormatWindow(t *testing.T) {
	tests := []struct {
		name string
		dur  string
		want string
	}{
		{"30 minutes", "30m", "30m"},
		{"2 hours", "2h0m0s", "2h"},
		{"2h30m", "2h30m", "2h30m"},
		{"25 hours", "25h0m0s", "~1 day"},
		{"72 hours", "72h0m0s", "~3d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dur, err := time.ParseDuration(tt.dur)
			if err != nil {
				t.Fatalf("parseDuration(%q): %v", tt.dur, err)
			}
			got := formatWindow(dur)
			if got != tt.want {
				t.Errorf("formatWindow(%s) = %q, want %q", tt.dur, got, tt.want)
			}
		})
	}
}

func writeJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func float64Ptr(v float64) *float64 {
	return &v
}
