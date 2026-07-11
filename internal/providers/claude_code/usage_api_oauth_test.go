package claude_code

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// writeCredentials drops a fake ~/.claude/.credentials.json under a temp HOME
// and points os.UserHomeDir at it via HOME.
func writeCredentials(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
}

func TestReadClaudeCodeOAuthToken(t *testing.T) {
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	past := strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10)

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "valid unexpired token",
			body: `{"claudeAiOauth":{"accessToken":"tok-123","expiresAt":` + future + `}}`,
			want: "tok-123",
		},
		{
			name: "no expiry field is accepted",
			body: `{"claudeAiOauth":{"accessToken":"tok-abc"}}`,
			want: "tok-abc",
		},
		{
			name:    "expired token rejected",
			body:    `{"claudeAiOauth":{"accessToken":"tok-123","expiresAt":` + past + `}}`,
			wantErr: true,
		},
		{
			name:    "empty token rejected",
			body:    `{"claudeAiOauth":{"accessToken":""}}`,
			wantErr: true,
		},
		{
			name:    "malformed json rejected",
			body:    `{not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeCredentials(t, tt.body)
			got, err := readClaudeCodeOAuthToken()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got token %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("token = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadClaudeCodeOAuthToken_MissingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no .claude/.credentials.json
	if _, err := readClaudeCodeOAuthToken(); err == nil {
		t.Fatal("expected error for missing credentials file")
	}
}

func TestFetchUsageAPIOAuth(t *testing.T) {
	var gotAuth, gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":42,"resets_at":"2999-01-01T00:00:00Z"},` +
			`"seven_day":{"utilization":13,"resets_at":"2999-01-01T00:00:00Z"}}`))
	}))
	defer srv.Close()

	old := oauthUsageURL
	oauthUsageURL = srv.URL
	defer func() { oauthUsageURL = old }()

	futureMs := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"tok-xyz","expiresAt":`+futureMs+`}}`)

	usage, err := fetchUsageAPIOAuth(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.FiveHour == nil || usage.FiveHour.Utilization != 42 {
		t.Fatalf("five_hour utilization = %+v, want 42", usage.FiveHour)
	}
	if usage.SevenDay == nil || usage.SevenDay.Utilization != 13 {
		t.Fatalf("seven_day utilization = %+v, want 13", usage.SevenDay)
	}
	if gotAuth != "Bearer tok-xyz" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer tok-xyz")
	}
	if gotBeta != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta header = %q, want %q", gotBeta, "oauth-2025-04-20")
	}
}

func TestFetchUsageAPIOAuth_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	defer srv.Close()

	old := oauthUsageURL
	oauthUsageURL = srv.URL
	defer func() { oauthUsageURL = old }()

	futureMs := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"tok","expiresAt":`+futureMs+`}}`)

	if _, err := fetchUsageAPIOAuth(context.Background()); err == nil {
		t.Fatal("expected error on 401 response")
	}
}
