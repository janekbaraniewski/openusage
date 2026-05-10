package detect

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withFakeCodexAuth writes ~/.codex/auth.json + a fake `codex` binary on PATH,
// then rewires HOME so detectCodex picks them up.
func withFakeCodexAuth(t *testing.T, authBody string) (home string) {
	t.Helper()
	home = t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(filepath.Join(codexDir, "sessions"), 0o700); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authBody), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write codex bin: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", binDir)
	t.Setenv("OPENAI_API_KEY", "")
	return home
}

// makeFakeIDToken returns a JWT with the given claims base64-encoded in the payload.
// The header and signature are dummies — extractCodexAuth only decodes the payload.
func makeFakeIDToken(t *testing.T, claims map[string]interface{}) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return "eyJhbGciOiJIUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestDetectCodex_ExtractsOpenAIAPIKey(t *testing.T) {
	body := `{
		"OPENAI_API_KEY": "sk-codex-stored-key-1234567890",
		"tokens": {"id_token": "", "access_token": "", "refresh_token": ""},
		"account_id": "acc_abc"
	}`
	withFakeCodexAuth(t, body)

	var result Result
	detectCodex(&result)

	var openai, codex bool
	for _, a := range result.Accounts {
		if a.Provider == "openai" && a.ID == "openai" {
			openai = true
			if a.Token != "sk-codex-stored-key-1234567890" {
				t.Errorf("openai Token = %q, want sk-codex-stored-key-1234567890", a.Token)
			}
			if a.Hint("credential_source", "") != "codex_auth_json" {
				t.Errorf("openai credential_source = %q, want codex_auth_json", a.Hint("credential_source", ""))
			}
		}
		if a.Provider == "codex" && a.ID == "codex-cli" {
			codex = true
		}
	}
	if !openai {
		t.Errorf("expected openai account from codex auth.json, got accounts: %+v", result.Accounts)
	}
	if !codex {
		t.Errorf("expected codex-cli account, got accounts: %+v", result.Accounts)
	}
}

func TestDetectCodex_EnvVarBeatsAuthJSON(t *testing.T) {
	body := `{"OPENAI_API_KEY": "sk-from-file", "tokens": {}, "account_id": "x"}`
	withFakeCodexAuth(t, body)
	t.Setenv("OPENAI_API_KEY", "sk-from-env-1234567890")

	var result Result
	detectCodex(&result)

	for _, a := range result.Accounts {
		if a.Provider == "openai" {
			t.Errorf("expected detectCodex to skip openai when OPENAI_API_KEY env is set; got %+v", a)
		}
	}
}

func TestDetectCodex_NoAPIKey_StillEmitsCodexAccount(t *testing.T) {
	body := `{"tokens": {"id_token": "` + makeFakeIDToken(t, map[string]interface{}{
		"email": "user@example.com",
		"https://api.openai.com/auth": map[string]interface{}{
			"chatgpt_plan_type": "plus",
		},
	}) + `"}, "account_id": "acc_xyz"}`
	withFakeCodexAuth(t, body)

	var result Result
	detectCodex(&result)

	if len(result.Accounts) != 1 {
		t.Fatalf("expected 1 account (codex-cli only), got %d: %+v", len(result.Accounts), result.Accounts)
	}
	a := result.Accounts[0]
	if a.ID != "codex-cli" {
		t.Errorf("ID = %q, want codex-cli", a.ID)
	}
	if a.RuntimeHints["email"] != "user@example.com" {
		t.Errorf("email = %q, want user@example.com", a.RuntimeHints["email"])
	}
	if a.RuntimeHints["plan_type"] != "plus" {
		t.Errorf("plan_type = %q, want plus", a.RuntimeHints["plan_type"])
	}
}

func TestDetectCodex_MalformedAuthJSONIsSafe(t *testing.T) {
	withFakeCodexAuth(t, `{not json`)

	var result Result
	detectCodex(&result) // must not panic

	// codex-cli should still be registered (binary + sessions dir exist).
	var found bool
	for _, a := range result.Accounts {
		if a.ID == "codex-cli" {
			found = true
		}
		if a.Provider == "openai" {
			t.Errorf("malformed auth.json should not produce openai account: %+v", a)
		}
	}
	if !found {
		t.Errorf("expected codex-cli account even with malformed auth.json")
	}
}
