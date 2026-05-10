package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// withCleanCredentialEnv resets HOME / APPDATA / known env vars to safe
// defaults so credential-file probes only see what tests deliberately set up.
func withCleanCredentialEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("PATH", "")
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "")
	for _, m := range envKeyMapping {
		t.Setenv(m.EnvVar, "")
	}
	return home
}

func TestProbeClaudeCodeCredentialsFile_AnnotatesExistingAccount(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS uses the keychain probe instead of this file path")
	}
	home := withCleanCredentialEnv(t)

	credPath := filepath.Join(home, ".claude", ".credentials.json")
	if err := os.MkdirAll(filepath.Dir(credPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"accessToken": "ya29.fake-claude-access-token", "refreshToken": "rt-fake", "expiresAt": 9999999999}`
	if err := os.WriteFile(credPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var result Result
	probeClaudeCodeCredentialsFile(&result)

	if len(result.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %+v", result.Accounts)
	}
	a := result.Accounts[0]
	if a.ID != "claude-code" {
		t.Errorf("ID = %q", a.ID)
	}
	if a.Provider != "claude_code" {
		t.Errorf("Provider = %q", a.Provider)
	}
	if got := a.Hint("credential_source", ""); got == "" {
		t.Errorf("credential_source not set")
	}
}

func TestProbeClaudeCodeCredentialsFile_DarwinIsNoOp(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only assertion")
	}
	home := withCleanCredentialEnv(t)
	credPath := filepath.Join(home, ".claude", ".credentials.json")
	if err := os.MkdirAll(filepath.Dir(credPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(credPath, []byte(`{"accessToken":"x"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var result Result
	probeClaudeCodeCredentialsFile(&result)
	for _, a := range result.Accounts {
		if a.ID == "claude-code" {
			t.Errorf("on darwin, file probe should defer to keychain probe; got %+v", a)
		}
	}
}

func TestProbeGHHostsFile_AnnotatesExistingCopilot(t *testing.T) {
	home := withCleanCredentialEnv(t)
	hostsDir := filepath.Join(home, ".config", "gh")
	if runtime.GOOS == "windows" {
		hostsDir = filepath.Join(home, "AppData", "Roaming", "GitHub CLI")
	}
	if err := os.MkdirAll(hostsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `github.com:
    user: test-user
    oauth_token: ghu_fake12345abcdef67890
    git_protocol: https
`
	if err := os.WriteFile(filepath.Join(hostsDir, "hosts.yml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var result Result
	probeGHHostsFile(&result)

	var found bool
	for _, a := range result.Accounts {
		if a.ID == "copilot" && a.Provider == "copilot" {
			found = true
			if a.Hint("credential_source", "") == "" {
				t.Errorf("credential_source not set")
			}
		}
	}
	if !found {
		t.Fatalf("expected copilot account to be created, got %+v", result.Accounts)
	}
}

func TestProbeGHHostsFile_NoOAuthTokenIsNoOp(t *testing.T) {
	home := withCleanCredentialEnv(t)
	hostsDir := filepath.Join(home, ".config", "gh")
	if runtime.GOOS == "windows" {
		hostsDir = filepath.Join(home, "AppData", "Roaming", "GitHub CLI")
	}
	if err := os.MkdirAll(hostsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// hosts.yml without oauth_token (e.g. only user field, or alt host).
	body := `github.com:
    user: test-user
    git_protocol: https
`
	if err := os.WriteFile(filepath.Join(hostsDir, "hosts.yml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var result Result
	probeGHHostsFile(&result)
	if len(result.Accounts) != 0 {
		t.Errorf("expected 0 accounts when oauth_token absent, got %+v", result.Accounts)
	}
}

func TestProbeGcloudADCFile_AnnotatesGeminiAccount(t *testing.T) {
	home := withCleanCredentialEnv(t)
	adcDir := filepath.Join(home, ".config", "gcloud")
	if runtime.GOOS == "windows" {
		adcDir = filepath.Join(home, "AppData", "Roaming", "gcloud")
	}
	if err := os.MkdirAll(adcDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{
		"type": "authorized_user",
		"client_id": "fake.apps.googleusercontent.com",
		"client_secret": "fake-secret",
		"refresh_token": "1//fake-refresh-token"
	}`
	if err := os.WriteFile(filepath.Join(adcDir, "application_default_credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Pre-existing gemini_api account that the probe should annotate.
	result := Result{Accounts: []core.AccountConfig{{
		ID:       "gemini-api",
		Provider: "gemini_api",
		Auth:     "api_key",
	}}}
	probeGcloudADCFile(&result)

	if len(result.Accounts) != 1 {
		t.Fatalf("expected 1 account (annotation, no new), got %d", len(result.Accounts))
	}
	if result.Accounts[0].Hint("gcloud_adc", "") == "" {
		t.Errorf("gcloud_adc hint not set on gemini account")
	}
}

func TestProbeGcloudADCFile_ServiceAccountSkipped(t *testing.T) {
	home := withCleanCredentialEnv(t)
	adcDir := filepath.Join(home, ".config", "gcloud")
	if runtime.GOOS == "windows" {
		adcDir = filepath.Join(home, "AppData", "Roaming", "gcloud")
	}
	if err := os.MkdirAll(adcDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"type": "service_account", "client_email": "sa@p.iam.gserviceaccount.com"}`
	if err := os.WriteFile(filepath.Join(adcDir, "application_default_credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	result := Result{Accounts: []core.AccountConfig{{ID: "gemini-api", Provider: "gemini_api"}}}
	probeGcloudADCFile(&result)
	if v := result.Accounts[0].Hint("gcloud_adc", ""); v != "" {
		t.Errorf("service-account JSON should not annotate; got %q", v)
	}
}

func TestProbeAllFiles_NoFilesPresent_NoOp(t *testing.T) {
	withCleanCredentialEnv(t)
	var result Result
	detectCredentialFiles(&result) // must not panic
	if len(result.Accounts) != 0 {
		t.Errorf("expected 0 accounts, got %+v", result.Accounts)
	}
}
