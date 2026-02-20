package gemini_cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestFetch_QuotaAPI(t *testing.T) {
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
			fmt.Fprint(w, `{"cloudaicompanionProject":"test-project-123","currentTier":{"id":"FREE"}}`)
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

	projectID, err := loadCodeAssistWithEndpoint(ctx, accessToken, "", server.URL)
	if err != nil {
		t.Fatalf("loadCodeAssist() error: %v", err)
	}
	if projectID != "test-project-123" {
		t.Errorf("projectID = %q, want test-project-123", projectID)
	}
	if !loadCalled {
		t.Error("loadCodeAssist endpoint was not called")
	}

	quota, err := retrieveUserQuotaWithEndpoint(ctx, accessToken, projectID, server.URL)
	if err != nil {
		t.Fatalf("retrieveUserQuota() error: %v", err)
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
