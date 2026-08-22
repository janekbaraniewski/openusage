package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestDetectMakora_None(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())
	t.Setenv("MAKORA_SESSION_TOKEN", "")
	t.Setenv("MAKORA_EMAIL", "")
	t.Setenv("MAKORA_PASSWORD", "")

	var result Result
	detectMakora(&result)

	for _, a := range result.Accounts {
		if a.ID == "makora" {
			t.Errorf("expected no makora account; got %+v", a)
		}
	}
}

func TestDetectMakora_FromSessionTokenEnv(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())
	t.Setenv("MAKORA_SESSION_TOKEN", "jwt-abc")
	t.Setenv("MAKORA_EMAIL", "")
	t.Setenv("MAKORA_PASSWORD", "")

	var result Result
	detectMakora(&result)

	acct := findMakoraAccount(t, result)
	if v := acct.Hint("auth_source", ""); v != "env_session" {
		t.Errorf("auth_source = %q, want env_session", v)
	}
}

func TestDetectMakora_FromStateFile(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())
	t.Setenv("MAKORA_SESSION_TOKEN", "")
	t.Setenv("MAKORA_EMAIL", "")
	t.Setenv("MAKORA_PASSWORD", "")

	dir := filepath.Join(home, ".config", "makora-usage")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state.json")
	if err := os.WriteFile(state, []byte(`{"access_token":"x","refresh_token":"y"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var result Result
	detectMakora(&result)

	acct := findMakoraAccount(t, result)
	if got := acct.Path("state_file", ""); got != state {
		t.Errorf("state_file = %q, want %q", got, state)
	}
}

func TestDetectMakora_FromPasswordEnv(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())
	t.Setenv("MAKORA_SESSION_TOKEN", "")
	t.Setenv("MAKORA_EMAIL", "user@example.com")
	t.Setenv("MAKORA_PASSWORD", "sekret")

	var result Result
	detectMakora(&result)

	acct := findMakoraAccount(t, result)
	if v := acct.Hint("auth_source", ""); v != "password" {
		t.Errorf("auth_source = %q, want password", v)
	}
}

func TestDetectMakora_FromBinary(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("MAKORA_SESSION_TOKEN", "")
	t.Setenv("MAKORA_EMAIL", "")
	t.Setenv("MAKORA_PASSWORD", "")

	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "makora-usage")
	t.Setenv("PATH", binDir)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", binDir)

	var result Result
	detectMakora(&result)

	findMakoraAccount(t, result)

	var toolFound bool
	for _, tool := range result.Tools {
		if tool.Name == "makora-usage" {
			toolFound = true
		}
	}
	if !toolFound {
		t.Errorf("expected makora-usage tool entry; tools=%+v", result.Tools)
	}
}

func findMakoraAccount(t *testing.T, result Result) core.AccountConfig {
	t.Helper()
	for _, a := range result.Accounts {
		if a.ID == "makora" && a.Provider == "makora" {
			return a
		}
	}
	t.Fatalf("expected makora account; accounts=%+v", result.Accounts)
	return core.AccountConfig{}
}
