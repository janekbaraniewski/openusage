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
//
// XDG_DATA_HOME is explicitly unset so the test is hermetic regardless of
// how the parent shell is configured (some Linux distros export it).
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
	t.Setenv("XDG_DATA_HOME", "")
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

// TestDetectOpenCodeAuth_AdoptsOpenCodeGoKey covers the github issue #90 case:
// an `opencode auth login` run that lands the credential under the
// "opencode-go" provider id (the Go subscription, not Zen) must still
// produce an openusage tile. Both opencode/opencode-go map to the same
// account id because they share OPENCODE_API_KEY upstream.
func TestDetectOpenCodeAuth_AdoptsOpenCodeGoKey(t *testing.T) {
	withFakeOpenCodeAuth(t, `{
		"opencode-go": {"type": "api", "key": "sk-opencode-go-aaaaaaaaaaaa"}
	}`)

	var result Result
	detectOpenCodeAuth(&result)

	var found *core.AccountConfig
	for i := range result.Accounts {
		if result.Accounts[i].ID == "opencode" {
			found = &result.Accounts[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected opencode account from opencode-go key, got %+v", result.Accounts)
	}
	if found.Provider != "opencode" {
		t.Errorf("provider = %q, want opencode", found.Provider)
	}
	if found.Token != "sk-opencode-go-aaaaaaaaaaaa" {
		t.Errorf("Token = %q, want the api key from auth.json", found.Token)
	}
	if got := found.Hint("credential_source", ""); got != "opencode_auth_json" {
		t.Errorf("credential_source = %q, want opencode_auth_json", got)
	}
}

// TestDetectOpenCodeAuth_HonoursXDGDataHome verifies XDG_DATA_HOME wins over
// the default ~/.local/share location, matching upstream xdg-basedir semantics.
func TestDetectOpenCodeAuth_HonoursXDGDataHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG_DATA_HOME path test is unix-shaped")
	}
	tmp := t.TempDir()
	xdg := filepath.Join(tmp, "custom-xdg")
	authDir := filepath.Join(xdg, "opencode")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"opencode-go": {"type": "api", "key": "sk-from-xdg-1234"}}`
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	// Point HOME at the temp dir too so nothing else interferes. Note that
	// we deliberately do NOT create ~/.local/share/opencode under HOME — the
	// only auth.json lives at $XDG_DATA_HOME/opencode/auth.json.
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_DATA_HOME", xdg)

	var result Result
	detectOpenCodeAuth(&result)

	var found *core.AccountConfig
	for i := range result.Accounts {
		if result.Accounts[i].ID == "opencode" {
			found = &result.Accounts[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected opencode account via XDG_DATA_HOME, got %+v", result.Accounts)
	}
	if found.Token != "sk-from-xdg-1234" {
		t.Errorf("Token = %q, want sk-from-xdg-1234 (XDG_DATA_HOME path not consulted)", found.Token)
	}
}

// TestDetectOpenCodeAuth_DarwinAppSupportFallback verifies macOS users who put
// auth.json under ~/Library/Application Support/opencode/ (the Apple-native
// location) still get auto-detected even though OpenCode's default lives at
// the XDG path.
func TestDetectOpenCodeAuth_DarwinAppSupportFallback(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Apple-native path fallback is darwin-only")
	}
	tmp := t.TempDir()
	authDir := filepath.Join(tmp, "Library", "Application Support", "opencode")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"opencode-go": {"type": "api", "key": "sk-from-appsupport-1234"}}`
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_DATA_HOME", "")

	var result Result
	detectOpenCodeAuth(&result)

	var found *core.AccountConfig
	for i := range result.Accounts {
		if result.Accounts[i].ID == "opencode" {
			found = &result.Accounts[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected opencode account via Library/Application Support fallback, got %+v", result.Accounts)
	}
	if found.Token != "sk-from-appsupport-1234" {
		t.Errorf("Token = %q, want sk-from-appsupport-1234", found.Token)
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
