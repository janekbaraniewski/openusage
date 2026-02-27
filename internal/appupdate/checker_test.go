package appupdate

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeReleaseVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "valid with prefix", input: "v1.2.3", want: "v1.2.3"},
		{name: "valid without prefix", input: "1.2.3", want: "v1.2.3"},
		{name: "pre-release skipped", input: "v1.2.3-rc.1", want: ""},
		{name: "dev skipped", input: "dev", want: ""},
		{name: "empty skipped", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeReleaseVersion(tt.input)
			if got != tt.want {
				t.Fatalf("normalizeReleaseVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectInstallMethod(t *testing.T) {
	tests := []struct {
		name string
		path string
		want InstallMethod
	}{
		{
			name: "homebrew cellar",
			path: "/opt/homebrew/Cellar/openusage/1.2.3/bin/openusage",
			want: InstallMethodHomebrew,
		},
		{
			name: "go install default",
			path: "/Users/test/go/bin/openusage",
			want: InstallMethodGoInstall,
		},
		{
			name: "install script default",
			path: "/usr/local/bin/openusage",
			want: InstallMethodInstallScript,
		},
		{
			name: "scoop",
			path: "C:/Users/test/scoop/apps/openusage/current/openusage.exe",
			want: InstallMethodScoop,
		},
		{
			name: "unknown",
			path: "/tmp/openusage",
			want: InstallMethodUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectInstallMethod(tt.path)
			if got != tt.want {
				t.Fatalf("detectInstallMethod(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCheckUpdateAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.3.0"}`))
	}))
	defer server.Close()

	result, err := Check(context.Background(), CheckOptions{
		CurrentVersion:   "v1.2.0",
		ExecutablePath:   "/opt/homebrew/Cellar/openusage/1.2.0/bin/openusage",
		LatestReleaseURL: server.URL,
		HTTPClient:       server.Client(),
		Timeout:          time.Second,
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=true")
	}
	if result.LatestVersion != "v1.3.0" {
		t.Fatalf("LatestVersion = %q, want v1.3.0", result.LatestVersion)
	}
	if result.UpgradeHint != "brew upgrade janekbaraniewski/tap/openusage" {
		t.Fatalf("UpgradeHint = %q", result.UpgradeHint)
	}
}

func TestCheckNoUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0"}`))
	}))
	defer server.Close()

	result, err := Check(context.Background(), CheckOptions{
		CurrentVersion:   "v1.2.0",
		ExecutablePath:   "/usr/local/bin/openusage",
		LatestReleaseURL: server.URL,
		HTTPClient:       server.Client(),
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=false")
	}
}

func TestCheckSkipsDevVersion(t *testing.T) {
	result, err := Check(context.Background(), CheckOptions{
		CurrentVersion:   "dev",
		LatestReleaseURL: "http://127.0.0.1:0/does-not-matter",
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=false")
	}
	if result.CurrentVersion != "" {
		t.Fatalf("CurrentVersion = %q, want empty", result.CurrentVersion)
	}
}

func TestCheckLatestReleaseHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := Check(context.Background(), CheckOptions{
		CurrentVersion:   "v1.2.0",
		LatestReleaseURL: server.URL,
		HTTPClient:       server.Client(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckUnknownInstallMethodUsesActionableHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.3.0"}`))
	}))
	defer server.Close()

	result, err := Check(context.Background(), CheckOptions{
		CurrentVersion:   "v1.2.0",
		ExecutablePath:   "/tmp/openusage-old",
		LatestReleaseURL: server.URL,
		HTTPClient:       server.Client(),
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=true")
	}
	if result.UpgradeHint == "" {
		t.Fatal("expected non-empty upgrade hint")
	}
	if !strings.HasPrefix(result.UpgradeHint, "curl ") {
		t.Fatalf("UpgradeHint = %q, want curl install command", result.UpgradeHint)
	}
}

type captureTransport struct {
	lastReq *http.Request
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.lastReq = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v1.3.0"}`)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestCheckForwardsGitHubTokenHeaderForGitHubHTTPS(t *testing.T) {
	t.Setenv("OPENUSAGE_GITHUB_TOKEN", "test-token-123")

	transport := &captureTransport{}
	client := &http.Client{Transport: transport}

	result, err := Check(context.Background(), CheckOptions{
		CurrentVersion:   "v1.2.0",
		ExecutablePath:   "/tmp/openusage-old",
		LatestReleaseURL: "https://api.github.com/repos/janekbaraniewski/openusage/releases/latest",
		HTTPClient:       client,
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=true")
	}
	if transport.lastReq == nil {
		t.Fatal("expected request to be captured")
	}
	gotAuth := transport.lastReq.Header.Get("Authorization")
	gotAccept := transport.lastReq.Header.Get("Accept")
	gotAgent := transport.lastReq.Header.Get("User-Agent")
	if gotAuth != "Bearer test-token-123" {
		t.Fatalf("Authorization header = %q, want Bearer test-token-123", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Fatalf("Accept header = %q, want application/vnd.github+json", gotAccept)
	}
	if gotAgent != "openusage/v1.2.0" {
		t.Fatalf("User-Agent header = %q, want openusage/v1.2.0", gotAgent)
	}
}

func TestCheckDoesNotForwardGitHubTokenHeaderForNonGitHubURL(t *testing.T) {
	t.Setenv("OPENUSAGE_GITHUB_TOKEN", "test-token-123")

	transport := &captureTransport{}
	client := &http.Client{Transport: transport}

	result, err := Check(context.Background(), CheckOptions{
		CurrentVersion:   "v1.2.0",
		ExecutablePath:   "/tmp/openusage-old",
		LatestReleaseURL: "https://example.com/releases/latest",
		HTTPClient:       client,
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=true")
	}
	if transport.lastReq == nil {
		t.Fatal("expected request to be captured")
	}
	if got := transport.lastReq.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization header = %q, want empty for non-GitHub URL", got)
	}
}

func TestShouldAttachGitHubToken(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "github https", url: "https://api.github.com/repos/x/y/releases/latest", want: true},
		{name: "github http", url: "http://api.github.com/repos/x/y/releases/latest", want: false},
		{name: "other host https", url: "https://example.com/repos/x/y/releases/latest", want: false},
		{name: "invalid", url: "://bad", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAttachGitHubToken(tt.url)
			if got != tt.want {
				t.Fatalf("shouldAttachGitHubToken(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
