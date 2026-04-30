package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// withFakeOpenCodeAuth writes an auth.json under a temp HOME and rewires
// HOME so detectOpenCodeAuth picks it up. Returns the temp dir; t.Cleanup
// restores the previous environment.
func withFakeOpenCodeAuth(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("opencode auth path test is unix-shaped")
	}
	tmp := t.TempDir()
	authDir := filepath.Join(tmp, ".local", "share", "opencode")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	t.Setenv("HOME", tmp)
	return tmp
}

func TestDetectOpenCodeAuth_AdoptsAPIKeyEntries(t *testing.T) {
	withFakeOpenCodeAuth(t, `{
		"moonshotai": {"type": "api", "key": "sk-moonshot-1234567890abcdef"},
		"openrouter": {"type": "api", "key": "sk-or-v1-aaaaaaaaaaaa"},
		"zai":        {"type": "api", "key": "zai-aaaa.bbbb"},
		"anthropic":  {"type": "oauth", "refresh": "r", "access": "a", "expires": 1},
		"openai":     {"type": "oauth", "refresh": "r", "access": "a", "expires": 1, "accountId": "id"}
	}`)

	var result Result
	detectOpenCodeAuth(&result)

	want := map[string]string{
		"moonshot-ai": "moonshot",
		"openrouter":  "openrouter",
		"zai":         "zai",
	}
	got := map[string]string{}
	for _, a := range result.Accounts {
		got[a.ID] = a.Provider
	}
	for accountID, providerID := range want {
		if got[accountID] != providerID {
			t.Errorf("account %q provider = %q, want %q (full result: %+v)", accountID, got[accountID], providerID, got)
		}
	}
	// OAuth-typed slots must NOT create accounts (we don't support OAuth-as-API-key).
	for _, a := range result.Accounts {
		if a.Provider == "anthropic" || a.Provider == "openai" || a.Provider == "google" {
			t.Errorf("unexpected oauth-derived account: %+v", a)
		}
	}
	// Tokens must land on the account so Fetch() can use them at runtime.
	for _, a := range result.Accounts {
		if a.ID == "moonshot-ai" && a.Token != "sk-moonshot-1234567890abcdef" {
			t.Errorf("moonshot Token = %q, want the api key from auth.json", a.Token)
		}
	}
	// Provenance hint should be set so we can debug where the key came from.
	for _, a := range result.Accounts {
		if a.Hint("credential_source", "") != "opencode_auth_json" {
			t.Errorf("account %q missing credential_source hint", a.ID)
		}
	}
}

func TestDetectOpenCodeAuth_EnvVarWins(t *testing.T) {
	// Existing env-var-derived account must NOT be overwritten by opencode auth.
	withFakeOpenCodeAuth(t, `{
		"moonshotai": {"type": "api", "key": "from-opencode"}
	}`)

	var result Result
	// Simulate detectEnvKeys having already populated the slot.
	addAccount(&result, core.AccountConfig{
		ID:        "moonshot-ai",
		Provider:  "moonshot",
		Auth:      "api_key",
		APIKeyEnv: "MOONSHOT_API_KEY",
	})

	detectOpenCodeAuth(&result)

	for _, a := range result.Accounts {
		if a.ID == "moonshot-ai" {
			if a.APIKeyEnv != "MOONSHOT_API_KEY" {
				t.Errorf("env-var account got overwritten: %+v", a)
			}
			if a.Token == "from-opencode" {
				t.Errorf("opencode token leaked into env-var account: %+v", a)
			}
		}
	}
}

func TestDetectOpenCodeAuth_MissingFileIsSilent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	var result Result
	detectOpenCodeAuth(&result) // must not panic, must not add accounts
	if len(result.Accounts) != 0 {
		t.Errorf("expected no accounts when auth.json missing, got %+v", result.Accounts)
	}
}

func TestDetectOpenCodeAuth_MalformedJSONLogsAndContinues(t *testing.T) {
	withFakeOpenCodeAuth(t, `{not-json`)

	var result Result
	detectOpenCodeAuth(&result) // must not panic
	if len(result.Accounts) != 0 {
		t.Errorf("expected no accounts on malformed json, got %+v", result.Accounts)
	}
}

func TestMaskKey(t *testing.T) {
	if got := maskKey("sk-moonshot-1234567890abcdef"); got != "sk-m...cdef" {
		t.Errorf("maskKey long = %q, want sk-m...cdef", got)
	}
	if got := maskKey("short"); got != "****" {
		t.Errorf("maskKey short = %q, want ****", got)
	}
}
